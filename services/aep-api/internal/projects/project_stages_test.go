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

// The #184 stage-aggregate derivation table, pinned row by row against fake
// poll sources. The fixture lives in project_service_test.go.
package projects

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/spec"
)

func devBinding(name, readyStatus, readyReason string) openchoreo.ReleaseBindingSummary {
	return openchoreo.ReleaseBindingSummary{
		ComponentName: name,
		Environment:   "development",
		ReadyStatus:   readyStatus,
		ReadyReason:   readyReason,
	}
}

func mustStatus(t *testing.T, fx statusFixture) *gen.ProjectStatus {
	t.Helper()
	st, err := fx.service().GetProjectStatus(context.Background(), "acme", "web")
	if err != nil {
		t.Fatalf("GetProjectStatus: %v", err)
	}
	return st
}

// devRun builds a dev milestone run for the version `tag` in `state`. A
// version's delivery IS its run, so these rows are the whole build stage.
func devRun(tag, state string) delivery.MilestoneRun {
	return delivery.MilestoneRun{
		MilestoneNumber: len(tag),
		MilestoneTitle:  tag,
		Kind:            delivery.RunKindDev,
		Origin:          delivery.RunOriginSpecBuild,
		State:           state,
	}
}

// TestStageDerivation_FullPipeline drives all three aggregates at once: a v2
// run in flight over a deployed v1 — the mid-flight overview.
func TestStageDerivation_FullPipeline(t *testing.T) {
	t.Parallel()
	fx := statusFixture{
		snap: spec.StatusSnapshot{
			HasSpec:     true,
			HasDesign:   true,
			SpecVersion: "v2",
			SpecDirty:   true,
		},
		runs: []delivery.MilestoneRun{
			devRun("v2", delivery.RunStateRunning),
			devRun("v1", delivery.RunStateSucceeded),
		},
		bindings: []openchoreo.ReleaseBindingSummary{
			devBinding("api", "True", "Ready"),
			devBinding("web", "False", "ResourcesProgressing"),
			{ComponentName: "api", Environment: "production", ReadyStatus: "True", ReadyReason: "Ready"}, // ignored: not dev
		},
		counts: map[string]int{"v1": 3},
	}
	st := mustStatus(t, fx)

	if want := (gen.SpecStage{Exists: true, Version: "v2", Dirty: true, Design: true}); st.Spec != want {
		t.Errorf("spec = %+v, want %+v", st.Spec, want)
	}

	if st.Build.Version != "v2" || st.Build.Status != "running" {
		t.Errorf("build = %s/%s, want v2/running", st.Build.Version, st.Build.Status)
	}
	// There is no task tally on this aggregate at all: its only honest source is
	// GitHub, and this endpoint is polled at 5s. The console renders counts from
	// the list-tasks response it already holds.

	if st.Deploy.Version != "v1" {
		t.Errorf("deploy version = %q, want v1 (newest SUCCEEDED run, not the running v2)", st.Deploy.Version)
	}
	if st.Deploy.Status != "deploying" {
		t.Errorf("deploy status = %q, want deploying (one dev binding not ready)", st.Deploy.Status)
	}
	if st.Deploy.Components.Total != 3 || st.Deploy.Components.Ready != 1 {
		t.Errorf("deploy components = %+v, want 3 total (design at v1) / 1 ready", st.Deploy.Components)
	}
}

// TestBuildStage_IgnoresRunsOnOlderVersions.
//
// The rows arrive newest-first across the WHOLE project, and only a spec build
// advances the version — the other origins work an existing milestone, which may
// be any version's. A revalidation of v1, or an incident adopted into it, is
// newer than the v3 build that shipped, so picking the newest row outright walks
// the overview backwards and reports the project as being on v1.
func TestBuildStage_IgnoresRunsOnOlderVersions(t *testing.T) {
	t.Parallel()
	old := devRun("v1", delivery.RunStateSucceeded)
	// A revalidation of the OLD version, started after the newer build shipped.
	revalidate := devRun("v1", delivery.RunStateRunning)
	revalidate.Kind, revalidate.Origin = delivery.RunKindValidation, delivery.RunOriginRevalidate

	st := mustStatus(t, statusFixture{
		runs:   []delivery.MilestoneRun{revalidate, devRun("v3", delivery.RunStateSucceeded), old},
		counts: map[string]int{"v3": 4},
	})
	if st.Build.Version != "v3" {
		t.Errorf("build version = %q, want v3 — a run on an older milestone must not move it", st.Build.Version)
	}
	if st.Build.Status != "succeeded" {
		t.Errorf("build status = %q, want succeeded — the revalidation's state is not the build's", st.Build.Status)
	}
}

