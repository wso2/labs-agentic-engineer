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

// Package app is the composition root. Assemble wires the entire service graph
// (HTTP handler + background watchers) from config + a resolved Infra bundle +
// injectable Seam values, doing NO I/O of its own — deterministic and
// millisecond-fast, so an assembly test can build the same real graph with a
// faked Infra (app.Assemble(cfg, app.Fake(), Seam{})). Resolve (infra.go)
// performs every boot side effect — DB open + migrations, the OpenBao key
// loads, the dev seed, k8s in-cluster init, the workspace fsck — and hands
// Assemble the results. Public app.Run owns Resolve → Assemble → serve.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/build"
	"github.com/wso2/aep/aep-api/internal/delivery/codingagent"
	"github.com/wso2/aep/aep-api/internal/delivery/eventcore"
	"github.com/wso2/aep/aep-api/internal/delivery/execution"
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
	"github.com/wso2/aep/aep-api/internal/delivery/run"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	dephttpapi "github.com/wso2/aep/aep-api/internal/dependencies/httpapi"
	"github.com/wso2/aep/aep-api/internal/dependencies/mcpdiscovery"
	"github.com/wso2/aep/aep-api/internal/dependencies/provisioning"
	"github.com/wso2/aep/aep-api/internal/dependencies/runtimeconfig"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/ops"
	opshttpapi "github.com/wso2/aep/aep-api/internal/ops/httpapi"
	"github.com/wso2/aep/aep-api/internal/organization"
	orghttpapi "github.com/wso2/aep/aep-api/internal/organization/httpapi"
	authn "github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/reaper"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/projects"
	projectshttpapi "github.com/wso2/aep/aep-api/internal/projects/httpapi"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	githubclient "github.com/wso2/aep/aep-api/internal/sourcecontrol/githubhost"
	schttpapi "github.com/wso2/aep/aep-api/internal/sourcecontrol/httpapi"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
	"github.com/wso2/aep/aep-api/internal/spec"
	spechttpapi "github.com/wso2/aep/aep-api/internal/spec/httpapi"
	"github.com/wso2/aep/aep-api/ocauth"
)

// Watcher is a long-running background loop. Every watcher blocks on its
// context and returns nothing; main starts each in its own goroutine and
// cancels the shared context on shutdown.
type Watcher interface {
	Run(ctx context.Context)
}

// App is the assembled service graph: the HTTP handler plus the background
// watchers to launch. Boot side effects (migrations, OpenBao, seed) already ran
// in Resolve; App holds only what main needs to serve and to shut down, plus the
// degradation report of which optional capabilities are off.
type App struct {
	Handler      http.Handler
	Watchers     []Watcher
	degradations []Degradation
}

// Seam carries injectable composition-root dependencies into Assemble.
// Every field's nil value is a feature off-switch: never panic, never silently
// degrade into a different credential path. Callers (public app.Run / Options)
// construct these; Assemble does not.
type Seam struct {
	// AuthProvider attaches a bearer on AuthModeServiceM2M OC (and CGW) calls.
	// Nil = no bearer attached (feature off).
	AuthProvider ocauth.AuthProvider

	// RequestAuthStrategy decides credential class per OC request.
	// Nil = all-M2M / never pass-through (openchoreo transport default).
	RequestAuthStrategy ocauth.RequestAuthStrategy

	// ImpersonateOrgResolver sets X-Impersonate-Org on M2M OC calls.
	// Nil = no impersonation header.
	ImpersonateOrgResolver func(ctx context.Context, namespace string) (string, error)

	// SecretsProvider is the write-only secrets delivery channel.
	// Nil = delivery off (no secret writes, no external-secret cleanup).
	SecretsProvider secretmanagersvc.Provider
}

