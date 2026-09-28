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
	"fmt"

	"gorm.io/gorm"
)

// RunPhase20ModelKeyRename copies every org's model connection key from its
// Anthropic-era org_secrets key, `anthropic/key`, to `model/key`, where every
// reader looks from this release on. The sealed value is copied
// as is — the column cipher binds no associated data to the key name — with
// its updated_at, so the bytes are identical.
//
// It runs at boot, before anything is served, so no read of `model/key` can
// miss. It is the first half of a copy-then-switch: it deletes nothing, and
// the SM-API mirror is moved after the app is assembled by
// organization.ModelKeyRename, which also retires `anthropic/key` once the old
// mirror is gone (its presence marks the org's move as unfinished) — never at
// boot, so a replica of the previous release still draining can read it.
//
// Only an org with a connection row is copied: bytes with no row are a
// disconnected key (or a credential phase19 did not carry over), and copying
// them would keep them under a name nothing retires.
//
// Idempotent: a target at least as new as its source is left alone, so a
// re-run changes nothing, while a key a replica of the previous release saved
// under the old name since is copied again. organization's per-org copy uses
// the same rules.
func RunPhase20ModelKeyRename(ctx context.Context, db *gorm.DB) error {
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO org_secrets (oc_org_id, key, value, updated_at)
		SELECT s.oc_org_id, 'model/key', s.value, s.updated_at FROM org_secrets s
		 WHERE s.key = 'anthropic/key'
		   AND EXISTS (SELECT 1 FROM org_model_connections c WHERE c.oc_org_id = s.oc_org_id)
		ON CONFLICT (oc_org_id, key) DO UPDATE
		  SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
		  WHERE org_secrets.updated_at < EXCLUDED.updated_at`).Error; err != nil {
		return fmt.Errorf("phase20 copy model connection keys: %w", err)
	}
	return nil
}