// TestValidationStage_IncidentRunDoesNotEraseTheVerdict.
//
// This one needs no revalidation to reproduce, which is the point: an incident
// adoption gets no validation cycle, and `settle` stamps `skipped` on a succeeded
// run that never validated. So the newest run on a milestone routinely carries a
// verdict meaning "I was never asked" — and reading it reported a version that had
// genuinely PASSED as unvalidated. One adopted issue was enough.
func TestValidationStage_IncidentRunDoesNotEraseTheVerdict(t *testing.T) {
	t.Parallel()
	build := devRun("v3", delivery.RunStateSucceeded)
	build.ValidationVerdict = delivery.ValidationVerdictPassed
	// An issue adopted into the delivered version's milestone afterwards. It never
	// validates, so its own verdict is `skipped`.
	incident := devRun("v3", delivery.RunStateSucceeded)
	incident.Kind, incident.Origin = delivery.RunKindTask, delivery.RunOriginIncidentAdoption
	incident.ValidationVerdict = delivery.ValidationVerdictSkipped

	st := mustStatus(t, statusFixture{
		runs:   []delivery.MilestoneRun{incident, build},
		counts: map[string]int{"v3": 4},
	})
	if got := string(st.Deploy.Validation); got != delivery.ValidationVerdictPassed {
		t.Errorf("validation = %q, want passed — an incident run never asked, so it cannot answer", got)
	}
}

// TestValidationStage_FollowsTheNewestRunOnTheVersion is the other half: a
// version CAN be re-judged, and the answer is then the later run's.
//
// So the chip is scoped to the milestone the build stage named — not to the
// dev row (which would pin the verdict to the first judgement forever) and
// not to the project (which would let an older version answer for this one).
func TestValidationStage_FollowsTheNewestRunOnTheVersion(t *testing.T) {
	t.Parallel()
	build := devRun("v3", delivery.RunStateSucceeded)
	build.ValidationVerdict = delivery.ValidationVerdictFailed
	build.TerminalReason = delivery.RunReasonValidationFailed
	// A revalidation of the SAME version that has since passed.
	revalidate := devRun("v3", delivery.RunStateSucceeded)
	revalidate.Kind, revalidate.Origin = delivery.RunKindValidation, delivery.RunOriginRevalidate
	revalidate.ValidationVerdict = delivery.ValidationVerdictPassed

	st := mustStatus(t, statusFixture{
		runs:   []delivery.MilestoneRun{revalidate, build},
		counts: map[string]int{"v3": 4},
	})
	if got := string(st.Deploy.Validation); got != delivery.ValidationVerdictPassed {
		t.Errorf("validation = %q, want passed — the newest judgement of this version is its answer", got)
	}
	if st.Build.Version != "v3" {
		t.Errorf("build version = %q, want v3", st.Build.Version)
	}
}

