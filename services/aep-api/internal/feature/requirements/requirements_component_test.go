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

// COMPONENT tier (bff-component-testing.md §4): the REAL requirements + collab
// services behind the REAL production chain — global middleware → faked auth at
// the jwt.WithClaims seam → orgensure → Huma parsing/validation → the tenant gate
// in ENFORCE → each handler's inline error mapping — driven in-process via the
// componenttest harness. Only the artifact seam is faked (real ArtifactStore
// decorator over FakeArtifactService). Per the GitHub-direct rework
// (docs/design/agents-generation-migration.md §12.2) the per-file PUT/DELETE, the
// generate stream, and the whole requirements-chat surface are gone; what remains
// is the read + version + save/discard + collab surface exercised below.
//
// ORG SCOPE: the active org is derived SOLELY from the token (no {orgHandle}
// path param), so the only runtime auth assertion the tier adds is the gate's
// no-claims 401 on an org-scoped op (proven once for the feature).
//
// GOLDEN FIELD SETS: the harvested goldens carry a Huma `$schema` link field
// that the current handler (api/huma.go: SchemasPath="") no longer emits, so the
// on-wire field set is compared with $schema excluded from both sides.
//
// External test package: the harness imports api, which imports requirements — an
// in-package test file would be an import cycle.
package requirements_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/api"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts/artifactstest"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/feature/requirements"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/models"
)

const reqPrefix = "/api/v1/projects/web/requirements"

// --- fakes / harness ---------------------------------------------------------

// fakeCollabRepos is the collab project-ownership oracle (gitrepo.RepoService).
// Only GetRepo is consulted by the collab handlers; the other methods panic.
type fakeCollabRepos struct {
	GetRepoFunc func(ctx context.Context, orgID, projectID string) (*models.GitRepository, error)
}

var _ gitrepo.RepoService = (*fakeCollabRepos)(nil)

func (f *fakeCollabRepos) ListByOrg(context.Context, string) ([]models.GitRepository, error) {
	panic("fakeCollabRepos: ListByOrg not expected")
}
func (f *fakeCollabRepos) GetRepo(ctx context.Context, orgID, projectID string) (*models.GitRepository, error) {
	if f.GetRepoFunc == nil {
		panic("fakeCollabRepos: GetRepo not set")
	}
	return f.GetRepoFunc(ctx, orgID, projectID)
}
func (f *fakeCollabRepos) CreateRepo(context.Context, string, string, string, string) (*models.GitRepository, error) {
	panic("fakeCollabRepos: CreateRepo not expected")
}
func (f *fakeCollabRepos) EnsureBareRepo(context.Context, string, string, string) (*models.GitRepository, error) {
	panic("fakeCollabRepos: EnsureBareRepo not expected")
}
func (f *fakeCollabRepos) SetWebhookID(context.Context, string, string, int64) error {
	panic("fakeCollabRepos: SetWebhookID not expected")
}
func (f *fakeCollabRepos) DeleteRepo(context.Context, string, string) error {
	panic("fakeCollabRepos: DeleteRepo not expected")
}

// newReqHarness assembles the real chain around the REAL requirements/collab
// services. The store decorator wraps `fake`, which is also the direct artifactSvc
// seam.
func newReqHarness(t *testing.T, fake *artifactstest.FakeArtifactService, repos gitrepo.RepoService) *componenttest.Harness {
	t.Helper()
	store := artifacts.NewArtifactStore(fake)
	return componenttest.New(t, componenttest.Options{Deps: api.HumaDeps{
		RequirementsSvc: requirements.NewRequirementsService(store, fake),
		CollabRepo:      repos,
	}})
}

// goldenPath resolves a harvested golden by name.
func goldenPath(name string) string {
	return filepath.Join("..", "..", "..", "testdata", "harvest", "golden", name)
}

// sansSchema drops the Huma `$schema` link key so a harvested golden's field set
// (which still carries it) compares against the current handler's (which omits it).
func sansSchema(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if k != "$schema" {
			out = append(out, k)
		}
	}
	return out
}

