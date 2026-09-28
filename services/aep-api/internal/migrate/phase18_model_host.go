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

// modelHostTables are the usage rows that carry the host their cost was priced
// on: spec turns, run cycles, executions and the ledger copied from the last
// two.
var modelHostTables = []string{"agent_turns", "run_cycles", "executions", "agent_usage_ledger"}

// RunPhase18ModelHost makes usage host-aware: model_rates is re-keyed from
// (model_id) to (host, model_id), and every usage row gains model_host, the host
// of the model connection it ran on. Every existing rate and usage row is
// api.anthropic.com's — it is the only host that could be connected before this
// step — so that is what the backfill writes, and it leaves every frozen
// cost_usd exactly as it was: a (api.anthropic.com, model) rate is the rate the
// model had before.
//
// On the normal boot path AutoMigrate has already added the columns from the
// models, NULLABLE, because the tables exist. The ADD COLUMNs here keep the step
// true on its own, which is what lets a dbtest rebuild the pre-migration shape.
//
// ## One-shot by construction
//
// The backfill matches NULL only, never the empty string. The application never
// writes a NULL host: gorm inserts an empty string for an unstamped one, and the
// ledger copies its source row. So after the first run no NULL remains and none
// is ever produced, and a re-run on the next boot updates nothing. Matching the
// empty string as well would
// relabel, on every boot, a cycle still waiting for its dispatch and every
// provision execution (which runs no model) as api.anthropic.com's.
//
// The columns stay nullable for the same reason: gorm's AutoMigrate reconciles
// nullability from the struct tags on every boot, so a NOT NULL here would be
// dropped again unless the tags said NOT NULL — and a NOT NULL tag would make
// AutoMigrate backfill empty strings on the upgrade boot, which this step could
// not tell apart from an unstamped row.
//
// ## The rates key
//
// The primary-key swap is guarded on the CURRENT key's shape (as in phase15), so
// it fires once, while the old key is still in place. ADD PRIMARY KEY makes host
// NOT NULL, which is why the backfill runs first. model_rates_seed runs before
// this step on the upgrade boot; it counts a NULL-host row as the Anthropic one,
// so it never inserts into the old key what this step then backfills.
//
// Forward-only, idempotent, and one transaction: a boot interrupted part-way
// leaves the pre-migration shape, not half of each.
func RunPhase18ModelHost(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := rekeyModelRates(tx); err != nil {
			return err
		}
		for _, table := range modelHostTables {
			if err := backfillModelHost(tx, table); err != nil {
				return err
			}
		}
		return nil
	})
}

// rekeyModelRates adds and backfills model_rates.host and widens the key to
// (host, model_id).
func rekeyModelRates(tx *gorm.DB) error {
	exists, err := probeTable(tx, "model_rates")
	if err != nil || !exists {
		return err
	}
	if err := tx.Exec(`ALTER TABLE model_rates ADD COLUMN IF NOT EXISTS host TEXT`).Error; err != nil {
		return fmt.Errorf("phase18 model_rates: add host: %w", err)
	}
	if err := tx.Exec(`UPDATE model_rates SET host = ? WHERE host IS NULL`, modelconn.AnthropicHost).Error; err != nil {
		return fmt.Errorf("phase18 model_rates: backfill host: %w", err)
	}
	if err := tx.Exec(`DO $$
		 BEGIN
		   IF EXISTS (
		     SELECT 1 FROM pg_constraint c
		      WHERE c.conrelid = 'model_rates'::regclass
		        AND c.contype  = 'p'
		        AND (SELECT array_agg(a.attname::text ORDER BY a.attname)
		               FROM pg_attribute a
		              WHERE a.attrelid = c.conrelid
		                AND a.attnum = ANY(c.conkey)) = ARRAY['model_id']::text[]
		   ) THEN
		     ALTER TABLE model_rates
		       DROP CONSTRAINT model_rates_pkey,
		       ADD  PRIMARY KEY (host, model_id);
		   END IF;
		 END $$`).Error; err != nil {
		return fmt.Errorf("phase18 model_rates: re-key: %w", err)
	}
	return nil
}

// backfillModelHost adds table.model_host and stamps every row that predates it
// with api.anthropic.com.
func backfillModelHost(tx *gorm.DB, table string) error {
	exists, err := probeTable(tx, table)
	if err != nil || !exists {
		return err
	}
	if err := tx.Exec(fmt.Sprintf(
		`ALTER TABLE %s ADD COLUMN IF NOT EXISTS model_host TEXT`, table)).Error; err != nil {
		return fmt.Errorf("phase18 %s: add model_host: %w", table, err)
	}
	if err := tx.Exec(fmt.Sprintf(
		`UPDATE %s SET model_host = ? WHERE model_host IS NULL`, table), modelconn.AnthropicHost).Error; err != nil {
		return fmt.Errorf("phase18 %s: backfill model_host: %w", table, err)
	}
	return nil
}

// probeTable reports whether a public table exists, surfacing the probe's
// error rather than guessing: a step that skipped on a failed probe would
// report success over a schema it never touched.
func probeTable(tx *gorm.DB, table string) (bool, error) {
	var exists bool
	if err := tx.Raw(`SELECT to_regclass(?) IS NOT NULL`, "public."+table).Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("phase18 probe %s: %w", table, err)
	}
	return exists, nil
}
