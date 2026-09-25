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

package issues_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
)

type repo struct{ sourcecontrol.RepoRepository }

func (repo) GetByOrgAndProjectID(_ context.Context, org, project string) (*sourcecontrol.GitRepository, error) {
	return &sourcecontrol.GitRepository{OrgID: org, ProjectID: project, RepoURL: "https://github.com/acme/shop"}, nil
}

type resolver struct{ secrets.Resolver }

func (resolver) Resolve(context.Context, string) (secrets.Credential, error) { return nil, nil }

type host struct {
	sourcecontrol.IssueOps
	created sourcecontrol.CreateIssueRequest
}

func (*host) ListIssues(context.Context, string, string, secrets.Credential, []string) ([]sourcecontrol.IssueInfo, error) {
	return nil, nil
}
func (*host) EnsureLabel(context.Context, string, string, secrets.Credential, string, string) error {
	return nil
}
func (h *host) CreateIssue(_ context.Context, _, _ string, _ secrets.Credential, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	h.created = req
	return &sourcecontrol.IssueResult{Number: 42, URL: "https://github.com/acme/shop/issues/42"}, nil
}

func TestSREHandlerUsesTrustedContextAndPreservesOutcome(t *testing.T) {
	gh := &host{}
	h := issues.New(sourcecontrol.NewIssueService(repo{}, gh, resolver{}))
	ctx := sourcecontrol.WithIncidentContext(tenant.WithBoundOrg(context.Background(), "acme"), "alert-1")
	response, err := h.CreateIssue(ctx, gen.CreateIssueRequestObject{ProjectName: "shop", Body: &gen.CreateIssueRequest{
		Title: "timeout", ComponentName: "checkout", ActionStatuses: []*string{nil}, DedupeKey: "spoof",
	}})
	if err != nil {
		t.Fatal(err)
	}
	result := response.(gen.CreateIssue200JSONResponse)
	if result.Number != 42 || result.Classification != "code-level" || result.Adopted || result.AdoptionError == "" {
		t.Fatalf("outcome = %+v", result)
	}
	if gh.created.DedupeKey != "" {
		t.Fatalf("client key reached host: %q", gh.created.DedupeKey)
	}
}

func TestSREHandlerRejectsMissingTrustedIdentity(t *testing.T) {
	h := issues.New(sourcecontrol.NewIssueService(repo{}, &host{}, resolver{}))
	_, err := h.CreateIssue(context.Background(), gen.CreateIssueRequestObject{ProjectName: "shop", Body: &gen.CreateIssueRequest{Title: "timeout", ComponentName: "checkout"}})
	apiError, ok := err.(*apierr.Error)
	if !ok || apiError.Status != 400 {
		t.Fatalf("invalid incident should be 400, got %v", err)
	}
}

// closedDuplicateHost answers the incident lookup with one closed match whose
// closure reason (a human's "duplicate") is not eligible to recur.
type closedDuplicateHost struct{ host }

func (*closedDuplicateHost) ListIssues(context.Context, string, string, secrets.Credential, []string) ([]sourcecontrol.IssueInfo, error) {
	return []sourcecontrol.IssueInfo{{Number: 7, State: "closed", StateReason: "duplicate", Labels: []string{"bug", "incident"}}}, nil
}

func TestSREHandlerReportsIneligibleClosedIncidentAsConflict(t *testing.T) {
	h := issues.New(sourcecontrol.NewIssueService(repo{}, &closedDuplicateHost{}, resolver{}))
	ctx := sourcecontrol.WithIncidentContext(tenant.WithBoundOrg(context.Background(), "acme"), "alert-1")
	_, err := h.CreateIssue(ctx, gen.CreateIssueRequestObject{ProjectName: "shop", Body: &gen.CreateIssueRequest{Title: "timeout", ComponentName: "checkout"}})
	apiError, ok := err.(*apierr.Error)
	if !ok || apiError.Status != 409 {
		t.Fatalf("an ineligible closed incident should be 409, got %v", err)
	}
}
