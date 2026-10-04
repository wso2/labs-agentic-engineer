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

package edge_test

// Review Focus 1: every git-backed /api/v1 op, driven through the REAL
// production handler chain over its REAL service and the in-memory AE Studio
// pod failing for the org, answers the AE Studio code — never the 500 a
// handler's fallback would flatten it into. Only the out-of-process edges
// (repository rows, the OpenChoreo project client, the execution ledger) are
// stubbed.

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/delivery"
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/projects"
	projectshttpapi "github.com/wso2/aep/aep-api/internal/projects/httpapi"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	schttpapi "github.com/wso2/aep/aep-api/internal/sourcecontrol/httpapi"
	"github.com/wso2/aep/aep-api/internal/spec"
	spechttpapi "github.com/wso2/aep/aep-api/internal/spec/httpapi"
)

const testOrg = "default"

// projectRow is project p's repository; skillsRow the org skills repo's.
var (
	projectRow = sourcecontrol.GitRepository{OrgID: testOrg, ProjectID: "p", Status: "ready",
		RepoURL: "https://github.com/acme/p", DefaultBranch: "main"}
	skillsRow = sourcecontrol.GitRepository{OrgID: testOrg, ProjectID: spec.SkillsRepoProject, Status: "ready",
		RepoURL: "https://github.com/acme/skills", DefaultBranch: "main"}
)

// repoRows is the repository-row table: every lookup answers the row for the
// project asked about, and an empty table answers none (project create).
type repoRows struct {
	rows map[string]sourcecontrol.GitRepository
}

var _ sourcecontrol.RepoRepository = repoRows{}

