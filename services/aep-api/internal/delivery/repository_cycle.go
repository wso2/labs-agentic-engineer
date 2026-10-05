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

package delivery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

// RunCycleRepository is the write-authority over a run's cycle records — one
// row per dispatch. Lookups miss with (nil, nil), never gorm.ErrRecordNotFound,
// and every mutator is guarded on the cycle still being open (ended_at IS NULL)
// so a duplicate webhook is a no-op returning (nil, nil) rather than a rewrite
// of a closed cycle.
//
// Mutators are keyed by cycle id and are deliberately NOT org-scoped: they are
// platform-internal writes driven by dispatch and by webhooks, both of which
// reached the cycle through already-org-resolved facts. The reads that serve the
// HTTP surface take an orgID and fence on it.
type RunCycleRepository interface {
	// Append inserts a fresh cycle for a run, with Attempts at zero — the first
	// NoteDispatch takes it to one. Kind must be one of the CycleKind* values.
	Append(ctx context.Context, cycle *RunCycle) error

	// NoteDispatch records a dispatch of the cycle: it increments Attempts and
	// re-points the row at the newly dispatched Job, stamping dispatched_at and
	// clearing the previous attempt's settle stamps (job_suspended_at,
	// pod_gone_at). The supervisor compares the
	// returned Attempts against RunMaxRedispatchPerCycle to decide whether the
	// per-cycle re-dispatch budget is spent. Guarded on the cycle being open.
	NoteDispatch(ctx context.Context, id, jobRef string) (*RunCycle, error)

	// NoteLaunch records what the cycle's agent was launched on: the host of
	// its model connection and the environment its Job was bound into. Both are
	// COPIED at dispatch, as the runtime and model are copied into the Job, so a
	// connection changed mid-run neither reprices the cycle nor makes its usage
	// name a host it never ran on (RecordUsage prices the capture against the
	// host), and a write target moved mid-run does not send the cycle's readers
	// to an environment its Job was never in. Guarded on the cycle being open.
	// componentUID is the UID of the Component the Job runs as.
	NoteLaunch(ctx context.Context, id, host, environment, componentUID string) (*RunCycle, error)

	// MarkJobSuspended stamps job_suspended_at once (WHERE it IS NULL); a later
	// call keeps the first stamp. stamped reports whether THIS call wrote it, so
	// only the caller whose suspend took effect announces it. Not fenced on the
	// cycle being open: a Job is suspended after its cycle closes.
	MarkJobSuspended(ctx context.Context, id string) (stamped bool, err error)

	// NotePodGone records when the cycle's pod was first seen gone, once
	// (WHERE pod_gone_at IS NULL).
	NotePodGone(ctx context.Context, id string, at time.Time) error

	// ClearPodGone forgets a recorded pod_gone_at, for a pod that reappeared.
	ClearPodGone(ctx context.Context, id string) error

	// MarkComponentDeleted stamps component_deleted_at once
	// (WHERE component_deleted_at IS NULL).
	MarkComponentDeleted(ctx context.Context, id string) error

	// FinishCancelled closes an open cycle with agent_reason CycleReasonCancelled.
	// Unlike FinishAgentFailed it has no pr_number fence: a cancel ends a cycle
	// that already opened its pull request. (nil, nil) when already closed.
	FinishCancelled(ctx context.Context, id string) (*RunCycle, error)

	// ListSettling returns closed Job cycles (job_ref 'ca-%') whose Component is
	// not yet deleted, at most limit: never-checked first, then the least
	// recently checked (settle_checked_at), oldest ended breaking ties. The
	// order is what makes the sweep fair: a row the settler visits goes to the
	// back, so a fixed set that never settles cannot hold every page.
	ListSettling(ctx context.Context, limit int) ([]RunCycle, error)

	// NoteSettleChecked stamps settle_checked_at: the settler visited the cycle.
	NoteSettleChecked(ctx context.Context, id string, at time.Time) error

	// NotePullRequest records the pull request the agent actually opened, learned
	// from the pull_request webhook — the platform never dictates branch identity
	// or link, it observes them. Guarded on the cycle being open.
	NotePullRequest(ctx context.Context, id string, pr CyclePullRequest) (*RunCycle, error)

	// NoteMergeDecision records what the merge policy decided about the cycle's
	// pull request: the matched issue set, and the verdict (with its reason) when
	// the pull request did not merge.
	//
	// It is a SEPARATE mutator from NotePullRequest on purpose. Pull request
	// identity is backfilled from the merge webhook too, and a backfill has no
	// decision in hand — folding both into one update would let it clobber a
	// recorded verdict with zero values. Guarded on the cycle being open.
	NoteMergeDecision(ctx context.Context, id string, resolves []int, verdict, reason string) (*RunCycle, error)

	// Finish closes the cycle: it stamps ended_at and records the merge SHA the
	// cycle landed. mergeSHA is empty for a cycle that ended without a merge
	// (agent death, budget exhaustion, cancel). Guarded on the cycle being open,
	// so the first close wins.
	Finish(ctx context.Context, id, mergeSHA string) (*RunCycle, error)

	// FinishAgentFailed closes a cycle whose agent died without landing a pull
	// request, recording why. It is the pod-truth watcher's ONE write.
	//
	// It is fenced twice — the cycle must be OPEN and must carry NO pull request
	// — and the second fence is the important one: a pull request means the side
	// effects landed, so a pod that exits badly afterwards is not evidence
	// against it. Closing such a cycle here would fence out the very
	// pull_request webhook that completes the run. Returns (nil, nil) when
	// either fence rejects, which is the ordinary outcome of a re-tick.
	FinishAgentFailed(ctx context.Context, id, reason string) (*RunCycle, error)

	// SetValidationVerdict records what one validation ATTEMPT concluded: the
	// verdict, the issue it was dispatched at, and the DIGEST of the evidence
	// behind it.
	//
	// It is the ONE mutator not fenced on the cycle being open, and deliberately:
	// the verdict is derived from the report at the cycle's own merge commit, which
	// the supervisor can only read AFTER Finish has stamped ended_at. The fence is
	// write-once instead — an empty verdict — because an attempt concludes exactly
	// once and a second write could only be a retry or a bug. Guarded that way, a
	// redelivered activity is a no-op rather than a rewrite.
	//
	// The digest rides THIS call rather than one of its own for exactly that
	// reason: the write-once fence would reject any later write, so a digest
	// recorded separately could never land on a cycle that already has a verdict.
	//
	// Returns (nil, nil) when the cycle is absent or already carries a verdict.
	SetValidationVerdict(ctx context.Context, id, verdict string, issue int, digest string) (*RunCycle, error)

	// LatestValidationDigest returns the newest recorded report digest among the
	// given runs' validation cycles, or "" when none of them recorded one.
	//
	// It is how one validation attempt reads what the PREVIOUS attempt concluded.
	// Attempts are separate runs, so the comparison cannot live in workflow state,
	// and it is keyed on run ids rather than on a milestone because run_cycles
	// carries no milestone column — the caller resolves the milestone's validation
	// runs first, which it must do anyway to count the attempts.
	//
	// An empty runIDs is not an error: a milestone's first validation attempt has
	// nothing to compare against.
	LatestValidationDigest(ctx context.Context, orgID string, runIDs []string) (string, error)

	// Latest returns a run's newest cycle, or (nil, nil) when the run has not
	// dispatched yet. This is how loop POSITION is read — never from a stored
	// phase enum on the run row.
	Latest(ctx context.Context, orgID, runID string) (*RunCycle, error)

	// GetByIDScoped returns the cycle only when it belongs to orgID, and (nil,
	// nil) otherwise — the tenant fence, so a cross-org id reads as absent rather
	// than forbidden and a probe learns nothing from the difference.
	//
	// This is the RUNNER's identity read: a dispatched pod names its cycle id on
	// every callback (AEP_TASK_ID), and this resolves it to the project the
	// callback may act on.
	GetByIDScoped(ctx context.Context, orgID, id string) (*RunCycle, error)

	// ListByRun returns a run's cycles oldest first — the cycle timeline.
	ListByRun(ctx context.Context, orgID, runID string) ([]RunCycle, error)

	// ListValidationCyclesByProject returns every VALIDATION cycle in a project,
	// oldest first — the whole validation ledger's timing in one read.
	//
	// Project-wide rather than per-run because the ledger has one row per
	// VERSION and a version's attempts can span several runs (a self-heal repeat
	// stays on one row; a revalidation is a new one). Asking per run would make
	// a single page load one query per milestone to answer a question about the
	// project, which is the cost that kept validation off the build ledger in the
	// first place.
	ListValidationCyclesByProject(ctx context.Context, orgID, projectID string) ([]RunCycle, error)

	// ListRecentDispatched returns every cycle that has launched a Job and is
	// either still open or closed no earlier than `since` — the JobWatcher's
	// claim set for pod-truth reads and terminal usage capture.
	//
	// It is deliberately NOT "open cycles only": the agent Job exits the moment
	// it opens its pull request, and the auto-merge that CLOSES the cycle follows
	// within seconds, so a watcher restricted to open cycles would routinely
	// arrive after the cycle had closed and miss the usage stamp. The window
	// instead tracks how long the Job's pod survives (its TTL), which is what
	// actually bounds the pass.
	//
	// Unscoped by org on purpose: it drives a platform watcher, not an HTTP read.
	ListRecentDispatched(ctx context.Context, since time.Time) ([]RunCycle, error)

	// HasOpenCycle reports whether any of the org's cycles has not ended, in
	// any project: an agent that may still be starting on the credential it
	// was dispatched with. The model connection key's rename reads it before
	// deleting the key's previous copy. A cycle row is appended before its Job
	// is launched, so a dispatch in progress already counts.
	HasOpenCycle(ctx context.Context, orgID string) (bool, error)

	// DeleteByProject purges a project's cycle records — the project-delete
	// cascade, paired with MilestoneRunRepository.DeleteByProject so a recreated
	// same-named project starts with a clean timeline.
	DeleteByProject(ctx context.Context, orgID, projectID string) error

	// RecordUsage stamps the cycle's captured token usage onto the row, and its
	// write-time USD onto cost_usd (#291) — each per-model slice priced at its
	// own rate row, so a multi-model run still stamps.
	//
	// It is the ONE mutator NOT guarded on the cycle being open, and that is the
	// whole point: usage arrives from the terminal-log capture, and a cycle
	// CLOSES on the merge webhook seconds after its agent Job exits — routinely
	// before the watcher's next tick. Fencing this on ended_at IS NULL would
	// discard nearly every capture. Idempotent by value: the capture re-derives
	// the same figures from the same log, so a repeat write is a no-op in effect.
	//
	// It also mirrors the capture into the agent-usage LEDGER, which this row's
	// purge does not reach. The rollup reads the ledger and nothing else, so the
	// stamp here is the run spine's own copy — not a second source of spend.
	RecordUsage(ctx context.Context, id string, u contracts.CapturedUsage) error
}

