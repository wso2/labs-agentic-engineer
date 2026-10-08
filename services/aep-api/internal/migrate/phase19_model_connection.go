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

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// RunPhase19ModelConnection moves every org's model connection out of the
// Anthropic-only card into its own table, org_model_connections: the cut-over
// that lets an org connect any public endpoint in one of the two formats.
//
// One locked transaction, in order:
//
//  1. lock org_anthropic_credentials and org_agent_settings against writes, so
//     a save from a replica of the previous release waits for the step and
//     then meets the new schema, never a half-moved one;
//  2. create org_model_connections;
//  3. backfill one row per ACTIVE default credential: Anthropic's own API, the
//     key as x-api-key, the model from org_agent_settings.model (the Anthropic
//     format's default when the org never chose) and NULL limits (the runtimes
//     know Claude). The key itself is not carried: it lives in the vault
//     behind the org's default-key reference row, and an org without one
//     re-enters it on the setup screen;
//  4. an org whose default credential is NOT active gets no connection: its
//     llm_disconnected_at is set (onboarding shows the disconnected alert), and
//     its unreachable key bytes (while org_secrets still has value rows) and
//     its Claude subscription — which cannot outlive the connection — are
//     deleted;
//  5. delete the default rows and restrict org_anthropic_credentials to the
//     Claude subscription (CHECK role = 'coding');
//  6. drop org_agent_settings.model.
//
// The deletes make the step one-way (ADR-0038 §10). Idempotent: a re-run
// finds no default row and no model column, and changes nothing.
//
// The SM-API copies of a deleted subscription (entity "anthropic-coding")
// cannot be removed here — deleting one needs a signed-in user's context — so
// they stay orphaned until the org next saves one, which overwrites that path.
func RunPhase19ModelConnection(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`LOCK TABLE org_anthropic_credentials, org_agent_settings IN SHARE ROW EXCLUSIVE MODE`).Error; err != nil {
			return fmt.Errorf("phase19 lock: %w", err)
		}
		if err := tx.Exec(createOrgModelConnections).Error; err != nil {
			return fmt.Errorf("phase19 create org_model_connections: %w", err)
		}
		if err := backfillModelConnections(tx); err != nil {
			return err
		}
		if err := retireInactiveDefaults(tx); err != nil {
			return err
		}
		stmts := []string{
			`DELETE FROM org_anthropic_credentials WHERE role = 'default'`,
			// ADD CONSTRAINT has no IF NOT EXISTS, and this list re-runs on
			// every boot, so the add is guarded.
			`DO $$
			 BEGIN
			   IF NOT EXISTS (
			     SELECT 1 FROM pg_constraint
			      WHERE conrelid = 'org_anthropic_credentials'::regclass
			        AND conname  = 'org_anthropic_credentials_subscription_only'
			   ) THEN
			     ALTER TABLE org_anthropic_credentials
			       ADD CONSTRAINT org_anthropic_credentials_subscription_only CHECK (role = 'coding');
			   END IF;
			 END $$`,
			`ALTER TABLE org_agent_settings DROP COLUMN IF EXISTS model`,
		}
		for i, sql := range stmts {
			if err := tx.Exec(sql).Error; err != nil {
				return fmt.Errorf("phase19 contract step %d: %w", i+1, err)
			}
		}
		return nil
	})
}

// createOrgModelConnections is the table organization.OrgModelConnection maps.
// Raw SQL, not AutoMigrate, so the CHECKs on its enums hold. It holds no key,
// preview or vault path; a table created by an earlier release still carries
// those columns until phase29 drops them.
const createOrgModelConnections = `
	CREATE TABLE IF NOT EXISTS org_model_connections (
	  oc_org_id           TEXT PRIMARY KEY,
	  format              TEXT NOT NULL CHECK (format IN ('anthropic', 'openai-compatible')),
	  base_url            TEXT NOT NULL,
	  host                TEXT NOT NULL,
	  model               TEXT NOT NULL,
	  auth_scheme         TEXT NOT NULL CHECK (auth_scheme IN ('x-api-key', 'bearer')),
	  context_window      INTEGER,
	  output_limit        INTEGER,
	  image_input         TEXT NOT NULL DEFAULT 'unknown' CHECK (image_input IN ('yes', 'no', 'unknown')),
	  connected_at        TIMESTAMPTZ NOT NULL,
	  updated_at          TIMESTAMPTZ NOT NULL,
	  updated_by          TEXT
	)`

// backfillModelConnections writes a connection for every active default
// credential. It reads no key column: a database that already ran this step
// has no default row left (the subscription_only CHECK keeps it so), and
// phase29 drops the preview and triplet columns an earlier release copied
// here. The model comes from org_agent_settings while that column still
// exists.
func backfillModelConnections(tx *gorm.DB) error {
	var hasModel bool
	if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM information_schema.columns
	                   WHERE table_schema = 'public' AND table_name = 'org_agent_settings' AND column_name = 'model')`).
		Scan(&hasModel).Error; err != nil {
		return fmt.Errorf("phase19 probe org_agent_settings.model: %w", err)
	}
	model := "?"
	args := []any{modelconn.FormatAnthropic, modelconn.AnthropicBaseURL, modelconn.AnthropicHost, modelconn.DefaultAnthropicModel}
	join := ""
	if hasModel {
		model = "COALESCE(s.model, ?)"
		join = "LEFT JOIN org_agent_settings s ON s.oc_org_id = c.oc_org_id"
	}
	args = append(args, modelconn.AuthXAPIKey, modelconn.Yes)
	sql := fmt.Sprintf(`
		INSERT INTO org_model_connections
		  (oc_org_id, format, base_url, host, model, auth_scheme, context_window, output_limit, image_input,
		   connected_at, updated_at, updated_by)
		SELECT c.oc_org_id, ?, ?, ?, %s, ?, NULL, NULL, ?,
		       c.connected_at, COALESCE(c.last_validated_at, c.connected_at), NULL
		  FROM org_anthropic_credentials c %s
		 WHERE c.role = 'default' AND c.status = 'active'
		ON CONFLICT (oc_org_id) DO NOTHING`, model, join)
	if err := tx.Exec(sql, args...).Error; err != nil {
		return fmt.Errorf("phase19 backfill connections: %w", err)
	}
	return nil
}

// retireInactiveDefaults handles every org whose default credential is not
// active (invalid or disconnected): it reads as having no connection. The
// key bytes are deleted only while org_secrets still has its legacy key
// column; phase29 renames it and deletes every value row.
func retireInactiveDefaults(tx *gorm.DB) error {
	const inactive = `SELECT oc_org_id FROM org_anthropic_credentials WHERE role = 'default' AND status <> 'active'`
	stmts := []string{
		`UPDATE organizations SET llm_disconnected_at = now()
		  WHERE llm_disconnected_at IS NULL AND name IN (` + inactive + `)`,
	}
	if hasColumn(tx, "org_secrets", "key") {
		stmts = append(stmts, `DELETE FROM org_secrets
		  WHERE key IN ('anthropic/key', 'anthropic/coding-key') AND oc_org_id IN (`+inactive+`)`)
	}
	stmts = append(stmts, `DELETE FROM org_anthropic_credentials
		  WHERE role = 'coding' AND oc_org_id IN (`+inactive+`)`)
	for i, sql := range stmts {
		if err := tx.Exec(sql).Error; err != nil {
			return fmt.Errorf("phase19 retire inactive defaults step %d: %w", i+1, err)
		}
	}
	return nil
}
