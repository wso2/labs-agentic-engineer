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

package runread_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
)

// fakeReaper records the reap requests and can be scripted to fail.
type fakeReaper struct {
	calls [][3]string
	err   error
	order *[]string
}

func (f *fakeReaper) ReapRunCycle(_ context.Context, orgID, projectID, runID string) error {
	if f.order != nil {
		*f.order = append(*f.order, "reap")
	}
	f.calls = append(f.calls, [3]string{orgID, projectID, runID})
	return f.err
}

// fakeRecorder is the durable half of cancel: it records that the request was
// written, and in what ORDER relative to the signal and the reap.
type fakeRecorder struct {
	ids   []string
	err   error
	order *[]string
}

func (f *fakeRecorder) RequestCancel(_ context.Context, runID string) (*delivery.MilestoneRun, error) {
	if f.order != nil {
		*f.order = append(*f.order, "record")
	}
	if f.err != nil {
		return nil, f.err
	}
	f.ids = append(f.ids, runID)
	return &delivery.MilestoneRun{ID: runID}, nil
}

func cancelCommands(canceller runread.RunCanceller, reaper runread.CycleReaper) *runread.Commands {
	return cancelCommandsWith(&fakeRecorder{}, canceller, reaper)
}

func cancelCommandsWith(rec runread.CancelRequester, canceller runread.RunCanceller, reaper runread.CycleReaper) *runread.Commands {
	runs := fakeRuns{org: "acme", rows: []delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)}}
	return runread.NewCommands(runs, rec, canceller, nil).WithCycleReaper(reaper)
}

// TestCancel_ReapsTheCyclesComponentAfterSignalling is the cancel contract this
// phase completes: the signal settles the RUN, the reap stops the POD and frees
// the org's billing slot. Signal first — if it failed, the run would keep
// waiting for an agent we had already killed.
func TestCancel_ReapsTheCyclesComponentAfterSignalling(t *testing.T) {
	canceller := &fakeCanceller{}
	reaper := &fakeReaper{}
	c := cancelCommands(canceller, reaper)

	if err := c.Cancel(context.Background(), "acme", "widgets", "r1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if canceller.cancels != 1 {
		t.Fatalf("supervisor signalled %d times, want 1", canceller.cancels)
	}
	if len(reaper.calls) != 1 || reaper.calls[0] != [3]string{"acme", "widgets", "r1"} {
		t.Errorf("reap calls = %v, want one for acme/widgets/r1", reaper.calls)
	}
}

// TestCancel_NeverReapsWhenTheSignalFailed: with the engine down nothing was
// cancelled and the caller retries, so killing the pod would abandon a run that
// is still going to be resumed.
func TestCancel_NeverReapsWhenTheSignalFailed(t *testing.T) {
	canceller := &fakeCanceller{err: delivery.ErrTemporalUnavailable}
	reaper := &fakeReaper{}
	c := cancelCommands(canceller, reaper)

	if err := c.Cancel(context.Background(), "acme", "widgets", "r1"); !errors.Is(err, delivery.ErrTemporalUnavailable) {
		t.Fatalf("Cancel = %v, want the engine-down sentinel", err)
	}
	if len(reaper.calls) != 0 {
		t.Errorf("reaped %v after a failed signal", reaper.calls)
	}
}

// TestCancel_ReapFailureStillCancels: the user asked to stop the run, and the
// run IS stopping. A leaked component is a slot to sweep, not a reason to
// answer "cancel failed" and invite a retry that changes nothing.
func TestCancel_ReapFailureStillCancels(t *testing.T) {
	canceller := &fakeCanceller{}
	reaper := &fakeReaper{err: errors.New("oc down")}
	c := cancelCommands(canceller, reaper)

	if err := c.Cancel(context.Background(), "acme", "widgets", "r1"); err != nil {
		t.Fatalf("Cancel must succeed even when the reap fails: %v", err)
	}
	if len(reaper.calls) != 1 {
		t.Errorf("reap attempts = %d, want 1", len(reaper.calls))
	}
}

// TestCancel_WithoutAReaperStillCancels keeps the collaborator optional: a boot
// without the OC client cancels exactly as it did before this phase.
func TestCancel_WithoutAReaperStillCancels(t *testing.T) {
	canceller := &fakeCanceller{}
	runs := fakeRuns{org: "acme", rows: []delivery.MilestoneRun{specRun("r1", delivery.RunStateRunning)}}
	if err := runread.NewCommands(runs, &fakeRecorder{}, canceller, nil).Cancel(context.Background(), "acme", "widgets", "r1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if canceller.cancels != 1 {
		t.Errorf("supervisor signalled %d times, want 1", canceller.cancels)
	}
}

// TestCancel_RecordsTheRequestBeforeSignalling is the ordering the whole fix
// rests on. Signal delivery is best-effort — the supervisor swallows a failed
// SignalWorkflow so a dead engine cannot wedge the console — so a cancel that
// signalled first and crashed would have killed an agent with nothing durable
// saying why. Record, signal, reap; in that order, always.
func TestCancel_RecordsTheRequestBeforeSignalling(t *testing.T) {
	var order []string
	rec := &fakeRecorder{order: &order}
	canceller := &fakeCanceller{order: &order}
	reaper := &fakeReaper{order: &order}

	if err := cancelCommandsWith(rec, canceller, reaper).Cancel(context.Background(), "acme", "widgets", "r1"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	want := []string{"record", "signal", "reap"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	if len(rec.ids) != 1 || rec.ids[0] != "r1" {
		t.Errorf("recorded %v, want one request for r1", rec.ids)
	}
}

// TestCancel_NeverSignalsWhenTheRequestCouldNotBeRecorded: without the durable
// stamp the loop has no way to re-derive the cancel, so a signal that raced a
// dead database would be the only record of it — exactly the fragility the
// stamp exists to remove. Nothing is signalled and nothing is reaped.
func TestCancel_NeverSignalsWhenTheRequestCouldNotBeRecorded(t *testing.T) {
	rec := &fakeRecorder{err: errors.New("database down")}
	canceller := &fakeCanceller{}
	reaper := &fakeReaper{}

	if err := cancelCommandsWith(rec, canceller, reaper).Cancel(context.Background(), "acme", "widgets", "r1"); err == nil {
		t.Fatal("Cancel must fail when the request could not be recorded")
	}
	if canceller.cancels != 0 {
		t.Errorf("signalled %d times after a failed record, want 0", canceller.cancels)
	}
	if len(reaper.calls) != 0 {
		t.Errorf("reaped %v after a failed record", reaper.calls)
	}
}
