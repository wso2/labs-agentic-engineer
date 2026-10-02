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
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrOrgSecretConflict is returned by a conditional write of an org secret's
// row when the row no longer names the reference the caller read: another
// write got there first.
var ErrOrgSecretConflict = errors.New("org secret row changed concurrently")

// OrgSecretRepository reads and writes the org secrets' reference rows in
// org_secrets: key = the OrgSecret string, value NULL, secret_ref_name = the
// SecretReference holding the value. A row means "set". The legacy value rows
// (keys such as github/pat) share the table but never these keys, so no
// accessor here reads or writes one. Every accessor is keyed by oc_org_id.
//
// Writes are compare-and-swap on secret_ref_name, so two writers that read
// the same row cannot both replace it: the loser gets ErrOrgSecretConflict.
type OrgSecretRepository interface {
	// Get returns the reference row of s, or nil when s is unset.
	Get(ctx context.Context, ocOrgID string, s OrgSecret) (*OrgSecretRef, error)
	// List returns the org's set secrets, ordered by secret.
	List(ctx context.Context, ocOrgID string) ([]OrgSecretRef, error)
	// Upsert writes the row of r.Secret if it still names expectPrev: with
	// expectPrev empty it inserts only when there is no row; otherwise it
	// updates only the row naming expectPrev. Anything else is
	// ErrOrgSecretConflict and writes nothing.
	Upsert(ctx context.Context, ocOrgID string, r OrgSecretRef, expectPrev string) error
	// Delete removes the row of s if it names name; a row naming another
	// reference, or none, is ErrOrgSecretConflict.
	Delete(ctx context.Context, ocOrgID string, s OrgSecret, name string) error
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
	WrittenAt     *time.Time
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

//deadcode:keep wired by Task 1.15 (the converger reads the set secrets)
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
func (r *orgSecretRepository) Upsert(ctx context.Context, ocOrgID string, ref OrgSecretRef, expectPrev string) error {
	if ref.Name == "" {
		return fmt.Errorf("org secret %s: reference name is required", ref.Secret)
	}
	var res *gorm.DB
	if expectPrev == "" {
		res = r.db.WithContext(ctx).Exec(`
			INSERT INTO org_secrets (oc_org_id, key, value, secret_ref_name, written_at)
			VALUES (?, ?, NULL, ?, ?)
			ON CONFLICT (oc_org_id, key) DO NOTHING`,
			ocOrgID, string(ref.Secret), ref.Name, ref.WrittenAt)
	} else {
		res = r.db.WithContext(ctx).Exec(`
			UPDATE org_secrets
			   SET secret_ref_name = ?, written_at = ?, updated_at = now()
			 WHERE oc_org_id = ? AND key = ? AND secret_ref_name = ?`,
			ref.Name, ref.WrittenAt, ocOrgID, string(ref.Secret), expectPrev)
	}
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrOrgSecretConflict
	}
	return nil
}

//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (r *orgSecretRepository) Delete(ctx context.Context, ocOrgID string, s OrgSecret, name string) error {
	if name == "" {
		return fmt.Errorf("org secret %s: reference name is required", s)
	}
	res := r.db.WithContext(ctx).Exec(
		`DELETE FROM org_secrets WHERE oc_org_id = ? AND key = ? AND secret_ref_name IS NOT NULL AND secret_ref_name = ?`,
		ocOrgID, string(s), name)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrOrgSecretConflict
	}
	return nil
}

// OrgSecretLock serializes the writes of one org secret, so a write's whole
// sequence (read row → new reference → row → repoint → retire) never
// interleaves with another write or removal of the same (org, secret).
type OrgSecretLock interface {
	// Lock blocks until the lock of (ocOrgID, s) is held or ctx is done, and
	// returns the release. The release is safe to call once, from a defer.
	Lock(ctx context.Context, ocOrgID string, s OrgSecret) (unlock func(), err error)
}

type orgSecretLock struct {
	db *gorm.DB
}

// NewOrgSecretLock returns the Postgres advisory-lock OrgSecretLock.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func NewOrgSecretLock(db *gorm.DB) OrgSecretLock {
	return &orgSecretLock{db: db}
}

// orgSecretLockKey names the advisory lock of one org secret. A hash
// collision with another key only serializes two unrelated writes.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func orgSecretLockKey(ocOrgID string, s OrgSecret) string {
	return "org_secret:" + ocOrgID + "/" + string(s)
}

// Lock holds pg_advisory_xact_lock in a transaction pinned to one pooled
// connection; releasing rolls the transaction back, which drops the lock
// (as does the server if the connection dies). The transaction ignores ctx
// cancellation once the lock is held, so a cancelled request cannot drop
// the lock while its write is still running; only the wait is bounded by
// ctx.
//
//deadcode:keep wired by Task 1.13 (the gitpat submit writes the org secrets through OrgSecretWriter)
func (l *orgSecretLock) Lock(ctx context.Context, ocOrgID string, s OrgSecret) (func(), error) {
	tx := l.db.WithContext(context.WithoutCancel(ctx)).Begin()
	if tx.Error != nil {
		return nil, fmt.Errorf("org secret %s: lock: %w", s, tx.Error)
	}
	if err := tx.WithContext(ctx).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, orgSecretLockKey(ocOrgID, s)).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("org secret %s: lock: %w", s, err)
	}
	return func() { tx.Rollback() }, nil
}
