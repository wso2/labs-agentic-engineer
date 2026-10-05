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

// component_settler.go — deletes each settled coding cycle's Component.
//
// A closed cycle's Component still holds a billing concurrency slot, and its
// Job is re-created by OpenChoreo after the TTL, so it is deleted once the
// cycle has settled. Per closed cycle (ListSettling), the delete waits for:
//
//  1. the binding to still resolve — a missing one means someone else deleted
//     the Component, which is recorded and nothing more;
//  2. no pod at all in the binding's tree, of ANY attempt — deleting with a pod
//     orphans it (the watcher's leftover rule does not apply here);
//  3. the Job suspended (job_suspended_at, set by the watcher, the cancel or
//     this sweep's backstop), so a Job re-created meanwhile is born suspended —
//     unless the release predates the suspend schema (ErrSuspendUnsupported),
//     where suspend does not apply and conditions 1, 2 and 4 alone decide;
//  4. a "no pod" read noted on an earlier pass (pod_gone_at), and the grace
//     elapsed since the LATER of that note and the suspend; any pass that sees
//     a pod clears the note. A resource tree read under load can answer 200
//     and empty (see the watcher), so one empty read is never evidence. The
//     grace also covers the observer's indexing lag and the runner's SIGTERM
//     grace many times over.
//
// Usage capture is done by construction: the watcher captures from the pod's
// log while the pod exists, and "no pod" is the moment no more capture is
// possible.
//
// The BACKSTOP suspends a closed cycle whose Job nobody suspended. A cycle can
// close on the merge webhook before its pod exits, so a Running or Pending pod
// is left to the watcher (which suspends at terminal, after the usage line)
// until backstopCeiling past the close, and "no pod" counts only once an
// earlier pass noted it too: one empty tree read must not kill a Running pod.
// A terminal pod is suspended on sight. A cancelled cycle has no line to
// protect: its failed cancel-time suspend is retried at once.

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

const (
	settleInterval = 60 * time.Second
	// settleBatch is how many closed cycles one pass visits. Fair paging
	// (ListSettling) rotates the rest through later passes.
	settleBatch = 200
	// defaultSettleGrace is the minimum distance between the two "no pod" reads
	// (CODING_AGENT_SETTLE_GRACE).
	defaultSettleGrace = 5 * time.Minute
	// backstopCeiling is how long after a merge-closed cycle the backstop leaves
	// a Running or Pending pod alone: the schema's deadline ceiling, past which
	// Kubernetes has killed the pod anyway, plus a margin.
	backstopCeiling = time.Duration(openchoreo.CodingAgentDeadlineCeilingSeconds)*time.Second + 10*time.Minute
)

// settleStore is the cycle state the settler reads and writes.
type settleStore interface {
	ListSettling(ctx context.Context, limit int) ([]delivery.RunCycle, error)
	NoteSettleChecked(ctx context.Context, id string, at time.Time) error
	MarkJobSuspended(ctx context.Context, id string) error
	NotePodGone(ctx context.Context, id string, at time.Time) error
	ClearPodGone(ctx context.Context, id string) error
	MarkComponentDeleted(ctx context.Context, id string) error
}

// componentDeleter deletes a Component by name; 404 is success. Satisfied by
// openchoreo.ComponentClient.
type componentDeleter interface {
	DeleteComponent(ctx context.Context, orgName, projectName, componentName string) error
}

