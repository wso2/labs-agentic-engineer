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

package organization_test

// DBTEST tier ("store"; skips under -short, runs on
// `make test-db`): the REAL CredentialService over a pristine per-test Postgres
// (dbtest.New) with the REAL AES-GCM credential store (secrets.NewDBStore)
// and a fake GitHub. This is where the SQL-shaped behavior lives — the Connect
// transaction + CHECK constraints, webhook-secret rotation, the webhook routing
// lookups, installation status flips, identity-drift bookkeeping, and org
// isolation. The stateless probes are unit-pinned (credential_service_test.go);
// the HTTP contract is component-pinned (credential_component_test.go).
//
// Behavior is pinned AS-IS ahead of the credential_service.go split (ADR-0003
// Pilot B) and depends only on the package's API, so it survives the file move.
//
// External test package: credential_service_test.go (unit tier, package
// organization) imports dbtest, which imports migrate, which imports
// organization — an in-package dbtest file would be an import cycle. stubGitHub
// and assertValidationCode are duplicated in dbtest_helpers_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// credAESKey is a fixed 32-byte AES-256 key for the test credential store.
const credAESKey = "0123456789abcdef0123456789abcdef"

const envWebhookSecret = "platform-webhook-secret"

// newCredentialStore builds the real DB-backed, AES-GCM credential store.
func newCredentialStore(t testing.TB, db *gorm.DB) secrets.CredentialStore {
	t.Helper()
	store, err := secrets.NewDBStore(db, []byte(credAESKey))
	if err != nil {
		t.Fatalf("NewDBStore: %v", err)
	}
	return store
}

// newCredSvcDB wires the real CredentialService over the dbtest DB with a fake
// GitHub. minter is no-app mode (nil material), which is correct for the
// PAT paths; the OAuth bind path is disabled (nil githubClient, empty client
// id/secret). Returns the store too so tests can inspect the sealed PAT.
func newCredSvcDB(t testing.TB, db *gorm.DB, gh *stubGitHub) (*organization.CredentialService, secrets.CredentialStore) {
	t.Helper()
	store := newCredentialStore(t, db)
	minter, err := secrets.NewAppTokenMinter(nil)
	if err != nil {
		t.Fatalf("NewAppTokenMinter: %v", err)
	}
	svc := organization.NewCredentialService(organization.NewOrgCredentialRepository(db), store, minter, envWebhookSecret, "", "", nil).WithGitHubAPIBase(gh.URL)
	return svc, store
}

// patHappyGitHub serves the responses a valid PAT connect needs: GET /user
// returns login/name/email, and GET /orgs/{login}/repos returns an empty list
// (accepted — fresh org). With githubLogin == login the membership probe is
// skipped, so no membership route is needed.
func patHappyGitHub(t testing.TB, login, name, email string) *stubGitHub {
	t.Helper()
	gh := newStubGitHub(t)
	gh.on("GET", "/user", 200, `{"login":"`+login+`","name":"`+name+`","email":"`+email+`"}`)
	gh.on("GET", "/orgs/"+login+"/repos", 200, `[]`)
	return gh
}

// fakeBuildCleaner records DeleteBuildSecretsForOrg calls from the Disconnect
// cascade.
type fakeBuildCleaner struct{ orgs []string }

func (f *fakeBuildCleaner) DeleteBuildSecretsForOrg(_ context.Context, ocOrgID string) error {
	f.orgs = append(f.orgs, ocOrgID)
	return nil
}

// getRow reads the raw credential row for assertions the projection hides
// (webhook_secrets, drift columns).
func getRow(t testing.TB, db *gorm.DB, ocOrgID string) organization.OrgCredential {
	t.Helper()
	var row organization.OrgCredential
	if err := db.Where("oc_org_id = ?", ocOrgID).First(&row).Error; err != nil {
		t.Fatalf("load row %s: %v", ocOrgID, err)
	}
	return row
}

