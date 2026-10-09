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
	"reflect"
	"regexp"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// prePhase29Shape rebuilds, on a migrated test database, the secret columns
// every release before phase29 had: org_secrets keyed by `key` with a nullable
// value and updated_at beside the reference columns, the reference triplets
// and the sealed or previewed value columns of the four credential tables,
// and the webhook CHECK. Tests of earlier steps call it so their pre-state
// seeds are legal; the next boot's phase29 converges it again.
func prePhase29Shape(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		`ALTER TABLE org_secrets RENAME COLUMN secret TO key`,
		`ALTER TABLE org_secrets ADD COLUMN value TEXT`,
		`ALTER TABLE org_secrets ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
		`ALTER TABLE org_secrets ALTER COLUMN secret_ref_name DROP NOT NULL`,
		`ALTER TABLE org_secrets ALTER COLUMN written_at DROP NOT NULL, ALTER COLUMN written_at DROP DEFAULT`,
		`ALTER TABLE org_credentials
		   ADD COLUMN pat_secret_ref TEXT, ADD COLUMN webhook_secrets JSONB,
		   ADD COLUMN secret_ref_name TEXT, ADD COLUMN secret_ref_kv_path TEXT,
		   ADD COLUMN secret_ref_property TEXT, ADD COLUMN secret_ref_written_at TIMESTAMPTZ,
		   ADD CONSTRAINT secrets_shape_per_kind CHECK (
		     (kind = 'user-pat' AND webhook_secrets IS NOT NULL AND jsonb_array_length(webhook_secrets) >= 1)
		     OR (kind = 'app-installation' AND webhook_secrets IS NULL))`,
		`ALTER TABLE org_model_connections
		   ADD COLUMN key_preview TEXT NOT NULL, ADD COLUMN secret_ref_name TEXT,
		   ADD COLUMN secret_ref_kv_path TEXT, ADD COLUMN secret_ref_property TEXT`,
		`ALTER TABLE org_anthropic_credentials
		   ADD COLUMN key_prefix TEXT NOT NULL, ADD COLUMN key_last4 TEXT NOT NULL,
		   ADD COLUMN secret_ref_name TEXT, ADD COLUMN secret_ref_kv_path TEXT, ADD COLUMN secret_ref_property TEXT`,
		`ALTER TABLE organization_idp_profiles
		   ADD COLUMN publisher_client_secret TEXT, ADD COLUMN publisher_secret_ref TEXT,
		   ADD COLUMN secret_ref_name TEXT, ADD COLUMN secret_ref_kv_path TEXT,
		   ADD COLUMN secret_ref_property TEXT, ADD COLUMN secret_ref_written_at TIMESTAMPTZ`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("rebuild the pre-phase29 shape (%s): %v", stmt, err)
		}
	}
	dropIdleConnections(t, db)
}

// droppedSecretColumns is every column phase29 drops; none may exist after it.
var droppedSecretColumns = []struct{ table, column string }{
	{"org_secrets", "key"}, {"org_secrets", "value"}, {"org_secrets", "updated_at"},
	{"org_credentials", "pat_secret_ref"}, {"org_credentials", "webhook_secrets"},
	{"org_credentials", "secret_ref_name"}, {"org_credentials", "secret_ref_kv_path"},
	{"org_credentials", "secret_ref_property"}, {"org_credentials", "secret_ref_written_at"},
	{"org_model_connections", "key_preview"}, {"org_model_connections", "secret_ref_name"},
	{"org_model_connections", "secret_ref_kv_path"}, {"org_model_connections", "secret_ref_property"},
	{"org_anthropic_credentials", "key_prefix"}, {"org_anthropic_credentials", "key_last4"},
	{"org_anthropic_credentials", "secret_ref_name"}, {"org_anthropic_credentials", "secret_ref_kv_path"},
	{"org_anthropic_credentials", "secret_ref_property"},
	{"organization_idp_profiles", "publisher_client_secret"}, {"organization_idp_profiles", "publisher_secret_ref"},
	{"organization_idp_profiles", "secret_ref_name"}, {"organization_idp_profiles", "secret_ref_kv_path"},
	{"organization_idp_profiles", "secret_ref_property"}, {"organization_idp_profiles", "secret_ref_written_at"},
}

// assertRefsOnly checks the end state phase29 promises: org_secrets is
// exactly (oc_org_id, secret, secret_ref_name, written_at), every column
// NOT NULL, written_at defaulting to now(), keyed by (oc_org_id, secret); no
// dropped column and no webhook CHECK survive.
func assertRefsOnly(t *testing.T, db *gorm.DB) {
	t.Helper()
	var cols []struct {
		Name, Nullable string
		Default        *string
	}
	if err := db.Raw(`SELECT column_name AS name, is_nullable AS nullable, column_default AS default
		FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'org_secrets'
		ORDER BY ordinal_position`).Scan(&cols).Error; err != nil {
		t.Fatalf("read org_secrets columns: %v", err)
	}
	var names []string
	for _, c := range cols {
		names = append(names, c.Name)
		if c.Nullable != "NO" {
			t.Errorf("org_secrets.%s is nullable", c.Name)
		}
		if c.Name == "written_at" && (c.Default == nil || *c.Default != "now()") {
			t.Errorf("org_secrets.written_at default = %v, want now()", c.Default)
		}
	}
	if want := []string{"oc_org_id", "secret", "secret_ref_name", "written_at"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("org_secrets = %v, want %v", names, want)
	}
	var pk []string
	if err := db.Raw(`SELECT a.attname FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = 'org_secrets'::regclass AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)`).Scan(&pk).Error; err != nil {
		t.Fatalf("read org_secrets primary key: %v", err)
	}
	if want := []string{"oc_org_id", "secret"}; !reflect.DeepEqual(pk, want) {
		t.Fatalf("org_secrets primary key = %v, want %v", pk, want)
	}
	for _, c := range droppedSecretColumns {
		if columnExists(t, db, c.table, c.column) {
			t.Errorf("%s.%s survived", c.table, c.column)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'org_credentials'::regclass AND conname = 'secrets_shape_per_kind'`); n != 0 {
		t.Error("the webhook CHECK secrets_shape_per_kind survived")
	}
}

