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
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// sreModelKeyStoreKey is the `org_secrets` key holding the SRE model
// connection key's encrypted bytes.
const sreModelKeyStoreKey = "sre-model/key"

// OrgSreModelConnection is the org's SRE model connection: the
// OpenAI-compatible endpoint only the OpenChoreo SRE agent calls. Format and
// auth are fixed (OpenAI-compatible, Bearer), so they are not stored — unlike
// OrgModelConnection, which serves multiple formats and auth schemes. The
// key's bytes live in org_secrets under sreModelKeyStoreKey. Created by
// migrate's phase22_org_sre_model_connections step (raw SQL), not
// AutoMigrate.
type OrgSreModelConnection struct {
	OcOrgID     string    `gorm:"column:oc_org_id;primaryKey;type:text"`
	BaseURL     string    `gorm:"column:base_url;not null"`
	Host        string    `gorm:"column:host;not null"`
	Model       string    `gorm:"column:model;not null"`
	ConnectedAt time.Time `gorm:"column:connected_at;not null"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null"`
	UpdatedBy   string    `gorm:"column:updated_by;not null"`
}

func (OrgSreModelConnection) TableName() string { return "org_sre_model_connections" }

// Connection is the row as the SRE agent dispatch path reads it: always
// OpenAI-compatible over a bearer token.
func (r OrgSreModelConnection) Connection() modelconn.Connection {
	return modelconn.Connection{
		Format:     modelconn.FormatOpenAICompatible,
		BaseURL:    r.BaseURL,
		Host:       r.Host,
		Model:      r.Model,
		AuthScheme: modelconn.AuthBearer,
	}
}
