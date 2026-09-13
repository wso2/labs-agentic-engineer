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

// watcher.go — the pod-truth watcher for coding-agent run cycles.
//
// It polls each dispatched cycle's OpenChoreo resource tree and classifies the
// cycle from the child POD's phase. Three rules hold it together:
//
//  1. It NEVER reads the ReleaseBinding's Ready condition. OpenChoreo registers
//     no health check for `batch/v1 Job`, so a binding reports "completed
//     successfully" over a Job that is still running or has already failed.
//  2. It NEVER writes an agent log to Postgres. What it DOES do is hand each
//     dispatched cycle to the CycleRecorder, which writes the cycle's feed to
//     the workspace volume as v2 RunEvents — observability, not a ledger
//     (ADR-0027) — so a viewer reads a file instead of re-deriving the pod's
//     log per connection. Postgres is still not the log system of record. The
//     one thing this watcher takes out of the log itself is the runner's
//     terminal token-usage line.
//  3. It NEVER deletes a Component on a natural terminal. Deletion frees the
//     billing slot but also destroys the archive, so it belongs to the
//     retention pass (and to cancel), which decide with the whole picture.
//
// Its state is the cycle rows themselves — the not-found streak is the only
// in-memory fact, and losing it on restart costs at most two extra ticks.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// finalLogTailBytes caps how much of a terminal pod's log is kept AFTER the
// OpenChoreo read returns. The OC call itself is unbounded (sinceSeconds=0)
// and returns whatever the platform still holds for the pod — never a
// head-anchored k8s `limitBytes` window — so trimming to the last N bytes
// here keeps the run's ENDING (where the outcome and any failure live), not
// its opening. These bytes are scanned for the runner's usage line and then
// dropped — never written to Postgres.
const finalLogTailBytes = 256 * 1024

// cycleCaptureWindow bounds how far back a CLOSED cycle is still worth polling.
// A cycle closes on the merge webhook seconds after its agent exits — long
// before the next tick — so restricting the pass to open cycles would miss
// nearly every usage capture.
const cycleCaptureWindow = 6 * time.Hour

// Watcher cadences. The startup grace is generous because it has to cover an
// image pull on a cold node, and its expiry is a verdict.
const (
	defaultPollInterval = 30 * time.Second
	defaultStartupGrace = 10 * time.Minute
	// missingTicksToFail is B9's "sustained 404": one missing read is a race
	// with a render or a delete, three consecutive ones are a fact.
	missingTicksToFail = 3
)

// cycleWatchStore is the cycle state this watcher reads and writes. It is a
// narrow interface rather than the whole repository so the watcher's write
// surface — exactly one mutator plus usage — is visible at a glance.
type cycleWatchStore interface {
	ListRecentDispatched(ctx context.Context, since time.Time) ([]delivery.RunCycle, error)
	FinishAgentFailed(ctx context.Context, id, reason string) (*delivery.RunCycle, error)
	RecordUsage(ctx context.Context, id string, u contracts.CapturedUsage) error
}

// JobWatcher reconciles dispatched run cycles against the pods OpenChoreo
// rendered for them.
type JobWatcher struct {
	runtime openchoreo.RuntimeClient
	cycles  cycleWatchStore

	// recorder owns each dispatched cycle's feed recording. The watcher is its
	// DISCOVERY, not its clock: it hands over every cycle it sees on its own 30s
	// tick, and each recording session then paces itself (1s while its pod is
	// Running). Discovery belongs here because this is already the one pass that
	// knows which cycles are dispatched and still in the window. nil → nothing is
	// recorded and every feed reports `none`.
	recorder *CycleRecorder

	// asService lifts the tick into the service identity — the watcher has no
	// inbound request to borrow a user token from. nil in tests.
	asService func(ctx context.Context) context.Context

	pollInterval time.Duration
	startupGrace time.Duration

	// missing counts CONSECUTIVE not-found reads per cycle id. Any successful
	// read clears it, so three scattered misses never add up to a verdict.
	missing map[string]int

	// absent counts CONSECUTIVE snapshots that returned no pod at all, per cycle
	// id, and seen records that a snapshot once returned the cycle's pod. Both
	// exist because an empty snapshot is not the same fact as "no pod was ever
	// scheduled": the resource tree is read through the OpenChoreo API and the
	// cluster agent, and under load either answers 200 with nothing in it.
	// Live, that turned a running agent into `startup_failed:no_pod_scheduled`
	// twenty-four minutes into its cycle; the pull request it merged sixteen
	// minutes later then belonged to no open cycle, and the run re-dispatched
	// at its landing deadline. So an empty snapshot after the grace is a verdict
	// only when it is sustained (missingTicksToFail, the same bar as a 404) and
	// only for a pod the watcher has never seen — a pod that was seen cannot
	// retroactively have never been scheduled.
	absent map[string]int
	seen   map[string]bool

	once sync.Once
}

