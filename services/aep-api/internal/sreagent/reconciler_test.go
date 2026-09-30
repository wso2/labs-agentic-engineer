// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package sreagent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

const (
	testOrg   = "acme"
	testKey   = "sk-live-0123456789abcdef"
	testToken = "mcp-token-fedcba9876543210"
)

var testCfg = config.SREAgentConfig{Org: testOrg, Namespace: "obs", Deployment: "sre-agent", Secret: "sre-agent-aep"}

// fakeKube records every write, in order, and serves dep and pods back.
type fakeKube struct {
	mu       sync.Mutex
	dep      DeploymentState
	selector map[string]string
	pods     []PodState
	calls    []string
	secret   map[string][]byte
	depErr   error
	scaleErr error
	reads    chan struct{} // one send per Deployment read, when set
}

func (k *fakeKube) PatchSecretData(_ context.Context, ns, name string, data map[string][]byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, fmt.Sprintf("PatchSecretData %s/%s", ns, name))
	k.secret = data
	return nil
}

func (k *fakeKube) PatchTemplateAnnotation(_ context.Context, ns, deploy, key, value string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, fmt.Sprintf("PatchTemplateAnnotation %s/%s %s=%s", ns, deploy, key, value))
	return nil
}

func (k *fakeKube) Scale(_ context.Context, ns, deploy string, replicas int32) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.calls = append(k.calls, fmt.Sprintf("Scale %s/%s %d", ns, deploy, replicas))
	return k.scaleErr
}

func (k *fakeKube) Deployment(_ context.Context, ns, deploy string) (DeploymentState, map[string]string, error) {
	k.mu.Lock()
	dep, sel, err, reads := k.dep, k.selector, k.depErr, k.reads
	k.mu.Unlock()
	if reads != nil {
		reads <- struct{}{}
	}
	return dep, sel, err
}

func (k *fakeKube) Pods(_ context.Context, ns string, selector map[string]string) ([]PodState, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.pods, nil
}

func (k *fakeKube) writes() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return slices.Clone(k.calls)
}

// fixedTokens hands out testToken for every org.
type fixedTokens struct{}

func (fixedTokens) Ensure(context.Context, string) (string, error)    { return testToken, nil }
func (fixedTokens) Get(context.Context, string) (string, bool, error) { return testToken, true, nil }
func effective(e organization.EffectiveSRE) func(context.Context, string) (organization.EffectiveSRE, error) {
	return func(_ context.Context, org string) (organization.EffectiveSRE, error) {
		if org != testOrg {
			return organization.EffectiveSRE{}, fmt.Errorf("read for %q, want %q", org, testOrg)
		}
		return e, nil
	}
}

var override = organization.EffectiveSRE{Source: organization.SRESourceOverride, Key: testKey,
	Conn: modelconn.Connection{Format: modelconn.FormatOpenAICompatible, BaseURL: "https://api.openai.com/v1",
		Host: "api.openai.com", Model: "gpt-5.4"}}

var none = organization.EffectiveSRE{Source: organization.SRESourceNone}

func annotate(hash string) string {
	return "PatchTemplateAnnotation obs/sre-agent " + HashAnnotation + "=" + hash
}

