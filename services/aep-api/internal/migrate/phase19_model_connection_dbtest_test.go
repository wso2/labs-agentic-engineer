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

package migrate_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// legacySecrets writes and reads org_secrets value rows the way the earlier
// releases' sealed store did (one row per (org, key), the value as stored):
// the steps under test copy or delete those rows and never read a value, so
// the tests seed opaque text and compare it as stored.
type legacySecrets struct{ db *gorm.DB }

func (s legacySecrets) Put(ctx context.Context, org, key string, value []byte) error {
	return s.db.WithContext(ctx).Exec(`
		INSERT INTO org_secrets (oc_org_id, key, value, updated_at) VALUES (?, ?, ?, now())
		ON CONFLICT (oc_org_id, key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		org, key, "sealed:"+string(value)).Error
}

func (s legacySecrets) Get(ctx context.Context, org, key string) ([]byte, error) {
	var value string
	err := s.db.WithContext(ctx).Raw(`SELECT value FROM org_secrets WHERE oc_org_id = ? AND key = ?`, org, key).Row().Scan(&value)
	return []byte(strings.TrimPrefix(value, "sealed:")), err
}

// preModelConnectionShape rebuilds the schema phase19 starts from on a
// migrated test database: the secret columns of the releases before phase29,
// org_anthropic_credentials may hold a default row, org_agent_settings has
// its model column, and org_model_connections does not exist. Tests of
// earlier steps call it so their pre-state seeds are legal.
func preModelConnectionShape(t *testing.T, db *gorm.DB) {
	t.Helper()
	prePhase29Shape(t, db)
	for _, stmt := range []string{
		`ALTER TABLE org_anthropic_credentials DROP CONSTRAINT IF EXISTS org_anthropic_credentials_subscription_only`,
		`ALTER TABLE org_agent_settings ADD COLUMN IF NOT EXISTS model TEXT NOT NULL DEFAULT 'claude-sonnet-5'`,
		`DROP TABLE IF EXISTS org_model_connections`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("rebuild the pre-phase19 shape (%s): %v", stmt, err)
		}
	}
	dropIdleConnections(t, db)
}

// phase19Fixture seeds the orgs the step must handle, each named for its case.
type phase19Fixture struct {
	disconnectedAt time.Time
}

func seedPhase19(t *testing.T, db *gorm.DB, store legacySecrets) phase19Fixture {
	t.Helper()
	ctx := context.Background()
	disconnectedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	stmts := []struct {
		sql  string
		args []any
	}{
		{sql: `INSERT INTO organizations (uuid, name) VALUES
		   (gen_random_uuid(), 'active'), (gen_random_uuid(), 'invalid'), (gen_random_uuid(), 'coding'),
		   (gen_random_uuid(), 'none'), (gen_random_uuid(), 'defaults')`},
		{sql: `INSERT INTO organizations (uuid, name, llm_disconnected_at) VALUES (gen_random_uuid(), 'disconnected', ?)`, args: []any{disconnectedAt}},
		// An active default key with a chosen model and runtime, mirrored.
		{sql: `INSERT INTO org_anthropic_credentials
		   (oc_org_id, role, credential_kind, key_prefix, key_last4, status, connected_at, last_validated_at,
		    secret_ref_name, secret_ref_kv_path, secret_ref_property)
		   VALUES ('active', 'default', 'api_key', 'sk-ant-api03-Ab', '1234', 'active', '2026-08-01T00:00:00Z', '2026-08-02T00:00:00Z',
		           'active-anthropic', 'user-app-secrets/wc-active/active-anthropic', 'api-key')`},
		{sql: `INSERT INTO org_agent_settings (oc_org_id, runtime, model, updated_by, updated_at)
		   VALUES ('active', 'opencode', 'claude-haiku-4-5', 'ada', now())`},
		// An active key on the defaults: no settings row at all.
		{sql: `INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
		   VALUES ('defaults', 'default', 'api_key', 'sk-ant-api03-De', '5678', 'active')`},
		// An invalid key, with a subscription that cannot outlive it.
		{sql: `INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status, validation_error)
		   VALUES ('invalid', 'default', 'api_key', 'sk-ant-api03-In', '0000', 'invalid', 'rejected')`},
		{sql: `INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
		   VALUES ('invalid', 'coding', 'oauth_token', 'sk-ant-oat01-In', '1111', 'active')`},
		// An active key with a Claude subscription beside it.
		{sql: `INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
		   VALUES ('coding', 'default', 'api_key', 'sk-ant-api03-Co', '2222', 'active'),
		          ('coding', 'coding', 'oauth_token', 'sk-ant-oat01-Co', '3333', 'active')`},
	}
	for _, s := range stmts {
		if err := db.Exec(s.sql, s.args...).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, s.sql)
		}
	}
	for org, key := range map[string]string{
		"active": "sk-ant-api03-AbActiveKeyBytes-1234", "defaults": "sk-ant-api03-DeDefaultsKeyBytes-5678",
		"invalid": "sk-ant-api03-InInvalidKeyBytes-0000", "coding": "sk-ant-api03-CoCodingKeyBytes-2222",
	} {
		if err := store.Put(ctx, org, "anthropic/key", []byte(key)); err != nil {
			t.Fatalf("seed key bytes %s: %v", org, err)
		}
	}
	for _, org := range []string{"invalid", "coding"} {
		if err := store.Put(ctx, org, "anthropic/coding-key", []byte("sk-ant-oat01-token-"+org)); err != nil {
			t.Fatalf("seed token bytes %s: %v", org, err)
		}
	}
	return phase19Fixture{disconnectedAt: disconnectedAt}
}

// The upgrade a real deployment goes through: the pre-phase19 schema holding
// every kind of org, then the actual boot path — AutoMigrate, then every step.
func TestPhase19ModelConnection_UpgradesAPopulatedDatabase(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	store := legacySecrets{db: db}
	preModelConnectionShape(t, db)
	fx := seedPhase19(t, db, store)

	bootMigrate(t, db)

	conns := organization.NewOrgModelConnectionRepository(db)
	// The active org: one connection on Anthropic's API with its model, NULL
	// limits and no author. No key, preview or vault path is carried.
	active, err := conns.GetByOrg(ctx, "active")
	if err != nil || active == nil {
		t.Fatalf("active org's connection: %+v (%v)", active, err)
	}
	want := organization.OrgModelConnection{
		OcOrgID: "active", Format: modelconn.FormatAnthropic, BaseURL: modelconn.AnthropicBaseURL,
		Host: modelconn.AnthropicHost, Model: "claude-haiku-4-5", AuthScheme: modelconn.AuthXAPIKey,
		ImageInput: modelconn.Yes,
	}
	got := *active
	if !got.ConnectedAt.Equal(time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)) || !got.UpdatedAt.Equal(time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("timestamps: connected %v updated %v", got.ConnectedAt, got.UpdatedAt)
	}
	got.ConnectedAt, got.UpdatedAt = time.Time{}, time.Time{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("active connection:\n got %+v\nwant %+v", got, want)
	}
	// The key's sealed bytes go with the same boot: they name no reference,
	// so phase29 deletes them.
	if n := count(t, db, `SELECT count(*) FROM org_secrets WHERE oc_org_id = 'active'`); n != 0 {
		t.Fatalf("active org's key bytes = %d rows, want 0", n)
	}
	// An org that never chose a model gets the format's default.
	if d, err := conns.GetByOrg(ctx, "defaults"); err != nil || d == nil || d.Model != modelconn.DefaultAnthropicModel {
		t.Fatalf("defaults org: %+v (%v)", d, err)
	}
	// The coding org keeps its subscription beside its new connection.
	if c, err := conns.GetByOrg(ctx, "coding"); err != nil || c == nil {
		t.Fatalf("coding org's connection: %+v (%v)", c, err)
	}
	if n := count(t, db, `SELECT count(*) FROM org_anthropic_credentials WHERE oc_org_id = 'coding' AND role = 'coding'`); n != 1 {
		t.Fatalf("coding org's subscription rows = %d, want 1", n)
	}
	// The invalid org has none, is marked disconnected, and loses its bytes and subscription.
	if inv, err := conns.GetByOrg(ctx, "invalid"); err != nil || inv != nil {
		t.Fatalf("invalid org must have no connection: %+v (%v)", inv, err)
	}
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE name = 'invalid' AND llm_disconnected_at IS NOT NULL`); n != 1 {
		t.Fatal("the invalid org's llm_disconnected_at is not set")
	}
	if n := count(t, db, `SELECT count(*) FROM org_secrets WHERE oc_org_id = 'invalid'`); n != 0 {
		t.Fatalf("the invalid org kept %d unreachable secret(s)", n)
	}
	if n := count(t, db, `SELECT count(*) FROM org_anthropic_credentials WHERE oc_org_id = 'invalid'`); n != 0 {
		t.Fatalf("the invalid org kept %d credential row(s)", n)
	}
	// The disconnected org and the org with no rows are untouched.
	var disconnected time.Time
	if err := db.Raw(`SELECT llm_disconnected_at FROM organizations WHERE name = 'disconnected'`).Scan(&disconnected).Error; err != nil ||
		!disconnected.Equal(fx.disconnectedAt) {
		t.Fatalf("disconnected org's timestamp moved: %v (%v)", disconnected, err)
	}
	if n := count(t, db, `SELECT count(*) FROM org_model_connections WHERE oc_org_id IN ('disconnected', 'none')`); n != 0 {
		t.Fatalf("an org with no key gained a connection")
	}
	if n := count(t, db, `SELECT count(*) FROM organizations WHERE name = 'none' AND llm_disconnected_at IS NOT NULL`); n != 0 {
		t.Fatal("an org that never had a key was marked disconnected")
	}
	// The contract: no default rows, the CHECK, no model column.
	if n := count(t, db, `SELECT count(*) FROM org_anthropic_credentials WHERE role = 'default'`); n != 0 {
		t.Fatalf("%d default row(s) survived", n)
	}
	if columnExists(t, db, "org_agent_settings", "model") {
		t.Fatal("org_agent_settings.model survived")
	}
	if err := db.Exec(`INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, status)
		VALUES ('late', 'default', 'api_key', 'active')`).Error; err == nil ||
		!strings.Contains(err.Error(), "subscription_only") {
		t.Fatalf("a default row is still accepted: %v", err)
	}
	// The runtime survives the dropped column.
	if n := count(t, db, `SELECT count(*) FROM org_agent_settings WHERE oc_org_id = 'active' AND runtime = 'opencode'`); n != 1 {
		t.Fatal("the active org's runtime was lost")
	}

	// A second run is a no-op: no row is rewritten.
	before := phase19RowVersions(t, db)
	if err := migrate.RunPhase19ModelConnection(ctx, db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if after := phase19RowVersions(t, db); !reflect.DeepEqual(before, after) {
		t.Fatalf("a second run rewrote rows:\n before %v\n after  %v", before, after)
	}
	bootMigrate(t, db)
}

