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
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/database"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/seed"
)

// Infra is every external dependency Assemble needs already resolved — the
// results of all boot-time I/O. Resolve produces the real bundle (network, disk,
// OpenBao, the dev seed); Fake produces a zero-I/O bundle for assembly tests.
// Assemble reads it and does no I/O of its own, so the whole service graph
// assembles deterministically in milliseconds.
type Infra struct {
	DB              *gorm.DB
	CredentialStore secrets.TxCredentialStore
	ColumnCipher    *secrets.ColumnCipher // same key as CredentialStore; seals column values
	// RateStamper prices captured agent usage at write time (#291), loaded once
	// from model_rates after migration. Assemble threads it into the turn +
	// execution repositories; nil ⇒ no stamping (cost_usd stays null).
	RateStamper *modelcost.Stamper
}

// Resolve performs every boot side effect and returns the resolved Infra: it
// opens the database and runs first-boot migrations (Bootstrap), builds the
// credential store, runs the dev-only app-platform seed (fatal). This is the ONLY place in the graph that
// touches the network, the clock, OpenBao, or the filesystem at boot — Assemble
// is pure. Required infra errors; optional infra warns.
func Resolve(ctx context.Context, cfg config.Config) (Infra, error) {
	// Credential encryption key — needed for migrations (encrypt-in-place) and
	// the CredentialStore / column cipher. Decoded once here.
	credKey, err := base64.StdEncoding.DecodeString(cfg.CredentialEncryptionKey)
	if err != nil || len(credKey) != 32 {
		// config.Validate guarantees this decodes to 32 bytes; kept as defense.
		return Infra{}, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key: %w", err)
	}

	// Database + first-boot schema. Opened here (not in main) so main is just
	// Resolve → Assemble → serve, and Assemble never touches the DB at build time.
	db, err := database.Open(cfg.DatabaseURL, migrate.BaseModels()...)
	if err != nil {
		return Infra{}, fmt.Errorf("database init: %w", err)
	}
	if err := Bootstrap(ctx, db, cfg, credKey); err != nil {
		return Infra{}, err
	}

	// Model pricing (#291): load the model_rates the seed migration wrote into
	// an immutable in-memory Stamper. Boot-time and not per-capture because
	// rates are ops-managed and change rarely — a rate edit takes effect on the
	// next restart. Assemble injects it into the capture repositories.
	rateRows, err := migrate.LoadModelRates(ctx, db)
	if err != nil {
		return Infra{}, fmt.Errorf("model rates load: %w", err)
	}
	rateStamper := modelcost.NewStamper(rateRows)

	// Credential store (AES-256-GCM over Postgres) + column cipher (same key).
	credStore, err := secrets.NewDBStore(db, credKey)
	if err != nil {
		return Infra{}, fmt.Errorf("credential store init: %w", err)
	}
	columnCipher, err := secrets.NewColumnCipher(credKey)
	if err != nil {
		return Infra{}, fmt.Errorf("column cipher init: %w", err)
	}
	slog.Info("credential store: postgres (aes-256-gcm)")

	// Dev-only app-platform seed (App private key + client_secret + webhook HMAC).
	// No-op outside DEPLOYMENT_TIER=dev.
	{
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		if err := seed.AppPlatformFromEnv(c, credStore, cfg); err != nil {
			cancel()
			return Infra{}, fmt.Errorf("app platform seed: %w", err)
		}
		cancel()
	}
	return Infra{
		DB:              db,
		CredentialStore: credStore,
		ColumnCipher:    columnCipher,
		RateStamper:     rateStamper,
	}, nil
}
