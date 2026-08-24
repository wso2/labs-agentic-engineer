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

package delivery_test

import (
	"context"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

// admitRun inserts a dev run and returns it, failing the test if the
// mutex rejects it.
func admitRun(t *testing.T, repo delivery.MilestoneRunRepository, org, project string, n int, title string) *delivery.MilestoneRun {
	t.Helper()
	ok, row, err := repo.TryAdmit(context.Background(), devRun(org, project, n, title))
	if err != nil || !ok || row == nil {
		t.Fatalf("TryAdmit(%s/%s) = (%v, %v)", org, project, ok, err)
	}
	return row
}

// TestRunCycleRepository_AppendDispatchAndFinish pins one cycle's whole life:
// appended with zero attempts, dispatched (attempt counter + Job ref), the
// branch and PR LEARNED from the webhook, then closed on the merge — and every
// write on a closed cycle a (nil, nil) no-op.
func TestRunCycleRepository_AppendDispatchAndFinish(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orga", "proj", 4, "v4")

	cycle := &delivery.RunCycle{
		OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID, Kind: delivery.CycleKindCoding,
	}
	if err := cycles.Append(ctx, cycle); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if cycle.ID == "" {
		t.Fatalf("Append did not populate the cycle id: %+v", cycle)
	}
	if cycle.Attempts != 0 {
		t.Fatalf("fresh cycle attempts = %d, want 0 (the first dispatch takes it to 1)", cycle.Attempts)
	}

	// An unknown kind and a parentless cycle never reach the table.
	if err := cycles.Append(ctx, &delivery.RunCycle{
		OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID, Kind: "typo",
	}); err == nil {
		t.Fatalf("Append(unknown kind) succeeded, want a rejection")
	}
	if err := cycles.Append(ctx, &delivery.RunCycle{
		OrgID: run.OrgID, ProjectID: run.ProjectID, Kind: delivery.CycleKindCoding,
	}); err == nil {
		t.Fatalf("Append(no run id) succeeded, want a rejection")
	}

	// Dispatch, then re-dispatch after the agent died: attempts is the per-cycle
	// re-dispatch budget, so it counts dispatches and the Job ref re-points.
	first, err := cycles.NoteDispatch(ctx, cycle.ID, "job-c1-a1")
	if err != nil || first == nil {
		t.Fatalf("NoteDispatch = (%+v, %v)", first, err)
	}
	if first.Attempts != 1 || first.JobRef != "job-c1-a1" {
		t.Fatalf("after first dispatch = (attempts %d, job %q), want (1, job-c1-a1)", first.Attempts, first.JobRef)
	}
	second, err := cycles.NoteDispatch(ctx, cycle.ID, "job-c1-a2")
	if err != nil || second == nil {
		t.Fatalf("NoteDispatch(re-dispatch) = (%+v, %v)", second, err)
	}
	if second.Attempts != 2 || second.JobRef != "job-c1-a2" {
		t.Fatalf("after re-dispatch = (attempts %d, job %q), want (2, job-c1-a2)", second.Attempts, second.JobRef)
	}

	// Branch and PR arrive on the pull_request webhook, not from dispatch — and a
	// draft is recorded as one, so a cycle parked behind an unfinished pull
	// request is distinguishable from one that never opened any.
	prPage := "https://github.com/acme/widgets/pull/42"
	draft, err := cycles.NotePullRequest(ctx, cycle.ID, delivery.CyclePullRequest{
		Branch: "aep/m4-c1", Number: 42, URL: prPage, Draft: true,
	})
	if err != nil || draft == nil {
		t.Fatalf("NotePullRequest(draft) = (%+v, %v)", draft, err)
	}
	if draft.Branch != "aep/m4-c1" || draft.PRNumber != 42 || !draft.PRDraft {
		t.Fatalf("draft PR facts = (%q, %d, draft %v), want (aep/m4-c1, 42, true)",
			draft.Branch, draft.PRNumber, draft.PRDraft)
	}
	// The host's own link is stored verbatim: it is what the console links to, and
	// nothing in the platform composes one.
	if draft.PRURL != prPage {
		t.Fatalf("pull request URL = %q, want %q", draft.PRURL, prPage)
	}
	// Ready for review is the SAME pull request, so the flag clears in place.
	withPR, err := cycles.NotePullRequest(ctx, cycle.ID, delivery.CyclePullRequest{
		Branch: "aep/m4-c1", Number: 42, URL: prPage, Draft: false,
	})
	if err != nil || withPR == nil {
		t.Fatalf("NotePullRequest = (%+v, %v)", withPR, err)
	}
	if withPR.PRDraft || withPR.PRURL != prPage {
		t.Fatalf("PR facts after ready_for_review = %+v, want draft cleared and the link kept", withPR)
	}

	// The merge policy's matched set is the cycle's only record of what it
	// worked; a verdict is written only when the pull request did NOT merge.
	declined, err := cycles.NoteMergeDecision(ctx, cycle.ID, []int{7, 8},
		delivery.CycleMergeDeclined, "no resolved issue is this run's work in this milestone")
	if err != nil || declined == nil {
		t.Fatalf("NoteMergeDecision(declined) = (%+v, %v)", declined, err)
	}
	if len(declined.Resolves) != 2 || declined.Resolves[0] != 7 || declined.Resolves[1] != 8 {
		t.Fatalf("resolves = %v, want [7 8]", declined.Resolves)
	}
	if declined.MergeVerdict != delivery.CycleMergeDeclined || declined.MergeReason == "" {
		t.Fatalf("verdict = (%q, %q), want declined with a reason",
			declined.MergeVerdict, declined.MergeReason)
	}
	// A re-push that now merges must not leave the stale verdict behind: every
	// decision overwrites, blanks included.
	merged, err := cycles.NoteMergeDecision(ctx, cycle.ID, []int{7, 8}, "", "resolves agent work in the run's milestone")
	if err != nil || merged == nil {
		t.Fatalf("NoteMergeDecision(merge) = (%+v, %v)", merged, err)
	}
	if merged.MergeVerdict != "" {
		t.Fatalf("verdict after a merging decision = %q, want cleared", merged.MergeVerdict)
	}

	done, err := cycles.Finish(ctx, cycle.ID, "deadbeef")
	if err != nil || done == nil {
		t.Fatalf("Finish = (%+v, %v)", done, err)
	}
	if done.MergeSHA != "deadbeef" || done.EndedAt == nil {
		t.Fatalf("finished cycle = %+v, want merge sha + ended stamp", done)
	}

	// A closed cycle is never rewritten — a duplicate webhook is a no-op.
	if got, err := cycles.NoteDispatch(ctx, cycle.ID, "job-zombie"); err != nil || got != nil {
		t.Fatalf("NoteDispatch on a closed cycle = (%+v, %v), want (nil, nil)", got, err)
	}
	if got, err := cycles.NotePullRequest(ctx, cycle.ID, delivery.CyclePullRequest{
		Branch: "other", Number: 99,
	}); err != nil || got != nil {
		t.Fatalf("NotePullRequest on a closed cycle = (%+v, %v), want (nil, nil)", got, err)
	}
	if got, err := cycles.NoteMergeDecision(ctx, cycle.ID, []int{99}, delivery.CycleMergeRefused, "late"); err != nil || got != nil {
		t.Fatalf("NoteMergeDecision on a closed cycle = (%+v, %v), want (nil, nil)", got, err)
	}
	if got, err := cycles.Finish(ctx, cycle.ID, "cafebabe"); err != nil || got != nil {
		t.Fatalf("second Finish = (%+v, %v), want (nil, nil)", got, err)
	}
	if got, err := cycles.Finish(ctx, "00000000-0000-0000-0000-000000000000", ""); err != nil || got != nil {
		t.Fatalf("Finish(unknown id) = (%+v, %v), want (nil, nil)", got, err)
	}

	final, err := cycles.Latest(ctx, "orga", run.ID)
	if err != nil || final == nil {
		t.Fatalf("Latest = (%+v, %v)", final, err)
	}
	if final.MergeSHA != "deadbeef" || final.Attempts != 2 || final.PRNumber != 42 {
		t.Fatalf("closed cycle was rewritten: %+v", final)
	}
}

// TestRunCycleRepository_LatestAndTimeline pins the read the console's loop
// position comes from: the run's newest cycle, plus the oldest-first timeline —
// and the org fence on both.
func TestRunCycleRepository_LatestAndTimeline(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orga", "proj", 5, "v5")
	other := admitRun(t, runs, "orga", "another", 1, "v1")

	// Explicit CreatedAt stamps so ordering does not lean on Postgres
	// microsecond resolution between back-to-back inserts.
	base := time.Now().UTC().Add(-time.Hour)
	appendCycle := func(parent *delivery.MilestoneRun, kind string, offset time.Duration) *delivery.RunCycle {
		t.Helper()
		c := &delivery.RunCycle{
			OrgID: parent.OrgID, ProjectID: parent.ProjectID, RunID: parent.ID, Kind: kind,
			CreatedAt: base.Add(offset),
		}
		if err := cycles.Append(ctx, c); err != nil {
			t.Fatalf("Append(%s): %v", kind, err)
		}
		return c
	}

	// A run whose loop re-entered earlier phases: coding → conflict → fix →
	// validation. This is exactly why no flat phase enum lives on the run row.
	c1 := appendCycle(run, delivery.CycleKindCoding, 0)
	c2 := appendCycle(run, delivery.CycleKindConflict, time.Minute)
	c3 := appendCycle(run, delivery.CycleKindFix, 2*time.Minute)
	c4 := appendCycle(run, delivery.CycleKindValidation, 3*time.Minute)
	// A cycle on a different run must never leak into either read.
	appendCycle(other, delivery.CycleKindCoding, 4*time.Minute)

	latest, err := cycles.Latest(ctx, "orga", run.ID)
	if err != nil || latest == nil {
		t.Fatalf("Latest = (%+v, %v)", latest, err)
	}
	if latest.ID != c4.ID || latest.Kind != delivery.CycleKindValidation {
		t.Fatalf("Latest = %+v, want the validation cycle %s", latest, c4.ID)
	}

	timeline, err := cycles.ListByRun(ctx, "orga", run.ID)
	if err != nil {
		t.Fatalf("ListByRun: %v", err)
	}
	if len(timeline) != 4 {
		t.Fatalf("ListByRun = %d cycles, want 4", len(timeline))
	}
	wantOrder := []string{c1.ID, c2.ID, c3.ID, c4.ID}
	for i, want := range wantOrder {
		if timeline[i].ID != want {
			t.Fatalf("timeline[%d] = %s (%s), want %s — the list is oldest-first",
				i, timeline[i].ID, timeline[i].Kind, want)
		}
	}

	// A run that has not dispatched yet misses cleanly.
	fresh := admitRun(t, runs, "orga", "third", 1, "v1")
	if got, err := cycles.Latest(ctx, "orga", fresh.ID); err != nil || got != nil {
		t.Fatalf("Latest(no cycles) = (%+v, %v), want (nil, nil)", got, err)
	}

	// Org scoping: a cross-org read misses on both.
	if got, err := cycles.Latest(ctx, "orgb", run.ID); err != nil || got != nil {
		t.Fatalf("Latest(cross-org) = (%+v, %v), want (nil, nil)", got, err)
	}
	if rows, err := cycles.ListByRun(ctx, "orgb", run.ID); err != nil || len(rows) != 0 {
		t.Fatalf("ListByRun(cross-org) = (%d rows, %v), want 0", len(rows), err)
	}

	// The project-delete cascade purges the timeline with the runs.
	if err := cycles.DeleteByProject(ctx, "orga", "proj"); err != nil {
		t.Fatalf("DeleteByProject: %v", err)
	}
	if rows, err := cycles.ListByRun(ctx, "orga", run.ID); err != nil || len(rows) != 0 {
		t.Fatalf("after purge: (%d rows, %v), want 0", len(rows), err)
	}
	// Another project's cycles survive.
	if rows, err := cycles.ListByRun(ctx, "orga", other.ID); err != nil || len(rows) != 1 {
		t.Fatalf("other project's timeline = (%d rows, %v), want 1", len(rows), err)
	}
}

// TestRunCycleRepository_GetByIDScoped pins the RUNNER's identity read: a
// dispatched pod names its cycle id on every callback (AEP_TASK_ID), and this is
// how the platform resolves that id to a project it may act on.
//
// It also pins the decision that the read resolves CYCLES ONLY. An execution id
// must miss — the rows left in that table are dependency-provisioning gates,
// which run no agent and are not a callback identity — so a future "helpful"
// fallback to executions would fail here rather than silently widen who can pass
// as a runner.
func TestRunCycleRepository_GetByIDScoped(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	execs := delivery.NewExecutionRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orga", "proj", 7, "v7")
	cycle := &delivery.RunCycle{
		OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID,
		Kind: delivery.CycleKindValidation,
	}
	if err := cycles.Append(ctx, cycle); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := cycles.GetByIDScoped(ctx, "orga", cycle.ID)
	if err != nil || got == nil {
		t.Fatalf("GetByIDScoped = (%+v, %v), want the cycle", got, err)
	}
	// The project is the whole point of the read: it is what the callback is then
	// allowed to act on.
	if got.ProjectID != "proj" || got.Kind != delivery.CycleKindValidation {
		t.Errorf("GetByIDScoped = %+v; want project proj, kind validation", got)
	}

	// The tenant fence: another org's read MISSES rather than erroring, so a
	// cross-tenant probe cannot tell "not yours" from "does not exist".
	if got, err := cycles.GetByIDScoped(ctx, "orgb", cycle.ID); err != nil || got != nil {
		t.Fatalf("GetByIDScoped(cross-org) = (%+v, %v), want (nil, nil)", got, err)
	}

	// An unknown id misses cleanly, never gorm.ErrRecordNotFound.
	const absent = "3f4b9c1e-0000-4000-8000-000000000000"
	if got, err := cycles.GetByIDScoped(ctx, "orga", absent); err != nil || got != nil {
		t.Fatalf("GetByIDScoped(unknown) = (%+v, %v), want (nil, nil)", got, err)
	}

	// An EXECUTION id is not a runner identity. Same org, real row, still a miss.
	admitted, exec, err := execs.TryAdmit(ctx, &delivery.Execution{
		OrgID: "orga", ProjectID: "proj", Repo: "orga/proj", IssueNumber: 3,
		Kind: "provision", Status: "queued",
	})
	if err != nil || !admitted || exec == nil {
		t.Fatalf("TryAdmit(provision execution) = (%v, %+v, %v)", admitted, exec, err)
	}
	if got, err := cycles.GetByIDScoped(ctx, "orga", exec.ID); err != nil || got != nil {
		t.Fatalf("GetByIDScoped(execution id) = (%+v, %v), want (nil, nil) — cycles only", got, err)
	}
}

// TestRunCycleRepository_RecordUsageAndPhaseRollup pins delivery's agent-spend
// capture end to end: the usage stamp lands on a CLOSED cycle (the normal case,
// since a cycle closes on the merge webhook seconds after the agent Job exits),
// the USD is frozen from the stamper at write time, and the rollup attributes
// each cycle kind to the right SDLC phase.
func TestRunCycleRepository_RecordUsageAndPhaseRollup(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	// $1/MTok in, $10/MTok out, $0.10/MTok cache read — round figures so the
	// expected stamp is obvious by inspection rather than reverse-engineered.
	stamper := modelcost.NewStamper([]modelcost.ModelRate{{
		ModelID: "model-a", InputPerMTok: 1, OutputPerMTok: 10, CacheReadPerMTok: 0.1,
	}})
	cycles := delivery.NewRunCycleRepository(db, stamper)
	// The rollup reads the LEDGER, which RecordUsage mirrors into as it stamps —
	// the dispatch row is the run spine's copy, not a second source of spend.
	ledger := delivery.NewAgentUsageLedgerRepository(db)
	ctx := context.Background()

	run := admitRun(t, runs, "orgu", "shop", 7, "v7")
	appendCycle := func(kind string) *delivery.RunCycle {
		t.Helper()
		c := &delivery.RunCycle{
			OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID, Kind: kind,
		}
		if err := cycles.Append(ctx, c); err != nil {
			t.Fatalf("Append(%s): %v", kind, err)
		}
		return c
	}

	coding := appendCycle(delivery.CycleKindCoding)
	fix := appendCycle(delivery.CycleKindFix)
	validation := appendCycle(delivery.CycleKindValidation)
	// A build-phase cycle that never captures usage — an agent that died before
	// its terminal message. It stays at 0 tokens with model_id '', and must not
	// drag the phase's model id to "unknown" the way a genuine multi-model phase
	// would (contracts.TokenUsage.Add keeps the model across a zero contributor).
	appendCycle(delivery.CycleKindConflict)

	// The coding cycle CLOSES before its usage is captured — the ordering that
	// actually happens in production, and the one an open-cycle guard would drop.
	if _, err := cycles.Finish(ctx, coding.ID, "deadbeef"); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// 1M in + 100k out + 2M cache-read = $1 + $1 + $0.20 = $2.20
	usage := contracts.TokenUsage{
		InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 2_000_000,
		Model: "model-a",
	}
	if err := cycles.RecordUsage(ctx, coding.ID, contracts.CapturedUsage{TokenUsage: usage}); err != nil {
		t.Fatalf("RecordUsage(closed coding cycle): %v", err)
	}
	// A second, smaller build-phase contributor so the rollup is proven to SUM
	// rather than to return the last row it saw.
	if err := cycles.RecordUsage(ctx, fix.ID, contracts.CapturedUsage{TokenUsage: contracts.TokenUsage{
		InputTokens: 1_000_000, Model: "model-a", // $1.00
	}}); err != nil {
		t.Fatalf("RecordUsage(fix): %v", err)
	}
	// An UNPRICEABLE validation run: real tokens, a model with no rate row. Its
	// cost must stay null while its tokens still count.
	if err := cycles.RecordUsage(ctx, validation.ID, contracts.CapturedUsage{TokenUsage: contracts.TokenUsage{
		InputTokens: 500_000, OutputTokens: 10_000, Model: "model-unknown",
	}}); err != nil {
		t.Fatalf("RecordUsage(validation): %v", err)
	}

	// The stamp is frozen on the row.
	stored, err := cycles.Latest(ctx, "orgu", run.ID)
	if err != nil || stored == nil {
		t.Fatalf("Latest = (%+v, %v)", stored, err)
	}
	timeline, err := cycles.ListByRun(ctx, "orgu", run.ID)
	if err != nil {
		t.Fatalf("ListByRun: %v", err)
	}
	byID := map[string]delivery.RunCycle{}
	for _, c := range timeline {
		byID[c.ID] = c
	}
	if got := byID[coding.ID]; got.CostUsd == nil || *got.CostUsd != 2.20 {
		t.Fatalf("coding cost_usd = %v, want 2.20", got.CostUsd)
	}
	if got := byID[coding.ID].Usage(); got != usage {
		t.Fatalf("coding usage round-trip = %+v, want %+v", got, usage)
	}
	if got := byID[validation.ID]; got.CostUsd != nil {
		t.Fatalf("unpriceable validation cost_usd = %v, want null", got.CostUsd)
	}

	build, valid, err := ledger.SumUsageByProjectPhase(ctx, "orgu")
	if err != nil {
		t.Fatalf("SumUsageByProjectPhase: %v", err)
	}
	// coding + fix fold into build; conflict contributed nothing and must not
	// appear as a project of its own or dilute the model id.
	b := build[liveScope("shop")]
	if b.Tokens.InputTokens != 2_000_000 || b.Tokens.OutputTokens != 100_000 ||
		b.Tokens.CacheReadTokens != 2_000_000 {
		t.Fatalf("build tokens = %+v, want 2M/100k/2M", b.Tokens)
	}
	if b.CostUsd == nil || *b.CostUsd != 3.20 {
		t.Fatalf("build cost = %v, want 3.20 (2.20 + 1.00)", b.CostUsd)
	}
	if b.Tokens.Model != "model-a" {
		t.Fatalf("build model = %q, want model-a (both contributors agree)", b.Tokens.Model)
	}
	// The validation cycle lands in the VALIDATION phase, not build — the split
	// this whole rollup exists to get right.
	v := valid[liveScope("shop")]
	if v.Tokens.InputTokens != 500_000 || v.Tokens.OutputTokens != 10_000 {
		t.Fatalf("validation tokens = %+v, want 500k/10k", v.Tokens)
	}
	if v.CostUsd != nil {
		t.Fatalf("validation cost = %v, want nil (no rate row for its model)", v.CostUsd)
	}

	// Org fence: another org's rollup sees none of it.
	if b2, v2, err := ledger.SumUsageByProjectPhase(ctx, "other-org"); err != nil ||
		len(b2) != 0 || len(v2) != 0 {
		t.Fatalf("cross-org rollup = (%v, %v, %v), want empty", b2, v2, err)
	}
}

// TestRunCycleRepository_RecordUsageStampsMultiModelSplit pins the fix for the
// tokens-instead-of-dollars Usage chips: a real coding run regularly touches a
// second model (the SDK's small-model helpers), which blanks the aggregate
// model id — but the runner now ships the per-model split, and the stamp must
// price each slice at its own rate and sum. A split containing a model with no
// rate row still stamps null (all-or-nothing), never a partial figure.
func TestRunCycleRepository_RecordUsageStampsMultiModelSplit(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	stamper := modelcost.NewStamper([]modelcost.ModelRate{
		{ModelID: "model-a", InputPerMTok: 1, OutputPerMTok: 10, CacheReadPerMTok: 0.1},
		{ModelID: "model-b", InputPerMTok: 2, OutputPerMTok: 20},
	})
	cycles := delivery.NewRunCycleRepository(db, stamper)
	ctx := context.Background()

	run := admitRun(t, runs, "orgm", "shop", 1, "v1")
	appendCycle := func() *delivery.RunCycle {
		t.Helper()
		c := &delivery.RunCycle{OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID, Kind: delivery.CycleKindCoding}
		if err := cycles.Append(ctx, c); err != nil {
			t.Fatalf("Append: %v", err)
		}
		return c
	}
	mixed := appendCycle()
	poisoned := appendCycle()

	// model-a: 1M in + 100k out = $2.00; model-b: 500k in = $1.00 → $3.00. The
	// aggregate model is "" (mixed), which used to make the whole cycle
	// unpriceable.
	if err := cycles.RecordUsage(ctx, mixed.ID, contracts.CapturedUsage{
		TokenUsage: contracts.TokenUsage{InputTokens: 1_500_000, OutputTokens: 100_000, Model: ""},
		Models: []contracts.TokenUsage{
			{InputTokens: 1_000_000, OutputTokens: 100_000, Model: "model-a"},
			{InputTokens: 500_000, Model: "model-b"},
		},
	}); err != nil {
		t.Fatalf("RecordUsage(mixed): %v", err)
	}
	if err := cycles.RecordUsage(ctx, poisoned.ID, contracts.CapturedUsage{
		TokenUsage: contracts.TokenUsage{InputTokens: 1_000_010, Model: ""},
		Models: []contracts.TokenUsage{
			{InputTokens: 1_000_000, Model: "model-a"},
			{InputTokens: 10, Model: "model-unrated"},
		},
	}); err != nil {
		t.Fatalf("RecordUsage(poisoned): %v", err)
	}

	timeline, err := cycles.ListByRun(ctx, "orgm", run.ID)
	if err != nil {
		t.Fatalf("ListByRun: %v", err)
	}
	byID := map[string]delivery.RunCycle{}
	for _, c := range timeline {
		byID[c.ID] = c
	}
	if got := byID[mixed.ID]; got.CostUsd == nil || *got.CostUsd != 3.00 {
		t.Fatalf("mixed-model cost_usd = %v, want 3.00", got.CostUsd)
	}
	if got := byID[poisoned.ID]; got.CostUsd != nil {
		t.Fatalf("cost_usd with an unrated slice = %v, want null", *got.CostUsd)
	}

	// The phase rollup sums the mixed cycle's stamp and keeps the aggregate
	// tokens; the poisoned cycle contributes tokens but no dollars.
	build, _, err := delivery.NewAgentUsageLedgerRepository(db).SumUsageByProjectPhase(ctx, "orgm")
	if err != nil {
		t.Fatalf("SumUsageByProjectPhase: %v", err)
	}
	b := build[liveScope("shop")]
	if b.CostUsd == nil || *b.CostUsd != 3.00 {
		t.Fatalf("build cost = %v, want 3.00", b.CostUsd)
	}
	if b.Tokens.InputTokens != 2_500_010 {
		t.Fatalf("build input tokens = %d, want 2_500_010", b.Tokens.InputTokens)
	}
}

// The validation verdict is the ONE cycle field written after the cycle closes: it
// is derived from the report at the cycle's own merge commit, which does not exist
// until the cycle has landed. So it cannot inherit the closed-cycle fence every
// other mutator uses, and it gets a write-once one instead.
func TestRunCycleRepository_SetValidationVerdictIsWriteOnceAfterClose(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgv", "proj", 9, "v9")
	cycle := &delivery.RunCycle{
		OrgID: run.OrgID, ProjectID: run.ProjectID, RunID: run.ID,
		Kind: delivery.CycleKindValidation,
	}
	if err := cycles.Append(ctx, cycle); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := cycles.Finish(ctx, cycle.ID, "deadbeefcafe0001"); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	// A CLOSED cycle still accepts its verdict — the point of the different fence.
	// The DIGEST rides the same write, and must: the fence below would reject any
	// later attempt to add it.
	got, err := cycles.SetValidationVerdict(ctx, cycle.ID, delivery.ValidationVerdictFailed, 77, "digest-one")
	if err != nil || got == nil {
		t.Fatalf("SetValidationVerdict on a closed cycle = (%+v, %v), want the row back", got, err)
	}
	if got.ValidationVerdict != delivery.ValidationVerdictFailed || got.ValidationIssue != 77 {
		t.Fatalf("recorded (%q, %d), want (failed, 77)", got.ValidationVerdict, got.ValidationIssue)
	}
	if got.ValidationDigest != "digest-one" {
		t.Fatalf("digest = %q, want digest-one — the next attempt compares against it", got.ValidationDigest)
	}

	// A second write is a no-op, not a rewrite: an attempt concludes once, so a
	// repeat can only be an activity retry. (nil, nil) is the no-op contract.
	again, err := cycles.SetValidationVerdict(ctx, cycle.ID, delivery.ValidationVerdictPassed, 77, "digest-two")
	if err != nil {
		t.Fatalf("SetValidationVerdict(retry): %v", err)
	}
	if again != nil {
		t.Fatalf("a second verdict write returned %+v, want the (nil, nil) no-op", again)
	}
	reread, err := cycles.GetByIDScoped(ctx, run.OrgID, cycle.ID)
	if err != nil || reread == nil {
		t.Fatalf("GetByIDScoped = (%+v, %v)", reread, err)
	}
	if reread.ValidationVerdict != delivery.ValidationVerdictFailed {
		t.Fatalf("verdict was overwritten to %q; an attempt's answer is final", reread.ValidationVerdict)
	}
	if reread.ValidationDigest != "digest-one" {
		t.Fatalf("digest was overwritten to %q; it is fenced with the verdict it belongs to", reread.ValidationDigest)
	}

	// And it is readable as the milestone's newest digest — the read one validation
	// run makes of the PREVIOUS run's answer.
	digest, err := cycles.LatestValidationDigest(ctx, run.OrgID, []string{run.ID})
	if err != nil || digest != "digest-one" {
		t.Fatalf("LatestValidationDigest = (%q, %v), want digest-one", digest, err)
	}
	// No prior runs is not an error: a version's first attempt has nothing to
	// compare against.
	empty, err := cycles.LatestValidationDigest(ctx, run.OrgID, nil)
	if err != nil || empty != "" {
		t.Fatalf("LatestValidationDigest(no runs) = (%q, %v), want the empty no-op", empty, err)
	}

	// The closed vocabulary is enforced here too, so a typo cannot reach the column.
	if _, err := cycles.SetValidationVerdict(ctx, cycle.ID, "kinda-passed", 77, ""); err == nil {
		t.Fatal("SetValidationVerdict accepted an unknown verdict")
	}
}

// TestRunCycleRepository_ListRecentDispatched pins the JobWatcher's claim set: it
// must include a cycle that has already CLOSED. The agent Job exits the instant
// it opens its pull request and the auto-merge closes the cycle seconds later —
// long before the next 30s tick — so an open-cycles-only query would miss nearly
// every terminal usage capture.
func TestRunCycleRepository_ListRecentDispatched(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	runs := delivery.NewMilestoneRunRepository(db)
	cycles := delivery.NewRunCycleRepository(db, nil)
	ctx := context.Background()

	run := admitRun(t, runs, "orgc", "proj", 4, "v4")
	open := appendCycle(t, cycles, run, delivery.CycleKindCoding, "ca-open")
	closed := appendCycle(t, cycles, run, delivery.CycleKindFix, "ca-closed")
	if _, err := cycles.Finish(ctx, closed.ID, "sha1"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	// A cycle that never launched a Job has no pod to read.
	undispatched := appendCycle(t, cycles, run, delivery.CycleKindConflict, "")

	got, err := cycles.ListRecentDispatched(ctx, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListRecentDispatched: %v", err)
	}
	seen := map[string]bool{}
	for i := range got {
		seen[got[i].ID] = true
	}
	if !seen[open.ID] {
		t.Error("an open dispatched cycle must be claimed")
	}
	if !seen[closed.ID] {
		t.Error("a recently closed cycle must still be claimed — usage capture outlives the cycle close")
	}
	if seen[undispatched.ID] {
		t.Error("a cycle with no Job must not be claimed")
	}

	// A window that starts after the close excludes it again.
	later, err := cycles.ListRecentDispatched(ctx, time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("ListRecentDispatched(future): %v", err)
	}
	for i := range later {
		if later[i].ID == closed.ID {
			t.Error("a cycle closed before the window must be excluded")
		}
	}
	// …but the still-open one is always in scope.
	found := false
	for i := range later {
		if later[i].ID == open.ID {
			found = true
		}
	}
	if !found {
		t.Error("an open cycle is in scope regardless of the window")
	}
}