// TestBuildStage_RunStateMapping pins the run-state → BuildStage enum table. A
// version is "running" for as long as its run is live — waiting between cycles
// included, because the version is still being delivered.
func TestBuildStage_RunStateMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		runs       []delivery.MilestoneRun
		wantVer    string
		wantStatus string
	}{
		{name: "no rows → idle", wantStatus: "idle"},
		{name: "succeeded", runs: []delivery.MilestoneRun{devRun("v3", delivery.RunStateSucceeded)}, wantVer: "v3", wantStatus: "succeeded"},
		{name: "failed", runs: []delivery.MilestoneRun{devRun("v3", delivery.RunStateFailed)}, wantVer: "v3", wantStatus: "failed"},
		// A cancel is its OWN value, not a flavour of failed: a person abandoning an
		// increment is a different fact from the platform failing to deliver one, and
		// the badge read "Build failed" over a build somebody had deliberately
		// stopped. It has to agree with the version ledger's own mapping
		// (build.statusFromRunState) or the two surfaces contradict each other.
		{name: "cancelled is its own status", runs: []delivery.MilestoneRun{devRun("v3", delivery.RunStateCancelled)}, wantVer: "v3", wantStatus: "cancelled"},
		{name: "running", runs: []delivery.MilestoneRun{devRun("v3", delivery.RunStateRunning)}, wantVer: "v3", wantStatus: "running"},
		{name: "waiting between cycles is still running", runs: []delivery.MilestoneRun{devRun("v3", delivery.RunStateWaiting)}, wantVer: "v3", wantStatus: "running"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A succeeded run becomes deploy.version → the denominator read
			// runs; give it a count so this test stays about the build row.
			st := mustStatus(t, statusFixture{runs: tc.runs, counts: map[string]int{"v3": 4}})
			if st.Build.Version != tc.wantVer || st.Build.Status != tc.wantStatus {
				t.Errorf("build = %s/%s, want %s/%s", st.Build.Version, st.Build.Status, tc.wantVer, tc.wantStatus)
			}
		})
	}
}

// A run that failed BECAUSE its validation cycle failed reports build=succeeded:
// every coding cycle landed, and the failure already rides
// deploy.validation=failed. The carve-out keys on the run's own terminal
// reason, which names exactly one failure class — so no tally guard or recency
// heuristic is needed any more.
func TestBuildStage_ValidationFailureAttribution(t *testing.T) {
	t.Parallel()
	failedForVerdict := func(reason, verdict string) []delivery.MilestoneRun {
		run := devRun("v1", delivery.RunStateFailed)
		run.TerminalReason = reason
		run.ValidationVerdict = verdict
		return []delivery.MilestoneRun{run}
	}
	failedFor := func(reason string) []delivery.MilestoneRun {
		return failedForVerdict(reason, delivery.ValidationVerdictFailed)
	}
	cases := []struct {
		name           string
		runs           []delivery.MilestoneRun
		wantBuild      string
		wantValidation string
	}{
		{
			name:      "validation-attributed failure → build succeeded",
			runs:      failedFor(delivery.RunReasonValidationFailed),
			wantBuild: "succeeded", wantValidation: "failed",
		},
		{
			// The phase can settle a run for two reasons, and BOTH are carved out:
			// an unreported run is no more a build failure than a red suite is.
			name:      "an unreported run is also not a build failure",
			runs:      failedForVerdict(delivery.RunReasonValidationUnreported, delivery.ValidationVerdictUnreported),
			wantBuild: "succeeded", wantValidation: "unreported",
		},
		{
			name:      "a budget failure is the build's own",
			runs:      failedFor(delivery.RunReasonRedispatchBudget),
			wantBuild: "failed", wantValidation: "failed",
		},
		{
			name:      "a plan failure is the build's own",
			runs:      failedFor(delivery.RunReasonPlanFailed),
			wantBuild: "failed", wantValidation: "failed",
		},
		{
			// Still never carved out — the carve-out is about a validation cycle's
			// failure being attributed to validation rather than to the build, and a
			// cancel has no verdict either way. What changed is only the word: the
			// build row says `cancelled`, not `failed`.
			name:      "a cancelled run is never carved out",
			runs:      []delivery.MilestoneRun{devRun("v1", delivery.RunStateCancelled)},
			wantBuild: "cancelled", wantValidation: "none",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := mustStatus(t, statusFixture{runs: tc.runs, counts: map[string]int{"v1": 1}})
			if st.Build.Status != tc.wantBuild {
				t.Errorf("build status = %q, want %q", st.Build.Status, tc.wantBuild)
			}
			if string(st.Deploy.Validation) != tc.wantValidation {
				t.Errorf("deploy.validation = %q, want %q", st.Deploy.Validation, tc.wantValidation)
			}
		})
	}
}

