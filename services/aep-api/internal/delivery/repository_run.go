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
	// RunBudgetValidationCycles counts VALIDATION ATTEMPTS, bounding the
	// repair-and-re-validate loop (RunMaxValidationAttempts). Unlike the other
	// three it names no terminal reason of its own: exhausting it settles the run
	// on the verdict the last attempt already produced, so `validation-failed`
	// means "still failing after every attempt".
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
	// TryAdmit is the spec-run mutex in DB form (the §5 409's twin): it INSERTs
	// the run unless a non-terminal spec-build run already exists for the same
	// (org, project), via INSERT … ON CONFLICT DO NOTHING against the partial
	// unique index. It returns admitted=true with the populated row on success,
	// or admitted=false and nil when another entrant won the race.
	//
	// Incident-adoption runs sit outside the index and are always admitted —
	// they execute concurrently on their own milestones. The row's State
	// defaults to waiting and CycleCeiling to RunDefaultCycleCeiling when unset.
	TryAdmit(ctx context.Context, run *MilestoneRun) (admitted bool, row *MilestoneRun, err error)

	// ActiveSpecRunByProject returns the project's live spec-build run — the
	// lookup behind the build endpoint's 409 — or (nil, nil) when the project is
	// free. The DB index is the authority; this read exists so the API can
	// answer with a useful conflict instead of a bare insert failure.
	ActiveSpecRunByProject(ctx context.Context, orgID, projectID string) (*MilestoneRun, error)

	// ActiveRunByProject is ActiveSpecRunByProject without the origin filter:
	// the project's newest non-terminal run whatever started it.
	//
	// A sibling rather than a widening of the one above, because the two answer
	// different questions. That one backs the build endpoint's 409 and means
	// "is the spec mutex held" — broadening it would start refusing builds
	// because an unrelated incident run is live. This one backs the deploy
	// gate's wake-up and means "is there a run that might be parked on this",
	// and an incident-adoption or revalidate run parks on the gate exactly like
	// a spec run does. Using the spec-only lookup here would leave those two
	// parked until their poll interval, forever if a signal were the only wake.
	ActiveRunByProject(ctx context.Context, orgID, projectID string) (*MilestoneRun, error)

	// SetState moves a non-terminal run between waiting and running (the loop
	// oscillates across cycle boundaries) and stamps started_at on the first
	// transition to running. It refuses a terminal state — that is Settle's job
	// — and returns (nil, nil) when the run is already terminal.
	SetState(ctx context.Context, id, state string) (*MilestoneRun, error)

	// SetWaiting parks a non-terminal run WITH the reason it is parked and the
	// dependencies it is parked on — one write, so the console never reads a
	// `waiting` row whose explanation has not landed yet. Returns (nil, nil) when
	// the run is already terminal, like SetState.
	SetWaiting(ctx context.Context, id, reason string, dependencies []string) (*MilestoneRun, error)

	// Settle ends a run: it writes a terminal state plus its terminal reason and
	// stamps ended_at, guarded on the run still being non-terminal so the first
	// settle wins and no later signal can overwrite a recorded outcome. reason
	// must be empty for succeeded and one of the RunReason* values otherwise.
	Settle(ctx context.Context, id, state, terminalReason string) (*MilestoneRun, error)

	// BumpBudget increments one budget counter by one, guarded on the run being
	// non-terminal. The supervisor compares the returned row against the
	// matching Run* limit to decide whether the budget is now exhausted.
	BumpBudget(ctx context.Context, id string, counter RunBudget) (*MilestoneRun, error)

	// SetValidationVerdict records the validation cycle's outcome on the run
	// (the verdict is a run property, not a per-issue one) together with the
	// validation issue that produced it, guarded on the run being non-terminal so
	// a settled run's verdict is frozen. An issue of 0 leaves the column as-is.
	SetValidationVerdict(ctx context.Context, id, verdict string, issue int) (*MilestoneRun, error)

	// GetByIDScoped returns the run only when it belongs to orgID — the store
	// fence for token-derived org. Returns (nil, nil) for both "no such id" and
	// "belongs to another org", so a cross-org read renders as 404, never 403.
	GetByIDScoped(ctx context.Context, orgID, id string) (*MilestoneRun, error)

	// ListByProject returns a project's runs, newest first — the version ledger
	// behind the builds read.
	ListByProject(ctx context.Context, orgID, projectID string) ([]MilestoneRun, error)

	// ListByMilestone returns the runs of ONE milestone, newest first: a
	// milestone sees sequential runs across its life (a spec build, then later
	// incident adoptions), and the builds detail read shows all of them.
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
	// Validate the origin rather than trusting it: the mutex is a partial index
	// keyed on origin = 'spec-build', so a typo'd origin would silently escape
	// the one-active-spec-run-per-project invariant.
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
	// ON CONFLICT DO NOTHING against the partial spec-run mutex: the losing
	// racer inserts zero rows. No conflict target is named — a plain DO NOTHING
	// catches the partial unique violation, and the uuid PK never collides.
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(run)
	if res.Error != nil {
		return false, nil, res.Error
	}
	if res.RowsAffected == 0 {
		return false, nil, nil
	}
	return true, run, nil
}

func (r *milestoneRunRepository) ActiveSpecRunByProject(ctx context.Context, orgID, projectID string) (*MilestoneRun, error) {
	var row MilestoneRun
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND origin = ? AND state IN ?",
			orgID, projectID, RunOriginSpecBuild, nonTerminalRunStates).
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

func (r *milestoneRunRepository) ActiveRunByProject(ctx context.Context, orgID, projectID string) (*MilestoneRun, error) {
	var row MilestoneRun
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND state IN ?", orgID, projectID, nonTerminalRunStates).
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
		// Leaving a stale reason on a running run would have the console showing
		// "waiting for your credentials" over a run that is already deploying.
		updates["waiting_reason"] = ""
		updates["blocking_dependencies"] = DependencyNames(nil)
	}
	return r.updateNonTerminal(ctx, id, updates)
}

// SetWaiting parks the run WITH its explanation, in one write. Separate from
// SetState because only the deploy gate has an explanation to give: every other
// park is between cycles and self-evident from the phase.
func (r *milestoneRunRepository) SetWaiting(ctx context.Context, id, reason string, dependencies []string) (*MilestoneRun, error) {
	return r.updateNonTerminal(ctx, id, map[string]any{
		"state":                 RunStateWaiting,
		"waiting_reason":        reason,
		"blocking_dependencies": DependencyNames(dependencies),
	})
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

func (r *milestoneRunRepository) SetValidationVerdict(ctx context.Context, id, verdict string, issue int) (*MilestoneRun, error) {
	if !ValidationVerdicts[verdict] {
		return nil, fmt.Errorf("milestone run: unknown validation verdict %q", verdict)
	}
	// One write for both: the supervisor knows the verdict and the issue that
	// produced it at the same instant, and a settled run needs the issue to stay
	// navigable. Issue 0 (an incident run, or a skip before minting) leaves the
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
