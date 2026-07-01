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
	"testing"
	"time"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// ---- classifyBinding unit tests (Step 1 / TDD RED → GREEN) -----------------
// Pure function, no DB, no OC client. Exercises the three buckets:
//   - Ready=True          → (done=true,  failed=false)
//   - Terminal False      → (done=false, failed=true)
//   - Nil / pending       → (done=false, failed=false)

func TestClassifyBinding_Nil(t *testing.T) {
	done, failed := classifyBinding(nil)
	if done || failed {
		t.Fatalf("nil binding: want (false,false), got (%v,%v)", done, failed)
	}
}

func TestClassifyBinding_NoStatus(t *testing.T) {
	b := &openchoreo.ResourceReleaseBinding{Status: nil}
	done, failed := classifyBinding(b)
	if done || failed {
		t.Fatalf("no-status binding: want (false,false), got (%v,%v)", done, failed)
	}
}

func TestClassifyBinding_Ready(t *testing.T) {
	b := &openchoreo.ResourceReleaseBinding{
		Status: &openchoreo.ResourceReleaseBindingStatus{
			Conditions: []openchoreo.OCCondition{
				{Type: "Ready", Status: "True"},
			},
		},
	}
	done, failed := classifyBinding(b)
	if !done || failed {
		t.Fatalf("Ready=True: want (true,false), got (%v,%v)", done, failed)
	}
}

func TestClassifyBinding_ReadyFalse_TerminalReason(t *testing.T) {
	// A False condition with a non-progressing reason is terminal → failed.
	b := &openchoreo.ResourceReleaseBinding{
		Status: &openchoreo.ResourceReleaseBindingStatus{
			Conditions: []openchoreo.OCCondition{
				{Type: "ResourcesReady", Status: "False", Reason: "Failed"},
			},
		},
	}
	done, failed := classifyBinding(b)
	if done || !failed {
		t.Fatalf("terminal-False condition: want (false,true), got (%v,%v)", done, failed)
	}
}

func TestClassifyBinding_ReadyFalse_ProgressingReason(t *testing.T) {
	// A False condition with a progressing reason is NOT terminal — still waiting.
	// Includes OpenChoreo's COMPOUND reasons (e.g. "ResourcesProgressing", emitted
	// while a CNPG Postgres is "Setting up primary") — the live-E2E regression.
	for _, reason := range []string{"Creating", "Initializing", "Progressing", "Pending", "", "ResourcesProgressing", "ResourcesProvisioning"} {
		b := &openchoreo.ResourceReleaseBinding{
			Status: &openchoreo.ResourceReleaseBindingStatus{
				Conditions: []openchoreo.OCCondition{
					{Type: "ResourcesReady", Status: "False", Reason: reason},
				},
			},
		}
		done, failed := classifyBinding(b)
		if done || failed {
			t.Fatalf("progressing reason %q: want (false,false), got (%v,%v)", reason, done, failed)
		}
	}
}

func TestClassifyBinding_ReadyUnknown(t *testing.T) {
	b := &openchoreo.ResourceReleaseBinding{
		Status: &openchoreo.ResourceReleaseBindingStatus{
			Conditions: []openchoreo.OCCondition{
				{Type: "Ready", Status: "Unknown"},
			},
		},
	}
	done, failed := classifyBinding(b)
	if done || failed {
		t.Fatalf("Ready=Unknown: want (false,false), got (%v,%v)", done, failed)
	}
}

// ---- sweep integration test (Step 4) ----------------------------------------
// Requires the deployments Postgres (docker-compose). Uses the dbtest harness
// pattern: skipped when the DB is unreachable so `go test ./...` is safe without
// a local stack. Build tag: dbtest.
//
// The test inserts a `building` resource-provisioning task, runs sweep once with
// a fake ResourceClient that reports the binding as ready, and asserts:
//   - The task row reaches `deployed`.
//   - The redispatch callback fires exactly once for the task's (org, project).
//   - A second sweep with a nil binding leaves a fresh `building` task unchanged.

// fakeResourceClient is a minimal openchoreo.ResourceClient stub for sweep tests.
// It returns the configured binding for any GetBinding call; all other methods
// are no-ops that return nil.
type fakeResourceClient struct {
	binding *openchoreo.ResourceReleaseBinding
	getErr  error
}

