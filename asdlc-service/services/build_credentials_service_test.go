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

package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/credentials"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// fakeGitSecretClient records CreateGitSecret/DeleteGitSecret calls.
type fakeGitSecretClient struct {
	created   []openchoreo.CreateGitSecretRequest
	deleted   []string
	deleteErr error
	createErr error
}

func (f *fakeGitSecretClient) CreateGitSecret(ctx context.Context, orgNS string, req openchoreo.CreateGitSecretRequest) (*openchoreo.GitSecretInfo, error) {
	f.created = append(f.created, req)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &openchoreo.GitSecretInfo{Name: req.Name, Namespace: orgNS}, nil
}
func (f *fakeGitSecretClient) DeleteGitSecret(ctx context.Context, orgNS, name string) error {
	f.deleted = append(f.deleted, name)
	return f.deleteErr
}
func (f *fakeGitSecretClient) ListGitSecrets(ctx context.Context, orgNS string) ([]*openchoreo.GitSecretInfo, error) {
	return nil, nil
}

// fakeRepoRepo is a minimal in-memory RepoRepository for the
// stage-build-secret tests.
type fakeRepoRepo struct {
	rows map[string]*models.GitRepository // key = ocOrgID + "/" + repoSlug
}

func (f *fakeRepoRepo) GetByProjectID(ctx context.Context, projectID string) (*models.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) GetByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) (*models.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) ListAllReady(context.Context) ([]models.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) GetByOrgAndSlug(ctx context.Context, ocOrgID, repoSlug string) (*models.GitRepository, error) {
	return f.rows[ocOrgID+"/"+repoSlug], nil
}
func (f *fakeRepoRepo) Create(context.Context, *models.GitRepository) error { return nil }
func (f *fakeRepoRepo) Update(context.Context, *models.GitRepository) error { return nil }
func (f *fakeRepoRepo) Delete(context.Context, string) error                { return nil }
func (f *fakeRepoRepo) DeleteAll(context.Context) error                     { return nil }

// fakeResolver dispatches a fixed Credential or returns a fixed error.
type fakeResolver struct {
	cred credentials.Credential
	err  error
}

func (f *fakeResolver) Resolve(ctx context.Context, ocOrgID string) (credentials.Credential, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.cred, nil
}

// fakeCred returns a constant token + expiry.
type fakeCred struct {
	token string
	exp   time.Time
	err   error
}

func (c *fakeCred) Token(context.Context) (string, time.Time, error) {
	return c.token, c.exp, c.err
}
func (c *fakeCred) Identity() credentials.Identity { return credentials.Identity{} }
func (c *fakeCred) RepoOwner() string              { return "" }
func (c *fakeCred) WebhookStrategy() credentials.WebhookStrategy {
	return credentials.WebhookPerRepo
}

const testRunName = "default-greeting-api-1731538100123"

func TestStageBuildSecret_Happy(t *testing.T) {
	repo := &models.GitRepository{
		OrgID:     "default",
		ProjectID: "p1",
		RepoSlug:  "asdlc-repos-myrepo",
	}
	repos := &fakeRepoRepo{rows: map[string]*models.GitRepository{"default/asdlc-repos-myrepo": repo}}
	res := &fakeResolver{cred: &fakeCred{token: "ghs_abc123", exp: time.Now().Add(time.Hour)}}
	gs := &fakeGitSecretClient{}

	svc := NewBuildCredentialsService(repos, res, gs)
	got, err := svc.StageBuildSecret(context.Background(), "default", "asdlc-repos-myrepo", testRunName)
	if err != nil {
		t.Fatalf("StageBuildSecret: %v", err)
	}
	if got.SecretRef != BuildGitSecretName {
		t.Errorf("SecretRef = %q; want %q", got.SecretRef, BuildGitSecretName)
	}
	// Refresh = delete then create with the fresh token.
	if len(gs.deleted) != 1 || gs.deleted[0] != BuildGitSecretName {
		t.Errorf("delete calls = %v; want [%s]", gs.deleted, BuildGitSecretName)
	}
	if len(gs.created) != 1 {
		t.Fatalf("create calls = %d; want 1", len(gs.created))
	}
	c := gs.created[0]
	if c.Name != BuildGitSecretName || c.Token != "ghs_abc123" || c.SecretType != openchoreo.GitSecretBasicAuth {
		t.Errorf("create req = %+v; want name=%s token=ghs_abc123 type=basic-auth", c, BuildGitSecretName)
	}
	if c.Username != "git" { // WebhookPerRepo + empty identity → "git"
		t.Errorf("username = %q; want git", c.Username)
	}
}

// A 404 on the delete leg (first-ever build for the org) must be tolerated —
// the create still proceeds.
func TestStageBuildSecret_DeleteNotFoundTolerated(t *testing.T) {
	repos := &fakeRepoRepo{rows: map[string]*models.GitRepository{
		"default/slug": {OrgID: "default", RepoSlug: "slug"},
	}}
	res := &fakeResolver{cred: &fakeCred{token: "t", exp: time.Now().Add(time.Hour)}}
	gs := &fakeGitSecretClient{deleteErr: openchoreo.ErrNotFound}

	svc := NewBuildCredentialsService(repos, res, gs)
	got, err := svc.StageBuildSecret(context.Background(), "default", "slug", testRunName)
	if err != nil {
		t.Fatalf("StageBuildSecret: %v", err)
	}
	if got.SecretRef != BuildGitSecretName || len(gs.created) != 1 {
		t.Errorf("want create after tolerated 404; SecretRef=%q creates=%d", got.SecretRef, len(gs.created))
	}
}

