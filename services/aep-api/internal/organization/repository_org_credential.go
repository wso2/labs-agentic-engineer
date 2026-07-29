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

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// BoundInstallation is the (installation_id, oc_org_id) projection the
// discover-then-bind path reads to filter out installs bound to OTHER orgs.
type BoundInstallation struct {
	InstallationID int64
	OcOrgID        string
}

// OrgCredentialRepository persists the per-org GitHub credential record
// (`org_credentials`, one row per OC org). Every accessor is keyed by the
// org handle (`oc_org_id`) or the bound `installation_id` — never a
// broadenable predicate — so a dropped filter is a missing method, not a
// cross-org write.
//
// The advisory-lock-guarded connect/replace/disconnect/rotation flows run
// inside Tx: it begins the transaction, holds a Postgres advisory xact lock
// across GitHub validation + credential-store writes + the row write, and
// commits (fn returns nil) or rolls back (fn returns an error). Post-commit
// external work (SM-API mirror, projection re-fetch) runs after Tx returns.
type OrgCredentialRepository interface {
	// GetByOrg returns the row for ocOrgID, or nil when absent (not an
	// error) — callers distinguish "no row yet" from a failure.
	GetByOrg(ctx context.Context, ocOrgID string) (*OrgCredential, error)
	// GetByInstallationID returns the row bound to installationID, or nil
	// when absent (not an error).
	GetByInstallationID(ctx context.Context, installationID int64) (*OrgCredential, error)
	// UpdateColumns writes the given columns onto the row scoped to
	// oc_org_id (a map so empty/nil values are written, not skipped).
	UpdateColumns(ctx context.Context, ocOrgID string, updates map[string]any) error
	// ListActiveRows returns every row in 'active' or 'suspended' status.
	ListActiveRows(ctx context.Context) ([]OrgCredential, error)
	// ListBoundInstallations returns the (installation_id, oc_org_id) pairs
	// for rows that carry an installation_id and are active/suspended.
	ListBoundInstallations(ctx context.Context) ([]BoundInstallation, error)
	// OrgIDByRepoURL resolves the org_id that owns the given GitHub repo
	// full name ("owner/repo") by matching git_repositories.repo_url against
	// the canonical clone URL (with and without a .git suffix), anchored on
	// host+owner+repo. Returns "" when no row matches. The lookup is
	// deliberately anchored — an unanchored LIKE would route a webhook to
	// the wrong org.
	OrgIDByRepoURL(ctx context.Context, fullName string) (string, error)

	// Tx begins a transaction, runs fn, and commits on nil / rolls back on
	// error. fn holds the advisory lock (via OrgCredentialTx.AdvisoryLock)
	// across the reads/writes it performs inside the closure.
	Tx(ctx context.Context, fn func(tx OrgCredentialTx) error) error
}

// OrgCredentialTx is the transaction-scoped surface passed to Tx's closure.
// Its reads/writes run inside the open transaction that holds the advisory
// lock; it deliberately does NOT take a context (the transaction was opened
// with the caller's context).
type OrgCredentialTx interface {
	// AdvisoryLock runs pg_advisory_xact_lock(hashtext(key)) — a
	// transaction-scoped lock released on commit or rollback.
	AdvisoryLock(key string) error
	// GetByOrg returns the row for ocOrgID within the tx, or nil when absent.
	GetByOrg(ocOrgID string) (*OrgCredential, error)
	// GetByInstallationID returns the row bound to installationID within the
	// tx, or nil when absent.
	GetByInstallationID(installationID int64) (*OrgCredential, error)
	// Create inserts a new row within the tx.
	Create(row *OrgCredential) error
	// UpdateColumns writes the given columns scoped to oc_org_id within the tx.
	UpdateColumns(ocOrgID string, updates map[string]any) error
	// UpdateStatusByInstallationID flips status on the row bound to
	// installationID within the tx (a no-op update when no row matches).
	UpdateStatusByInstallationID(installationID int64, status string) error
}

type orgCredentialRepository struct {
	db     *gorm.DB
	cipher *secrets.ColumnCipher
}

// NewOrgCredentialRepository constructs the gorm-backed OrgCredentialRepository.
// cipher may be nil (passthrough) — production always passes the credential
// column cipher so webhook_secrets entries are AES-256-GCM at rest.
func NewOrgCredentialRepository(db *gorm.DB, cipher *secrets.ColumnCipher) OrgCredentialRepository {
	return &orgCredentialRepository{db: db, cipher: cipher}
}

func (r *orgCredentialRepository) openRow(row *OrgCredential) error {
	opened, err := openWebhookSecrets(r.cipher, row.WebhookSecrets)
	if err != nil {
		return err
	}
	row.WebhookSecrets = opened
	return nil
}

