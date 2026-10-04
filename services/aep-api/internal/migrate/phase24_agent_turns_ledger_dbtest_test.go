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
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/migrate"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// engineColumns are the columns only aep-api's in-process turn engine wrote;
// phase24 drops them.
var engineColumns = []string{"heartbeat_at", "spec_tag", "paths", "commit_sha", "no_changes", "use_case"}

// rebuildEngineShape puts agent_turns back in the shape the in-process engine
// left it (its columns, the one-active guard, the primary key on id alone)
// and re-creates project_conversations, as an upgrading deployment has them.
func rebuildEngineShape(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		`ALTER TABLE agent_turns ADD COLUMN heartbeat_at timestamptz`,
		`ALTER TABLE agent_turns ADD COLUMN spec_tag text`,
		`ALTER TABLE agent_turns ADD COLUMN paths text`,
		`ALTER TABLE agent_turns ADD COLUMN commit_sha text`,
		`ALTER TABLE agent_turns ADD COLUMN no_changes boolean`,
		`ALTER TABLE agent_turns ADD COLUMN use_case text NOT NULL`,
		`ALTER TABLE agent_turns DROP CONSTRAINT agent_turns_pkey`,
		`ALTER TABLE agent_turns ADD PRIMARY KEY (id)`,
		`CREATE UNIQUE INDEX ux_agent_turns_active ON agent_turns (org_id, project_id) WHERE status = 'running'`,
		`CREATE TABLE project_conversations (id text PRIMARY KEY, org_id text NOT NULL, project_id text NOT NULL, use_case text NOT NULL, current boolean NOT NULL)`,
		`CREATE UNIQUE INDEX ux_project_conversations_current ON project_conversations (org_id, project_id, use_case) WHERE current`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("rebuild the engine's shape (%s): %v", stmt, err)
		}
	}
}

// engineRow is one agent_turns row as the in-process engine or the 3.16
// ledger wrote it before phase24.
type engineRow struct {
	id, org, project, useCase, kind, flow, summary, status string
	startedAt                                              *time.Time
}

func insertEngineRow(t *testing.T, db *gorm.DB, r engineRow, createdAt time.Time) {
	t.Helper()
	if err := db.Exec(`INSERT INTO agent_turns
		(id, org_id, project_id, conversation_id, use_case, kind, flow, summary, base_ref, status, heartbeat_at, started_at, created_at, updated_at)
		VALUES (?, ?, ?, 'c1', ?, ?, ?, ?, 'aaaa', ?, now(), ?, ?, ?)`,
		r.id, r.org, r.project, r.useCase, r.kind, r.flow, r.summary, r.status, r.startedAt, createdAt, createdAt).Error; err != nil {
		t.Fatalf("insert %s: %v", r.id, err)
	}
}

func turnFacts(t *testing.T, db *gorm.DB, org, id string) (found bool, kind, project string, startedAt time.Time) {
	t.Helper()
	var rows []struct {
		Kind      string
		ProjectID string
		StartedAt time.Time
	}
	if err := db.Raw(`SELECT kind, project_id, started_at FROM agent_turns WHERE org_id = ? AND id = ?`, org, id).Scan(&rows).Error; err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	if len(rows) == 0 {
		return false, "", "", time.Time{}
	}
	return true, rows[0].Kind, rows[0].ProjectID, rows[0].StartedAt
}

