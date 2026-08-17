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
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// TestPhase11SecretRefColumns_ExpandAndBackfill proves phase11 copies leftover
// sm_api_* values into secret_ref_* when the new side is still null.
// dbtest.New already dropped sm_api_*; the leftover columns are restored first.
func TestPhase11SecretRefColumns_ExpandAndBackfill(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	addLeftoverSMAPIColumns(t, db)

	for _, col := range []struct {
		table, column string
	}{
		{"org_anthropic_credentials", "secret_ref_name"},
		{"org_credentials", "secret_ref_name"},
		{"org_credentials", "secret_ref_written_at"},
		{"organization_idp_profiles", "secret_ref_name"},
		{"organization_idp_profiles", "secret_ref_written_at"},
	} {
		if !leftoverColumnExists(t, db, col.table, col.column) {
			t.Fatalf("expected column %s.%s after expand", col.table, col.column)
		}
	}

	writtenAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	if err := db.Exec(`
		INSERT INTO org_anthropic_credentials
		  (oc_org_id, role, key_prefix, key_last4, status, connected_at,
		   sm_api_secret_ref_name, sm_api_kv_path, sm_api_property)
		VALUES
		  ('org-a', 'default', 'sk-ant-api03-', 'wxyz', 'active', now(),
		   'a-anthropic', 'user-app-secrets/ns/a-anthropic', 'api-key')
	`).Error; err != nil {
		t.Fatalf("seed anthropic leftover-only: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO org_credentials
		  (oc_org_id, kind, github_login, identity_name, identity_email, identity_login,
		   webhook_secrets, status, connected_at,
		   sm_api_secret_ref_name, sm_api_kv_path, sm_api_property, sm_api_written_at)
		VALUES
		  ('org-a', 'user-pat', 'alice', 'Alice', 'a@ex.com', 'alice',
		   '[{"secret":"seed","added_at":"2026-01-01T00:00:00Z"}]'::jsonb, 'active', now(),
		   'a-pat', 'user-app-secrets/ns/a-pat', 'api-key', ?)
	`, writtenAt).Error; err != nil {
		t.Fatalf("seed org_credentials leftover-only: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO organization_idp_profiles
		  (org_id, kind, issuer, jwks_url, created_at, updated_at,
		   sm_api_secret_ref_name, sm_api_kv_path, sm_api_property, sm_api_written_at)
		VALUES
		  ('org-a', 'platform', 'https://issuer.example', 'https://issuer.example/jwks', now(), now(),
		   'a-pub', 'user-app-secrets/ns/a-pub', 'publisher', ?)
	`, writtenAt).Error; err != nil {
		t.Fatalf("seed idp leftover-only: %v", err)
	}

	if err := migrate.RunPhase11SecretRefColumns(ctx, db); err != nil {
		t.Fatalf("phase11 backfill: %v", err)
	}

	var antName, antPath, antProp string
	if err := db.Raw(`
		SELECT secret_ref_name, secret_ref_kv_path, secret_ref_property
		  FROM org_anthropic_credentials WHERE oc_org_id='org-a'
	`).Row().Scan(&antName, &antPath, &antProp); err != nil {
		t.Fatalf("read anthropic secret_ref_*: %v", err)
	}
	if antName != "a-anthropic" || antPath != "user-app-secrets/ns/a-anthropic" || antProp != "api-key" {
		t.Fatalf("anthropic backfill = (%q,%q,%q)", antName, antPath, antProp)
	}

	var patName, patPath, patProp string
	var patWritten *time.Time
	if err := db.Raw(`
		SELECT secret_ref_name, secret_ref_kv_path, secret_ref_property, secret_ref_written_at
		  FROM org_credentials WHERE oc_org_id='org-a'
	`).Row().Scan(&patName, &patPath, &patProp, &patWritten); err != nil {
		t.Fatalf("read org_credentials secret_ref_*: %v", err)
	}
	if patName != "a-pat" || patPath != "user-app-secrets/ns/a-pat" || patProp != "api-key" {
		t.Fatalf("pat backfill = (%q,%q,%q)", patName, patPath, patProp)
	}
	if patWritten == nil || !patWritten.Equal(writtenAt) {
		t.Fatalf("pat written_at backfill = %v; want %v", patWritten, writtenAt)
	}

	var idpName, idpPath, idpProp string
	var idpWritten *time.Time
	if err := db.Raw(`
		SELECT secret_ref_name, secret_ref_kv_path, secret_ref_property, secret_ref_written_at
		  FROM organization_idp_profiles WHERE org_id='org-a'
	`).Row().Scan(&idpName, &idpPath, &idpProp, &idpWritten); err != nil {
		t.Fatalf("read idp secret_ref_*: %v", err)
	}
	if idpName != "a-pub" || idpPath != "user-app-secrets/ns/a-pub" || idpProp != "publisher" {
		t.Fatalf("idp backfill = (%q,%q,%q)", idpName, idpPath, idpProp)
	}
	if idpWritten == nil || !idpWritten.Equal(writtenAt) {
		t.Fatalf("idp written_at backfill = %v; want %v", idpWritten, writtenAt)
	}

	if err := migrate.RunPhase11SecretRefColumns(ctx, db); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

func TestPhase11SecretRefColumns_EmptySecretRefNameIsBackfilled(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	addLeftoverSMAPIColumns(t, db)

	if err := db.Exec(`
		INSERT INTO org_anthropic_credentials
		  (oc_org_id, role, key_prefix, key_last4, status, connected_at,
		   secret_ref_name, sm_api_secret_ref_name, sm_api_kv_path, sm_api_property)
		VALUES
		  ('org-empty', 'default', 'sk-ant-api03-', 'abcd', 'active', now(),
		   '', 'from-leftover', 'user-app-secrets/ns/from-leftover', 'api-key')
	`).Error; err != nil {
		t.Fatalf("seed blank secret_ref_name: %v", err)
	}

	if err := migrate.RunPhase11SecretRefColumns(ctx, db); err != nil {
		t.Fatalf("phase11: %v", err)
	}

	var name, path, prop string
	if err := db.Raw(`
		SELECT secret_ref_name, secret_ref_kv_path, secret_ref_property
		  FROM org_anthropic_credentials WHERE oc_org_id='org-empty'
	`).Row().Scan(&name, &path, &prop); err != nil {
		t.Fatalf("read: %v", err)
	}
	if name != "from-leftover" || path != "user-app-secrets/ns/from-leftover" || prop != "api-key" {
		t.Fatalf("blank secret_ref_name must be backfilled, got (%q,%q,%q)", name, path, prop)
	}
}

func TestPhase11SecretRefColumns_DoesNotOverwritePopulatedSecretRef(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	addLeftoverSMAPIColumns(t, db)

	if err := db.Exec(`
		INSERT INTO org_anthropic_credentials
		  (oc_org_id, role, key_prefix, key_last4, status, connected_at,
		   secret_ref_name, secret_ref_kv_path, secret_ref_property,
		   sm_api_secret_ref_name, sm_api_kv_path, sm_api_property)
		VALUES
		  ('org-keep', 'default', 'sk-ant-api03-', 'keep', 'active', now(),
		   'keep-me', 'keep/path', 'keep-prop',
		   'drop-me', 'drop/path', 'drop-prop')
	`).Error; err != nil {
		t.Fatalf("seed both-set row: %v", err)
	}

	if err := migrate.RunPhase11SecretRefColumns(ctx, db); err != nil {
		t.Fatalf("phase11: %v", err)
	}

	var name, path, prop string
	if err := db.Raw(`
		SELECT secret_ref_name, secret_ref_kv_path, secret_ref_property
		  FROM org_anthropic_credentials WHERE oc_org_id='org-keep'
	`).Row().Scan(&name, &path, &prop); err != nil {
		t.Fatalf("read: %v", err)
	}
	if name != "keep-me" || path != "keep/path" || prop != "keep-prop" {
		t.Fatalf("populated secret_ref_* must win, got (%q,%q,%q)", name, path, prop)
	}
}
