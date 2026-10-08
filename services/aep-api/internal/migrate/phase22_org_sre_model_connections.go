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

package migrate

import (
	"context"

	"gorm.io/gorm"
)

// RunPhase22OrgSreModelConnections creates org_sre_model_connections: the
// org's SRE model connection, the OpenAI-compatible endpoint the OpenChoreo
// SRE agent calls when set. One row per org; the key's bytes live in
// org_secrets under sre-model/key.
func RunPhase22OrgSreModelConnections(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Exec(`
CREATE TABLE IF NOT EXISTS org_sre_model_connections (
	oc_org_id    TEXT PRIMARY KEY,
	base_url     TEXT NOT NULL,
	host         TEXT NOT NULL,
	model        TEXT NOT NULL,
	connected_at TIMESTAMPTZ NOT NULL,
	updated_at   TIMESTAMPTZ NOT NULL,
	updated_by   TEXT NOT NULL
)`).Error
}
