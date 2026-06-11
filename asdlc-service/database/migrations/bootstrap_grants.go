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

package migrations

import (
	"context"
	"log/slog"

	"gorm.io/gorm"
)

// RunBootstrapGrants restores the connecting role's full privilege set on
// objects it owns in the public schema. Runs before any other migration.
//
// Some managed-DB setups provision the app role via an out-of-band
// hardening step that REVOKEs everything and re-grants only DML
// (INSERT/SELECT/UPDATE/DELETE). The owner retains ALTER (an owner-only
// check that bypasses the ACL) so column-level migrations keep working,
// but raw `CREATE TABLE … REFERENCES <owned-table>` fails with
// `permission denied for table <owned-table>` because REFERENCES is an
// ACL bit that was stripped.
//
// Owners can always GRANT to themselves, so this is a safe self-heal:
// no-op when fine, recovery when an out-of-band REVOKE has run.
// Errors are logged and swallowed so a more locked-down environment
// can still reach the diagnostic specific migration errors that follow.
func RunBootstrapGrants(ctx context.Context, db *gorm.DB) error {
	stmts := []string{
		`GRANT ALL ON ALL TABLES IN SCHEMA public TO CURRENT_USER`,
		`GRANT ALL ON ALL SEQUENCES IN SCHEMA public TO CURRENT_USER`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO CURRENT_USER`,
		`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON SEQUENCES TO CURRENT_USER`,
	}
	for _, s := range stmts {
		if err := db.WithContext(ctx).Exec(s).Error; err != nil {
			slog.Warn("bootstrap_grants: statement failed (continuing)", "stmt", s, "error", err)
		}
	}
	return nil
}
