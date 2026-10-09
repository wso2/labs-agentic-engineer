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

package app

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/migrate"
)

// Bootstrap runs the imperative first-boot schema steps that must complete
// before the service graph is assembled and served. It is deliberately kept out
// of Build so the composition stays a pure, side-effect-light assembly a
// component test can drive without migrating a database.
//
//   - RunBootstrapGrants self-grants privileges on owned schema objects
//     (best-effort, non-fatal) so FK-creating migrations don't trip over a
//     managed-DB REVOKE that stripped REFERENCES/TRIGGER from the owner role.
//   - RunAll then applies every migration in dependency order (each
//     context-taking step gets its own timeout internally).
func Bootstrap(ctx context.Context, db *gorm.DB, cfg config.Config) error {
	grantCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	_ = migrate.RunBootstrapGrants(grantCtx, db)
	cancel()

	if err := migrate.RunAll(ctx, db, cfg.DeploymentTier); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	return nil
}
