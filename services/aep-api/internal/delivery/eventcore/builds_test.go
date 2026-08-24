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

package eventcore

import (
	"context"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// §8 row 4: build terminals and the automatic re-trigger budget. These drive
// OnBuildTerminal directly — it is the root observer port the watcher calls,
// not a webhook — while the fake OpenChoreo remembers every run it was asked
// to create, so the budget is counted against a growing run list exactly as it
// is in the cluster.

const testMergeSHA = "abc123def456789"

// buildHarness is the webhook harness with a run whose current cycle landed
// testMergeSHA — the state a build terminal arrives in.
func buildHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	cycle := aCycle("cycle-1", "run-1")
	cycle.MergeSHA = testMergeSHA
	h.cycles.latest = cycle
	return h
}

func terminal(component string, succeeded bool, reason string) delivery.BuildTerminal {
	return delivery.BuildTerminal{
		OrgID: testOrg, ProjectID: testProject, Component: component,
		CommitSHA: testMergeSHA, RunName: "proj1-" + component + "-abc123def456-1",
		Succeeded: succeeded, Reason: reason,
	}
}

func TestBuildTerminal_GreenIsReportedAndNothingIsRetriggered(t *testing.T) {
	h := buildHarness(t)

	if err := h.events.OnBuildTerminal(context.Background(), terminal("order-service", true, "")); err != nil {
		t.Fatalf("OnBuildTerminal: %v", err)
	}
	sigs := h.sup.named(delivery.SigRunBuildTerminal)
	if len(sigs) != 1 || !sigs[0].Succeeded || sigs[0].Component != "order-service" {
		t.Fatalf("a green build must be reported per component, got %+v", sigs)
	}
	if len(h.builds.triggered) != 0 || len(h.issues.created) != 0 {
		t.Fatalf("a green build triggers and mints nothing, got %v / %v", h.builds.triggered, h.issues.titles())
	}
}

// TestBuildTerminal_FirstRedRetriggersExactlyOnce pins the budget's first half:
// one automatic re-trigger per component per SHA, at the SAME commit, and no
// fix issue yet — a build that fails once and passes on an identical re-run
// failed for an infrastructure reason, not a code reason.
func TestBuildTerminal_FirstRedRetriggersExactlyOnce(t *testing.T) {
	h := buildHarness(t)
	// The original attempt exists in OpenChoreo, as it would after the merge
	// fan-out triggered it.
	if _, err := h.events.ensureBuildRun(context.Background(), testOrg, testProject, "order-service", testMergeSHA, staged("org-git-secret"), mergeBuildLimit); err != nil {
		t.Fatalf("seed attempt 1: %v", err)
	}

	if err := h.events.OnBuildTerminal(context.Background(), terminal("order-service", false, "step docker-build failed")); err != nil {
		t.Fatalf("OnBuildTerminal: %v", err)
	}

	runs := h.builds.triggeredFor("order-service")
	if len(runs) != 2 || runs[1] != delivery.BuildRunName(testProject, "order-service", testMergeSHA, 2) {
		t.Fatalf("the first red must re-trigger attempt 2 at the same SHA, got %v", runs)
	}
	if len(h.issues.created) != 0 {
		t.Fatalf("no fix issue until the budget is spent, got %v", h.issues.titles())
	}
	if len(h.sup.named(delivery.SigRunBuildTerminal)) != 0 {
		t.Fatal("a re-triggered build is not yet terminal for the run")
	}
	if len(h.runs.bumps) != 1 || h.runs.bumps[0] != delivery.RunBudgetBuildRetriggers {
		t.Fatalf("the re-trigger must be tallied on the run, got %v", h.runs.bumps)
	}
}

