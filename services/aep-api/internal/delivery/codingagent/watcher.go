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
//  2. It NEVER writes an agent log anywhere. The log is the pod's while the
//     pod exists and the observability plane's after (ADR-0027); the feed
//     reads it from there (cycle_feed.go). The one thing this watcher takes
//     out of the log itself is the runner's terminal line: its token usage,
//     and whether its model provider's limit is what stopped it.
//  3. It never deletes a Component; it suspends the Job at the first terminal
//     pod, and the settler deletes once no pod is left. The suspend comes after
//     the run's usage is captured, so the pod whose log carries the spend is
//     read before anything is done to its Job. A cycle whose pod never started
//     is suspended when the watcher closes it startup_failed (and on any later
//     tick that finds it closed so, unsuspended, with a live pod): Kubernetes
//     would otherwise start that pod once the cluster had room, an agent
//     working for a closed cycle.
//
// While an open cycle's pod is stuck before Running, the watcher records why
// on the row (NoteStartupWait), so the console can say what the agent is
// waiting for before the grace runs out.
//
// Its state is the cycle rows themselves — the not-found streak is the only
// in-memory fact, and losing it on restart costs at most two extra ticks.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
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
// its opening. These bytes are scanned for the runner's terminal line and then
// dropped — never written to Postgres.
const finalLogTailBytes = 256 * 1024

// cycleCaptureWindow bounds how far back a CLOSED cycle is still worth polling.
// A cycle closes on the merge webhook seconds after its agent exits — long
// before the next tick — so restricting the pass to open cycles would miss
// nearly every usage capture.
const cycleCaptureWindow = 6 * time.Hour

// Watcher cadences. The startup grace (delivery.CycleStartupGrace) is the
// platform's, not the watcher's: the run view derives the waiting cycle's
// deadline from the same constant.
const (
	defaultPollInterval = 30 * time.Second
	// missingTicksToFail is B9's "sustained 404": one missing read is a race
	// with a render or a delete, three consecutive ones are a fact.
	missingTicksToFail = 3
	// podClockSkew is how far a pod's creation time (the cluster's clock) may
	// sit before the cycle's dispatch stamp (aep-api's clock) and still be the
	// current attempt's pod. Beyond it, on a re-dispatched cycle, the pod is the
	// previous attempt's leftover on the reused binding.
	podClockSkew = 30 * time.Second
)

// cycleWatchStore is the cycle state this watcher reads and writes. It is a
// narrow interface rather than the whole repository so the watcher's write
// surface — one verdict, usage, the suspend stamp and the startup wait — is
// visible at a glance.
type cycleWatchStore interface {
	ListRecentDispatched(ctx context.Context, since time.Time) ([]delivery.RunCycle, error)
	FinishAgentFailed(ctx context.Context, id, reason string) (*delivery.RunCycle, error)
	RecordUsage(ctx context.Context, id string, u contracts.CapturedUsage) error
	MarkJobSuspended(ctx context.Context, id string) (stamped bool, err error)
	NoteStartupWait(ctx context.Context, id, reason string, at time.Time) error
	ClearStartupWait(ctx context.Context, id string) error
}

