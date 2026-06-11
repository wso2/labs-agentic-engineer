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

import (
	"time"

	"github.com/google/uuid"
)

// Organization is a local UUID side-car for an OpenChoreo namespace.
//
// OpenChoreo has no Organization CRD — namespaces *are* organizational
// boundaries. The BFF maintains UUIDs locally so other tables can foreign-key
// onto an org without depending on the OC namespace name (which is mutable in
// principle and ambiguous as a join key across renames).
type Organization struct {
	UUID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"uuid"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	DisplayName string    `gorm:"" json:"displayName,omitempty"`
	CreatedBy   string    `gorm:"" json:"createdBy,omitempty"`
	CreatedAt   time.Time `gorm:"not null;default:CURRENT_TIMESTAMP" json:"createdAt"`
	// ThunderOrgUUID is the org UUID Thunder assigns and embeds as the
	// JWT's `ouId` claim. SM-API derives per-org namespaces from this,
	// not from UUID (which is just the BFF's local PK). Populated
	// lazily by orgensure middleware on the first authed request that
	// carries an `ouId` claim. Nullable for backward compatibility.
	ThunderOrgUUID *uuid.UUID `gorm:"type:uuid;column:thunder_org_uuid;index" json:"thunderOrgUuid,omitempty"`
}

// OrganizationView is the API response shape — joins the local UUID with the
// OC namespace's display fields.
type OrganizationView struct {
	UUID        uuid.UUID `json:"uuid"`
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName,omitempty"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// OrganizationList is the list-endpoint response.
type OrganizationList struct {
	Items []OrganizationView `json:"items"`
}
