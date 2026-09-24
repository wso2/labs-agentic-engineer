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

	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

// RunModelRatesSeed seeds model_rates with the platform's active models at the
// rates in force today (#291, amended ADR-0011). The table is ops-managed
// thereafter — a price change is an UPDATE, and because USD is stamped at
// capture (never re-derived), that change only affects work captured after it.
//
// Two rows, exactly the contract's AgentModel enum — the models an org can
// choose for every one of its agents (spec and coding alike, ADR-0036):
//
//   - claude-sonnet-5 (the default agent model) at its
//     INTRODUCTORY rates, in force through 2026-08-31: input $2.00/MTok,
//     output $10.00/MTok, cache-read ~0.1x input ($0.20), cache-write ~1.25x
//     input ($2.50). The 2026-08-31 step-up to standard $3/$15 is the first
//     intended ops override — a live demonstration of why history must not
//     reprice.
//   - claude-haiku-4-5 at standard rates ($1.00/$5.00, cache-read $0.10,
//     cache-write $1.25), the second model an org can choose. Pricing is
//     all-or-nothing per capture (Stamper.SumCost), so an offered model with
//     no row would stamp every run on it null and show tokens instead of
//     dollars.
//
// Idempotent per model: seeds only where the model has no row, so an
// ops-adjusted rate is never clobbered by a redeploy. AutoMigrate (BaseModels)
// creates the table before this step runs.
func RunModelRatesSeed(ctx context.Context, db *gorm.DB) error {
	seeds := []modelcost.ModelRate{
		{
			ModelID:           "claude-sonnet-5",
			InputPerMTok:      2.00,
			OutputPerMTok:     10.00,
			CacheReadPerMTok:  0.20,
			CacheWritePerMTok: 2.50,
		},
		{
			ModelID:           "claude-haiku-4-5",
			InputPerMTok:      1.00,
			OutputPerMTok:     5.00,
			CacheReadPerMTok:  0.10,
			CacheWritePerMTok: 1.25,
		},
	}
	for _, seed := range seeds {
		var count int64
		if err := db.WithContext(ctx).Model(&modelcost.ModelRate{}).
			Where("model_id = ?", seed.ModelID).Count(&count).Error; err != nil {
			return fmt.Errorf("model_rates seed: count %s: %w", seed.ModelID, err)
		}
		if count > 0 {
			continue // already seeded (or ops-adjusted) — never overwrite
		}
		if err := db.WithContext(ctx).Create(&seed).Error; err != nil {
			return fmt.Errorf("model_rates seed: insert %s: %w", seed.ModelID, err)
		}
	}
	return nil
}

// LoadModelRates reads every model_rates row — the boot-time source for the
// modelcost.Stamper (app wiring). Lives here beside the seed because migrate is
// the sanctioned gorm importer for the rates table; app calls it after Steps.
func LoadModelRates(ctx context.Context, db *gorm.DB) ([]modelcost.ModelRate, error) {
	var rows []modelcost.ModelRate
	if err := db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("model_rates load: %w", err)
	}
	return rows, nil
}
