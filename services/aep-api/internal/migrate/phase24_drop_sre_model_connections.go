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

// sreSecretKeys are the org_secrets keys the SRE model connection and the
// per-org handoff token were stored under before both moved to install time.
var sreSecretKeys = []string{"sre-model/key", "sre-model/seed-applied", "sre/handoff-token"}

// RunPhase24DropSreModelConnections removes what phase22 and its service left
// behind once the SRE agent's model and handoff key became install-time
// values that `aectl sre install` writes into the agent's own Secret: the
// org_sre_model_connections table, and the org_secrets rows holding the
// stored SRE key, the seed marker and the minted handoff token. Phase22 stays
// in the list because databases have already run it. Idempotent.
func RunPhase24DropSreModelConnections(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`DROP TABLE IF EXISTS org_sre_model_connections`).Error; err != nil {
			return err
		}
		return tx.Exec(`DELETE FROM org_secrets WHERE key IN ?`, sreSecretKeys).Error
	})
}
