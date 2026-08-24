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
	"gorm.io/gorm/clause"
)

// RunBudget names one budget counter column on a run row. It is a closed set
// with a whitelist (runBudgetColumns) because BumpBudget interpolates the
// column into SQL — the type keeps callers honest, the whitelist keeps the
// statement safe even if a caller invents a value.
type RunBudget string

const (
	// RunBudgetCycles is the total-cycle tally, checked against
	// MilestoneRun.CycleCeiling (RunReasonCycleCeiling).
	RunBudgetCycles RunBudget = "cycles_total"
	// RunBudgetFixCycles bounds the fix chain (RunReasonFixChainBudget).
	RunBudgetFixCycles RunBudget = "fix_cycles"
	// RunBudgetConflictCycles bounds the conflict chain (RunReasonConflictBudget).
	RunBudgetConflictCycles RunBudget = "conflict_cycles"
	// RunBudgetBuildRetriggers tallies automatic build re-triggers
	// (RunReasonBuildRetriggerBudget).
	RunBudgetBuildRetriggers RunBudget = "build_retriggers"
	// RunBudgetValidationCycles counts the validation cycles a run opened — one on
	// the ordinary path, more when an agent merged without committing a report and
	// was re-dispatched. Read-model bookkeeping only: the VERSION's attempt
	// allowance (RunMaxValidationAttempts) is counted from the milestone's
	// validation runs, not from this column.
	RunBudgetValidationCycles RunBudget = "validation_cycles"
)

// runBudgetColumns whitelists the columns BumpBudget may increment.
var runBudgetColumns = map[RunBudget]bool{
	RunBudgetCycles:           true,
	RunBudgetFixCycles:        true,
	RunBudgetConflictCycles:   true,
	RunBudgetBuildRetriggers:  true,
	RunBudgetValidationCycles: true,
}

