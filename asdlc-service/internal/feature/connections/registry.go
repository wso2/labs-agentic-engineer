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

// Package connections implements the org-level connection registry (the
// reusable "definition" layer for external dependencies, plan §3) and — later —
// the per-project provisioning of those connections onto the OpenChoreo Resource
// model. This file is the registry; provisioning lives alongside.
package connections

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/wso2/asdlc/asdlc-service/models"
)

// Registry is the org-level catalog of registered external connections.
type Registry struct {
	db *gorm.DB
}

func NewRegistry(db *gorm.DB) *Registry { return &Registry{db: db} }

// Upsert registers (or updates) a connection definition for an org. On first
// registration the OC ResourceType name equals the connection name. If the
// config key schema CHANGES for an existing connection, the ResourceType name
// gets a numeric suffix (salesforce → salesforce-2) — ResourceTypes are
// effectively immutable, so a new shape needs a new type — and the stored
// schema is updated. Description-only edits don't bump the suffix.
func (r *Registry) Upsert(ctx context.Context, orgID, name, description string, schema []models.ConfigKey) (*models.Connection, error) {
	if orgID == "" || name == "" {
		return nil, fmt.Errorf("connections: orgID and name are required")
	}
	var existing models.Connection
	err := r.db.WithContext(ctx).Where("org_id = ? AND name = ?", orgID, name).First(&existing).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c := &models.Connection{
			OrgID:            orgID,
			Name:             name,
			Description:      description,
			ConfigSchema:     schema,
			ResourceTypeName: name,
		}
		if err := r.db.WithContext(ctx).Create(c).Error; err != nil {
			return nil, fmt.Errorf("connections: create %q: %w", name, err)
		}
		return c, nil
	case err != nil:
		return nil, fmt.Errorf("connections: lookup %q: %w", name, err)
	default:
		existing.Description = description
		if len(schema) > 0 && !SchemaEqual(existing.ConfigSchema, schema) {
			existing.ConfigSchema = schema
			existing.ResourceTypeName = bumpSuffix(existing.ResourceTypeName, name)
		}
		if err := r.db.WithContext(ctx).Save(&existing).Error; err != nil {
			return nil, fmt.Errorf("connections: update %q: %w", name, err)
		}
		return &existing, nil
	}
}

// Get returns a connection by (org, name), or (nil, nil) when absent.
func (r *Registry) Get(ctx context.Context, orgID, name string) (*models.Connection, error) {
	var c models.Connection
	err := r.db.WithContext(ctx).Where("org_id = ? AND name = ?", orgID, name).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("connections: get %q: %w", name, err)
	}
	return &c, nil
}

// List returns all connections registered for an org, ordered by name.
func (r *Registry) List(ctx context.Context, orgID string) ([]models.Connection, error) {
	var out []models.Connection
	if err := r.db.WithContext(ctx).Where("org_id = ?", orgID).Order("name").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("connections: list org %q: %w", orgID, err)
	}
	return out, nil
}

// SchemaEqual reports whether two config key schemas are equivalent (same keys,
// same secret flags), order-independent.
func SchemaEqual(a, b []models.ConfigKey) bool {
	if len(a) != len(b) {
		return false
	}
	idx := make(map[string]bool, len(a))
	for _, k := range a {
		idx[k.Key] = k.Secret
	}
	for _, k := range b {
		s, ok := idx[k.Key]
		if !ok || s != k.Secret {
			return false
		}
	}
	return true
}

// bumpSuffix returns the next "-N" suffix for an immutable-ResourceType rename.
// base "salesforce", current "salesforce" → "salesforce-2"; "salesforce-2" → "salesforce-3".
func bumpSuffix(current, base string) string {
	n := 1
	if rest, ok := strings.CutPrefix(current, base+"-"); ok {
		if parsed, err := strconv.Atoi(rest); err == nil {
			n = parsed
		}
	}
	return fmt.Sprintf("%s-%d", base, n+1)
}
