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

// Same-package tests for the VERSION stream's termination rule, which the
// component tier cannot show because it only ever drives a finite stream: the
// stream holds while ANY run on the milestone is live, and settles only when none
// is. Positive assertions get a 1s deadline, negative ones a 50ms window —
// matching progress_settle_test.go.
package runread

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// milestoneRuns is a milestone's run rows, mutable under a lock — runs settling,
// runs being admitted, and the whole set being purged by a project delete, all
// seen through the reader.
type milestoneRuns struct {
	mu   sync.Mutex
	rows []delivery.MilestoneRun
	err  error
}

func (m *milestoneRuns) setState(id, state string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rows {
		if m.rows[i].ID == id {
			m.rows[i].State = state
		}
	}
}

// purge is the project-delete cascade seen through the reader: every run row for
// the milestone is gone.
func (m *milestoneRuns) purge() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows = nil
}

// fail makes the read error — a transient outage, which says nothing about
// whether any run is live.
func (m *milestoneRuns) fail(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *milestoneRuns) MilestoneNumberForTag(_ context.Context, _, _, tag string) (int, bool, error) {
	if tag != "v3" {
		return 0, false, nil
	}
	return 7, true, nil
}

func (m *milestoneRuns) ListByMilestone(context.Context, string, string, int) ([]delivery.MilestoneRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	// Newest first, exactly as the repository answers — the stream owes the
	// chronological order itself.
	out := make([]delivery.MilestoneRun, 0, len(m.rows))
	for i := len(m.rows) - 1; i >= 0; i-- {
		out = append(out, m.rows[i])
	}
	return out, nil
}

func (m *milestoneRuns) GetByIDScoped(context.Context, string, string) (*delivery.MilestoneRun, error) {
	return nil, nil
}

// run is one row of a milestone's history, created `minutes` after the epoch so
// the chronological sort has something to sort on.
func run(id, kind, state string, minutes int) delivery.MilestoneRun {
	return delivery.MilestoneRun{
		ID: id, OrgID: "acme", ProjectID: "widgets",
		MilestoneNumber: 7, MilestoneTitle: "v3", Tag: "v3",
		Kind: kind, State: state,
		CreatedAt: time.Date(2026, 8, 1, 9, minutes, 0, 0, time.UTC),
	}
}

// startVersionStream drives the version loop in a goroutine against a fast tick
// and returns the buffer plus a stop func.
func startVersionStream(t *testing.T, runs *milestoneRuns) (*syncBuffer, context.CancelFunc) {
	t.Helper()
	svc := NewProgressService(runs, noCycles{}, nil)
	svc.tick = 5 * time.Millisecond
	svc.keepAlive = time.Hour // out of the way

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := svc.OpenBuildProgressStream(ctx, "acme", "widgets", "v3")
	if err != nil {
		cancel()
		t.Fatalf("open: %v", err)
	}
	buf := &syncBuffer{}
	go stream(buf, func() {})
	return buf, cancel
}

// TestBuildProgress_HoldsWhileAnyRunIsLive: the newest run being terminal is NOT
// the version being finished, and neither is any single run's state. A settled
// dev run beside a live validation run must hold the stream open. Negative
// assertion, 50ms window.
func TestBuildProgress_HoldsWhileAnyRunIsLive(t *testing.T) {
	runs := &milestoneRuns{rows: []delivery.MilestoneRun{
		run("dev-1", delivery.RunKindDev, delivery.RunStateSucceeded, 0),
		run("val-1", delivery.RunKindValidation, delivery.RunStateRunning, 30),
	}}
	buf, cancel := startVersionStream(t, runs)
	defer cancel()

	time.Sleep(50 * time.Millisecond)
	if got := buf.String(); strings.Contains(got, "[DONE]") {
		t.Fatalf("a live run on the milestone must hold the stream open\n---\n%s", got)
	}
}

