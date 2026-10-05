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

// UNIT tier: the pure, DB-free surface of the
// credential service — projectionFromRow's field mapping, the typed errors'
// messages and the PAT-validation probes
// (fetchPATIdentity / validatePATMembership / probePATRepoRead) driven against
// a fake GitHub. No Postgres, no Huma handler. The SQL-shaped behavior
// (Connect/Disconnect/webhook-secret rotation/routing lookups) is pinned in
// credential_dbtest_test.go; the HTTP contract in credential_component_test.go.
//
// These tests pin BEHAVIOR AS-IS ahead of the credential_service.go file split
// (ADR-0003 Pilot B) and depend only on the package's API — never on which file
// a symbol lives in — so they stay green across the restructure.

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// --- fake GitHub -------------------------------------------------------------

// stubGitHub is a configurable stand-in for api.github.com. Register a status +
// body per "METHOD /path" with on(); an unregistered path 404s, so a probe's
// own not-found handling is exercised. Query strings are ignored (routing is on
// r.URL.Path). Shared by the unit probe tests and the dbtest tier (same
// package); the external-package component tests roll their own.
type stubGitHub struct {
	*httptest.Server
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
}

func newStubGitHub(t testing.TB) *stubGitHub {
	t.Helper()
	s := &stubGitHub{routes: map[string]http.HandlerFunc{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		h := s.routes[r.Method+" "+r.URL.Path]
		s.mu.Unlock()
		if h == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// on registers a fixed status+body for an exact method+path.
func (s *stubGitHub) on(method, path string, code int, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[method+" "+path] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

// probeSvc builds a CredentialService whose only wired dependency is the GitHub
// base URL — enough for the stateless PAT probes, which touch neither db nor
// store.
func probeSvc(t testing.TB, gh *stubGitHub) *CredentialService {
	t.Helper()
	return NewCredentialService(nil, nil).WithGitHubAPIBase(gh.URL)
}

// --- projectionFromRow -------------------------------------------------------

func TestProjectionFromRow_FieldMapping(t *testing.T) {
	t.Parallel()
	connectedAt := time.Date(2026, 7, 1, 5, 1, 44, 0, time.UTC)
	validated := connectedAt.Add(time.Hour)
	changed := connectedAt.Add(2 * time.Hour)
	instID := int64(4242)
	prev := "old-login"

	row := &OrgCredential{
		OcOrgID:           "acme",
		Kind:              "user-pat",
		GitHubLogin:       "acme-org",
		IdentityName:      "Ada Lovelace",
		IdentityEmail:     "ada@example.com",
		IdentityLogin:     "ada",
		InstallationID:    &instID,
		SelectedRepos:     JSONStringList{"acme-org/one", "acme-org/two"},
		Status:            "active",
		ConnectedAt:       connectedAt,
		LastValidatedAt:   &validated,
		IdentityChangedAt: &changed,
		PrevIdentityLogin: &prev,
	}

	p := projectionFromRow(row)
	if p.OcOrgID != "acme" || p.Kind != "user-pat" || p.GitHubLogin != "acme-org" {
		t.Fatalf("core fields: %+v", p)
	}
	if p.IdentityName != "Ada Lovelace" || p.IdentityEmail != "ada@example.com" || p.IdentityLogin != "ada" {
		t.Fatalf("identity fields: %+v", p)
	}
	if p.InstallationID == nil || *p.InstallationID != 4242 {
		t.Fatalf("installationID: %v", p.InstallationID)
	}
	if len(p.SelectedRepos) != 2 || p.SelectedRepos[0] != "acme-org/one" {
		t.Fatalf("selectedRepos: %v", p.SelectedRepos)
	}
	if p.Status != "active" || !p.ConnectedAt.Equal(connectedAt) {
		t.Fatalf("status/connectedAt: %+v", p)
	}
	if p.LastValidatedAt == nil || !p.LastValidatedAt.Equal(validated) {
		t.Fatalf("lastValidatedAt: %v", p.LastValidatedAt)
	}
	if p.IdentityChangedAt == nil || !p.IdentityChangedAt.Equal(changed) {
		t.Fatalf("identityChangedAt: %v", p.IdentityChangedAt)
	}
	if p.PrevIdentityLogin == nil || *p.PrevIdentityLogin != "old-login" {
		t.Fatalf("prevIdentityLogin: %v", p.PrevIdentityLogin)
	}
}

func TestProjectionFromRow_NilSelectedReposStaysNil(t *testing.T) {
	t.Parallel()
	// A user-pat row carries no selected_repos; the projection must leave the
	// slice nil (so it JSON-omits) rather than materialize an empty [].
	p := projectionFromRow(&OrgCredential{OcOrgID: "acme", Kind: "user-pat"})
	if p.SelectedRepos != nil {
		t.Fatalf("nil SelectedRepos must stay nil, got %#v", p.SelectedRepos)
	}
}

// --- typed error messages ----------------------------------------------------

func TestCredentialErrors_Messages(t *testing.T) {
	t.Parallel()
	if got := (&ValidationError{Code: "pat_invalid", Message: "PAT is not valid"}).Error(); got != "PAT is not valid" {
		t.Fatalf("ValidationError.Error = %q", got)
	}
	if got := (&ConflictError{Reason: "in progress"}).Error(); got != "conflict: in progress" {
		t.Fatalf("ConflictError.Error = %q", got)
	}
	if got := (&NotFoundError{What: "org_credentials.acme"}).Error(); got != "not found: org_credentials.acme" {
		t.Fatalf("NotFoundError.Error = %q", got)
	}
}

func TestFetchPATIdentity(t *testing.T) {
	t.Parallel()

	t.Run("200 full identity", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user", 200, `{"login":"ada","name":"Ada","email":"ada@x.io"}`)
		id, err := probeSvc(t, gh).fetchPATIdentity(context.Background(), "pat")
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if id.Login != "ada" || id.Name != "Ada" || id.Email != "ada@x.io" {
			t.Fatalf("identity: %+v", id)
		}
	})

	t.Run("200 missing name falls back to login", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user", 200, `{"login":"ada","email":"ada@x.io"}`)
		id, _ := probeSvc(t, gh).fetchPATIdentity(context.Background(), "pat")
		if id.Name != "ada" {
			t.Fatalf("name fallback: got %q", id.Name)
		}
	})

	t.Run("200 missing email falls back to noreply", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user", 200, `{"login":"ada","name":"Ada"}`)
		id, _ := probeSvc(t, gh).fetchPATIdentity(context.Background(), "pat")
		if id.Email != "ada@users.noreply.github.com" {
			t.Fatalf("email fallback: got %q", id.Email)
		}
	})

	t.Run("200 missing login → github_no_login", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user", 200, `{"name":"Ada"}`)
		assertValidationCode(t, secondErr(probeSvc(t, gh).fetchPATIdentity(context.Background(), "pat")), "github_no_login")
	})

	t.Run("401 → pat_invalid", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user", 401, `{"message":"Bad credentials"}`)
		assertValidationCode(t, secondErr(probeSvc(t, gh).fetchPATIdentity(context.Background(), "pat")), "pat_invalid")
	})

	t.Run("403 → pat_forbidden", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user", 403, `{"message":"rate limited"}`)
		assertValidationCode(t, secondErr(probeSvc(t, gh).fetchPATIdentity(context.Background(), "pat")), "pat_forbidden")
	})

	t.Run("500 → github_error", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user", 500, `boom`)
		assertValidationCode(t, secondErr(probeSvc(t, gh).fetchPATIdentity(context.Background(), "pat")), "github_error")
	})

	t.Run("unreachable → github_unreachable", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		svc := probeSvc(t, gh)
		gh.Close() // now the base URL refuses connections
		assertValidationCode(t, secondErr(svc.fetchPATIdentity(context.Background(), "pat")), "github_unreachable")
	})
}