type runCycleRepository struct {
	db      *gorm.DB
	stamper *modelcost.Stamper
}

// NewRunCycleRepository wires the gorm-backed repository. stamper prices
// captured cycle usage at write time (#291); nil disables stamping (tests) and
// cost_usd stays null.
func NewRunCycleRepository(db *gorm.DB, stamper *modelcost.Stamper) RunCycleRepository {
	return &runCycleRepository{db: db, stamper: stamper}
}

func (r *runCycleRepository) Append(ctx context.Context, cycle *RunCycle) error {
	switch cycle.Kind {
	case CycleKindCoding, CycleKindConflict, CycleKindFix, CycleKindValidation:
	default:
		return fmt.Errorf("run cycle: unknown kind %q", cycle.Kind)
	}
	if cycle.RunID == "" {
		return errors.New("run cycle: RunID is required")
	}
	return r.db.WithContext(ctx).Create(cycle).Error
}

func (r *runCycleRepository) NoteDispatch(ctx context.Context, id, jobRef string) (*RunCycle, error) {
	// The settle stamps describe one attempt's Job: a new attempt starts with
	// neither, and with its own dispatch time, in the same write that moves
	// job_ref.
	return r.updateOpen(ctx, id, map[string]any{
		"attempts":         gorm.Expr("attempts + 1"),
		"job_ref":          jobRef,
		"dispatched_at":    time.Now().UTC(),
		"job_suspended_at": nil,
		"pod_gone_at":      nil,
	})
}