// TestDeployStage_ConditionMatrix pins the condition-driven status: failed >
// deploying > deployed, none without bindings; undeploy-state and non-dev
// bindings excluded; unknown reasons read as deploying, never failed.
func TestDeployStage_ConditionMatrix(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		bindings   []openchoreo.ReleaseBindingSummary
		wantStatus string
		wantReady  int64
	}{
		{name: "no bindings → none", wantStatus: "none"},
		{
			name: "all ready → deployed",
			bindings: []openchoreo.ReleaseBindingSummary{
				devBinding("api", "True", "Ready"),
				devBinding("web", "True", "Ready"),
			},
			wantStatus: "deployed",
			wantReady:  2,
		},
		{
			name: "any failure reason wins over progress",
			bindings: []openchoreo.ReleaseBindingSummary{
				devBinding("api", "True", "Ready"),
				devBinding("web", "False", "ResourcesProgressing"),
				devBinding("db", "False", "ResourceApplyFailed"),
			},
			wantStatus: "failed",
			wantReady:  1,
		},
		{
			name: "unknown not-ready reason → deploying (forgiving default)",
			bindings: []openchoreo.ReleaseBindingSummary{
				devBinding("api", "False", "SomeNewReason"),
			},
			wantStatus: "deploying",
		},
		{
			name: "absent Ready condition → deploying",
			bindings: []openchoreo.ReleaseBindingSummary{
				devBinding("api", "", ""),
			},
			wantStatus: "deploying",
		},
		{
			name: "undeploy-state binding excluded from status and counts",
			bindings: []openchoreo.ReleaseBindingSummary{
				devBinding("api", "True", "Ready"),
				{ComponentName: "web", Environment: "development", Undeploy: true, ReadyStatus: "False", ReadyReason: "ResourcesUndeployed"},
			},
			wantStatus: "deployed",
			wantReady:  1,
		},
		{
			name: "only non-dev bindings → none",
			bindings: []openchoreo.ReleaseBindingSummary{
				{ComponentName: "api", Environment: "production", ReadyStatus: "True", ReadyReason: "Ready"},
			},
			wantStatus: "none",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := mustStatus(t, statusFixture{bindings: tc.bindings})
			if st.Deploy.Status != tc.wantStatus {
				t.Errorf("deploy status = %q, want %q", st.Deploy.Status, tc.wantStatus)
			}
			if st.Deploy.Components.Ready != tc.wantReady {
				t.Errorf("ready = %d, want %d", st.Deploy.Components.Ready, tc.wantReady)
			}
		})
	}
}

// TestDeployStage_VanishedTagDegrades: a deploy tag missing from the local
// mirror (deleted on GitHub, or a stale run row) is a data state, not a
// source outage — the poll must stay alive with an unknown denominator, not
// 500 forever.
func TestDeployStage_VanishedTagDegrades(t *testing.T) {
	t.Parallel()
	fx := statusFixture{
		runs:     []delivery.MilestoneRun{devRun("v1", delivery.RunStateSucceeded)},
		countErr: fmt.Errorf("wrapped: %w", spec.ErrSpecTagNotFound),
		bindings: []openchoreo.ReleaseBindingSummary{devBinding("api", "True", "Ready")},
	}
	st := mustStatus(t, fx)
	if st.Deploy.Version != "v1" || st.Deploy.Status != "deployed" {
		t.Errorf("deploy = %s/%s, want v1/deployed (degraded, not failed)", st.Deploy.Version, st.Deploy.Status)
	}
	if st.Deploy.Components.Total != 0 || st.Deploy.Components.Ready != 1 {
		t.Errorf("components = %+v, want 0 total (unknown) / 1 ready", st.Deploy.Components)
	}
}

// TestDeployStage_VersionlessSkipsDenominator: with no completed run there is
// no deployed tag — the denominator read must not happen (the fixture errors
// on any unexpected tag) and counts stay 0/0.
func TestDeployStage_VersionlessSkipsDenominator(t *testing.T) {
	t.Parallel()
	fx := statusFixture{
		runs: []delivery.MilestoneRun{devRun("v1", delivery.RunStateRunning)},
	}
	st := mustStatus(t, fx)
	if st.Deploy.Version != "" {
		t.Errorf("deploy version = %q, want \"\" (no succeeded run)", st.Deploy.Version)
	}
	if st.Deploy.Components.Total != 0 || st.Deploy.Components.Ready != 0 {
		t.Errorf("components = %+v, want 0/0", st.Deploy.Components)
	}
}

