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
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/gittest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	githubclient "github.com/wso2/aep/aep-api/internal/sourcecontrol/githubhost"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
)

// SERVICE TIER — the REAL issue service driving the REAL GitHub client against
// a route-registry stub. Nothing between the event plane and HTTP is faked, so
// these tests see what the fakes cannot: the actual requests (a PUT to the
// merge route, one POST to the issues route) and the dedupe label round-trip
// that makes minting idempotent.

// stubResolver / stubCred are the credential seam — the one edge a stub server
// cannot serve.
type stubCred struct{}

func (stubCred) Token(context.Context) (string, time.Time, error) {
	return "test-token", time.Time{}, nil
}
func (stubCred) Identity() secrets.Identity               { return secrets.Identity{} }
func (stubCred) RepoOwner() string                        { return "acme" }
func (stubCred) WebhookStrategy() secrets.WebhookStrategy { return secrets.WebhookPerRepo }
func (stubResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return stubCred{}, nil
}

type stubResolver struct{}

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

// newIssueSvcOnStub builds the real issue service over the real client, aimed
// at the stub. Every call lands under /repos/acme/widgets.
func newIssueSvcOnStub(stub *gittest.Stub) sourcecontrol.IssueService {
	return sourcecontrol.NewIssueService(
		stubRepoRepo{row: &sourcecontrol.GitRepository{
			OrgID: testOrg, ProjectID: testProject, RepoSlug: "widgets",
			RepoURL: "https://github.com/acme/widgets",
		}},
		githubclient.NewClient(githubclient.WithAPIBase(stub.URL)),
		stubResolver{},
	)
}

// serviceHarness is the event plane with its GitHub-facing ports served by the
// real service, and only the run/cycle/build/supervisor seams faked.
type serviceHarness struct {
	*harness
	stub *gittest.Stub
}

func newServiceHarness(t *testing.T, rows ...delivery.MilestoneRun) *serviceHarness {
	t.Helper()
	stub := gittest.NewStub(t)
	svc := newIssueSvcOnStub(stub)

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
	return &serviceHarness{harness: h, stub: stub}
}

