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

//go:build dbtest

// Package dbtest is the DB-backed test harness (§8.0). It is compiled ONLY
// under the `dbtest` build tag, so the default `go test ./...` (and `make
// test`) never require a database — only `make test-db` (`go test -tags
// dbtest ./...`) exercises it.
//
// It reuses the deployments Postgres on :5433 (the doc's option 1; override
// with TEST_DATABASE_URL), applies schema with AutoMigrate, and isolates test
// data by namespacing rows (e.g. org handles prefixed "dbtest-") and deleting
// them via t.Cleanup — so it never truncates or collides with application
// data. If the database is unreachable, Open skips the test rather than
// failing, so `-tags dbtest` is a safe no-op on machines without the local
// stack.
package dbtest

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DefaultURL points at the deployments Postgres (docker-compose `postgres`
// service, host-mapped to :5433 with the asdlc/asdlc dev credentials).
const DefaultURL = "postgres://asdlc:asdlc@localhost:5433/asdlc?sslmode=disable"

// URL returns the test database DSN (TEST_DATABASE_URL or DefaultURL).
func URL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return DefaultURL
}

// Open returns a *gorm.DB connected to the test Postgres. It calls t.Skip when
// the database is unreachable so the dbtest suite degrades gracefully where no
// local stack is running (rather than failing the build).
func Open(t testing.TB) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.Open(URL()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("dbtest: Postgres not reachable at %s (%v); skipping (run `bash deployments/scripts/start.sh`)", URL(), err)
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Skipf("dbtest: cannot obtain sql.DB (%v); skipping", err)
		return nil
	}
	if err := sqlDB.Ping(); err != nil {
		t.Skipf("dbtest: ping failed (%v); skipping (run `bash deployments/scripts/start.sh`)", err)
		return nil
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

// Migrate AutoMigrates the given models into the test database. Idempotent —
// the tables typically already exist (the app created them), so this just
// reconciles columns/indexes for the schema under test.
func Migrate(t testing.TB, db *gorm.DB, models ...any) {
	t.Helper()
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("dbtest: AutoMigrate failed: %v", err)
	}
}

// CleanupRows registers a t.Cleanup that deletes the rows matched by the given
// gorm where-clause from the model's table. Use it to remove a test's
// namespaced fixtures (e.g. org_id LIKE 'dbtest-%') without touching
// application data. Runs both before the test body (immediate) and after
// (deferred) so a prior crashed run leaves no residue.
func CleanupRows(t testing.TB, db *gorm.DB, model any, query string, args ...any) {
	t.Helper()
	del := func() {
		if err := db.Unscoped().Where(query, args...).Delete(model).Error; err != nil {
			t.Logf("dbtest: cleanup delete failed (non-fatal): %v", err)
		}
	}
	del()
	t.Cleanup(del)
}