// deploy.validation is the newest run's own VERDICT — a column on the row the
// build stage already read, so the poll costs nothing extra. The report and the
// per-cycle detail behind it live on the version's run story, which is why
// there is no validationUrl or validationIssue here any more.
func TestDeployStage_ValidationDerivation(t *testing.T) {
	t.Parallel()

	// Keep the run live (not succeeded) so this test stays about validation
	// rather than the deploy denominator.
	withVerdict := func(state, verdict string) []delivery.MilestoneRun {
		run := devRun("v1", state)
		run.ValidationVerdict = verdict
		return []delivery.MilestoneRun{run}
	}

	// A VALIDATION run over the milestone the dev row names, built by re-kinding
	// devRun the way the multi-run tests above do. It has to be this kind:
	// newestValidatingOnMilestone selects on RunValidates, so a dev-kind row can
	// never stand in for the run that answers for the version. The dev row stays
	// LIVE for the reason at the top of this test.
	validationOver := func(tag, state string) []delivery.MilestoneRun {
		v := devRun(tag, state)
		v.Kind, v.Origin = delivery.RunKindValidation, delivery.RunOriginRevalidate
		return []delivery.MilestoneRun{v, devRun(tag, delivery.RunStateRunning)}
	}

	cycle := func(kind string, ended bool) *delivery.RunCycle {
		c := &delivery.RunCycle{Kind: kind}
		if ended {
			at := time.Now()
			c.EndedAt = &at
		}
		return c
	}

	cases := []struct {
		name       string
		runs       []delivery.MilestoneRun
		cycle      *delivery.RunCycle
		wantStatus string
	}{
		{name: "no run at all → none", wantStatus: "none"},

		// Every verdict is MIRRORED, so the chip says what the run concluded rather
		// than a coarser word that has to be looked up. These four are final the
		// moment they are written — the loop never repairs them — so a live run
		// renders them straight away.
		{"passed", withVerdict(delivery.RunStateRunning, delivery.ValidationVerdictPassed), nil, "passed"},
		{"partial", withVerdict(delivery.RunStateRunning, delivery.ValidationVerdictPartial), nil, "partial"},
		{"inconclusive", withVerdict(delivery.RunStateRunning, delivery.ValidationVerdictInconclusive), nil, "inconclusive"},
		// skipped is surfaced, not folded into none: "no validation criteria" is
		// actionable ("author some"), where none means "nothing to say yet".
		{"skipped", withVerdict(delivery.RunStateRunning, delivery.ValidationVerdictSkipped), nil, "skipped"},

		// The two REPAIRABLE verdicts are not final on a live run: the platform files
		// the failure as work and validates again. Rendering `failed` here would read
		// as terminal while the loop is actively fixing it, so a live run mid-repair
		// says so instead — and names the IMPLEMENTATION, since the cycle in flight is
		// ordinary coding work.
		{
			name:       "live run repairing a failed verdict → awaiting-fix",
			runs:       withVerdict(delivery.RunStateRunning, delivery.ValidationVerdictFailed),
			cycle:      cycle(delivery.CycleKindCoding, false),
			wantStatus: "awaiting-fix",
		},
		{
			name:       "live run re-dispatching after unreported → awaiting-fix",
			runs:       withVerdict(delivery.RunStateRunning, delivery.ValidationVerdictUnreported),
			cycle:      cycle(delivery.CycleKindCoding, false),
			wantStatus: "awaiting-fix",
		},
		// A repeat attempt already in flight is `running`, not `awaiting-fix`: the
		// latest cycle is the validation cycle, which wins over the stale verdict.
		{
			name:       "live run whose repeat validation cycle is in flight → running",
			runs:       withVerdict(delivery.RunStateRunning, delivery.ValidationVerdictFailed),
			cycle:      cycle(delivery.CycleKindValidation, false),
			wantStatus: "running",
		},
		// Once the run SETTLES, the repairable verdicts are the final answer — the
		// loop is over and it kept failing.
		{
			name:       "settled run that failed validation → failed",
			runs:       withVerdict(delivery.RunStateFailed, delivery.ValidationVerdictFailed),
			wantStatus: "failed",
		},
		{
			name:       "settled run that reported nothing → unreported",
			runs:       withVerdict(delivery.RunStateFailed, delivery.ValidationVerdictUnreported),
			wantStatus: "unreported",
		},

		{
			name:       "settled run that never validated → none",
			runs:       withVerdict(delivery.RunStateFailed, ""),
			wantStatus: "none",
		},

		// A person STOPPED the judging. `none` promises a verdict is still coming and
		// nothing is, so this version would sit "any moment now" forever — and the
		// promote gate, which holds on `none`, would never open again for it.
		{
			name:       "cancelled validation run with no verdict → cancelled",
			runs:       validationOver("v1", delivery.RunStateCancelled),
			wantStatus: "cancelled",
		},
		// The KIND guard. A cancelled DEV run is an ABANDONED INCREMENT, not judging
		// somebody declined — the reconcile sweep suppresses its whole milestone for
		// that reason. Without the guard this reads `cancelled`, which tells the
		// console there is nothing left to wait for and offers an unjudged, abandoned
		// version for promotion.
		{
			name:       "cancelled DEV run with no verdict → none, not cancelled",
			runs:       withVerdict(delivery.RunStateCancelled, ""),
			wantStatus: "none",
		},

		// A live run with no verdict is the one case the run row cannot answer, so
		// the latest CYCLE decides. This is what stops the chip claiming
		// "validating" through every coding cycle of every run.
		{
			name:       "live run in a validation cycle → running",
			runs:       withVerdict(delivery.RunStateRunning, ""),
			cycle:      cycle(delivery.CycleKindValidation, false),
			wantStatus: "running",
		},
		{
			name:       "live run in a CODING cycle → none, not running",
			runs:       withVerdict(delivery.RunStateRunning, ""),
			cycle:      cycle(delivery.CycleKindCoding, false),
			wantStatus: "none",
		},
		{
			name:       "live run in a FIX cycle → none, not running",
			runs:       withVerdict(delivery.RunStateRunning, ""),
			cycle:      cycle(delivery.CycleKindFix, false),
			wantStatus: "none",
		},
		{
			name:       "validation cycle already ended but no verdict written yet → none",
			runs:       withVerdict(delivery.RunStateRunning, ""),
			cycle:      cycle(delivery.CycleKindValidation, true),
			wantStatus: "none",
		},
		{
			name:       "live run that has dispatched no cycle at all → none",
			runs:       withVerdict(delivery.RunStateRunning, ""),
			wantStatus: "none",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := mustStatus(t, statusFixture{runs: tc.runs, cycle: tc.cycle})
			if string(st.Deploy.Validation) != tc.wantStatus {
				t.Errorf("validation = %q, want %q", st.Deploy.Validation, tc.wantStatus)
			}
		})
	}
}