// --- validatePATMembership ---------------------------------------------------

func TestValidatePATMembership(t *testing.T) {
	t.Parallel()

	t.Run("owner==login skips the probe entirely", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t) // no /user/memberships route registered — must not be hit
		if err := probeSvc(t, gh).validatePATMembership(context.Background(), "pat", "Ada", "ada"); err != nil {
			t.Fatalf("case-insensitive self-match must skip probe, got %v", err)
		}
	})

	t.Run("404 → pat_not_member", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user/memberships/orgs/acme-org", 404, `{}`)
		err := probeSvc(t, gh).validatePATMembership(context.Background(), "pat", "acme-org", "ada")
		assertValidationCode(t, err, "pat_not_member")
	})

	t.Run("403 is tolerated (fine-grained PAT) → nil", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user/memberships/orgs/acme-org", 403, `{}`)
		if err := probeSvc(t, gh).validatePATMembership(context.Background(), "pat", "acme-org", "ada"); err != nil {
			t.Fatalf("403 must be skip-with-log, got %v", err)
		}
	})

	t.Run("200 active → nil", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user/memberships/orgs/acme-org", 200, `{"state":"active"}`)
		if err := probeSvc(t, gh).validatePATMembership(context.Background(), "pat", "acme-org", "ada"); err != nil {
			t.Fatalf("active membership: %v", err)
		}
	})

	t.Run("200 pending → pat_membership_inactive", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user/memberships/orgs/acme-org", 200, `{"state":"pending"}`)
		err := probeSvc(t, gh).validatePATMembership(context.Background(), "pat", "acme-org", "ada")
		assertValidationCode(t, err, "pat_membership_inactive")
	})

	t.Run("500 → github_error", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/user/memberships/orgs/acme-org", 500, `boom`)
		err := probeSvc(t, gh).validatePATMembership(context.Background(), "pat", "acme-org", "ada")
		assertValidationCode(t, err, "github_error")
	})
}

