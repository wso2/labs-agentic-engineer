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

// DBTEST tier (skips under -short, runs on `make test-db`): the REAL
// CredentialService over a pristine per-test Postgres (dbtest.New) and a fake
// GitHub. The PAT lives only in vault, so no value is stored here. This is where the SQL-shaped behavior lives — the Connect
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
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// newCredSvcDB wires the real CredentialService over the dbtest DB with a fake
// GitHub.
func newCredSvcDB(t testing.TB, db *gorm.DB, gh *stubGitHub) *organization.CredentialService {
	t.Helper()
	return organization.NewCredentialService(organization.NewOrgCredentialRepository(db, nil)).WithGitHubAPIBase(gh.URL)
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
	svc := newCredSvcDB(t, db, gh)

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

	// webhook_secrets is seeded non-empty (the CHECK needs it) until phase 6
	// drops the column.
	row := getRow(t, db, "acme")
	if len(row.WebhookSecrets) != 1 || row.WebhookSecrets[0].Secret == "" {
		t.Fatalf("webhook_secrets seed: %+v", row.WebhookSecrets)
	}

	// The PAT lives only in vault: Connect writes no github/pat entry.
	var pats int64
	if err := db.Raw(`SELECT count(*) FROM org_secrets WHERE oc_org_id = 'acme' AND key = 'github/pat'`).Scan(&pats).Error; err != nil || pats != 0 {
		t.Fatalf("Connect stored the PAT in Postgres: rows %d (%v)", pats, err)
	}

	// The on-wire projection shape matches the harvested golden's key-set.
	if got, want := projectionKeys(t, proj), goldenKeys(t); !equalStrs(got, want) {
		t.Fatalf("projection keys drifted from golden:\n got=%v\nwant=%v", got, want)
	}
}

// The gitpat lives only in vault: after a PAT connect no table of the schema
// holds the PAT, and (from Task 6.5) no value-bearing column is left.
func TestConnectPAT_WritesNoValueToPostgres(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	const pat = "ghp_testvalue_1234567890"
	gh := patHappyGitHub(t, "gh-org", "GH Org", "gh@example.com")
	svc := newCredSvcDB(t, db, gh)

	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: pat, GitHubLogin: "gh-org"}); err != nil {
		t.Fatal(err)
	}

	t.Run("no github/pat entry", func(t *testing.T) {
		var n int64
		if err := db.Raw(`SELECT count(*) FROM org_secrets WHERE key = 'github/pat'`).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d org_secrets rows hold the PAT", n)
		}
	})
	t.Run("value in no table", func(t *testing.T) {
		assertNoValueInDump(t, db, pat)
	})
	t.Run("no value-bearing columns", func(t *testing.T) {
		t.Skip("columns dropped in Task 6.5")
		var n int64
		if err := db.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_name IN ('org_secrets','org_credentials')
		        AND column_name IN ('value','webhook_secrets','pat_secret_ref','secret_ref_kv_path','secret_ref_property')`).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("%d value-bearing columns still exist", n)
		}
	})
}

// assertNoValueInDump fails if any row of any table in the schema, rendered
// with row_to_json, contains value.
func assertNoValueInDump(t *testing.T, db *gorm.DB, value string) {
	t.Helper()
	var tables []string
	if err := db.Raw(`SELECT quote_ident(table_name) FROM information_schema.tables
	        WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`).Scan(&tables).Error; err != nil {
		t.Fatalf("list tables: %v", err)
	}
	if len(tables) == 0 {
		t.Fatal("no tables in the schema")
	}
	for _, table := range tables {
		var rows []string
		if err := db.Raw(`SELECT row_to_json(t)::text FROM ` + table + ` t`).Scan(&rows).Error; err != nil {
			t.Fatalf("dump %s: %v", table, err)
		}
		for _, row := range rows {
			if strings.Contains(row, value) {
				t.Fatalf("table %s holds the value", table)
			}
		}
	}
}

func TestConnectPAT_InvalidPAT_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := newStubGitHub(t)
	gh.on("GET", "/user", 401, `{"message":"Bad credentials"}`)
	svc := newCredSvcDB(t, db, gh)

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
	svc := newCredSvcDB(t, db, newStubGitHub(t))

	_, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "", GitHubLogin: "ada"})
	assertValidationCode(t, err, "pat_missing")

	_, err = svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: ""})
	assertValidationCode(t, err, "github_login_missing")
}

func TestConnect_UnknownKind_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc := newCredSvcDB(t, db, newStubGitHub(t))
	_, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "wat"})
	assertValidationCode(t, err, "kind_invalid")
}

func TestConnectPAT_ReplaceRecordsDrift_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada Lovelace", "ada@example.com")
	svc := newCredSvcDB(t, db, gh)

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
	svc := newCredSvcDB(t, db, newStubGitHub(t))
	_, err := svc.Status(context.Background(), "ghost")
	var nfe *organization.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("want *organization.NotFoundError, got %#v", err)
	}
}

// ============================================================================
// Disconnect
// ============================================================================

func TestDisconnect_FlipsTheRowToDisconnected_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc := newCredSvcDB(t, db, gh)

	if _, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := svc.Disconnect(ctx, "acme"); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	if row := getRow(t, db, "acme"); row.Status != "disconnected" {
		t.Fatalf("status after disconnect: %q", row.Status)
	}
}

func TestDisconnect_Idempotent_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc := newCredSvcDB(t, db, gh)

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
// Identity drift + validator bookkeeping
// ============================================================================

func TestRecordIdentityFromGitHub_Drift_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	svc := newCredSvcDB(t, db, gh)
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

func TestListActiveRows_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc := newCredSvcDB(t, db, newStubGitHub(t))
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
	svc := newCredSvcDB(t, db, gh)

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

// TestConnect_RefusesTheRetiredAppKind_DB: the GitHub App connect went with
// App mode, so its kind is refused like any unknown one and nothing persists.
func TestConnect_RefusesTheRetiredAppKind_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	svc := newCredSvcDB(t, db, newStubGitHub(t))

	_, err := svc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "app-installation"})
	assertValidationCode(t, err, "kind_invalid")
	var n int64
	if err := db.Model(&organization.OrgCredential{}).Where("oc_org_id = ?", "acme").Count(&n).Error; err != nil || n != 0 {
		t.Fatalf("a refused connect must not persist anything: n=%d err=%v", n, err)
	}
}
