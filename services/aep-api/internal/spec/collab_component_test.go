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

// COMPONENT tier: the REAL collab feature behind
// the REAL production chain — global middleware → faked auth at the
// jwt.WithClaims seam → orgensure → contract validation → the tenant gate
// in ENFORCE → strict handlers (handlers_collab.go) with their inline error
// mapping — driven in-process via the componenttest harness. The
// requirements read/version/save-discard surface
// (get-requirements, discard-requirements, list-requirements-versions,
// get-requirements-at-version) was removed — superseded by the Files API
// (list-files/read-file); only the collab-session + collab-validate surface
// remains.
//
// ORG SCOPE: the active org is derived SOLELY from the token (no {orgHandle}
// path param), so the only runtime auth assertion the tier adds is the gate's
// no-claims 401 on an org-scoped op (proven once for the feature).
//
// GOLDEN FIELD SETS: the harvested goldens carry a Huma-era `$schema` link
// field that the strict handlers never emit, so the on-wire field set is
// compared with $schema excluded from both sides.
//
// External test package: the harness imports api, which imports requirements — an
// in-package test file would be an import cycle.
package spec_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

const specPrefix = "/api/v1/projects/web/spec"

// --- fakes / harness ---------------------------------------------------------

// fakeCollabRepos is the collab project-ownership oracle (sourcecontrol.RepoService).
// Only GetRepo is consulted by the collab handlers; the other methods panic.
type fakeCollabRepos struct {
	GetRepoFunc func(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
}

var _ sourcecontrol.RepoService = (*fakeCollabRepos)(nil)

func (f *fakeCollabRepos) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	panic("fakeCollabRepos: ListByOrg not expected")
}
func (f *fakeCollabRepos) GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
	if f.GetRepoFunc == nil {
		panic("fakeCollabRepos: GetRepo not set")
	}
	return f.GetRepoFunc(ctx, orgID, projectID)
}
func (f *fakeCollabRepos) CreateRepo(context.Context, string, string, string, string) (*sourcecontrol.GitRepository, error) {
	panic("fakeCollabRepos: CreateRepo not expected")
}
func (f *fakeCollabRepos) EnsureBareRepo(context.Context, string, string, string) (*sourcecontrol.GitRepository, error) {
	panic("fakeCollabRepos: EnsureBareRepo not expected")
}
func (f *fakeCollabRepos) SetWebhookID(context.Context, string, string, int64) error {
	panic("fakeCollabRepos: SetWebhookID not expected")
}
func (f *fakeCollabRepos) DeleteRepo(context.Context, string, string) error {
	panic("fakeCollabRepos: DeleteRepo not expected")
}

// newReqHarness assembles the real chain around the REAL collab service.
func newReqHarness(t *testing.T, repos sourcecontrol.RepoService) *componenttest.Harness {
	t.Helper()
	return componenttest.New(t, componenttest.Options{Deps: edge.Deps{
		Spec: mustSpecHandlers(t, spec.Deps{CollabRepo: repos}),
	}})
}

// goldenPath resolves a harvested golden by name.
func goldenPath(name string) string {
	return filepath.Join("..", "..", "testdata", "harvest", "golden", name)
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

// --- collab ------------------------------------------------------------------

func TestReqComponent_CollabSession_HappyMatchesGoldenFieldSet(t *testing.T) {
	t.Parallel()
	repos := &fakeCollabRepos{
		GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return &sourcecontrol.GitRepository{RepoURL: "https://github.com/o/r.git"}, nil
		},
	}
	h := newReqHarness(t, repos)

	resp := h.AsOrg("acme").Get(specPrefix + "/collab-session")
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
		GetRepoFunc: func(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
			return nil, nil // no repo row → project not found
		},
	}
	h := newReqHarness(t, repos)

	resp := h.AsOrg("acme").Get(specPrefix + "/collab-session")
	if resp.Code != 404 {
		t.Fatalf("collab-session unknown project: want 404, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// TestReqComponent_CollabValidate_MissingRoom drives the server-to-server
// route's reachable input-validation branch. validate-collab-access sits
// behind the deny-by-default tenant gate like every strict operation, and the
// handler additionally keeps its own claims check (the Huma-era semantics,
// load-bearing in gate LOG mode); the room-prefix/oracle branches need an
// X-Room-Id header the in-process request builder can't set, so they ride the
// raw-request test below. What is reachable here — an authed request with no
// room → 400 — is pinned.
func TestReqComponent_CollabValidate_MissingRoom(t *testing.T) {
	t.Parallel()
	h := newReqHarness(t, &fakeCollabRepos{})

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
		GetRepoFunc: func(_ context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
			if orgID != "acme" || projectID != "demo-shop" {
				return nil, nil
			}
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	h := newReqHarness(t, repos)

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

// TestReqComponent_CollabValidate_DeniedWithoutDesignView proves the
// permission gate — not just the handler's own org/ownership check — refuses
// a caller who holds no ae:design-view: the gate runs BEFORE the handler, so
// this 403s even though the room/project would otherwise resolve cleanly
// (same repos fake as the success test above).
func TestReqComponent_CollabValidate_DeniedWithoutDesignView(t *testing.T) {
	t.Parallel()
	repos := &fakeCollabRepos{
		GetRepoFunc: func(_ context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
			if orgID != "acme" || projectID != "demo-shop" {
				return nil, nil
			}
			return &sourcecontrol.GitRepository{Status: "ready"}, nil
		},
	}
	h := newReqHarness(t, repos)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/collab/validate", nil)
	key, value := componenttest.ClaimsHeaderWithScope(t, "acme", "openid ae:resource-config")
	req.Header.Set(key, value)
	req.Header.Set("X-Room-Id", "spec-acme-demo-shop")
	rec := httptest.NewRecorder()
	h.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("collab-validate without ae:design-view: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