// ComponentSettler deletes each closed coding cycle's Component once no pod is
// left, and suspends the Jobs nobody else suspended.
type ComponentSettler struct {
	runtime    openchoreo.RuntimeClient
	jobs       jobSuspender
	components componentDeleter
	cycles     settleStore
	// targets is the fallback for a cycle with no recorded environment.
	targets writeTargetResolver
	// asService lifts each pass into the service identity: the sweep has no
	// inbound request. nil in tests.
	asService func(ctx context.Context) context.Context

	grace time.Duration
	now   func() time.Time

	// unsupported records the cycles whose release cannot render suspend, learnt
	// from the first ErrSuspendUnsupported. A closed cycle's release never
	// changes, so the suspend is not asked again (and the warning is logged
	// once). In memory: a restart re-learns it with one call per cycle.
	unsupported map[string]bool

	// staleGone records the cycles whose pod_gone_at outlived a pod seen since:
	// the ClearPodGone write failed. Until a clear lands, that stamp is no
	// evidence of anything, so nothing is decided on it.
	staleGone map[string]bool
	// startedAt is this process's first pass. A pod_gone_at written before it
	// is stale too: staleGone does not survive a restart, so a note from an
	// earlier process may hide a pod seen (and a failed clear) since. Such a
	// note is cleared and the current read starts a fresh sequence.
	startedAt time.Time

	once sync.Once
}

// NewComponentSettler wires the settler. Every dependency but asService is
// required.
func NewComponentSettler(runtime openchoreo.RuntimeClient, jobs jobSuspender, components componentDeleter,
	cycles settleStore, targets writeTargetResolver, asService func(context.Context) context.Context) *ComponentSettler {
	if runtime == nil || jobs == nil || components == nil || cycles == nil || targets == nil {
		panic("codingagent.ComponentSettler: runtime, jobs, components, cycles and targets are required")
	}
	return &ComponentSettler{
		runtime:     runtime,
		jobs:        jobs,
		components:  components,
		cycles:      cycles,
		targets:     targets,
		asService:   asService,
		grace:       defaultSettleGrace,
		now:         time.Now,
		unsupported: map[string]bool{},
		staleGone:   map[string]bool{},
	}
}

// WithGrace sets the minimum distance between the two "no pod" reads. Zero or
// less keeps the default. Returns the receiver.
func (s *ComponentSettler) WithGrace(d time.Duration) *ComponentSettler {
	if d > 0 {
		s.grace = d
	}
	return s
}

// WithClock replaces the clock (tests). Returns the receiver.
func (s *ComponentSettler) WithClock(now func() time.Time) *ComponentSettler {
	s.now = now
	return s
}

// Run blocks until ctx is canceled, ticking at once and then every minute.
func (s *ComponentSettler) Run(ctx context.Context) {
	ticker := time.NewTicker(settleInterval)
	defer ticker.Stop()
	s.once.Do(func() {
		slog.Info("codingagent.ComponentSettler: started", "interval", settleInterval, "grace", s.grace)
	})
	s.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			slog.Info("codingagent.ComponentSettler: stopping")
			return
		case <-ticker.C:
			s.Tick(ctx)
		}
	}
}

// Tick runs one pass over at most settleBatch closed cycles. Exported so a
// test drives a single pass.
func (s *ComponentSettler) Tick(ctx context.Context) {
	if s.asService != nil {
		ctx = s.asService(ctx)
	}
	if s.startedAt.IsZero() {
		s.startedAt = s.now()
	}
	rows, err := s.cycles.ListSettling(ctx, settleBatch)
	if err != nil {
		slog.ErrorContext(ctx, "codingagent.ComponentSettler: list settling cycles failed", "error", err)
		return
	}
	for i := range rows {
		s.settle(ctx, &rows[i])
	}
}

