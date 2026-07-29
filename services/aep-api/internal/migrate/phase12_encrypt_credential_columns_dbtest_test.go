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
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

func TestPhase12EncryptCredentialColumns_InPlace(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	key := make([]byte, 32)
	cipher, err := secrets.NewColumnCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	// Seed plaintext publisher secret (bypass repository seal).
	profileID := uuid.New().String()
	if err := db.Exec(`
		INSERT INTO organization_idp_profiles
		  (id, org_id, kind, issuer, jwks_url, publisher_client_secret, created_at, updated_at)
		VALUES (?, 'acme', 'platform', 'https://issuer', 'https://jwks', 'plain-publisher-secret', now(), now())`,
		profileID).Error; err != nil {
		t.Fatalf("seed idp: %v", err)
	}

	// Seed plaintext webhook_secrets on a user-pat row (raw SQL so secrets stay plaintext).
	wh := organization.WebhookSecrets{{Secret: "whsec-plain", AddedAt: time.Now().UTC()}}
	raw, _ := json.Marshal(wh)
	if err := db.Exec(`
		INSERT INTO org_credentials
		  (oc_org_id, kind, status, connected_at, github_login,
		   identity_name, identity_email, identity_login, webhook_secrets)
		VALUES ('acme', 'user-pat', 'active', now(), 'ada',
		        'Ada', 'ada@x.io', 'ada', ?::jsonb)`, string(raw)).Error; err != nil {
		t.Fatalf("seed org_credentials: %v", err)
	}

	if err := migrate.RunPhase12EncryptCredentialColumns(ctx, db, key); err != nil {
		t.Fatalf("phase12: %v", err)
	}

	var storedPub string
	if err := db.Raw(`SELECT publisher_client_secret FROM organization_idp_profiles WHERE id = ?`, profileID).
		Scan(&storedPub).Error; err != nil {
		t.Fatalf("read publisher: %v", err)
	}
	if storedPub == "plain-publisher-secret" {
		t.Fatal("publisher_client_secret still plaintext after migration")
	}
	if !cipher.IsSealed(storedPub) {
		t.Fatalf("publisher_client_secret not sealed: %q", storedPub)
	}
	opened, err := cipher.Open(storedPub)
	if err != nil || string(opened) != "plain-publisher-secret" {
		t.Fatalf("open publisher: %q err=%v", opened, err)
	}

	var storedWH string
	if err := db.Raw(`SELECT webhook_secrets::text FROM org_credentials WHERE oc_org_id = 'acme'`).
		Scan(&storedWH).Error; err != nil {
		t.Fatalf("read webhook: %v", err)
	}
	var entries organization.WebhookSecrets
	if err := json.Unmarshal([]byte(storedWH), &entries); err != nil {
		t.Fatalf("parse webhook: %v", err)
	}
	if len(entries) != 1 || entries[0].Secret == "whsec-plain" {
		t.Fatalf("webhook_secrets not sealed: %+v", entries)
	}
	if !cipher.IsSealed(entries[0].Secret) {
		t.Fatalf("webhook entry not sealed: %q", entries[0].Secret)
	}

	// Idempotent re-run.
	if err := migrate.RunPhase12EncryptCredentialColumns(ctx, db, key); err != nil {
		t.Fatalf("phase12 re-run: %v", err)
	}
}

func TestPhase12_RepositoryRoundTrip(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	key := make([]byte, 32)
	cipher, err := secrets.NewColumnCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	idpRepo := organization.NewIDPRepository(db, cipher)
	profile := &organization.OrganizationIDPProfile{
		ID:                    uuid.New().String(),
		OrgID:                 "roundtrip",
		Kind:                  "platform",
		Issuer:                "https://issuer",
		JWKSURL:               "https://jwks",
		PublisherClientSecret: "live-secret",
	}
	if err := idpRepo.CreateProfile(ctx, profile); err != nil {
		t.Fatalf("create: %v", err)
	}

	var raw string
	if err := db.Raw(`SELECT publisher_client_secret FROM organization_idp_profiles WHERE org_id = 'roundtrip'`).
		Scan(&raw).Error; err != nil {
		t.Fatalf("raw: %v", err)
	}
	if raw == "live-secret" || !cipher.IsSealed(raw) {
		t.Fatalf("expected sealed at rest, got %q", raw)
	}

	got, err := idpRepo.GetProfileByOrgID(ctx, "roundtrip")
	if err != nil || got == nil {
		t.Fatalf("get: %v %+v", err, got)
	}
	if got.PublisherClientSecret != "live-secret" {
		t.Fatalf("readback = %q", got.PublisherClientSecret)
	}
}