// A 409 on the create leg (a concurrent same-org build re-created the secret
// between our delete and create) must be tolerated — the secret exists with a
// valid token, so the build proceeds.
func TestStageBuildSecret_CreateConflictTolerated(t *testing.T) {
	repos := &fakeRepoRepo{rows: map[string]*models.GitRepository{
		"default/slug": {OrgID: "default", RepoSlug: "slug"},
	}}
	res := &fakeResolver{cred: &fakeCred{token: "t", exp: time.Now().Add(time.Hour)}}
	gs := &fakeGitSecretClient{createErr: openchoreo.ErrConflict}

	svc := NewBuildCredentialsService(repos, res, gs)
	got, err := svc.StageBuildSecret(context.Background(), "default", "slug", testRunName)
	if err != nil {
		t.Fatalf("StageBuildSecret should tolerate create-409, got: %v", err)
	}
	if got.SecretRef != BuildGitSecretName {
		t.Errorf("SecretRef = %q; want %q", got.SecretRef, BuildGitSecretName)
	}
}

// A non-tolerated provisioning failure (e.g. the dev-cloud platform-api 404 on
// CreateGitSecret, surfaced as ErrNotFound) must NOT block the build — it
// degrades to an empty SecretRef so the build dispatches and clones the
// (public) repo unauthenticated. Private-repo support is tracked by
// wso2-enterprise/wso2cloud#319.
func TestStageBuildSecret_ProvisionFailureDegrades(t *testing.T) {
	repos := &fakeRepoRepo{rows: map[string]*models.GitRepository{
		"default/slug": {OrgID: "default", RepoSlug: "slug"},
	}}
	res := &fakeResolver{cred: &fakeCred{token: "t", exp: time.Now().Add(time.Hour)}}
	gs := &fakeGitSecretClient{createErr: openchoreo.ErrNotFound}

	svc := NewBuildCredentialsService(repos, res, gs)
	got, err := svc.StageBuildSecret(context.Background(), "default", "slug", testRunName)
	if err != nil {
		t.Fatalf("StageBuildSecret should degrade on provision failure, got: %v", err)
	}
	if got.SecretRef != "" {
		t.Errorf("SecretRef = %q; want empty (degraded)", got.SecretRef)
	}
}

// With no git-secret client wired (degraded), provisioning is skipped and an
// empty SecretRef is returned so the build clones unauthenticated.
func TestStageBuildSecret_NilClientDegraded(t *testing.T) {
	repos := &fakeRepoRepo{rows: map[string]*models.GitRepository{
		"default/slug": {OrgID: "default", RepoSlug: "slug"},
	}}
	res := &fakeResolver{cred: &fakeCred{token: "t", exp: time.Now().Add(time.Hour)}}

	svc := NewBuildCredentialsService(repos, res, nil)
	got, err := svc.StageBuildSecret(context.Background(), "default", "slug", testRunName)
	if err != nil {
		t.Fatalf("StageBuildSecret: %v", err)
	}
	if got.SecretRef != "" {
		t.Errorf("SecretRef = %q; want empty (degraded)", got.SecretRef)
	}
}

func TestStageBuildSecret_RepoNotInOrg(t *testing.T) {
	repos := &fakeRepoRepo{rows: map[string]*models.GitRepository{}}
	svc := NewBuildCredentialsService(repos, &fakeResolver{}, nil)
	_, err := svc.StageBuildSecret(context.Background(), "default", "missing-slug", testRunName)
	if !errors.Is(err, ErrRepoNotInOrg) {
		t.Errorf("got %v; want ErrRepoNotInOrg", err)
	}
}

func TestStageBuildSecret_OrgDisconnected(t *testing.T) {
	repos := &fakeRepoRepo{rows: map[string]*models.GitRepository{
		"default/slug": {OrgID: "default", RepoSlug: "slug"},
	}}
	res := &fakeResolver{err: &credentials.OrgNotActiveError{OcOrgID: "default", Status: "disconnected"}}
	svc := NewBuildCredentialsService(repos, res, nil)
	_, err := svc.StageBuildSecret(context.Background(), "default", "slug", testRunName)
	if !errors.Is(err, ErrOrgDisconnected) {
		t.Errorf("got %v; want ErrOrgDisconnected", err)
	}
}

func TestStageBuildSecret_MissingArgs(t *testing.T) {
	svc := NewBuildCredentialsService(&fakeRepoRepo{}, &fakeResolver{}, nil)
	for _, tc := range []struct{ org, slug, wrn string }{
		{"", "slug", testRunName},
		{"default", "", testRunName},
		{"default", "slug", ""},
	} {
		if _, err := svc.StageBuildSecret(context.Background(), tc.org, tc.slug, tc.wrn); err == nil {
			t.Errorf("StageBuildSecret(%q,%q,%q): expected error", tc.org, tc.slug, tc.wrn)
		}
	}
}
