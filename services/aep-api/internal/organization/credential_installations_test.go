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

// UNIT + DBTEST tiers for the App-installation path (fetchInstallation,
// ListInstallationRepos/listInstallationRepos, fetchAppBotIdentity).
//
// fetchInstallation and fetchAppBotIdentity sign their own App JWT
// (secrets.AppTokenMinter.SignAppJWT) and hit s.githubAPI directly via
// s.httpClient — both are TEST-SEAMED via WithGitHubAPIBase, so their GitHub
// calls run for real against an httptest fake, no live network.
//
// listInstallationRepos (selected-repos fetch inside fetchInstallation, and
// the public ListInstallationRepos wrapper) first mints an installation
// token via AppTokenMinter.MintForInstallation, which POSTs to a HARDCODED
// https://api.github.com/... URL using the minter's own *http.Client — a
// private field of package credentials with no test seam reachable from
// here. Two consequences pinned below:
//   - With an UNCONFIGURED minter (no App key), MintForInstallation returns
//     secrets.ErrAppNotConfigured before any network I/O — fully
//     deterministic, no network, and exercised directly.
//   - With a CONFIGURED minter (needed for fetchInstallation's own JWT to
//     succeed), the token-mint call cannot be redirected to a fake without
//     either reaching package credentials or touching the real internet. The
//     happy-path test below forces that specific hop to fail deterministically
//     via context cancellation (cancelAfterResponseTransport) instead — this
//     pins fetchInstallation's real "best effort" fallback (log + continue
//     with an empty selectedRepos) without ever dialing api.github.com. The
//     multi-page repository-listing merge logic inside listInstallationRepos
//     itself (the part downstream of a successful mint) is NOT reachable from
//     this package for the same reason and is left uncovered here.
package organization

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// sortedKeys returns the sorted JSON keys of a decoded object. Package-scoped
// (not file-scoped): idp_service_test.go also calls this. It used to be
// defined in credential_dbtest_test.go, which moved to package
// organization_test during the dbtest/import-cycle conversion (see AGENTS.md
// task notes on that migration); kept here, in a file that stays package
// organization, so idp_service_test.go's reference keeps resolving.
func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// --- key + minter helpers -----------------------------------------------------

// genTestRSAKeyPEM generates a fresh 2048-bit RSA key for App-JWT signing.
// Mirrors internal/credentials/app_token_minter_test.go's generateTestKey,
// duplicated here because that helper is unexported in a different package.
func genTestRSAKeyPEM(t testing.TB) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}

// newConfiguredMinter builds an AppTokenMinter with real key material so
// SignAppJWT succeeds — needed for fetchInstallation/fetchAppBotIdentity's
// own JWT-signed calls.
func newConfiguredMinter(t testing.TB, appID int64) *secrets.AppTokenMinter {
	t.Helper()
	m, err := secrets.NewAppTokenMinter(&secrets.AppKeyMaterial{AppID: appID, PrivateKeyPEM: genTestRSAKeyPEM(t)})
	if err != nil {
		t.Fatalf("NewAppTokenMinter: %v", err)
	}
	return m
}

// newUnconfiguredMinter builds a minter with no App key material — every
// SignAppJWT/MintForInstallation call fails fast with ErrAppNotConfigured,
// deterministically and without any network I/O.
func newUnconfiguredMinter(t testing.TB) *secrets.AppTokenMinter {
	t.Helper()
	m, err := secrets.NewAppTokenMinter(nil)
	if err != nil {
		t.Fatalf("NewAppTokenMinter(nil): %v", err)
	}
	return m
}

// installSvc builds a CredentialService for the App-installation tests: db
// and store stay nil (none of fetchInstallation/fetchAppBotIdentity/
// listInstallationRepos touch either), minter + GitHub base are the only
// wiring under test.
func installSvc(t testing.TB, minter *secrets.AppTokenMinter, ghBase string) *CredentialService {
	t.Helper()
	return NewCredentialService(nil, nil, minter, "").WithGitHubAPIBase(ghBase)
}