func TestReconcile_PushesAndRestartsOnChange(t *testing.T) {
	kube := &fakeKube{dep: DeploymentState{Replicas: 0, TemplateHash: ""}}
	r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
	if err := r.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	d := DesiredFrom(override, testToken)
	want := []string{"PatchSecretData obs/sre-agent-aep", annotate(d.Hash()), "Scale obs/sre-agent 1"}
	if got := kube.writes(); !slices.Equal(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	wantData := map[string]string{"RCA_LLM_API_KEY": testKey, "RCA_MODEL_NAME": "openai:gpt-5.4",
		"RCA_LLM_BASE_URL": "https://api.openai.com/v1", "AEP_MCP_TOKEN": testToken}
	if len(kube.secret) != len(wantData) {
		t.Fatalf("secret carries %d keys, want all 4", len(kube.secret))
	}
	for k, v := range wantData {
		if string(kube.secret[k]) != v {
			t.Errorf("secret[%s] is not the desired value", k)
		}
	}
}

func TestReconcile_NoopWhenHashMatches(t *testing.T) {
	d := DesiredFrom(override, testToken)
	kube := &fakeKube{dep: DeploymentState{Replicas: 1, TemplateHash: d.Hash()}}
	r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
	if err := r.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := kube.writes(); len(got) != 0 {
		t.Fatalf("calls = %q, want none", got)
	}
}

func TestReconcile_UnconfiguredClearsAndScalesToZero(t *testing.T) {
	kube := &fakeKube{dep: DeploymentState{Replicas: 1, TemplateHash: DesiredFrom(override, testToken).Hash()}}
	r := NewReconciler(testCfg, kube, effective(none), fixedTokens{})
	if err := r.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	want := []string{"PatchSecretData obs/sre-agent-aep", annotate(Desired{}.Hash()), "Scale obs/sre-agent 0"}
	if got := kube.writes(); !slices.Equal(got, want) {
		t.Fatalf("calls = %q, want %q", got, want)
	}
	if len(kube.secret) != 4 {
		t.Fatalf("secret carries %d keys, want all 4 (the Deployment's secretKeyRefs require them)", len(kube.secret))
	}
	for k, v := range kube.secret {
		if len(v) != 0 {
			t.Errorf("secret[%s] is not cleared", k)
		}
	}
}

func TestReconcile_RestoresAfterDrift(t *testing.T) {
	d := DesiredFrom(override, testToken)

	t.Run("helm upgrade dropped the hash annotation", func(t *testing.T) {
		kube := &fakeKube{dep: DeploymentState{Replicas: 1, TemplateHash: ""}}
		r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
		if err := r.reconcile(context.Background()); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		want := []string{"PatchSecretData obs/sre-agent-aep", annotate(d.Hash())}
		if got := kube.writes(); !slices.Equal(got, want) {
			t.Fatalf("calls = %q, want %q", got, want)
		}
	})

	t.Run("helm upgrade reset replicas to 0 while configured", func(t *testing.T) {
		kube := &fakeKube{dep: DeploymentState{Replicas: 0, TemplateHash: d.Hash()}}
		r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
		if err := r.reconcile(context.Background()); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		want := []string{"Scale obs/sre-agent 1"}
		if got := kube.writes(); !slices.Equal(got, want) {
			t.Fatalf("calls = %q, want %q", got, want)
		}
	})
}

func TestReconcile_DeploymentReadFailsWritesNothing(t *testing.T) {
	kube := &fakeKube{depErr: errors.New("forbidden")}
	r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
	if err := r.reconcile(context.Background()); err == nil {
		t.Fatal("want the read error")
	}
	if got := kube.writes(); len(got) != 0 {
		t.Fatalf("calls = %q, want none", got)
	}
}

func TestKick_IgnoresOtherOrgs(t *testing.T) {
	r := NewReconciler(testCfg, &fakeKube{}, effective(override), fixedTokens{})
	r.Kick("other")
	if len(r.kick) != 0 {
		t.Fatal("Kick for another org queued a pass")
	}
	r.Kick(testOrg)
	r.Kick(testOrg) // coalesces, never blocks
	if len(r.kick) != 1 {
		t.Fatalf("queued %d passes, want 1", len(r.kick))
	}
}

func TestRun_BootPassThenKick(t *testing.T) {
	kube := &fakeKube{reads: make(chan struct{})}
	r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
	r.interval = time.Hour // only the boot pass and kicks may run a pass
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()

	pass := func(what string) {
		t.Helper()
		select {
		case <-kube.reads:
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s pass", what)
		}
	}
	pass("boot")
	r.Kick("other")
	select {
	case <-kube.reads:
		t.Fatal("a kick for another org ran a pass")
	case <-time.After(50 * time.Millisecond):
	}
	r.Kick(testOrg)
	pass("kicked")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return on cancel")
	}
}

// orderRecorder tracks the sequence of steps a reconcile pass took, across
// goroutine-safe fakes.
type orderRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *orderRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, s)
}

func (r *orderRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.order)
}

// recordingSeeder is a Seeder that records its call on rec and answers err.
type recordingSeeder struct {
	rec *orderRecorder
	err error
}

func (s recordingSeeder) ApplySeed(_ context.Context, org string) (string, error) {
	s.rec.add("seed:" + org)
	return "applied", s.err
}

// effectiveRecording is like effective(e), but also records the call on rec
// so a test can assert the seed step ran before the effective-connection read.
func effectiveRecording(rec *orderRecorder, e organization.EffectiveSRE) func(context.Context, string) (organization.EffectiveSRE, error) {
	return func(_ context.Context, org string) (organization.EffectiveSRE, error) {
		rec.add("eff:" + org)
		return e, nil
	}
}

