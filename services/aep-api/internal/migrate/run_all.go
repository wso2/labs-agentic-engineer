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
	"github.com/wso2/aep/aep-api/internal/identity"
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
		// How the org's agents run: the coding agent's runtime. A plain settings
		// table with a text primary key and nothing to encrypt, so AutoMigrate
		// expresses the whole schema; phase17 moves the rows of its
		// predecessor, org_coding_agent_settings.
		&organization.OrgAgentSettings{},
		&delivery.Execution{},
		&spec.AgentTurn{},
		&modelcost.ModelRate{},
		&delivery.MilestoneRun{},
		&delivery.RunCycle{},
		&delivery.AgentUsageLedgerEntry{},
		// The platform's record of the SHARED directory objects it created at
		// build time: the roles a design declares, the test users that exercise
		// them, and the per-project references that join the two. Plain tables
		// with plain indexes, so AutoMigrate expresses the whole schema.
		&identity.IdPRole{},
		&identity.TestUser{},
		&identity.TestUserRef{},
		// The project-OWNED half of the same record: the OAuth resource server
		// one project's build created, and that project's own roles with the
		// groups they were assigned to. Plain tables with composite primary keys
		// and one extra unique index, all of it expressible as struct tags, so
		// these need no Step either.
		//
		// There is deliberately NO data migration behind them. Nothing wrote a
		// predecessor, and the rows are a cache of directory objects a rebuild
		// recreates: an existing deployment gets two empty tables and the next
		// build fills them.
		&identity.IdPResourceServer{},
		&identity.IdPRoleBinding{},
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
		// org_secrets in its legacy (key, value) shape; phase26, last, makes
		// it reference rows only, on fresh and upgraded databases alike.
		ctxStep("org_secrets", RunOrgSecretsMigration),
		// The reference columns of org_secrets, named 23 by phase but ordered
		// here.
		ctxStep("phase23_org_secret_refs", RunPhase23OrgSecretRefs),
		ctxStep("per_org_secret_name", RunPerOrgSecretName),
		ctxStep("org_anthropic_credentials", RunOrgAnthropicCredentialsMigration),
		ctxStep("phase3_thunder_org_uuid", RunPhase3ThunderOrgUUID),
		ctxStep("phase3_coding_agent_logs", RunPhase3CodingAgentLogs),
		// GitRepository table from the model tag (creates the new composite index).
		dbStep("automigrate_git_repository", func(db *gorm.DB) error { return db.AutoMigrate(&sourcecontrol.GitRepository{}) }),
		// Composite (org_id, project_id) unique — must run AFTER AutoMigrate,
		// which creates the new index from the tag but never drops the old one.
		ctxStep("git_repositories_composite_unique", RunGitRepoCompositeUnique),
		dbStep("phase7_skills", RunPhase7Skills),
		// Executions table (AutoMigrated from the model) gains its partial
		// admission-mutex unique index, which AutoMigrate cannot express.
		ctxStep("executions", RunExecutions),
		// agent_turns table (AutoMigrated from the model) gains its
		// newest-turn index, which AutoMigrate cannot express.
		ctxStep("agent_turns", RunAgentTurns),
		// tasks-github-native cutover: drop component_tasks + the
		// git_repositories.github_project_id cache column (both AutoMigrate-only,
		// now gone). Runs LAST — after every legacy component_tasks migration and
		// the git_repositories AutoMigrate. Idempotent (IF EXISTS).
		dbStep("tasks_github_native", RunTasksGitHubNative),
		// dependency-management (§3.6): the external-resource catalog +
		// cross-project access-request tables. Two idempotent CREATE TABLEs only —
		// no component_tasks ALTER (dependency gating lives on `provision` GitHub
		// issues + the funnel depsGate, not DB columns).
		ctxStep("phase9_dependency_mgmt", RunPhase9DependencyMgmt),
		// workflow_runs: the retired devflow lookup index. Its model is gone and
		// nothing creates the table any more, so on a fresh schema this step is a
		// no-op; it stays in the ordered list because the list is frozen and
		// because an existing deployment's abandoned table keeps its index.
		ctxStep("workflow_runs", RunWorkflowRuns),
		// coding_agent_logs (GitHub-native): legacy CREATE TABLE for execution-
		// keyed agent-log rows. New cycle logs are read from OpenChoreo + the
		// observer; nothing writes this table. Runs after `executions` (FK) and
		// `tasks_github_native`. Supersedes the phase3_coding_agent_logs no-op.
		ctxStep("coding_agent_logs", RunCodingAgentLogs),
		// rca_agent_reports (ops.RcaAgentReport): the store backing the
		// console's Alerts notification bell and Alerts list/stepper
		// (issues #154, #155, BE handshake #156). One idempotent CREATE TABLE
		// + its (org_id, created_at) list index.
		ctxStep("phase10_rca_agent_reports", RunPhase10RcaAgentReports),
		// milestone_runs (AutoMigrated from the model) gains the one-live-run-
		// per-milestone partial unique index, which AutoMigrate cannot express.
		// The per-project build mutex is milestone_run_kind's, below.
		ctxStep("milestone_runs", RunMilestoneRuns),
		// run_cycle_logs: RETIRED tombstone. Writers deleted (grill Q2); step
		// kept for frozen order and no longer creates the table (see
		// run_cycle_logs.go).
		ctxStep("run_cycle_logs", RunRunCycleLogs),
		// agent_usage_ledger: the spend record that outlives the project. Its
		// upsert arbiter index, plus the one-time backfill from the dispatch rows
		// spend used to live on — so it must follow both of those tables
		// (AutoMigrate) and the milestone_runs it joins for the version label.
		ctxStep("agent_usage_ledger", RunAgentUsageLedger),
		// model_rates seed (#291): the platform's active model at today's
		// rates. AutoMigrate (BaseModels) creates the table; this idempotent
		// step inserts the claude-sonnet-5 row so write-time USD stamping has
		// a price card to resolve against. Ops-managed thereafter.
		ctxStep("model_rates_seed", RunModelRatesSeed),
		// secret_ref_* triplet columns: RETIRED tombstone (phase26 drops the
		// triplet; see phase11_secret_ref_columns.go).
		ctxStep("phase11_secret_ref_columns", RunPhase11SecretRefColumns),
		// Seal publisher_client_secret + webhook_secrets in place: RETIRED
		// tombstone (phase26 drops both columns).
		ctxStep("phase12_encrypt_credential_columns", RunPhase12EncryptCredentialColumns),
		// Re-key org_anthropic_credentials to (oc_org_id, role) so an org can
		// hold a coding credential beside its default key (ADR-0016; since
		// phase16 that credential is a Claude subscription only, ADR-0036).
		ctxStep("phase13_anthropic_credential_role", RunPhase13AnthropicCredentialRole),
		// project_conversations: RETIRED tombstone. The conversation store went
		// with aep-api's turn orchestration (phase 3); the step is kept for
		// frozen order and does nothing. phase24 drops the table.
		ctxStep("project_conversations", RunProjectConversations),
		// Drop leftover sm_api_* columns.
		ctxStep("phase14_drop_sm_api_columns", RunPhase14DropSMAPIColumns),
		// milestone_runs.kind: backfill the kind from the origin, then move the
		// per-project build mutex onto it. Ordered AFTER the milestone_runs step
		// that owns the per-milestone index, and last overall because the list is
		// append-only. The backfill MUST precede the index creation — see
		// milestone_run_kind.go.
		ctxStep("milestone_run_kind", RunMilestoneRunKind),
		// The identity tables move from one cluster-wide directory to one per
		// (org, environment): new key columns, composite primary keys, and the
		// platform-IdP era's rows discarded because they name directory objects
		// on an instance builds no longer provision to. Ordered LAST because the
		// list is append-only; it depends only on the AutoMigrate above it, which
		// is where those three tables come from (BaseModels).
		ctxStep("phase15_identity_per_environment", RunPhase15IdentityPerEnvironment),
		// The coding role holds a Claude subscription token only (ADR-0036):
		// separate coding API keys and their bytes are deleted and the CHECK
		// becomes default ⇔ api_key, coding ⇔ oauth_token. Follows phase13,
		// which created the role/credential_kind columns and the CHECK this
		// replaces.
		ctxStep("phase16_coding_role_subscription_only", RunPhase16CodingRoleSubscriptionOnly),
		// org_coding_agent_settings → org_agent_settings: copy the rows into the
		// table AutoMigrate created, then drop the old one.
		ctxStep("phase17_org_agent_settings", RunPhase17OrgAgentSettings),
		// Usage becomes host-aware: model_rates is re-keyed to (host, model_id)
		// and turns, cycles, executions and the ledger gain model_host, every
		// existing row backfilled to api.anthropic.com. Appended after
		// model_rates_seed, which is why the seed counts a NULL-host row as the
		// Anthropic one: on the upgrade boot it runs before the key is widened.
		ctxStep("phase18_model_host", RunPhase18ModelHost),
		// The model connection moves to its own table, org_model_connections:
		// every active default credential becomes a connection on Anthropic's
		// API with the org's model, the default rows go, the credential table
		// keeps only the Claude subscription, and org_agent_settings loses its
		// model column. Follows phase17 and every step that shaped
		// org_anthropic_credentials.
		ctxStep("phase19_model_connection", RunPhase19ModelConnection),
		// The connection key's sealed bytes moved from org_secrets
		// 'anthropic/key' to 'model/key': RETIRED tombstone (value rows are
		// gone, phase26).
		ctxStep("phase20_model_key_rename", RunPhase20ModelKeyRename),
		// The endpoint each governed ai-agent's key was stored beside, so the
		// govern stage can tell when a connection switch moved its base path.
		// A new table with no backfill; depends on nothing above it.
		ctxStep("phase21_ai_agent_model_endpoints", RunPhase21AIAgentModelEndpoints),
		// The activity feed is gone (no reader: the console's feed was deleted,
		// apps/console ADR-0022). AutoMigrate never drops a table, so this is
		// the explicit drop. Idempotent.
		dbStep("phase22_drop_activity_events", RunPhase22DropActivityEvents),
		// agent_turns becomes the finished-turn ledger (07 §12): turns run in
		// the org's AE Studio pod, so the in-process engine's rows get a kind
		// and a start time, its running rows, guard index and columns go, the
		// primary key widens to (org_id, id), and project_conversations is
		// dropped. Appended last because the list is append-only.
		ctxStep("phase24_agent_turns_ledger", RunPhase24AgentTurnsLedger),
		// The coding Component settle sweep's partial index over closed,
		// undeleted coding cycles, ordered least recently checked first.
		// settle_checked_at itself comes from AutoMigrate. Appended last because
		// the list is append-only.
		ctxStep("phase25_run_cycle_settling", RunPhase25RunCycleSettling),
		// Postgres holds secret reference names only: org_secrets becomes
		// (oc_org_id, secret, secret_ref_name, written_at), its ref-less value
		// rows go, and every value, preview and vault-path column of the
		// credential tables is dropped. Last, so every step above meets the
		// legacy shape on a first boot; each of them tolerates the converged
		// shape on the boots after. Appended last because the list is
		// append-only.
		ctxStep("phase26_secrets_refs_only", RunPhase26SecretsRefsOnly),
		// run_cycles gains the durable startup wait: why the current attempt's
		// pod is stuck before Running and since when, so the run view shows the
		// wait without a cluster read. Two plain columns, no backfill (a row
		// written before them has no wait to show). Appended last because the
		// list is append-only.
		ctxStep("phase27_run_cycle_startup_wait", RunPhase27RunCycleStartupWait),
	}
}

// RunAll applies every schema migration in dependency order. It is the single
// ordered list main used to inline as ~19 copy-pasted blocks (each with its own
// context/os.Exit); the ordering constraints that lived in the comments between
// those blocks are preserved on the steps in Steps. Fails fast on the first
// error, naming the offending step.
func RunAll(ctx context.Context, db *gorm.DB, deploymentTier string) error {
	return database.Run(ctx, db, Steps(db, deploymentTier))
}