// TestBuildProgress_SettlesWhenNoRunIsLive: the last live run settling is what
// closes the stream, and the `done` frame says only that — `reason`, never a run
// state, because a later run may still be admitted on this milestone.
func TestBuildProgress_SettlesWhenNoRunIsLive(t *testing.T) {
	runs := &milestoneRuns{rows: []delivery.MilestoneRun{
		run("dev-1", delivery.RunKindDev, delivery.RunStateSucceeded, 0),
		run("val-1", delivery.RunKindValidation, delivery.RunStateWaiting, 30),
	}}
	buf, cancel := startVersionStream(t, runs)
	defer cancel()

	time.Sleep(20 * time.Millisecond)
	if strings.Contains(buf.String(), "[DONE]") {
		t.Fatal("a waiting run must not settle the version stream")
	}
	runs.setState("val-1", delivery.RunStateFailed)

	waitFor(t, buf, `"type":"done"`, time.Second)
	waitFor(t, buf, "data: [DONE]", time.Second)
	got := buf.String()
	if !strings.Contains(got, `"reason":"no_live_run"`) {
		t.Errorf("the done frame must say why it ended\n---\n%s", got)
	}
	// The run stream's field would publish one run's outcome as the version's.
	if strings.Contains(got, `"state":`) {
		t.Errorf("the version stream must not report a run state\n---\n%s", got)
	}
}

// TestBuildProgress_SettlesWhenTheRunsArePurged: a project delete purges the
// milestone's runs underneath a live stream. No row is live, so the ordinary rule
// already covers it — there is no second ending to get wrong, which is the one
// place this stream is simpler than the per-run one.
func TestBuildProgress_SettlesWhenTheRunsArePurged(t *testing.T) {
	runs := &milestoneRuns{rows: []delivery.MilestoneRun{
		run("dev-1", delivery.RunKindDev, delivery.RunStateRunning, 0),
	}}
	buf, cancel := startVersionStream(t, runs)
	defer cancel()

	time.Sleep(20 * time.Millisecond)
	if strings.Contains(buf.String(), "[DONE]") {
		t.Fatal("a running run must not settle the version stream")
	}
	runs.purge()

	waitFor(t, buf, `"reason":"no_live_run"`, time.Second)
	waitFor(t, buf, "data: [DONE]", time.Second)
}

// TestBuildProgress_ReadFailure_DoesNotSettle: the counterweight. A read that
// FAILED says nothing about whether a run is live, so it keeps the stream and
// retries — settling on it would end every live version's stream on one database
// blip, and the console would redraw a moving version as a finished one. Negative
// assertion, 50ms window.
func TestBuildProgress_ReadFailure_DoesNotSettle(t *testing.T) {
	runs := &milestoneRuns{rows: []delivery.MilestoneRun{
		run("dev-1", delivery.RunKindDev, delivery.RunStateRunning, 0),
	}}
	buf, cancel := startVersionStream(t, runs)
	defer cancel()

	runs.fail(errors.New("connection reset"))
	time.Sleep(50 * time.Millisecond)
	if got := buf.String(); strings.Contains(got, "[DONE]") {
		t.Fatalf("a failed read must not settle the stream\n---\n%s", got)
	}
}

// TestBuildProgress_UnknownTag_Refused: the fence is the tag resolution, and it
// runs before the connection exists — so "no such version" is an error the
// handler can turn into a JSON 404, never an empty stream a caller could read as
// "this version did nothing".
func TestBuildProgress_UnknownTag_Refused(t *testing.T) {
	runs := &milestoneRuns{rows: []delivery.MilestoneRun{
		run("dev-1", delivery.RunKindDev, delivery.RunStateSucceeded, 0),
	}}
	svc := NewProgressService(runs, noCycles{}, nil)
	if _, err := svc.OpenBuildProgressStream(context.Background(), "acme", "widgets", "v99"); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("unknown tag = %v, want ErrTagNotFound", err)
	}
}

// TestChronological_OrdersOldestFirstAndIsStable pins the narrative's direction
// and the stability of the run index the console prints. The repository answers
// newest-first, so a reversal that relied on that promise would silently
// re-order every section if the read order ever changed.
func TestChronological_OrdersOldestFirstAndIsStable(t *testing.T) {
	same := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	rows := []delivery.MilestoneRun{
		{ID: "c", CreatedAt: same},
		{ID: "b", CreatedAt: same},
		{ID: "newest", CreatedAt: same.Add(time.Hour)},
		{ID: "oldest", CreatedAt: same.Add(-time.Hour)},
	}
	chronological(rows)
	var ids []string
	for i := range rows {
		ids = append(ids, rows[i].ID)
	}
	want := []string{"oldest", "b", "c", "newest"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", ids, want)
	}
}
