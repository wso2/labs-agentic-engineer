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

package sourcecontrol_test

// Shared fakes for the sourcecontrol unit tier. Fakes sit only at the real
// edges of these services — the GitHub owner lookup (OwnerLookup) and the
// persistence seam (sourcecontrol.RepoRepository); the org's pod is the
// in-memory aestudiotest.Fake. No service is mocked.
//
// These tests live in the external sourcecontrol_test package (not white-box
// sourcecontrol): aestudiotest imports sourcecontrol, so a white-box test
// would form an import cycle. Unexported-helper tests (slugifyProjectName)
// stay white-box in repo_internal_test.go.

import (
	"context"
	"sync"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ---- owner seam ------------------------------------------------------------

// fakeOwners answers owner for every org, unless err is set.
type fakeOwners struct {
	owner string
	err   error
}

func (f fakeOwners) GitHubOwner(context.Context, string) (string, error) {
	return f.owner, f.err
}

var _ sourcecontrol.OwnerLookup = fakeOwners{}

// ---- persistence seam ------------------------------------------------------

// fakeRepoRepo is an in-memory sourcecontrol.RepoRepository keyed on
// (orgID, projectID). It mimics gorm's copy-in/copy-out semantics — Get returns
// a fresh copy so callers can't alias stored state — which matters for the
// async performClone path (Get → mutate → Update). `updates` counts persisted
// Updates so a test can prove a status write actually happened.
type fakeRepoRepo struct {
	mu      sync.Mutex
	rows    map[string]*sourcecontrol.GitRepository
	updates int

	getErr    error // injected into GetByOrgAndProjectID
	createErr error // injected into Create
}

func newFakeRepoRepo() *fakeRepoRepo {
	return &fakeRepoRepo{rows: map[string]*sourcecontrol.GitRepository{}}
}

func repoKey(orgID, projectID string) string { return orgID + "|" + projectID }

func (f *fakeRepoRepo) put(r *sourcecontrol.GitRepository) {
	cp := *r
	f.rows[repoKey(r.OrgID, r.ProjectID)] = &cp
}

func (f *fakeRepoRepo) GetByOrgAndProjectID(_ context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	r, ok := f.rows[repoKey(orgID, projectID)]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (f *fakeRepoRepo) FindInOrgByFullName(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}

func (f *fakeRepoRepo) GetByOrgAndSlug(_ context.Context, orgID, repoSlug string) (*sourcecontrol.GitRepository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == orgID && r.RepoSlug == repoSlug {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeRepoRepo) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sourcecontrol.GitRepository
	for _, r := range f.rows {
		if r.Status == "ready" {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakeRepoRepo) ListByOrg(_ context.Context, ocOrgID string) ([]sourcecontrol.GitRepository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sourcecontrol.GitRepository
	for _, r := range f.rows {
		if r.OrgID == ocOrgID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakeRepoRepo) Create(_ context.Context, repo *sourcecontrol.GitRepository) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.put(repo)
	return nil
}

func (f *fakeRepoRepo) Update(_ context.Context, repo *sourcecontrol.GitRepository) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.put(repo)
	f.updates++
	return nil
}

func (f *fakeRepoRepo) SetWebhookIDIfReady(_ context.Context, orgID, projectID string, hookID int64) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[repoKey(orgID, projectID)]
	if !ok || r.Status != sourcecontrol.RepoStatusReady {
		return false, nil
	}
	id := hookID
	r.WebhookID = &id
	return true, nil
}

func (f *fakeRepoRepo) ClearWebhookIDs(_ context.Context, orgID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.rows {
		if r.OrgID == orgID {
			r.WebhookID = nil
		}
	}
	return nil
}

func (f *fakeRepoRepo) SetStatusIf(_ context.Context, orgID, projectID, from, to string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.rows[repoKey(orgID, projectID)]
	if !ok || r.Status != from {
		return false, nil
	}
	r.Status = to
	return true, nil
}

func (f *fakeRepoRepo) DeleteByOrgAndProjectID(_ context.Context, orgID, projectID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, repoKey(orgID, projectID))
	return nil
}

// preload seeds a row directly (bypassing Create's semantics) so tests can
// arrange existing repo state.
func (f *fakeRepoRepo) preload(rows ...*sourcecontrol.GitRepository) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range rows {
		f.put(r)
	}
}

var _ sourcecontrol.RepoRepository = (*fakeRepoRepo)(nil)

// ---- shared helpers --------------------------------------------------------

func testContext() context.Context { return context.Background() }
