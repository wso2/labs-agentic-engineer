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

package migrate

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// RunPhase12EncryptCredentialColumns seals existing plaintext
// organization_idp_profiles.publisher_client_secret and
// org_credentials.webhook_secrets entries with the same AES-256-GCM framing
// as org_secrets (credential-encryption-key). Idempotent: already-sealed
// values are left untouched (detected via successful Open).
//
// Readers tolerate plaintext only via ColumnCipher.OpenTolerant for the
// migration window; new writes always Seal.
func RunPhase12EncryptCredentialColumns(ctx context.Context, db *gorm.DB, credKey []byte) error {
	if len(credKey) == 0 {
		return nil // step-order / dry builders pass nil
	}
	cipher, err := secrets.NewColumnCipher(credKey)
	if err != nil {
		return fmt.Errorf("phase12_encrypt_credential_columns: cipher: %w", err)
	}
	if err := encryptPublisherClientSecrets(ctx, db, cipher); err != nil {
		return err
	}
	return encryptWebhookSecrets(ctx, db, cipher)
}

func encryptPublisherClientSecrets(ctx context.Context, db *gorm.DB, cipher *secrets.ColumnCipher) error {
	if !tableExists(db, "organization_idp_profiles") {
		return nil
	}
	type row struct {
		ID     string
		Secret string `gorm:"column:publisher_client_secret"`
	}
	var rows []row
	if err := db.WithContext(ctx).
		Table("organization_idp_profiles").
		Select("id, publisher_client_secret").
		Where("publisher_client_secret IS NOT NULL AND publisher_client_secret <> ''").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("phase12_encrypt_credential_columns: list publisher secrets: %w", err)
	}
	for _, r := range rows {
		if cipher.IsSealed(r.Secret) {
			continue
		}
		sealed, err := cipher.Seal([]byte(r.Secret))
		if err != nil {
			return fmt.Errorf("phase12_encrypt_credential_columns: seal publisher %s: %w", r.ID, err)
		}
		if err := db.WithContext(ctx).Exec(
			`UPDATE organization_idp_profiles SET publisher_client_secret = ? WHERE id = ?`,
			sealed, r.ID,
		).Error; err != nil {
			return fmt.Errorf("phase12_encrypt_credential_columns: update publisher %s: %w", r.ID, err)
		}
	}
	return nil
}

func encryptWebhookSecrets(ctx context.Context, db *gorm.DB, cipher *secrets.ColumnCipher) error {
	if !tableExists(db, "org_credentials") {
		return nil
	}
	type row struct {
		OcOrgID string          `gorm:"column:oc_org_id"`
		Raw     json.RawMessage `gorm:"column:webhook_secrets"`
	}
	var rows []row
	if err := db.WithContext(ctx).
		Table("org_credentials").
		Select("oc_org_id, webhook_secrets").
		Where("webhook_secrets IS NOT NULL").
		Find(&rows).Error; err != nil {
		return fmt.Errorf("phase12_encrypt_credential_columns: list webhook_secrets: %w", err)
	}
	for _, r := range rows {
		if len(r.Raw) == 0 || string(r.Raw) == "null" {
			continue
		}
		var entries organization.WebhookSecrets
		if err := json.Unmarshal(r.Raw, &entries); err != nil {
			return fmt.Errorf("phase12_encrypt_credential_columns: parse webhook_secrets %s: %w", r.OcOrgID, err)
		}
		changed := false
		for i := range entries {
			if entries[i].Secret == "" || cipher.IsSealed(entries[i].Secret) {
				continue
			}
			sealed, err := cipher.Seal([]byte(entries[i].Secret))
			if err != nil {
				return fmt.Errorf("phase12_encrypt_credential_columns: seal webhook %s[%d]: %w", r.OcOrgID, i, err)
			}
			entries[i].Secret = sealed
			changed = true
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(entries)
		if err != nil {
			return fmt.Errorf("phase12_encrypt_credential_columns: marshal webhook_secrets %s: %w", r.OcOrgID, err)
		}
		if err := db.WithContext(ctx).Exec(
			`UPDATE org_credentials SET webhook_secrets = ?::jsonb WHERE oc_org_id = ?`,
			string(encoded), r.OcOrgID,
		).Error; err != nil {
			return fmt.Errorf("phase12_encrypt_credential_columns: update webhook_secrets %s: %w", r.OcOrgID, err)
		}
	}
	return nil
}
