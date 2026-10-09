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

// COMPONENT tier: the REAL project service
// behind the REAL production handler chain — global middleware → faked auth at
// the jwt.WithClaims seam → orgensure → contract validation → the deny-by-
// default tenant gate in ENFORCE → strict handlers → mapProjectError — driven
// in-process via the componenttest harness. Only the out-of-process OpenChoreo client is mocked. Error-shape
// assertions mirror the harvested goldens (testdata/harvest/golden/): the 422
// and 404 bodies here are the same RFC-9457 problems the deployed stack
// serves.
//
// External test package: the harness imports api, which imports project — an
// in-package test file would be an import cycle.
package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/projects"
	projectshttpapi "github.com/wso2/aep/aep-api/internal/projects/httpapi"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// mustProjects assembles the projects domain from its Deps or fails the harness
// wiring loudly. Shared by both projects_test component files (the config +
// component harness lives in component_component_test.go).
func mustProjects(h *projectshttpapi.Handlers, err error) *projectshttpapi.Handlers {
	if err != nil {
		panic(err)
	}
	return h
}

// conflictRepoSvc is a minimal sourcecontrol.RepoService whose CreateRepo always
// reports a name conflict; every other method is unreachable from create.
type conflictRepoSvc struct{}

func (conflictRepoSvc) CreateRepo(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, fmt.Errorf("create github repo: %w", sourcecontrol.ErrRepoNameConflict)
}
func (conflictRepoSvc) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (conflictRepoSvc) EnsureBareRepo(context.Context, string, string, string) (*sourcecontrol.GitRepository, error) {
	panic("EnsureBareRepo not expected")
}
func (conflictRepoSvc) GetRepo(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	panic("GetRepo not expected")
}
func (conflictRepoSvc) SetWebhookID(context.Context, string, string, int64) error {
	panic("SetWebhookID not expected")
}
func (conflictRepoSvc) BeginDelete(context.Context, string, string) error { return nil }
func (conflictRepoSvc) AbortDelete(context.Context, string, string) error { return nil }
func (conflictRepoSvc) DeleteRepo(context.Context, string, string) error {
	panic("DeleteRepo not expected")
}

// newProjectHarness assembles the real handler with the REAL project service;
// only the OC client is mocked. The sibling ports (repo, webhook, artifacts,
// tasks) stay nil — CreateProject/DeleteProject treat them as best-effort and
// GetProjectStatus degrades to no-repo, all unit-proven branches.
func newProjectHarness(t *testing.T) (*componenttest.Harness, *ocmocks.ProjectClientMock) {
	t.Helper()
	oc := &ocmocks.ProjectClientMock{}
	svc := projects.NewProjectService(oc, nil, nil, nil, nil)
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{Projects: mustProjects(projectshttpapi.New(projects.Deps{ProjectSvc: svc}))}}), oc
}

