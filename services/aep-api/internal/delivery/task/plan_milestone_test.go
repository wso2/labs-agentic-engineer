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
// REAL sourcecontrol.IssueService at the org's pod (the in-memory
// aestudiotest.Fake). It exists to pin the CALL SHAPE of a plan — the N of
// the plan path's 1+N call budget, against GitHub's
// 80-content-requests-per-minute ceiling — which no fake IssueClient can prove.

package task

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// stubRepoRepo resolves every project to github.com/acme/widgets, so every
// pod call addresses acme/widgets.
type stubRepoRepo struct{}

func (stubRepoRepo) GetByOrgAndProjectID(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return &sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/acme/widgets"}, nil
}
func (stubRepoRepo) FindInOrgByFullName(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}

func (stubRepoRepo) GetByOrgAndSlug(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (stubRepoRepo) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (stubRepoRepo) SetWebhookIDIfReady(context.Context, string, string, int64) (bool, error) {
	panic("not used")
}
func (stubRepoRepo) ClearWebhookIDs(context.Context, string) error { panic("not used") }
func (stubRepoRepo) SetStatusIf(context.Context, string, string, string, string) (bool, error) {
	panic("not used")
}
func (stubRepoRepo) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (stubRepoRepo) Create(context.Context, *sourcecontrol.GitRepository) error    { return nil }
func (stubRepoRepo) Update(context.Context, *sourcecontrol.GitRepository) error    { return nil }
func (stubRepoRepo) DeleteByOrgAndProjectID(context.Context, string, string) error { return nil }

// A plan of N Tasks costs N issue creates: the milestone rides each CREATE, so
// there is no follow-up edit, and the one shared `aep` label is ensured once
// for the whole batch rather than once per issue.
func TestPlanTap_AgainstRealIssueService_CostsOneCallPerTask(t *testing.T) {
	pod := aestudiotest.New()
	widgets := sourcecontrol.RepoRef{Org: "org1", Owner: "acme", Repo: "widgets"}
	for i := 1; i <= 9; i++ {
		if _, err := pod.CreateMilestone(context.Background(), widgets, sourcecontrol.CreateMilestoneRequest{Title: "v" + strconv.Itoa(i)}); err != nil {
			t.Fatal(err)
		}
	}
	before := len(pod.Calls())

	issues := sourcecontrol.NewIssueService(stubRepoRepo{}, pod)
	tap := newPlanTap(context.Background(), "org1", "proj1", issues, delivery.NewIssueWriter(issues))
	tap.milestone = 9
	tap.appPaths = map[string]string{"user-service": "src/user", "order-service": "src/order"}

	if err := tap.Stream(turn(
		taskOp(planOK("user-service", "Implement user-service", nil)),
		taskOp(planOK("order-service", "Implement order-service", []string{"user-service"})),
		taskOp(planOK("cart-service", "Implement cart-service", []string{"order-service"})),
	), noAbort); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if tap.failures != 0 {
		t.Fatalf("plan reported %d write failures", tap.failures)
	}

	// N creates, and NOTHING else that costs a content-generating request per
	// task: no create-then-edit, no per-issue label ensure.
	var creates, labelEnsures int
	for _, c := range pod.Calls()[before:] {
		switch c.Op {
		case aestudiotest.OpCreateIssue:
			creates++
		case aestudiotest.OpEnsureLabel:
			labelEnsures++
		default:
			t.Errorf("unexpected pod call %s — a plan only creates issues", c.Op)
		}
	}
	if creates != 3 {
		t.Errorf("issue creates = %d, want 3 (one per planned Task)", creates)
	}
	// One ensure per DISTINCT label in the batch's vocabulary, memoised per repo —
	// two here (the arming label and the `development` kind), not two per task.
	// The number that must never scale with the plan is this one.
	if labelEnsures != 2 {
		t.Errorf("label ensures = %d, want 2 for the whole batch (one per distinct label)", labelEnsures)
	}

	// Every create carries the milestone NUMBER, the arming label and the kind,
	// and no machine block.
	members, err := pod.ListMilestoneIssues(context.Background(), widgets, sourcecontrol.MilestoneIssuesFilter{Number: 9})
	if err != nil || len(members) != 3 {
		t.Fatalf("milestone 9 members = %+v err=%v, want the 3 planned Tasks", members, err)
	}
	for _, is := range members {
		if !slices.Equal(is.Labels, []string{"aep", "development"}) {
			t.Errorf("%q: labels = %v, want [aep development]", is.Title, is.Labels)
		}
		if strings.Contains(is.Body, "aep:task/v1") {
			t.Errorf("%q: body still carries a machine block:\n%s", is.Title, is.Body)
		}
	}

	// The middle Task's dependency resolved to the issue the first create
	// returned — the reference the agent follows.
	if !strings.Contains(members[1].Body, `Depends on #1`) {
		t.Errorf("order-service body lost its resolved dependency:\n%s", members[1].Body)
	}
}
