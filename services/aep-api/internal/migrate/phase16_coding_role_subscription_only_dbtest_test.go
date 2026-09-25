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
	"strings"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

func constraintExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM pg_constraint
		WHERE conrelid = 'org_anthropic_credentials'::regclass AND conname = ?`, name).Scan(&n).Error; err != nil {
		t.Fatalf("read constraint %s: %v", name, err)
	}
	return n > 0
}

func countWhere(t *testing.T, db *gorm.DB, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Raw(sql, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// dbtest.New has applied phase16 through the production migrate.RunAll: the
// new CHECK is in place and the one it replaced is gone.
func TestPhase16_InstallsTheRoleKindCheck(t *testing.T) {
	db := dbtest.New(t)
	if !constraintExists(t, db, "org_anthropic_credentials_role_kind") {
		t.Fatal("org_anthropic_credentials_role_kind is missing")
	}
	if constraintExists(t, db, "org_anthropic_credentials_oauth_is_coding_only") {
		t.Fatal("the CHECK phase16 replaces is still installed")
	}
}

// The data half, on a reconstructed pre-phase16 state: separate coding API keys
// and their bytes go, subscriptions and default keys stay, and a re-run is a
// no-op — as is phase13 running before it on the next boot.
func TestPhase16_DeletesSeparateCodingKeysAndTheirBytes(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	// Back to the phase13 schema, which admitted a coding api_key.
	if err := db.Exec(`ALTER TABLE org_anthropic_credentials DROP CONSTRAINT org_anthropic_credentials_role_kind`).Error; err != nil {
		t.Fatalf("drop new CHECK: %v", err)
	}
	for _, row := range []struct{ org, role, kind string }{
		{"keyed", "default", "api_key"}, {"keyed", "coding", "api_key"}, // a separate coding key
		{"subbed", "default", "api_key"}, {"subbed", "coding", "oauth_token"}, // a subscription
	} {
		if err := db.Exec(`INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
			VALUES (?, ?, ?, 'sk-ant-x', 'wxyz', 'active')`, row.org, row.role, row.kind).Error; err != nil {
			t.Fatalf("seed %+v: %v", row, err)
		}
	}
	for _, sec := range []struct{ org, key string }{
		{"keyed", "anthropic/key"}, {"keyed", "anthropic/coding-key"},
		{"subbed", "anthropic/key"}, {"subbed", "anthropic/coding-key"},
		{"orphan", "anthropic/coding-key"}, // bytes with no row at all
	} {
		if err := db.Exec(`INSERT INTO org_secrets (oc_org_id, key, value) VALUES (?, ?, 'sealed')`, sec.org, sec.key).Error; err != nil {
			t.Fatalf("seed secret %+v: %v", sec, err)
		}
	}

	for i := 0; i < 2; i++ { // the second pass proves idempotence
		if err := migrate.RunPhase13AnthropicCredentialRole(ctx, db); err != nil {
			t.Fatalf("phase13 pass %d: %v", i+1, err)
		}
		if err := migrate.RunPhase16CodingRoleSubscriptionOnly(ctx, db); err != nil {
			t.Fatalf("phase16 pass %d: %v", i+1, err)
		}
	}

	if n := countWhere(t, db, `SELECT count(*) FROM org_anthropic_credentials WHERE role = 'coding' AND credential_kind = 'api_key'`); n != 0 {
		t.Fatalf("%d separate coding key row(s) survived", n)
	}
	if n := countWhere(t, db, `SELECT count(*) FROM org_secrets WHERE key = 'anthropic/coding-key' AND oc_org_id IN ('keyed','orphan')`); n != 0 {
		t.Fatalf("%d unreachable coding-key byte row(s) survived", n)
	}
	// Kept: both default keys, the subscription and its bytes.
	if n := countWhere(t, db, `SELECT count(*) FROM org_anthropic_credentials`); n != 3 {
		t.Fatalf("want 3 rows left (two keys, one subscription), got %d", n)
	}
	if n := countWhere(t, db, `SELECT count(*) FROM org_secrets WHERE oc_org_id = 'subbed'`); n != 2 {
		t.Fatalf("the subscription org lost bytes: %d left", n)
	}
	if n := countWhere(t, db, `SELECT count(*) FROM org_secrets WHERE oc_org_id = 'keyed' AND key = 'anthropic/key'`); n != 1 {
		t.Fatal("the keyed org lost its default key's bytes")
	}
	if constraintExists(t, db, "org_anthropic_credentials_oauth_is_coding_only") {
		t.Fatal("phase13 re-added the CHECK phase16 replaced")
	}

	err := db.Exec(`INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
		VALUES ('late', 'coding', 'api_key', 'sk-ant-x', 'wxyz', 'active')`).Error
	if err == nil || !strings.Contains(err.Error(), "role_kind") {
		t.Fatalf("a coding api_key must be refused by the CHECK, got %v", err)
	}
}