// TestReconcile_SeederRunsFirstAndToleratesError guards the wiring a Seeder
// needs: it runs before the effective-connection read on every pass, and a
// failing Seeder does not stop the pass from pushing the (unrelated)
// effective connection it already has.
func TestReconcile_SeederRunsFirstAndToleratesError(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	rec := &orderRecorder{}
	seeder := recordingSeeder{rec: rec, err: errors.New("seed probe refused")}
	kube := &fakeKube{dep: DeploymentState{Replicas: 0, TemplateHash: ""}}
	r := NewReconciler(testCfg, kube, effectiveRecording(rec, override), fixedTokens{}).WithSeeder(seeder)

	if err := r.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := rec.snapshot(); len(got) != 2 || got[0] != "seed:"+testOrg || got[1] != "eff:"+testOrg {
		t.Fatalf("order = %v, want [seed:%s eff:%s]", got, testOrg, testOrg)
	}
	if got := kube.writes(); len(got) == 0 {
		t.Fatal("reconcile did nothing after a seeder error, want the pass to continue")
	}
	if !strings.Contains(buf.String(), "seed probe refused") {
		t.Fatalf("want the seeder error logged, got %q", buf.String())
	}
}

// TestReconcile_WithoutSeederStillReconciles guards the default: a nil
// Seeder (no seed configured) does not change behavior at all.
func TestReconcile_WithoutSeederStillReconciles(t *testing.T) {
	kube := &fakeKube{dep: DeploymentState{Replicas: 0, TemplateHash: ""}}
	r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
	if err := r.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := kube.writes(); len(got) == 0 {
		t.Fatal("reconcile did nothing, want the push to still happen without a Seeder")
	}
}

func TestReconcile_NeverLogsSecretValues(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	kube := &fakeKube{scaleErr: errors.New("scale refused"), reads: make(chan struct{}, 4)}
	r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.Run(ctx); close(done) }()
	<-kube.reads // boot pass: push, restart, a failing scale
	r.Kick(testOrg)
	<-kube.reads
	cancel()
	<-done

	mu.Lock()
	logged := buf.String()
	mu.Unlock()
	if !strings.Contains(logged, "sreagent.reconcile_failed") {
		t.Fatalf("want the failed pass logged, got %q", logged)
	}
	for name, v := range map[string]string{"API key": testKey, "MCP token": testToken, "hash": DesiredFrom(override, testToken).Hash()} {
		if strings.Contains(logged, v) {
			t.Errorf("the log carries the %s", name)
		}
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestStatus(t *testing.T) {
	ctx := context.Background()
	d := DesiredFrom(override, testToken)

	t.Run("another org is not served", func(t *testing.T) {
		r := NewReconciler(testCfg, &fakeKube{}, effective(override), fixedTokens{})
		if _, _, ok, err := r.Status(ctx, "globex"); ok || err != nil {
			t.Fatalf("ok=%v err=%v, want ok=false", ok, err)
		}
	})

	t.Run("unconfigured without reading the cluster", func(t *testing.T) {
		r := NewReconciler(testCfg, &fakeKube{depErr: errors.New("unreachable")}, effective(none), fixedTokens{})
		st, _, ok, err := r.Status(ctx, testOrg)
		if err != nil || !ok || st != string(StatusUnconfigured) {
			t.Fatalf("status=%q ok=%v err=%v", st, ok, err)
		}
	})

	t.Run("running once rolled out", func(t *testing.T) {
		kube := &fakeKube{selector: map[string]string{"app": "sre-agent"}, pods: []PodState{{Hash: d.Hash()}},
			dep: DeploymentState{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1, TemplateHash: d.Hash()}}
		r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
		st, reason, ok, err := r.Status(ctx, testOrg)
		if err != nil || !ok || st != string(StatusRunning) || reason != "" {
			t.Fatalf("status=%q reason=%q ok=%v err=%v", st, reason, ok, err)
		}
		if got := kube.writes(); len(got) != 0 {
			t.Fatalf("Status wrote %q, want a read only", got)
		}
	})

	t.Run("failed with the crashloop reason", func(t *testing.T) {
		kube := &fakeKube{selector: map[string]string{"app": "sre-agent"},
			pods: []PodState{{Hash: d.Hash(), WaitingReason: "CrashLoopBackOff", ExitCode: 1}},
			dep:  DeploymentState{Replicas: 1, TemplateHash: d.Hash()}}
		r := NewReconciler(testCfg, kube, effective(override), fixedTokens{})
		st, reason, ok, err := r.Status(ctx, testOrg)
		if err != nil || !ok || st != string(StatusFailed) || reason == "" {
			t.Fatalf("status=%q reason=%q ok=%v err=%v", st, reason, ok, err)
		}
	})

	t.Run("a read error is returned", func(t *testing.T) {
		r := NewReconciler(testCfg, &fakeKube{depErr: errors.New("forbidden")}, effective(override), fixedTokens{})
		if _, _, _, err := r.Status(ctx, testOrg); err == nil {
			t.Fatal("want the read error")
		}
	})
}
