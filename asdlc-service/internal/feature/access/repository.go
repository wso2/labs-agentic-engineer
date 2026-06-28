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

// Package access implements the cross-project access-request store (marketplace
// P3.5): the tracking/UX record for a consumer asking a provider project to
// publish an org service cross-project. This file is the repository; the request
// action + status sync are wired alongside in later increments.
package access

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// ErrAccessRequestNotFound is returned when no access request matches the lookup.
var ErrAccessRequestNotFound = errors.New("access request not found")

// Repository is the gorm-backed store for AccessRequest rows.
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// Create inserts a new access request. The ID is minted here when unset (so the
// store works without relying on the DB `gen_random_uuid()` default), and the
// status defaults to `requested` when empty.
func (r *Repository) Create(ctx context.Context, ar *models.AccessRequest) error {
	if ar == nil {
		return fmt.Errorf("access: nil access request")
	}
	if ar.OrgID == "" || ar.ConsumerProjectID == "" {
		return fmt.Errorf("access: orgID and consumerProjectID are required")
	}
	if ar.ID == "" {
		ar.ID = uuid.NewString()
	}
	if ar.Status == "" {
		ar.Status = models.AccessRequestStatusRequested
	}
	if err := r.db.WithContext(ctx).Create(ar).Error; err != nil {
		return fmt.Errorf("access: create request: %w", err)
	}
	return nil
}

// Get returns a single access request scoped to (org, id). Returns
// ErrAccessRequestNotFound when absent or owned by another org (no existence
// leak across orgs).
func (r *Repository) Get(ctx context.Context, orgID, id string) (*models.AccessRequest, error) {
	if orgID == "" || id == "" {
		return nil, fmt.Errorf("access: orgID and id are required")
	}
	var ar models.AccessRequest
	err := r.db.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, id).First(&ar).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAccessRequestNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("access: get %q: %w", id, err)
	}
	return &ar, nil
}

// ListByConsumerProject returns every access request a project's components have
// raised, newest first.
func (r *Repository) ListByConsumerProject(ctx context.Context, orgID, projectID string) ([]models.AccessRequest, error) {
	if orgID == "" || projectID == "" {
		return nil, fmt.Errorf("access: orgID and projectID are required")
	}
	var out []models.AccessRequest
	if err := r.db.WithContext(ctx).
		Where("org_id = ? AND consumer_project_id = ?", orgID, projectID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("access: list consumer project %q: %w", projectID, err)
	}
	return out, nil
}

// FindOpenForTarget returns an existing still-open (requested|in_progress)
// request targeting the given provider component, for idempotency/dedup — so
// multiple consumers of one provider endpoint reuse the same publish task.
// Returns (nil, nil) when there is no open request for that target.
func (r *Repository) FindOpenForTarget(ctx context.Context, orgID, providerProjectID, providerComponentName string) (*models.AccessRequest, error) {
	if orgID == "" || providerProjectID == "" || providerComponentName == "" {
		return nil, fmt.Errorf("access: orgID, providerProjectID and providerComponentName are required")
	}
	var ar models.AccessRequest
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND provider_project_id = ? AND provider_component_name = ? AND status IN ?",
			orgID, providerProjectID, providerComponentName,
			[]string{models.AccessRequestStatusRequested, models.AccessRequestStatusInProgress}).
		Order("created_at DESC").
		First(&ar).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("access: find open for target %s/%s: %w", providerProjectID, providerComponentName, err)
	}
	return &ar, nil
}

// UpdateStatus flips a request's status (and bumps updated_at). Returns
// ErrAccessRequestNotFound when no row matched.
func (r *Repository) UpdateStatus(ctx context.Context, id, status string) error {
	if id == "" || status == "" {
		return fmt.Errorf("access: id and status are required")
	}
	res := r.db.WithContext(ctx).Model(&models.AccessRequest{}).
		Where("id = ?", id).
		Update("status", status)
	if res.Error != nil {
		return fmt.Errorf("access: update status %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrAccessRequestNotFound
	}
	return nil
}

// ListByProviderTask returns every consumer request riding on one provider
// publish task — used by the grant-sync to flip all consumers together when the
// provider task lands.
func (r *Repository) ListByProviderTask(ctx context.Context, providerTaskID string) ([]models.AccessRequest, error) {
	if providerTaskID == "" {
		return nil, fmt.Errorf("access: providerTaskID is required")
	}
	var out []models.AccessRequest
	if err := r.db.WithContext(ctx).
		Where("provider_task_id = ?", providerTaskID).
		Order("created_at DESC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("access: list by provider task %q: %w", providerTaskID, err)
	}
	return out, nil
}