func (r *runCycleRepository) NoteLaunch(ctx context.Context, id, host, environment, componentUID string) (*RunCycle, error) {
	return r.updateOpen(ctx, id, map[string]any{
		"model_host": host, "environment": environment, "component_uid": componentUID,
	})
}

func (r *runCycleRepository) MarkJobSuspended(ctx context.Context, id string) (bool, error) {
	res := r.db.WithContext(ctx).Model(&RunCycle{}).
		Where("id = ? AND job_suspended_at IS NULL", id).
		Update("job_suspended_at", time.Now().UTC())
	return res.RowsAffected > 0, res.Error
}

func (r *runCycleRepository) NotePodGone(ctx context.Context, id string, at time.Time) error {
	return r.stampOnce(ctx, id, "pod_gone_at", at.UTC())
}

func (r *runCycleRepository) ClearPodGone(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&RunCycle{}).
		Where("id = ? AND pod_gone_at IS NOT NULL", id).
		Update("pod_gone_at", nil).Error
}

func (r *runCycleRepository) MarkComponentDeleted(ctx context.Context, id string) error {
	return r.stampOnce(ctx, id, "component_deleted_at", time.Now().UTC())
}

// stampOnce sets a nullable timestamp column only while it is still NULL, so a
// repeated call is a no-op that keeps the first stamp.
func (r *runCycleRepository) stampOnce(ctx context.Context, id, column string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&RunCycle{}).
		Where("id = ? AND "+column+" IS NULL", id).
		Update(column, at).Error
}