func (r *orgCredentialRepository) GetByOrg(ctx context.Context, ocOrgID string) (*OrgCredential, error) {
	var row OrgCredential
	err := r.db.WithContext(ctx).Where("oc_org_id = ?", ocOrgID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.openRow(&row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *orgCredentialRepository) GetByInstallationID(ctx context.Context, installationID int64) (*OrgCredential, error) {
	var row OrgCredential
	err := r.db.WithContext(ctx).Where("installation_id = ?", installationID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.openRow(&row); err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *orgCredentialRepository) UpdateColumns(ctx context.Context, ocOrgID string, updates map[string]any) error {
	if err := sealWebhookUpdates(r.cipher, updates); err != nil {
		return err
	}
	return r.db.WithContext(ctx).
		Model(&OrgCredential{}).
		Where("oc_org_id = ?", ocOrgID).
		Updates(updates).Error
}

func (r *orgCredentialRepository) ListActiveRows(ctx context.Context) ([]OrgCredential, error) {
	var rows []OrgCredential
	err := r.db.WithContext(ctx).
		Where("status IN ?", []string{"active", "suspended"}).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if err := r.openRow(&rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
}

func (r *orgCredentialRepository) ListBoundInstallations(ctx context.Context) ([]BoundInstallation, error) {
	var bound []BoundInstallation
	if err := r.db.WithContext(ctx).
		Model(&OrgCredential{}).
		Where("installation_id IS NOT NULL AND status IN ?", []string{"active", "suspended"}).
		Select("installation_id, oc_org_id").
		Find(&bound).Error; err != nil {
		return nil, err
	}
	return bound, nil
}

func (r *orgCredentialRepository) OrgIDByRepoURL(ctx context.Context, fullName string) (string, error) {
	// Match the canonical clone URL EXACTLY, not with an unanchored
	// `LIKE '%/owner/repo'`. git_repositories stores the canonical
	// `https://github.com/<owner>/<repo>` (optionally `.git`); match both
	// exact shapes, anchored on host+owner+repo. No `LIKE`, no leading wildcard.
	var row struct {
		OrgID string `gorm:"column:org_id"`
	}
	canonical := "https://github.com/" + fullName
	err := r.db.WithContext(ctx).
		Table("git_repositories").
		Select("org_id").
		Where("repo_url = ? OR repo_url = ?", canonical, canonical+".git").
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return "", err
	}
	return row.OrgID, nil
}

func (r *orgCredentialRepository) Tx(ctx context.Context, fn func(tx OrgCredentialTx) error) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed
	if err := fn(&orgCredentialTx{tx: tx, cipher: r.cipher}); err != nil {
		return err
	}
	return tx.Commit().Error
}

type orgCredentialTx struct {
	tx     *gorm.DB
	cipher *secrets.ColumnCipher
}

func (t *orgCredentialTx) AdvisoryLock(key string) error {
	return t.tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, key).Error
}

func (t *orgCredentialTx) GetByOrg(ocOrgID string) (*OrgCredential, error) {
	var row OrgCredential
	err := t.tx.Where("oc_org_id = ?", ocOrgID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	opened, err := openWebhookSecrets(t.cipher, row.WebhookSecrets)
	if err != nil {
		return nil, err
	}
	row.WebhookSecrets = opened
	return &row, nil
}

func (t *orgCredentialTx) GetByInstallationID(installationID int64) (*OrgCredential, error) {
	var row OrgCredential
	err := t.tx.Where("installation_id = ?", installationID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	opened, err := openWebhookSecrets(t.cipher, row.WebhookSecrets)
	if err != nil {
		return nil, err
	}
	row.WebhookSecrets = opened
	return &row, nil
}

func (t *orgCredentialTx) Create(row *OrgCredential) error {
	if row == nil {
		return t.tx.Create(row).Error
	}
	sealed, err := sealWebhookSecrets(t.cipher, row.WebhookSecrets)
	if err != nil {
		return err
	}
	cp := *row
	cp.WebhookSecrets = sealed
	return t.tx.Create(&cp).Error
}

func (t *orgCredentialTx) UpdateColumns(ocOrgID string, updates map[string]any) error {
	if err := sealWebhookUpdates(t.cipher, updates); err != nil {
		return err
	}
	return t.tx.
		Model(&OrgCredential{}).
		Where("oc_org_id = ?", ocOrgID).
		Updates(updates).Error
}

func (t *orgCredentialTx) UpdateStatusByInstallationID(installationID int64, status string) error {
	return t.tx.
		Model(&OrgCredential{}).
		Where("installation_id = ?", installationID).
		Update("status", status).Error
}