// The upgrade a deployment goes through: the engine's table, then the real
// boot path. Old rows get their kind from use_case (task-plan → plan; a
// `/start` turn → kickoff; else browser) and their start from created_at,
// running rows go, marketplace rows move to the empty project (""), the engine's columns,
// guard index and conversation table go, and the key widens to (org_id, id).
// A second boot changes nothing.
func TestPhase24AgentTurnsLedger_UpgradesTheEnginesTable(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()
	rebuildEngineShape(t, db)

	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	zero := time.Time{}
	podStart := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	ids := map[string]string{}
	for _, name := range []string{"plan", "kickoffFlow", "kickoffSummary", "design", "running", "marketplace", "ledgerKickoff", "ledgerTypedStart"} {
		ids[name] = uuid.NewString()
	}
	for _, r := range []engineRow{
		{id: ids["plan"], org: "acme", project: "shop", useCase: "task-plan", kind: "browser", status: "completed"},
		{id: ids["kickoffFlow"], org: "acme", project: "shop", useCase: "general", kind: "browser", flow: "start", summary: "/start\n\nAn expense tracker", status: "completed", startedAt: &zero},
		{id: ids["kickoffSummary"], org: "acme", project: "web", useCase: "general", kind: "browser", summary: "/start An expense tracker", status: "failed"},
		{id: ids["design"], org: "acme", project: "shop", useCase: "general", kind: "browser", flow: "design", status: "completed"},
		{id: ids["running"], org: "acme", project: "billing", useCase: "general", kind: "browser", status: "running"},
		{id: ids["marketplace"], org: "acme", project: "__marketplace_register__", useCase: "general", kind: "browser", status: "completed"},
		// Rows the 3.16 ledger wrote: their kind and start are the pod's.
		{id: ids["ledgerKickoff"], org: "acme", project: "web", useCase: "general", kind: "kickoff", flow: "start", status: "completed", startedAt: &podStart},
		{id: ids["ledgerTypedStart"], org: "acme", project: "shop", useCase: "general", kind: "browser", flow: "start", status: "completed", startedAt: &podStart},
	} {
		insertEngineRow(t, db, r, created)
	}

	bootMigrate(t, db)
	bootMigrate(t, db) // idempotent

	for name, want := range map[string]string{
		"plan": "plan", "kickoffFlow": "kickoff", "kickoffSummary": "kickoff", "design": "browser",
		"marketplace": "browser", "ledgerKickoff": "kickoff", "ledgerTypedStart": "browser",
	} {
		found, kind, _, _ := turnFacts(t, db, "acme", ids[name])
		if !found || kind != want {
			t.Errorf("%s: found=%v kind=%q, want %q", name, found, kind, want)
		}
	}
	for _, name := range []string{"plan", "kickoffFlow", "kickoffSummary", "design"} {
		if _, _, _, started := turnFacts(t, db, "acme", ids[name]); !started.Equal(created) {
			t.Errorf("%s: started_at = %v, want created_at %v", name, started, created)
		}
	}
	if _, _, _, started := turnFacts(t, db, "acme", ids["ledgerKickoff"]); !started.Equal(podStart) {
		t.Errorf("a ledger row's started_at = %v, want the pod's %v", started, podStart)
	}
	if found, _, _, _ := turnFacts(t, db, "acme", ids["running"]); found {
		t.Error("a running row survived")
	}
	if _, _, project, _ := turnFacts(t, db, "acme", ids["marketplace"]); project != "" {
		t.Errorf("marketplace row project = %q, want ''", project)
	}
	for _, col := range engineColumns {
		if columnExists(t, db, "agent_turns", col) {
			t.Errorf("agent_turns.%s survived", col)
		}
	}
	if hasIndex(t, db, "ux_agent_turns_active") {
		t.Error("ux_agent_turns_active survived")
	}
	if !hasIndex(t, db, "ix_agent_turns_project_newest") {
		t.Error("ix_agent_turns_project_newest is gone")
	}
	if got := primaryKeyColumns(t, db, "agent_turns"); got != "org_id,id" {
		t.Errorf("agent_turns primary key = (%s), want (org_id,id)", got)
	}
	var tables int64
	if err := db.Raw(`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name='project_conversations'`).Scan(&tables).Error; err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		t.Error("project_conversations survived")
	}

	// The widened key: another org's row with the same turn id no longer
	// stands in for this org's record.
	repo := spec.NewTurnRepository(db, nil)
	rec := spec.TurnRecord{TurnID: ids["design"], Project: "shop", Kind: spec.TurnKindBrowser, Status: "completed",
		BaseRef: "bbbb", StartedAt: podStart, FinishedAt: podStart.Add(time.Minute)}
	if err := repo.RecordFinished(ctx, "evil", []spec.TurnRecord{rec}); err != nil {
		t.Fatalf("record under another org: %v", err)
	}
	if found, _, _, _ := turnFacts(t, db, "evil", ids["design"]); !found {
		t.Error("another org's record with a known turn id was not stored")
	}
}

// A fresh schema is already the ledger: the step finds nothing to do.
func TestPhase24AgentTurnsLedger_FreshSchema(t *testing.T) {
	db := dbtest.New(t)
	if got := primaryKeyColumns(t, db, "agent_turns"); got != "org_id,id" {
		t.Fatalf("agent_turns primary key = (%s), want (org_id,id)", got)
	}
	for _, col := range engineColumns {
		if columnExists(t, db, "agent_turns", col) {
			t.Errorf("agent_turns.%s exists on a fresh schema", col)
		}
	}
	if hasIndex(t, db, "ux_agent_turns_active") {
		t.Error("ux_agent_turns_active exists on a fresh schema")
	}
	for i := range 2 {
		if err := migrate.RunPhase24AgentTurnsLedger(context.Background(), db); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
	}
}

// On a table already in the ledger's shape, phase24 runs on every boot and
// must take no lock that blocks the other replica's ledger writes or reads
// (R1-M2): no ACCESS EXCLUSIVE (DROP COLUMN, the key swap) and no full-table
// UPDATE/DELETE. A concurrent transaction holds SHARE on agent_turns, which
// conflicts with both; the re-run must still finish.
func TestPhase24AgentTurnsLedger_ARerunTakesNoTableLock(t *testing.T) {
	db := dbtest.New(t)
	ctx := context.Background()

	holder := db.Begin()
	if holder.Error != nil {
		t.Fatalf("begin: %v", holder.Error)
	}
	defer holder.Rollback()
	if err := holder.Exec(`LOCK TABLE agent_turns IN SHARE MODE`).Error; err != nil {
		t.Fatalf("hold SHARE: %v", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := migrate.RunPhase24AgentTurnsLedger(runCtx, db); err != nil {
		t.Fatalf("a re-run on the ledger's shape waited on a table lock: %v", err)
	}
}