// NewJobWatcher wires the watcher. runtime + cycles are required; asService may
// be nil (tests).
func NewJobWatcher(runtime openchoreo.RuntimeClient, cycles cycleWatchStore, asService func(ctx context.Context) context.Context) *JobWatcher {
	if runtime == nil || cycles == nil {
		panic("codingagent.JobWatcher: runtime + cycles are required")
	}
	return &JobWatcher{
		runtime:      runtime,
		cycles:       cycles,
		asService:    asService,
		pollInterval: defaultPollInterval,
		startupGrace: defaultStartupGrace,
		missing:      map[string]int{},
		absent:       map[string]int{},
		seen:         map[string]bool{},
	}
}

// WithRecorder attaches the run-feed recorder. Optional. Returns the receiver.
func (w *JobWatcher) WithRecorder(rec *CycleRecorder) *JobWatcher {
	w.recorder = rec
	return w
}

// WithIntervals overrides the poll cadence and the startup grace. Zero values
// keep the defaults. Returns the receiver.
func (w *JobWatcher) WithIntervals(poll, startupGrace time.Duration) *JobWatcher {
	if poll > 0 {
		w.pollInterval = poll
	}
	if startupGrace > 0 {
		w.startupGrace = startupGrace
	}
	return w
}

// Run blocks until ctx is canceled, ticking immediately then on pollInterval.
func (w *JobWatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	w.once.Do(func() { slog.Info("codingagent.JobWatcher: started", "interval", w.pollInterval) })
	w.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			slog.Info("codingagent.JobWatcher: stopping")
			return
		case <-ticker.C:
			w.Tick(ctx)
		}
	}
}

// Tick runs one reconciliation pass. Exported so a test drives a single pass.
func (w *JobWatcher) Tick(ctx context.Context) {
	if w.asService != nil {
		ctx = w.asService(ctx)
	}
	rows, err := w.cycles.ListRecentDispatched(ctx, time.Now().UTC().Add(-cycleCaptureWindow))
	if err != nil {
		slog.ErrorContext(ctx, "codingagent.JobWatcher: list recent run cycles failed", "error", err)
		return
	}
	live := make(map[string]bool, len(rows))
	for i := range rows {
		cycle := &rows[i]
		if !isCodingAgentRun(cycle.JobRef) {
			continue
		}
		live[cycle.ID] = true
		// Recording is started BEFORE the classification below, because a cycle
		// this pass is about to close still has a feed worth keeping — and the
		// recorder's own terminal handling (a final full read) is what captures
		// the runner's last words.
		//
		// ctx here is the Run loop's context lifted into the service identity, not
		// a per-tick one: a session outlives the tick that started it, and one
		// started on a tick-scoped context would be cancelled a moment later and
		// record exactly one poll.
		w.recorder.Ensure(ctx, cycle)
		w.checkCycle(ctx, cycle)
	}
	// Drop streaks for cycles that have left the window, so the maps cannot grow
	// with the table. The recorder's sessions go with them: a cycle out of the
	// window is one nothing is watching any more.
	for id := range w.missing {
		if !live[id] {
			delete(w.missing, id)
		}
	}
	for id := range w.absent {
		if !live[id] {
			delete(w.absent, id)
		}
	}
	for id := range w.seen {
		if !live[id] {
			delete(w.seen, id)
		}
	}
	w.recorder.retain(live)
}

func (w *JobWatcher) checkCycle(ctx context.Context, cycle *delivery.RunCycle) {
	binding, err := w.runtime.ReleaseBindingName(ctx, cycle.OrgID, cycle.ProjectID, cycle.JobRef, openchoreo.DevEnvironmentName)
	if err != nil {
		w.noteReadFailure(ctx, cycle, err, "release binding lookup")
		return
	}
	pod, err := w.runtime.PodSnapshot(ctx, cycle.OrgID, binding)
	if err != nil {
		w.noteReadFailure(ctx, cycle, err, "resource tree")
		return
	}
	delete(w.missing, cycle.ID)
	if pod.Found {
		w.seen[cycle.ID] = true
		delete(w.absent, cycle.ID)
	}

	switch ClassifyPod(pod) {
	case OutcomeSucceeded:
		// The agent's process ended. Whether the WORK landed is the pull
		// request's answer, and it reaches the run as a webhook — so nothing is
		// concluded here beyond banking the run's token spend.
		w.captureUsage(ctx, cycle, binding, pod)
	case OutcomeFailed:
		w.captureUsage(ctx, cycle, binding, pod)
		w.failCycle(ctx, cycle, FailureReason(pod))
	case OutcomePending:
		if !pod.Found {
			// See the absent/seen fields: an empty snapshot is evidence only
			// when sustained, and never about a pod that has been seen.
			if w.seen[cycle.ID] {
				slog.WarnContext(ctx, "codingagent.JobWatcher: snapshot returned no pod for a cycle whose pod was seen (transient; no verdict)",
					"cycle", cycle.ID, "run", cycle.JobRef)
				return
			}
			w.absent[cycle.ID]++
			if w.absent[cycle.ID] < missingTicksToFail {
				return
			}
		}
		w.checkStartupGrace(ctx, cycle, binding, pod)
	case OutcomeRunning:
		// Nothing to decide; the live tail is what the console wants meanwhile.
	}
}