// settle moves one closed cycle as far towards deletion as this pass allows.
// Every exit before the delete leaves the row settling for a later pass.
func (s *ComponentSettler) settle(ctx context.Context, cycle *delivery.RunCycle) {
	now := s.now()
	// First, whatever follows: a row that fails below must still rotate to the
	// back of the sweep.
	if err := s.cycles.NoteSettleChecked(ctx, cycle.ID, now); err != nil {
		slog.WarnContext(ctx, "codingagent.ComponentSettler: note settle check failed",
			"cycle", cycle.ID, "error", err)
	}
	env, err := cycleEnvironment(ctx, s.targets, cycle)
	if err != nil {
		slog.WarnContext(ctx, "codingagent.ComponentSettler: no environment to read the cycle in (retried next pass)",
			"cycle", cycle.ID, "component", cycle.JobRef, "error", err)
		return
	}
	cycle.Environment = env

	binding, err := s.runtime.ReleaseBindingName(ctx, cycle.OrgID, cycle.ProjectID, cycle.JobRef, env)
	if errors.Is(err, openchoreo.ErrNotFound) {
		s.markDeleted(ctx, cycle)
		slog.InfoContext(ctx, "codingagent.ComponentSettler: component already gone",
			"cycle", cycle.ID, "component", cycle.JobRef)
		return
	}
	if err != nil {
		slog.WarnContext(ctx, "codingagent.ComponentSettler: release binding lookup failed (retried next pass)",
			"cycle", cycle.ID, "component", cycle.JobRef, "error", err)
		return
	}
	pod, err := s.runtime.PodSnapshot(ctx, cycle.OrgID, binding)
	if err != nil {
		slog.WarnContext(ctx, "codingagent.ComponentSettler: resource tree read failed (retried next pass)",
			"cycle", cycle.ID, "component", cycle.JobRef, "error", err)
		return
	}

	// A pod_gone_at that outlived a seen pod, or that an earlier process wrote,
	// is cleared before anything (the delete, the backstop) reads it; while the
	// clear keeps failing, this row decides nothing.
	if cycle.PodGoneAt != nil && (pod.Found || s.staleGone[cycle.ID] || cycle.PodGoneAt.Before(s.startedAt)) {
		if !s.clearPodGone(ctx, cycle) {
			return
		}
	}
	held := s.jobHeld(ctx, cycle, pod, now)
	if pod.Found {
		return
	}
	if cycle.PodGoneAt == nil {
		// The first no-pod read, held or not: evidence only for a later pass.
		if err := s.cycles.NotePodGone(ctx, cycle.ID, now); err != nil {
			slog.WarnContext(ctx, "codingagent.ComponentSettler: note pod gone failed",
				"cycle", cycle.ID, "error", err)
		}
		return
	}
	if !held {
		return
	}
	if now.Sub(graceStart(cycle)) < s.grace {
		return
	}
	// By NAME: a pre-UID row has no UID, and a cycle's Component name is its own.
	if err := s.components.DeleteComponent(ctx, cycle.OrgID, cycle.ProjectID, cycle.JobRef); err != nil {
		slog.WarnContext(ctx, "codingagent.ComponentSettler: delete component failed (retried next pass)",
			"cycle", cycle.ID, "component", cycle.JobRef, "error", err)
		return
	}
	if s.markDeleted(ctx, cycle) {
		slog.InfoContext(ctx, "codingagent.component_deleted",
			"cycle", cycle.ID, "component", cycle.JobRef, "componentUid", cycle.ComponentUID)
	}
}

// clearPodGone forgets the cycle's pod_gone_at because a pod was seen since.
// A failed write is remembered (staleGone), so the stale stamp cannot count as
// a no-pod read on a later pass. False when the write failed.
func (s *ComponentSettler) clearPodGone(ctx context.Context, cycle *delivery.RunCycle) bool {
	if err := s.cycles.ClearPodGone(ctx, cycle.ID); err != nil {
		s.staleGone[cycle.ID] = true
		slog.WarnContext(ctx, "codingagent.ComponentSettler: clear pod gone failed (row skipped until it lands)",
			"cycle", cycle.ID, "error", err)
		return false
	}
	delete(s.staleGone, cycle.ID)
	cycle.PodGoneAt = nil
	return true
}

// graceStart is when the delete grace began: the later of the first no-pod
// note and the suspend, so a Job suspended after its pod was noted gone still
// gets the whole grace with the Job held.
func graceStart(cycle *delivery.RunCycle) time.Time {
	start := *cycle.PodGoneAt
	if cycle.JobSuspendedAt != nil && cycle.JobSuspendedAt.After(start) {
		start = *cycle.JobSuspendedAt
	}
	return start
}