// insertAppRow inserts an app-installation row directly (bypassing the App
// connect flow, which needs a real App key). Satisfies the CHECK constraints:
// webhook_secrets NULL, installation_id NOT NULL.
func insertAppRow(t testing.TB, db *gorm.DB, ocOrgID string, installID int64, status string, selected []string) {
	t.Helper()
	id := installID
	row := organization.OrgCredential{
		OcOrgID:        ocOrgID,
		Kind:           "app-installation",
		GitHubLogin:    "acme-org",
		IdentityName:   "AEP Bot",
		IdentityEmail:  "bot@aep.dev",
		IdentityLogin:  "aep[bot]",
		InstallationID: &id,
		SelectedRepos:  organization.JSONStringList(selected),
		Status:         status,
		ConnectedAt:    time.Now().UTC(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert app row %s: %v", ocOrgID, err)
	}
}

// ============================================================================
// Connect (PAT)
// ============================================================================

func TestConnectPAT_FreshRow_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada Lovelace", "ada@example.com")
	svc, store := newCredSvcDB(t, db, gh)

	proj, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp_live", GitHubLogin: "ada"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if proj.Status != "active" || proj.Kind != "user-pat" || proj.IdentityLogin != "ada" {
		t.Fatalf("projection: %+v", proj)
	}
	if proj.GitHubLogin != "ada" || proj.IdentityName != "Ada Lovelace" || proj.IdentityEmail != "ada@example.com" {
		t.Fatalf("identity projection: %+v", proj)
	}
	if proj.LastValidatedAt == nil {
		t.Fatal("lastValidatedAt must be stamped on connect")
	}

	// webhook_secrets[0] must be seeded from the platform env secret so per-repo
	// hooks (signed with the same value) verify.
	row := getRow(t, db, "acme")
	if len(row.WebhookSecrets) != 1 || row.WebhookSecrets[0].Secret != envWebhookSecret {
		t.Fatalf("webhook_secrets seed: %+v", row.WebhookSecrets)
	}

	// The PAT is sealed into the credential store under github/pat.
	got, err := store.Get(ctx, "acme", "github/pat")
	if err != nil || string(got) != "ghp_live" {
		t.Fatalf("stored PAT: got %q err %v", string(got), err)
	}

	// The on-wire projection shape matches the harvested golden's key-set.
	if got, want := projectionKeys(t, proj), goldenKeys(t); !equalStrs(got, want) {
		t.Fatalf("projection keys drifted from golden:\n got=%v\nwant=%v", got, want)
	}
}

func TestConnectPAT_InvalidPAT_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := newStubGitHub(t)
	gh.on("GET", "/user", 401, `{"message":"Bad credentials"}`)
	svc, _ := newCredSvcDB(t, db, gh)

	_, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "bad", GitHubLogin: "ada"})
	assertValidationCode(t, err, "pat_invalid")

	// No row must be created when validation fails (the tx rolls back).
	var count int64
	db.Model(&organization.OrgCredential{}).Where("oc_org_id = ?", "acme").Count(&count)
	if count != 0 {
		t.Fatalf("failed connect must not leave a row, got %d", count)
	}
}

func TestConnectPAT_MissingFields_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))

	_, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "", GitHubLogin: "ada"})
	assertValidationCode(t, err, "pat_missing")

	_, err = svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: ""})
	assertValidationCode(t, err, "github_login_missing")
}

func TestConnect_UnknownKind_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))
	_, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "wat"})
	assertValidationCode(t, err, "kind_invalid")
}