// Assemble wires the entire service graph from config + a resolved Infra and
// returns the HTTP handler + background watchers. It performs NO I/O and NO
// process-lifecycle work (no os.Exit, no signals, no network, no clock, no
// filesystem) — every dependency that needed boot-time I/O arrives pre-resolved
// in `in` (see Resolve) — so an assembly test builds the same real graph in
// milliseconds with Fake(). AuthProvider / RequestAuthStrategy /
// ImpersonateOrgResolver / SecretsProvider arrive via seam (nil = off).
// Wiring order is load-bearing: several constructors read the value a prior
// one produced; the comments call out the couplings.
func Assemble(cfg config.Config, in Infra, seam Seam) (*App, error) {
	var err error
	db := in.DB
	credStore := in.CredentialStore
	minter := in.Minter
	appClientSecret := in.AppClientSecret
	workspaceEngine := in.Workspace

	// Skills are repo-backed now (one private org-skills repo per org —
	// docs/design/skills-repo-storage.md). The store needs gitOpsService +
	// repoService, so it is constructed below once those exist. No startup
	// bootstrap: built-ins seed/reconcile into each org's repo on demand.

	// Repositories
	executionRepo := delivery.NewExecutionRepository(db, in.RateStamper)
	configRepo := projects.NewConfigRepository(db)
	repoRepo := sourcecontrol.NewRepoRepository(db)
	milestoneRunRepo := delivery.NewMilestoneRunRepository(db)
	runCycleRepo := delivery.NewRunCycleRepository(db, in.RateStamper)
	// The agent-usage ledger: the read + retire half of delivery's spend record.
	// The WRITE half needs no wiring — each capture repository mirrors into it as
	// part of its own stamp, so there is no way to run one without the other.
	usageLedgerRepo := delivery.NewAgentUsageLedgerRepository(db)
	orgRepo := organization.NewOrganizationRepository(db)
	orgCredRepo := organization.NewOrgCredentialRepository(db, in.ColumnCipher)
	orgAnthropicRepo := organization.NewOrgAnthropicRepository(db)
	idpRepo := organization.NewIDPRepository(db, in.ColumnCipher)
	codingAgentLogRepo := delivery.NewCodingAgentLogRepository(db)
	activityRepo := projects.NewActivityEventRepository(db)
	activityHub := projects.NewActivityHub()
	activitySvc := projects.NewActivityService(activityRepo, activityHub)
	// Shared by the turn + files recorders: a room-scoped turn marks the project
	// agent-authored; the committer's later files/apply flush claims the mark and
	// suppresses its user line (issue #239 — see specAuthorship).
	specAuthored := &specAuthorship{}

	// Temporal runtime for the milestone run supervisor. Constructed always, but
	// connects lazily in the worker watcher's retry loop (never on the build
	// click), so aep-api boots and serves everything else when Temporal is down.
	// Its worker watcher is appended to the watcher slice below only when
	// Temporal is configured.
	temporalRuntime := delivery.NewRuntime(cfg.Temporal)

	if seam.AuthProvider != nil {
		slog.Info("Service auth configured via seam AuthProvider")
	}

	// OpenChoreo clients. Each one resolves the OC namespace as the OC
	// org handle directly (== ouHandle); there is no override map. Migrated
	// clients (namespace, project) take an openchoreo.Config; the still-hand-
	// rolled clients (component, secretref) keep the legacy positional args
	// until they migrate too. AuthProvider / strategy / impersonation resolver
	// arrive via seam — Assemble does not construct them.
	ocConfig := openchoreo.Config{
		BaseURL:                cfg.PlatformAPI.BaseURL,
		HostHeader:             cfg.PlatformAPI.HostHeader,
		AuthProvider:           seam.AuthProvider,
		RequestAuthStrategy:    seam.RequestAuthStrategy,
		ImpersonateOrgResolver: seam.ImpersonateOrgResolver,
		// A plane whose gateway does not terminate TLS serves only plain http,
		// while OpenChoreo advertises an https URL beside it regardless. See
		// Config.PreferPlainHTTPEndpoints.
		PreferPlainHTTPEndpoints: !cfg.PlatformAPI.DataPlaneGatewayTLS,
	}
	projectClient := openchoreo.NewProjectClient(ocConfig)
	namespaceClient := openchoreo.NewNamespaceClient(ocConfig)
	componentClient := openchoreo.NewComponentClient(ocConfig)
	// GitSecret client lands the per-org build git credential on the workflow
	// plane (via OC → OpenBao → SecretReference). Used by BuildCredentialsService
	// for both cloud (CP/WP split) and local k3d — one unified path.
	gitSecretClient := openchoreo.NewGitSecretClient(ocConfig)
	// The runtime reader: a release binding's rendered pods, their logs and
	// their events. It is what makes a coding cycle observable without a
	// Kubernetes client — status from the pod, live logs from the pod, and
	// (through the observer below) history for as long as the component lives.
	runtimeClient := openchoreo.NewRuntimeClient(ocConfig)

	// Observability client (optional — build logs disabled when URL not set)
	var observClient observability.Client
	if cfg.Observability.BaseURL != "" {
		observClient = observability.NewClient(cfg.Observability.BaseURL)
		slog.Info("Observability API", "baseURL", cfg.Observability.BaseURL)
	}

	// Secrets delivery client. Built only when seam.SecretsProvider is set
	// (OSS OpenBao-direct or overlay-injected provider). Nil provider =
	// delivery off — no construction from URL, no plaintext substitute.
	var smClient secretmanagersvc.SecretManagementClient
	if provider := seam.SecretsProvider; provider != nil {
		cfgStore := &secretmanagersvc.StoreConfig{Provider: "injected"}
		if ob := deliveryOpenBaoConfigFromAppConfig(cfg); ob != nil {
			cfgStore.OpenBao = ob
		}
		var ocSR secretmanagersvc.OpenChoreoSecretReferenceClient
		managesRefs := false
		if m, ok := provider.(secretmanagersvc.SecretReferenceManager); ok {
			managesRefs = m.ManagesSecretReferences()
		}
		if !managesRefs {
			ocSR = openchoreo.NewSecretReferenceClient(ocConfig)
		}
		smClient, err = secretmanagersvc.NewSecretManagementClientWithConfig(secretmanagersvc.SecretManagementClientConfig{
			StoreConfig: cfgStore,
			Provider:    provider,
			OCClient:    ocSR,
		})
		if err != nil {
			return nil, fmt.Errorf("secrets provider client init: %w", err)
		}
		slog.Info("secrets client", "provider", "injected", "managesRefs", managesRefs)
	} else {
		slog.Warn("secrets provider not injected — secrets delivery disabled")
	}
	_ = smClient // consumed via secretRefWriter below.

	// Secret-ref mirror writer. Constructed ahead of the credential / IDP service
	// constructors so all consumers can attach via WithSecretRefWriter (the no-op
	// case when smClient is nil is fine).
	secretRefWriter := organization.NewSecretRefWriter(smClient, orgCredRepo, orgAnthropicRepo, idpRepo)

	// Credentials + git-service services and controllers. The credential store,
	// the App-token minter (post OpenBao key-load / dev seed / bot-identity load),
	// and the App OAuth client_secret are all resolved in Resolve and arrive via
	// Infra — Assemble does no OpenBao/network I/O.
	credResolver := secrets.NewOrgResolver(db, credStore, minter)

	// One git host, selected by GIT_PROVIDER, threaded into every gitrepo
	// domain service where it narrows to that service's capability port.
	gitHost, err := buildGitHost(cfg)
	if err != nil {
		return nil, err
	}

	// Workspace engine (resolved in Resolve, arrives via Infra) — the disk-backed
	// git plumbing over the shared /workspaces mount. It backs the disk-lifecycle
	// pieces (the two best-effort trash hooks below + the reaper watcher) and the
	// GitOpsService Workspace port.
	//
	// Both hooks are phase 1 of the two-phase delete (O(1) rename into
	// trash/) and best-effort by contract: failures are logged, never
	// surfaced — the reaper's orphan pass is the correctness backstop.
	trashWorkspaceRepo := func(ctx context.Context, orgID, projectID, repoSlug string) {
		if err := workspaceEngine.TrashRepo(ctx, gitfs.RepoRef{OrgID: orgID, ProjectID: projectID, RepoSlug: repoSlug}); err != nil {
			slog.WarnContext(ctx, "workspace: trash repo subtree failed (reaper will reconcile)",
				"org", orgID, "project", projectID, "slug", repoSlug, "error", err)
		}
	}
	trashWorkspaceOrg := func(ctx context.Context, ocOrgID string) {
		if err := workspaceEngine.TrashOrg(ctx, ocOrgID); err != nil {
			slog.WarnContext(ctx, "workspace: trash org subtree failed (reaper will reconcile)",
				"ocOrgId", ocOrgID, "error", err)
		}
	}

	repoService := sourcecontrol.NewRepoService(repoRepo, gitHost, credResolver, cfg.GitHubRepoVisibility,
		sourcecontrol.WithWorkspaceTrash(trashWorkspaceRepo))
	gitOpsService := sourcecontrol.NewGitOpsService(credResolver, workspaceEngine)
	artifactSvcGit := spec.NewArtifactService(repoRepo, gitOpsService)
	issueService := sourcecontrol.NewIssueService(repoRepo, gitHost, credResolver)
	// THE delivery-side issue-write surface: every issue the delivery domain
	// mints, closes, reopens or labels goes through this one writer, so the
	// label vocabulary and the dedupe contract are decided once rather than once
	// per sub-package. Its slices (eventcore, task, validation, build) each hold
	// it; nothing else in delivery writes an issue.
	deliveryIssues := delivery.NewIssueWriter(issueService)
	webhookRegService := sourcecontrol.NewWebhookService(repoRepo, gitHost, repoService, issueService, cfg.WebhookDeliveryURL, cfg.WebhookHMACSecret)
	credRefreshService := organization.NewCredentialsRefreshService(credResolver)
	credService := organization.NewCredentialService(orgCredRepo, credStore, minter, cfg.WebhookHMACSecret, cfg.GitHubAppClientID, appClientSecret, gitHost)
	buildCredService := organization.NewBuildCredentialsService(repoRepo, credResolver, gitSecretClient)
	credService.WithBuildSecretCleaner(buildCredService)
	anthropicCredService := organization.NewAnthropicCredentialService(orgAnthropicRepo, credStore)

	// Task JWT manager — RS256. The public key is published on
	// /auth/external/jwks.json. Used to mint BFF MCP tokens
	// (IssueServiceToken) for the design agent and playground. Runner
	// callbacks do not verify Task JWTs.
	var taskTokens *authn.TaskTokenManager
	if cfg.TaskTokenSigningKey != "" {
		mgr, err := authn.NewTaskTokenManager(authn.TaskTokenConfig{
			PrivateKey: cfg.TaskTokenSigningKey,
			Issuer:     cfg.TaskTokenIssuer,
			Audience:   cfg.TaskTokenAudience,
			TTL:        24 * time.Hour,
		})
		if err != nil {
			return nil, fmt.Errorf("task token manager init: %w", err)
		}
		taskTokens = mgr
		slog.Info("Task token manager", "kid", mgr.KeyID(), "issuer", cfg.TaskTokenIssuer, "audience", cfg.TaskTokenAudience)
	} else {
		slog.Warn("BFF_TASK_SIGNING_KEY not set — MCP identity tokens and JWKS will be unavailable")
	}

	// Secret-ref mirror writer wired into both credential services. nil-safe via
	// the Enabled() check.
	credService.WithSecretRefWriter(secretRefWriter)
	anthropicCredService.WithSecretRefWriter(secretRefWriter)
	validatorProbes := organization.NewValidatorProbes(credService, gitHost, credResolver, minter)
	credValidator := secrets.NewValidator(db, validatorProbes, nil, cfg.CredentialValidatorInterval)

	// Artifact store — in-process via artifactSvcGit. Adds the
	// external-API catalog + the `DesignFile` YAML split/assemble layer
	// on top of raw file I/O.
	artifactStore := spec.NewArtifactStore(artifactSvcGit)

	// Repo-backed skills store (single source of truth = per-org org-skills
	// repo). Reads walk the shared-volume mirror at branch tip and writes
	// commit to main through the Workspace port
	// (services/aep-api/design/shared-workspace-volume.md). Built-ins + flow
	// skills seed/reconcile from the embedded files on demand.
	// docs/design/skills-repo-storage.md.
	skillSvc := spec.NewSkillService(gitOpsService, repoService, os.DirFS(cfg.SkillsDir))
	skillMutationSvc := spec.NewSkillMutationService(skillSvc)
	skillImportSvc := spec.NewSkillImportService(skillSvc)

	// File-mutation agents service (services/agents) — the requirements/design/
	// chat generation and task-planning flows. Plain HS256 M2M bearer; the
	// per-org Anthropic key is resolved by genai pre-stream and forwarded as
	// X-Anthropic-Key.
	agentsvcClient := agentsvc.New(agentsvc.Config{
		BaseURL:  cfg.AgentsSvc.BaseURL,
		Secret:   cfg.AgentsSvc.JWTSecret,
		Audience: cfg.AgentsSvc.JWTAudience,
		Issuer:   cfg.AgentsSvc.JWTIssuer,
	})

	// Files API — generic specs/-scoped, GitHub-at-HEAD reads + atomic apply
	// (commits straight to main under CAS retry). No local working tree.
	filesSvc := spec.NewFilesService(repoService, gitOpsService)

	// Unified genai committed-truth turn surface (shared-workspace-volume). It
	// resolves the org Anthropic key (no platform fallback), snapshots the
	// project repo + the org's _skills repo onto the workspace mount, and
	// runs turns detached behind the durable agent_turns guard. Skills are
	// NOT pushed inline anymore — agents reads the full catalog (embedded
	// flow skills seeded into _skills + org skills) from the SkillsRef
	// snapshot.
	anthropicKeyForGenAI := func(ctx context.Context, orgID string) (string, error) {
		res, err := anthropicCredService.EffectiveKey(ctx, orgID)
		if err != nil {
			return "", err
		}
		if res == nil || res.Source == "none" {
			return "", nil // no key → genai raises a pre-202 4xx
		}
		return res.Key, nil
	}
	// skillsRepoForTurns ensures the org's _skills repo exists (seeding the
	// embedded builtin/flow skills on first touch) and hands back its row —
	// the SkillsRef source for genai + task-plan turns. A closure at the
	// composition root so neither feature grows a skills edge.
	skillsRepoForTurns := func(ctx context.Context, orgID string) (*sourcecontrol.GitRepository, error) {
		if err := skillSvc.EnsureProvisioned(ctx, orgID); err != nil {
			return nil, err
		}
		return repoService.GetRepo(ctx, orgID, spec.SkillsRepoSentinelProjectID)
	}
	turnRepo := spec.NewTurnRepository(db, in.RateStamper)
	turnBroker := spec.NewTurnBroker()
	genaiDeps := spec.ServiceDeps{
		Repos:      repoService,
		Git:        gitOpsService,
		Keys:       anthropicKeyForGenAI,
		Client:     agentsvcClient,
		Turns:      turnRepo,
		Broker:     turnBroker,
		Snapshots:  workspaceEngine,
		SkillsRepo: skillsRepoForTurns,
		// #430: the project-scoped thread store — resolve/rotate the current
		// conversation, and the conversation_rotated admission fence on turns.
		Conversations: spec.NewConversationRepository(db),
		Recorder:      turnActivityRecorder{svc: activitySvc, authorship: specAuthored},
	}
	// MCP discovery on design-generation turns (dependency-management Phase 5):
	// the BFF mints a short-lived aud:aep-api-mcp token per turn so the agents
	// service can call back into /internal/v1/mcp. Wired only when the token
	// manager exists (a nil *TaskTokenManager would satisfy the interface but
	// panic on use) AND the internal base URL is configured; otherwise the
	// additive `mcp` field is simply omitted.
	if taskTokens != nil && cfg.AEPInternalBaseURL != "" {
		genaiDeps.MCPTokens = taskTokens
		genaiDeps.MCPBaseURL = cfg.AEPInternalBaseURL
	}
	genaiSvc := spec.NewService(genaiDeps)

	// Services. componentService is constructed before configService so
	// configService can call back into it to mirror env-var edits onto
	// the OC Component's workflow params.
	projectService := projects.NewProjectService(projectClient, repoService, webhookRegService, artifactSvcGit, executionRepo)
	// Build/deploy stage sources for the status poll (#184): the milestone-run
	// index (one row read) + the org-scoped release-binding list —
	// consumer-side ports wired here so projects imports neither.
	projectService.SetStageSources(projectRunRows{runs: milestoneRunRepo, cycles: runCycleRepo, ledger: usageLedgerRepo}, componentClient)
	organizationService := organization.NewOrganizationService(orgRepo, namespaceClient)
	// componentService takes repoSvc + buildCredSvc so TriggerBuild can
	// pre-stage the per-WorkflowRun build Secret in workflows-<orgID>
	// before the WorkflowRun is created (see
	// docs/design/build-credential-injection.md). buildCredService is never nil
	// (NewBuildCredentialsService always returns a value; its gitSecrets are
	// nil-safe internally), so the stager is always wired.
	buildStager := buildSecretStagerAdapter{svc: buildCredService}
	componentService := projects.NewComponentService(componentClient, observClient, artifactStore, repoService, buildStager)
	// deploymentService is built below, so the converger is attached after
	// construction — an env-var edit pushes onto the live binding through the one
	// writer rather than patching a field of it.
	configService := projects.NewConfigService(configRepo, nil)
	designService := spec.NewDesignService(artifactStore, artifactSvcGit)

	// Tasks are GitHub issues (the Task/Execution split, tasks-github-native):
	// the read + plan surface reads them live and fuses executions. The dispatch
	// half (funnel, coding executor, watchers) is wired below, after
	// asServiceIdentity. repoService/artifactStore/artifactSvcGit/gitOpsService
	// satisfy the task consumer ports directly.
	taskReads := task.NewReads(issueService, repoService, executionRepo, milestoneRunRepo)
	taskPlan := task.NewPlanService(repoService, artifactSvcGit, gitOpsService,
		anthropicKeyForGenAI, agentsvcClient, issueService, deliveryIssues, workspaceEngine,
		task.SkillsRepoResolver(skillsRepoForTurns))

	// Eagerly provision each org's skills repo on project creation.
	projectService.SetSkillsProvisioner(skillSvc)
	// Seed the new repo's .claude/skills copies (async + best-effort inside).
	projectService.SetSkillMirror(skillSvc)

	// Stamp specs/.agentic-engineer.toml into each new project's repo: the
	// Agentic Engineer marker, carrying the idea the user typed at create for
	// the /start flow to generate requirements from.
	projectService.SetDescriptorWriter(spec.NewDescriptorWriter(filesSvc))

	// The journey starts itself (#562): creation fires `/start` server-side,
	// so the user lands on a project whose agent is already interviewing them
	// instead of a dashboard asking them to press a button. Wired after the
	// descriptor writer above because that is the order the create path runs
	// them in — the turn reads the idea from the file that write commits.
	projectService.SetKickoffStarter(genaiSvc)
	// …and the status poll reports whether it is still running, which is the
	// one thing the git-derived spec fields cannot say.
	projectService.SetSpecTurnSource(turnRepo)
	// The build gate's staleness input (#575): the commit the newest successful
	// design run read the project at. A build whose requirements have moved
	// past its design is refused with the rest of the gate's conditions — the
	// one refusal that is about the design being WRONG rather than incomplete.
	artifactSvcGit.SetDesignBaselineResolver(func(ctx context.Context, orgID, projectID string) (string, error) {
		last, err := turnRepo.NewestCompletedFlow(ctx, orgID, projectID, "design")
		if err != nil || last == nil {
			return "", err
		}
		return last.BaseRef, nil
	})

	// The Task-keyed log endpoint (issue number → newest execution by default,
	// executionId query pins one for history browsing). (The runner skills-pull
	// S2S endpoint is retired — the runner now clones `org-skills` and resolves
	// applied skills locally, stamped via AEP_SKILLS_REPO_URL above.)
	execProgressSvc := execution.NewProgressService(executionRepo, componentClient)
	// One agent-log edge, two callers: the task-level progress endpoint and the
	// milestone run's per-cycle stream both read through this reader. Live logs
	// come from OpenChoreo; a finished cycle's come from the observability
	// plane while its component is retained; when neither can answer the reader
	// says so rather than serving an empty stream.
	agentProgressReader := codingagent.NewAgentProgressReader(
		codingagent.NewOCLogSource(runtimeClient), codingAgentLogRepo).
		WithArchive(codingagent.NewObserverArchive(observClient, runtimeClient))
	execProgressSvc.WithCodingProgress(agentProgressReader)
	// The task-log SSE stream: one connection per open task-detail page carries
	// the Task's whole live state (status + executions + unified timeline across
	// attempts). The hub is the in-proc change bus the PR webhook + job/exec
	// watchers ping so an attached stream re-derives instantly; a slow re-derive
	// tick on each connection is the safety net (no durable event table).
	taskStreamHub := delivery.NewTaskStreamHub()
	taskStreamSvc := execution.NewTaskStreamService(
		execProgressSvc,
		taskSnapshotAdapter{reads: taskReads},
		executionsByIssueAdapter{repos: repoRepo, execs: executionRepo},
		repoFullNameLookup{repos: repoRepo},
		taskStreamHub,
	)

	// The deployment service: single writer of a user component's ReleaseBinding.
	// It cuts the release from the Workload a build posted, composes the whole
	// desired binding — release pin, trait env configs, workload overrides — and
	// writes it once. Driven by the run supervisor's deploy stage, because
	// components carry AutoDeploy=false and nothing else promotes a release.
	deploymentService := projects.NewDeploymentService(componentClient, artifactStore)

	// Thunder admin client + IDP service. Reads
	// aep-system-client credentials from env (THUNDER_*) and exposes
	// EnsureOrgPublisher / RevokeOrgPublisher / RegenerateClientSecret
	// for per-org publisher OAuth app lifecycle. Optional — when the
	// Thunder base URL is empty the IDP service still runs and serves
	// GetProfile / GetOrCreateProfile, but mutating calls fail with
	// ErrIDPThunderUnavailable (non-fatal — protected components keep
	// deploying, just without per-org publishers).
	var thunderAdminClient thundersvc.Client
	thunderBase := cfg.ThunderAdmin.BaseURL
	if thunderBase == "" {
		// Fall back to the public Thunder URL the auth middleware
		// already trusts. setup-prerequisites and docker compose set
		// this to http://k3d-openchoreo-serverlb:8080 in-cluster /
		// http://thunder.openchoreo.localhost:8080 from the host.
		thunderBase = cfg.ServiceAuth.TokenURL
		// TokenURL contains /oauth2/token — strip back to the host:
		if idx := strings.Index(thunderBase, "/oauth2/"); idx > 0 {
			thunderBase = thunderBase[:idx]
		}
	}
	if cfg.ThunderAdmin.ClientID != "" && cfg.ThunderAdmin.ClientSecret != "" && thunderBase != "" {
		thunderAdminClient = thundersvc.New(thundersvc.Config{
			BaseURL:      thunderBase,
			ClientID:     cfg.ThunderAdmin.ClientID,
			ClientSecret: cfg.ThunderAdmin.ClientSecret,
		})
		slog.Info("Thunder admin client", "baseURL", thunderBase, "clientID", cfg.ThunderAdmin.ClientID)
	} else {
		slog.Warn("Thunder admin client disabled — set THUNDER_ADMIN_URL + THUNDER_SYSTEM_CLIENT_ID + THUNDER_SYSTEM_CLIENT_SECRET")
	}

	// Wire the Thunder OU validator into the org service so a stale/phantom JWT
	// `ouId` can't poison the org→OU mapping (the root cause behind the runner
	// publisher cc-token invalid_client: a phantom OU broke wc- namespace
	// derivation + forced a publisher re-registration under a non-existent OU →
	// Thunder 400 APP-1018). nil client → validation disabled (trust the JWT).
	if thunderAdminClient != nil {
		organizationService.SetOUValidator(thunderAdminClient)
		slog.Info("org OU validation wired — JWT ouId is validated against Thunder before the org→OU mapping is (over)written")
	}
	// WithSecretRefWriter mirrors per-org publisher client_secret to SM-API on
	// EnsureOrgPublisher / RegenerateClientSecret and on
	// ProvisionPublisherForBuild (POST /build, actor build-provision).
	// Coding dispatch reads secret_ref_name only and mounts PUBLISHER_CLIENT_ID
	// and PUBLISHER_CLIENT_SECRET from that SecretReference.
	idpService := organization.NewIDPService(idpRepo, orgRepo, thunderAdminClient, organization.PlatformIDPConfig{
		Issuer:  cfg.PlatformIDP.Issuer,
		JWKSURL: cfg.PlatformIDP.JWKSURL,
	}).WithSecretRefWriter(secretRefWriter)
	// Make idpService available to the deployment projection so a
	// first-protected-deploy provisions the org publisher app lazily.
	deploymentService.SetIDPService(idpService)

	// Connect-state JWT issuer (App-mode OAuth CSRF state). This HS256 signing
	// key only ever leaves the BFF as a JWT signature inside the GitHub OAuth
	// `state` query param. (Task JWTs use RS256 via taskTokens below.)
	bearerSvc := organization.NewBearerService(cfg.OAuthStateSigningKey, 24*time.Hour)
	if cfg.OAuthStateSigningKey == "" {
		slog.Warn("OAUTH_STATE_SIGNING_KEY not set — connect-state JWTs will fail to mint")
	}

	// taskTokens (the RS256 Task-JWT manager) is constructed earlier, before
	// the agents client — that client uses it to mint per-call outbound
	// identity tokens, so it must exist by then.

	// asServiceIdentity marks OC API calls made from inside dispatch, webhook
	// handlers, and the watchers as orchestration / async calls: they
	// authenticate with the BFF's M2M service identity and impersonate the
	// target org (via X-Impersonate-Org, derived from the request URL's
	// namespace) instead of forwarding the inbound user JWT. The OC client's
	// AuthProvider supplies the M2M token, so this only needs to set the marker.
	asServiceIdentity := func(ctx context.Context) context.Context {
		return authn.WithServiceIdentity(ctx)
	}

	// Webhook receiver wiring. The verifier's HMAC secrets come from the
	// per-org credential record (via git-service).
	secretProvider := webhook.NewGitServiceSecretProvider(credService, 30*time.Second)
	var routingLookup webhook.OcOrgIDLookup = credService
	webhookVerifier := webhook.NewVerifier(secretProvider).
		WithRefetchLimiter(webhook.NewRefetchLimiter(1, 5))
	routingCache := webhook.NewRoutingCache(60 * time.Second)
	deliveryStore := sourcecontrol.NewDeliveryStore(db)
	webhookRouter := webhook.NewRouter()

	// The coding executor: it launches one milestone cycle's agent Job for the
	// run supervisor (delivery.MilestoneDispatcher) and the post-merge build
	// re-try the exec watcher asks for.
	codingExecutor := codingagent.NewCodingExecutor(
		componentClient, repoService, identities{cred: credService},
		executionRepo,
		cfg.AgentPlatformURL, cfg.AgentPlatformURL,
		orgRepo, anthropicCredService, orgCredRepo, idpRepo)
	// Dispatch reads secret_ref_name only — it does not call
	// EnsureOrgPublisher. POST /build provisions the SecretReference while the
	// console JWT is still on ctx.
	codingExecutor.WithPublisherCredentials(
		codingagent.NewIDPPublisherResolver(idpRepo),
		codingagent.PublisherTokenURLFromJWKS(cfg.PlatformIDP.JWKSURL),
	)
	// The OpenChoreo Component dispatch path (phase 08): one Component per run
	// cycle in the milestone's own project, rendered by OC into the project's
	// dataplane namespace. It needs only the OC client and the runner image —
	// no proxy, no in-cluster Kubernetes client, no per-env branch — and it is
	// the only coding-agent dispatch path.
	//
	// Retention shares the same OC client: before each create it deletes the
	// project's oldest RETIRED agent components (liveness read from the cycle
	// rows), because a finished component still holds a billing concurrency
	// slot.
	if cfg.AgentRunnerImage != "" {
		retentionLimit := cfg.CodingAgentComponentRetention
		if retentionLimit <= 0 {
			retentionLimit = codingagent.DefaultCodingAgentComponentRetention
		}
		ocDispatcher := codingagent.NewOCDispatcher(componentClient).
			WithImage(cfg.AgentRunnerImage).
			WithRetention(codingagent.NewComponentRetention(
				componentClient, runCycleRepo, retentionLimit))
		codingExecutor.WithOCDispatch(ocDispatcher)
		slog.Info("coding executor: OpenChoreo component dispatch path enabled",
			"runnerImage", cfg.AgentRunnerImage,
			"componentRetention", retentionLimit)
	}
	// Build-secret staging so the post-merge build clones a PRIVATE project repo
	// (the local plane sets GITHUB_REPO_VISIBILITY=private). Reuses the same
	// per-org build GitSecret stager feature/component uses for manual builds.
	codingExecutor.WithBuildSecrets(buildStager, 0)
	slog.Info("coding executor: build-secret staging enabled (private-repo builds)")

	// The milestone RUN SUPERVISOR — a nil-safe concrete type, because the event
	// plane and the build click both hold it unconditionally and a degraded boot
	// (Temporal down) has to be a logged no-op rather than a nil check at each
	// call site.
	//
	// It is constructed HERE, after the coding executor, because the executor IS
	// its agent dispatcher (delivery.MilestoneDispatcher): a supervisor with no
	// dispatcher refuses to start runs, so the two must be wired together.
	runSupervisor := run.NewSupervisor(temporalRuntime, runRuns{runs: milestoneRunRepo}, codingExecutor)
	// The run-supervisor half of the project-delete cascade. Wired HERE rather
	// than beside SetStageSources because the supervisor does not exist until the
	// coding executor does: purging a project's run ROWS leaves the workflows that
	// write them running, polling a repository the same delete is about to remove.
	projectService.SetRunAbandoner(projectRunSupervision{runs: milestoneRunRepo, supervisor: runSupervisor})

	platformSender := githubBotLogin(cfg.GitHubAppSlug)
	registerWebhook := func(event, action string, h func(ctx context.Context, event, action string, payload []byte) error) {
		webhookRouter.Register(event, action, webhook.EventHandlerFunc(h))
	}
	// The event plane: THE webhook half of the milestone loop. Every handler
	// resolves a milestone RUN ROW first and returns without a write when there
	// is none, so a project with no live run costs nothing.
	eventPlane := eventcore.New(eventcore.Ports{
		Runs:   eventcoreRuns{runs: milestoneRunRepo},
		Cycles: eventcoreCycles{cycles: runCycleRepo},
		Issues: issueService,
		Writer: deliveryIssues,
		PRs:    issueService,
		Merger: issueService,
		Repos:  repoLocator{db: db},
		Design: designComponents{store: artifactStore},
		Builds: eventcoreBuilds{oc: componentClient, repos: repoRepo, stager: buildStager},
		// The wiring-conformance check on the merged-PR fan-out: does what shipped
		// consume the resources the design declares?
		Workloads: workloadReader{files: filesSvc},
		// The revalidate trigger's last guard: refuse a version with no oracle
		// rather than starting a run that could only conclude `skipped`.
		Criteria: validationCriteria{files: filesSvc},
		Signaler: runSupervisor,
		Starter:  runSupervisor,
		// A first-ever component has no OpenChoreo Component CR, and a merged
		// PR's build would fail "Component not found" — so the fan-out ensures
		// the CR from the design facts immediately before it triggers.
		// Components is SET below, once the runtime-config emitter it composes
		// with exists (SetComponentEnsurer).
		// Echo suppression (issues.* only) uses the same platform identity the
		// task handlers do.
		PlatformSender: platformSender,
	})
	eventPlane.RegisterHandlers(registerWebhook)
	// The SRE/RCA handoff's dispatch leg (promote-task-from-issue): adopt a
	// freshly filed issue into the deployed version's milestone and start an
	// incident run over it.
	taskCommands := task.NewCommands(componentService, eventcoreAdopter{events: eventPlane})
	webhook.RegisterInstallationHandlers(webhookRouter, credService, issueService, trashWorkspaceOrg)
	webhookCtrl := webhook.NewWebhookController(webhookVerifier, deliveryStore, webhookRouter, routingLookup, routingCache)

	// The reconcile sweep (missed webhooks / disaster recovery) + the exec
	// watcher (OC WorkflowRun → execution-row outcomes + build terminals).
	eventPlaneSweep := eventcore.NewSweep(eventPlane, eventcoreRepoLister{repos: repoRepo}, 0)
	// The build half of the same plane. The ExecWatcher below only reports build
	// terminals for `kind=build` execution rows, and the run loop records its
	// cycles in run_cycles instead — so for anything the run loop builds, this
	// sweep is the only thing that observes a build finishing.
	buildSweep := eventcore.NewBuildSweep(eventPlane, eventcoreRepoLister{repos: repoRepo}, 0)
	execWatcher := codingagent.NewExecWatcher(componentClient, executionRepo, asServiceIdentity, 0).
		WithTaskNotifier(taskStreamHub).
		// Build terminals reach the milestone-run loop through the root observer
		// port — the watcher stays with the executor whose classification helpers
		// it shares, and reports outwards rather than importing a peer.
		WithBuildObserver(eventPlane)
	// A build that fails at git-clone-auth within budget is re-minted + re-tried
	// (§7): the build-secret stager is always wired, so the retrier is too.
	execWatcher.WithBuildRetrier(codingExecutor, codingExecutor.AuthRetryBudget())

	// Inbound JWT verifier — Thunder publishes the User JWT and Service JWT
	// signing keys at JWKSURL. Lazy fetch on first request avoids compose
	// start-order races.
	var thunderJWKS *jwtassertion.JWKSCache
	if cfg.JWKSURL != "" {
		thunderJWKS = jwtassertion.NewJWKSCache(cfg.JWKSURL)
		slog.Info("Inbound JWT verifier", "jwksURL", cfg.JWKSURL, "audience", cfg.JWTAllowedAudience, "issuer", cfg.JWTAllowedIssuer)
	} else {
		// Fail closed: with no JWKS the verifier rejects every /api/ request
		// (401). There is no unsigned-claim fallback — both planes set JWKS_URL.
		slog.Error("JWKS_URL not set — every authenticated /api/ request will be rejected (401)")
	}

	// Org-scoped GitHub connect/disconnect surface. Tasks are GitHub issues now
	// (no rows to abandon on disconnect); the disconnect service severs the
	// credential and the issues become inert to the router (no valid webhook).
	disconnectSvc := organization.NewOrgDisconnectService(credService, issueService).
		WithWorkspaceTrash(trashWorkspaceOrg)
	orgGitHubCtrl := organization.NewOrgGitHubController(
		credService,
		disconnectSvc,
		bearerSvc,
		cfg.GitHubAppSlug,
		cfg.BFFPublicURL,
		cfg.GitHubAppClientID,
	)

	// Internal S2S runner authorizer — keyed to the CYCLE: the id in the runner
	// bearer is the run cycle the pod was dispatched for, and the publisher-cc
	// branch resolves the acting org by that cycle id. Every agent pod in the
	// system is launched by the milestone supervisor, so a cycle id is the only
	// runner identity there is; an execution id named in a path fails closed.
	publisherVerifier := authn.NewPublisherTokenVerifier(thunderJWKS, cfg.PlatformIDP.Issuer, "aep-publisher-")
	runnerAuth := authn.NewRunnerAuthorizer(publisherVerifier, cycleOrgLookup(db))

	// Validation-context runner callback: resolves the run's deployed endpoint
	// URLs so they never enter the public issue.
	validationContextSvc := validation.NewContextService(
		validationCycleLocator{repo: runCycleRepo},
		validationEndpointResolver{store: artifactStore, comp: componentService},
	)
	// Test-credentials runner callback: the runner requests a login on demand
	// (only when a criterion needs one). v1 returns a shared mock account; the
	// cycle→project fence + request contract are what real per-project user
	// provisioning slots into later.
	validationCredentialsSvc := validation.NewCredentialService(
		validationCycleLocator{repo: runCycleRepo},
		mockValidationCredentials{},
	)

	// Controllers
	params := edge.AppParams{
		Config: cfg,
		// Runner callbacks are the internal contract-first surface (InternalDeps);
		// only the connect-callback + webhook controllers remain raw handlers.
		// Every other feature is served by the strict handlers via params.Deps.
		InternalDeps: edge.InternalDeps{
			CredsRefresh:          credRefreshService,
			RunnerAuth:            runnerAuth,
			ValidationContext:     validationContextSvc,
			ValidationCredentials: validationCredentialsSvc,
		},
		WebhookController:   webhookCtrl,
		OrgGitHubController: orgGitHubCtrl,
		ConfigRepo:          configRepo,
		ThunderJWKS:         thunderJWKS,
		OrganizationService: organizationService,

		DB:                   db,
		CredService:          credService,
		AnthropicCredService: anthropicCredService,
	}

	// The consolidated /config orchestrator (docs/design/org-config-consolidation.md):
	// one settings resource over the reused Anthropic/GitHub/IDP services, with the
	// platform IDP defaults so GET /config can render the default idp section
	// without persisting a row on read.
	orgConfigSvc := organization.NewService(
		anthropicCredService,
		credService,
		disconnectSvc,
		bearerSvc,
		idpService,
		organization.PlatformIDPConfig{Issuer: cfg.PlatformIDP.Issuer, JWKSURL: cfg.PlatformIDP.JWKSURL},
		cfg.BFFPublicURL,
		cfg.GitHubAppClientID,
	)

	// Strict-handler feature dependencies — everything the contract-first
	// /api/v1 edge serves (internal/api/handlers_*.go).
	params.Deps = edge.Deps{
		TaskTokens:      taskTokens,
		PublisherTokens: publisherVerifier,
		// DesignSvc backs the edge's own GET /projects/{name}/design/dependencies
		// handler (the one op served directly on the composite, not a domain
		// embed). *spec.designService satisfies the narrow reader port.
		DesignSvc: designService,
		// The projects domain (project CRUD + component read/build + config) is
		// assembled below (params.Deps.Projects).
		// The delivery domain (build + task reads/promote + task-log stream) is
		// assembled below (params.Deps.Delivery), after the external-resource
		// provisioner exists — build authors external bindings unset and SaveValues
		// writes supplied external-config secrets through the provisioner's SM-API path,
		// and its PreflightService reads the provisioning tri-state.
	}

	// Dependency-management MCP discovery readers (agnostic subset — Phase 4 of
	// the dependency-management migration). The MCP surface (surfaces.go) is
	// mounted behind the AgentsScopedVerifier; wire real backends for its four
	// read-only tools: the org external-resource catalog (org-namespaced OC
	// ResourceTypes, Task 3 — no longer the external_resources table) and the
	// org published endpoints + platform resource types (OC Resource-model
	// client). The provisioning surface (value/param collection + the
	// `provision` gate issue funnel) is wired in the Phase-6 block further below.
	resourceClient := openchoreo.NewResourceClient(ocConfig)
	// The resolver collaborators (repo locator + design reader) let the endpoint
	// catalog discover each org-service's real OpenAPI contract + repo coords
	// (endpoint spec discovery). Wired here so the A3 MCP tool projects them;
	// the read-only List/Resolve* surface degrades gracefully if either is nil.
	orgEndpointCatalog := dependencies.NewCatalog(resourceClient,
		dependencies.WithRepoLocator(repoRepo),
		dependencies.WithDesignReader(artifactStore),
	)
	// External-resource definitions are no longer persisted in AEP's database:
	// the authored OpenChoreo ResourceType is the org-level registry (read back
	// via openchoreo.ExternalDefinitionFromRT). MCP discovery + org-settings
	// list/delete re-source to those provisioned RTs; secret classification and
	// RT authoring read the project's committed design.json.
	// externalResourceRTCatalog is the single OC-RT-backed instance shared by
	// the MCP discovery surface (List/Get) and the org-settings
	// list+delete surface wired into provisioningSvc below (List/Delete) — one
	// reconstruction/dedupe rule (dependencies.ExternalResourceCatalog) for both.
	externalResourceRTCatalog := dependencies.NewExternalResourceCatalog(resourceClient)
	params.MCPExternalResources = externalResourceRTCatalog
	// ops — the Incident RCA domain (P1, the first landed domain). Alerts
	// (console issues #154, #155, BE handshake #156): the org-scoped store for
	// RCA-agent reports the console's notification bell and Alerts list/stepper
	// read. Write side is a plain userJWT-secured endpoint (no separate
	// service-auth scheme yet).
	//
	// This is the whole domain's wiring: its ports in, its handlers out. The
	// handlers are embedded directly into the edge's composite — the edge holds
	// no ops service.
	// sourcecontrol — the git-host substrate (P2). Its handlers are embedded
	// straight into the edge's composite; the edge holds no issue service.
	scHandlers, err := schttpapi.New(sourcecontrol.Deps{Issues: issueService})
	if err != nil {
		return nil, fmt.Errorf("assemble sourcecontrol domain: %w", err)
	}
	params.Deps.SourceControl = scHandlers

	// organization — org config + the organizations list (P3). Its handlers are
	// embedded straight into the edge's composite; the edge holds no org service.
	orgHandlers, err := orghttpapi.New(organization.Deps{OrgSvc: organizationService, Config: orgConfigSvc})
	if err != nil {
		return nil, fmt.Errorf("assemble organization domain: %w", err)
	}
	params.Deps.Organization = orgHandlers

	// spec — the Spec Authoring & Versioning domain (P4): genai turns, files,
	// tag reads, the org skills library, and the collab oracle/descriptor. Its
	// slice handlers embed straight into the edge's composite.
	specHandlers, err := spechttpapi.New(spec.Deps{
		GenAI:         genaiSvc,
		Files:         filesSvc,
		FilesActivity: filesActivityRecorder{svc: activitySvc, authorship: specAuthored},
		Artifacts:     artifactSvcGit,
		Skills:        skillSvc,
		SkillMut:      skillMutationSvc,
		SkillImport:   skillImportSvc,
		CollabRepo:    repoService,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble spec domain: %w", err)
	}
	params.Deps.Spec = specHandlers

	// projects — the Projects, Components, Builds & Config domain (P7): project
	// CRUD + status, the component read + build + deploy surface, and the
	// component env-var config. Its slice handlers embed straight into the edge's
	// composite; the edge holds no project/component/config service.
	// Settings → Usage (#291): fold spec-turn + delivery per-project usage,
	// labelled by the org's live projects (a usage slug with no live project is a
	// since-deleted project, shown greyed). Delivery's half reads the agent-usage
	// LEDGER — the one delivery table a project delete does not purge, and the one
	// place both its capture surfaces mirror into, so spend survives the project
	// and is never counted twice (see delivery.PhaseUsageRollup).
	usageService := projects.NewUsageService(
		turnRepo.SumUsageByProject,
		delivery.PhaseUsageRollup(usageLedgerRepo),
		func(ctx context.Context, orgID string) (map[string]string, error) {
			names := map[string]string{}
			list, err := projectService.ListProjects(ctx, orgID, 100, "", "")
			if err != nil {
				return nil, err
			}
			for _, p := range list.Items {
				display := p.DisplayName
				if display == "" {
					display = p.Name
				}
				names[p.Name] = display
			}
			// no-silent-caps: an org with >100 projects would drop the overflow
			// from the live-name lookup (they'd render as "deleted"). Log so the
			// truncation is visible rather than a silent mislabel.
			if list.NextCursor != "" {
				slog.Warn("usage roll-up: live-project name lookup truncated at 100; extra projects will render as deleted", "org", orgID)
			}
			return names, nil
		},
	)

	projectsHandlers, err := projectshttpapi.New(projects.Deps{
		ProjectSvc:   projectService,
		ComponentSvc: componentService,
		ConfigSvc:    configService,
		UsageSvc:     usageService,
		ActivitySvc:  activitySvc,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble projects domain: %w", err)
	}
	params.Deps.Projects = projectsHandlers

	opsHandlers, err := opshttpapi.New(ops.Deps{
		Reports: ops.NewRepository(db),
		Execs:   execution.NewOpsExecutionReader(executionRepo),
	})
	if err != nil {
		return nil, fmt.Errorf("assemble ops domain: %w", err)
	}
	params.Deps.Ops = opsHandlers
	params.MCPOrgEndpoints = orgEndpointCatalog
	resourceTypeCatalog := dependencies.NewResourceTypeCatalog(resourceClient, cfg.PlatformResourcesEnabled)
	params.MCPResourceTypes = resourceTypeCatalog
	// params.Deps.Dependencies (the strict ListPlatformResourceTypes + provisioning
	// ops) is assembled below, after provisioningSvc exists.
	// Endpoint spec discovery: the read-only remote-git reader an agent uses to
	// read a provider's OpenAPI file from its own repo (Contents + Code Search,
	// no clone). It resolves the org's credential (token + owner) from
	// credResolver and refuses any owner that is not the org's GitHub account.
	params.MCPRemoteGit = mcpdiscovery.NewRemoteGitClient(credResolver)
	// OpenAPI spec MCP tools (validate_openapi_spec, fetch_openapi_spec): wired
	// straight to the spec package's spec functions. FetchSpecFromURL is
	// PLATFORM-TOUCHING SSRF hardening reused as-is — the MCP tool layer only
	// adds a tighter context-safety size cap on top (mcp_tools.go), never a
	// looser network-level guard.
	params.MCPSpecValidator = spec.ValidateOpenAPI
	params.MCPSpecNormalizer = spec.NormalizeOpenAPIYAML
	params.MCPSpecFetcher = spec.FetchSpecFromURL
	// design-save keys BOTH platform-resource derivations on this catalog: the CRT
	// role marker for end-user auth (thunder-app generalization), and the type's
	// declared outputs for the dependency wiring it stamps into design.json. Wired
	// consumer-side so design holds only a narrow ResourceTypesByName port. When
	// the design declares a platform-resource dependency and this catalog is
	// unreachable, the save fails closed (ErrResourceCatalogUnavailable → 503).
	designService.SetResourceCatalog(crtTypeCatalog{resourceTypeCatalog})

	// Read-time org-service dependency resolution (dependency-management Phase 5):
	// the same endpoint catalog that backs the MCP list_org_endpoints tool marks
	// each design's `org-service` dependencies resolved/blocked/unresolved against
	// the live namespace-visible catalog. Consumer-side wiring — artifacts never
	// imports the dependencies feature (the *Catalog satisfies
	// spec.OrgServiceResolver structurally).
	artifactStore.SetOrgServiceResolver(orgEndpointCatalog)

	// Dependency provisioning (dependency-management Phase 6): the value/param
	// collection surface + the `provision` gate funnel. The provisioner cores
	// author the OC Resource model; the service drives gate issues + provision
	// Executions (Kind=provision) and closes each issue with a no-secrets
	// reference; the readiness watcher observes platform-resource bindings'
	// native Ready condition out-of-band and releases gated consumer tasks.
	// designComponents{store: artifactStore} is the SAME design-reader adapter
	// wired into provisioningSvc below (Deps.Design) — ResolveRunnerSecrets
	// classifies secret-vs-plain config keys from the project's committed
	// design.json, never the org catalog (parity with the build path).
	externalProvisioner := dependencies.NewExternalResourceProvisioner(designComponents{store: artifactStore}, resourceClient, secretRefWriter)
	// The public build surface: its InputsCoordinator runs pre-tag work (collect
	// external specs, derive end-user auth), derives unset external authoring from
	// the design, and carries the provision payload into the dev workflow.
	buildSvc := build.NewService(build.Deps{
		Repos:  repoFullNameLookup{repos: repoRepo},
		Tagger: buildSpecTagger{art: artifactSvcGit},
		Coord: build.NewInputsCoordinator(
			designService,                          // SpecCollector (CollectSpec)
			buildDesignDeriver{svc: designService}, // DesignFactDeriver (sentinel translation)
			designComponents{store: artifactStore},
		).WithSkillMirror(skillSvc), // refresh .claude/skills onto HEAD before the tag-cut
		// The build-time dependency hard gate's fresh read (dependencyGateFailures) —
		// the SAME designComponents{store: artifactStore} adapter PreflightSvc.Design
		// uses below, so both surfaces read the exact same
		// models.ComputeDependencyStatus-resolved Status/Reason (artifactStore's
		// SetOrgServiceResolver/SetExternalResourceResolver wiring above).
		Design: designComponents{store: artifactStore},
	})
	platformProvisioner := dependencies.NewOCNativeProvisioner(resourceClient)
	provisioningSvc := provisioning.NewService(provisioning.Deps{
		Issues:    issueService,
		Execs:     executionRepo,
		Design:    designComponents{store: artifactStore},
		Repos:     repoNamer{repos: repoRepo, db: db},
		RTCatalog: externalResourceRTCatalog,
		ExtProv:   externalProvisioner,
		PlatProv:  platformProvisioner,
		Bindings:  resourceClient,
		Projects:  provisionProjects{repos: repoRepo},
		Access:    dependencies.NewAccessRequestRepository(db),
		Providers: orgEndpointCatalog,
	})
	// Assemble the dependencies domain (P8): the provisioning slice (7 ops over
	// provisioningSvc) + the resource-type-discovery slice (ListPlatformResourceTypes
	// over the catalog). Both slices are nil-tolerant; the edge 503s when unwired.
	dependenciesHandlers, err := dephttpapi.New(dephttpapi.Deps{
		ProvisioningSvc: provisioningSvc,
		ResourceTypes:   resourceTypeCatalog,
		OrgEndpoints:    orgEndpointCatalog,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble dependencies domain: %w", err)
	}
	params.Deps.Dependencies = dependenciesHandlers
	// The build dependency-drawer preflight (issue #164): walks the design at HEAD
	// and emits a drawer item per dependency that still needs input, filtering out
	// anything already provisioned OR in-flight (buildProvisionStatus collapses the
	// provisioning tri-state onto the "already handled" bool).
	preflightSvc := build.NewPreflightService(build.PreflightDeps{
		Design: designComponents{store: artifactStore},
		Status: buildProvisionStatus{svc: provisioningSvc},
	})
	// delivery — the Delivery Pipeline domain (P6): the public single-tag build
	// surface, the task read + promote-dispatch surface, and the task-log SSE
	// stream. Its slice handlers embed straight into the edge's composite; the
	// edge holds no build/task/stream service. Assembled here, after the build +
	// preflight services (whose ports depend on the external-resource provisioner
	// constructed just above).
	// The milestone run READ surface. Both readers are the root repositories
	// (this is a read model — it writes nothing), and the log source is the same
	// OC/archive reader the task-log stream uses.
	runReads := runread.NewReads(milestoneRunRepo, runCycleRepo)
	runProgress := runread.NewProgressService(milestoneRunRepo, runCycleRepo, agentProgressReader)
	// A cycle's builds are DERIVED from OpenChoreo on read, never stored, so
	// this read is the one part of the run surface that touches the cluster —
	// which is why it is its own endpoint rather than a field on the run read.
	runCycleBuilds := runread.NewCycleBuilds(milestoneRunRepo, runCycleRepo,
		runreadProjectBuilds{oc: componentClient})

	deliveryDeps := deliveryhttpapi.Deps{
		BuildSvc:      buildSvc,
		PreflightSvc:  preflightSvc,
		BuildActivity: buildActivityRecorder{svc: activitySvc},
		TaskReads:     taskReads,
		TaskCommands:  taskCommands,
		TaskStream:    taskStreamSvc,
		RunReads:      runReads,
		RunProgress:   runProgress,
		// Cancel signals the supervisor AND deletes the cycle's agent
		// Component, which is what actually stops the pod and frees the org's
		// billing concurrency slot. Revalidate is the event plane's.
		RunCommands: runread.NewCommands(milestoneRunRepo, milestoneRunRepo, runSupervisor, eventcoreRevalidator{events: eventPlane}).
			WithCycleReaper(codingagent.NewCycleReaper(componentClient, runCycleRepo)),
		RunCycleBuilds: runCycleBuilds,
	}
	// WritePublisher stamps secret_ref_name onto the org's IDP profile;
	// without a SecretsProvider, ProvisionPublisherForBuild fails closed and
	// every POST /build 503s until a SecretsProvider is injected.
	deliveryDeps.PublisherProvisioner = idpService
	deliveryHandlers, err := deliveryhttpapi.New(deliveryDeps)
	if err != nil {
		return nil, fmt.Errorf("assemble delivery domain: %w", err)
	}
	params.Deps.Delivery = deliveryHandlers
	// The project's single validation task. The RUN mints it, at
	// deployed-green: minting it at plan time would put an issue in the working
	// set that nothing can work until every component is deployed.
	validationSvc := validation.NewService(validation.Deps{
		Issues:   issueService,
		Writer:   deliveryIssues,
		Criteria: validationCriteria{files: filesSvc},
	})
	// A planned Task's prose body names the App Path the agent works in — the
	// same component → appPath read the merged-PR build fan-out matches against.
	taskPlan.SetComponentPaths(designComponents{store: artifactStore})
	// Committed-truth spec-collect write surface: CollectSpec fetches/validates an
	// external dependency's OpenAPI contract and atomically commits the spec file
	// + the design.json specPath edit (clearing the external-needs-spec gate) via
	// the Files API. Composition-root adapter keeps files out of the design feature.
	designService.SetFileCommitter(designFilesCommitter{files: filesSvc})
	// Grant cascade → design: commit the exposesAPI.orgPublished durability marker
	// on a provider component when its cross-project access request is granted.
	// Setter-wired at the root (provisioning holds a narrow design port).
	provisioningSvc.SetOrgPublishMarker(designService)
	// Provider-build auto-kick (issue #164, Task 4): the automated org-service
	// visibility flow starts a not-yet-published provider project's build so it
	// deploys and publishes org-wide. Declared as a provisioning port so the
	// feature never imports build/devflow; the app-root adapter calls the build
	// service's non-HTTP StartProjectBuild entry point (idempotent).
	provisioningSvc.SetProviderBuildTrigger(providerBuildTrigger{build: buildSvc})
	// The milestone plan path (issue-driven execution §5): once the build's
	// whole-spec gate cuts `v<N>`, the click supersedes the previous milestone,
	// mints this version's and admits the run row that IS the project's build
	// mutex; the run then fills the milestone (gates, then the planning turn) as
	// its own first phase. Set here rather than in build.Deps because its gate
	// resolver is provisioningSvc, which is constructed after buildSvc.
	buildSvc.SetPlanPath(build.PlanPathDeps{
		Milestones: issueService,
		Issues:     deliveryIssues,
		Runs:       milestoneRunRepo,
		Planner:    taskPlan,
		Gates:      buildGateResolver{prov: provisioningSvc},
		Starter:    runSupervisor,
	})
	// Reject cascade: an org-publish gate issue closed with its consumers still
	// ungranted is a decline → flip those access requests to rejected. Registered
	// on the router's issues/closed chain alongside task's noop (both run). The
	// router is a pointer held by the already-built controller, so a late Register
	// (before serve) is picked up.
	registerWebhook("issues", "closed", provisioningSvc.OnIssueClosed)
	// Deprovision a project's OC Resource model on project delete (OC does not
	// cascade the logically-owned Resources/bindings).
	projectService.SetResourceDeprovisioner(provisioningSvc)
	resourceWatcher := provisioning.NewResourceWatcher(provisioningSvc, asServiceIdentity, 0)

	// Mount the component's external-resource secrets into the coding runner so
	// the agent can integration-test against the live service.
	codingExecutor.WithRunnerSecrets(runnerSecretResolver{svc: provisioningSvc})

	// Publish the platform-resolved `endpoints:` wiring onto the working set at
	// every cycle dispatch. Wired consumer-side so delivery holds only the narrow
	// WiringPublisher port (delivery cannot import dependencies — dependencies
	// already imports delivery).
	codingExecutor.WithWiringPublisher(provisioningSvc)
	// Refresh .claude/skills before each dispatch, so the clone the agent works
	// in carries the guidance its build was designed against.
	codingExecutor.WithSkillMirror(skillSvc)

	// Runtime-config (env-config.js) emission — the SPA's `window._env_` (API URLs
	// + generic <DEP>_<OUTPUT> keys for its platform-resource deps) is materialised
	// onto each web-app ReleaseBinding. Two triggers:
	//   - ensure-time: in the merged-PR build fan-out, right after the component's
	//     CR is ensured (self-no-ops for non-web-apps);
	//   - deploy-time: when ANY component deploys, re-emit across every SPA in the
	//     project (a backend's deploy can resolve a SPA's dep URL).
	runtimeConfigSvc := runtimeconfig.NewRuntimeConfigService(componentClient, resourceClient, artifactStore)
	// A web-app's platform-resource dependency outputs go into window._env_ as
	// generic <DEP>_<OUTPUT> keys; deps whose CRT carries the consumer-URL-env-config
	// marker also get the SPA's callback URL patched into their binding. Both key
	// off the same CRT marker catalog design-save uses — wired consumer-side so
	// runtimeconfig holds only a narrow MarkersByName port. Unlike design-save
	// this fails OPEN (defer + retry) when the catalog is unreachable: emission is
	// a retried cascade hook, not a user-facing save gate.
	runtimeConfigSvc.SetResourceCatalog(resourceTypeCatalog)
	// The pre-build ensure is now the Component CR alone. env-config.js used to be
	// emitted here too and could not land — the binding it writes to does not
	// exist before the first build — so it is a deploy-stage input instead, pulled
	// by the deployment service while it composes the binding.
	eventPlane.SetComponentEnsurer(eventcoreComponents{comp: componentService})
	// The deployment service's two config inputs, wired here because both are
	// built after it.
	deploymentService.SetConfigSources(configService, runtimeConfigSvc)
	configService.SetConverger(deploymentService)
	// The cross-project access grant is the only deploy observer left. The two
	// that rode beside it — the env-config.js re-emit and the api-configuration
	// trait re-emit — were writes to a ReleaseBinding this platform now composes
	// in one place, and both were inert on the run-loop rail anyway: they hang off
	// the ExecWatcher, which sweeps `kind=build` execution rows the run loop never
	// mints.
	execWatcher.WithDeployObserver(codingagent.NewMultiDeployObserver(provisioningSvc))

	slog.Info("OpenChoreo API", "baseURL", cfg.PlatformAPI.BaseURL)

	// R8b readiness gate: true after successful boot layout (Resolve → gitfs.New);
	// the reaper clears it on root-health failure and sets it again on recovery.
	var workspaceReady *reaper.ReadyGate
	if workspaceEngine != nil {
		workspaceReady = &reaper.ReadyGate{}
		workspaceReady.Set(true)
	}
	params.WorkspaceReady = workspaceReady
	handler := edge.NewHandler(params)

	// Disk-lifecycle reaper (design §14/D12): trash purge, snapshot age-reap,
	// DB↔disk orphan reconciliation, quota/LRU eviction. ENOSPC on Ensure/Mutate
	// triggers ForceSweep (unconditional trash purge + full Sweep). Fake()
	// leaves Workspace nil (no disk); skip reaper wiring in that case.
	var workspaceReaper Watcher
	if workspaceEngine != nil {
		r := reaper.New(workspaceEngine, reaperRepoLister{repoRepo}, cfg.Workspace, workspaceReady)
		workspaceEngine.SetOnENOSPC(func() {
			r.ForceSweep(context.Background())
		})
		workspaceReaper = r
	}

	// Background watchers, launched by main under a shared cancellable context.
	// State lives in Postgres + GitHub, so a plain goroutine per watcher is
	// enough. The reconciliation sweep re-gates queued executions and picks up
	// missed command labels; the exec watcher turns OC WorkflowRun outcomes into
	// execution-row terminals; the trait-sync + credential-validator watchers
	// are unchanged.
	watchers := []Watcher{
		// The event plane's reconcile backstop: a milestone with open work and no
		// live run gets one. It heals a webhook GitHub never delivered and the
		// adoption-versus-settle race, and walks only milestones the platform has
		// run — so it, too, is inert until run rows exist.
		eventPlaneSweep,
		// Observes run-loop builds reaching terminal and drives the re-trigger /
		// fix-issue / supervisor-signal path that OnBuildTerminal owns. Without it
		// a red build in a run never reaches a verdict and the run polls forever.
		buildSweep,
		execWatcher,
		// Resource-readiness watcher: turns platform-resource bindings going Ready
		// into provision-Execution terminals + gate-issue closes, releasing gated
		// consumer tasks (dependency-management §3.6).
		resourceWatcher,
		// Runtime-config convergence backstop: re-emits SPA env-config.js once a
		// web-app's own public URL resolves. The build-success emit runs before
		// the web-app's ReleaseBinding endpoint is up (so the consumer-url-env-config
		// gate defers the write), and — since a web-app is dispatched last — no
		// later build-success re-fires it. This idempotent sweep lands env-config.js
		// once the URL converges (replaces the dropped periodic reconcile backstop).
		// The binding converge sweep: the ONE backstop for drift no event causes.
		// It replaces two half-backstops that each covered one field of one object
		// — the retired trait drift watcher and the env-config.js sweep — because
		// with a single writer there is one thing to re-assert.
		projects.NewConvergeWatcher(projectLister{repos: repoRepo}, deploymentService, artifactStore, asServiceIdentity, 0),
		// Periodic credential validator — walks every active org_credentials row
		// once per cfg.CredentialValidatorInterval (default 24h), probes GitHub,
		// flags identity drift on confirmed unauthorised secrets.
		credValidator,
		// agent_turns crash-safety sweep (design D17): a stale-heartbeat
		// running turn is failed and the D18 one-active guard released;
		// locally-buffered streams get the terminal event.
		spec.NewTurnSweeper(turnRepo, turnBroker, 0, 0),
	}
	// Disk-lifecycle reaper: global passes self-elect via non-blocking flock.
	// Omitted when Fake() leaves Workspace nil (no disk at assemble time).
	if workspaceReaper != nil {
		watchers = append(watchers, workspaceReaper)
	}
	// The pod-truth watcher: it classifies each dispatched cycle from the Pod
	// OpenChoreo rendered for it, records a terminal agent reason when the agent
	// died without a pull request, and banks the run's token spend. It writes no
	// logs and deletes no components — history is the observability plane's and
	// deletion is retention's. Always on (no longer gated on cluster-gateway-proxy).
	watchers = append(watchers, codingagent.NewJobWatcher(runtimeClient, runCycleRepo, asServiceIdentity))
	slog.Info("codingagent.JobWatcher: enabled (OpenChoreo resource tree)")
	// The milestone run supervisor's Temporal worker. Registered only when
	// Temporal is configured (TEMPORAL_HOSTPORT set). The watcher dials in a
	// retry loop, so a Temporal server that is down at boot is not fatal — the
	// worker connects when it comes up.
	if cfg.Temporal.Enabled() {
		runActs := run.NewActivities(run.Deps{
			Runs:       runRuns{runs: milestoneRunRepo},
			Cycles:     runCycles{cycles: runCycleRepo},
			Milestones: issueService,
			PRs:        issueService,
			Design:     designComponents{store: artifactStore},
			Builds:     runBuilds{oc: componentClient},
			Validation: runValidation{svc: validationSvc, files: filesSvc},
			// The coding executor launches the cycle's runner Job and answers with
			// its Job ref. It mints no execution row — the cycle record is the
			// supervisor's own bookkeeping.
			Dispatcher: codingExecutor,
			// The deploy stage. The supervisor promotes each cycle's components
			// itself and waits for them to serve, which is what puts validation
			// after a running version rather than after a green build.
			Deploy:       deploymentService,
			Deployments:  deploymentService,
			DeployIssues: eventPlane,
			// The halt: a failed run's leftovers are marked so the reconcile sweep
			// does not restart them with fresh budgets. Same plane, same reason —
			// the supervisor decides, the plane writes the issue.
			Halter: eventPlane,
			// The cancel's other half: a cancelled run's in-flight issues are closed
			// and stamped so the sweep does not restart the run the user just
			// stopped, and so a rebuild of the same spec knows what to reopen.
			Canceller: eventPlane,
			// The planning phase. These are the same two collaborators the build
			// click used to drive in a detached goroutine; behind an activity they
			// are durable across a restart and retried on a blip.
			Gates:   buildGateResolver{prov: provisioningSvc},
			Planner: taskPlan,
		})
		watchers = append(watchers, run.NewWorkerWatcher(temporalRuntime, runActs))
		slog.Info("run: temporal worker watcher registered", "hostPort", cfg.Temporal.HostPort)
	}

	return &App{
		Handler:      handler,
		Watchers:     watchers,
		degradations: computeDegradations(cfg, smClient != nil),
	}, nil
}

// Degradation is one optional capability the assembled graph is running WITHOUT,
// with the config that would enable it. It is pure data, re-derived from cfg +
// Infra by computeDegradations: the if-cfg.X gates stay greppable two-liners in
// Assemble, and one assembly test enumerates the whole degraded-mode matrix off
// Degradations() — no capability/Profile abstraction.
type Degradation struct {
	Capability string // stable slug (e.g. "build-logs", "coding-dispatch-oc")
	Reason     string // which config is missing and what it turns off
}

// Degradations reports every optional capability the assembled app is running
// without, and why. Required config (JWKSURL, TaskTokenSigningKey) is not listed:
// config.Validate boot-fails on it, so it can never be a degradation here.
func (a *App) Degradations() []Degradation { return a.degradations }

func computeDegradations(cfg config.Config, secretsDelivery bool) []Degradation {
	var d []Degradation
	off := func(capability, reason string) { d = append(d, Degradation{capability, reason}) }

	if cfg.ServiceAuth.TokenURL == "" || cfg.ServiceAuth.ClientID == "" {
		off("m2m-service-auth", "SERVICE_AUTH_TOKEN_URL / SERVICE_AUTH_CLIENT_ID not set — OC calls carry no M2M token")
	}
	if cfg.Observability.BaseURL == "" {
		off("build-logs", "OBSERVABILITY_API_URL not set — build logs disabled")
		off("cycle-log-archive", "OBSERVER_URL not set — a finished cycle's agent log cannot be read back")
	}
	if !secretsDelivery {
		off("secrets-delivery", "SecretsProvider not injected — secret writes + external-secret cleanup disabled")
	}
	if cfg.AEPInternalBaseURL == "" {
		off("mcp-discovery", "AEP_INTERNAL_BASE_URL not set — design-turn MCP discovery omitted")
	}
	thunderBase := cfg.ThunderAdmin.BaseURL
	if thunderBase == "" {
		thunderBase = cfg.ServiceAuth.TokenURL
	}
	if cfg.ThunderAdmin.ClientID == "" || cfg.ThunderAdmin.ClientSecret == "" || thunderBase == "" {
		off("idp-mutations", "THUNDER_ADMIN_URL / THUNDER_SYSTEM_CLIENT_ID / THUNDER_SYSTEM_CLIENT_SECRET not set — per-org publisher IDP mutations 503")
	}
	if cfg.OAuthStateSigningKey == "" {
		off("connect-oauth-state", "OAUTH_STATE_SIGNING_KEY not set — GitHub App connect-state JWTs will fail to mint")
	}
	// Working dispatch is the OpenChoreo component path: the BFF creates the run
	// cycle's Component through platform-api-service and OC renders the Job. It
	// still needs secrets delivery, because the cycle's ExternalSecrets resolve
	// against the org's secret store — the BFF writes no secret material itself.
	if !secretsDelivery {
		off("coding-dispatch-oc", "secrets delivery not configured — the OpenChoreo coding-agent dispatch path cannot resolve its cycle secret refs")
	}
	if !cfg.Temporal.Enabled() {
		off("run-temporal", "TEMPORAL_HOSTPORT not set — milestone run worker watcher not registered")
	}
	return d
}

// buildSecretStagerAdapter maps the concrete *organization.BuildCredentialsService
// (StageBuildSecret → *StageResult) onto the component feature's
// BuildSecretStager port (→ secretRef string), so the component package need
// not import the services StageResult type. The adapter satisfies the consumer
// port at the composition root, not in the feature (§6.8).
type buildSecretStagerAdapter struct {
	svc *organization.BuildCredentialsService
}

func (a buildSecretStagerAdapter) StageBuildSecret(ctx context.Context, ocOrgID, repoSlug, workflowRunName string) (string, error) {
	res, err := a.svc.StageBuildSecret(ctx, ocOrgID, repoSlug, workflowRunName)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	return res.SecretRef, nil
}

// buildGitHost selects the git host implementation named by GIT_PROVIDER and
// returns it as sourcecontrol.Host. This is the only place a concrete provider client
// is constructed; every gitrepo domain service narrows Host to its own
// capability port. Deliberately a plain switch — NOT a registry or capability
// framework. A GitLab impl later is one new clients/gitlab package + one case.
// cfg.Validate() already rejects unknown providers at boot; the default arm is
// defensive.
func buildGitHost(cfg config.Config) (sourcecontrol.Host, error) {
	switch cfg.GitProvider {
	case "github":
		return githubclient.NewClient(), nil
	default:
		return nil, fmt.Errorf("unknown GIT_PROVIDER %q — supported: github", cfg.GitProvider)
	}
}

// deliveryOpenBaoConfigFromAppConfig maps local OPENBAO_* config onto the
// provider-neutral StoreConfig.OpenBao shape. Nil when addr is unset.
func deliveryOpenBaoConfigFromAppConfig(cfg config.Config) *secretmanagersvc.OpenBaoConfig {
	if cfg.OpenBaoAddr == "" {
		return nil
	}
	return &secretmanagersvc.OpenBaoConfig{
		Server: cfg.OpenBaoAddr,
		Path:   "secret",
		Auth:   &secretmanagersvc.OpenBaoAuth{Token: cfg.OpenBaoToken},
	}
}