// jobHeld reports whether a re-created Job cannot run the runner again: the
// Job is suspended, or its release cannot render suspend at all (legacy). It
// runs the backstop suspend when the cycle's Job is not yet suspended and the
// pod allows it.
func (s *ComponentSettler) jobHeld(ctx context.Context, cycle *delivery.RunCycle, pod openchoreo.RuntimePod, now time.Time) bool {
	if cycle.JobSuspendedAt != nil || s.unsupported[cycle.ID] {
		return true
	}
	if !backstopDue(cycle, pod, now) {
		return false
	}
	err := s.jobs.SuspendJobBinding(ctx, cycle.OrgID, cycle.ProjectID, cycle.JobRef, cycle.Environment)
	switch {
	case err == nil:
		if err := s.cycles.MarkJobSuspended(ctx, cycle.ID); err != nil {
			slog.WarnContext(ctx, "codingagent.ComponentSettler: mark job suspended failed (retried next pass)",
				"cycle", cycle.ID, "error", err)
			return false
		}
		cycle.JobSuspendedAt = &now
		slog.InfoContext(ctx, "codingagent.job_suspended", "cycle", cycle.ID, "component", cycle.JobRef, "cause", "backstop")
		return true
	case errors.Is(err, openchoreo.ErrNotFound):
		// The binding went between the reads: nothing is left to suspend.
		if err := s.cycles.MarkJobSuspended(ctx, cycle.ID); err != nil {
			slog.WarnContext(ctx, "codingagent.ComponentSettler: mark job suspended failed (retried next pass)",
				"cycle", cycle.ID, "error", err)
			return false
		}
		cycle.JobSuspendedAt = &now
		return true
	case errors.Is(err, openchoreo.ErrSuspendUnsupported):
		s.unsupported[cycle.ID] = true
		slog.WarnContext(ctx, "codingagent.job_suspend_unsupported", "cycle", cycle.ID, "component", cycle.JobRef)
		return true
	default:
		// Includes a binding naming a missing release: never held, so never
		// deleted, and retried on each of its passes.
		slog.WarnContext(ctx, "codingagent.ComponentSettler: backstop suspend failed (retried next pass)",
			"cycle", cycle.ID, "component", cycle.JobRef, "error", err)
		return false
	}
}

// backstopDue reports whether the backstop may suspend the cycle's Job now: a
// cancelled cycle at once; a terminal pod on sight; no pod only when an
// earlier pass saw none either (pod_gone_at), because one empty tree read can
// hide a Running pod; a Running or Pending pod only past backstopCeiling after
// the close.
func backstopDue(cycle *delivery.RunCycle, pod openchoreo.RuntimePod, now time.Time) bool {
	if cycle.AgentReason == delivery.CycleReasonCancelled {
		return true
	}
	switch ClassifyPod(pod) {
	case OutcomeSucceeded, OutcomeFailed:
		return true
	}
	if !pod.Found {
		return cycle.PodGoneAt != nil
	}
	return cycle.EndedAt != nil && now.Sub(*cycle.EndedAt) > backstopCeiling
}

// markDeleted records the Component as deleted and forgets the cycle's
// in-memory facts. False when the write failed (retried next pass).
func (s *ComponentSettler) markDeleted(ctx context.Context, cycle *delivery.RunCycle) bool {
	if err := s.cycles.MarkComponentDeleted(ctx, cycle.ID); err != nil {
		slog.WarnContext(ctx, "codingagent.ComponentSettler: mark component deleted failed (retried next pass)",
			"cycle", cycle.ID, "error", err)
		return false
	}
	delete(s.unsupported, cycle.ID)
	delete(s.staleGone, cycle.ID)
	return true
}
