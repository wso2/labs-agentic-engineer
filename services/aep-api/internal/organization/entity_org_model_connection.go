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

// OrgModelConnection is the org's model connection: one row per org, absent
// while it has none. The key lives only in vault, under the org's default-key
// reference; this row holds only what is safe to read: where the connection
// points and what the save-time probe learned.
//
// There is no status column: a save is refused unless the probe passes and
// nothing revalidates later, so a stored row is usable by construction.
// Created by migrate's phase19_model_connection step (raw SQL, CHECKs on the
// enums), not AutoMigrate.
type OrgModelConnection struct {
	OcOrgID    string               `gorm:"column:oc_org_id;primaryKey;type:text"`
	Format     modelconn.Format     `gorm:"column:format;not null"`
	BaseURL    string               `gorm:"column:base_url;not null"`
	Host       string               `gorm:"column:host;not null"`
	Model      string               `gorm:"column:model;not null"`
	AuthScheme modelconn.AuthScheme `gorm:"column:auth_scheme;not null"`
	// ContextWindow and OutputLimit are NULL where the runtimes know the model
	// themselves (Anthropic's own API).
	ContextWindow *int               `gorm:"column:context_window"`
	OutputLimit   *int               `gorm:"column:output_limit"`
	ImageInput    modelconn.Tristate `gorm:"column:image_input;not null"`
	// KeyPreview is written empty: no character of the key is kept.
	KeyPreview string `gorm:"column:key_preview;not null"`
	// ConnectedAt is when a key was first saved on this host; a key rotation or
	// a model change keeps it, a host change resets it.
	ConnectedAt time.Time `gorm:"column:connected_at;not null"`
	UpdatedAt   time.Time `gorm:"column:updated_at;not null"`
	// UpdatedBy is the actor from the JWT; NULL on a row the migration carried
	// over from the Anthropic-only card, which recorded no author.
	UpdatedBy *string `gorm:"column:updated_by"`

	// The pre-reference-row triplet; nothing writes or reads it (the
	// default-key row names the reference).
	SecretRefName     *string `gorm:"column:secret_ref_name"`
	SecretRefKVPath   *string `gorm:"column:secret_ref_kv_path"`
	SecretRefProperty *string `gorm:"column:secret_ref_property"`
}

func (OrgModelConnection) TableName() string { return "org_model_connections" }

// Connection is the row as every consumer reads it.
func (r *OrgModelConnection) Connection() modelconn.Connection {
	return modelconn.Connection{
		Format:        r.Format,
		BaseURL:       r.BaseURL,
		Host:          r.Host,
		Model:         r.Model,
		AuthScheme:    r.AuthScheme,
		ContextWindow: r.ContextWindow,
		OutputLimit:   r.OutputLimit,
		ImageInput:    r.ImageInput,
	}
}