// TestRepoNotReady_ZeroValueStages pins the short-circuit: the nested stages
// are contract-required, so a pending repo returns them present but
// zero-valued — idle build, no deploy, empty spec.
func TestRepoNotReady_ZeroValueStages(t *testing.T) {
	t.Parallel()
	repoSvc := &fakeRepoSvc{
		GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{Status: "pending", RepoURL: "https://github.com/o/r.git"}, nil
		},
	}
	// Sources deliberately unwired: the short-circuit must not touch them.
	svc := NewProjectService(nil, repoSvc, nil, nil, nil)
	st, err := svc.GetProjectStatus(context.Background(), "acme", "web")
	if err != nil {
		t.Fatalf("GetProjectStatus: %v", err)
	}
	if st.Phase != "repo-cloning" {
		t.Fatalf("phase = %q, want repo-cloning", st.Phase)
	}
	if st.Spec != (gen.SpecStage{}) {
		t.Errorf("spec = %+v, want zero-valued", st.Spec)
	}
	if st.Build.Status != "idle" || st.Build.Version != "" {
		t.Errorf("build = %+v, want idle zero-valued", st.Build)
	}
	if st.Deploy.Status != "none" || st.Deploy.Version != "" || st.Deploy.Components.Total != 0 {
		t.Errorf("deploy = %+v, want none zero-valued", st.Deploy)
	}
}
