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

// ErrConnectionNotFound is returned when a connection is not registered for the org.
var ErrConnectionNotFound = errors.New("connection not found")

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

// ConnectionConsumer is one component that uses a registered connection.
type ConnectionConsumer struct {
	ProjectID     string `json:"projectId"`
	ComponentName string `json:"componentName"`
}

// Consumers returns the components that depend on a connection — every
// component-type ComponentTask whose `dependsOnConnections` includes `name`,
// deduplicated by (project, component). Empty ⇒ no project/component uses it, so
// the connection is safe to delete. (Component tasks are removed with their
// project, so a stale registration from a deleted project reports no consumers.)
func (r *Registry) Consumers(ctx context.Context, orgID, name string) ([]ConnectionConsumer, error) {
	if orgID == "" || name == "" {
		return nil, fmt.Errorf("connections: orgID and name required")
	}
	var tasks []models.ComponentTask
	// jsonb containment: depends_on_connections @> ["name"] — exact element
	// match (won't false-positive `openweather` against `openweathermap`).
	if err := r.db.WithContext(ctx).
		Where("org_id = ? AND type = ? AND depends_on_connections @> ?::jsonb",
			orgID, models.TaskTypeComponent, `["`+name+`"]`).
		Find(&tasks).Error; err != nil {
		return nil, fmt.Errorf("connections: consumers of %q: %w", name, err)
	}
	seen := make(map[string]struct{}, len(tasks))
	out := make([]ConnectionConsumer, 0, len(tasks))
	for i := range tasks {
		k := tasks[i].ProjectID + "/" + tasks[i].ComponentName
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, ConnectionConsumer{ProjectID: tasks[i].ProjectID, ComponentName: tasks[i].ComponentName})
	}
	return out, nil
}

// Delete removes a connection's org-level registration. It does NOT check
// consumers — the caller (DeleteConnection endpoint) enforces the "not in use"
// guard so it can return a precise error. The shared, immutable OC ResourceType
// is intentionally left in place (re-registration reuses it).
func (r *Registry) Delete(ctx context.Context, orgID, name string) error {
	if orgID == "" || name == "" {
		return fmt.Errorf("connections: orgID and name required")
	}
	res := r.db.WithContext(ctx).Where("org_id = ? AND name = ?", orgID, name).Delete(&models.Connection{})
	if res.Error != nil {
		return fmt.Errorf("connections: delete %q: %w", name, res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrConnectionNotFound
	}
	return nil
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