// A fresh database ends refs-only, and boots twice more on that shape: every
// step before phase29 runs clean on a schema phase29 already converged.
func TestMigrate_FreshTwice(t *testing.T) {
	db := dbtest.New(t)
	assertRefsOnly(t, db)
	bootMigrate(t, db)
	bootMigrate(t, db)
	assertRefsOnly(t, db)
}

// The upgrade a deployment goes through: the schema of the release before
// phase29, holding real rows, then two boots. The org's six reference rows
// stay (a row means "set"); every ref-less row, the legacy value rows of
// every era included, goes; the credential tables keep their rows without
// their value, preview and triplet columns.
func TestMigrate_TwiceOnAPrePhase29SchemaEndsRefsOnly(t *testing.T) {
	db := dbtest.New(t)
	prePhase29Shape(t, db)

	seeds := []string{
		// The six reference rows, one stamped before written_at existed.
		`INSERT INTO org_secrets (oc_org_id, key, value, secret_ref_name, written_at) VALUES
		   ('acme', 'github-pat', NULL, 'acme-github-pat-0a1b2c3d', now()),
		   ('acme', 'github-webhook-secret', NULL, 'acme-github-webhook-secret-0a1b2c3d', now()),
		   ('acme', 'default-key', NULL, 'acme-default-key-0a1b2c3d', now()),
		   ('acme', 'coding-agent-key', NULL, 'acme-coding-agent-key-0a1b2c3d', now()),
		   ('acme', 'ae-publisher-client', NULL, 'acme-ae-publisher-client-0a1b2c3d', now()),
		   ('acme', 'ae-studio-client', NULL, 'acme-ae-studio-client-0a1b2c3d', NULL)`,
		// Legacy value rows of every era, and a blank reference: none names a reference.
		`INSERT INTO org_secrets (oc_org_id, key, value) VALUES
		   ('acme', 'github/pat', 'sealed:pat'), ('acme', 'anthropic/key', 'sealed:key'),
		   ('acme', 'model/key', 'sealed:key'), ('acme', 'anthropic/coding-key', 'sealed:token'),
		   ('legacy', 'github/pat', 'sealed:pat')`,
		`INSERT INTO org_secrets (oc_org_id, key, value, secret_ref_name) VALUES ('blank', 'github-pat', NULL, '')`,
		`INSERT INTO org_credentials (oc_org_id, kind, github_login, identity_name, identity_email, identity_login,
		   pat_secret_ref, webhook_secrets, secret_ref_name, secret_ref_kv_path, secret_ref_property, secret_ref_written_at)
		 VALUES ('acme', 'user-pat', 'octo', 'Octo', 'octo@example.com', 'octo',
		   'acme-pat', '[{"secret":"sealed:hook","added_at":"2026-09-01T00:00:00Z"}]',
		   'acme-github', 'user-app-secrets/wc-acme/acme-github', 'token', now())`,
		`INSERT INTO org_model_connections (oc_org_id, format, base_url, host, model, auth_scheme, image_input,
		   key_preview, connected_at, updated_at, secret_ref_name, secret_ref_kv_path, secret_ref_property)
		 VALUES ('acme', 'anthropic', 'https://api.anthropic.com', 'api.anthropic.com', 'claude-sonnet-5', 'x-api-key', 'yes',
		   'sk-a…1234', now(), now(), 'acme-anthropic', 'user-app-secrets/wc-acme/acme-anthropic', 'api-key')`,
		`INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status,
		   secret_ref_name, secret_ref_kv_path, secret_ref_property)
		 VALUES ('acme', 'coding', 'oauth_token', 'sk-ant-oat01-Ab', '5678', 'active',
		   'acme-anthropic-coding', 'user-app-secrets/wc-acme/acme-anthropic-coding', 'api-key')`,
		`INSERT INTO organization_idp_profiles (org_id, kind, issuer, jwks_url, publisher_client_id,
		   publisher_client_secret, publisher_secret_ref, secret_ref_name, secret_ref_kv_path, secret_ref_property, secret_ref_written_at)
		 VALUES ('acme', 'platform', 'https://thunder.example', 'https://thunder.example/jwks', 'acme-publisher',
		   'sealed:client-secret', 'aep/publisher/acme', 'acme-publisher', 'user-app-secrets/wc-acme/acme-publisher', 'client_secret', now())`,
	}
	for _, s := range seeds {
		if err := db.Exec(s).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, s)
		}
	}

	bootMigrate(t, db)
	bootMigrate(t, db)

	assertRefsOnly(t, db)
	if n := count(t, db, `SELECT count(*) FROM org_secrets WHERE oc_org_id = 'acme'`); n != 6 {
		t.Fatalf("acme rows = %d, want the six referenced ones (the ref-less legacy rows are gone)", n)
	}
	if n := count(t, db, `SELECT count(*) FROM org_secrets WHERE oc_org_id IN ('legacy', 'blank')`); n != 0 {
		t.Fatalf("%d ref-less row(s) survived", n)
	}
	if n := count(t, db, `SELECT count(*) FROM org_secrets
		WHERE oc_org_id = 'acme' AND secret = 'ae-studio-client' AND secret_ref_name = 'acme-ae-studio-client-0a1b2c3d'`); n != 1 {
		t.Fatal("the reference stamped before written_at existed was lost")
	}
	for table, where := range map[string]string{
		"org_credentials":           "oc_org_id = 'acme' AND kind = 'user-pat'",
		"org_model_connections":     "oc_org_id = 'acme'",
		"org_anthropic_credentials": "oc_org_id = 'acme' AND role = 'coding'",
		"organization_idp_profiles": "org_id = 'acme' AND publisher_client_id = 'acme-publisher'",
	} {
		if n := count(t, db, `SELECT count(*) FROM `+table+` WHERE `+where); n != 1 {
			t.Errorf("%s lost the org's row", table)
		}
	}
}

