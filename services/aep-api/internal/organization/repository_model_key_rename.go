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

package organization

import (
	"context"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// ModelKeyRenameRepository is the rename's storage (model_key_rename.go): the
// orgs still holding the connection key under its Anthropic-era `org_secrets`
// key, one locked transaction per org that copies the bytes and switches the
// row's secret reference, and the delete that retires the old bytes.
type ModelKeyRenameRepository interface {
	// OrgsWithLegacyKey lists the orgs that still hold an `anthropic/key` row.
	OrgsWithLegacyKey(ctx context.Context) ([]string, error)
	// Tx runs fn in one transaction, committing when it returns nil.
	Tx(ctx context.Context, fn func(tx ModelKeyRenameTx) error) error
	// DeleteLegacyKey deletes the org's `anthropic/key` row when nothing still
	// needs it: its bytes are already under `model/key` at least as new, or the
	// org has no connection. A row written after the copy (by a replica of the
	// previous release, mid-rollout) is kept for the next pass to copy.
	DeleteLegacyKey(ctx context.Context, ocOrgID string) error
}

// ModelKeyRenameTx is the transaction-scoped surface of one org's move.
type ModelKeyRenameTx interface {
	// Lock takes the AI agents card's per-org advisory locks (lockCard), so
	// the move and a save of the card serialize.
	Lock(ocOrgID string) error
	// CopyLegacyKey copies the org's `anthropic/key` bytes to `model/key`,
	// unless `model/key` is newer (a save since) or the org has no connection
	// (a disconnect since); the sealed value is copied as is, with its
	// updated_at.
	CopyLegacyKey(ocOrgID string) error
	// Connection reads the org's connection row, nil when it has none.
	Connection(ocOrgID string) (*OrgModelConnection, error)
	// Key reads the connection key's bytes.
	Key(ctx context.Context, ocOrgID string) ([]byte, error)
	// SwitchSecretRef points the row's secret-ref columns at ref, only while
	// they still name from (a compare-and-swap).
	SwitchSecretRef(ocOrgID, from string, ref SecretRefTriplet) error
}

type modelKeyRenameRepository struct {
	db    *gorm.DB
	store secrets.TxCredentialStore
}

// NewModelKeyRenameRepository constructs the gorm-backed repository. store
// must be the encrypted store the connection's readers use.
func NewModelKeyRenameRepository(db *gorm.DB, store secrets.TxCredentialStore) ModelKeyRenameRepository {
	return &modelKeyRenameRepository{db: db, store: store}
}

func (r *modelKeyRenameRepository) OrgsWithLegacyKey(ctx context.Context) ([]string, error) {
	var orgs []string
	err := r.db.WithContext(ctx).Raw(
		`SELECT oc_org_id FROM org_secrets WHERE key = ? ORDER BY oc_org_id`, legacyModelKeyStoreKey,
	).Scan(&orgs).Error
	return orgs, err
}

func (r *modelKeyRenameRepository) Tx(ctx context.Context, fn func(tx ModelKeyRenameTx) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&modelKeyRenameTx{tx: tx, store: r.store.WithDB(tx)})
	})
}

func (r *modelKeyRenameRepository) DeleteLegacyKey(ctx context.Context, ocOrgID string) error {
	return r.db.WithContext(ctx).Exec(`
		DELETE FROM org_secrets o
		 WHERE o.oc_org_id = ? AND o.key = ?
		   AND (EXISTS (SELECT 1 FROM org_secrets n
		                 WHERE n.oc_org_id = o.oc_org_id AND n.key = ? AND n.updated_at >= o.updated_at)
		        OR NOT EXISTS (SELECT 1 FROM org_model_connections c WHERE c.oc_org_id = o.oc_org_id))`,
		ocOrgID, legacyModelKeyStoreKey, modelKeyStoreKey).Error
}

type modelKeyRenameTx struct {
	tx    *gorm.DB
	store secrets.CredentialStore
}

func (t *modelKeyRenameTx) Lock(ocOrgID string) error {
	return lockCard(func(key string) error {
		return t.tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, key).Error
	}, ocOrgID)
}

// CopyLegacyKey is the per-org form of migrate's phase20_model_key_rename
// copy; the two must agree on "newer" and on copying only a connected org.
func (t *modelKeyRenameTx) CopyLegacyKey(ocOrgID string) error {
	return t.tx.Exec(`
		INSERT INTO org_secrets (oc_org_id, key, value, updated_at)
		SELECT s.oc_org_id, ?, s.value, s.updated_at FROM org_secrets s
		 WHERE s.oc_org_id = ? AND s.key = ?
		   AND EXISTS (SELECT 1 FROM org_model_connections c WHERE c.oc_org_id = s.oc_org_id)
		ON CONFLICT (oc_org_id, key) DO UPDATE
		  SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
		  WHERE org_secrets.updated_at < EXCLUDED.updated_at`,
		modelKeyStoreKey, ocOrgID, legacyModelKeyStoreKey).Error
}

func (t *modelKeyRenameTx) Connection(ocOrgID string) (*OrgModelConnection, error) {
	return getModelConnection(t.tx, ocOrgID)
}

func (t *modelKeyRenameTx) Key(ctx context.Context, ocOrgID string) ([]byte, error) {
	return t.store.Get(ctx, ocOrgID, modelKeyStoreKey)
}

func (t *modelKeyRenameTx) SwitchSecretRef(ocOrgID, from string, ref SecretRefTriplet) error {
	return t.tx.Model(&OrgModelConnection{}).
		Where("oc_org_id = ? AND secret_ref_name = ?", ocOrgID, from).
		Updates(stampSecretRefTriplet(ref.Name, ref.KVPath, ref.Property)).Error
}
