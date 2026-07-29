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

package secrets_test

// DBTEST tier (skips under -short, runs on `make test-db`): the REAL
// orgResolver (secrets.NewOrgResolver) against a pristine per-test Postgres
// (dbtest.New). This is where the SQL-shaped dispatch behavior lives —
// kind switch, status gating, and the distinct not-found/not-active/
// unknown-kind error shapes. store and minter are test doubles: a fake
// secrets.CredentialStore (Get/Put/Delete are not SQL-backed, so a fake is honest
// here) and a minter built with a throwaway RSA key (mint/network paths
// are exercised in app_token_minter_test.go, not here).

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// fakeCredentialStore is a minimal in-memory secrets.CredentialStore double. Get can be
// gated (via the gate channel) to force concurrent callers to overlap, and
// counts calls so singleflight coalescing can be asserted.
type fakeCredentialStore struct {
	mu    sync.Mutex
	calls int32
	value []byte
	err   error
	gate  chan struct{} // if non-nil, Get blocks on it before returning
}

func (f *fakeCredentialStore) Get(ctx context.Context, ocOrgID, key string) ([]byte, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.gate != nil {
		<-f.gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.value, f.err
}

func (f *fakeCredentialStore) Put(ctx context.Context, ocOrgID, key string, value []byte) error {
	return errors.New("fakeCredentialStore: Put not implemented")
}

func (f *fakeCredentialStore) Delete(ctx context.Context, ocOrgID, key string) error {
	return errors.New("fakeCredentialStore: Delete not implemented")
}

// newTestMinter builds a secrets.AppTokenMinter with a throwaway RSA key so
// app-installation dispatch can be exercised without hitting GitHub. The
// mint/network path itself is covered by app_token_minter_test.go.
func newTestMinter(t *testing.T) *secrets.AppTokenMinter {
	t.Helper()
	_, pemBytes := generateTestKey(t)
	m, err := secrets.NewAppTokenMinter(&secrets.AppKeyMaterial{AppID: 1, PrivateKeyPEM: pemBytes})
	if err != nil {
		t.Fatalf("NewAppTokenMinter: %v", err)
	}
	return m
}

// insertOrgCredRow inserts an org_credentials row directly via gorm,
// mirroring the shape orgcreds' credential_dbtest_test.go seeds with. The
// secrets_shape_per_kind CHECK constraint requires kind='user-pat' rows to
// carry at least one webhook secret (App-mode rows must have none), so a
// default is filled in here unless the caller already set one.
func insertOrgCredRow(t *testing.T, db *gorm.DB, row organization.OrgCredential) {
	t.Helper()
	if row.Status == "" {
		row.Status = "active"
	}
	if row.ConnectedAt.IsZero() {
		row.ConnectedAt = time.Now().UTC()
	}
	if row.Kind == "user-pat" && row.WebhookSecrets == nil {
		row.WebhookSecrets = organization.WebhookSecrets{{Secret: "test-webhook-secret", AddedAt: row.ConnectedAt}}
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert org_credentials row %q: %v", row.OcOrgID, err)
	}
}

// dropOrgCredentialsCheckConstraints drops every CHECK constraint on
// org_credentials in the caller's (per-test, throwaway) DB clone. The table
// carries three CHECK constraints (kind enum, secrets_shape_per_kind,
// app_fields) that make "unknown kind" and "app-installation row missing
// installation_id" impossible to reach via a normal INSERT — those rows can
// only exist in production via a future/older-binary skew or a manual data
// fix. Dropping the constraints here lets those two defensive branches in
// orgResolver.Resolve be exercised honestly against a real row, instead of
// being untestable dead code.
func dropOrgCredentialsCheckConstraints(t *testing.T, db *gorm.DB) {
	t.Helper()
	var names []string
	if err := db.Raw(
		`SELECT conname FROM pg_constraint WHERE conrelid = 'org_credentials'::regclass AND contype = 'c'`,
	).Scan(&names).Error; err != nil {
		t.Fatalf("list org_credentials CHECK constraints: %v", err)
	}
	for _, name := range names {
		if err := db.Exec(fmt.Sprintf(`ALTER TABLE org_credentials DROP CONSTRAINT %q`, name)).Error; err != nil {
			t.Fatalf("drop constraint %s: %v", name, err)
		}
	}
}

func TestOrgResolver_Resolve_UserPAT_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	store := &fakeCredentialStore{value: []byte("ghp_secret")}
	resolver := secrets.NewOrgResolver(db, store, newTestMinter(t))

	insertOrgCredRow(t, db, organization.OrgCredential{
		OcOrgID:       "acme",
		Kind:          "user-pat",
		GitHubLogin:   "acme-org",
		IdentityName:  "Ada Lovelace",
		IdentityEmail: "ada@example.com",
		IdentityLogin: "ada",
	})

	cred, err := resolver.Resolve(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// user-PAT mode + acme scoping, verified through the exported surface
	// (secrets.WebhookPerRepo strategy + the acme identity/owner below) and the
	// test-only OcOrgIDForTest seam — this is a black-box test, so no
	// concrete-type assertion.
	if ocOrgID, ok := secrets.OcOrgIDForTest(cred); !ok || ocOrgID != "acme" {
		t.Fatalf("resolved credential is not a user-PAT scoped to acme: ocOrgID=%q ok=%v", ocOrgID, ok)
	}
	if got := cred.Identity(); got.Name != "Ada Lovelace" || got.Email != "ada@example.com" || got.Login != "ada" {
		t.Fatalf("Identity() = %+v", got)
	}
	if got := cred.RepoOwner(); got != "acme-org" {
		t.Fatalf("RepoOwner() = %q; want acme-org", got)
	}
	if got := cred.WebhookStrategy(); got != secrets.WebhookPerRepo {
		t.Fatalf("WebhookStrategy() = %v; want WebhookPerRepo", got)
	}
}

func TestOrgResolver_Resolve_AppInstallation_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	store := &fakeCredentialStore{}
	resolver := secrets.NewOrgResolver(db, store, newTestMinter(t))

	installID := int64(4242)
	insertOrgCredRow(t, db, organization.OrgCredential{
		OcOrgID:        "acme",
		Kind:           "app-installation",
		GitHubLogin:    "acme-org",
		IdentityName:   "AEP Bot",
		IdentityEmail:  "bot@aep.dev",
		IdentityLogin:  "aep[bot]",
		InstallationID: &installID,
	})

	cred, err := resolver.Resolve(context.Background(), "acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// app-installation mode is verified through the exported surface
	// (secrets.WebhookPlatform strategy + acme-org owner below) and the
	// test-only InstallationIDForTest seam, which pins the row.installation_id ->
	// credential mapping that Token() mints against (the App-side tenant key).
	if id, ok := secrets.InstallationIDForTest(cred); !ok || id != installID {
		t.Fatalf("resolved credential is not an app-installation with id %d: id=%d ok=%v", installID, id, ok)
	}
	if got := cred.RepoOwner(); got != "acme-org" {
		t.Fatalf("RepoOwner() = %q; want acme-org", got)
	}
	if got := cred.WebhookStrategy(); got != secrets.WebhookPlatform {
		t.Fatalf("WebhookStrategy() = %v; want secrets.WebhookPlatform", got)
	}
}

func TestOrgResolver_Resolve_EmptyOcOrgID_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	resolver := secrets.NewOrgResolver(db, &fakeCredentialStore{}, newTestMinter(t))

	_, err := resolver.Resolve(context.Background(), "")
	if !errors.Is(err, secrets.ErrEmptyOcOrgID) {
		t.Fatalf("Resolve(\"\") = %v; want secrets.ErrEmptyOcOrgID", err)
	}
}