// No column that could hold a secret value remains anywhere in the schema
// (scenario 2.6 as a dbtest). The regex finds every candidate; a candidate is
// allowed only if it holds a name: org_secrets.secret, a reference name
// (*_ref_name, *_secret_ref), or the one sealed column the ColumnCipher still
// owns, test_users.password_sealed. The columns phase29 drops are checked by
// name too, because the reference triplet's secret_ref_name would otherwise
// pass the name rule.
func TestSchema_NoValueColumnsRemain(t *testing.T) {
	db := dbtest.New(t)
	bootMigrate(t, db)
	var got []string
	if err := db.Raw(`SELECT table_name || '.' || column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name ~ '(secret|key_preview|key_prefix|key_last4|sealed|^value$)'
		ORDER BY 1`).Scan(&got).Error; err != nil {
		t.Fatalf("read columns: %v", err)
	}
	allowed := map[string]bool{"org_secrets.secret": true, "test_users.password_sealed": true}
	nameHolder := regexp.MustCompile(`\.[a-z_]*(_ref_name|_secret_ref)$`)
	for _, c := range got {
		if !allowed[c] && !nameHolder.MatchString(c) {
			t.Errorf("unexpected column %s", c)
		}
	}
	for _, c := range droppedSecretColumns {
		if columnExists(t, db, c.table, c.column) {
			t.Errorf("%s.%s survived", c.table, c.column)
		}
	}
}