// MilestoneRunRepository is the write-authority over milestone run rows — the
// platform's record of "work the open issues in milestone M". Lookups miss with
// (nil, nil), never gorm.ErrRecordNotFound, matching the house convention;
// guarded transitions likewise return (nil, nil) when they change no row, so a
// duplicate signal is a no-op rather than a resurrection.
//
// Mutators are keyed by run id and are deliberately NOT org-scoped: they are
// platform-internal writes driven by webhooks and the supervisor, which reach a
// run through facts (repo, milestone, job) that were already resolved to an org.
// The reads that serve the HTTP surface all take an orgID and fence on it.
type MilestoneRunRepository interface {
	// TryAdmit is the build mutex in DB form (the §5 409's twin): it INSERTs the
	// run unless a non-terminal DEV run already exists for the same (org,
	// project), via INSERT … ON CONFLICT DO NOTHING against the partial unique
	// index. It returns admitted=true with the populated row on success, or
	// admitted=false and nil when another entrant won the race.
	//
	// Task and validation runs sit outside that index and are refused only by the
	// per-milestone one — they execute concurrently on their own milestones. The
	// row's State defaults to waiting and CycleCeiling to RunDefaultCycleCeiling
	// when unset.
	TryAdmit(ctx context.Context, run *MilestoneRun) (admitted bool, row *MilestoneRun, err error)

	// ActiveDevRunByProject returns the project's live dev run — the lookup
	// behind the build endpoint's 409 — or (nil, nil) when the project is free.
	// The DB index is the authority; this read exists so the API can answer with
	// a useful conflict instead of a bare insert failure.
	//
	// It is the Go twin of that index's predicate, which is why it filters on the
	// same kind literal: a read narrowed differently from the index would either
	// refuse a build the database would have admitted, or promise one it will
	// then reject.
	ActiveDevRunByProject(ctx context.Context, orgID, projectID string) (*MilestoneRun, error)

	// ActiveValidationRunByProject returns a live VALIDATION run anywhere in the
	// project, or (nil, nil) when there is none. It is the second half of the
	// build endpoint's 409.
	//
	// It is a project-wide read where the mutex index is per-project-and-dev,
	// because there is no index to lean on here: a validation run deliberately
	// sits outside the build mutex (it re-judges a version that already shipped,
	// so holding up the next build for its duration would be wrong). What must not
	// happen is a delivery run merging and promoting WHILE validation asserts
	// against the deployment — that judges a moving target. The refusal is
	// therefore a read, and the way past it is to cancel the validation, which is
	// one click.
	ActiveValidationRunByProject(ctx context.Context, orgID, projectID string) (*MilestoneRun, error)

	// SetState moves a non-terminal run between waiting and running (the loop
	// oscillates across cycle boundaries) and stamps started_at on the first
	// transition to running. It refuses a terminal state — that is Settle's job
	// — and returns (nil, nil) when the run is already terminal.
	SetState(ctx context.Context, id, state string) (*MilestoneRun, error)

	// Settle ends a run: it writes a terminal state plus its terminal reason and
	// stamps ended_at, guarded on the run still being non-terminal so the first
	// settle wins and no later signal can overwrite a recorded outcome. reason
	// must be empty for succeeded and one of the RunReason* values otherwise.
	Settle(ctx context.Context, id, state, terminalReason string) (*MilestoneRun, error)

	// BumpBudget increments one budget counter by one, guarded on the run being
	// non-terminal. The supervisor compares the returned row against the
	// matching Run* limit to decide whether the budget is now exhausted.
	BumpBudget(ctx context.Context, id string, counter RunBudget) (*MilestoneRun, error)

	// RequestCancel stamps a human's cancellation request on the run, guarded on
	// the run being non-terminal, and returns (nil, nil) when it has already
	// settled — a cancel arriving after the run finished changed nothing, and is
	// not an error.
	//
	// This is the DURABLE half of cancel. The signal that follows it is the fast
	// path; this row is the evidence the loop re-derives from, which is what
	// stops a reaped agent pod from reading as agent death and buying a
	// re-dispatch. The FIRST request wins: a second click cannot move the stamp.
	RequestCancel(ctx context.Context, id string) (*MilestoneRun, error)

	// SetValidationVerdict records the judgement on the run (the verdict is a run
	// property, not a per-issue one) together with the validation task that produced
	// it, guarded on the run being non-terminal so a settled run's verdict is
	// frozen. An issue of 0 leaves the column as-is. Written by a validation run,
	// and by a dev run only to record `skipped` for a version with no oracle.
	SetValidationVerdict(ctx context.Context, id, verdict string, issue int) (*MilestoneRun, error)

	// GetByIDScoped returns the run only when it belongs to orgID — the store
	// fence for token-derived org. Returns (nil, nil) for both "no such id" and
	// "belongs to another org", so a cross-org read renders as 404, never 403.
	GetByIDScoped(ctx context.Context, orgID, id string) (*MilestoneRun, error)

	// ListByProject returns a project's runs, newest first — the version ledger
	// behind the builds read.
	ListByProject(ctx context.Context, orgID, projectID string) ([]MilestoneRun, error)

	// ListByMilestone returns the runs of ONE milestone, newest first: a
	// milestone sees sequential runs across its life (a dev run, then later task
	// and validation runs), and the builds detail read shows all of them.
	ListByMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) ([]MilestoneRun, error)

	// MilestoneNumberForTag resolves a `?tag=v<N>` query to a milestone number
	// through the run rows — never by title-matching against GitHub, whose
	// titles are renamable and whose title filters are case-insensitive. found
	// is false (with a nil error) when the project has no run for that tag.
	MilestoneNumberForTag(ctx context.Context, orgID, projectID, tag string) (number int, found bool, err error)

	// DeleteByProject purges a project's runs — the project-delete cascade.
	// Without it a recreated same-named project resurrects stale runs and
	// MilestoneNumberForTag resolves a tag to a milestone the fresh repo never
	// had. Org-scoped so a per-org project slug reused across orgs cannot
	// cross-delete.
	DeleteByProject(ctx context.Context, orgID, projectID string) error
}

