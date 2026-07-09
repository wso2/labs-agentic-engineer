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
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/models"
	"gorm.io/gorm"
)

// stepTimeout bounds each context-taking migration. The plain *gorm.DB
// migrations manage their own lifetimes and ignore it.
const stepTimeout = 30 * time.Second

// RunAll applies every schema migration in dependency order. It is the single
// ordered list main used to inline as ~19 copy-pasted blocks (each with its own
// context/os.Exit); the ordering constraints that lived in the comments between
// those blocks are preserved on the steps below. Fails fast on the first error,
// naming the offending step. RunBootstrapGrants is NOT part of this list — it is
// a non-fatal self-grant the caller runs before migrating.
func RunAll(ctx context.Context, db *gorm.DB, deploymentTier string) error {
	type step struct {
		name string
		run  func(context.Context) error
	}
	// dbStep: the migration takes only *gorm.DB and manages its own timeout.
	dbStep := func(name string, fn func(*gorm.DB) error) step {
		return step{name, func(context.Context) error { return fn(db) }}
	}
	// ctxStep: the migration takes a context; give it a per-step timeout.
	ctxStep := func(name string, fn func(context.Context, *gorm.DB) error) step {
		return step{name, func(ctx context.Context) error {
			c, cancel := context.WithTimeout(ctx, stepTimeout)
			defer cancel()
			return fn(c, db)
		}}
	}

	steps := []step{
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
		dbStep("automigrate_git_repository", func(db *gorm.DB) error { return db.AutoMigrate(&models.GitRepository{}) }),
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
		// rca_agent_reports (models.RcaAgentReport): the store backing the
		// console's Alerts notification bell and Alerts list/stepper
		// (issues #154, #155, BE handshake #156). One idempotent CREATE TABLE
		// + its (org_id, created_at) list index.
		ctxStep("phase10_rca_agent_reports", RunPhase10RcaAgentReports),
	}

	for _, s := range steps {
		if err := s.run(ctx); err != nil {
			return fmt.Errorf("%s migration: %w", s.name, err)
		}
		slog.Debug("migration applied", "name", s.name)
	}
	slog.Info("schema migrations applied", "count", len(steps))
	return nil
}