func TestOrgResolver_Resolve_NoRow_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	resolver := secrets.NewOrgResolver(db, &fakeCredentialStore{}, newTestMinter(t))

	_, err := resolver.Resolve(context.Background(), "ghost")
	var nfe *secrets.OrgNotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("Resolve(ghost) = %#v; want *secrets.OrgNotFoundError", err)
	}
	if nfe.OcOrgID != "ghost" {
		t.Fatalf("OrgNotFoundError.OcOrgID = %q; want ghost", nfe.OcOrgID)
	}
}

func TestOrgResolver_Resolve_NotActive_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	resolver := secrets.NewOrgResolver(db, &fakeCredentialStore{}, newTestMinter(t))

	insertOrgCredRow(t, db, organization.OrgCredential{
		OcOrgID:       "acme",
		Kind:          "user-pat",
		GitHubLogin:   "acme-org",
		IdentityName:  "Ada Lovelace",
		IdentityEmail: "ada@example.com",
		IdentityLogin: "ada",
		Status:        "suspended",
	})

	_, err := resolver.Resolve(context.Background(), "acme")
	var nae *secrets.OrgNotActiveError
	if !errors.As(err, &nae) {
		t.Fatalf("Resolve(acme) = %#v; want *secrets.OrgNotActiveError", err)
	}
	if nae.OcOrgID != "acme" {
		t.Fatalf("OrgNotActiveError.OcOrgID = %q; want acme", nae.OcOrgID)
	}
	if nae.Status != "suspended" {
		t.Fatalf("OrgNotActiveError.Status = %q; want suspended", nae.Status)
	}
}