func (f *fakeResourceClient) EnsureResourceType(_ context.Context, _ string, rt *openchoreo.ResourceType) (*openchoreo.ResourceType, error) {
	return rt, nil
}
func (f *fakeResourceClient) ApplyResource(_ context.Context, _ string, r *openchoreo.Resource) (*openchoreo.Resource, error) {
	return r, nil
}
func (f *fakeResourceClient) GetResource(_ context.Context, _, _ string) (*openchoreo.Resource, error) {
	return nil, nil
}
func (f *fakeResourceClient) EnsureBinding(_ context.Context, _ string, b *openchoreo.ResourceReleaseBinding) (*openchoreo.ResourceReleaseBinding, error) {
	return b, nil
}
func (f *fakeResourceClient) GetBinding(_ context.Context, _, _ string) (*openchoreo.ResourceReleaseBinding, error) {
	return f.binding, f.getErr
}
func (f *fakeResourceClient) DeleteBinding(_ context.Context, _, _ string) error  { return nil }
func (f *fakeResourceClient) DeleteResource(_ context.Context, _, _ string) error { return nil }
func (f *fakeResourceClient) ListClusterResourceTypes(_ context.Context) ([]openchoreo.ResourceType, error) {
	return nil, nil
}
func (f *fakeResourceClient) PatchWorkloadResourceDeps(_ context.Context, _, _ string, _ []openchoreo.WorkloadResourceDep) error {
	return nil
}
func (f *fakeResourceClient) PatchWorkloadEndpointDeps(_ context.Context, _, _ string, _ []openchoreo.WorkloadEndpointDep) error {
	return nil
}
func (f *fakeResourceClient) ListWorkloadEndpoints(_ context.Context, _ string) ([]openchoreo.WorkloadEndpointInfo, error) {
	return nil, nil
}

// readyBinding returns a ResourceReleaseBinding with Ready=True.
func readyBinding() *openchoreo.ResourceReleaseBinding {
	return &openchoreo.ResourceReleaseBinding{
		Status: &openchoreo.ResourceReleaseBindingStatus{
			Conditions: []openchoreo.OCCondition{
				{Type: "Ready", Status: "True"},
			},
		},
	}
}

// TestResourceWatcher_Sweep_ReadyBinding_DeploysTask is a placeholder for a
// future Postgres-backed test that verifies the full sweep path: it would insert
// a `building` resource-provisioning task, run sweep with a ready-binding fake,
// and confirm the task reaches `deployed` + redispatch is called once. The test
// requires the deployments Postgres (docker-compose on :5433) and is not yet
// implemented; remove this Skip and add assertions when that harness is wired.
func TestResourceWatcher_Sweep_ReadyBinding_DeploysTask(t *testing.T) {
	t.Skip("placeholder: requires a live Postgres (deployments Postgres on :5433) — no assertions yet")
}

// TestResourceWatcher_Sweep_NilBinding_LeavesTask is a no-DB unit test that
// exercises the real handleTask path when GetBinding returns (nil, nil) — the
// in-flight case where the binding has not been created yet. handleTask must
// return without touching w.db or calling redispatch (the nil-binding branch
// hits classifyBinding → (false,false) → default case → leave for next tick).
func TestResourceWatcher_Sweep_NilBinding_LeavesTask(t *testing.T) {
	redispatchCalled := 0
	w := &ResourceWatcher{
		// db is nil intentionally: the not-ready branch of handleTask never
		// reaches any DB call, so a nil *gorm.DB must not cause a panic.
		db: nil,
		rc: &fakeResourceClient{binding: nil},
		redispatch: func(_ context.Context, _, _ string) (int, error) {
			redispatchCalled++
			return 1, nil
		},
		asServiceIdentity: nil,
		tick:              10 * time.Second,
		staleAfter:        10 * time.Minute,
	}

	task := models.ComponentTask{
		ID:           "test-task-nil-binding",
		OrgID:        "test-org",
		ProjectID:    "test-project",
		ResourceName: "my-db",
		Status:       string(models.TaskStatusBuilding),
		// LastEventAt is nil → stale guard is skipped.
	}

	// Call the real per-task method — must not panic even with db==nil.
	w.handleTask(context.Background(), &task)

	if redispatchCalled != 0 {
		t.Fatalf("nil binding must not trigger redispatch, got %d calls", redispatchCalled)
	}
	// Status is a local copy; the real status remains unchanged because no DB
	// write is issued on the not-ready branch.
	if task.Status != string(models.TaskStatusBuilding) {
		t.Fatalf("nil binding must leave task status unchanged; got %q", task.Status)
	}
}

// TestClassifyBinding_ReadyBinding_ReturnsDone asserts that classifyBinding
// returns (true, false) for a binding whose Ready condition is True.
// The full handleTask path (DB update + redispatch) is exercised separately
// by the Postgres-backed stub above once that harness is wired.
func TestClassifyBinding_ReadyBinding_ReturnsDone(t *testing.T) {
	done, failed := classifyBinding(readyBinding())
	if !done || failed {
		t.Fatalf("ready binding classify: want (true,false), got (%v,%v)", done, failed)
	}
}
