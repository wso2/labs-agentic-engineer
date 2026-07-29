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

// Package dbtest is the DB-backed test harness. A single call —
// dbtest.New(t) — hands back a pristine, fully-migrated, isolated *gorm.DB per
// test, so a DB test is just:
//
//	func TestFoo(t *testing.T) {
//	    t.Parallel()
//	    db := dbtest.New(t)
//	    repo := repositories.NewFooRepository(db)
//	    // ...just write the test.
//	}
//
// How it works (template-clone, not truncate/transaction):
//
//   - Once per process: one throwaway Postgres is started via testcontainers-go
//     (sync.Once, reused across every test in the run) and migrated ONCE with
//     the real production migrator into a template database. The container is
//     fully hermetic — a run depends on nothing but Docker (no compose stack, no
//     :5433, no shared server to collide on). Its schema is byte-for-byte the
//     production schema, because it is built by the same migrate.RunAll the
//     app runs at boot.
//   - Per test: pgtestdb issues `CREATE DATABASE … TEMPLATE <template>` — a
//     Postgres file-copy in single-digit ms — so each test gets its own
//     pristine, fully-migrated, fully-isolated database, dropped on t.Cleanup.
//     Template-clone keeps every test a real separate DB with real transactions,
//     so production paths that use `FOR UPDATE SKIP LOCKED` / manual Begin/Commit
//     test truthfully (an outer wrapping txn would break them).
//
// Fast lane: New calls t.Skip under `testing.Short()`, so `make test`
// (go test -short ./...) needs no Docker and `make test-db` (go test ./...)
// runs the DB tier. There is no build tag — every test compiles in one world.
// Where Docker is unavailable, New skips (via testcontainers' provider health
// check) rather than failing, so machines without Docker degrade gracefully.
package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/peterldowns/pgtestdb"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	// Registers the "pgx" database/sql driver that pgtestdb opens the
	// template/base connections with (Config.DriverName below).
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/wso2/aep/aep-api/internal/migrate"
)

const (
	// postgresImage is the throwaway backing store. It must track the Postgres
	// major the app runs against so template-clone schema fidelity is real.
	postgresImage = "postgres:17-alpine"

	// migrationTier drives the dev-only destructive/seed branches in RunAll.
	// "dev" matches config's DEPLOYMENT_TIER default, so the template is built
	// on the same code path the local app boots on.
	migrationTier = "dev"

	// baseUser/basePassword/baseDB are the throwaway container superuser and its
	// default database. pgtestdb connects as this superuser to CREATE/DROP the
	// template and per-test clones, and (since it is the superuser) migrations
	// run with full privileges — e.g. phase6's `CREATE EXTENSION pgcrypto`.
	baseUser     = "postgres"
	basePassword = "postgres"
	baseDB       = "aep_dbtest"

	// schemaVersion is folded into the migrator Hash so pgtestdb rebuilds the
	// template when the schema changes. The backing container is ephemeral (one
	// per process), so a stale template can never outlive a run — this only
	// needs to be stable within a run. Bump it when internal/migrate's step list
	// or base-model set changes, purely as documentation of intent.
	schemaVersion = "6"
)

// container holds the process-wide testcontainers Postgres, started once by
// startContainer and reused by every New call. It is torn down by Ryuk when the
// test process exits (never per test — the container must outlive every clone).
var (
	containerOnce sync.Once
	baseConfig    pgtestdb.Config
	containerErr  error
	container     *tcpostgres.PostgresContainer //nolint:unused // kept alive for the process lifetime
)

// New returns a fresh, fully-migrated, isolated *gorm.DB for the test, dropped
// on t.Cleanup. It skips under -short (fast lane) and where Docker is
// unavailable, so it is safe to leave in the default build.
func New(t testing.TB) *gorm.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("dbtest: skipped under -short (run `make test-db`)")
	}
	// Degrade gracefully where Docker isn't running: skip rather than fail, so
	// only genuine container/migration errors below are fatal.
	if tt, ok := t.(*testing.T); ok {
		testcontainers.SkipIfProviderIsNotHealthy(tt)
	}

	base := ensureContainer(t)

	// Clone a pristine per-test database off the migrated template. pgtestdb
	// registers its own t.Cleanup to drop the clone once the test passes.
	cfg := pgtestdb.Custom(t, base, &runAllMigrator{})

	db, err := gorm.Open(gormpostgres.Open(cfg.URL()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		// Match production (database.Open) so paths that use FOR UPDATE SKIP
		// LOCKED / manual Begin/Commit test truthfully — without this, gorm wraps
		// every write in an implicit transaction that prod does not have.
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("dbtest: open gorm on clone %s: %v", cfg.Database, err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// ensureContainer starts the throwaway Postgres exactly once per process and
// returns the pgtestdb base config pointing at it. A start failure is fatal
// (Docker was already confirmed healthy by New's skip check).
func ensureContainer(t testing.TB) pgtestdb.Config {
	t.Helper()
	containerOnce.Do(startContainer)
	if containerErr != nil {
		t.Fatalf("dbtest: start Postgres container: %v", containerErr)
	}
	return baseConfig
}

// startContainer boots the testcontainers Postgres and populates baseConfig /
// containerErr. Run once via containerOnce.
func startContainer() {
	ctx := context.Background()
	c, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase(baseDB),
		tcpostgres.WithUsername(baseUser),
		tcpostgres.WithPassword(basePassword),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		containerErr = err
		return
	}
	host, err := c.Host(ctx)
	if err != nil {
		containerErr = fmt.Errorf("container host: %w", err)
		return
	}
	port, err := c.MappedPort(ctx, "5432/tcp")
	if err != nil {
		containerErr = fmt.Errorf("container port: %w", err)
		return
	}
	container = c
	baseConfig = pgtestdb.Config{
		DriverName: "pgx",
		Host:       host,
		Port:       port.Port(),
		User:       baseUser,
		Password:   basePassword,
		Database:   baseDB,
		Options:    "sslmode=disable",
		// Own and connect to the template/clones as the container superuser, so
		// migrations run with full privileges (matching the local dev DB role).
		TestRole: &pgtestdb.Role{
			Username:     baseUser,
			Password:     basePassword,
			Capabilities: "",
		},
	}
}

// runAllMigrator builds the template database with the real production migrator,
// so every clone has the exact production schema.
type runAllMigrator struct{}

// Migrate provisions the template exactly as the app provisions a fresh DB at
// boot: AutoMigrate the base models, self-heal grants, then run every ordered
// migration via migrate.RunAll. pgtestdb calls this once, on the empty
// template database it just created.
func (runAllMigrator) Migrate(ctx context.Context, sqlDB *sql.DB, _ pgtestdb.Config) error {
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return fmt.Errorf("open gorm on template: %w", err)
	}
	if err := db.AutoMigrate(migrate.BaseModels()...); err != nil {
		return fmt.Errorf("auto-migrate base models: %w", err)
	}
	_ = migrate.RunBootstrapGrants(ctx, db) // non-fatal self-heal, as in main
	// Zero key matches config's default CREDENTIAL_ENCRYPTION_KEY (32 zero bytes).
	if err := migrate.RunAll(ctx, db, migrationTier, make([]byte, 32)); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// Hash keys pgtestdb's template cache. See schemaVersion for why a stable
// string (not a content hash) is sufficient here.
func (runAllMigrator) Hash() (string, error) {
	return "aep-api-schema-v" + schemaVersion, nil
}