func TestProjectComponent_ListAuthedReachesRealService(t *testing.T) {
	t.Parallel()
	h, oc := newProjectHarness(t)
	oc.ListProjectsFunc = func(_ context.Context, orgName string, _ int, _ string) (*gen.ProjectList, error) {
		return &gen.ProjectList{Items: []gen.Project{{Name: "hello-world-api", NamespaceName: orgName}}}, nil
	}

	resp := h.AsOrg("acme").Get("/api/v1/projects")
	if resp.Code != 200 {
		t.Fatalf("authed list: got %d, body=%s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "hello-world-api") {
		t.Fatalf("list body missing item: %s", resp.Body.String())
	}
	// The org the service saw MUST be the token org — OrgHandle is bound by the
	// gate from claims, never from client input.
	calls := oc.ListProjectsCalls()
	if len(calls) != 1 || calls[0].OrgName != "acme" {
		t.Fatalf("service org: got %+v, want one call with org acme", calls)
	}
}

func TestProjectComponent_NoClaimsDeniedByEnforceGate(t *testing.T) {
	t.Parallel()
	h, _ := newProjectHarness(t)

	resp := h.NoAuth().Get("/api/v1/projects")
	if resp.Code != 401 {
		t.Fatalf("no-claims list: want the gate's ENFORCE 401, got %d body=%s", resp.Code, resp.Body.String())
	}
	// This is the GATE's 401, not the JWT middleware's rejection (the harness
	// fakes the verifier away; that path is integration-owned). The deny-by-
	// default gate answers with the flat envelope.
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "unauthorized" || !strings.Contains(e.Message, "authentication required") {
		t.Fatalf("gate 401 envelope shape: got %s", resp.Body.String())
	}
}

func TestProjectComponent_CreateValidationAndHappyPath(t *testing.T) {
	t.Parallel()
	h, oc := newProjectHarness(t)
	oc.CreateProjectFunc = func(_ context.Context, orgName string, req *gen.CreateProjectRequest) (*gen.Project, error) {
		return &gen.Project{Name: req.Name, NamespaceName: orgName}, nil
	}

	// name ABSENT → the contract validator's 400 (the error-model break:
	// schema violations are 400 validation_failed now, not Huma's 422), with
	// the offending property named in details[].
	resp := h.AsOrg("acme").Post("/api/v1/projects", `{}`)
	if resp.Code != 400 {
		t.Fatalf("empty body: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "validation_failed" || len(e.Details) == 0 || !strings.Contains(e.Details[0].Message, "name") {
		t.Fatalf("validation 400 shape: %s", resp.Body.String())
	}
	if len(oc.CreateProjectCalls()) != 0 {
		t.Fatal("schema-invalid request must never reach the service")
	}

	// name PRESENT but empty → the handler guard's 400.
	resp = h.AsOrg("acme").Post("/api/v1/projects", `{"name":""}`)
	if resp.Code != 400 {
		t.Fatalf("empty name: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if e := componenttest.DecodeEnvelope(t, resp.Body.String()); e.Message != "name is required" {
		t.Fatalf("400 message: got %q", e.Message)
	}

	// Valid create → 201 with the project body; exactly one OC call, token org.
	resp = h.AsOrg("acme").Post("/api/v1/projects", `{"name":"web","displayName":"Web"}`)
	if resp.Code != 201 {
		t.Fatalf("create: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	calls := oc.CreateProjectCalls()
	if len(calls) != 1 || calls[0].OrgName != "acme" || calls[0].Req.Name != "web" {
		t.Fatalf("OC create call: got %+v", calls)
	}

	// prompt + repoName are contract fields the console sends (issue #72 /
	// #98): huma must accept them, and they must survive into the service's
	// request. prompt is not consumed yet; repoName overrides the provisioned
	// repo's name (asserted at the service tier).
	resp = h.AsOrg("acme").Post("/api/v1/projects",
		`{"name":"gym","prompt":"a workout tracker","repoName":"gym-repo"}`)
	if resp.Code != 201 {
		t.Fatalf("create with prompt+repoName: want 201, got %d body=%s", resp.Code, resp.Body.String())
	}
	calls = oc.CreateProjectCalls()
	last := calls[len(calls)-1]
	if last.Req.Prompt != "a workout tracker" || last.Req.RepoName != "gym-repo" {
		t.Fatalf("prompt/repoName lost in transit: got %+v", last.Req)
	}

	// repoName that isn't a DNS-label slug → the handler guard's 400 (GitHub
	// would otherwise silently normalize it, diverging from the stored name).
	resp = h.AsOrg("acme").Post("/api/v1/projects", `{"name":"web2","repoName":"Bad Name!"}`)
	if resp.Code != 400 {
		t.Fatalf("bad repoName: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if e := componenttest.DecodeEnvelope(t, resp.Body.String()); !strings.Contains(e.Message, "repoName") {
		t.Fatalf("400 message should name repoName: got %q", e.Message)
	}
}

// A user-chosen repoName that already exists on the Git host must fail the
// whole create with a 409 (and compensate the OC project away) — the no-repo
// limbo would be unrecoverable for that name.
func TestProjectComponent_CreateExplicitRepoNameConflictIs409(t *testing.T) {
	t.Parallel()
	oc := &ocmocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, orgName string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name, NamespaceName: orgName}, nil
		},
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	svc := projects.NewProjectService(oc, conflictRepoSvc{}, nil, nil, nil)
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{Projects: mustProjects(projectshttpapi.New(projects.Deps{ProjectSvc: svc}))}})

	resp := h.AsOrg("acme").Post("/api/v1/projects", `{"name":"gym","repoName":"taken-repo"}`)
	if resp.Code != 409 {
		t.Fatalf("taken repoName: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	e := componenttest.DecodeEnvelope(t, resp.Body.String())
	if e.Code != "conflict" || !strings.Contains(strings.ToLower(e.Message), "repo") {
		t.Fatalf("409 envelope should mention the repository name: got %q", e.Message)
	}
	if n := len(oc.DeleteProjectCalls()); n != 1 {
		t.Fatalf("OC project compensation: DeleteProject called %d times, want 1", n)
	}
}

func TestProjectComponent_GetMapsNotFoundToGoldenProblem(t *testing.T) {
	t.Parallel()
	h, oc := newProjectHarness(t)
	oc.GetProjectFunc = func(context.Context, string, string) (*gen.Project, error) {
		return nil, openchoreo.ErrNotFound
	}

	resp := h.AsOrg("acme").Get("/api/v1/projects/does-not-exist-xyz")
	if resp.Code != 404 {
		t.Fatalf("want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if e := componenttest.DecodeEnvelope(t, resp.Body.String()); e.Code != "not_found" || e.Message != "project not found" {
		t.Fatalf("404 envelope shape: %s", resp.Body.String())
	}
}

// TestProjectComponent_ErrorMapping drives the rest of mapProjectError through
// the real chain: untranslated OC sentinels ride the shared ocerr classifier
// (409/…), feature sentinels map to their statuses (403), and an opaque error
// falls back to an opaque 500 (no internal detail leaked).
func TestProjectComponent_ErrorMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantDetail string
	}{
		{"oc unauthorized → 401", openchoreo.ErrUnauthorized, 401, "invalid or expired token"},
		{"oc conflict → 409", openchoreo.ErrConflict, 409, ""},
		{"oc forbidden → 403", openchoreo.ErrForbidden, 403, "insufficient permissions to perform this action"},
		{"opaque → 500 without leaking internals", errors.New("pg: connection refused"), 500, "internal error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, oc := newProjectHarness(t)
			oc.GetProjectFunc = func(context.Context, string, string) (*gen.Project, error) {
				return nil, tc.err
			}
			resp := h.AsOrg("acme").Get("/api/v1/projects/web")
			if resp.Code != tc.wantStatus {
				t.Fatalf("want %d, got %d body=%s", tc.wantStatus, resp.Code, resp.Body.String())
			}
			e := componenttest.DecodeEnvelope(t, resp.Body.String())
			if tc.wantDetail != "" && e.Message != tc.wantDetail {
				t.Fatalf("message: got %q want %q", e.Message, tc.wantDetail)
			}
			if tc.wantStatus == 500 && strings.Contains(resp.Body.String(), "connection refused") {
				t.Fatalf("500 body leaks internals: %s", resp.Body.String())
			}
		})
	}
}

func TestProjectComponent_GetHappyPath(t *testing.T) {
	t.Parallel()
	h, oc := newProjectHarness(t)
	oc.GetProjectFunc = func(_ context.Context, org, name string) (*gen.Project, error) {
		return &gen.Project{Name: name, NamespaceName: org}, nil
	}
	resp := h.AsOrg("acme").Get("/api/v1/projects/web")
	if resp.Code != 200 || !strings.Contains(resp.Body.String(), `"name":"web"`) {
		t.Fatalf("get: got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestProjectComponent_DeleteReturns204(t *testing.T) {
	t.Parallel()
	h, oc := newProjectHarness(t)
	oc.DeleteProjectFunc = func(context.Context, string, string) error { return nil }

	resp := h.AsOrg("acme").Delete("/api/v1/projects/web")
	if resp.Code != 204 {
		t.Fatalf("delete: want 204, got %d body=%s", resp.Code, resp.Body.String())
	}
	if got := len(oc.DeleteProjectCalls()); got != 1 {
		t.Fatalf("OC delete calls: got %d", got)
	}
}

func TestProjectComponent_StatusWiredThroughRealService(t *testing.T) {
	t.Parallel()
	h, _ := newProjectHarness(t)

	// With nil sibling ports the real service returns the no-repo phase — the
	// full ladder is unit-proven; this proves the route→service→body wiring.
	resp := h.AsOrg("acme").Get("/api/v1/projects/web/status")
	if resp.Code != 200 {
		t.Fatalf("status: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	var st gen.ProjectStatus
	if err := json.Unmarshal(resp.Body.Bytes(), &st); err != nil {
		t.Fatalf("status body: %v\n%s", err, resp.Body.String())
	}
	if st.Phase != "no-repo" {
		t.Fatalf("phase: got %q", st.Phase)
	}
}
