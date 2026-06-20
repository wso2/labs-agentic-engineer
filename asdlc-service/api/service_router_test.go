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

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/middleware"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// fakeRepoRepo is the minimum RepoRepository surface RequireOrgScope needs to
// fence a repo route. It mirrors middleware/org_scope_test.go's fake.
type fakeRepoRepo struct {
	rows map[string]*models.GitRepository // key = orgId+"/"+projectId
}

func (f *fakeRepoRepo) GetByOrgAndProjectID(_ context.Context, orgID, projectID string) (*models.GitRepository, error) {
	return f.rows[orgID+"/"+projectID], nil
}
func (f *fakeRepoRepo) GetByOrgAndSlug(context.Context, string, string) (*models.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) ListAllReady(context.Context) ([]models.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) Create(context.Context, *models.GitRepository) error                { return nil }
func (f *fakeRepoRepo) Update(context.Context, *models.GitRepository) error                { return nil }
func (f *fakeRepoRepo) DeleteByOrgAndProjectID(context.Context, string, string) error      { return nil }
func (f *fakeRepoRepo) DeleteAll(context.Context) error                                    { return nil }

// Compile-time check: if the interface grows a method, this fails the build.
var _ repositories.RepoRepository = (*fakeRepoRepo)(nil)

// fakeRepoController records whether its handler ran, so the test can prove the
// org-scope middleware fenced the request BEFORE the controller was reached.
type fakeRepoController struct {
	getCalled bool
}

func (c *fakeRepoController) CreateRepo(w http.ResponseWriter, r *http.Request) {}
func (c *fakeRepoController) GetRepo(w http.ResponseWriter, r *http.Request) {
	c.getCalled = true
	w.WriteHeader(http.StatusOK)
}
func (c *fakeRepoController) DeleteRepo(w http.ResponseWriter, r *http.Request) {}

// TestServiceRouter_RepoScoped_FencesNonExistentPair proves a representative
// repo route registered through the ServiceRouter (RepoScoped → RequireOrgScope)
// still 404s for a non-existent (org, project) pair — confirming the typed
// wrapper preserves the repo-scope fence rather than just re-routing handlers.
func TestServiceRouter_RepoScoped_FencesNonExistentPair(t *testing.T) {
	repos := &fakeRepoRepo{rows: map[string]*models.GitRepository{
		"org-a/proj-1": {OrgID: "org-a", ProjectID: "proj-1"},
	}}
	mux := http.NewServeMux()
	rt := NewServiceRouter(mux, middleware.RequireOrgScope(repos))

	ctrl := &fakeRepoController{}
	rt.RepoScoped("GET /api/v1/repos/{orgId}/{projectId}", ctrl.GetRepo)

	// (org-x, no-such-project) → RequireOrgScope returns nil row → 404, and the
	// controller handler is never reached.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/repos/org-x/no-such-project", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("non-existent (org,project): want 404, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if ctrl.getCalled {
		t.Fatal("controller handler ran for a non-existent (org,project) — repo-scope fence was bypassed")
	}

	// Sanity: the existing pair passes through to the handler (200), proving the
	// fence allows a valid pair and the RepoScoped wiring is live.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/repos/org-a/proj-1", nil))
	if rec.Code != http.StatusOK || !ctrl.getCalled {
		t.Fatalf("existing (org,project): want 200 with handler reached, got %d (called=%v)", rec.Code, ctrl.getCalled)
	}
}