func TestConnectPAT_ReplaceRecordsDrift_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada Lovelace", "ada@example.com")
	svc, _ := newCredSvcDB(t, db, gh)

	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "p1", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	before := getRow(t, db, "acme")

	// Re-connect with a DIFFERENT identity login (the PAT now belongs to a
	// renamed/other account) — must record identity drift and preserve the
	// existing webhook_secrets.
	gh.on("GET", "/user", 200, `{"login":"bob","name":"Bob","email":"bob@example.com"}`)
	gh.on("GET", "/orgs/bob/repos", 200, `[]`)
	proj, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "p2", GitHubLogin: "bob"})
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if proj.IdentityLogin != "bob" {
		t.Fatalf("replace identity: %+v", proj)
	}
	if proj.PrevIdentityLogin == nil || *proj.PrevIdentityLogin != "ada" || proj.IdentityChangedAt == nil {
		t.Fatalf("drift not recorded: prev=%v changed=%v", proj.PrevIdentityLogin, proj.IdentityChangedAt)
	}
	after := getRow(t, db, "acme")
	if len(after.WebhookSecrets) != 1 || after.WebhookSecrets[0].Secret != before.WebhookSecrets[0].Secret {
		t.Fatalf("replace must preserve webhook_secrets: before=%v after=%v", before.WebhookSecrets, after.WebhookSecrets)
	}
}

// ============================================================================
// Status
// ============================================================================

func TestStatus_NotFound_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))
	_, err := svc.Status(context.Background(), "ghost")
	var nfe *organization.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("want *organization.NotFoundError, got %#v", err)
	}
}

// ============================================================================
// Disconnect
// ============================================================================

func TestDisconnect_ClearsRowAndSecrets_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc, store := newCredSvcDB(t, db, gh)
	cleaner := &fakeBuildCleaner{}
	svc.WithBuildSecretCleaner(cleaner)

	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := svc.Disconnect(ctx, "acme"); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	if row := getRow(t, db, "acme"); row.Status != "disconnected" {
		t.Fatalf("status after disconnect: %q", row.Status)
	}
	if _, err := store.Get(ctx, "acme", "github/pat"); !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("PAT must be GC'd from the store, got err %v", err)
	}
	if len(cleaner.orgs) != 1 || cleaner.orgs[0] != "acme" {
		t.Fatalf("build-secret cleaner calls: %v", cleaner.orgs)
	}
}

func TestDisconnect_Idempotent_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc, _ := newCredSvcDB(t, db, gh)

	// Absent row → no-op nil.
	if err := svc.Disconnect(ctx, "ghost"); err != nil {
		t.Fatalf("disconnect absent: %v", err)
	}
	// Connect then disconnect twice → both nil, terminal state stable.
	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := svc.Disconnect(ctx, "acme"); err != nil {
		t.Fatalf("disconnect 1: %v", err)
	}
	if err := svc.Disconnect(ctx, "acme"); err != nil {
		t.Fatalf("disconnect 2 (idempotent): %v", err)
	}
	if row := getRow(t, db, "acme"); row.Status != "disconnected" {
		t.Fatalf("status: %q", row.Status)
	}
}

// ============================================================================
// Webhook secrets
// ============================================================================

func TestWebhookSecrets_RoundTrip_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc, _ := newCredSvcDB(t, db, gh)
	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Seeded secret is returned.
	got, err := svc.GetWebhookSecrets(ctx, "acme")
	if err != nil || len(got) != 1 || string(got[0]) != envWebhookSecret {
		t.Fatalf("initial secrets: %q err %v", got, err)
	}

	// Append prepends (current-first).
	if err := svc.AppendWebhookSecret(ctx, "acme", "rotated"); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, _ = svc.GetWebhookSecrets(ctx, "acme")
	if len(got) != 2 || string(got[0]) != "rotated" || string(got[1]) != envWebhookSecret {
		t.Fatalf("after append: %q", got)
	}

	// Remove drops the named one.
	if err := svc.RemoveWebhookSecret(ctx, "acme", envWebhookSecret); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, _ = svc.GetWebhookSecrets(ctx, "acme")
	if len(got) != 1 || string(got[0]) != "rotated" {
		t.Fatalf("after remove: %q", got)
	}

	// Cannot drop the last secret.
	err = svc.RemoveWebhookSecret(ctx, "acme", "rotated")
	var ce *organization.ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("dropping last secret must ConflictError, got %#v", err)
	}
}