// A fresh schema is already in the final shape; the boot path runs clean and
// the step changes nothing.
func TestPhase19ModelConnection_FreshSchema(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	if columnExists(t, db, "org_agent_settings", "model") {
		t.Fatal("a fresh schema carries org_agent_settings.model")
	}
	if !columnExists(t, db, "org_model_connections", "model") {
		t.Fatal("a fresh schema has no org_model_connections")
	}
	before := phase19RowVersions(t, db)
	if err := migrate.RunPhase19ModelConnection(context.Background(), db); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if after := phase19RowVersions(t, db); !reflect.DeepEqual(before, after) {
		t.Fatalf("a re-run on a fresh schema rewrote rows")
	}
}

// A database still holding org_coding_agent_settings (upgraded straight from
// before phase17) boots: phase17 copies the runtime into org_agent_settings,
// which AutoMigrate created without a model column, and phase19 gives the org
// a connection on the format's default model.
func TestPhase19ModelConnection_UpgradesFromBeforePhase17(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	preModelConnectionShape(t, db)
	for _, stmt := range []string{
		`DROP TABLE org_agent_settings`,
		`CREATE TABLE org_coding_agent_settings (
		   oc_org_id text PRIMARY KEY, runtime text NOT NULL, model text NOT NULL,
		   updated_by text NOT NULL, updated_at timestamptz NOT NULL)`,
		`INSERT INTO org_coding_agent_settings VALUES ('acme', 'opencode', 'claude-haiku-4-5', 'ada', now())`,
		`INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
		   VALUES ('acme', 'default', 'api_key', 'sk-ant-api03-Ac', '4444', 'active')`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("rebuild the pre-phase17 shape (%s): %v", stmt, err)
		}
	}

	bootMigrate(t, db)

	if n := count(t, db, `SELECT count(*) FROM org_agent_settings WHERE oc_org_id = 'acme' AND runtime = 'opencode'`); n != 1 {
		t.Fatal("the runtime did not move across")
	}
	conn, err := organization.NewOrgModelConnectionRepository(db).GetByOrg(ctx, "acme")
	if err != nil || conn == nil || conn.Model != modelconn.DefaultAnthropicModel {
		t.Fatalf("connection: %+v (%v), want the default model", conn, err)
	}
}

