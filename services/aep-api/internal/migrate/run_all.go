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

// Package migrate holds the ONE ordered schema-migration list and the base-model
// set it presupposes.
//
// It sits beside internal/edge as the second package permitted to import every
// domain: the list NAMES domain-owned steps and entities, which is exactly why
// it cannot live in the platform/database kernel — that would make the kernel
// domain-aware. The kernel owns the MECHANISM (Step, the adapters, the runner);
// this package owns WHICH steps exist and IN WHAT ORDER.
//
// The order is load-bearing and NOT obvious (phase2_pra runs before phase0; the
// raw-SQL schema steps must precede AutoMigrate so CHECK constraints and partial
// indexes win over GORM's struct-tag inference). TestStepOrderGolden pins the
// exact sequence. Never reorder to "tidy" it — as domains take ownership of their
// steps, move the registration call-site, never its position.
package migrate

import (
	"context"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/database"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/projects"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// BaseModels is the single source of truth for the AutoMigrate set that must
// exist before Steps runs its ALTERs — the tables those steps assume already
// exist. Both the production boot path (cmd/aep-api/main.go) and the dbtest
// template migrator build the schema from this one list, so the two can never
// drift. Tasks are GitHub issues now (no component_tasks table), and
// org_credentials lives in git-service — the BFF neither auto-migrates nor reads
// it locally.
//
// It lives here rather than in platform/database because it names domain
// entities, and the kernel stays domain-free.
func BaseModels() []any {
	return []any{
		&projects.ComponentConfig{},
		&sourcecontrol.WebhookDelivery{},
		&sourcecontrol.WebhookPayload{},
		&organization.Organization{},
		&delivery.Execution{},
		&spec.AgentTurn{},
		&delivery.DevflowRun{},
		&modelcost.ModelRate{},
		&projects.ActivityEvent{},
	}
}

// Steps returns every schema migration in dependency order. It is a pure builder
// — nothing runs until database.Run applies the list — so TestStepOrderGolden
// asserts the sequence without needing a database.
//
// RunBootstrapGrants is NOT part of this list: it is a non-fatal self-grant the
// caller runs before migrating.
func Steps(db *gorm.DB, deploymentTier string) []database.Step {
	// dbStep: the migration takes only *gorm.DB and manages its own timeout.
	dbStep := func(name string, fn func(*gorm.DB) error) database.Step {
		return database.DBStep(name, db, fn)
	}
	// ctxStep: the migration takes a context; give it a per-step timeout.
	ctxStep := func(name string, fn func(context.Context, *gorm.DB) error) database.Step {
		return database.CtxStep(name, db, fn)
	}

	return []database.Step{
		// Dev-tier destructive migrations (refuse unless tier=dev) + column adds.
		dbStep("phase2_pra", func(db *gorm.DB) error { return RunPhase2PRA(db, deploymentTier) }),
		dbStep("phase0", func(db *gorm.DB) error { return RunPhase0(db, deploymentTier) }),
		dbStep("phase2_prd", RunPhase2PRD),
		dbStep("phase3_tech_lead", RunPhase3TechLead),
		dbStep("phase4_coding_agent", RunPhase4CodingAgent),
		dbStep("phase5_deploy_gating", RunPhase5DeployGating),
		dbStep("phase6_api_platform_idp", RunPhase6APIPlatformIDP),
		// Git-service schema migrations. Must run BEFORE AutoMigrate so raw-SQL
		// CHECK constraints + partial indexes win over GORM struct-tag inference.
		ctxStep("phase2_pra_schema", RunPhase2PRASchema),
		ctxStep("phase2_prc", RunPhase2PRC),
		ctxStep("org_secrets", RunOrgSecretsMigration),
		ctxStep("per_org_secret_name", RunPerOrgSecretName),
		ctxStep("org_anthropic_credentials", RunOrgAnthropicCredentialsMigration),
		ctxStep("phase3_sm_api_columns", RunPhase3SMAPIColumns),
		ctxStep("phase3_thunder_org_uuid", RunPhase3ThunderOrgUUID),
		ctxStep("phase3_coding_agent_logs", RunPhase3CodingAgentLogs),
		// GitRepository table from the model tag (creates the new composite index).
		dbStep("automigrate_git_repository", func(db *gorm.DB) error { return db.AutoMigrate(&sourcecontrol.GitRepository{}) }),
		// Composite (org_id, project_id) unique — must run AFTER AutoMigrate,
		// which creates the new index from the tag but never drops the old one.
		ctxStep("git_repositories_composite_unique", RunGitRepoCompositeUnique),
		dbStep("phase7_skills", RunPhase7Skills),
		ctxStep("phase8_idp_sm_api_columns", RunPhase8IDPSMAPIColumns),
		// Executions table (AutoMigrated from the model) gains its partial
		// admission-mutex unique index, which AutoMigrate cannot express.
		ctxStep("executions", RunExecutions),
		// agent_turns table (AutoMigrated from the model) gains the D18
		// one-active-turn-per-project partial unique index.
		ctxStep("agent_turns", RunAgentTurns),
		// tasks-github-native cutover: drop component_tasks + the
		// git_repositories.github_project_id cache column (both AutoMigrate-only,
		// now gone). Runs LAST — after every legacy component_tasks migration and
		// the git_repositories AutoMigrate. Idempotent (IF EXISTS).
		dbStep("tasks_github_native", RunTasksGitHubNative),
		// dependency-management (§3.6): the external-resource catalog +
		// cross-project access-request tables. Two idempotent CREATE TABLEs only —
		// no component_tasks ALTER (dependency gating lives on aep:provision GitHub
		// issues + the funnel depsGate, not DB columns).
		ctxStep("phase9_dependency_mgmt", RunPhase9DependencyMgmt),
		// workflow_runs (Temporal devflow lookup index, AutoMigrated from the
		// model) gains its one-running-task-per-issue partial unique index.
		ctxStep("workflow_runs", RunWorkflowRuns),
		// coding_agent_logs (GitHub-native): create the JobWatcher's final-log
		// sidecar keyed to executions(id). Runs after `executions` (FK target) and
		// `tasks_github_native` (cascade-drops any legacy component_tasks-keyed
		// table). Supersedes the guarded phase3_coding_agent_logs no-op above.
		ctxStep("coding_agent_logs", RunCodingAgentLogs),
		// rca_agent_reports (ops.RcaAgentReport): the store backing the
		// console's Alerts notification bell and Alerts list/stepper
		// (issues #154, #155, BE handshake #156). One idempotent CREATE TABLE
		// + its (org_id, created_at) list index.
		ctxStep("phase10_rca_agent_reports", RunPhase10RcaAgentReports),
		// model_rates seed (#291): the platform's active model at today's
		// rates. AutoMigrate (BaseModels) creates the table; this idempotent
		// step inserts the claude-sonnet-5 row so write-time USD stamping has
		// a price card to resolve against. Ops-managed thereafter.
		ctxStep("model_rates_seed", RunModelRatesSeed),
		// EXPAND: provider-neutral secret_ref_* columns alongside sm_api_*
		// (phase-03 item 14). CONTRACT (drop sm_api_*) waits for phase 09.
		ctxStep("phase11_secret_ref_columns", RunPhase11SecretRefColumns),
	}
}

// RunAll applies every schema migration in dependency order. It is the single
// ordered list main used to inline as ~19 copy-pasted blocks (each with its own
// context/os.Exit); the ordering constraints that lived in the comments between
// those blocks are preserved on the steps in Steps. Fails fast on the first
// error, naming the offending step.
func RunAll(ctx context.Context, db *gorm.DB, deploymentTier string) error {
	return database.Run(ctx, Steps(db, deploymentTier))
}
