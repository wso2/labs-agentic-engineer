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

package aestudio

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

var errFake = errors.New("openchoreo unavailable")

// Review Focus 2: what OpenChoreo fills in on its own is never drift.
func TestDrift_LiveWithOCDefaultsEqualsDesired(t *testing.T) {
	f := newFixture(t).withAllRefs()
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(t)
	f.oc.fillDefaults() // OC adds schema defaults (modelConnection "", githubOwner "", storage defaults) and stamps labels
	f.oc.setReady(true)
	f.oc.resetCalls()
	st, _ := f.svc.Status(userCtx(), "default")
	if st.State != StateReady || st.URLs.Tools == "" || f.oc.writes() != 0 {
		t.Fatalf("%+v writes=%v", st, f.oc.calls)
	}
	// And a converge over the defaulted objects has nothing to write.
	f.svc.Trigger(userCtx(), "default")
	f.waitConverged(t)
	if f.oc.writes() != 0 {
		t.Fatalf("a converge over OC-defaulted objects wrote: %v", f.oc.calls)
	}
}

func TestStatus_States(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*fixture)
		want  State
	}{
		{"no gitpat reference", func(f *fixture) {}, StateAbsent},
		{"config missing", func(f *fixture) { f.withAllRefs().withoutConfig("AE_STUDIO_IMAGE_COLLAB") }, StateFailed},
		{"no write target", func(f *fixture) { f.withAllRefs().withWriteTargetErr(&openchoreo.ErrNoWriteTarget{}) }, StateFailed},
		{"fresh org", func(f *fixture) { f.withAllRefs() }, StateProvisioning},
		{"image drift", func(f *fixture) { f.withAllRefs().converged().withImage("ae-collab:new") }, StateProvisioning},
		{"rt hash drift", func(f *fixture) { f.withAllRefs().converged().withLiveHash("old") }, StateProvisioning},
		{"pin behind latestRelease", func(f *fixture) { f.withAllRefs().converged().withNewRelease() }, StateProvisioning},
		{"binding not ready yet", func(f *fixture) { f.withAllRefs().converged().withReady(false) }, StateProvisioning},
		{"ready", func(f *fixture) { f.withAllRefs().converged().withReady(true) }, StateReady},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			c.setup(f)
			st, err := f.svc.Status(userCtx(), "default")
			if err != nil || st.State != c.want {
				t.Fatalf("state %s err %v", st.State, err)
			}
		})
	}
}

// Drift answers provisioning at once and converges; a binding that is only
// not Ready yet is waited on, not converged.
func TestStatus_DriftConvergesNotReadyWaits(t *testing.T) {
	f := newFixture(t).withAllRefs().converged().withNewRelease()
	f.svc.Status(userCtx(), "default")
	f.waitConverged(t)
	if f.oc.rrb.Spec.ResourceRelease != f.oc.release {
		t.Fatalf("pin %s, latest %s", f.oc.rrb.Spec.ResourceRelease, f.oc.release)
	}
	f.oc.resetCalls()
	f.withReady(false)
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateProvisioning || f.svc.busy("default") || f.oc.writes() != 0 {
		t.Fatalf("state %s busy %v calls %v", st.State, f.svc.busy("default"), f.oc.calls)
	}
}

func TestStatus_ReadyCarriesURLsAndOUID(t *testing.T) {
	f := newFixture(t).withAllRefs().converged()
	st, err := f.svc.Status(userCtx(), "default")
	if err != nil || st.State != StateReady {
		t.Fatalf("state %s err %v", st.State, err)
	}
	if *st.URLs != (URLs{DesignAgent: "http://default-ae-design-agent.gw", Collab: "ws://default-ae-collab.gw", Tools: "http://default-ae-studio-tools.gw"}) {
		t.Fatalf("urls %+v", st.URLs)
	}
	if st.OUID != testOU {
		t.Fatalf("OUID %q: the OU id the pod pins (parameters.org.id)", st.OUID)
	}
}

func TestStatus_ReadErrorIsAnError(t *testing.T) {
	f := newFixture(t).withAllRefs().converged().withWriteTargetErr(errFake)
	if _, err := f.svc.Status(userCtx(), "default"); !errors.Is(err, errFake) {
		t.Fatalf("err %v", err)
	}
}

func TestSameOnOurKeys(t *testing.T) {
	cases := []struct {
		name      string
		want, got string
		same      bool
	}{
		{"extra live keys", `{"a":1,"b":{"c":"x"}}`, `{"a":1,"b":{"c":"x","d":2},"e":true}`, true},
		{"changed value", `{"a":1}`, `{"a":2}`, false},
		{"missing key", `{"a":1,"b":""}`, `{"a":1}`, false},
		{"empty array dropped", `{"d":[]}`, `{}`, true},
		{"empty array null", `{"d":[]}`, `{"d":null}`, true},
		{"array items projected", `{"d":[{"k":"v"}]}`, `{"d":[{"k":"v","x":1}]}`, true},
		{"array item changed", `{"d":[{"k":"v"}]}`, `{"d":[{"k":"w"}]}`, false},
		{"array grew", `{"d":[1]}`, `{"d":[1,2]}`, false},
		{"string not number", `{"n":"2147483648"}`, `{"n":2147483648}`, false},
		{"nothing live", `{"a":1}`, ``, false},
	}
	for _, c := range cases {
		if got := sameOnOurKeys(json.RawMessage(c.want), json.RawMessage(c.got)); got != c.same {
			t.Errorf("%s: same=%v", c.name, got)
		}
	}
}

// A deleted ProjectReleaseBinding (no cell namespace) is drift even when the
// RRB still reads Ready.
func TestStatus_MissingProjectBindingIsDrift(t *testing.T) {
	f := newFixture(t).withAllRefs().converged()
	f.oc.mu.Lock()
	f.oc.prb = false
	f.oc.mu.Unlock()
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateProvisioning {
		t.Fatalf("state %s", st.State)
	}
	f.waitConverged(t)
	if f.oc.count("PRB ae-system-development") != 1 {
		t.Fatalf("the converge must recreate the PRB: %v", f.oc.calls)
	}
	if st, _ := f.svc.Status(userCtx(), "default"); st.State != StateReady {
		t.Fatalf("state %s", st.State)
	}
}

// countingHandler counts the records logged at Warn or above, by message.
type countingHandler struct {
	mu   sync.Mutex
	warn map[string]int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Level >= slog.LevelWarn {
		h.warn[r.Message]++
	}
	return nil
}
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

// A polling console logs a failed answer once per desired state.
func TestStatus_FailedAnswerLoggedOncePerDesiredState(t *testing.T) {
	h := &countingHandler{warn: map[string]int{}}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	f := newFixture(t).withAllRefs().withWriteTargetErr(&openchoreo.ErrNoWriteTarget{})
	for i := 0; i < 3; i++ {
		f.svc.Status(userCtx(), "default")
	}
	f.withImage("ae-collab:new")
	f.svc.Status(userCtx(), "default")
	h.mu.Lock()
	defer h.mu.Unlock()
	if n := h.warn["ae_studio.status_failed"]; n != 2 {
		t.Fatalf("status_failed logged at Warn %d times, want once per desired state (2)", n)
	}
}