// TestBuildTerminal_SecondRedMintsTheFixIssueOnce pins the second half: the
// budget is spent, so the component's verdict is red — one fix issue carrying
// component, merge SHA and the failure output, and one signal.
func TestBuildTerminal_SecondRedMintsTheFixIssueOnce(t *testing.T) {
	h := buildHarness(t)
	ctx := context.Background()
	if _, err := h.events.ensureBuildRun(ctx, testOrg, testProject, "order-service", testMergeSHA, staged("org-git-secret"), mergeBuildLimit); err != nil {
		t.Fatalf("seed attempt 1: %v", err)
	}
	if err := h.events.OnBuildTerminal(ctx, terminal("order-service", false, "step docker-build failed")); err != nil {
		t.Fatalf("first red: %v", err)
	}

	// The re-triggered run goes red too, and the SAME terminal is then reported
	// twice (a watcher restart re-reads a completed WorkflowRun).
	red := terminal("order-service", false, "step docker-build failed: exit 2")
	if err := h.events.OnBuildTerminal(ctx, red); err != nil {
		t.Fatalf("second red: %v", err)
	}
	if err := h.events.OnBuildTerminal(ctx, red); err != nil {
		t.Fatalf("repeated terminal: %v", err)
	}

	if runs := h.builds.triggeredFor("order-service"); len(runs) != 2 {
		t.Fatalf("the budget is 1 re-trigger per component per SHA — no third run, got %v", runs)
	}
	titles := h.issues.titles()
	if len(titles) != 1 || titles[0] != "Fix the failing build for order-service" {
		t.Fatalf("exactly one fix issue must be minted, got %v", titles)
	}
	body := h.issues.created[0].Body
	for _, want := range []string{"order-service", testMergeSHA, "exit 2"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the fix issue must carry %q, got:\n%s", want, body)
		}
	}
	if ms := h.issues.created[0].Milestone; ms == nil || *ms != 7 {
		t.Fatalf("the fix issue must join the run's milestone, got %v", ms)
	}
	if !delivery.HasLabel(h.issues.created[0].Labels, delivery.LabelAgentWork) {
		t.Fatalf("the fix issue must be agent work so it joins the next cycle, got %v", h.issues.created[0].Labels)
	}
	// The key is the domain's, not this package's: an inlined spelling here
	// would drift from the frozen vocabulary without failing anything.
	if got, want := h.issues.created[0].DedupeKey,
		delivery.DedupeKeyFix("order-service", delivery.ShortSHA(testMergeSHA)); got != want {
		t.Fatalf("fix dedupe key = %q; want the domain's %q", got, want)
	}
	sigs := h.sup.named(delivery.SigRunBuildTerminal)
	if len(sigs) != 2 || sigs[0].Succeeded {
		t.Fatalf("each terminal report must reach the supervisor as red, got %+v", sigs)
	}
}

// TestBuildTerminal_RedMainOutsideARunMintsAnIncident covers the last routing
// row: a red build nobody's run owns is main regressing, and it is filed
// against the DEPLOYED version — never labelled agent work, never dispatched.
func TestBuildTerminal_RedMainOutsideARunMintsAnIncident(t *testing.T) {
	h := newHarness(t, aRun("run-old", 5, delivery.RunStateSucceeded))
	ctx := context.Background()

	ev := delivery.BuildTerminal{
		OrgID: testOrg, ProjectID: testProject, Component: "web",
		CommitSHA: "deadbeef1234", RunName: "proj1-web-deadbeef1234-1",
		Succeeded: false, Reason: "step docker-build failed",
	}
	if err := h.events.OnBuildTerminal(ctx, ev); err != nil {
		t.Fatalf("OnBuildTerminal: %v", err)
	}
	if err := h.events.OnBuildTerminal(ctx, ev); err != nil {
		t.Fatalf("repeat: %v", err)
	}

	titles := h.issues.titles()
	if len(titles) != 1 || titles[0] != "Red main: web" {
		t.Fatalf("one incident issue must be filed for a red main, got %v", titles)
	}
	if ms := h.issues.created[0].Milestone; ms == nil || *ms != 5 {
		t.Fatalf("the incident belongs to the deployed version's milestone, got %v", ms)
	}
	// CLASSIFIED but NOT ARMED. The kind and its source are how a human reading
	// the milestone knows what this is; the missing arming label is why nothing
	// is dispatched at it until that human decides. Classification is not
	// permission — arming an issue is the only thing that grants it.
	if delivery.HasLabel(h.issues.created[0].Labels, delivery.LabelAgentWork) {
		t.Fatalf("a red-main incident must not be armed — it is never auto-dispatched, got %v",
			h.issues.created[0].Labels)
	}
	if got := delivery.KindOf(h.issues.created[0].Labels); got != delivery.KindBug {
		t.Fatalf("a red-main incident is a bug, got kind %q from %v", got, h.issues.created[0].Labels)
	}
	if !delivery.HasLabel(h.issues.created[0].Labels, delivery.SrcIncident) {
		t.Fatalf("a red-main incident is sourced from an incident, got %v",
			h.issues.created[0].Labels)
	}
	if got, want := h.issues.created[0].DedupeKey,
		delivery.DedupeKeyRedMain("web", delivery.ShortSHA(ev.CommitSHA)); got != want {
		t.Fatalf("red-main dedupe key = %q; want the domain's %q", got, want)
	}
	if len(h.builds.triggered) != 0 {
		t.Fatalf("a red main outside a run is not re-triggered, got %v", h.builds.triggered)
	}
}

func TestBuildTerminal_RedMainWithNoDeployedVersionWritesNothing(t *testing.T) {
	h := newHarness(t) // never built

	if err := h.events.OnBuildTerminal(context.Background(), terminal("web", false, "boom")); err != nil {
		t.Fatalf("OnBuildTerminal: %v", err)
	}
	if len(h.issues.created) != 0 {
		t.Fatalf("with no deployed version there is nothing to attribute a red main to, got %v", h.issues.titles())
	}
}
