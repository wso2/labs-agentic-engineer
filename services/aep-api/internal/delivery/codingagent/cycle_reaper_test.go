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
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// cancelCycles is the reaper's cycle store: one latest row, and what the
// cancel wrote onto it. order, when set, records each write so a test can pin
// the close-before-suspend ordering against the jobs fake.
type cancelCycles struct {
	latest    *delivery.RunCycle
	latestErr error
	finishErr error
	marked    bool
	cancelled bool
	order     *[]string
}

func (c *cancelCycles) Latest(context.Context, string, string) (*delivery.RunCycle, error) {
	return c.latest, c.latestErr
}

func (c *cancelCycles) MarkJobSuspended(context.Context, string) error {
	c.marked = true
	c.note("mark")
	return nil
}

func (c *cancelCycles) FinishCancelled(_ context.Context, id string) (*delivery.RunCycle, error) {
	if c.finishErr != nil {
		return nil, c.finishErr
	}
	c.cancelled = true
	c.note("finish-cancelled")
	return &delivery.RunCycle{ID: id, AgentReason: delivery.CycleReasonCancelled}, nil
}

func (c *cancelCycles) note(step string) {
	if c.order != nil {
		*c.order = append(*c.order, step)
	}
}

func TestReap_SuspendsMarksAndClosesButDeletesNothing(t *testing.T) {
	jobs := &fakeJobs{}
	store := &cancelCycles{latest: &delivery.RunCycle{ID: "c1", OrgID: "acme", ProjectID: "shop", JobRef: "ca-c1-x", Environment: "development"}}
	r := NewCycleReaper(jobs, store, testWriteTargets())
	if err := r.ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jobs.suspends, []string{"ca-c1-x@development"}) || !store.marked || !store.cancelled {
		t.Fatalf("suspends %v marked %v cancelled %v", jobs.suspends, store.marked, store.cancelled)
	}
}

func TestReap_ComponentAlreadyGoneIsNotAnError(t *testing.T) {
	jobs := &fakeJobs{err: fmt.Errorf("x: %w", openchoreo.ErrNotFound)}
	store := &cancelCycles{latest: &delivery.RunCycle{ID: "c1", JobRef: "ca-c1-x", Environment: "development"}}
	if err := NewCycleReaper(jobs, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
		t.Fatal(err)
	}
	if !store.cancelled {
		t.Fatal("the cycle still closes as cancelled")
	}
}

func TestReap_NonAgentRefIsANoOp(t *testing.T) {
	for name, row := range map[string]*delivery.RunCycle{
		"never dispatched": nil,
		"no job ref":       {ID: "c1"},
		"build run ref":    {ID: "c1", JobRef: "wf-build-1"},
	} {
		t.Run(name, func(t *testing.T) {
			jobs := &fakeJobs{}
			store := &cancelCycles{latest: row}
			if err := NewCycleReaper(jobs, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
				t.Fatal(err)
			}
			if len(jobs.suspends) != 0 || store.marked || store.cancelled {
				t.Fatalf("suspends %v marked %v cancelled %v, want nothing", jobs.suspends, store.marked, store.cancelled)
			}
		})
	}
}

