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

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// secretRows is org_secrets as (org/key → sealed value, xmin), for comparing
// bytes and rewrites.
func secretRows(t *testing.T, db *gorm.DB) (values, versions map[string]string) {
	t.Helper()
	var rows []struct{ OcOrgID, Key, Value, Xmin string }
	if err := db.Raw(`SELECT oc_org_id, key, value, xmin::text AS xmin FROM org_secrets`).Scan(&rows).Error; err != nil {
		t.Fatalf("read org_secrets: %v", err)
	}
	values, versions = map[string]string{}, map[string]string{}
	for _, r := range rows {
		values[r.OcOrgID+"/"+r.Key] = r.Value
		versions[r.OcOrgID+"/"+r.Key] = r.Xmin
	}
	return values, versions
}

// seedConnection gives org a connection row: only a connected org's key is
// copied.
func seedConnection(t *testing.T, db *gorm.DB, org string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO org_model_connections
		(oc_org_id, format, base_url, host, model, auth_scheme, key_preview, connected_at, updated_at)
		VALUES (?, 'anthropic', 'https://api.anthropic.com/v1', 'api.anthropic.com', 'claude-sonnet-5', 'x-api-key', '…0001', now(), now())`,
		org).Error; err != nil {
		t.Fatalf("seed connection %s: %v", org, err)
	}
}

// The boot path on a database holding keys under the Anthropic-era name: every
// connected org's key is readable under model/key with identical bytes, the
// old rows are left for the SM-API half to retire, and a second run rewrites
// nothing. Bytes with no connection row (a disconnected key) are not copied.
func TestPhase20ModelKeyRename_CopiesEveryKeyAndReRunsAsANoOp(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	bootMigrate(t, db)
	store := legacySecrets{db: db}
	keys := map[string]string{"acme": "sk-ant-api03-acme-key-bytes-0001", "globex": "sk-ant-api03-globex-key-bytes-0002"}
	for org, key := range keys {
		seedConnection(t, db, org)
		if err := store.Put(ctx, org, "anthropic/key", []byte(key)); err != nil {
			t.Fatalf("seed %s: %v", org, err)
		}
	}
	if err := store.Put(ctx, "initech", "anthropic/key", []byte("sk-ant-api03-disconnected-key-0003")); err != nil {
		t.Fatalf("seed disconnected: %v", err)
	}
	// Another secret of the org is not touched.
	if err := store.Put(ctx, "acme", "github/pat", []byte("ghp-token")); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if err := migrate.RunPhase20ModelKeyRename(ctx, db); err != nil {
		t.Fatalf("phase20: %v", err)
	}
	values, _ := secretRows(t, db)
	for org, key := range keys {
		if values[org+"/model/key"] != values[org+"/anthropic/key"] {
			t.Fatalf("%s: model/key is not the same sealed value as anthropic/key", org)
		}
		if got, err := store.Get(ctx, org, "model/key"); err != nil || string(got) != key {
			t.Fatalf("%s model/key = %q (%v), want %q", org, got, err, key)
		}
	}
	if n := count(t, db, `SELECT count(*) FROM org_secrets WHERE key = 'anthropic/key'`); n != 3 {
		t.Fatalf("anthropic/key rows = %d, want all kept for the SM-API half", n)
	}
	if _, ok := values["initech/model/key"]; ok {
		t.Fatal("a key with no connection row was copied to model/key")
	}
	if n := count(t, db, `SELECT count(*) FROM org_secrets WHERE key <> 'anthropic/key' AND key <> 'model/key'`); n != 1 {
		t.Fatalf("other secrets = %d, want the one untouched", n)
	}

	_, before := secretRows(t, db)
	bootMigrate(t, db)
	if _, after := secretRows(t, db); len(after) != len(before) {
		t.Fatalf("a re-run changed org_secrets: %v -> %v", before, after)
	} else {
		for k, v := range before {
			if after[k] != v {
				t.Fatalf("a re-run rewrote %s", k)
			}
		}
	}
}

// "Newer wins": a save under model/key since the copy is kept, and a key a
// replica of the previous release saved under the old name since is copied.
func TestPhase20ModelKeyRename_TheNewerKeyWins(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	bootMigrate(t, db)
	store := legacySecrets{db: db}
	put := func(org, key, value string) {
		t.Helper()
		if err := store.Put(ctx, org, key, []byte(value)); err != nil {
			t.Fatalf("put %s %s: %v", org, key, err)
		}
	}
	age := func(org, key string) {
		t.Helper()
		if err := db.Exec(`UPDATE org_secrets SET updated_at = updated_at - interval '1 hour'
		                    WHERE oc_org_id = ? AND key = ?`, org, key).Error; err != nil {
			t.Fatalf("age %s %s: %v", org, key, err)
		}
	}
	seedConnection(t, db, "saved")
	seedConnection(t, db, "resaved")
	// saved: the old key, then a save on this release.
	put("saved", "anthropic/key", "old-bytes")
	age("saved", "anthropic/key")
	put("saved", "model/key", "saved-bytes")
	// resaved: the copy, then a save by the previous release under the old name.
	put("resaved", "model/key", "copied-bytes")
	age("resaved", "model/key")
	put("resaved", "anthropic/key", "resaved-bytes")

	if err := migrate.RunPhase20ModelKeyRename(ctx, db); err != nil {
		t.Fatalf("phase20: %v", err)
	}
	for org, want := range map[string]string{"saved": "saved-bytes", "resaved": "resaved-bytes"} {
		if got, err := store.Get(ctx, org, "model/key"); err != nil || string(got) != want {
			t.Fatalf("%s model/key = %q (%v), want %q", org, got, err, want)
		}
	}
}
