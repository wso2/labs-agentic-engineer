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
	"fmt"
	"log/slog"

	"gorm.io/gorm"
)

// RunPhase7Skills now performs the clean cutover to git-backed skills storage
// (docs/design/skills-repo-storage.md §11): the per-org `org-skills` GitHub
// repo is the single source of truth, so the three Postgres tables that
// previously held skills are dropped. There is no custom-skill data to
// preserve (the org-skills feature never shipped), and built-ins re-seed from
// the embedded container files into each org's repo on demand.
//
// Idempotent — DROP TABLE IF EXISTS is a no-op on a DB that never had them.
func RunPhase7Skills(db *gorm.DB) error {
	for _, table := range []string{
		"design_version_skill_snapshots",
		"skill_audit_events",
		"skills",
	} {
		if !hasTable(db, table) {
			continue
		}
		if err := db.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, table)).Error; err != nil {
			return fmt.Errorf("phase7_skills: drop %s: %w", table, err)
		}
		slog.Info("phase7_skills migration: dropped legacy skills table (git-backed cutover)", "table", table)
	}
	return nil
}