func TestWebhookSecrets_AppModeConflict_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))
	insertAppRow(t, db, "acme", 555, "active", nil)

	// Rotation is PAT-only — App rows 409.
	var ce *organization.ConflictError
	if err := svc.AppendWebhookSecret(ctx, "acme", "x"); !errors.As(err, &ce) {
		t.Fatalf("append on app row must ConflictError, got %#v", err)
	}
	if err := svc.RemoveWebhookSecret(ctx, "acme", "x"); !errors.As(err, &ce) {
		t.Fatalf("remove on app row must ConflictError, got %#v", err)
	}

	// GetWebhookSecrets on an App row goes through the platform-secret loader,
	// which has no OpenBao wired here → "no app webhook secrets configured".
	if _, err := svc.GetWebhookSecrets(ctx, "acme"); !errors.As(err, &ce) {
		t.Fatalf("app webhook secrets w/o platform config must ConflictError, got %#v", err)
	}
}

func TestWebhookSecrets_MissingRow_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))
	var nfe *organization.NotFoundError
	if err := svc.AppendWebhookSecret(ctx, "ghost", "x"); !errors.As(err, &nfe) {
		t.Fatalf("append on missing row must NotFoundError, got %#v", err)
	}
	if err := svc.RemoveWebhookSecret(ctx, "ghost", "x"); !errors.As(err, &nfe) {
		t.Fatalf("remove on missing row must NotFoundError, got %#v", err)
	}
}

// ============================================================================
// Routing lookups (used by the webhook receiver)
// ============================================================================

func TestOrgIDByInstallationID_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))
	insertAppRow(t, db, "acme", 909, "active", nil)

	org, err := svc.OrgIDByInstallationID(ctx, 909)
	if err != nil || org != "acme" {
		t.Fatalf("lookup: org=%q err=%v", org, err)
	}
	var nfe *organization.NotFoundError
	if _, err := svc.OrgIDByInstallationID(ctx, 111111); !errors.As(err, &nfe) {
		t.Fatalf("absent install must NotFoundError, got %#v", err)
	}
}

func TestOrgIDByRepoFullName_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))

	// Canonical clone URL for acme/web.
	if err := db.Create(&sourcecontrol.GitRepository{OrgID: "acme", ProjectID: "web", RepoURL: "https://github.com/acme-org/web"}).Error; err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	// A .git-suffixed clone URL for a second repo.
	if err := db.Create(&sourcecontrol.GitRepository{OrgID: "globex", ProjectID: "svc", RepoURL: "https://github.com/globex-org/svc.git"}).Error; err != nil {
		t.Fatalf("seed repo 2: %v", err)
	}
	// A same-suffix repo hosted elsewhere — must NOT match "acme-org/web"
	// (the lookup is anchored on host+owner+repo, not an unanchored LIKE).
	if err := db.Create(&sourcecontrol.GitRepository{OrgID: "evil", ProjectID: "x", RepoURL: "https://evil.example.com/acme-org/web"}).Error; err != nil {
		t.Fatalf("seed repo 3: %v", err)
	}

	if org, err := svc.OrgIDByRepoFullName(ctx, "acme-org/web"); err != nil || org != "acme" {
		t.Fatalf("exact match: org=%q err=%v", org, err)
	}
	if org, err := svc.OrgIDByRepoFullName(ctx, "globex-org/svc"); err != nil || org != "globex" {
		t.Fatalf(".git match: org=%q err=%v", org, err)
	}

	var nfe *organization.NotFoundError
	if _, err := svc.OrgIDByRepoFullName(ctx, "nobody/nope"); !errors.As(err, &nfe) {
		t.Fatalf("absent repo must NotFoundError, got %#v", err)
	}
	if _, err := svc.OrgIDByRepoFullName(ctx, ""); !errors.As(err, &nfe) {
		t.Fatalf("empty repo must NotFoundError, got %#v", err)
	}
}

// ============================================================================
// Installation status flips + repo merge
// ============================================================================