type milestoneRunRepository struct{ db *gorm.DB }

// NewMilestoneRunRepository wires the gorm-backed repository.
func NewMilestoneRunRepository(db *gorm.DB) MilestoneRunRepository {
	return &milestoneRunRepository{db: db}
}

func (r *milestoneRunRepository) TryAdmit(ctx context.Context, run *MilestoneRun) (bool, *MilestoneRun, error) {
	// Validate the kind rather than trusting it: the mutex is a partial index
	// keyed on kind = 'dev', so a typo'd kind would not fail the insert — it
	// would silently escape the one-active-build-per-project invariant and land
	// a second agent on the branch.
	if !IsRunKind(run.Kind) {
		return false, nil, fmt.Errorf("milestone run: unknown kind %q", run.Kind)
	}
	// Origin is validated for the ordinary reason: it is a NOT NULL closed enum
	// the read model renders, so a value the reader rejects must never be
	// written.
	if !IsRunOrigin(run.Origin) {
		return false, nil, fmt.Errorf("milestone run: unknown origin %q", run.Origin)
	}
	if run.State == "" {
		run.State = RunStateWaiting
	}
	if IsTerminalRunState(run.State) {
		return false, nil, fmt.Errorf("milestone run: cannot admit in terminal state %q", run.State)
	}
	if run.CycleCeiling <= 0 {
		run.CycleCeiling = RunDefaultCycleCeiling
	}
	// ON CONFLICT DO NOTHING against the partial build mutex: the losing racer
	// inserts zero rows. No conflict target is named — a plain DO NOTHING
	// catches either partial unique violation, and the uuid PK never collides.
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(run)
	if res.Error != nil {
		return false, nil, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil, nil
	}
	return true, run, nil
}

func (r *milestoneRunRepository) ActiveDevRunByProject(ctx context.Context, orgID, projectID string) (*MilestoneRun, error) {
	return r.activeRunByKind(ctx, orgID, projectID, RunKindDev)
}

func (r *milestoneRunRepository) ActiveValidationRunByProject(ctx context.Context, orgID, projectID string) (*MilestoneRun, error) {
	return r.activeRunByKind(ctx, orgID, projectID, RunKindValidation)
}

// activeRunByKind is the one query behind both project-wide "is a run of this
// kind live?" reads. Written once because the two differ only in the kind
// literal, and a second spelling of the predicate is a second chance to narrow
// it differently from the index it mirrors.
func (r *milestoneRunRepository) activeRunByKind(ctx context.Context, orgID, projectID, kind string) (*MilestoneRun, error) {
	var row MilestoneRun
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND kind = ? AND state IN ?",
			orgID, projectID, kind, nonTerminalRunStates).
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

func (r *milestoneRunRepository) SetState(ctx context.Context, id, state string) (*MilestoneRun, error) {
	if state != RunStateWaiting && state != RunStateRunning {
		return nil, fmt.Errorf("milestone run: SetState takes a non-terminal state, got %q (use Settle)", state)
	}
	updates := map[string]any{"state": state}
	if state == RunStateRunning {
		// COALESCE so a re-entry into running keeps the ORIGINAL start stamp:
		// started_at marks when the run first did work, not the latest cycle.
		updates["started_at"] = gorm.Expr("COALESCE(started_at, ?)", time.Now().UTC())
	}
	return r.updateNonTerminal(ctx, id, updates)
}

func (r *milestoneRunRepository) Settle(ctx context.Context, id, state, terminalReason string) (*MilestoneRun, error) {
	if !IsTerminalRunState(state) {
		return nil, fmt.Errorf("milestone run: Settle takes a terminal state, got %q (use SetState)", state)
	}
	if state == RunStateSucceeded && terminalReason != "" {
		return nil, fmt.Errorf("milestone run: a succeeded run carries no terminal reason, got %q", terminalReason)
	}
	return r.updateNonTerminal(ctx, id, map[string]any{
		"state":           state,
		"terminal_reason": terminalReason,
		"ended_at":        time.Now().UTC(),
	})
}