// --- fetchInstallation: pre-mint branches (no network trickery needed) ------

func TestFetchInstallation_ErrorBranches(t *testing.T) {
	t.Parallel()

	t.Run("unconfigured minter fails signing before any HTTP call", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t) // no routes registered — a hit here would 404, proving over-reach
		svc := installSvc(t, newUnconfiguredMinter(t), gh.URL)
		_, _, _, err := svc.fetchInstallation(context.Background(), 1)
		var ce *ConflictError
		if !errors.As(err, &ce) {
			t.Fatalf("want *ConflictError (app sign failure), got %#v", err)
		}
		if !strings.Contains(ce.Reason, "app sign") {
			t.Fatalf("ConflictError.Reason = %q; want it to mention app sign", ce.Reason)
		}
	})

	t.Run("404 → installation_not_found", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/app/installations/42", 404, `{"message":"Not Found"}`)
		svc := installSvc(t, newConfiguredMinter(t, 111), gh.URL)
		_, _, _, err := svc.fetchInstallation(context.Background(), 42)
		assertValidationCode(t, err, "installation_not_found")
	})

	t.Run("non-200/404 → github_error", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/app/installations/42", 500, `boom`)
		svc := installSvc(t, newConfiguredMinter(t, 111), gh.URL)
		_, _, _, err := svc.fetchInstallation(context.Background(), 42)
		assertValidationCode(t, err, "github_error")
	})

	t.Run("malformed JSON body → decode error", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/app/installations/42", 200, `{not json`)
		svc := installSvc(t, newConfiguredMinter(t, 111), gh.URL)
		_, _, _, err := svc.fetchInstallation(context.Background(), 42)
		if err == nil {
			t.Fatalf("want a decode error for malformed JSON")
		}
		var ve *ValidationError
		if errors.As(err, &ve) {
			t.Fatalf("malformed JSON should be a raw decode error, not a *ValidationError: %v", err)
		}
	})

	t.Run("200 but missing account.login → install_no_account", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/app/installations/42", 200, `{"account":{"type":"Organization"}}`)
		svc := installSvc(t, newConfiguredMinter(t, 111), gh.URL)
		_, _, _, err := svc.fetchInstallation(context.Background(), 42)
		assertValidationCode(t, err, "install_no_account")
	})
}

// --- fetchInstallation: happy path (forces the unseamed mint hop to fail) ---

// cancelAfterResponseTransport lets a test force fetchInstallation's SECOND,
// unseamed HTTP hop (the installation-token mint, hardcoded inside
// secrets.AppTokenMinter to real api.github.com with no test seam
// reachable from this package) to fail deterministically without touching
// the network. It fully buffers the FIRST response — so the caller's
// io.ReadAll is unaffected by what happens next — then cancels ctx. Because
// fetchInstallation issues its second request with that same ctx immediately
// afterward, in the same goroutine, strictly after this RoundTrip returns,
// the mint's http.NewRequestWithContext sees an already-Done context and
// fails before dialing: deterministic regardless of whether outbound network
// is actually reachable where the test runs.
type cancelAfterResponseTransport struct {
	inner  http.RoundTripper
	cancel context.CancelFunc
	fired  bool
}

func (rt *cancelAfterResponseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.inner.RoundTrip(req)
	if err == nil && resp.Body != nil {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}
	if !rt.fired {
		rt.fired = true
		rt.cancel()
	}
	return resp, err
}