func (r *runCycleRepository) FinishCancelled(ctx context.Context, id string) (*RunCycle, error) {
	return r.updateOpen(ctx, id, map[string]any{
		"agent_reason": CycleReasonCancelled,
		"ended_at":     time.Now().UTC(),
	})
}

func (r *runCycleRepository) ListSettling(ctx context.Context, limit int) ([]RunCycle, error) {
	var rows []RunCycle
	err := r.db.WithContext(ctx).
		Where("job_ref LIKE 'ca-%' AND ended_at IS NOT NULL AND component_deleted_at IS NULL").
		Order("settle_checked_at ASC NULLS FIRST, ended_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *runCycleRepository) NoteSettleChecked(ctx context.Context, id string, at time.Time) error {
	return r.db.WithContext(ctx).Model(&RunCycle{}).
		Where("id = ?", id).
		Update("settle_checked_at", at.UTC()).Error
}

func (r *runCycleRepository) NotePullRequest(ctx context.Context, id string, pr CyclePullRequest) (*RunCycle, error) {
	return r.updateOpen(ctx, id, map[string]any{
		"branch":    pr.Branch,
		"pr_number": pr.Number,
		"pr_url":    pr.URL,
		"pr_draft":  pr.Draft,
	})
}

func (r *runCycleRepository) NoteMergeDecision(ctx context.Context, id string, resolves []int, verdict, reason string) (*RunCycle, error) {
	// A STRUCT update, not the map the other mutators use: resolves is a
	// serializer-backed jsonb column, and only the struct path runs the schema's
	// serializer. Select names the three columns so blanks are written too — the
	// row is a snapshot of the LATEST decision, so a pull request that was
	// declined and then re-pushed into a merge must not keep its stale verdict.
	return r.updateOpenColumns(ctx, id,
		[]string{"resolves", "merge_verdict", "merge_reason"},
		RunCycle{
			Resolves:     IssueNumbers(resolves),
			MergeVerdict: verdict,
			MergeReason:  reason,
		})
}

func (r *runCycleRepository) Finish(ctx context.Context, id, mergeSHA string) (*RunCycle, error) {
	return r.updateOpen(ctx, id, map[string]any{
		"merge_sha": mergeSHA,
		"ended_at":  time.Now().UTC(),
	})
}

func (r *runCycleRepository) FinishAgentFailed(ctx context.Context, id, reason string) (*RunCycle, error) {
	res := r.db.WithContext(ctx).
		Model(&RunCycle{}).
		Where("id = ? AND ended_at IS NULL AND pr_number = 0", id).
		Updates(map[string]any{
			"agent_reason": reason,
			"ended_at":     time.Now().UTC(),
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.getByID(ctx, id)
}

func (r *runCycleRepository) SetValidationVerdict(ctx context.Context, id, verdict string, issue int, digest string) (*RunCycle, error) {
	if !ValidationVerdicts[verdict] {
		return nil, fmt.Errorf("run cycle: unknown validation verdict %q", verdict)
	}
	// Not updateOpen: this write lands after Finish. The write-once fence replaces
	// the closed-cycle one — see SetValidationVerdict on the interface.
	res := r.db.WithContext(ctx).
		Model(&RunCycle{}).
		Where("id = ? AND (validation_verdict IS NULL OR validation_verdict = '')", id).
		Updates(map[string]any{
			"validation_verdict": verdict,
			"validation_issue":   issue,
			"validation_digest":  digest,
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.getByID(ctx, id)
}

func (r *runCycleRepository) LatestValidationDigest(ctx context.Context, orgID string, runIDs []string) (string, error) {
	if len(runIDs) == 0 {
		return "", nil
	}
	var row RunCycle
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND run_id IN ? AND kind = ? AND validation_digest <> ''",
			orgID, runIDs, CycleKindValidation).
		Order("created_at DESC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.ValidationDigest, nil
}

func (r *runCycleRepository) Latest(ctx context.Context, orgID, runID string) (*RunCycle, error) {
	var row RunCycle
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND run_id = ?", orgID, runID).
		Order("created_at DESC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *runCycleRepository) GetByIDScoped(ctx context.Context, orgID, id string) (*RunCycle, error) {
	var row RunCycle
	// The org is part of the WHERE, not a check after the read: a cycle that
	// belongs to another org must be indistinguishable from one that does not
	// exist.
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND id = ?", orgID, id).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *runCycleRepository) ListByRun(ctx context.Context, orgID, runID string) ([]RunCycle, error) {
	var rows []RunCycle
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND run_id = ?", orgID, runID).
		Order("created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *runCycleRepository) ListValidationCyclesByProject(ctx context.Context, orgID, projectID string) ([]RunCycle, error) {
	var rows []RunCycle
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND kind = ?", orgID, projectID, CycleKindValidation).
		Order("created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *runCycleRepository) ListRecentDispatched(ctx context.Context, since time.Time) ([]RunCycle, error) {
	var rows []RunCycle
	err := r.db.WithContext(ctx).
		Where("job_ref <> '' AND (ended_at IS NULL OR ended_at >= ?)", since.UTC()).
		Order("created_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *runCycleRepository) HasOpenCycle(ctx context.Context, orgID string) (bool, error) {
	var open bool
	err := r.db.WithContext(ctx).Raw(
		`SELECT EXISTS (SELECT 1 FROM run_cycles WHERE org_id = ? AND ended_at IS NULL)`, orgID,
	).Scan(&open).Error
	return open, err
}

func (r *runCycleRepository) DeleteByProject(ctx context.Context, orgID, projectID string) error {
	return r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Delete(&RunCycle{}).Error
}

func (r *runCycleRepository) RecordUsage(ctx context.Context, id string, u contracts.CapturedUsage) error {
	updates := map[string]any{
		"input_tokens":          u.InputTokens,
		"output_tokens":         u.OutputTokens,
		"cache_read_tokens":     u.CacheReadTokens,
		"cache_creation_tokens": u.CacheCreationTokens,
		"model_id":              u.Model,
	}
	// Both writes commit together or not at all. The ledger entry is copied out of
	// the row this stamps, and PhaseUsageRollup reads the ledger ALONE — so a
	// stamped row whose ledger copy failed is spend that exists on the cycle and
	// is invisible everywhere it is reported. The ledger is bound to the same tx
	// rather than injected, which is what keeps the two impossible to wire apart.
	//
	// Ordering inside the tx is load-bearing: the ledger copies the row, so the
	// row is stamped first and the INSERT … SELECT reads it uncommitted.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Stamp USD at capture from the rates in force now (#291): frozen on the
		// row, never re-derived. Priced on the host the dispatch copied onto the
		// row; null when unpriceable (any token-bearing slice without a
		// (host, model) rate row).
		if r.stamper != nil {
			host, err := rowModelHost(tx, &RunCycle{}, id)
			if err != nil {
				return err
			}
			updates["cost_usd"] = stampCapturedCost(r.stamper, host, u)
		}
		// NOT applyOpen: see RecordUsage's contract — a closed cycle is exactly the
		// case this has to serve.
		if err := tx.Model(&RunCycle{}).
			Where("id = ?", id).
			Updates(updates).Error; err != nil {
			return err
		}
		return NewAgentUsageLedgerRepository(tx).RecordCycleUsage(ctx, id)
	})
}

// rowModelHost reads the model host stamped on a usage row (a cycle or an
// execution) — the host its capture is priced on. A missing row reads as no
// host, which prices nil; the UPDATE that follows then touches nothing either.
func rowModelHost(tx *gorm.DB, model any, id string) (string, error) {
	var hosts []string
	if err := tx.Model(model).
		Where("id = ?", id).
		Pluck("COALESCE(model_host, '')", &hosts).Error; err != nil {
		return "", err
	}
	if len(hosts) == 0 {
		return "", nil
	}
	return hosts[0], nil
}

// stampCapturedCost prices a capture for a row write (#291): the runner's
// per-model split when present (each slice at its own rate, summed by
// Stamper.SumCost's all-or-nothing rule), else the aggregate as one slice —
// the pre-split runner shape, where a mixed run has model "" and stays null.
// Every slice is on host: a run launches on one connection, so its models are
// all that connection's.
func stampCapturedCost(stamper *modelcost.Stamper, host string, u contracts.CapturedUsage) *float64 {
	slices := u.PricingSlices()
	ts := make([]modelcost.Tokens, 0, len(slices))
	for _, s := range slices {
		ts = append(ts, modelcost.Tokens{
			Host:                host,
			ModelID:             s.Model,
			InputTokens:         s.InputTokens,
			OutputTokens:        s.OutputTokens,
			CacheReadTokens:     s.CacheReadTokens,
			CacheCreationTokens: s.CacheCreationTokens,
		})
	}
	return stamper.SumCost(ts)
}

// updateOpen applies a guarded update to a cycle that has not been closed and
// re-reads it. It is the ONE place the "a closed cycle is never rewritten"
// fence lives, so every mutator inherits it — and the (nil, nil) no-op contract
// on RowsAffected == 0.
func (r *runCycleRepository) updateOpen(ctx context.Context, id string, updates map[string]any) (*RunCycle, error) {
	return r.applyOpen(ctx, id, nil, updates)
}

// updateOpenColumns is updateOpen for a STRUCT update: the named columns are
// written even when their value is a zero value, and serializer-backed columns
// go through the schema rather than being handed to the driver raw.
func (r *runCycleRepository) updateOpenColumns(ctx context.Context, id string, columns []string, values RunCycle) (*RunCycle, error) {
	return r.applyOpen(ctx, id, columns, values)
}

func (r *runCycleRepository) applyOpen(ctx context.Context, id string, columns []string, values any) (*RunCycle, error) {
	tx := r.db.WithContext(ctx).
		Model(&RunCycle{}).
		Where("id = ? AND ended_at IS NULL", id)
	if len(columns) > 0 {
		tx = tx.Select(columns)
	}
	res := tx.Updates(values)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.getByID(ctx, id)
}

func (r *runCycleRepository) getByID(ctx context.Context, id string) (*RunCycle, error) {
	var row RunCycle
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
