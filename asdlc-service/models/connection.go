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

package models

import "time"

// Connection is an org-level registered external dependency — the reusable
// "definition" layer (plan §3): name + description + the config key schema +
// the OpenChoreo ResourceType the wiring maps to. One row per (OrgID, Name).
//
// Values + wiring are NOT stored here — they live per-project in the OC Resource
// model (Resource + per-env ResourceReleaseBinding). The registry is the catalog
// the architect reads to reuse a connection's shape across projects, and the
// source of which ResourceType the provisioner authors against.
type Connection struct {
	ID          string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID       string `gorm:"uniqueIndex:uq_connection_org_name;not null" json:"-"`
	Name        string `gorm:"uniqueIndex:uq_connection_org_name;not null" json:"name"`
	Description string `gorm:"type:text" json:"description,omitempty"`

	// ConfigSchema is the connection's key schema (which env-var keys, which are
	// secret). This alone drives the OC ResourceType (no separate auth descriptor).
	ConfigSchema []ConfigKey `gorm:"type:jsonb;serializer:json" json:"config"`

	// ResourceTypeName is the OC ResourceType this connection's wiring uses.
	// Defaults to Name; a config-schema change bumps a numeric suffix
	// (salesforce → salesforce-2) because ResourceTypes are effectively immutable.
	ResourceTypeName string `gorm:"not null" json:"resourceTypeName"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