func (r repoRows) GetByOrgAndProjectID(_ context.Context, _, projectID string) (*sourcecontrol.GitRepository, error) {
	row, ok := r.rows[projectID]
	if !ok {
		return nil, nil
	}
	return &row, nil
}
func (repoRows) GetByOrgAndSlug(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, sourcecontrol.ErrRepoNotFound
}
func (repoRows) FindInOrgByFullName(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (repoRows) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) { return nil, nil }
func (repoRows) ListAll(context.Context) ([]sourcecontrol.GitRepository, error)      { return nil, nil }
func (repoRows) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (repoRows) Create(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (repoRows) Update(context.Context, *sourcecontrol.GitRepository) error { return nil }
func (repoRows) DeleteByOrgAndProjectID(context.Context, string, string) error {
	return nil
}

// fixedOwner is the org's connected GitHub owner.
type fixedOwner struct{}

func (fixedOwner) GitHubOwner(context.Context, string) (string, error) { return "acme", nil }

// noExecutions is an empty execution ledger.
type noExecutions struct{}

func (noExecutions) LatestPerKindScoped(context.Context, string, string, int) (map[string]*delivery.Execution, error) {
	return nil, nil
}
func (noExecutions) LatestPerKindForRepoScoped(context.Context, string, string) (map[int]map[string]*delivery.Execution, error) {
	return nil, nil
}
func (noExecutions) ListByIssueScoped(context.Context, string, string, int) ([]delivery.Execution, error) {
	return nil, nil
}

// unusedCommitter is the design write surface; the design read fails first.
type unusedCommitter struct{ t *testing.T }

func (c unusedCommitter) ReadFile(context.Context, string, string, string) (string, string, bool, error) {
	c.t.Error("design write reached ReadFile past a failing design read")
	return "", "", false, nil
}
func (c unusedCommitter) Commit(context.Context, string, string, []spec.DesignFileWrite, string) error {
	c.t.Error("design write reached Commit past a failing design read")
	return nil
}

// newEdgeWithFake assembles the real handler over real services and the
// in-memory pod; arrange fails it for the org.
func newEdgeWithFake(t *testing.T, arrange func(*aestudiotest.Fake)) *componenttest.Harness {
	t.Helper()
	pod := aestudiotest.New()
	for _, row := range []sourcecontrol.GitRepository{projectRow, skillsRow} {
		ref, err := sourcecontrol.RefForRow(testOrg, &row)
		if err != nil {
			t.Fatal(err)
		}
		pod.SeedRepo(ref, map[string]string{"README.md": "seed\n"})
	}
	arrange(pod)

	rows := repoRows{rows: map[string]sourcecontrol.GitRepository{"p": projectRow, spec.SkillsRepoProject: skillsRow}}
	repoSvc := sourcecontrol.NewRepoService(rows, pod, fixedOwner{}, "private")
	issueSvc := sourcecontrol.NewIssueService(rows, pod)

	artifacts := spec.NewArtifactService(rows, pod)
	design := spec.NewDesignService(spec.NewArtifactStore(artifacts), artifacts)
	design.SetFileCommitter(unusedCommitter{t: t})
	skills := spec.NewSkillService(pod, pod, repoSvc, nil)
	specH, err := spechttpapi.New(spec.Deps{
		References: pod, Repos: rows, Artifacts: artifacts,
		Skills: skills, SkillMut: spec.NewSkillMutationService(skills), SkillImport: spec.NewSkillImportService(skills),
		Design: design,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Project create: no row yet, so the repo create reaches the pod.
	oc := &mocks.ProjectClientMock{
		CreateProjectFunc: func(_ context.Context, _ string, req *gen.CreateProjectRequest) (*gen.Project, error) {
			return &gen.Project{Name: req.Name}, nil
		},
		DeleteProjectFunc: func(context.Context, string, string) error { return nil },
	}
	createRepos := sourcecontrol.NewRepoService(repoRows{}, pod, fixedOwner{}, "private")
	projH, err := projectshttpapi.New(projects.Deps{ProjectSvc: projects.NewProjectService(oc, createRepos, nil, artifacts, nil)})
	if err != nil {
		t.Fatal(err)
	}
	scH, err := schttpapi.New(sourcecontrol.Deps{Issues: issueSvc})
	if err != nil {
		t.Fatal(err)
	}
	delH, err := deliveryhttpapi.New(deliveryhttpapi.Deps{TaskReads: task.NewReads(issueSvc, repoSvc, noExecutions{}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Spec: specH, Projects: projH, SourceControl: scH, Delivery: delH,
	}})
}

// referencesUpload is a one-document multipart body for the references op.
func referencesUpload(t *testing.T) (string, []byte) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("files", "brief.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("# brief\n"))
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return mw.FormDataContentType(), buf.Bytes()
}

// do issues one git-backed op as an authenticated member of the org.
func do(t *testing.T, h *componenttest.Harness, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	if body == "multipart" {
		ct, raw := referencesUpload(t)
		return h.AsOrg(testOrg).PostRaw(path, ct, raw)
	}
	req := h.AsOrg(testOrg)
	switch method {
	case http.MethodGet:
		return req.Get(path)
	case http.MethodPost:
		return req.Post(path, body)
	}
	t.Fatalf("unsupported method %s", method)
	return nil
}

const skillMD = "---\nname: my-skill\ndescription: Does a thing when asked to.\n---\n\nBody.\n"

func TestGitBackedOps_MapAEStudioErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"tags", http.MethodGet, "/api/v1/projects/p/tags", ""},
		{"create", http.MethodPost, "/api/v1/projects", `{"name":"p"}`},
		{"references", http.MethodPost, "/api/v1/projects/p/references", "multipart"},
		{"skills", http.MethodGet, "/api/v1/skills", ""},
		{"skills-save", http.MethodPost, "/api/v1/skills", `{"name":"my-skill","skillMd":` + jsonString(skillMD) + `}`},
		{"issues", http.MethodGet, "/api/v1/projects/p/issues", ""},
		{"task", http.MethodGet, "/api/v1/projects/p/tasks/1", ""},
		// T-4.17.1: the design write that goes through the committed-truth surface.
		{"design-save", http.MethodPost, "/api/v1/projects/p/dependencies/payments/assumption", `{}`},
	} {
		for _, f := range []struct {
			err    error
			status int
			code   string
		}{
			{sourcecontrol.ErrAEStudioAbsent, http.StatusConflict, "github_not_connected"},
			{sourcecontrol.ErrAEStudioUnavailable, http.StatusServiceUnavailable, "ae_studio_unavailable"},
			{sourcecontrol.ErrAEStudioMisconfigured, http.StatusServiceUnavailable, "ae_studio_misconfigured"},
		} {
			t.Run(tc.name+"/"+f.code, func(t *testing.T) {
				t.Parallel()
				h := newEdgeWithFake(t, func(fk *aestudiotest.Fake) { fk.FailOrg(testOrg, f.err) })
				rec := do(t, h, tc.method, tc.path, tc.body)
				if rec.Code != f.status {
					t.Fatalf("%s: status %d, want %d\n%s", tc.name, rec.Code, f.status, rec.Body.String())
				}
				if got := componenttest.DecodeEnvelope(t, rec.Body.String()).Code; got != f.code {
					t.Errorf("%s: code %q, want %q", tc.name, got, f.code)
				}
				if want := map[string]string{"ae_studio_unavailable": "5"}[f.code]; rec.Header().Get("Retry-After") != want {
					t.Errorf("%s/%s: Retry-After = %q, want %q (none on ae_studio_misconfigured, C3)",
						tc.name, f.code, rec.Header().Get("Retry-After"), want)
				}
			})
		}
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// The same rig with a serving pod: the table above is meaningful only if
// these ops reach the pod and answer normally when it serves — and a missing
// issue is still the Task's 404, not an AE Studio answer.
func TestGitBackedOps_HealthyPodAnswers(t *testing.T) {
	t.Parallel()
	h := newEdgeWithFake(t, func(*aestudiotest.Fake) {})
	for _, tc := range []struct {
		name, path string
		status     int
	}{
		{"tags", "/api/v1/projects/p/tags", http.StatusOK},
		{"skills", "/api/v1/skills", http.StatusOK},
		{"issues", "/api/v1/projects/p/issues", http.StatusOK},
		{"task", "/api/v1/projects/p/tasks/1", http.StatusNotFound},
	} {
		if rec := do(t, h, http.MethodGet, tc.path, ""); rec.Code != tc.status {
			t.Errorf("%s: status %d, want %d\n%s", tc.name, rec.Code, tc.status, rec.Body.String())
		}
	}
}
