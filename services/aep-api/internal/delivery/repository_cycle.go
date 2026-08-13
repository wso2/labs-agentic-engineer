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
	// re-points the row at the newly dispatched Job. The supervisor compares the
	// returned Attempts against RunMaxRedispatchPerCycle to decide whether the
	// per-cycle re-dispatch budget is spent. Guarded on the cycle being open.
	NoteDispatch(ctx context.Context, id, jobRef string) (*RunCycle, error)

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

	// SetValidationVerdict records what one validation ATTEMPT concluded, and the
	// issue it was dispatched at.
	//
	// It is the ONE mutator not fenced on the cycle being open, and deliberately:
	// the verdict is derived from the report at the cycle's own merge commit, which
	// the supervisor can only read AFTER Finish has stamped ended_at. The fence is
	// write-once instead — an empty verdict — because an attempt concludes exactly
	// once and a second write could only be a retry or a bug. Guarded that way, a
	// redelivered activity is a no-op rather than a rewrite.
	//
	// Returns (nil, nil) when the cycle is absent or already carries a verdict.
	SetValidationVerdict(ctx context.Context, id, verdict string, issue int) (*RunCycle, error)

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

	// ListOpenCycleIDs returns the ids of the project's cycles that have not
	// ended — the LIVE set. The agent-component reaper reads it to decide what
	// it may delete: OpenChoreo registers no health check for a `batch/v1 Job`,
	// so a Component's own status cannot say whether its pod is still running,
	// while a cycle row with no ended_at can.
	//
	// Org-scoped because it is derived from an already-org-resolved dispatch.
	ListOpenCycleIDs(ctx context.Context, orgID, projectID string) ([]string, error)

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
	return r.updateOpen(ctx, id, map[string]any{
		"attempts": gorm.Expr("attempts + 1"),
		"job_ref":  jobRef,
	})
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

func (r *runCycleRepository) SetValidationVerdict(ctx context.Context, id, verdict string, issue int) (*RunCycle, error) {
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
		})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.getByID(ctx, id)
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

func (r *runCycleRepository) ListOpenCycleIDs(ctx context.Context, orgID, projectID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).
		Model(&RunCycle{}).
		Where("org_id = ? AND project_id = ? AND ended_at IS NULL", orgID, projectID).
		Order("created_at ASC").
		Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
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
	// Stamp USD at capture from the rates in force now (#291): frozen on the row,
	// never re-derived. Null when unpriceable (any token-bearing slice without a
	// rate row).
	if r.stamper != nil {
		updates["cost_usd"] = stampCapturedCost(r.stamper, u)
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

// stampCapturedCost prices a capture for a row write (#291): the runner's
// per-model split when present (each slice at its own rate, summed by
// Stamper.SumCost's all-or-nothing rule), else the aggregate as one slice —
// the pre-split runner shape, where a mixed run has model "" and stays null.
func stampCapturedCost(stamper *modelcost.Stamper, u contracts.CapturedUsage) *float64 {
	slices := u.PricingSlices()
	ts := make([]modelcost.Tokens, 0, len(slices))
	for _, s := range slices {
		ts = append(ts, modelcost.Tokens{
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
