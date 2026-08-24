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

package migrate_test

import (
	"context"
	"testing"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

// dropKindMutex puts the database back into the state an upgrading deployment is
// in the instant before the kind step runs: the column exists (AutoMigrate has
// already added it) but the kind-keyed mutex has not been created.
func dropKindMutex(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`DROP INDEX IF EXISTS ux_milestone_runs_dev_active_v3`).Error; err != nil {
		t.Fatalf("drop the kind mutex: %v", err)
	}
}

// seedRun inserts a run carrying an origin and the kind an upgrade would leave
// it holding — 'dev' for the AutoMigrate default (the real upgrade path: the
// column is added NOT NULL DEFAULT 'dev' so every pre-existing row reads 'dev'
// whatever it actually is), or the empty string for a row written some other
// way.
//
// Raw SQL for the same reason the ledger backfill's seed uses it: going through
// the repository would write the very kind the migration exists to derive, so
// the test would prove nothing.
func seedRun(t *testing.T, db *gorm.DB, org, project string, milestone int, origin, seededKind string) {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO milestone_runs (org_id, project_id, milestone_number, milestone_title,
		  tag, kind, origin, state, cycle_ceiling)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'waiting', 8)`,
		org, project, milestone, "v1", "v1", seededKind, origin).Error; err != nil {
		t.Fatalf("seed %s run: %v", origin, err)
	}
}

// kindOf reads the ONE kind every row of this origin now carries, failing when
// they disagree — the read has to be exhaustive or a backfill that touched the
// first row and missed the rest would pass.
func kindOf(t *testing.T, db *gorm.DB, origin string) string {
	t.Helper()
	var kinds []string
	if err := db.Raw(`SELECT DISTINCT kind FROM milestone_runs WHERE origin = ?`, origin).Scan(&kinds).Error; err != nil {
		t.Fatalf("read kind for %s: %v", origin, err)
	}
	if len(kinds) != 1 {
		t.Fatalf("origin %q carries kinds %v, want exactly one", origin, kinds)
	}
	return kinds[0]
}

// TestMilestoneRunKind_BackfillsEveryOriginRecordedBeforeTheColumn.
//
// Every deployment upgrading into the kind column already has runs, and the only
// record of what they are is their origin. A row left without a kind is invisible
// to every predicate the platform now writes on it: it is not the project's
// version, it never validates, and — worst — it is not in the build mutex.
func TestMilestoneRunKind_BackfillsEveryOriginRecordedBeforeTheColumn(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	dropKindMutex(t, db)
	// The AutoMigrate default: every pre-existing row reads 'dev' regardless of
	// what it is, so the two non-dev rows are MISLABELLED until the backfill
	// re-derives them. Correcting a wrong value is the case a fill-the-blanks
	// backfill would miss, and the one that would have put two task runs in the
	// project mutex.
	seedRun(t, db, "orga", "shop", 1, delivery.RunOriginSpecBuild, delivery.RunKindDev)
	seedRun(t, db, "orga", "shop", 2, delivery.RunOriginIncidentAdoption, delivery.RunKindDev)
	seedRun(t, db, "orga", "shop", 3, delivery.RunOriginRevalidate, delivery.RunKindDev)
	// And a row written with no kind at all — the other shape a pre-column row
	// can arrive in.
	seedRun(t, db, "orgb", "shop", 4, delivery.RunOriginRevalidate, "")

	if err := migrate.RunMilestoneRunKind(ctx, db); err != nil {
		t.Fatalf("RunMilestoneRunKind: %v", err)
	}

	// The mapping the whole split rests on, and the one delivery.RunKindForOrigin
	// mirrors in Go. A disagreement between the two would make a row read as one
	// kind in SQL and another in the loop.
	for origin, want := range map[string]string{
		delivery.RunOriginSpecBuild:        delivery.RunKindDev,
		delivery.RunOriginIncidentAdoption: delivery.RunKindTask,
		delivery.RunOriginRevalidate:       delivery.RunKindValidation,
	} {
		if got := kindOf(t, db, origin); got != want {
			t.Errorf("origin %q backfilled to kind %q, want %q", origin, got, want)
		}
		if got := delivery.RunKindForOrigin(origin); got != want {
			t.Errorf("RunKindForOrigin(%q) = %q, want %q — the Go twin disagrees with the SQL", origin, got, want)
		}
	}

	// Idempotent: a converged database is re-migrated on every boot.
	if err := migrate.RunMilestoneRunKind(ctx, db); err != nil {
		t.Fatalf("RunMilestoneRunKind (second run): %v", err)
	}
	if got := kindOf(t, db, delivery.RunOriginIncidentAdoption); got != delivery.RunKindTask {
		t.Errorf("kind after a re-run = %q, want task", got)
	}
}

// TestMilestoneRunKind_ColumnAddsToAPopulatedTable is the step BEFORE this
// migration, and the one that would brick an upgrade boot rather than corrupt an
// invariant quietly.
//
// kind is NOT NULL, and Postgres refuses ADD COLUMN … NOT NULL on a table that
// already has rows unless the column carries a DEFAULT. That default is the ONLY
// reason MilestoneRun declares one — it is never a semantic default, which is why
// the backfill immediately re-derives every row from its origin.
//
// Reproduced by dropping the column from a populated table and letting AutoMigrate
// add it back, which is exactly what an upgrading deployment does.
func TestMilestoneRunKind_ColumnAddsToAPopulatedTable(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	seedRun(t, db, "orga", "shop", 1, delivery.RunOriginIncidentAdoption, delivery.RunKindTask)
	// Back to a schema that never had the column. CASCADE takes the partial index
	// with it, which is what a real pre-kind database also lacks.
	if err := db.Exec(`ALTER TABLE milestone_runs DROP COLUMN kind CASCADE`).Error; err != nil {
		t.Fatalf("drop the kind column: %v", err)
	}

	if err := db.AutoMigrate(&delivery.MilestoneRun{}); err != nil {
		t.Fatalf("AutoMigrate could not add a NOT NULL kind to a populated table: %v", err)
	}
	if got := kindOf(t, db, delivery.RunOriginIncidentAdoption); got != delivery.RunKindDev {
		t.Fatalf("kind after AutoMigrate = %q, want the column default %q — every pre-existing row is mislabelled until the backfill",
			got, delivery.RunKindDev)
	}

	if err := migrate.RunMilestoneRunKind(ctx, db); err != nil {
		t.Fatalf("RunMilestoneRunKind: %v", err)
	}
	if got := kindOf(t, db, delivery.RunOriginIncidentAdoption); got != delivery.RunKindTask {
		t.Fatalf("kind after the backfill = %q, want task", got)
	}
	if !hasIndex(t, db, "ux_milestone_runs_dev_active_v3") {
		t.Error("the build mutex was not re-created after the column came back")
	}
}

// TestMilestoneRunKind_IndexIsCreatedAgainstPopulatedRows is the ORDERING, which
// is the whole reason this step is not two independent statements.
//
// A partial index created before the backfill matches nothing: every existing row
// still holds its AutoMigrate default, so `WHERE kind = 'dev'` selects an empty
// set. Nothing fails and nothing logs — the mutex is simply not enforced, and two
// concurrent build clicks are both admitted.
//
// So the assertion is not "the index exists" but "the index REFUSES", made
// against a row that was already in the table when the migration ran.
func TestMilestoneRunKind_IndexIsCreatedAgainstPopulatedRows(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	dropKindMutex(t, db)
	// A live run that predates the column, holding the AutoMigrate default.
	seedRun(t, db, "orga", "shop", 1, delivery.RunOriginSpecBuild, delivery.RunKindDev)

	if err := migrate.RunMilestoneRunKind(ctx, db); err != nil {
		t.Fatalf("RunMilestoneRunKind: %v", err)
	}

	// The next build click, arriving after the upgrade. It must lose to the run
	// that was already there.
	repo := delivery.NewMilestoneRunRepository(db)
	ok, row, err := repo.TryAdmit(ctx, &delivery.MilestoneRun{
		OrgID: "orga", ProjectID: "shop", MilestoneNumber: 2, MilestoneTitle: "v2",
		Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
	})
	if err != nil {
		t.Fatalf("TryAdmit: %v", err)
	}
	if ok || row != nil {
		t.Fatalf("a second dev run was admitted (%+v) over a backfilled live run — the index was created before the backfill and matches nothing", row)
	}

	// And the 409 read finds the backfilled row, so the endpoint and the index
	// agree about a project that upgraded mid-run.
	active, aerr := repo.ActiveDevRunByProject(ctx, "orga", "shop")
	if aerr != nil || active == nil || active.MilestoneNumber != 1 {
		t.Fatalf("ActiveDevRunByProject = (%+v, %v), want the backfilled run on milestone 1", active, aerr)
	}

	// The superseded origin-keyed index is gone; the per-milestone one is not.
	// That second index is what makes "one live run per milestone, of ANY kind"
	// true, and the kind split deliberately leaves it alone.
	if has := hasIndex(t, db, "ux_milestone_runs_spec_active_v2"); has {
		t.Error("the origin-keyed mutex survived — two indexes now claim the same invariant")
	}
	if has := hasIndex(t, db, "ux_milestone_runs_active_per_milestone_v1"); !has {
		t.Error("the per-milestone index was dropped — nothing now stops two runs on one milestone")
	}
}

func hasIndex(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT count(*) FROM pg_indexes WHERE indexname = ?`, name).Scan(&n).Error; err != nil {
		t.Fatalf("look up index %s: %v", name, err)
	}
	return n > 0
}