func (r *milestoneRunRepository) BumpBudget(ctx context.Context, id string, counter RunBudget) (*MilestoneRun, error) {
	if !runBudgetColumns[counter] {
		return nil, fmt.Errorf("milestone run: unknown budget counter %q", counter)
	}
	// The column comes from the whitelist above, never from the caller's string.
	return r.updateNonTerminal(ctx, id, map[string]any{
		string(counter): gorm.Expr(string(counter) + " + 1"),
	})
}

func (r *milestoneRunRepository) RequestCancel(ctx context.Context, id string) (*MilestoneRun, error) {
	// COALESCE for the same reason SetState uses it on started_at: the column
	// records WHEN a person first asked, and a second click must not move it.
	return r.updateNonTerminal(ctx, id, map[string]any{
		"cancel_requested_at": gorm.Expr("COALESCE(cancel_requested_at, ?)", time.Now().UTC()),
	})
}

func (r *milestoneRunRepository) SetValidationVerdict(ctx context.Context, id, verdict string, issue int) (*MilestoneRun, error) {
	if !ValidationVerdicts[verdict] {
		return nil, fmt.Errorf("milestone run: unknown validation verdict %q", verdict)
	}
	// One write for both: the supervisor knows the verdict and the issue that
	// produced it at the same instant, and a settled run needs the issue to stay
	// navigable. Issue 0 (a task run, or a skip before minting) leaves the
	// column alone rather than overwriting a real number with zero.
	fields := map[string]any{"validation_verdict": verdict}
	if issue > 0 {
		fields["validation_issue"] = issue
	}
	return r.updateNonTerminal(ctx, id, fields)
}

func (r *milestoneRunRepository) GetByIDScoped(ctx context.Context, orgID, id string) (*MilestoneRun, error) {
	var row MilestoneRun
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

func (r *milestoneRunRepository) ListByProject(ctx context.Context, orgID, projectID string) ([]MilestoneRun, error) {
	var rows []MilestoneRun
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Order("created_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *milestoneRunRepository) ListByMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) ([]MilestoneRun, error) {
	var rows []MilestoneRun
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND milestone_number = ?", orgID, projectID, milestoneNumber).
		Order("created_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *milestoneRunRepository) MilestoneNumberForTag(ctx context.Context, orgID, projectID, tag string) (int, bool, error) {
	var row MilestoneRun
	// The predicate is SpecTag() in SQL: the tag column when it is set, the
	// title only for rows that predate it. Matching the title unconditionally
	// would resolve a phase-titled row by its PHASE name and answer 404 for the
	// version the console actually asked about.
	err := r.db.WithContext(ctx).
		Select("milestone_number").
		Where("org_id = ? AND project_id = ? AND (tag = ? OR (COALESCE(tag, '') = '' AND milestone_title = ?))",
			orgID, projectID, tag, tag).
		Order("created_at DESC").
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return row.MilestoneNumber, true, nil
}

func (r *milestoneRunRepository) DeleteByProject(ctx context.Context, orgID, projectID string) error {
	return r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Delete(&MilestoneRun{}).Error
}

// updateNonTerminal applies a guarded update to a run that has not settled and
// re-reads it. It is the ONE place the "terminal rows are never resurrected"
// fence is written, so every mutator inherits it — and the (nil, nil) no-op
// contract on RowsAffected == 0.
func (r *milestoneRunRepository) updateNonTerminal(ctx context.Context, id string, updates map[string]any) (*MilestoneRun, error) {
	res := r.db.WithContext(ctx).
		Model(&MilestoneRun{}).
		Where("id = ? AND state IN ?", id, nonTerminalRunStates).
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil
	}
	return r.getByID(ctx, id)
}

func (r *milestoneRunRepository) getByID(ctx context.Context, id string) (*MilestoneRun, error) {
	var row MilestoneRun
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}