// The step locks the credential tables, so a save from a replica of the
// previous release waits for it and then meets the new schema: here an old
// default-row write, blocked mid-step, is refused by the new CHECK once the
// step commits, instead of landing in a half-moved table.
func TestPhase19ModelConnection_AConcurrentSaveWaitsOnTheLock(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	preModelConnectionShape(t, db)
	if err := db.Exec(`INSERT INTO org_agent_settings (oc_org_id, runtime, model, updated_by, updated_at)
		VALUES ('acme', 'claude-code', 'claude-sonnet-5', 'ada', now())`).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Hold org_agent_settings so the step stops after locking
	// org_anthropic_credentials: "during the step".
	blocker := db.Begin()
	if err := blocker.Exec(`UPDATE org_agent_settings SET updated_by = updated_by WHERE oc_org_id = 'acme'`).Error; err != nil {
		t.Fatalf("blocker: %v", err)
	}
	stepDone := make(chan error, 1)
	go func() { stepDone <- migrate.RunPhase19ModelConnection(ctx, db) }()
	waitForLockWaiters(t, db, 1)

	saveDone := make(chan error, 1)
	go func() {
		saveDone <- db.Exec(`INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
			VALUES ('acme', 'default', 'api_key', 'sk-ant-api03-La', '9999', 'active')`).Error
	}()
	waitForLockWaiters(t, db, 2)
	select {
	case err := <-saveDone:
		t.Fatalf("the save did not wait for the step: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := blocker.Rollback().Error; err != nil {
		t.Fatalf("release the blocker: %v", err)
	}
	if err := <-stepDone; err != nil {
		t.Fatalf("step: %v", err)
	}
	if err := <-saveDone; err == nil || !strings.Contains(err.Error(), "subscription_only") {
		t.Fatalf("the waiting save must meet the new schema's CHECK, got %v", err)
	}
	if n := count(t, db, `SELECT count(*) FROM org_anthropic_credentials WHERE role = 'default'`); n != 0 {
		t.Fatalf("%d default row(s) landed", n)
	}
}

// waitForLockWaiters polls until n sessions are waiting on a lock.
func waitForLockWaiters(t *testing.T, db *gorm.DB, n int64) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if count(t, db, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()`) >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d lock waiter(s)", n)
}

func count(t *testing.T, db *gorm.DB, sql string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(sql).Scan(&n).Error; err != nil {
		t.Fatalf("count (%s): %v", sql, err)
	}
	return n
}

// phase19RowVersions reads every row's xmin in the tables the step touches.
func phase19RowVersions(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, q := range []struct{ table, key string }{
		{"org_model_connections", "oc_org_id"}, {"org_anthropic_credentials", "oc_org_id || '/' || role"},
		{"org_agent_settings", "oc_org_id"}, {"organizations", "name"}, {"org_secrets", "oc_org_id || '/' || secret"},
	} {
		var rows []struct{ Key, Xmin string }
		if err := db.Raw(`SELECT ` + q.key + ` AS key, xmin::text AS xmin FROM ` + q.table).Scan(&rows).Error; err != nil {
			t.Fatalf("read %s row versions: %v", q.table, err)
		}
		for _, r := range rows {
			out[q.table+"/"+r.Key] = r.Xmin
		}
	}
	return out
}