func TestSuspendUnsuspendInstallation_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))
	insertAppRow(t, db, "acme", 42, "active", nil)

	if err := svc.SuspendInstallation(ctx, 42); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if row := getRow(t, db, "acme"); row.Status != "suspended" {
		t.Fatalf("after suspend: %q", row.Status)
	}
	if err := svc.UnsuspendInstallation(ctx, 42); err != nil {
		t.Fatalf("unsuspend: %v", err)
	}
	if row := getRow(t, db, "acme"); row.Status != "active" {
		t.Fatalf("after unsuspend: %q", row.Status)
	}
	// Missing install is idempotent (webhook may precede the connect row).
	if err := svc.SuspendInstallation(ctx, 999999); err != nil {
		t.Fatalf("suspend missing install must be idempotent nil, got %v", err)
	}
}

func TestMergeSelectedRepos_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))
	insertAppRow(t, db, "acme", 77, "active", []string{"acme-org/a", "acme-org/b"})

	if err := svc.MergeSelectedRepos(ctx, 77, []string{"acme-org/c"}, []string{"acme-org/a"}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	row := getRow(t, db, "acme")
	got := map[string]bool{}
	for _, r := range row.SelectedRepos {
		got[r] = true
	}
	if len(got) != 2 || !got["acme-org/b"] || !got["acme-org/c"] || got["acme-org/a"] {
		t.Fatalf("merged set: %v", row.SelectedRepos)
	}
	var nfe *organization.NotFoundError
	if err := svc.MergeSelectedRepos(ctx, 424242, nil, nil); !errors.As(err, &nfe) {
		t.Fatalf("merge on missing install must NotFoundError, got %#v", err)
	}
}

// ============================================================================
// Identity drift + validator bookkeeping
// ============================================================================

func TestRecordIdentityFromGitHub_Drift_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc, _ := newCredSvcDB(t, db, gh)
	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// New login → drift recorded, name/email defaulted from login.
	drifted, err := svc.RecordIdentityFromGitHub(ctx, "acme", "bob", "", "")
	if err != nil || !drifted {
		t.Fatalf("expected drift: drifted=%v err=%v", drifted, err)
	}
	row := getRow(t, db, "acme")
	if row.IdentityLogin != "bob" || row.IdentityName != "bob" || row.IdentityEmail != "bob@users.noreply.github.com" {
		t.Fatalf("identity after drift: %+v", row)
	}
	if row.PrevIdentityLogin == nil || *row.PrevIdentityLogin != "ada" || row.IdentityChangedAt == nil {
		t.Fatalf("drift columns: prev=%v changed=%v", row.PrevIdentityLogin, row.IdentityChangedAt)
	}

	// Same login again → no drift.
	drifted, err = svc.RecordIdentityFromGitHub(ctx, "acme", "bob", "Bob B", "bob@x.io")
	if err != nil || drifted {
		t.Fatalf("no-drift expected: drifted=%v err=%v", drifted, err)
	}
}

func TestTouchValidatedAt_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc, _ := newCredSvcDB(t, db, gh)
	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	before := getRow(t, db, "acme").LastValidatedAt

	if err := svc.TouchValidatedAt(ctx, "acme"); err != nil {
		t.Fatalf("touch: %v", err)
	}
	after := getRow(t, db, "acme").LastValidatedAt
	if before == nil || after == nil || after.Before(*before) {
		t.Fatalf("last_validated_at must advance: before=%v after=%v", before, after)
	}
}

func TestListActiveRows_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))
	insertAppRow(t, db, "acme", 1, "active", nil)
	insertAppRow(t, db, "globex", 2, "suspended", nil)
	insertAppRow(t, db, "initech", 3, "disconnected", nil)

	rows, err := svc.ListActiveRows(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.OcOrgID] = true
	}
	// active + suspended are walked by the validator; disconnected is excluded.
	if !got["acme"] || !got["globex"] || got["initech"] {
		t.Fatalf("ListActiveRows set: %v", got)
	}
}