// JobWatcher reconciles dispatched run cycles against the pods OpenChoreo
// rendered for them.
type JobWatcher struct {
	runtime openchoreo.RuntimeClient
	cycles  cycleWatchStore
	// targets is the fallback for a cycle with no recorded environment: the
	// project's write target, resolved on the tick that needs it.
	targets writeTargetResolver

	// jobs suspends a cycle's Job once its pod is terminal, so the Job
	// OpenChoreo re-creates after its TTL never runs the runner again. nil →
	// nothing is suspended (tests).
	jobs jobSuspender

	// deaths wakes the run supervisor when a cycle's agent ended without a pull
	// request. It belongs here because this is the one pass that learns a pod
	// died.
	deaths AgentDeathNotifier

	// failures records the run's failure record for the one fault this pass
	// learns first: a provider limit, whose host and reset time only the
	// runner's settle carries. nil → the run still settles blocked, and the
	// console says so without naming either.
	failures RunFailureRecorder

	// asService lifts the tick into the service identity — the watcher has no
	// inbound request to borrow a user token from. nil in tests.
	asService func(ctx context.Context) context.Context

	pollInterval time.Duration
	startupGrace time.Duration

	// missing counts CONSECUTIVE not-found reads per cycle id. Any successful
	// read clears it, so three scattered misses never add up to a verdict.
	missing map[string]int

	// absent counts CONSECUTIVE snapshots that returned no pod at all, per cycle
	// ATTEMPT (attemptKey), and seen records that a snapshot once returned that
	// attempt's pod — a pod seen on attempt 1 says nothing about attempt 2. Both
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

// NewJobWatcher wires the watcher. runtime, cycles and targets are required;
// jobs and asService may be nil (tests).
func NewJobWatcher(runtime openchoreo.RuntimeClient, cycles cycleWatchStore, targets writeTargetResolver,
	jobs jobSuspender, asService func(ctx context.Context) context.Context) *JobWatcher {
	if runtime == nil || cycles == nil || targets == nil {
		panic("codingagent.JobWatcher: runtime, cycles and targets are required")
	}
	return &JobWatcher{
		runtime:      runtime,
		cycles:       cycles,
		targets:      targets,
		jobs:         jobs,
		asService:    asService,
		pollInterval: defaultPollInterval,
		startupGrace: delivery.CycleStartupGrace,
		missing:      map[string]int{},
		absent:       map[string]int{},
		seen:         map[string]bool{},
	}
}

// WithAgentDeathNotifier attaches the run wake-up. Optional. Returns the
// receiver.
func (w *JobWatcher) WithAgentDeathNotifier(n AgentDeathNotifier) *JobWatcher {
	w.deaths = n
	return w
}

// WithRunFailures attaches the run failure recorder. Optional. Returns the
// receiver.
func (w *JobWatcher) WithRunFailures(r RunFailureRecorder) *JobWatcher {
	w.failures = r
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
	live := make(map[string]bool, 2*len(rows))
	for i := range rows {
		cycle := &rows[i]
		// A deleted Component (the settler's) has no binding left to read.
		if !isCodingAgentRun(cycle.JobRef) || cycle.ComponentDeletedAt != nil {
			continue
		}
		live[cycle.ID] = true
		live[attemptKey(cycle)] = true
		// Everything below reads the cycle in the environment its Job was bound
		// into. The row copy carries it for this pass; nothing writes it back. A
		// cycle whose fallback cannot be resolved is left alone this tick: that
		// is no evidence about its Job.
		env, err := cycleEnvironment(ctx, w.targets, cycle)
		if err != nil {
			slog.WarnContext(ctx, "codingagent.JobWatcher: no environment to read the cycle in (no verdict)",
				"cycle", cycle.ID, "run", cycle.JobRef, "error", err)
			continue
		}
		cycle.Environment = env
		w.checkCycle(ctx, cycle)
	}
	// Drop streaks for cycles that have left the window, so the maps cannot grow
	// with the table.
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
}

func (w *JobWatcher) checkCycle(ctx context.Context, cycle *delivery.RunCycle) {
	binding, err := w.runtime.ReleaseBindingName(ctx, cycle.OrgID, cycle.ProjectID, cycle.JobRef, cycle.Environment)
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
	if isLeftoverPod(cycle, pod) {
		// The previous attempt's pod on the reused binding: not this attempt's
		// terminal pod (no suspend, no usage), and not its pod for the startup
		// grace either. This attempt has no pod yet.
		pod = openchoreo.RuntimePod{}
	}
	attempt := attemptKey(cycle)
	if pod.Found {
		w.seen[attempt] = true
		delete(w.absent, attempt)
	} else if cycle.JobSuspendedAt != nil || cycle.EndedAt != nil {
		// A suspended Job runs no pod, and a closed cycle needs none: "no pod"
		// is the expected state here, never an absent or startup verdict.
		return
	}

	outcome := ClassifyPod(pod)
	if cycle.EndedAt != nil && delivery.IsStartupFailure(cycle.AgentReason) &&
		(outcome == OutcomePending || outcome == OutcomeRunning) {
		// Closed startup_failed, and its pod is still there — not yet started,
		// or started since: a zombie working for a closed cycle. The close that
		// should have suspended it lost to another replica, or a restart fell
		// between the two writes, or the row predates the rule. suspendJob is a
		// no-op once job_suspended_at is stamped. A terminal pod takes the
		// branches below, which suspend it as any terminal pod.
		w.suspendJob(ctx, cycle, causeStartupFailed)
		return
	}
	if cycle.EndedAt == nil {
		w.noteStartupWait(ctx, cycle, pod)
	}

	switch outcome {
	case OutcomeSucceeded:
		// The agent's process ended. Whether the WORK landed is the pull
		// request's answer, and it reaches the run as a webhook — so nothing is
		// concluded here beyond banking the run's token spend.
		w.captureUsage(ctx, cycle, w.readTerminal(ctx, cycle, binding, pod, false))
		w.suspendJob(ctx, cycle, causeTerminal)
	case OutcomeFailed:
		// An open cycle still needs its verdict, and the runner's last line is
		// where a provider limit says it was one.
		report := w.readTerminal(ctx, cycle, binding, pod, cycle.EndedAt == nil)
		w.captureUsage(ctx, cycle, report)
		w.suspendJob(ctx, cycle, causeTerminal)
		if report.providerLimit != nil {
			w.failOnProviderLimit(ctx, cycle, *report.providerLimit)
			return
		}
		w.failCycle(ctx, cycle, FailureReason(pod))
	case OutcomePending:
		if !pod.Found {
			// See the absent/seen fields: an empty snapshot is evidence only
			// when sustained, and never about a pod that has been seen.
			if w.seen[attempt] {
				slog.WarnContext(ctx, "codingagent.JobWatcher: snapshot returned no pod for a cycle whose pod was seen (transient; no verdict)",
					"cycle", cycle.ID, "run", cycle.JobRef)
				return
			}
			w.absent[attempt]++
			if w.absent[attempt] < missingTicksToFail {
				return
			}
		}
		w.checkStartupGrace(ctx, cycle, binding, pod)
	case OutcomeRunning:
		// Nothing to decide; the live tail is what the console wants meanwhile.
	}
}

// attemptKey names one dispatch attempt of a cycle: the in-memory pod facts
// (absent, seen) are per attempt, because a re-dispatch reuses the binding.
func attemptKey(cycle *delivery.RunCycle) string {
	return cycle.ID + "#" + strconv.Itoa(cycle.Attempts)
}

// isLeftoverPod reports whether the snapshot's pod is the previous attempt's
// finished pod on the reused binding (a re-dispatch reuses the cycle's
// Component, and attempt 1's pod stays in the tree until its Job's TTL).
//
// Only a TERMINAL pod can be a leftover, and only one that both started and
// ended before this attempt was dispatched (less podClockSkew). A Running or
// Pending pod from before the dispatch is the Job still in flight — the same
// Job cannot start a second pod — so it is watched as this attempt's: present
// for the grace, captured and suspended at its terminal. A finish time the
// tree did not carry counts as before the dispatch.
//
// Only from attempt 2: a first dispatch stamps its time after the launch, so
// its own pod may be older.
func isLeftoverPod(cycle *delivery.RunCycle, pod openchoreo.RuntimePod) bool {
	if !pod.Found || cycle.Attempts <= 1 || cycle.DispatchedAt == nil || pod.CreatedAt.IsZero() {
		return false
	}
	if outcome := ClassifyPod(pod); outcome != OutcomeSucceeded && outcome != OutcomeFailed {
		return false
	}
	cutoff := cycle.DispatchedAt.Add(-podClockSkew)
	return pod.CreatedAt.Before(cutoff) && (pod.FinishedAt.IsZero() || pod.FinishedAt.Before(cutoff))
}

// The causes a watcher suspend is announced with (codingagent.job_suspended
// `cause`): the first terminal pod, or a cycle closed startup_failed.
const (
	causeTerminal      = "terminal"
	causeStartupFailed = "startup_failed"
)

// suspendJob suspends the cycle's Job binding once: the first time its pod is
// seen terminal, or when the cycle is closed startup_failed. Idempotent through
// job_suspended_at: a marked cycle is never asked again. Outcomes:
//   - success: marked, then codingagent.job_suspended is logged — the one event
//     that says a suspend took effect;
//   - ErrNotFound: the binding is gone, so there is nothing left to suspend;
//     marked so it is not re-asked, not announced;
//   - ErrSuspendUnsupported: a legacy release with no suspend schema; the Job is
//     left to its TTL and NOT marked, which is how the settler knows suspend
//     did not apply;
//   - anything else: not marked, so the next tick retries.
func (w *JobWatcher) suspendJob(ctx context.Context, cycle *delivery.RunCycle, cause string) {
	if w.jobs == nil || cycle.JobSuspendedAt != nil {
		return
	}
	err := w.jobs.SuspendJobBinding(ctx, cycle.OrgID, cycle.ProjectID, cycle.JobRef, cycle.Environment)
	switch {
	case err == nil:
		stamped, err := w.cycles.MarkJobSuspended(ctx, cycle.ID)
		if err != nil {
			slog.WarnContext(ctx, "codingagent.JobWatcher: mark job suspended failed (retried next tick)",
				"cycle", cycle.ID, "error", err)
			return
		}
		if stamped {
			slog.InfoContext(ctx, "codingagent.job_suspended", "cycle", cycle.ID, "component", cycle.JobRef, "cause", cause)
		}
	case errors.Is(err, openchoreo.ErrNotFound):
		slog.InfoContext(ctx, "codingagent.JobWatcher: component gone, nothing to suspend",
			"cycle", cycle.ID, "component", cycle.JobRef)
		if _, err := w.cycles.MarkJobSuspended(ctx, cycle.ID); err != nil {
			slog.WarnContext(ctx, "codingagent.JobWatcher: mark job suspended failed (retried next tick)",
				"cycle", cycle.ID, "error", err)
		}
	case errors.Is(err, openchoreo.ErrSuspendUnsupported):
		slog.WarnContext(ctx, "codingagent.job_suspend_unsupported", "cycle", cycle.ID, "component", cycle.JobRef)
	default:
		slog.WarnContext(ctx, "codingagent.JobWatcher: suspend job failed (retried next tick)",
			"cycle", cycle.ID, "component", cycle.JobRef, "error", err)
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
// no room" and "a secret had not synced yet" — and then suspends its Job, so
// the pod Kubernetes would schedule once the cluster has room never starts an
// agent on the closed cycle.
func (w *JobWatcher) checkStartupGrace(ctx context.Context, cycle *delivery.RunCycle, binding string, pod openchoreo.RuntimePod) {
	// Measured from the attempt in flight (its dispatch), so a re-dispatch
	// restarts it.
	if time.Since(cycle.StartupGraceStart()) < w.startupGrace {
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
	// Suspend only on the branch that won the close, AFTER it: the close is
	// the verdict, and an open cycle is never suspended here (a cycle with a
	// pull request is fenced out of the close). A close lost to another
	// replica is suspended by the closed-cycle rule in checkCycle next tick.
	if w.failCycle(ctx, cycle, StartupFailureReason(pod, events)) {
		w.suspendJob(ctx, cycle, causeStartupFailed)
	}
}

// startupWaitBenign are waiting reasons of a pod that is starting normally,
// not stuck: they are not recorded as a startup wait.
var startupWaitBenign = map[string]bool{"ContainerCreating": true, "PodInitializing": true}

// noteStartupWait keeps the open cycle's durable startup wait in step with
// its pod: a Pending pod with a stuck waiting reason (Unschedulable,
// ImagePullBackOff, CreateContainerConfigError, …) is recorded, written only
// when the reason differs from the row's, so a steady wait costs no write per
// tick; a pod that runs, or that is Pending and no longer stuck, clears a
// recorded wait. An Unknown pod (a node that stopped reporting), no pod, and a
// leftover from the previous attempt (already blanked) write nothing. A
// failed write is logged and retried by the next tick's comparison.
func (w *JobWatcher) noteStartupWait(ctx context.Context, cycle *delivery.RunCycle, pod openchoreo.RuntimePod) {
	if !pod.Found {
		return
	}
	waiting := cycle.StartupWaitReason != "" || cycle.StartupWaitSince != nil
	switch pod.Phase {
	case "Pending":
		if reason := pod.WaitingReason; reason != "" && !startupWaitBenign[reason] {
			if reason == cycle.StartupWaitReason {
				return
			}
			if err := w.cycles.NoteStartupWait(ctx, cycle.ID, reason, time.Now().UTC()); err != nil {
				slog.WarnContext(ctx, "codingagent.JobWatcher: note startup wait failed (retried next tick)",
					"cycle", cycle.ID, "error", err)
				return
			}
			slog.InfoContext(ctx, "codingagent.startup_wait", "cycle", cycle.ID, "component", cycle.JobRef, "reason", reason)
			return
		}
	case "Running", "Succeeded", "Failed":
	default:
		return
	}
	if !waiting {
		return
	}
	if err := w.cycles.ClearStartupWait(ctx, cycle.ID); err != nil {
		slog.WarnContext(ctx, "codingagent.JobWatcher: clear startup wait failed (retried next tick)",
			"cycle", cycle.ID, "error", err)
	}
}

// failCycle records the terminal reason. The repository's own fences (open, and
// no pull request) decide whether the write lands, so this is safe to re-enter
// and safe to run in more than one replica. Reports whether THIS call closed
// the cycle.
func (w *JobWatcher) failCycle(ctx context.Context, cycle *delivery.RunCycle, reason string) bool {
	if cycle.EndedAt != nil {
		return false
	}
	closed, err := w.cycles.FinishAgentFailed(ctx, cycle.ID, reason)
	if err != nil {
		slog.ErrorContext(ctx, "codingagent.JobWatcher: finish cycle failed", "cycle", cycle.ID, "reason", reason, "error", err)
		return false
	}
	if closed == nil {
		return false // another replica got there, or the cycle has a pull request
	}
	slog.InfoContext(ctx, "codingagent.JobWatcher: cycle agent terminal",
		"cycle", cycle.ID, "run", cycle.RunID, "job", cycle.JobRef, "reason", reason)
	// AFTER the durable write and only on the branch that won it: record, then
	// signal, the order the cancel surface uses. Riding the once-only fence is
	// also what keeps a second replica from waking the same run twice. Identity
	// comes from the cycle being reconciled — cycleWatchStore promises nothing
	// about which columns `closed` carries.
	if w.deaths != nil && cycle.RunID != "" {
		if err := w.deaths.AgentDied(ctx, cycle.OrgID, cycle.RunID, reason); err != nil {
			slog.WarnContext(ctx, "codingagent.JobWatcher: agent-death notify failed (run waits out its landing deadline)",
				"cycle", cycle.ID, "run", cycle.RunID, "error", err)
		}
	}
	return true
}

// failOnProviderLimit closes a cycle whose runner stopped because its model
// provider refused every call (delivery/provider_limit.go), under the reason
// the supervisor settles BLOCKED on instead of reading agent death.
//
// The failure record is written FIRST, and that order is load-bearing:
// RecordFailure only writes a run that is not yet terminal, and closing the
// cycle is what wakes the supervisor into settling it. Best-effort, like every
// record write — the cycle's reason is the fact the run settles on; the record
// is what lets the console name the host and the reset time.
func (w *JobWatcher) failOnProviderLimit(ctx context.Context, cycle *delivery.RunCycle, limit providerLimit) {
	if cycle.EndedAt != nil {
		return
	}
	// The provider is named by the host the cycle was DISPATCHED on — stamped by
	// aep-api from the connection whose key the Job mounted — rather than the
	// one the runner reported: the platform's own record wins over a producer's
	// claim, as it does for pricing. The runner's is the fallback for a cycle
	// dispatched before the host was stamped.
	host := cycle.ModelHost
	if host == "" {
		host = limit.host
	}
	// The operator's evidence: what a spent plan actually returns is learned
	// from production, one line per stopped run, grouped by host. The runner
	// saw no response headers (its runtime made the call), so its retry delay
	// is folded into resetAt and the provider's own words ride `body`. Never
	// stored, never shown: it is a third party's text.
	slog.WarnContext(ctx, "model_provider_429",
		"source", "runner", "org", cycle.OrgID, "run", cycle.RunID, "cycle", cycle.ID,
		// The runner's rule counts only 429s (by status, or by the rate-limit
		// class when the runtime did not know the status).
		"host", host, "status", http.StatusTooManyRequests,
		"resetAt", limit.resetAt, "body", limit.detail, "verdict", "provider_limit")
	if w.failures != nil && cycle.RunID != "" {
		now := time.Now().UTC()
		phase := delivery.RunPhaseCoding
		if cycle.Kind == delivery.CycleKindValidation {
			phase = delivery.RunPhaseValidating
		}
		if _, err := w.failures.RecordFailure(ctx, cycle.RunID, delivery.RunFailure{
			Code:  delivery.RunFailureCodeModelProviderLimit,
			Phase: phase,
			// Not retried: the answer cannot change before the plan resets, so
			// the one attempt it took is the whole of it.
			Attempts:    1,
			MaxAttempts: 1,
			FirstAt:     now,
			LastAt:      now,
			Host:        host,
			ResetAt:     limit.resetAt,
		}); err != nil {
			slog.WarnContext(ctx, "codingagent.JobWatcher: record provider-limit failure failed (the run still settles blocked)",
				"cycle", cycle.ID, "run", cycle.RunID, "error", err)
		}
	}
	w.failCycle(ctx, cycle, delivery.CycleReasonModelProviderLimit)
}

// readTerminal reads the runner's last words off a finished pod — the only
// reason this watcher reads a log, and the bytes are dropped straight after:
// the console reads logs from OpenChoreo and the observability plane, never
// from a table.
//
// It reads nothing when nothing is left to learn: the usage is banked once a
// cycle carries a model id (a restart re-reads nothing), and a verdict is only
// wanted while the cycle is open (needVerdict). An empty report is also what a
// failed read degrades to, so a pod whose log is gone fails as agent death.
func (w *JobWatcher) readTerminal(ctx context.Context, cycle *delivery.RunCycle, binding string, pod openchoreo.RuntimePod, needVerdict bool) terminalReport {
	if !pod.Found || (cycle.ModelID != "" && !needVerdict) {
		return terminalReport{}
	}
	lines, err := w.runtime.PodLogs(ctx, cycle.OrgID, binding, pod.Name, 0)
	if err != nil {
		slog.WarnContext(ctx, "codingagent.JobWatcher: terminal log read failed; usage not captured",
			"cycle", cycle.ID, "pod", pod.Name, "error", err)
		return terminalReport{}
	}
	return terminalFromLog(joinTail(lines, finalLogTailBytes))
}

// captureUsage banks the run's token spend from its terminal report.
// Idempotence is DB-driven: a cycle that already carries a model id has been
// captured.
func (w *JobWatcher) captureUsage(ctx context.Context, cycle *delivery.RunCycle, report terminalReport) {
	if cycle.ModelID != "" || report.usage == nil {
		return
	}
	if err := w.cycles.RecordUsage(ctx, cycle.ID, *report.usage); err != nil {
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