func (s *serviceHarness) countRequests(method, path string) int {
	n := 0
	for _, r := range s.stub.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// TestService_AutoMergeIssuesTheSquashMergeOnce drives the whole merge row
// through real HTTP: the milestone membership read, the ground-truth PR read,
// and the merge itself — twice, to prove a redelivery merges nothing.
func TestService_AutoMergeIssuesTheSquashMergeOnce(t *testing.T) {
	h := newServiceHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	// The milestone's open agent-work issues.
	h.stub.On(http.MethodGet, "/repos/acme/widgets/issues", http.StatusOK,
		`[{"number":12,"state":"open","labels":[{"name":"aep"}]}]`)
	// Ground truth: open before the merge, merged afterwards.
	h.stub.OnSequence(http.MethodGet, "/repos/acme/widgets/pulls/42",
		gittest.Response{Status: http.StatusOK, Body: `{"state":"open","merged":false}`},
		gittest.Response{Status: http.StatusOK, Body: `{"state":"closed","merged":true,"merge_commit_sha":"abc123def456789"}`},
	)
	h.stub.On(http.MethodPut, "/repos/acme/widgets/pulls/42/merge", http.StatusOK, `{"merged":true}`)

	payload := prBody("opened", "aep/m7-c1", "Resolves #12", 42, false, false, "")
	for i := 0; i < 2; i++ {
		if err := h.deliver(t, "pull_request", payload); err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
	}

	if got := h.countRequests(http.MethodPut, "/repos/acme/widgets/pulls/42/merge"); got != 1 {
		t.Fatalf("the merge must be issued exactly once across a delivery and its redelivery, got %d", got)
	}
	// The milestone read must be scoped to the milestone and to open issues, or
	// the predicate would be decided against the wrong population.
	milestoneRead := false
	for _, r := range h.stub.Requests() {
		if r.Method == http.MethodGet && r.Path == "/repos/acme/widgets/issues" {
			milestoneRead = true
			for _, want := range []string{"milestone=7", "state=open"} {
				if !strings.Contains(r.Query, want) {
					t.Fatalf("milestone read query %q must carry %q", r.Query, want)
				}
			}
			// And it must NOT narrow by label. This endpoint's `labels` is AND, so
			// any value here excludes some population the policy accepts — it used
			// to send `labels=aep`, which hid the milestone's validation task (then
			// unarmed) and left every validation pull request unmerged. Adding a
			// second label would not have fixed it either: AND demands an issue
			// carrying both, and matches nothing. The label decision belongs to
			// decideAutoMerge, over the labels this read returns.
			if strings.Contains(r.Query, "labels=") {
				t.Fatalf("milestone read query %q must not narrow by label — that decision is the policy's", r.Query)
			}
			break
		}
	}
	// Nothing above fails when the read never happened — the loop simply matches
	// no request and every assertion inside it is skipped. So a read that moved to
	// another path would leave the label guard, which is the whole reason these
	// assertions exist, silently testing nothing.
	if !milestoneRead {
		t.Fatal("the merge policy must read the milestone's issues; no such request was made")
	}
}

// TestService_FixIssueIsMintedOnce proves minting's idempotency where it
// actually lives: the DedupeKey becomes a label, the service lists open issues
// carrying it, and the second attempt returns the first issue instead of
// filing another.
func TestService_FixIssueIsMintedOnce(t *testing.T) {
	h := newServiceHarness(t, aRun("run-1", 7, delivery.RunStateRunning))
	cycle := aCycle("cycle-1", "run-1")
	cycle.MergeSHA = testMergeSHA
	h.cycles.latest = cycle
	// The re-trigger budget is already spent, so a red terminal mints.
	h.builds.runs["order-service"] = []BuildRun{
		{Name: delivery.BuildRunName(testProject, "order-service", testMergeSHA, 1), Completed: true},
		{Name: delivery.BuildRunName(testProject, "order-service", testMergeSHA, 2), Completed: true},
	}

	// The dedupe read, served from what the stub has actually been asked to
	// create: empty until an issue carrying the queried dedupe label exists,
	// which is exactly how GitHub answers once one has been filed.
	h.stub.OnFunc(http.MethodGet, "/repos/acme/widgets/issues", func(w http.ResponseWriter, r *http.Request) {
		body := "[]"
		for _, prior := range h.stub.Requests() {
			if prior.Method != http.MethodPost || prior.Path != "/repos/acme/widgets/issues" {
				continue
			}
			for _, label := range dedupeLabelsIn(prior.Body) {
				if strings.Contains(r.URL.RawQuery, label) {
					body = fmt.Sprintf(`[{"number":501,"state":"open","labels":[{"name":%q}]}]`, label)
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	})
	// Labels are ensured before the issue is created (GitHub silently drops
	// unknown ones).
	h.stub.On(http.MethodPost, "/repos/acme/widgets/labels", http.StatusCreated, `{}`)
	h.stub.On(http.MethodPost, "/repos/acme/widgets/issues", http.StatusCreated,
		`{"number":501,"html_url":"https://github.com/acme/widgets/issues/501"}`)

	red := terminal("order-service", false, "step docker-build failed: exit 2")
	for i := 0; i < 2; i++ {
		if err := h.events.OnBuildTerminal(context.Background(), red); err != nil {
			t.Fatalf("terminal %d: %v", i, err)
		}
	}

	if got := h.countRequests(http.MethodPost, "/repos/acme/widgets/issues"); got != 1 {
		t.Fatalf("the fix issue must be filed exactly once across two terminals, got %d", got)
	}
	// The created issue must carry the milestone NUMBER (a title 422s) and the
	// agent-work label, and must NOT carry the aep-api-only dedupe key.
	for _, r := range h.stub.Requests() {
		if r.Method == http.MethodPost && r.Path == "/repos/acme/widgets/issues" {
			if !strings.Contains(r.Body, `"milestone":7`) {
				t.Fatalf("the issue must be assigned to milestone 7 at creation, got %s", r.Body)
			}
			if strings.Contains(r.Body, "dedupeKey") {
				t.Fatalf("the dedupe key is aep-api-only and must never reach GitHub, got %s", r.Body)
			}
			if !strings.Contains(r.Body, `"aep"`) {
				t.Fatalf("the fix issue must be labelled agent work, got %s", r.Body)
			}
		}
	}
}

// dedupeLabelsIn pulls the derived dedupe labels out of a create-issue request
// body — the lossy transform of a DedupeKey the issue service files under.
func dedupeLabelsIn(body string) []string {
	var out []string
	rest := body
	for {
		i := strings.Index(rest, `"dedupe:`)
		if i < 0 {
			return out
		}
		rest = rest[i+1:]
		j := strings.Index(rest, `"`)
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		rest = rest[j:]
	}
}
