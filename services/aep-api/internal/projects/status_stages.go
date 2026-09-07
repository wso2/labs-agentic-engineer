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

package projects

// The #184 stage aggregates: one GET /projects/{name}/status cheap enough to
// poll at 5s serves the whole overview pipeline. Poll-path budget: no GitHub
// API, no Temporal query, no origin git fetch — the git source is the local
// mirror snapshot (spec.StatusSnapshot), the build source is one milestone_runs
// row read, the deploy source is one org-scoped OpenChoreo call. See
// internal/projects/README.md.

import (
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/gen"

	"golang.org/x/sync/errgroup"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Stage status vocabularies (the contract enums).
const (
	buildIdle      = "idle"
	buildRunning   = "running"
	buildFailed    = "failed"
	buildCancelled = "cancelled"
	buildSucceeded = "succeeded"

	deployNone      = "none"
	deployDeploying = "deploying"
	deployDeployed  = "deployed"
	deployFailed    = "failed"

	// Validation state (the deploy.validation contract enum). Only the three
	// LIFECYCLE values live here — the rest of the enum is the run's verdict
	// verbatim (delivery.ValidationVerdict*), because a fold would have to discard
	// `partial`, `inconclusive` and `unreported` at the one surface that needs
	// them, and `completed` never said whether anything passed.
	validationNone    = "none"
	validationRunning = "running"
	// validationAwaitingFix is a live run REPAIRING a failed validation: the run
	// holds a fatal verdict but has attempts left, and the work in flight is a
	// CODING cycle. It names the implementation rather than validation because that
	// is what is being fixed — rendering the bare `failed` verdict here would read
	// as terminal while the platform is actively resolving it.
	validationAwaitingFix = "awaiting-fix"
	// validationCancelled is a person STOPPING the judging: a validation run
	// settled cancelled before it recorded a verdict, so nothing will answer for
	// this version unless somebody re-asks. It is the one no-verdict state that
	// does NOT hold promotion, and the distinction is the whole reason it exists:
	// every other way to reach no verdict is an accident — a failed increment, an
	// agent that died — where refusing to promote an unjudged version is the safe
	// answer, and this one is a decision somebody already made.
	validationCancelled = "cancelled"
)

// milestoneRunRows is the narrow port over the milestone_runs index: the status
// read plus the project-delete purge. An app-root adapter over
// delivery.MilestoneRunRepository (which also purges the cycle records) is what
// satisfies it.
type milestoneRunRows interface {
	// ListByProject returns the project's runs newest-first.
	ListByProject(ctx context.Context, orgID, projectID string) ([]delivery.MilestoneRun, error)
	DeleteByProject(ctx context.Context, orgID, projectID string) error
	// LatestCycle returns the newest cycle record for a run, or (nil, nil) when it
	// has dispatched none. The validation chip needs it: loop position is never a
	// stored enum (a fix or conflict cycle re-enters an earlier phase), so "a
	// validation cycle is in flight" is knowable only from the latest cycle.
	LatestCycle(ctx context.Context, orgID, runID string) (*delivery.RunCycle, error)
}

// bindingsReader is the narrow status-read port over OpenChoreo release
// bindings. openchoreo.ComponentClient satisfies it.
type bindingsReader interface {
	ListProjectReleaseBindings(ctx context.Context, orgName, projectName string) ([]openchoreo.ReleaseBindingSummary, error)
}

// specTurnRows is the narrow status-read port over the agent_turns index: one
// indexed row read answering "is an agent working on this project's spec right
// now" (#562). spec.TurnRepository satisfies it.
//
// It is a FOURTH poll source, admitted against the #184 budget because it is a
// single-row read off a covering index (ix_agent_turns_project_newest) on a
// database the poll already talks to — the same cost class as the milestone-run
// read beside it, and an order cheaper than the OpenChoreo call. Nothing else
// can answer the question: exists,
// version and dirty all read committed git, and a kickoff writes nothing until
// it lands, so the busiest moment in the journey is precisely the one git has
// no record of.
type specTurnRows interface {
	Newest(ctx context.Context, orgID, projectID string) (*spec.AgentTurn, error)
	// NewestCompletedFlow finds the newest successful run of one flow — the
	// staleness check (#575) needs the last DESIGN run, so it can read the
	// requirements as that run saw them.
	NewestCompletedFlow(ctx context.Context, orgID, projectID, flow string) (*spec.AgentTurn, error)
}

// designFlow is the `/<skill>` token a design re-derivation runs under. Only a
// full re-derivation counts as reconciling the design with the requirements: a
// targeted edit to one document leaves the SET inconsistent, so clearing the
// staleness flag on one would drop the warning while the problem stood.
const designFlow = "design"

// designOutdated answers whether the requirements have moved since the design
// was last derived from them.
//
// nowFingerprint is the requirements as they stand; the baseline is the same
// reduction taken at the commit the newest successful design run read. No
// design run on record means there is nothing for the design to be behind —
// including the ordinary case of a project that has never designed at all.
//
// A baseline that cannot be read is reported as an ERROR rather than as
// "unchanged". The two failures are not symmetric: a spurious warning costs one
// re-derivation, while a swallowed one lets the coding agents implement a
// design the user has already changed their mind about.
func (s *Service) designOutdated(ctx context.Context, orgName, projectName, nowFingerprint string) (bool, error) {
	if s.specTurns == nil {
		return false, nil
	}
	lastDesign, err := s.specTurns.NewestCompletedFlow(ctx, orgName, projectName, designFlow)
	if err != nil {
		return false, fmt.Errorf("newest design turn: %w", err)
	}
	if lastDesign == nil || lastDesign.BaseRef == "" {
		return false, nil
	}
	was, err := s.artifactSvc.RequirementsFingerprintAt(ctx, orgName, projectName, lastDesign.BaseRef)
	if err != nil {
		return false, fmt.Errorf("requirements at the last design run's base: %w", err)
	}
	return was != nowFingerprint, nil
}

// SetStageSources wires the build/deploy stage inputs at the composition
// root. On a ready repo GetProjectStatus fails when either is missing — the
// stages are contract-required and never silently fabricated (D7).
func (s *Service) SetStageSources(runs milestoneRunRows, bindings bindingsReader) {
	s.runReader = runs
	s.bindingsReader = bindings
}

// SetSpecTurnSource wires the spec stage's agent-activity read (#562).
// Separate from SetStageSources because it is optional in a way those are not:
// a service without it serves spec.agent as "" — the same value a project with
// no turns gets — so the overview degrades to its pre-#562 reading rather than
// failing the poll.
func (s *Service) SetSpecTurnSource(turns specTurnRows) { s.specTurns = turns }

// Spec-stage agent activity (the spec.agent contract enum).
const (
	specAgentIdle         = ""
	specAgentWorking      = "working"
	specAgentFailed       = "failed"
	specAgentNeverStarted = "never-started"
)

// specAgentState folds the project's newest turn row into the contract enum.
//
// A COMPLETED turn reads as idle, not as "done": whatever it produced is in
// git, so exists/version/dirty already describe it, and a second vocabulary
// for the same fact would let the two disagree. Only the states git cannot see
// survive — a turn in flight, a turn that died leaving nothing behind, and no
// turn at all.
//
// NO ROW is its own state rather than more idle. The two look identical in git
// and need opposite handling: a project that has never run a turn needs a way
// to begin, while one merely between turns is mid-interview and must not be
// offered a restart that would supersede it. Collapsing them left a project
// whose dispatch never landed showing a spinner for work that was never
// coming, with nothing to click.
// specAgentOf guards the fold on the source being WIRED. Without it an
// unwired service would report `never-started` — a positive claim about turn
// history it has no way to make — where the documented degradation is the
// pre-#562 reading: "", meaning "this says nothing".
func specAgentOf(source specTurnRows, newest *spec.AgentTurn) string {
	if source == nil {
		return specAgentIdle
	}
	return specAgentState(newest)
}

// runningFlowOf reports WHICH work is in flight, for the spec rail to pulse the
// right section (#575).
//
// Only a RUNNING turn has one. A finished turn's flow says nothing about what is
// happening now, and reporting it would leave the rail pulsing whatever the last
// run happened to be long after it ended.
func runningFlowOf(newest *spec.AgentTurn) string {
	if newest == nil || newest.Status != spec.TurnStatusRunning {
		return ""
	}
	return newest.Flow
}

func specAgentState(newest *spec.AgentTurn) string {
	if newest == nil {
		return specAgentNeverStarted
	}
	switch newest.Status {
	case spec.TurnStatusRunning:
		return specAgentWorking
	case spec.TurnStatusFailed:
		return specAgentFailed
	default:
		return specAgentIdle
	}
}

// populateStages fills the three nested aggregates plus the flat artifact
// fields from four concurrently-read sources — strict join: any source
// failing fails the whole read (the console's poller keeps last-good data
// and retries; the endpoint never fabricates emptiness). The fourth, the
// newest agent turn, is skipped entirely when unwired rather than failing:
// its absence reads as "no agent working", which is what a project with no
// turns reports anyway.
func (s *Service) populateStages(ctx context.Context, orgName, projectName string, status *gen.ProjectStatus) error {
	if s.artifactSvc == nil || s.runReader == nil || s.bindingsReader == nil {
		return fmt.Errorf("project status: stage sources not wired")
	}

	var (
		snap        *spec.StatusSnapshot
		runs        []delivery.MilestoneRun
		bindings    []openchoreo.ReleaseBindingSummary
		newestTurn  *spec.AgentTurn
		deployVer   string
		deployTotal int64
	)
	g, gctx := errgroup.WithContext(ctx)
	if s.specTurns != nil {
		g.Go(func() error {
			var err error
			if newestTurn, err = s.specTurns.Newest(gctx, orgName, projectName); err != nil {
				return fmt.Errorf("newest agent turn: %w", err)
			}
			return nil
		})
	}
	g.Go(func() error {
		var err error
		if snap, err = s.artifactSvc.StatusSnapshot(gctx, orgName, projectName); err != nil {
			return fmt.Errorf("git snapshot: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		var err error
		if runs, err = s.runReader.ListByProject(gctx, orgName, projectName); err != nil {
			return fmt.Errorf("list milestone runs: %w", err)
		}
		// The deploy denominator depends only on the rows, so read it here —
		// overlapped with the OC call instead of serial after the join.
		// deploy.version = the newest SUCCEEDED DEV run's version: what the
		// platform last finished delivering (a running v2 does not unseat a
		// live v1).
		for i := range runs {
			if runs[i].Kind == delivery.RunKindDev && runs[i].State == delivery.RunStateSucceeded {
				// SpecTag, not the milestone title: the title is the milestone's
				// GitHub name and ComponentCountAtTag below resolves this as a GIT
				// TAG — a title that is not the tag reads as a vanished tag and
				// silently blanks the deploy denominator.
				deployVer = runs[i].SpecTag()
				break
			}
		}
		if deployVer == "" {
			return nil
		}
		total, err := s.artifactSvc.ComponentCountAtTag(gctx, orgName, projectName, deployVer)
		switch {
		case errors.Is(err, spec.ErrSpecTagNotFound):
			// A vanished tag (deleted on GitHub, or a stale run row from a
			// recreated project) is a DATA state, not a source outage — the
			// strict join is for outages. Degrade to an unknown denominator
			// instead of bricking every poll.
			return nil
		case err != nil:
			return fmt.Errorf("component count at %s: %w", deployVer, err)
		}
		deployTotal = int64(total)
		return nil
	})
	g.Go(func() error {
		var err error
		if bindings, err = s.bindingsReader.ListProjectReleaseBindings(gctx, orgName, projectName); err != nil {
			return fmt.Errorf("list release bindings: %w", err)
		}
		return nil
	})
	if err := g.Wait(); err != nil {
		return err
	}

	// Spec stage + flat artifact fields: one snapshot, same semantics as the
	// retired per-call reads — minus their per-poll origin fetches.
	// Staleness is checked only when a design exists — nothing can be behind
	// the requirements before it has been written, and this keeps the extra
	// tree read off every pre-design poll.
	outdated := false
	if snap.HasDesign {
		stale, err := s.designOutdated(ctx, orgName, projectName, snap.RequirementsFingerprint)
		if err != nil {
			return err
		}
		outdated = stale
	}
	status.Spec = gen.SpecStage{
		Exists:         snap.HasSpec,
		Version:        snap.SpecVersion,
		Dirty:          snap.SpecDirty,
		Design:         snap.HasDesign,
		Agent:          specAgentOf(s.specTurns, newestTurn),
		AgentFlow:      runningFlowOf(newestTurn),
		DesignOutdated: outdated,
	}
	applyFlatArtifactFields(status, snap)

	// Build stage: the newest DEV RUN (ListByProject is newest-first).
	//
	// Dev runs specifically, not simply the newest row, because only a dev run
	// advances the project's version — "what version is this project on" has
	// always meant the last one built. The other kinds work an EXISTING
	// milestone, which may be any version's: a bug adopted into v3 while the
	// project is on v5, or a revalidation of v3, would otherwise walk the overview
	// backwards and report the project as being on v3.
	//
	// The task tally is deliberately ZERO. Its only honest source is the
	// version's milestone on GitHub, and this endpoint is polled at 5s — a
	// per-poll GitHub read is exactly what the #184 budget forbids. The console
	// renders per-version counts from the list-tasks response it already holds.
	latest := newestByKind(runs, delivery.RunKindDev)
	if latest != nil {
		status.Build.Version = latest.SpecTag()
		status.Build.Status = buildStageStatus(latest.State)
		// A VALIDATING-phase failure is not a build failure: every coding cycle
		// landed and the failure already rides deploy.validation below. Without this
		// the overview says "build failed" while the validation chip contradicts it.
		// Covers every reason the phase can settle under, not just a red suite — an
		// unreported run is equally not a build failure.
		if delivery.IsValidationTerminalReason(latest.TerminalReason) {
			status.Build.Status = buildSucceeded
		}
	}

	// Deploy stage: version + denominator computed alongside the row read
	// above (the design at the DEPLOYED tag — what that build actually
	// implemented; HEAD may hold newer spec edits no build has seen); status
	// + numerator from the dev-environment bindings. ready and total come
	// from independent sources, so ready > total is possible transiently
	// (component removed from the design between builds) — informational
	// counts, deliberately not reconciled.
	status.Deploy.Version = deployVer
	status.Deploy.Components.Total = deployTotal
	var dev []openchoreo.ReleaseBindingSummary
	for _, b := range bindings {
		if b.Environment == openchoreo.DevEnvironmentName && !b.Undeploy {
			dev = append(dev, b)
		}
	}
	status.Deploy.Status = deployStageStatus(dev)
	status.Deploy.Components.Ready = int64(countReady(dev))

	// Validation: the newest verdict for the version the build stage just named —
	// a column on a row already read above, so the poll costs nothing extra.
	//
	// Scoped to that MILESTONE rather than to the dev run, because a version
	// can be judged more than once: a revalidation is a later run on the same
	// milestone and its verdict is the version's current answer. Scoped to the
	// milestone rather than to the whole project for the reason above — a run on an
	// older version must not answer for this one.
	//
	// The report itself, and the per-cycle detail behind it, live on the version's
	// run story (list-build-runs), which is where the console's validation surface
	// reads them; validationUrl/validationIssue are therefore no longer served here.
	state, err := s.validationStage(ctx, orgName, newestValidatingOnMilestone(runs, latest))
	if err != nil {
		return err
	}
	status.Deploy.Validation = gen.DeployStageValidation(state)
	return nil
}

// validationStage resolves the deploy.validation value, reading the run's latest
// cycle ONLY when the verdict alone cannot answer.
//
// That conditional read is the whole point of the split: a verdict that is final is
// the answer, and a settled run without one either had its judging cancelled or never
// reached validation — all decided from the row already in hand. The extra query
// happens for the two cases the row cannot settle: a live run with no verdict yet (the
// case the old code got wrong by calling every such run "validating"), and a live run
// holding a REPAIRABLE verdict, which is mid-loop rather than finished.
func (s *Service) validationStage(ctx context.Context, orgID string, run *delivery.MilestoneRun) (string, error) {
	state, decided := validationStageFromRun(run)
	if decided {
		return state, nil
	}
	cycle, err := s.runReader.LatestCycle(ctx, orgID, run.ID)
	if err != nil {
		return "", fmt.Errorf("latest cycle for run %s: %w", run.ID, err)
	}
	if cycle != nil && cycle.Kind == delivery.CycleKindValidation && cycle.EndedAt == nil {
		return validationRunning, nil
	}
	// A live run carrying a repairable verdict, whose current cycle is ordinary
	// work: this is the self-healing loop mid-flight. The verdict is real but not
	// final, and the cycle in flight is what will make it stale.
	if _, fatal := delivery.ValidationVerdictFailsRun(run.ValidationVerdict); fatal {
		return validationAwaitingFix, nil
	}
	// A live run whose current cycle is coding, fixing or resolving a conflict has
	// nothing to say about validation yet. Saying "validating" here was wrong for
	// most of every run's life.
	return validationNone, nil
}

// newestByKind returns the newest run of one kind, or nil. rows must be
// newest-first, which is what ListByProject guarantees — so this is a scan, not a
// sort, and costs nothing on a slice the caller already holds.
func newestByKind(rows []delivery.MilestoneRun, kind string) *delivery.MilestoneRun {
	for i := range rows {
		if rows[i].Kind == kind {
			return &rows[i]
		}
	}
	return nil
}

// newestValidatingOnMilestone returns the newest run on ref's milestone that could
// have produced a verdict — which is ref itself unless something later re-judged
// that version.
//
// It exists because a version's answer and the version's BUILD can come from
// different rows: the dev run delivers it, and a revalidation started afterwards
// may hold a newer verdict for the very same milestone.
//
// The KIND filter is the load-bearing half, and it predates revalidation. A task
// run never validates, and `settle` stamps `skipped` on any succeeded run that
// never did — so the newest run on a milestone is routinely one whose verdict
// means "I was never asked". Returning it made a single adopted issue report a
// genuinely passed version as unvalidated. RunValidates is delivery's own answer
// to which kinds ask the question, so this cannot drift from the loop.
//
// Keyed on the milestone number, which is the platform key; nil ref means there is
// no version to answer about.
func newestValidatingOnMilestone(rows []delivery.MilestoneRun, ref *delivery.MilestoneRun) *delivery.MilestoneRun {
	if ref == nil {
		return nil
	}
	for i := range rows {
		if rows[i].MilestoneNumber == ref.MilestoneNumber && delivery.RunValidates(rows[i].Kind) {
			return &rows[i]
		}
	}
	return ref
}

// validationStageFromRun answers deploy.validation from the run row alone,
// reporting decided=false when only the run's latest cycle can settle it.
//
// The verdict is MIRRORED, not folded: it is the thing a reader wants to know, and
// folding it into a coarser word is how "completed" came to mean "passed" without
// saying so — it would discard partial, inconclusive and unreported entirely.
//
// A TERMINAL run with no verdict splits in two, and the split is the difference
// between a state that resolves and one that never will: judging that was CANCELLED
// is settled (`cancelled`), and everything else is a run that simply never got there
// (`none`, which promises a verdict is still coming).
//
// A verdict is final on a TERMINAL run, and on a live run when it is not one the
// loop repairs. A live run holding a repairable verdict is undecided: the verdict is
// mid-loop, so rendering it would tell a reader the version failed validation while
// the platform is repairing it and about to validate again. Which verdicts the loop
// repairs is delivery's to say (ValidationVerdictFailsRun), so this cannot drift
// from what the supervisor actually does.
//
// Undecided therefore means: a live run with no verdict yet, or one whose verdict is
// still repairable. Whether a validation cycle is in flight is not on this row —
// loop position is never a stored enum, because a fix or conflict cycle re-enters an
// earlier phase and a flat enum would lie mid-loop.
func validationStageFromRun(run *delivery.MilestoneRun) (state string, decided bool) {
	if run == nil {
		return validationNone, true
	}
	if delivery.IsTerminalRunState(run.State) {
		if run.ValidationVerdict != "" {
			return run.ValidationVerdict, true
		}
		// A cancelled VALIDATION run: somebody stopped the judging, so no verdict is
		// coming for this version and `none` — which promises one — would be a lie
		// that never resolves.
		//
		// The KIND guard is load-bearing rather than defensive. This function is
		// handed whatever newestValidatingOnMilestone returns, which is a
		// validation-kind run OR a fall-back to the dev run, so testing the state
		// alone would also catch a cancelled DEV run. That is an ABANDONED INCREMENT
		// — the reconcile sweep suppresses its whole milestone for exactly that
		// reason — and reporting it as "nothing left to wait for" would offer the
		// version for promotion, which is worse than the confusion this state exists
		// to remove. RunValidates is delivery's own answer to which kinds ask the
		// question, and the selector above already keys on it, so the two cannot
		// drift onto different ideas of what validates.
		if run.State == delivery.RunStateCancelled && delivery.RunValidates(run.Kind) {
			return validationCancelled, true
		}
		// Settled without ever recording a verdict: the run never reached validation.
		return validationNone, true
	}
	if run.ValidationVerdict != "" {
		if _, fatal := delivery.ValidationVerdictFailsRun(run.ValidationVerdict); !fatal {
			return run.ValidationVerdict, true
		}
	}
	return "", false
}

// applyFlatArtifactFields recomputes the pre-#184 flat fields from the
// snapshot, outputs identical to the retired per-call reads: hasSpec from
// the requirements listing; specStatus approved on any v<N> tag, draft on an
// unversioned spec; designStatus approved on any legacy v<N>-<M> tag;
// hasDesign only ever true when a spec exists (the old ladder returned at
// "prompt" before reading the design); the phase ladder unchanged. One
// accepted deviation: a design.cell with malformed frontmatter counts as
// present here, where the old ReadDesign failed the whole status read — see
// spec.StatusSnapshot.HasDesign. HasTasks stays false — tasks are
// counted live from GitHub, never here.
func applyFlatArtifactFields(status *gen.ProjectStatus, snap *spec.StatusSnapshot) {
	status.HasSpec = snap.HasSpec
	switch {
	case snap.SpecVersion != "":
		status.SpecStatus = "approved"
	case snap.HasSpec:
		status.SpecStatus = "draft"
	}
	if snap.HasDesignTag {
		status.DesignStatus = "approved"
	}
	if !snap.HasSpec {
		status.Phase = "prompt"
		return
	}
	status.HasDesign = snap.HasDesign
	if !snap.HasDesign {
		status.Phase = "spec"
		return
	}
	status.Phase = "tasks"
}

// buildStageStatus maps a milestone run's state onto the BuildStage enum. A
// version's delivery IS its run: it is running while the run is (waiting between
// cycles included — the version is still being delivered), and it settles when
// the run does.
//
// CANCELLED IS ITS OWN VALUE, matching build.statusFromRunState. The two
// aggregates are read by different surfaces off the same run row — this one
// drives the project badge in the toolbar, that one the version ledger — so a
// difference between them is a self-contradiction a reader sees in one glance.
// It was one: the build page header said Cancelled while the toolbar two
// centimetres away said "Build failed".
//
// A BLOCKED run still reads as failed. The version did not ship and a human has
// to supply something before it can, which is what "failed, see the reason" says;
// nobody chose it, and that is the whole difference from a cancel.
func buildStageStatus(state string) string {
	switch state {
	case delivery.RunStateSucceeded:
		return buildSucceeded
	case delivery.RunStateCancelled:
		return buildCancelled
	case delivery.RunStateFailed, delivery.RunStateBlocked:
		return buildFailed
	default: // waiting | running | planning
		return buildRunning
	}
}

// bindingFailureReasons is the aggregate Ready condition's terminal-failure
// vocabulary (OpenChoreo v1.1.1 releasebinding controller). Anything else
// not-ready reads as still-progressing — the forgiving default: an unknown
// or missing reason renders "deploying", never a false "failed".
var bindingFailureReasons = map[string]bool{
	"RenderingFailed":             true,
	"ResourceApplyFailed":         true,
	"ResourcesDegraded":           true,
	"ResourcesNotReady":           true,
	"JobFailed":                   true,
	"InvalidReleaseConfiguration": true,
	"ReleaseUpdateFailed":         true,
	"ReleaseOwnershipConflict":    true,
	"ComponentReleaseNotFound":    true,
	"EnvironmentNotFound":         true,
	"DataPlaneNotFound":           true,
	"DataPlaneNotConfigured":      true,
	"ComponentNotFound":           true,
	"ProjectNotFound":             true,
}

func bindingReady(b openchoreo.ReleaseBindingSummary) bool { return b.ReadyStatus == "True" }

func bindingFailed(b openchoreo.ReleaseBindingSummary) bool {
	return !bindingReady(b) && bindingFailureReasons[b.ReadyReason]
}

// deployStageStatus derives the stage from binding conditions alone,
// precedence failed > deploying > deployed; no bindings → none. The design
// denominator never gates the status (a designed-but-never-built component
// shows "deployed · 2/3", not forever-"deploying").
func deployStageStatus(dev []openchoreo.ReleaseBindingSummary) string {
	if len(dev) == 0 {
		return deployNone
	}
	progressing := false
	for _, b := range dev {
		if bindingFailed(b) {
			return deployFailed
		}
		if !bindingReady(b) {
			progressing = true
		}
	}
	if progressing {
		return deployDeploying
	}
	return deployDeployed
}

func countReady(dev []openchoreo.ReleaseBindingSummary) int {
	n := 0
	for _, b := range dev {
		if bindingReady(b) {
			n++
		}
	}
	return n
}
