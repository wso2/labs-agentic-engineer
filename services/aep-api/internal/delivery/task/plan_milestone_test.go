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

// SERVICE tier for the plan tap's GitHub half: the REAL plan tap driving the
// REAL sourcecontrol.IssueService through the REAL githubhost client at a
// gittest.Stub. It exists to pin the WIRE SHAPE of a plan — the N of the plan
// path's 1+N call budget, against GitHub's 80-content-requests-per-minute
// ceiling — which no fake IssueClient can prove.

package task

import (
	"context"
	"encoding/json"
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
)

type stubCred struct{}

func (stubCred) Token(context.Context) (string, time.Time, error) {
	return "test-token", time.Time{}, nil
}
func (stubCred) Identity() secrets.Identity               { return secrets.Identity{} }
func (stubCred) RepoOwner() string                        { return "acme" }
func (stubCred) WebhookStrategy() secrets.WebhookStrategy { return secrets.WebhookPerRepo }

type stubResolver struct{}

func (stubResolver) Resolve(context.Context, string) (secrets.Credential, error) {
	return stubCred{}, nil
}

// stubRepoRepo resolves every project to github.com/acme/widgets, so every REST
// call lands under /repos/acme/widgets on the stub.
type stubRepoRepo struct{}

func (stubRepoRepo) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/acme/widgets"}, nil
}
func (stubRepoRepo) GetByOrgAndSlug(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (stubRepoRepo) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (stubRepoRepo) ListAll(context.Context) ([]sourcecontrol.GitRepository, error) { return nil, nil }
func (stubRepoRepo) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (stubRepoRepo) Create(context.Context, *sourcecontrol.GitRepository) error    { return nil }
func (stubRepoRepo) Update(context.Context, *sourcecontrol.GitRepository) error    { return nil }
func (stubRepoRepo) DeleteByOrgAndProjectID(context.Context, string, string) error { return nil }

// A plan of N Tasks costs N issue creates: the milestone rides each CREATE, so
// there is no follow-up PATCH, and the one shared `aep` label is ensured once
// for the whole batch rather than once per issue.
func TestPlanTap_AgainstRealIssueService_CostsOneCallPerTask(t *testing.T) {
	stub := gittest.NewStub(t)
	var created int
	stub.OnFunc(http.MethodPost, "/repos/acme/widgets/issues", func(w http.ResponseWriter, r *http.Request) {
		created++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, fmt.Sprintf(
			`{"number":%d,"html_url":"https://github.com/acme/widgets/issues/%d","node_id":"N%d"}`,
			created, created, created))
	})
	stub.On(http.MethodPost, "/repos/acme/widgets/labels", http.StatusCreated, `{}`)

	issues := sourcecontrol.NewIssueService(stubRepoRepo{},
		githubclient.NewClient(githubclient.WithAPIBase(stub.URL)), stubResolver{})
	tap := newPlanTap(context.Background(), "org1", "proj1", issues, delivery.NewIssueWriter(issues))
	tap.milestone = 9
	tap.appPaths = map[string]string{"user-service": "src/user", "order-service": "src/order"}

	var sink strings.Builder
	tap.Stream(stream(
		toolResult(planOK("user-service", "Implement user-service", nil)),
		toolResult(planOK("order-service", "Implement order-service", []string{"user-service"})),
		toolResult(planOK("cart-service", "Implement cart-service", []string{"order-service"})),
		"data: [DONE]\n\n",
	), &sink, func() {})

	if tap.failures != 0 {
		t.Fatalf("plan reported %d write failures", tap.failures)
	}

	// N creates, and NOTHING else that costs a content-generating request per
	// task: no create-then-patch, no per-issue label ensure.
	var creates, patches, labelEnsures int
	for _, r := range stub.Requests() {
		switch {
		case r.Method == http.MethodPost && r.Path == "/repos/acme/widgets/issues":
			creates++
		case r.Method == http.MethodPatch && strings.HasPrefix(r.Path, "/repos/acme/widgets/issues/"):
			patches++
		case r.Method == http.MethodPost && r.Path == "/repos/acme/widgets/labels":
			labelEnsures++
		default:
			t.Errorf("unexpected request %s %s — a plan touches only issues", r.Method, r.Path)
		}
	}
	if creates != 3 {
		t.Errorf("issue creates = %d, want 3 (one per planned Task)", creates)
	}
	if patches != 0 {
		t.Errorf("issue PATCHes = %d, want 0 — the milestone rides the create", patches)
	}
	// One ensure per DISTINCT label in the batch's vocabulary, memoised per repo —
	// two here (the arming label and the `development` kind), not two per task.
	// The number that must never scale with the plan is this one.
	if labelEnsures != 2 {
		t.Errorf("label ensures = %d, want 2 for the whole batch (one per distinct label)", labelEnsures)
	}

	// Every create carries the milestone NUMBER, the arming label and the kind.
	for _, r := range stub.Requests() {
		if r.Method != http.MethodPost || r.Path != "/repos/acme/widgets/issues" {
			continue
		}
		var body struct {
			Title     string   `json:"title"`
			Body      string   `json:"body"`
			Labels    []string `json:"labels"`
			Milestone *int     `json:"milestone"`
			DedupeKey string   `json:"dedupeKey"`
		}
		if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
			t.Fatalf("decode create body %q: %v", r.Body, err)
		}
		if body.Milestone == nil || *body.Milestone != 9 {
			t.Errorf("%q: milestone = %v on the wire, want 9", body.Title, body.Milestone)
		}
		if len(body.Labels) != 2 || body.Labels[0] != "aep" || body.Labels[1] != "development" {
			t.Errorf("%q: labels = %v on the wire, want [aep development]", body.Title, body.Labels)
		}
		if body.DedupeKey != "" {
			t.Errorf("%q: dedupeKey reached GitHub — it is an aep-api-only field", body.Title)
		}
		if strings.Contains(body.Body, "aep:task/v1") {
			t.Errorf("%q: body still carries a machine block:\n%s", body.Title, body.Body)
		}
	}

	// The middle Task's dependency resolved to the issue the first create
	// returned — the reference the agent follows.
	second := requestsFor(stub, http.MethodPost, "/repos/acme/widgets/issues")[1]
	if !strings.Contains(second.Body, `Depends on #1`) {
		t.Errorf("order-service body lost its resolved dependency:\n%s", second.Body)
	}
}

func requestsFor(stub *gittest.Stub, method, path string) []gittest.RecordedRequest {
	var out []gittest.RecordedRequest
	for _, r := range stub.Requests() {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}