func TestFetchInstallation_HappyPath_SelectedReposBestEffortFallback(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	gh := newStubGitHub(t)
	gh.on("GET", "/app/installations/777", 200, `{"account":{"login":"acme-org","type":"Organization"},"repository_selection":"selected"}`)

	svc := installSvc(t, newConfiguredMinter(t, 555), gh.URL)
	svc.httpClient = &http.Client{Transport: &cancelAfterResponseTransport{inner: http.DefaultTransport, cancel: cancel}}

	login, acctType, repos, err := svc.fetchInstallation(ctx, 777)
	if err != nil {
		t.Fatalf("fetchInstallation: %v", err)
	}
	if login != "acme-org" || acctType != "Organization" {
		t.Fatalf("account fields: login=%q type=%q; want acme-org/Organization", login, acctType)
	}
	// Quirk: the account fields parse fine, but selectedRepos silently comes
	// back empty — listInstallationRepos' mint call failed (forced, here) and
	// fetchInstallation treats that as best-effort, per its doc comment.
	if len(repos) != 0 {
		t.Fatalf("selectedRepos = %v; want empty (best-effort fallback when the mint hop fails)", repos)
	}
}

// --- listInstallationRepos / ListInstallationRepos ---------------------------

func TestListInstallationRepos_UnconfiguredMinter_NoNetwork(t *testing.T) {
	t.Parallel()
	// privateKey == nil short-circuits MintForInstallation before any HTTP
	// work — deterministic, no network, and exercises listInstallationRepos'
	// error-propagation branch directly.
	gh := newStubGitHub(t) // no routes registered — any hit would 404
	svc := installSvc(t, newUnconfiguredMinter(t), gh.URL)
	repos, err := svc.ListInstallationRepos(context.Background(), 42)
	if !errors.Is(err, secrets.ErrAppNotConfigured) {
		t.Fatalf("ListInstallationRepos error = %v; want ErrAppNotConfigured", err)
	}
	if repos != nil {
		t.Fatalf("repos = %v; want nil on mint failure", repos)
	}
}

// --- fetchAppBotIdentity -------------------------------------------------------

func TestFetchAppBotIdentity(t *testing.T) {
	t.Parallel()

	t.Run("unconfigured minter fails signing, no HTTP call", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t) // no routes registered
		svc := installSvc(t, newUnconfiguredMinter(t), gh.URL)
		_, err := svc.fetchAppBotIdentity(context.Background())
		if !errors.Is(err, secrets.ErrAppNotConfigured) {
			t.Fatalf("fetchAppBotIdentity error = %v; want ErrAppNotConfigured", err)
		}
	})

	t.Run("200 with slug+name builds the [bot] identity", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/app", 200, `{"slug":"aep","name":"AEP Bot"}`)
		svc := installSvc(t, newConfiguredMinter(t, 222), gh.URL)
		id, err := svc.fetchAppBotIdentity(context.Background())
		if err != nil {
			t.Fatalf("fetchAppBotIdentity: %v", err)
		}
		want := secrets.Identity{Name: "AEP Bot", Email: "aep[bot]@users.noreply.github.com", Login: "aep[bot]"}
		if id != want {
			t.Fatalf("identity = %+v; want %+v", id, want)
		}
	})

	t.Run("200 missing slug is an error", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/app", 200, `{"name":"AEP Bot"}`)
		svc := installSvc(t, newConfiguredMinter(t, 222), gh.URL)
		_, err := svc.fetchAppBotIdentity(context.Background())
		if err == nil || !strings.Contains(err.Error(), "missing slug") {
			t.Fatalf("want a missing-slug error, got %v", err)
		}
	})

	t.Run("non-200 is an error", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/app", 500, `boom`)
		svc := installSvc(t, newConfiguredMinter(t, 222), gh.URL)
		_, err := svc.fetchAppBotIdentity(context.Background())
		if err == nil || !strings.Contains(err.Error(), "GET /app:") {
			t.Fatalf("want a wrapped GET /app error, got %v", err)
		}
	})

	t.Run("malformed JSON is a decode error", func(t *testing.T) {
		t.Parallel()
		gh := newStubGitHub(t)
		gh.on("GET", "/app", 200, `{not json`)
		svc := installSvc(t, newConfiguredMinter(t, 222), gh.URL)
		_, err := svc.fetchAppBotIdentity(context.Background())
		if err == nil {
			t.Fatalf("want a decode error for malformed JSON")
		}
	})
}
