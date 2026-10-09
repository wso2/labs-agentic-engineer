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
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
)

// SERVICE TIER — the REAL issue service driving the org's pod, here the
// in-memory aestudiotest.Fake. Nothing between the event plane and the pod
// port is faked, so these tests see what the event-plane fakes cannot: the
// actual pod calls (one merge, one issue create) and the dedupe label
// round-trip that makes minting idempotent.

// widgets is the repository the project row resolves to.
var widgets = sourcecontrol.RepoRef{Org: testOrg, Owner: "acme", Repo: "widgets"}

// stubRepoRepo answers the ONE repository read the issue service makes, by
// embedding the interface: any other method would panic, which is the point —
// a service reaching further than expected fails loudly.
type stubRepoRepo struct {
	sourcecontrol.RepoRepository
	row *sourcecontrol.GitRepository
}

func (r stubRepoRepo) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return r.row, nil
}

// serviceHarness is the event plane with its GitHub-facing ports served by the
// real service, and only the run/cycle/build/supervisor seams faked.
type serviceHarness struct {
	*harness
	pod *aestudiotest.Fake
}

func newServiceHarness(t *testing.T, rows ...delivery.MilestoneRun) *serviceHarness {
	t.Helper()
	pod := aestudiotest.New()
	svc := sourcecontrol.NewIssueService(
		stubRepoRepo{row: &sourcecontrol.GitRepository{
			OrgID: testOrg, ProjectID: testProject, RepoSlug: "widgets",
			RepoURL: "https://github.com/acme/widgets",
		}},
		pod,
	)

	h := &harness{
		runs:   newFakeRuns(rows...),
		cycles: newFakeCycles(nil),
		builds: newFakeBuilds(),
		sup:    &fakeSupervisor{},
	}
	h.events = New(Ports{
		Runs:           h.runs,
		Cycles:         h.cycles,
		Issues:         svc,
		Writer:         delivery.NewIssueWriter(svc),
		PRs:            svc,
		Merger:         svc,
		Repos:          fakeRepoLookup{},
		Design:         fakeDesign{paths: map[string]string{"order-service": "services/order"}},
		Builds:         h.builds,
		Signaler:       h.sup,
		Starter:        h.sup,
		PlatformSender: platformBot,
	})
	h.router = webhook.NewRouter()
	h.events.RegisterHandlers(func(event, action string, fn func(ctx context.Context, event, action string, payload []byte) error) {
		h.router.Register(event, action, webhook.EventHandlerFunc(fn))
	})
	return &serviceHarness{harness: h, pod: pod}
}

// seedMilestones mints milestones 1..n on the pod, so a run's milestone
// number resolves.
func (s *serviceHarness) seedMilestones(t *testing.T, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		if _, err := s.pod.CreateMilestone(context.Background(), widgets, sourcecontrol.CreateMilestoneRequest{Title: fmt.Sprintf("v%d", i)}); err != nil {
			t.Fatal(err)
		}
	}
}

// calls answers the pod calls of op, in order.
func (s *serviceHarness) calls(op string) []aestudiotest.Call {
	var out []aestudiotest.Call
	for _, c := range s.pod.Calls() {
		if c.Op == op {
			out = append(out, c)
		}
	}
	return out
}

