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

package codingagent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// fakeOCSurface records the create-chain order for OCDispatcher tests.
type fakeOCSurface struct {
	mu sync.Mutex

	calls  []string
	create *openchoreo.CreateComponentRequest
	load   openchoreo.WorkloadInput
	rel    string
	bind   [2]string // environment, releaseName

	createErr              error
	ensureTypeErr          error
	simulateCreateConflict bool // mirrors ComponentClient 409 → GetComponent
	componentUID           string
}

func (f *fakeOCSurface) note(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op)
}

func (f *fakeOCSurface) EnsureComponentType(_ context.Context, _ string, _ map[string]any) error {
	f.note("ensure-type")
	return f.ensureTypeErr
}

func (f *fakeOCSurface) CreateComponent(_ context.Context, _, _ string, req *openchoreo.CreateComponentRequest) (*gen.Component, error) {
	f.note("create-component")
	f.mu.Lock()
	f.create = req
	err := f.createErr
	simulateConflict := f.simulateCreateConflict
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if simulateConflict {
		// ComponentClient.CreateComponent coalesces 409 into a GetComponent refetch.
		f.note("create-conflict-refetch")
		return &gen.Component{Name: req.Name, DisplayName: "pre-existing", UID: f.componentUID}, nil
	}
	return &gen.Component{Name: req.Name, UID: f.componentUID}, nil
}

func (f *fakeOCSurface) EnsureWorkload(_ context.Context, _, _ string, in openchoreo.WorkloadInput) error {
	f.note("ensure-workload")
	f.mu.Lock()
	f.load = in
	f.mu.Unlock()
	return nil
}

func (f *fakeOCSurface) EnsureRelease(_ context.Context, _, _, _, releaseName string) (string, error) {
	f.note("ensure-release")
	f.mu.Lock()
	f.rel = releaseName
	f.mu.Unlock()
	return releaseName, nil
}

func (f *fakeOCSurface) EnsureReleaseBinding(_ context.Context, _, _, _, environment, releaseName string) error {
	f.note("ensure-binding")
	f.mu.Lock()
	f.bind = [2]string{environment, releaseName}
	f.mu.Unlock()
	return nil
}

func ocDispatchInputs() OCDispatchInputs {
	return OCDispatchInputs{
		OrgID:           "acme",
		ProjectID:       "widgets",
		CycleID:         "11111111-1111-1111-1111-111111111111",
		MilestoneNumber: 4,
		MilestoneTitle:  "v3",
		Kind:            "coding",
		RunName:         "ca-11111111-2608061200",
		Image:           "ghcr.io/wso2/aep/remote-worker:latest",
		Env: map[string]string{
			"AEP_TASK_ID":   "11111111-1111-1111-1111-111111111111",
			"AEP_TASK_KIND": "implementation",
		},
		SecretEnv: []SecretEnvRef{
			{Key: "ANTHROPIC_API_KEY", SecretName: "acme-anthropic-secrets", SecretKey: "api-key"},
		},
	}
}

