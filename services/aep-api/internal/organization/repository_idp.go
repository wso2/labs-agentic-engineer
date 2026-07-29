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

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// IDPRepository persists the idp feature's two tables: the per-org
// `organization_idp_profiles` row (one per org — kind/issuer/JWKS plus the
// Thunder publisher triplet) and the append-only `idp_audit_events` trail.
// Every profile accessor is keyed by org_id — the identity the idp domain owns
// — never a broadenable predicate, so a dropped filter is a missing method, not
// a cross-org write. The organizations read the publisher-provisioning path
// needs (org → Thunder OU) is NOT here: it stays on OrganizationRepository so
// that query has exactly one owner.
type IDPRepository interface {
	// GetProfileByOrgID returns the org's IDP profile, or nil when absent
	// (not an error) — the caller distinguishes "no profile yet" from failure.
	GetProfileByOrgID(ctx context.Context, orgID string) (*OrganizationIDPProfile, error)
	// CreateProfile inserts a new profile row, returning the raw driver error
	// so the caller can recover from a create-vs-create race by re-reading.
	CreateProfile(ctx context.Context, profile *OrganizationIDPProfile) error
	// UpdateProfileColumns writes the given columns onto the loaded profile
	// row, scoped to org_id. profile is the row Model() keys the statement on
	// (its primary key), so the caller passes the exact row it read; updates is
	// the column set the caller decided to write (a map so empty strings are
	// written, not skipped — the field-level "empty clears it" semantics some
	// write paths depend on).
	UpdateProfileColumns(ctx context.Context, profile *OrganizationIDPProfile, orgID string, updates map[string]interface{}) error
	// CreateAuditEvent appends one row to idp_audit_events.
	CreateAuditEvent(ctx context.Context, event *IDPAuditEvent) error
}

type idpRepository struct {
	db     *gorm.DB
	cipher *secrets.ColumnCipher
}

// NewIDPRepository constructs the gorm-backed IDPRepository.
// cipher may be nil (passthrough) — production always passes the credential
// column cipher so publisher_client_secret is AES-256-GCM at rest.
func NewIDPRepository(db *gorm.DB, cipher *secrets.ColumnCipher) IDPRepository {
	return &idpRepository{db: db, cipher: cipher}
}

func (r *idpRepository) GetProfileByOrgID(ctx context.Context, orgID string) (*OrganizationIDPProfile, error) {
	var profile OrganizationIDPProfile
	err := r.db.WithContext(ctx).Where("org_id = ?", orgID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	plain, err := openPublisherSecret(r.cipher, profile.PublisherClientSecret)
	if err != nil {
		return nil, fmt.Errorf("open publisher_client_secret: %w", err)
	}
	profile.PublisherClientSecret = plain
	return &profile, nil
}

func (r *idpRepository) CreateProfile(ctx context.Context, profile *OrganizationIDPProfile) error {
	if profile != nil && profile.PublisherClientSecret != "" {
		sealed, err := sealPublisherSecret(r.cipher, profile.PublisherClientSecret)
		if err != nil {
			return fmt.Errorf("seal publisher_client_secret: %w", err)
		}
		cp := *profile
		cp.PublisherClientSecret = sealed
		return r.db.WithContext(ctx).Create(&cp).Error
	}
	return r.db.WithContext(ctx).Create(profile).Error
}

func (r *idpRepository) UpdateProfileColumns(ctx context.Context, profile *OrganizationIDPProfile, orgID string, updates map[string]interface{}) error {
	if v, ok := updates["publisher_client_secret"]; ok {
		plain, _ := v.(string)
		sealed, err := sealPublisherSecret(r.cipher, plain)
		if err != nil {
			return fmt.Errorf("seal publisher_client_secret: %w", err)
		}
		updates["publisher_client_secret"] = sealed
	}
	return r.db.WithContext(ctx).
		Model(profile).
		Where("org_id = ?", orgID).
		Updates(updates).Error
}

func (r *idpRepository) CreateAuditEvent(ctx context.Context, event *IDPAuditEvent) error {
	return r.db.WithContext(ctx).Create(event).Error
}