// ============================================================================
// Org isolation — two orgs' rows never bleed
// ============================================================================

func TestOrgIsolation_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := newStubGitHub(t)
	gh.on("GET", "/user", 200, `{"login":"ada","name":"Ada","email":"ada@x.io"}`)
	gh.on("GET", "/orgs/ada/repos", 200, `[]`)
	svc, _ := newCredSvcDB(t, db, gh)

	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "pa", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect acme: %v", err)
	}
	if _, err := svc.Connect(ctx, "globex", organization.ConnectRequest{Kind: "user-pat", PAT: "pg", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect globex: %v", err)
	}

	// Each org's Status returns its own row.
	if p, _ := svc.Status(ctx, "acme"); p.OcOrgID != "acme" {
		t.Fatalf("acme status org: %q", p.OcOrgID)
	}
	if p, _ := svc.Status(ctx, "globex"); p.OcOrgID != "globex" {
		t.Fatalf("globex status org: %q", p.OcOrgID)
	}

	// Disconnecting one must not touch the other.
	if err := svc.Disconnect(ctx, "acme"); err != nil {
		t.Fatalf("disconnect acme: %v", err)
	}
	if row := getRow(t, db, "acme"); row.Status != "disconnected" {
		t.Fatalf("acme should be disconnected: %q", row.Status)
	}
	if row := getRow(t, db, "globex"); row.Status != "active" {
		t.Fatalf("globex must remain active, got %q", row.Status)
	}
}

// ============================================================================
// SM-API reseed bundle (no writer configured → idempotent no-op)
// ============================================================================

func TestPrepareSMAPISeed_NoTriplet_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc, _ := newCredSvcDB(t, db, gh)

	// Absent org → (nil, nil).
	if b, err := svc.PrepareSMAPISeed(ctx, "ghost"); b != nil || err != nil {
		t.Fatalf("absent org: b=%v err=%v", b, err)
	}
	// Connected PAT but no SM-API triplet stamped (writer disabled) → (nil, nil).
	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if b, err := svc.PrepareSMAPISeed(ctx, "acme"); b != nil || err != nil {
		t.Fatalf("no-triplet: b=%v err=%v", b, err)
	}
}

// ============================================================================
// golden key-set helpers
// ============================================================================

// goldenKeys returns the sorted JSON keys of the harvested connected-status
// golden — the on-wire shape a live PAT connection serves.
func goldenKeys(t testing.TB) []string {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/harvest/golden/get_org_credentials_github.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("golden json: %v", err)
	}
	return sortedKeys(m)
}

// projectionKeys marshals the projection and returns its sorted JSON keys.
func projectionKeys(t testing.TB, p *organization.Projection) []string {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("projection json: %v", err)
	}
	return sortedKeys(m)
}

// sortedKeys is the generic helper from organization_component_test.go (same
// package) — not redefined here to avoid a duplicate declaration.

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestConnectApp_EntryGuards_DB pins the two App-mode dispatch guards that are
// reachable WITHOUT a real GitHub App key: a missing installationId is a
// ValidationError before any lock/lookup, and a no-app minter (AppID()==0 —
// this deployment has no App configured) is a ConflictError. Everything past
// those guards (install fetch, bot identity, token mint) needs real App
// material and is integration-owned.
func TestConnectApp_EntryGuards_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc, _ := newCredSvcDB(t, db, newStubGitHub(t))

	_, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "app-installation", InstallationID: 0})
	assertValidationCode(t, err, "installation_id_missing")

	_, err = svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "app-installation", InstallationID: 5})
	var ce *organization.ConflictError
	if !errors.As(err, &ce) || ce.Reason != "GitHub App not configured on this deployment" {
		t.Fatalf("no-app minter must yield the not-configured ConflictError, got %v", err)
	}
	// Neither guard may leave a row behind.
	var n int64
	if err := db.Model(&organization.OrgCredential{}).Where("oc_org_id = ?", "acme").Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("entry guards must not persist anything: n=%d err=%v", n, err)
	}
}