func TestOCDispatcher_HappyCreateChain(t *testing.T) {
	fake := &fakeOCSurface{}
	d := NewOCDispatcher(fake, testWriteTargets())

	got, err := d.Dispatch(context.Background(), ocDispatchInputs())
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if got.RunName != "ca-11111111-2608061200" {
		t.Errorf("Dispatch returned %q, want RunName", got.RunName)
	}
	want := []string{"ensure-type", "create-component", "ensure-workload", "ensure-release", "ensure-binding"}
	if fmt.Sprint(fake.calls) != fmt.Sprint(want) {
		t.Errorf("chain = %v, want %v", fake.calls, want)
	}
	if fake.create.Type != openchoreo.CodingAgentComponentTypeRef {
		t.Errorf("component type = %q, want %q", fake.create.Type, openchoreo.CodingAgentComponentTypeRef)
	}
	if fake.create.AutoBuild || fake.create.AutoDeploy {
		t.Error("agent component must not auto-build or auto-deploy")
	}
	if fake.create.DisplayName != "Coding cycle — milestone #4 v3" {
		t.Errorf("displayName = %q", fake.create.DisplayName)
	}
	for key, wantVal := range map[string]string{
		string(openchoreo.LabelKeyAepInternal):  openchoreo.LabelValueAepInternal,
		string(openchoreo.LabelKeyAepMilestone): "4",
		string(openchoreo.LabelKeyAepCycle):     "11111111-1111-1111-1111-111111111111",
		string(openchoreo.LabelKeyAepRunName):   "ca-11111111-2608061200",
		string(openchoreo.LabelKeyK8sManagedBy): openchoreo.LabelValueAep,
		string(openchoreo.LabelKeyK8sPartOf):    openchoreo.LabelValueAep,
		string(openchoreo.LabelKeyK8sName):      openchoreo.CodingAgentComponentTypeName,
	} {
		if fake.create.Labels[key] != wantVal {
			t.Errorf("label %s = %q, want %q", key, fake.create.Labels[key], wantVal)
		}
	}
	if fake.bind[0] != "development" {
		t.Errorf("bound env = %q, want the project's write target", fake.bind[0])
	}
	if fake.bind[1] != fake.rel {
		t.Errorf("binding release %q != generated release %q", fake.bind[1], fake.rel)
	}
	if fake.load.Image != "ghcr.io/wso2/aep/remote-worker:latest" {
		t.Errorf("workload image = %q", fake.load.Image)
	}
}

// The Job is bound into the project's own write target, and the result says
// which one so the cycle can record it.
func TestOCDispatcher_BindsIntoTheProjectsWriteTarget(t *testing.T) {
	fake := &fakeOCSurface{}
	d := NewOCDispatcher(fake, &fakeWriteTargets{env: "dev-b"})

	got, err := d.Dispatch(context.Background(), ocDispatchInputs())
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if fake.bind[0] != "dev-b" {
		t.Errorf("bound env = %q, want dev-b", fake.bind[0])
	}
	want := OCDispatchResult{RunName: "ca-11111111-2608061200", Environment: "dev-b"}
	if got != want {
		t.Errorf("Dispatch = %+v, want %+v", got, want)
	}
}