// readFile reads a golden file into bytes, failing the test on error.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// firstElementKeys decodes a JSON array of objects and returns element[0]'s
// sorted keys — the low-maintenance element-shape assertion for array responses
// (componenttest.GoldenFieldSet is object-only).
func firstElementKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("not a JSON array of objects: %v\n%s", err, string(raw))
	}
	if len(arr) == 0 {
		t.Fatalf("expected at least one element, got empty array: %s", string(raw))
	}
	keys := make([]string, 0, len(arr[0]))
	for k := range arr[0] {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- get-requirements --------------------------------------------------------

func TestReqComponent_GetRequirements_MatchesGoldenFieldSet(t *testing.T) {
	t.Parallel()
	files := map[string]string{"requirements.md": "# R\n"}
	fake := &artifactstest.FakeArtifactService{
		ListRequirementFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
		ListRequirementsVersionsFunc: func(context.Context, string, string) ([]artifacts.RequirementsVersionInfo, error) {
			return []artifacts.RequirementsVersionInfo{{Version: 1, Tag: "v1", CommitHash: "abc"}}, nil
		},
		GetRequirementsAtTagFunc: func(context.Context, string, string, string) (map[string]string, error) {
			return map[string]string{"requirements.md": "# R\n"}, nil // equal → no unsaved changes
		},
	}
	h := newReqHarness(t, fake, nil)

	resp := h.AsOrg("acme").Get(reqPrefix)
	if resp.Code != 200 {
		t.Fatalf("get: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	got := sansSchema(componenttest.FieldSet(t, resp.Body.String()))
	want := sansSchema(componenttest.GoldenFieldSet(t, goldenPath("get_requirements.json")))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("get-requirements field set drifted from golden:\n got %v\nwant %v", got, want)
	}
}

func TestReqComponent_GetRequirements_NoClaimsDeniedByEnforceGate(t *testing.T) {
	t.Parallel()
	h := newReqHarness(t, &artifactstest.FakeArtifactService{}, nil)

	resp := h.NoAuth().Get(reqPrefix)
	if resp.Code != 401 {
		t.Fatalf("no-claims: want the gate's ENFORCE 401, got %d body=%s", resp.Code, resp.Body.String())
	}
	// The gate aggregates its resolver error into one RFC-9457 problem carrying
	// "authentication required" in errors[]; this is NOT the JWT middleware's
	// pre-gate rejection (that path is integration-owned — see §3).
	p := componenttest.DecodeProblem(t, resp.Body.String())
	if p.Status != 401 || len(p.Errors) == 0 || !strings.Contains(p.Errors[0].Message, "authentication required") {
		t.Fatalf("gate 401 problem shape: got %s", resp.Body.String())
	}
}

func TestReqComponent_GetRequirements_OpaqueErrorIs500(t *testing.T) {
	t.Parallel()
	fake := &artifactstest.FakeArtifactService{
		ListRequirementFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return nil, errors.New("pg: connection refused")
		},
	}
	h := newReqHarness(t, fake, nil)

	resp := h.AsOrg("acme").Get(reqPrefix)
	if resp.Code != 500 {
		t.Fatalf("opaque error: want 500, got %d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "connection refused") {
		t.Fatalf("500 body leaks internals: %s", resp.Body.String())
	}
}

// --- save / discard ----------------------------------------------------------

func TestReqComponent_SaveAndDiscard_Happy(t *testing.T) {
	t.Parallel()
	fake := &artifactstest.FakeArtifactService{
		SaveRequirementsFunc: func(context.Context, string, string, artifacts.SaveRequest) (*artifacts.RequirementsSaveResult, error) {
			return &artifacts.RequirementsSaveResult{Status: "approved", Tag: "v2", Version: 2}, nil
		},
		DiscardRequirementsFunc: func(context.Context, string, string) (map[string]string, error) {
			return map[string]string{"requirements.md": "# R\n"}, nil
		},
		ListRequirementFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return map[string]string{"requirements.md": "# R\n"}, nil
		},
		ListRequirementsVersionsFunc: func(context.Context, string, string) ([]artifacts.RequirementsVersionInfo, error) {
			return nil, nil
		},
	}
	h := newReqHarness(t, fake, nil)

	if resp := h.AsOrg("acme").Post(reqPrefix+"/save", "{}"); resp.Code != 200 {
		t.Fatalf("save: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if resp := h.AsOrg("acme").Post(reqPrefix+"/discard", "{}"); resp.Code != 200 {
		t.Fatalf("discard: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// The publish flow pins the commit its files-apply just created; the optional
// `{commitSha}` body must reach the artifact save verbatim (and its absence —
// a bare `{}` or no body at all — must mean "resolve HEAD", i.e. empty).
func TestReqComponent_Save_CommitShaBodyPassesThrough(t *testing.T) {
	t.Parallel()
	var got []string
	fake := &artifactstest.FakeArtifactService{
		SaveRequirementsFunc: func(_ context.Context, _, _ string, req artifacts.SaveRequest) (*artifacts.RequirementsSaveResult, error) {
			got = append(got, req.CommitSHA)
			return &artifacts.RequirementsSaveResult{Status: "approved", Tag: "v1", Version: 1}, nil
		},
		ListRequirementFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return map[string]string{"requirements.md": "# R\n"}, nil
		},
		ListRequirementsVersionsFunc: func(context.Context, string, string) ([]artifacts.RequirementsVersionInfo, error) {
			return nil, nil
		},
	}
	h := newReqHarness(t, fake, nil)

	if resp := h.AsOrg("acme").Post(reqPrefix+"/save", `{"commitSha":"c6659d085ea2217c7a8a633f360ab6deabb3cc71"}`); resp.Code != 200 {
		t.Fatalf("save with commitSha: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if resp := h.AsOrg("acme").Post(reqPrefix+"/save", "{}"); resp.Code != 200 {
		t.Fatalf("save with empty body: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	want := []string{"c6659d085ea2217c7a8a633f360ab6deabb3cc71", ""}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("CommitSHA seen by artifact save = %v, want %v", got, want)
	}
}

func TestReqComponent_Save_SpecNotFoundMapsTo404(t *testing.T) {
	t.Parallel()
	fake := &artifactstest.FakeArtifactService{
		SaveRequirementsFunc: func(context.Context, string, string, artifacts.SaveRequest) (*artifacts.RequirementsSaveResult, error) {
			return nil, artifacts.ErrSpecNotFound
		},
	}
	h := newReqHarness(t, fake, nil)

	resp := h.AsOrg("acme").Post(reqPrefix+"/save", "{}")
	if resp.Code != 404 {
		t.Fatalf("save spec-not-found: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeProblem(t, resp.Body.String()); p.Detail != "requirements not found" {
		t.Fatalf("404 detail: got %q", p.Detail)
	}
}

// --- list-versions / get-at-version ------------------------------------------

func TestReqComponent_ListVersions_MatchesGoldenElementShape(t *testing.T) {
	t.Parallel()
	fake := &artifactstest.FakeArtifactService{
		ListRequirementsVersionsFunc: func(context.Context, string, string) ([]artifacts.RequirementsVersionInfo, error) {
			return []artifacts.RequirementsVersionInfo{{Version: 1, Tag: "v1", CommitHash: "abc"}}, nil
		},
	}
	h := newReqHarness(t, fake, nil)

	resp := h.AsOrg("acme").Get(reqPrefix + "/versions")
	if resp.Code != 200 {
		t.Fatalf("list-versions: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	// The golden is a JSON array; compare the element field set (GoldenFieldSet
	// is object-only, so decode the arrays here).
	got := firstElementKeys(t, resp.Body.Bytes())
	want := firstElementKeys(t, readFile(t, goldenPath("get_requirements_versions.json")))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list-versions element shape drifted from golden:\n got %v\nwant %v", got, want)
	}
}

func TestReqComponent_GetAtVersion_HappyAndNotFound(t *testing.T) {
	t.Parallel()
	fake := &artifactstest.FakeArtifactService{
		GetRequirementsAtTagFunc: func(_ context.Context, _, _, tag string) (map[string]string, error) {
			if tag == "v9" {
				return nil, artifacts.ErrArtifactNotFound
			}
			return map[string]string{"requirements.md": "# R\n"}, nil
		},
	}
	h := newReqHarness(t, fake, nil)

	resp := h.AsOrg("acme").Get(reqPrefix + "/versions/v1")
	if resp.Code != 200 {
		t.Fatalf("get-at-version: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	got := sansSchema(componenttest.FieldSet(t, resp.Body.String()))
	want := sansSchema(componenttest.GoldenFieldSet(t, goldenPath("get_requirements_at_version.json")))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("get-at-version field set drifted from golden:\n got %v\nwant %v", got, want)
	}

	// A missing tag → ErrSpecNotFound → 404.
	resp = h.AsOrg("acme").Get(reqPrefix + "/versions/v9")
	if resp.Code != 404 {
		t.Fatalf("get-at-version missing: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// --- collab ------------------------------------------------------------------

func TestReqComponent_CollabSession_HappyMatchesGoldenFieldSet(t *testing.T) {
	t.Parallel()
	repos := &fakeCollabRepos{
		GetRepoFunc: func(context.Context, string, string) (*models.GitRepository, error) {
			return &models.GitRepository{RepoURL: "https://github.com/o/r.git"}, nil
		},
	}
	h := newReqHarness(t, &artifactstest.FakeArtifactService{}, repos)

	resp := h.AsOrg("acme").Get(reqPrefix + "/collab-session")
	if resp.Code != 200 {
		t.Fatalf("collab-session: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	// Field set matches the golden (sans $schema). Values differ from the golden
	// because the display identity is decoded from the Authorization Bearer, which
	// the in-process harness does not forward — so userName/email are empty here
	// (their projection is unit-proven in collab_identity_test.go). The room ID is
	// derived from the token org + project.
	got := sansSchema(componenttest.FieldSet(t, resp.Body.String()))
	want := sansSchema(componenttest.GoldenFieldSet(t, goldenPath("get_collab_session.json")))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collab-session field set drifted from golden:\n got %v\nwant %v", got, want)
	}
	var body struct {
		RoomID string `json:"roomId"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("collab body: %v", err)
	}
	if body.RoomID != "spec-acme-web" {
		t.Fatalf("roomId: got %q, want spec-acme-web (token org + project)", body.RoomID)
	}
}

func TestReqComponent_CollabSession_UnknownProjectIs404(t *testing.T) {
	t.Parallel()
	repos := &fakeCollabRepos{
		GetRepoFunc: func(context.Context, string, string) (*models.GitRepository, error) {
			return nil, nil // no repo row → project not found
		},
	}
	h := newReqHarness(t, &artifactstest.FakeArtifactService{}, repos)

	resp := h.AsOrg("acme").Get(reqPrefix + "/collab-session")
	if resp.Code != 404 {
		t.Fatalf("collab-session unknown project: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// TestReqComponent_CollabValidate_MissingRoom drives the server-to-server
// carve-out's reachable input-validation branch. validate-collab-access is NOT
// org-scoped (no OrgScopedInput), so per §3 its verifier auth is integration-owned
// and NOT asserted as a gate here; the room-prefix/oracle branches need an
// X-Room-Id header the in-process request builder can't set, so they stay
// integration-owned too. What is reachable — an authed request with no room →
// 400 — is pinned.
func TestReqComponent_CollabValidate_MissingRoom(t *testing.T) {
	t.Parallel()
	h := newReqHarness(t, &artifactstest.FakeArtifactService{}, &fakeCollabRepos{})

	resp := h.AsOrg("acme").Get("/api/v1/collab/validate")
	if resp.Code != 400 {
		t.Fatalf("collab-validate no room: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// TestReqComponent_CollabValidate_ReturnsProjectName pins the success path:
// the oracle resolves `spec-<org>-<project>` and the response carries the
// projectName the collab service needs for its seed read (#114). Uses a raw
// request via ClaimsHeader because the Req builder can't set X-Room-Id.
func TestReqComponent_CollabValidate_ReturnsProjectName(t *testing.T) {
	t.Parallel()
	repos := &fakeCollabRepos{
		GetRepoFunc: func(_ context.Context, orgID, projectID string) (*models.GitRepository, error) {
			if orgID != "acme" || projectID != "demo-shop" {
				return nil, nil
			}
			return &models.GitRepository{Status: "ready"}, nil
		},
	}
	h := newReqHarness(t, &artifactstest.FakeArtifactService{}, repos)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/collab/validate", nil)
	key, value := componenttest.ClaimsHeader(t, "acme")
	req.Header.Set(key, value)
	req.Header.Set("X-Room-Id", "spec-acme-demo-shop")
	rec := httptest.NewRecorder()
	h.Handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("collab-validate: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Name        string `json:"name"`
		Email       string `json:"email"`
		ProjectName string `json:"projectName"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("collab-validate: unmarshal: %v body=%s", err, rec.Body.String())
	}
	if body.ProjectName != "demo-shop" {
		t.Fatalf("collab-validate: want projectName demo-shop, got %q body=%s", body.ProjectName, rec.Body.String())
	}
}