// --- probePATRepoRead --------------------------------------------------------

func TestProbePATRepoRead(t *testing.T) {
	t.Parallel()

	t.Run("orgs repos 200 → nil", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/orgs/acme-org/repos", 200, `[]`)
		if err := probeSvc(t, gh).probePATRepoRead(context.Background(), "pat", "acme-org"); err != nil {
			t.Fatalf("repo probe: %v", err)
		}
	})

	t.Run("orgs 403 → pat_no_repo_read", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/orgs/acme-org/repos", 403, `{}`)
		assertValidationCode(t, probeSvc(t, gh).probePATRepoRead(context.Background(), "pat", "acme-org"), "pat_no_repo_read")
	})

	t.Run("orgs 404 then users 200 → nil (user account)", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/orgs/ada/repos", 404, `{}`)
		gh.on("GET", "/users/ada/repos", 200, `[]`)
		if err := probeSvc(t, gh).probePATRepoRead(context.Background(), "pat", "ada"); err != nil {
			t.Fatalf("user-account fallback: %v", err)
		}
	})

	t.Run("orgs 404 then users 404 → pat_no_repo_read", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/orgs/ghost/repos", 404, `{}`)
		gh.on("GET", "/users/ghost/repos", 404, `{}`)
		assertValidationCode(t, probeSvc(t, gh).probePATRepoRead(context.Background(), "pat", "ghost"), "pat_no_repo_read")
	})

	t.Run("orgs 500 → github_error", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/orgs/acme-org/repos", 500, `boom`)
		assertValidationCode(t, probeSvc(t, gh).probePATRepoRead(context.Background(), "pat", "acme-org"), "github_error")
	})
}

// --- helpers -----------------------------------------------------------------

// secondErr discards a (T, error) return's value, yielding just the error — so a
// probe returning (*ghIdentity, error) can be fed straight into an assertion.
func secondErr[T any](_ T, err error) error { return err }

// assertValidationCode asserts err is a *ValidationError carrying wantCode.
func assertValidationCode(t testing.TB, err error, wantCode string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError(%s), got %#v", wantCode, err)
	}
	if ve.Code != wantCode {
		t.Fatalf("ValidationError.Code: got %q want %q", ve.Code, wantCode)
	}
}