// A project whose pipeline names no write target is refused before
// CreateComponent, so no billed Component is minted for a Job that could not
// be bound anywhere, and the refusal carries the delivery sentinel the
// supervisor classifies.
func TestOCDispatcher_NoWriteTargetFailsBeforeCreate(t *testing.T) {
	fake := &fakeOCSurface{}
	cause := &openchoreo.ErrNoWriteTarget{Org: "acme", Project: "widgets", Cause: openchoreo.ErrPipelineRefMissing}
	d := NewOCDispatcher(fake, &fakeWriteTargets{err: cause})

	_, err := d.Dispatch(context.Background(), ocDispatchInputs())
	if !errors.Is(err, delivery.ErrNoWriteTarget) {
		t.Fatalf("want delivery.ErrNoWriteTarget, got %v", err)
	}
	var nwt *openchoreo.ErrNoWriteTarget
	if !errors.As(err, &nwt) {
		t.Errorf("the resolver's cause must survive, got %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("chain = %v, want nothing written", fake.calls)
	}
}

// A transient resolve failure is not a configuration fact: it is returned as a
// plain launch error, and still nothing is created.
func TestOCDispatcher_TransientResolveFailureIsNotNoWriteTarget(t *testing.T) {
	fake := &fakeOCSurface{}
	d := NewOCDispatcher(fake, &fakeWriteTargets{err: fmt.Errorf("%w: 503", openchoreo.ErrInternalServerError)})

	_, err := d.Dispatch(context.Background(), ocDispatchInputs())
	if err == nil || errors.Is(err, delivery.ErrNoWriteTarget) {
		t.Fatalf("want a plain launch error, got %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("chain = %v, want nothing written", fake.calls)
	}
}

func TestOCDispatcher_ConflictIsSuccess(t *testing.T) {
	// Mirrors ComponentClient: CreateComponent 409 → refetch → nil error.
	// Dispatch must still walk Workload → Release → Binding over the existing
	// Component name.
	fake := &fakeOCSurface{simulateCreateConflict: true}
	d := NewOCDispatcher(fake, testWriteTargets())

	got, err := d.Dispatch(context.Background(), ocDispatchInputs())
	if err != nil {
		t.Fatalf("Dispatch after conflict-as-success: %v", err)
	}
	if got.RunName != "ca-11111111-2608061200" {
		t.Errorf("got %q", got.RunName)
	}
	want := []string{
		"ensure-type", "create-component", "create-conflict-refetch",
		"ensure-workload", "ensure-release", "ensure-binding",
	}
	if fmt.Sprint(fake.calls) != fmt.Sprint(want) {
		t.Errorf("chain = %v, want conflict-refetch path then downstream ensures", fake.calls)
	}
	if fake.load.ComponentName != "ca-11111111-2608061200" {
		t.Errorf("workload component = %q, want the existing run name", fake.load.ComponentName)
	}
}

func TestOCDispatcher_402MapsToQuotaExceeded(t *testing.T) {
	fake := &fakeOCSurface{
		createErr: fmt.Errorf("%w: create component ca-x: quota", openchoreo.ErrPaymentRequired),
	}
	d := NewOCDispatcher(fake, testWriteTargets())

	_, err := d.Dispatch(context.Background(), ocDispatchInputs())
	if !errors.Is(err, delivery.ErrAgentQuotaExceeded) {
		t.Fatalf("want delivery.ErrAgentQuotaExceeded, got %v", err)
	}
	if fmt.Sprint(fake.calls) != fmt.Sprint([]string{"ensure-type", "create-component"}) {
		t.Errorf("chain must stop after 402 create, got %v", fake.calls)
	}
}

func TestOCDispatcher_ValidationDisplayName(t *testing.T) {
	fake := &fakeOCSurface{}
	d := NewOCDispatcher(fake, testWriteTargets())
	in := ocDispatchInputs()
	in.Kind = "validation"

	if _, err := d.Dispatch(context.Background(), in); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if fake.create.DisplayName != "Validation cycle — milestone #4 v3" {
		t.Errorf("displayName = %q", fake.create.DisplayName)
	}
}

// U1: the configured Job TTL is rendered per Component as the ComponentType's
// ttlSecondsAfterFinished parameter, in whole seconds.
func TestDispatch_RendersTheConfiguredJobTTL(t *testing.T) {
	fake := &fakeOCSurface{}
	d := NewOCDispatcher(fake, testWriteTargets()).WithImage("img").WithJobTTL(600 * time.Second)
	if _, err := d.Dispatch(context.Background(), ocDispatchInputs()); err != nil {
		t.Fatal(err)
	}
	if got := fake.create.Parameters["ttlSecondsAfterFinished"]; got != 600 {
		t.Fatalf("ttlSecondsAfterFinished = %v, want 600 (U1)", got)
	}
}

// No TTL configured leaves the parameter out, so the schema default applies.
func TestDispatch_NoJobTTLLeavesTheSchemaDefault(t *testing.T) {
	fake := &fakeOCSurface{}
	d := NewOCDispatcher(fake, testWriteTargets()).WithImage("img")
	if _, err := d.Dispatch(context.Background(), ocDispatchInputs()); err != nil {
		t.Fatal(err)
	}
	if got, ok := fake.create.Parameters["ttlSecondsAfterFinished"]; ok {
		t.Fatalf("ttlSecondsAfterFinished = %v, want unset", got)
	}
}

// The cycle row stores the Component UID the dispatch minted (or, on the 409
// path, re-read), so the settler can later delete exactly that Component.
func TestDispatch_ReportsTheComponentUID(t *testing.T) {
	for name, conflict := range map[string]bool{"created": false, "conflict re-read": true} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeOCSurface{componentUID: "uid-9", simulateCreateConflict: conflict}
			d := NewOCDispatcher(fake, testWriteTargets())
			got, err := d.Dispatch(context.Background(), ocDispatchInputs())
			if err != nil {
				t.Fatalf("Dispatch: %v", err)
			}
			if got.ComponentUID != "uid-9" {
				t.Errorf("ComponentUID = %q, want uid-9", got.ComponentUID)
			}
		})
	}
}
