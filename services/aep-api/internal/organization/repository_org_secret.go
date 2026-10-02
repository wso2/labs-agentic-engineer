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
	"errors"
	"time"

	"gorm.io/gorm"
)

// OrgSecretRepository reads and writes the org secrets' reference rows in
// org_secrets: key = the OrgSecret string, value NULL, secret_ref_name = the
// SecretReference holding the value. A row means "set". The legacy value rows
// (keys such as github/pat) share the table but never these keys, so no
// accessor here reads or writes one. Every accessor is keyed by oc_org_id.
type OrgSecretRepository interface {
	// Get returns the reference row of s, or nil when s is unset.
	Get(ctx context.Context, ocOrgID string, s OrgSecret) (*OrgSecretRef, error)
	// List returns the org's set secrets, ordered by secret.
	List(ctx context.Context, ocOrgID string) ([]OrgSecretRef, error)
	// Upsert inserts or replaces the reference row of r.Secret.
	Upsert(ctx context.Context, ocOrgID string, r OrgSecretRef) error
	// Delete removes the reference row of s; an absent row is not an error.
	Delete(ctx context.Context, ocOrgID string, s OrgSecret) error
}

type orgSecretRepository struct {
	db *gorm.DB
}

// NewOrgSecretRepository constructs the gorm-backed OrgSecretRepository.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func NewOrgSecretRepository(db *gorm.DB) OrgSecretRepository {
	return &orgSecretRepository{db: db}
}

// orgSecretRefRow is the projection of an org_secrets reference row.
type orgSecretRefRow struct {
	Key           string
	SecretRefName string
	WrittenAt     time.Time
}

//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (r orgSecretRefRow) ref() OrgSecretRef {
	return OrgSecretRef{Secret: OrgSecret(r.Key), Name: r.SecretRefName, WrittenAt: r.WrittenAt}
}

//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (r *orgSecretRepository) Get(ctx context.Context, ocOrgID string, s OrgSecret) (*OrgSecretRef, error) {
	var row orgSecretRefRow
	err := r.db.WithContext(ctx).Table("org_secrets").
		Select("key, secret_ref_name, written_at").
		Where("oc_org_id = ? AND key = ? AND secret_ref_name IS NOT NULL", ocOrgID, string(s)).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ref := row.ref()
	return &ref, nil
}

//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (r *orgSecretRepository) List(ctx context.Context, ocOrgID string) ([]OrgSecretRef, error) {
	var rows []orgSecretRefRow
	if err := r.db.WithContext(ctx).Table("org_secrets").
		Select("key, secret_ref_name, written_at").
		Where("oc_org_id = ? AND key IN ? AND secret_ref_name IS NOT NULL", ocOrgID, OrgSecrets()).
		Order("key").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	refs := make([]OrgSecretRef, 0, len(rows))
	for _, row := range rows {
		refs = append(refs, row.ref())
	}
	return refs, nil
}

//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (r *orgSecretRepository) Upsert(ctx context.Context, ocOrgID string, ref OrgSecretRef) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO org_secrets (oc_org_id, key, value, secret_ref_name, written_at)
		VALUES (?, ?, NULL, ?, ?)
		ON CONFLICT (oc_org_id, key) DO UPDATE
		  SET secret_ref_name = EXCLUDED.secret_ref_name,
		      written_at = EXCLUDED.written_at,
		      updated_at = now()`,
		ocOrgID, string(ref.Secret), ref.Name, ref.WrittenAt).Error
}

//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (r *orgSecretRepository) Delete(ctx context.Context, ocOrgID string, s OrgSecret) error {
	return r.db.WithContext(ctx).Exec(
		`DELETE FROM org_secrets WHERE oc_org_id = ? AND key = ?`, ocOrgID, string(s)).Error
}