// noteReadFailure applies B9: a not-found read counts toward the sustained-404
// verdict, and anything else — a 5xx, a timeout, a DNS blip — does not count
// and breaks the streak.
func (w *JobWatcher) noteReadFailure(ctx context.Context, cycle *delivery.RunCycle, err error, what string) {
	if !errors.Is(err, openchoreo.ErrNotFound) {
		slog.WarnContext(ctx, "codingagent.JobWatcher: "+what+" failed (transient; no verdict)",
			"cycle", cycle.ID, "run", cycle.JobRef, "error", err)
		delete(w.missing, cycle.ID)
		return
	}
	w.missing[cycle.ID]++
	if w.missing[cycle.ID] < missingTicksToFail {
		return
	}
	w.failCycle(ctx, cycle, ReasonJobNotFound)
}

// checkStartupGrace fails a cycle whose pod never reached Running within the
// grace, naming the cause from the pod's own waiting reason or its events —
// which is the difference between "your image does not pull", "the cluster has
// no room" and "a secret had not synced yet".
func (w *JobWatcher) checkStartupGrace(ctx context.Context, cycle *delivery.RunCycle, binding string, pod openchoreo.RuntimePod) {
	// UpdatedAt is the row's last dispatch write (NoteDispatch), so the grace is
	// measured from the attempt in flight and a re-dispatch restarts it.
	if time.Since(cycle.UpdatedAt) < w.startupGrace {
		return
	}
	var events []openchoreo.RuntimeEvent
	if pod.Found {
		if evs, err := w.runtime.PodEvents(ctx, cycle.OrgID, binding, pod.Name); err == nil {
			events = evs
		} else {
			slog.WarnContext(ctx, "codingagent.JobWatcher: pod events read failed; reason will be less specific",
				"cycle", cycle.ID, "pod", pod.Name, "error", err)
		}
	}
	w.failCycle(ctx, cycle, StartupFailureReason(pod, events))
}

// failCycle records the terminal reason. The repository's own fences (open, and
// no pull request) decide whether the write lands, so this is safe to re-enter
// and safe to run in more than one replica.
func (w *JobWatcher) failCycle(ctx context.Context, cycle *delivery.RunCycle, reason string) {
	if cycle.EndedAt != nil {
		return
	}
	closed, err := w.cycles.FinishAgentFailed(ctx, cycle.ID, reason)
	if err != nil {
		slog.ErrorContext(ctx, "codingagent.JobWatcher: finish cycle failed", "cycle", cycle.ID, "reason", reason, "error", err)
		return
	}
	if closed == nil {
		return // another replica got there, or the cycle has a pull request
	}
	slog.InfoContext(ctx, "codingagent.JobWatcher: cycle agent terminal",
		"cycle", cycle.ID, "run", cycle.JobRef, "reason", reason)
}

// captureUsage banks the run's token spend from the runner's terminal NDJSON
// line. It is the ONLY reason this watcher reads a log, and the bytes are
// dropped straight after: the console reads logs from OpenChoreo and the
// observability plane, never from a table.
//
// Idempotence is DB-driven: a cycle that already carries a model id has been
// captured, so a restart re-reads nothing.
func (w *JobWatcher) captureUsage(ctx context.Context, cycle *delivery.RunCycle, binding string, pod openchoreo.RuntimePod) {
	if cycle.ModelID != "" || !pod.Found {
		return
	}
	lines, err := w.runtime.PodLogs(ctx, cycle.OrgID, binding, pod.Name, 0)
	if err != nil {
		slog.WarnContext(ctx, "codingagent.JobWatcher: terminal log read failed; usage not captured",
			"cycle", cycle.ID, "pod", pod.Name, "error", err)
		return
	}
	u := usageFromLog(joinTail(lines, finalLogTailBytes))
	if u == nil {
		return
	}
	if err := w.cycles.RecordUsage(ctx, cycle.ID, *u); err != nil {
		slog.WarnContext(ctx, "codingagent.JobWatcher: record cycle usage failed", "cycle", cycle.ID, "error", err)
	}
}

// joinTail renders log lines as text, keeping at most maxBytes from the END —
// the usage line is the runner's last word, so the tail is the half that
// matters.
func joinTail(lines []openchoreo.PodLogLine, maxBytes int) string {
	var b strings.Builder
	for i := range lines {
		b.WriteString(lines[i].Log)
		b.WriteByte('\n')
	}
	text := b.String()
	if len(text) <= maxBytes {
		return text
	}
	return text[len(text)-maxBytes:]
}
