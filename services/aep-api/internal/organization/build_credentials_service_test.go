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

package organization

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// fakeRepoRepo is a minimal in-memory RepoRepository for the
// stage-build-secret tests.
type fakeRepoRepo struct {
	rows map[string]*sourcecontrol.GitRepository // key = ocOrgID + "/" + repoSlug
}

func (f *fakeRepoRepo) GetByProjectID(ctx context.Context, projectID string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) GetByOrgAndProjectID(ctx context.Context, ocOrgID, projectID string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) ListAllReady(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) SetWebhookIDIfReady(context.Context, string, string, int64) (bool, error) {
	panic("not used")
}
func (f *fakeRepoRepo) ClearWebhookIDs(context.Context, string) error { panic("not used") }
func (f *fakeRepoRepo) SetStatusIf(context.Context, string, string, string, string) (bool, error) {
	panic("not used")
}
func (f *fakeRepoRepo) ListByOrg(context.Context, string) ([]sourcecontrol.GitRepository, error) {
	panic("fakeRepoRepo: ListByOrg not expected in orgcreds tests")
}
func (f *fakeRepoRepo) ListAll(context.Context) ([]sourcecontrol.GitRepository, error) {
	return nil, nil
}
func (f *fakeRepoRepo) FindInOrgByFullName(context.Context, string, string) (*sourcecontrol.GitRepository, error) {
	return nil, nil
}

func (f *fakeRepoRepo) GetByOrgAndSlug(ctx context.Context, ocOrgID, repoSlug string) (*sourcecontrol.GitRepository, error) {
	return f.rows[ocOrgID+"/"+repoSlug], nil
}
func (f *fakeRepoRepo) Create(context.Context, *sourcecontrol.GitRepository) error    { return nil }
func (f *fakeRepoRepo) Update(context.Context, *sourcecontrol.GitRepository) error    { return nil }
func (f *fakeRepoRepo) Delete(context.Context, string) error                          { return nil }
func (f *fakeRepoRepo) DeleteByOrgAndProjectID(context.Context, string, string) error { return nil }
func (f *fakeRepoRepo) DeleteAll(context.Context) error                               { return nil }

// memRepoBySlug is a fakeRepoRepo holding one active repo, org/slug.
func memRepoBySlug(t *testing.T, ocOrgID, repoSlug string) *fakeRepoRepo {
	t.Helper()
	return &fakeRepoRepo{rows: map[string]*sourcecontrol.GitRepository{
		ocOrgID + "/" + repoSlug: {OrgID: ocOrgID, ProjectID: "p1", RepoSlug: repoSlug},
	}}
}

// fakeOrgSecrets is an OrgSecretRefReader over "org/secret" → reference
// name; a missing key is an unset secret.
type fakeOrgSecrets map[string]string

func (f fakeOrgSecrets) Get(_ context.Context, ocOrgID string, s OrgSecret) (*OrgSecretRef, error) {
	name, ok := f[ocOrgID+"/"+string(s)]
	if !ok {
		return nil, nil
	}
	return &OrgSecretRef{Secret: s, Name: name}, nil
}

// failingOrgSecrets fails every row read.
type failingOrgSecrets struct{ err error }

func (f failingOrgSecrets) Get(context.Context, string, OrgSecret) (*OrgSecretRef, error) {
	return nil, f.err
}

const testRunName = "default-greeting-api-1731538100123"

func TestStageBuildSecret_ReturnsGitpatRefName(t *testing.T) {
	svc := NewBuildCredentialsService(memRepoBySlug(t, "default", "acme-greeter"), fakeOrgSecrets{"default/github-pat": "default-github-pat-3f9a"})
	res, err := svc.StageBuildSecret(context.Background(), "default", "acme-greeter", "run-1")
	if err != nil || res.SecretRef != "default-github-pat-3f9a" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	_, err = NewBuildCredentialsService(memRepoBySlug(t, "default", "acme-greeter"), fakeOrgSecrets{}).StageBuildSecret(context.Background(), "default", "acme-greeter", "run-1")
	if !errors.Is(err, ErrOrgDisconnected) {
		t.Fatalf("err = %v, want ErrOrgDisconnected", err)
	}
}

// Another org's github-pat row is never this org's reference.
func TestStageBuildSecret_ReadsOnlyTheOrgsOwnRow(t *testing.T) {
	svc := NewBuildCredentialsService(memRepoBySlug(t, "default", "slug"), fakeOrgSecrets{"other/github-pat": "other-github-pat-1"})
	if _, err := svc.StageBuildSecret(context.Background(), "default", "slug", testRunName); !errors.Is(err, ErrOrgDisconnected) {
		t.Fatalf("err = %v, want ErrOrgDisconnected", err)
	}
}

// A row read failure is a transient 500-class error, never a disconnect and
// never an empty reference.
func TestStageBuildSecret_RowReadFailureIsNotDisconnected(t *testing.T) {
	svc := NewBuildCredentialsService(memRepoBySlug(t, "default", "slug"), failingOrgSecrets{err: errors.New("db down")})
	res, err := svc.StageBuildSecret(context.Background(), "default", "slug", testRunName)
	if err == nil || errors.Is(err, ErrOrgDisconnected) || res != nil {
		t.Fatalf("res=%+v err=%v, want a non-disconnect error", res, err)
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Fatalf("err = %v, want the read failure wrapped", err)
	}
}

func TestStageBuildSecret_RepoNotInOrg(t *testing.T) {
	svc := NewBuildCredentialsService(&fakeRepoRepo{rows: map[string]*sourcecontrol.GitRepository{}}, fakeOrgSecrets{"default/github-pat": "default-github-pat-3f9a"})
	_, err := svc.StageBuildSecret(context.Background(), "default", "missing-slug", testRunName)
	if !errors.Is(err, ErrRepoNotInOrg) {
		t.Errorf("got %v; want ErrRepoNotInOrg", err)
	}
}

func TestStageBuildSecret_MissingArgs(t *testing.T) {
	svc := NewBuildCredentialsService(&fakeRepoRepo{}, fakeOrgSecrets{})
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