func TestOrgResolver_Resolve_UnknownKind_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	resolver := secrets.NewOrgResolver(db, &fakeCredentialStore{}, newTestMinter(t))

	// A bogus kind can't occur via normal writes — the "kind" CHECK
	// constraint enum-gates INSERT/UPDATE. Drop the table's CHECK
	// constraints on this throwaway DB clone so the row can exist, pinning
	// Resolve's default-case error as the second line of defense (e.g. a
	// future kind added by a newer binary, read by this one mid-rollout).
	dropOrgCredentialsCheckConstraints(t, db)
	insertOrgCredRow(t, db, organization.OrgCredential{
		OcOrgID:       "acme",
		Kind:          "carrier-pigeon",
		GitHubLogin:   "acme-org",
		IdentityName:  "Ada Lovelace",
		IdentityEmail: "ada@example.com",
		IdentityLogin: "ada",
	})

	_, err := resolver.Resolve(context.Background(), "acme")
	if err == nil {
		t.Fatal("Resolve with unknown kind = nil error; want error")
	}
	var nfe *secrets.OrgNotFoundError
	var nae *secrets.OrgNotActiveError
	if errors.As(err, &nfe) || errors.As(err, &nae) {
		t.Fatalf("unknown-kind error must not be secrets.OrgNotFoundError/secrets.OrgNotActiveError, got %#v", err)
	}
}

func TestOrgResolver_Resolve_AppInstallation_NilInstallationID_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	resolver := secrets.NewOrgResolver(db, &fakeCredentialStore{}, newTestMinter(t))

	// The app_fields CHECK constraint normally requires installation_id
	// NOT NULL for kind='app-installation'; drop it on this throwaway DB
	// clone so the row can exist, pinning the resolver's own defensive
	// nil-check as a second layer, not just relying on the DB constraint.
	dropOrgCredentialsCheckConstraints(t, db)
	insertOrgCredRow(t, db, organization.OrgCredential{
		OcOrgID:       "acme",
		Kind:          "app-installation",
		GitHubLogin:   "acme-org",
		IdentityName:  "AEP Bot",
		IdentityEmail: "bot@aep.dev",
		IdentityLogin: "aep[bot]",
		// InstallationID intentionally left nil.
	})

	_, err := resolver.Resolve(context.Background(), "acme")
	if err == nil {
		t.Fatal("Resolve with NULL installation_id = nil error; want error")
	}
}

// TestOrgResolver_UserPATToken_Singleflight_DB pins a resolver-level
// property: patFlight is shared across every credential the resolver
// hands out (it lives on orgResolver, not per-credential), so concurrent
// Token() calls for the SAME org — even from separately-Resolve()'d
// credentials — coalesce into one OpenBao read.
func TestOrgResolver_UserPATToken_Singleflight_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	store := &fakeCredentialStore{value: []byte("ghp_secret"), gate: make(chan struct{})}
	resolver := secrets.NewOrgResolver(db, store, newTestMinter(t))

	insertOrgCredRow(t, db, organization.OrgCredential{
		OcOrgID:       "acme",
		Kind:          "user-pat",
		GitHubLogin:   "acme-org",
		IdentityName:  "Ada Lovelace",
		IdentityEmail: "ada@example.com",
		IdentityLogin: "ada",
	})

	const n = 5
	// Resolve n credentials up front (sequentially) — each is a distinct
	// *userPATCred instance, but they share the resolver's patFlight. Resolve
	// hits the DB, so it must run BEFORE the raced section: leaving a DB read
	// inside the goroutines lets them reach flight.Do at staggered times (past
	// the gate) and never overlap, which is the coalescing window we're testing.
	creds := make([]secrets.Credential, n)
	for i := 0; i < n; i++ {
		c, err := resolver.Resolve(context.Background(), "acme")
		if err != nil {
			t.Fatalf("Resolve[%d]: %v", i, err)
		}
		creds[i] = c
	}

	var wg sync.WaitGroup
	tokens := make([]string, n)
	errs := make([]error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			// Only Token() is raced — it hits flight.Do(ocOrgID) immediately,
			// so all n goroutines enter the shared singleflight while the first
			// call is held at the gate.
			tok, _, err := creds[idx].Token(context.Background())
			tokens[idx] = tok
			errs[idx] = err
		}(i)
	}
	// Give every goroutine time to reach the singleflight gate before
	// releasing it.
	time.Sleep(50 * time.Millisecond)
	close(store.gate)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}
	for i, tok := range tokens {
		if tok != "ghp_secret" {
			t.Fatalf("tokens[%d] = %q; want ghp_secret", i, tok)
		}
	}
	if got := atomic.LoadInt32(&store.calls); got != 1 {
		t.Errorf("store.Get calls = %d; want 1 (singleflight collapses %d fetches)", got, n)
	}
}

// generateTestKey is a local copy of the app_token_minter_test helper so this
// black-box test needs no exported production surface for its RSA fixture.
func generateTestKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, pemBytes
}