// TestService_AutoMergeIssuesTheSquashMergeOnce drives the whole merge row
// through the pod port: the milestone membership read, the ground-truth PR
// read, and the merge itself — twice, to prove a redelivery merges nothing.
func TestService_AutoMergeIssuesTheSquashMergeOnce(t *testing.T) {
	h := newServiceHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.seedMilestones(t, 7)
	// The milestone's open agent-work issue and the pull request resolving it.
	issue := h.pod.SeedIssue(widgets, sourcecontrol.IssueInfo{Title: "work", Labels: []string{"aep"}}, 7)
	pr := h.pod.SeedPullRequest(widgets, []string{"services/order/main.go"})

	payload := prBody("opened", "aep/m7-c1", fmt.Sprintf("Resolves #%d", issue), pr, false, false, "")
	for i := 0; i < 2; i++ {
		if err := h.deliver(t, "pull_request", payload); err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
	}

	if got := len(h.calls(aestudiotest.OpMergePullRequest)); got != 1 {
		t.Fatalf("the merge must be issued exactly once across a delivery and its redelivery, got %d", got)
	}
	if state, err := h.pod.GetPullRequest(context.Background(), widgets, pr); err != nil || !state.Merged {
		t.Fatalf("pull request state = %+v err=%v, want merged", state, err)
	}
	// The milestone read must be scoped to the milestone and to open issues, or
	// the predicate would be decided against the wrong population.
	reads := h.calls(aestudiotest.OpListMilestoneIssues)
	// Nothing below fails when the read never happened, so a read that moved
	// elsewhere would leave the label guard — the whole reason these assertions
	// exist — silently testing nothing.
	if len(reads) == 0 {
		t.Fatal("the merge policy must read the milestone's issues; no such call was made")
	}
	for _, r := range reads {
		if r.Milestone.Number != 7 || r.Milestone.State != "open" {
			t.Fatalf("milestone read = %+v, want milestone 7, open", r.Milestone)
		}
		// And it must NOT narrow by label. The label filter is AND, so any value
		// here excludes some population the policy accepts — it used to send
		// `labels=aep`, which hid the milestone's validation task (then unarmed)
		// and left every validation pull request unmerged. Adding a second label
		// would not have fixed it either: AND demands an issue carrying both, and
		// matches nothing. The label decision belongs to decideAutoMerge, over
		// the labels this read returns.
		if len(r.Milestone.Labels) != 0 {
			t.Fatalf("milestone read labels = %v; it must not narrow by label — that decision is the policy's", r.Milestone.Labels)
		}
	}
}

// TestService_FixIssueIsMintedOnce proves minting's idempotency where it
// actually lives: the DedupeKey becomes a label, the service lists open issues
// carrying it, and the second attempt returns the first issue instead of
// filing another.
func TestService_FixIssueIsMintedOnce(t *testing.T) {
	h := newServiceHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	h.seedMilestones(t, 7)
	cycle := aCycle("cycle-1", "run-1")
	cycle.MergeSHA = testMergeSHA
	h.cycles.latest = cycle
	// The re-trigger budget is already spent, so a red terminal mints.
	h.builds.runs["order-service"] = []BuildRun{
		{Name: delivery.BuildRunName(testProject, "order-service", testMergeSHA, 1), Completed: true},
		{Name: delivery.BuildRunName(testProject, "order-service", testMergeSHA, 2), Completed: true},
	}

	red := terminal("order-service", false, "step docker-build failed: exit 2")
	for i := 0; i < 2; i++ {
		if err := h.events.OnBuildTerminal(context.Background(), red); err != nil {
			t.Fatalf("terminal %d: %v", i, err)
		}
	}

	if got := len(h.calls(aestudiotest.OpCreateIssue)); got != 1 {
		t.Fatalf("the fix issue must be filed exactly once across two terminals, got %d", got)
	}
	// The created issue carries the milestone NUMBER (a title 422s), the
	// agent-work label and the dedupe label its key became.
	members, err := h.pod.ListMilestoneIssues(context.Background(), widgets, sourcecontrol.MilestoneIssuesFilter{Number: 7})
	if err != nil || len(members) != 1 {
		t.Fatalf("milestone 7 members = %+v err=%v, want the fix issue", members, err)
	}
	labels := members[0].Labels
	if !slices.Contains(labels, "aep") {
		t.Fatalf("the fix issue must be labelled agent work, got %v", labels)
	}
	if !slices.ContainsFunc(labels, func(l string) bool { return strings.HasPrefix(l, "dedupe:") }) {
		t.Fatalf("the fix issue must carry its dedupe label, got %v", labels)
	}
}