// The close is the fence a racing re-dispatch reads: NoteDispatch moves only
// an OPEN row, and only a moved row is ever un-suspended. Closing after the
// suspend would leave a window in which a dispatch notes the still-open row,
// clears the stamp and resumes the binding the cancel just suspended.
func TestReap_ClosesTheCycleBeforeSuspending(t *testing.T) {
	var order []string
	jobs := &fakeJobs{before: func() { order = append(order, "suspend") }}
	store := &cancelCycles{order: &order, latest: &delivery.RunCycle{ID: "c1", JobRef: "ca-c1-x", Environment: "development"}}
	if err := NewCycleReaper(jobs, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"finish-cancelled", "suspend", "mark"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

// A cycle that some other path already closed (the loop's own close after the
// signal, a merge webhook) is still suspended: the user asked for the pod to
// stop, whoever wrote ended_at first.
func TestReap_AlreadyClosedCycleIsStillSuspended(t *testing.T) {
	jobs := &fakeJobs{}
	store := &alreadyClosedCycles{cancelCycles{latest: &delivery.RunCycle{ID: "c1", JobRef: "ca-c1-x", Environment: "development"}}}
	if err := NewCycleReaper(jobs, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
		t.Fatal(err)
	}
	if len(jobs.suspends) != 1 || !store.marked {
		t.Fatalf("suspends %v marked %v, want one suspend, marked", jobs.suspends, store.marked)
	}
}

// alreadyClosedCycles answers FinishCancelled as the fenced write does for a
// closed row: (nil, nil), nothing changed.
type alreadyClosedCycles struct{ cancelCycles }

func (c *alreadyClosedCycles) FinishCancelled(context.Context, string) (*delivery.RunCycle, error) {
	return nil, nil
}

// Q-5: a legacy release renders no suspend. The cycle still closes as
// cancelled, but it is NOT marked — the settler reads the missing stamp as
// "suspend did not apply" — and nothing announces a suspend.
func TestReap_LegacyReleaseClosesButDoesNotMark(t *testing.T) {
	logs := captureLogs(t)
	jobs := &fakeJobs{err: fmt.Errorf("suspend: %w", openchoreo.ErrSuspendUnsupported)}
	store := &cancelCycles{latest: &delivery.RunCycle{ID: "c1", JobRef: "ca-c1-x", Environment: "development"}}
	if err := NewCycleReaper(jobs, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
		t.Fatal(err)
	}
	if !store.cancelled || store.marked {
		t.Fatalf("cancelled %v marked %v, want closed and unmarked", store.cancelled, store.marked)
	}
	if got := logsNamed(*logs, "codingagent.job_suspended"); len(got) != 0 {
		t.Fatalf("a suspend that did not apply was announced: %+v", got)
	}
}

// The suspend event is value-free and names its cause, logged only when the
// suspend took effect.
func TestReap_LogsTheSuspendWithCauseCancel(t *testing.T) {
	logs := captureLogs(t)
	store := &cancelCycles{latest: &delivery.RunCycle{ID: "c1", JobRef: "ca-c1-x", Environment: "development"}}
	if err := NewCycleReaper(&fakeJobs{}, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
		t.Fatal(err)
	}
	got := logsNamed(*logs, "codingagent.job_suspended")
	want := map[string]string{"cycle": "c1", "component": "ca-c1-x", "cause": "cancel"}
	if len(got) != 1 || !reflect.DeepEqual(got[0].attrs, want) {
		t.Fatalf("job_suspended logs = %+v, want one with %v", got, want)
	}
}

// A gone binding is marked (there is nothing left to suspend) but not
// announced.
func TestReap_GoneBindingIsMarkedNotAnnounced(t *testing.T) {
	logs := captureLogs(t)
	jobs := &fakeJobs{err: fmt.Errorf("x: %w", openchoreo.ErrNotFound)}
	store := &cancelCycles{latest: &delivery.RunCycle{ID: "c1", JobRef: "ca-c1-x", Environment: "development"}}
	if err := NewCycleReaper(jobs, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
		t.Fatal(err)
	}
	if !store.marked || len(logsNamed(*logs, "codingagent.job_suspended")) != 0 {
		t.Fatalf("marked %v logs %+v", store.marked, *logs)
	}
}

// Any other suspend failure is the caller's to log. The cycle is already
// closed as cancelled (the settler's backstop suspends it later) and nothing
// claims the suspend.
func TestReap_SuspendFailureIsReportedAndLeavesTheCycleUnmarked(t *testing.T) {
	boom := errors.New("oc down")
	jobs := &fakeJobs{err: boom}
	store := &cancelCycles{latest: &delivery.RunCycle{ID: "c1", JobRef: "ca-c1-x", Environment: "development"}}
	err := NewCycleReaper(jobs, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1")
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the suspend failure", err)
	}
	if !store.cancelled || store.marked {
		t.Fatalf("cancelled %v marked %v", store.cancelled, store.marked)
	}
}

// A failed close suspends nothing: without the fence a racing re-dispatch
// could still resume what the cancel suspended.
func TestReap_CloseFailureSuspendsNothing(t *testing.T) {
	boom := errors.New("db down")
	jobs := &fakeJobs{}
	store := &cancelCycles{finishErr: boom, latest: &delivery.RunCycle{ID: "c1", JobRef: "ca-c1-x", Environment: "development"}}
	if err := NewCycleReaper(jobs, store, testWriteTargets()).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the close failure", err)
	}
	if len(jobs.suspends) != 0 {
		t.Fatalf("suspended %v without the fence", jobs.suspends)
	}
}

// A cycle with no recorded environment suspends in the project's write target.
func TestReap_FallsBackToTheWriteTarget(t *testing.T) {
	jobs := &fakeJobs{}
	store := &cancelCycles{latest: &delivery.RunCycle{ID: "c1", OrgID: "acme", ProjectID: "shop", JobRef: "ca-c1-x"}}
	if err := NewCycleReaper(jobs, store, &fakeWriteTargets{env: "staging"}).ReapRunCycle(context.Background(), "acme", "shop", "run-1"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(jobs.suspends, []string{"ca-c1-x@staging"}) {
		t.Fatalf("suspends = %v", jobs.suspends)
	}
}
