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
// (HTTP handler + background watchers) from config + a resolved Infra bundle,
// doing NO I/O of its own — deterministic and millisecond-fast, so an assembly
// test can build the same real graph with a faked Infra (app.Assemble(cfg,
// app.Fake())). Resolve (infra.go) performs every boot side effect — DB open +
// migrations, the OpenBao key loads, the dev seed, k8s in-cluster init, the
// workspace fsck — and hands Assemble the results. main runs Resolve → Assemble
// → serve.
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
	"github.com/wso2/aep/aep-api/internal/clients/clustergatewayproxy"
	"github.com/wso2/aep/aep-api/internal/clients/oauth"
	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc/providers/secretmanagerapi"
	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/build"
	"github.com/wso2/aep/aep-api/internal/delivery/codingagent"
	"github.com/wso2/aep/aep-api/internal/delivery/devflow"
	"github.com/wso2/aep/aep-api/internal/delivery/execution"
	deliveryhttpapi "github.com/wso2/aep/aep-api/internal/delivery/httpapi"
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
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/projects"
	projectshttpapi "github.com/wso2/aep/aep-api/internal/projects/httpapi"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	githubclient "github.com/wso2/aep/aep-api/internal/sourcecontrol/githubhost"
	schttpapi "github.com/wso2/aep/aep-api/internal/sourcecontrol/httpapi"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
	"github.com/wso2/aep/aep-api/internal/spec"
	spechttpapi "github.com/wso2/aep/aep-api/internal/spec/httpapi"
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

// Assemble wires the entire service graph from config + a resolved Infra and
// returns the HTTP handler + background watchers. It performs NO I/O and NO
// process-lifecycle work (no os.Exit, no signals, no network, no clock, no
// filesystem) — every dependency that needed boot-time I/O arrives pre-resolved
// in `in` (see Resolve) — so an assembly test builds the same real graph in
// milliseconds with Fake(). Wiring order is load-bearing: several constructors
// read the value a prior one produced; the comments call out the couplings.
func Assemble(cfg config.Config, in Infra) (*App, error) {
	var err error
	db := in.DB
	credStore := in.CredStore
	minter := in.Minter
	appClientSecret := in.AppClientSecret
	wpClient := in.K8sClient
	workspaceEngine := in.Workspace

	// Skills are repo-backed now (one private org-skills repo per org —
	// docs/design/skills-repo-storage.md). The store needs gitOpsService +
	// repoService, so it is constructed below once those exist. No startup
	// bootstrap: built-ins seed/reconcile into each org's repo on demand.

	// Repositories
	executionRepo := delivery.NewExecutionRepository(db)
	// Read-time USD derivation for every usage surface (#245, ADR-0011).
	pricer := modelcost.New(modelcost.Rates{
		ModelID:              cfg.ModelPricing.ModelID,
		InputUSDPerMTok:      cfg.ModelPricing.InputUSDPerMTok,
		OutputUSDPerMTok:     cfg.ModelPricing.OutputUSDPerMTok,
		CacheReadUSDPerMTok:  cfg.ModelPricing.CacheReadUSDPerMTok,
		CacheWriteUSDPerMTok: cfg.ModelPricing.CacheWriteUSDPerMTok,
	})
	configRepo := projects.NewConfigRepository(db)
	repoRepo := sourcecontrol.NewRepoRepository(db)
	workflowRunRepo := delivery.NewWorkflowRunRepository(db)
	orgRepo := organization.NewOrganizationRepository(db)
	orgCredRepo := organization.NewOrgCredentialRepository(db)
	orgAnthropicRepo := organization.NewOrgAnthropicRepository(db)
	idpRepo := organization.NewIDPRepository(db)
	codingAgentLogRepo := delivery.NewCodingAgentLogRepository(db)

	// Temporal devflow runtime. Constructed always, but connects lazily in the
	// worker watcher's retry loop (never at Build time), so aep-api boots and
	// serves everything else when Temporal is down. Its worker watcher is
	// appended to the watcher slice below only when Temporal is configured.
	// The signaler is nil-safe and no-ops while the runtime is not connected,
	// so webhook handlers/watchers hold it unconditionally.
	devflowRuntime := delivery.NewRuntime(cfg.Temporal)
	devflowSignaler := delivery.NewSignaler(devflowRuntime, workflowRunRepo)

	// Token provider for service-to-service auth. OC authorizes requests by
	// the service client subject (aep-api-client), so every OC API call
	// must carry this token rather than the end-user's token.
	var tokenProvider *oauth.TokenProvider
	if cfg.ServiceAuth.TokenURL != "" && cfg.ServiceAuth.ClientID != "" {
		tokenProvider = oauth.NewTokenProvider(
			cfg.ServiceAuth.TokenURL,
			cfg.ServiceAuth.ClientID,
			cfg.ServiceAuth.ClientSecret,
			cfg.ServiceAuth.HostHeader,
		)
		slog.Info("Service auth configured", "tokenURL", cfg.ServiceAuth.TokenURL, "clientID", cfg.ServiceAuth.ClientID)
	}

	// orgUUIDResolver maps an OC namespace (the org handle the BFF puts in the
	// request URL) to the org's UUID for the X-Impersonate-Org header on M2M OC
	// calls: JWT-first (the caller's own ouId), else the organizations side-car.
	// The decision logic lives in the named, tested impersonationResolver.
	orgUUIDResolver := impersonationResolver{sidecar: orgSideCar{db: db}}.Resolve

	// OpenChoreo clients. Each one resolves the OC namespace as the OC
	// org handle directly (== ouHandle); there is no override map. Migrated
	// clients (namespace, project) take an openchoreo.Config; the still-hand-
	// rolled clients (component, secretref) keep the legacy positional args
	// until they migrate too.
	ocConfig := openchoreo.Config{
		BaseURL:                cfg.PlatformAPI.BaseURL,
		HostHeader:             cfg.PlatformAPI.HostHeader,
		AuthProvider:           tokenProvider,
		ImpersonateOrgResolver: orgUUIDResolver,
	}
	projectClient := openchoreo.NewProjectClient(ocConfig)
	namespaceClient := openchoreo.NewNamespaceClient(ocConfig)
	componentClient := openchoreo.NewComponentClient(ocConfig)
	// GitSecret client lands the per-org build git credential on the workflow
	// plane (via OC → OpenBao → SecretReference). Used by BuildCredentialsService
	// for both cloud (CP/WP split) and local k3d — one unified path.
	gitSecretClient := openchoreo.NewGitSecretClient(ocConfig)

	// Observability client (optional — build logs disabled when URL not set)
	var observClient observability.Client
	if cfg.Observability.BaseURL != "" {
		observClient = observability.NewClient(cfg.Observability.BaseURL)
		slog.Info("Observability API", "baseURL", cfg.Observability.BaseURL)
	}

	// SM-API provider (ADR-0002). Same provider in local + cloud: local
	// SM-API runs in the docker-compose stack, cloud SM-API is reached at
	// its public DNS. When SECRET_MANAGER_API_URL is empty the provider is
	// not constructed and downstream callers handle the absence.
	var smClient secretmanagersvc.SecretManagementClient
	if cfg.SecretManagerAPIURL != "" {
		smProvider := secretmanagerapi.NewProvider(secretmanagerapi.Config{
			BaseURL: cfg.SecretManagerAPIURL,
			Timeout: cfg.SecretManagerAPITimeout,
		})
		smClient, err = secretmanagersvc.NewSecretManagementClient(&secretmanagersvc.StoreConfig{
			Provider: secretmanagerapi.ProviderName,
		}, smProvider)
		if err != nil {
			return nil, fmt.Errorf("sm-api client init: %w", err)
		}
		slog.Info("sm-api client", "baseURL", cfg.SecretManagerAPIURL, "timeout", cfg.SecretManagerAPITimeout)
	} else {
		slog.Warn("SECRET_MANAGER_API_URL not set — Phase 1 secret writes disabled")
	}
	_ = smClient // consumed via smWriter below.

	// SM-API mirror writer. Constructed ahead of the credential / IDP service
	// constructors so all consumers can attach via WithSMAPIWriter (the no-op
	// case when smClient is nil is fine).
	smWriter := organization.NewSMAPIWriter(smClient, orgCredRepo, orgAnthropicRepo, idpRepo)

	// cluster-gateway-proxy client. Used for reading coding-agent pod logs +
	// job status (streaming feed + JobWatcher) and, when sm-api is also
	// configured, for the full proxy DISPATCH path. When CLUSTER_GATEWAY_PROXY_URL
	// is empty none of those are wired and dispatch uses the direct
	// K8sJobDispatcher with no live streaming. In a local install this points at
	// the in-cluster cluster-gateway-proxy stub (reads only).
	var cgwClient *clustergatewayproxy.Client
	if cfg.ClusterGatewayProxyURL != "" {
		cgwCfg := clustergatewayproxy.Config{BaseURL: cfg.ClusterGatewayProxyURL}
		if tokenProvider != nil {
			cgwCfg.AuthProvider = tokenProvider
		}
		cgwClient = clustergatewayproxy.New(cgwCfg)
		slog.Info("cluster-gateway-proxy client", "baseURL", cfg.ClusterGatewayProxyURL, "authenticated", tokenProvider != nil)
	} else {
		slog.Warn("CLUSTER_GATEWAY_PROXY_URL not set — coding-agent live streaming + JobWatcher disabled; dispatch uses the direct K8s Job path")
	}

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
	webhookRegService := sourcecontrol.NewWebhookService(repoRepo, gitHost, repoService, issueService, cfg.WebhookDeliveryURL, cfg.WebhookHMACSecret)
	credRefreshService := organization.NewCredentialsRefreshService(credResolver)
	credService := organization.NewCredentialService(orgCredRepo, credStore, minter, cfg.WebhookHMACSecret, cfg.GitHubAppClientID, appClientSecret, gitHost)
	buildCredService := organization.NewBuildCredentialsService(repoRepo, credResolver, gitSecretClient)
	credService.WithBuildSecretCleaner(buildCredService)
	anthropicCredService := organization.NewAnthropicCredentialService(orgAnthropicRepo, credStore, wpClient)

	// Task JWT manager — RS256, 24h TTL. The public key is published on the
	// JWKS endpoint (/auth/external/jwks.json) and verified by both the runner
	// callbacks (inbound S2S) and agents-service (outbound S2S). Constructed
	// here, before the agents client, because that client uses it to mint the
	// per-call outbound identity token.
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
		slog.Warn("BFF_TASK_SIGNING_KEY not set — task dispatch will fail")
	}

	// SM-API mirror writer wired into both credential services. nil-safe via
	// the Enabled() check.
	credService.WithSMAPIWriter(smWriter)
	anthropicCredService.WithSMAPIWriter(smWriter)
	// Push the org's Anthropic key to a consumer's ExternalSecret on every
	// successful Connect (both first-time connect and later rotation) — see
	// AnthropicCredentialService.pushExternalSecret. nil-safe: disabled
	// unless both env vars are set (no consumer assumed by default).
	anthropicCredService.WithRCAAgentPush(cgwClient, cfg.RCAAgentAnthropicPushNamespace, cfg.RCAAgentAnthropicPushSecretName)
	validatorProbes := organization.NewValidatorProbes(credService, gitHost, credResolver, minter)
	credValidator := secrets.NewValidator(db, validatorProbes, nil, cfg.CredentialValidatorInterval)

	// Artifact store — in-process via artifactSvcGit. Adds the
	// external-API catalog + the `DesignFile` YAML split/assemble layer
	// on top of raw file I/O.
	artifactStore := spec.NewArtifactStore(artifactSvcGit)

	// Repo-backed skills store (single source of truth = per-org org-skills
	// repo). Reads walk the shared-volume mirror at branch tip and writes
	// commit to main through the Workspace port (shared-volume-clone
	// architecture, Phase 1). Built-ins + flow skills seed/reconcile from the
	// embedded files on demand. docs/design/skills-repo-storage.md.
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

	// Unified genai committed-truth turn surface (shared-volume-clone §6). It
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
	turnRepo := spec.NewTurnRepository(db)
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
	// Build/deploy stage sources for the status poll (#184): the
	// workflow_runs index (one row read) + the org-scoped release-binding
	// list — consumer-side ports wired here so project imports neither.
	projectService.SetStageSources(workflowRunRepo, componentClient)
	organizationService := organization.NewOrganizationService(orgRepo, namespaceClient)
	// componentService takes repoSvc + buildCredSvc so TriggerBuild can
	// pre-stage the per-WorkflowRun build Secret in workflows-<orgID>
	// before the WorkflowRun is created (see
	// docs/design/build-credential-injection.md). buildCredService is never nil
	// (NewBuildCredentialsService always returns a value; its gitSecrets are
	// nil-safe internally), so the stager is always wired.
	buildStager := buildSecretStagerAdapter{svc: buildCredService}
	componentService := projects.NewComponentService(componentClient, observClient, artifactStore, repoService, buildStager)
	configService := projects.NewConfigService(configRepo, componentService)
	designService := spec.NewDesignService(artifactStore, artifactSvcGit)

	// Tasks are GitHub issues (the Task/Execution split, tasks-github-native):
	// the read + plan surface reads them live and fuses executions. The dispatch
	// half (funnel, coding executor, watchers) is wired below, after
	// asServiceIdentity. repoService/artifactStore/artifactSvcGit/gitOpsService
	// satisfy the task consumer ports directly.
	taskReads := task.NewReads(issueService, repoService, executionRepo, artifactSvcGit, designComponents{store: artifactStore}, pricer)
	taskPlan := task.NewPlanService(repoService, artifactSvcGit, gitOpsService,
		anthropicKeyForGenAI, agentsvcClient, issueService, workspaceEngine, task.SkillsRepoResolver(skillsRepoForTurns))

	// Eagerly provision each org's skills repo on project creation.
	projectService.SetSkillsProvisioner(skillSvc)

	// The Task-keyed log endpoint (issue number → newest execution by default,
	// executionId query pins one for history browsing). (The runner skills-pull
	// S2S endpoint is retired — the runner now clones `org-skills` and resolves
	// applied skills locally, stamped via AEP_SKILLS_REPO_URL above.)
	execProgressSvc := execution.NewProgressService(executionRepo, componentClient)
	// Coding-execution activity feed: live-tail the ca-… pod log while running,
	// serve the captured coding_agent_logs snapshot once terminal. Keyed on the
	// proxy client alone (NOT on the dispatch path): a local install dispatches
	// via the direct K8sJobDispatcher but still reads pod logs through the
	// proxy stub, so streaming works regardless of which dispatcher ran.
	if cgwClient != nil {
		execProgressSvc.WithCodingProgress(codingagent.NewAgentProgressReader(cgwClient, codingAgentLogRepo, orgRepo))
	}
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

	// trait_sync is the single shared emitter that reconciles the
	// `api-configuration` ClusterTrait on a Component CR + per-environment
	// ReleaseBindings. Hooked from both the dispatch path (after
	// CreateComponent) and the design-edit path (after
	// `components/<name>/design.md` PUT). See
	// docs/design/api-platform-integration.md §6 Phase 2.
	traitSyncService := projects.NewTraitSyncService(componentClient, artifactStore)

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
	// WithSMAPIWriter mirrors per-org publisher client_secret to SM-API on
	// EnsureOrgPublisher / RegenerateClientSecret so the dispatcher's
	// PUBLISHER_CLIENT_SECRET ExternalSecret can materialise it into runner
	// pods without the BFF holding the plaintext.
	idpService := organization.NewIDPService(idpRepo, orgRepo, thunderAdminClient, organization.PlatformIDPConfig{
		Issuer:  cfg.PlatformIDP.Issuer,
		JWKSURL: cfg.PlatformIDP.JWKSURL,
	}).WithSMAPIWriter(smWriter)
	// Make idpService available to trait_sync so first-protected-deploy
	// provisions the publisher app lazily.
	traitSyncService.SetIDPService(idpService)

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

	// The reactive engine (tasks-github-native §5): the executions repository +
	// an executor registry + THE single funnel. The execute endpoint, the
	// webhook handlers, and the reconciliation sweep all call into the funnel —
	// there is no imperative second door, so gates cannot be bypassed.
	registry := execution.NewRegistry()
	funnel := execution.NewFunnel(executionRepo, issueService, repoLocator{db: db}, designComponents{store: artifactStore}, registry)

	// The coding-class executor (feature/codingagent) is the one wired executor.
	// It dispatches the coding-agent run and the post-merge build, writing
	// execution rows. The ops class has no executor yet (§11 — the funnel flags
	// aep:attention for it).
	codingExecutor := codingagent.NewCodingExecutor(
		componentClient, repoService, identities{cred: credService},
		anthropicProvisioner{svc: anthropicCredService}, taskTokens, executionRepo,
		cfg.AgentPlatformURL, cfg.AgentPlatformURL,
		orgRepo, orgAnthropicRepo, orgCredRepo, idpRepo)
	// Stamp AEP_SKILLS_REPO_URL so the runner clones `org-skills` and resolves
	// applied skills locally (the same EnsureProvisioned+GetRepo closure the
	// genai + task-plan turns use for their SkillsRef).
	codingExecutor.WithSkillsRepo(skillsRepoForTurns)
	// The cluster-gateway-proxy DISPATCH path (per-org NS + per-run
	// ExternalSecrets + a K8s Job via the proxy) requires sm-api: the per-run
	// ExternalSecrets source their values (Anthropic key, etc.) from it. So this
	// path is gated on BOTH the proxy AND sm-api being configured — the cloud/prod
	// posture. In a local install the proxy stub is present for reads (streaming +
	// JobWatcher, both keyed on cgwClient below) but sm-api is not, so dispatch
	// falls through to the direct K8sJobDispatcher, which writes the Anthropic
	// Secret straight into the runner namespace (no OpenBao/ESO/sm-api).
	if cgwClient != nil && cfg.SecretManagerAPIURL != "" {
		codingExecutor.WithProxy(codingagent.New(cgwClient), idpService, cfg.AgentRunnerImage, cfg.AgentClusterSecretStore)
		slog.Info("coding executor: cluster-gateway-proxy dispatch path enabled (proxy + sm-api)",
			"runnerImage", cfg.AgentRunnerImage, "clusterSecretStore", cfg.AgentClusterSecretStore)
	}
	// Direct K8s Job fallback: no cluster-gateway-proxy or SM-API needed.
	// Enabled when the in-cluster client, runner image, and platform URL are all set.
	if wpClient != nil && cfg.AgentRunnerImage != "" && cfg.AgentPlatformURL != "" {
		k8sJobDispatcher := codingagent.NewK8sJobDispatcher(
			wpClient,
			anthropicKeyReaderAdapter{svc: anthropicCredService},
			cfg.AgentPlatformURL,
			cfg.AgentRunnerImage,
		)
		codingExecutor.WithK8sJobDispatch(k8sJobDispatcher)
		slog.Info("coding executor: direct k8s-job dispatch path enabled", "runnerImage", cfg.AgentRunnerImage)
	}
	// Build-secret staging so the post-merge build clones a PRIVATE project repo
	// (the local plane sets GITHUB_REPO_VISIBILITY=private). Reuses the same
	// per-org build GitSecret stager feature/component uses for manual builds.
	codingExecutor.WithBuildSecrets(buildStager, 0)
	slog.Info("coding executor: build-secret staging enabled (private-repo builds)")
	// Coding-dispatch pre-flight: provision the OpenChoreo Component CR from the
	// design facts before the coding run, so the merged-PR build has a Component
	// to build (else "Component not found"). Ported from the legacy dispatch
	// service's ensureOCComponent; componentService reads the design facts.
	codingExecutor.WithComponentEnsurer(componentService)
	codingExecutor.WithValidationImage(cfg.AgentValidationRunnerImage)
	registry.Register(taskmeta.ClassCoding, codingExecutor)
	// The same executor serves ClassValidation: its runCoding branch swaps the
	// Playwright image + AEP_TASK_KIND=validation and skips the coding-only
	// component-ensure/wiring pre-flight (validation-phase).
	registry.Register(taskmeta.ClassValidation, codingExecutor)

	// Command surface calls into the funnel; webhook handling splits across the
	// two package halves: issues.* (task birth / block repair / command labels)
	// in feature/task, pull_request.* (end coding / spawn build) in feature/execution.
	taskCommands := task.NewCommands(issueService, repoService, funnel, componentService)
	platformSender := githubBotLogin(cfg.GitHubAppSlug)
	registerWebhook := func(event, action string, h func(ctx context.Context, event, action string, payload []byte) error) {
		webhookRouter.Register(event, action, webhook.EventHandlerFunc(h))
	}
	task.NewWebhookEvents(issueService, repoLocator{db: db}, funnel, platformSender).RegisterHandlers(registerWebhook)
	// pull_request.* handlers apply NO echo suppression (the platform authors no
	// PRs; in App mode the runner's PR opens as <slug>[bot] and must be acted on).
	execEvents := execution.NewEvents(executionRepo, funnel, registry, issueService).
		WithWorkflowSignaler(devflowSignaler).
		WithTaskNotifier(taskStreamHub)
	execEvents.RegisterHandlers(registerWebhook)
	webhook.RegisterInstallationHandlers(webhookRouter, credService, issueService, trashWorkspaceOrg)
	webhookCtrl := webhook.NewWebhookController(webhookVerifier, deliveryStore, webhookRouter, routingLookup, routingCache)

	// Reconciliation sweep (missed webhooks / requeue gating / PR-state healing /
	// disaster recovery, §5) + the exec watcher (OC WorkflowRun → execution-row
	// outcomes; build success re-evaluates the funnel).
	sweep := execution.NewSweep(funnel, execEvents, executionRepo, repoLister{repos: repoRepo}, issueService, 0)
	execWatcher := codingagent.NewExecWatcher(componentClient, executionRepo, funnel, asServiceIdentity, 0).
		WithWorkflowSignaler(devflowSignaler).
		WithTaskNotifier(taskStreamHub)
	// A build that fails at git-clone-auth within budget is re-minted + re-tried
	// (§7): the build-secret stager is always wired, so the retrier is too.
	execWatcher.WithBuildRetrier(codingExecutor, codingExecutor.AuthRetryBudget())

	// NOTE: the trait_sync drift watcher enumerated (org,project,component) from
	// the component_tasks table to periodically reconcile the api-configuration
	// ClusterTrait. That table is gone (tasks are GitHub issues), so the periodic
	// watcher is dropped. The per-env traitEnvironmentConfigs (jwtAuth/CORS) are
	// instead re-emitted on the ExecWatcher deploy path via traitDeployObserver
	// (wired into the MultiDeployObserver fan-out below), which fires once a
	// protected component — or a sibling SPA — deploys and its ReleaseBinding
	// exists to carry the config. A component-enumerating reconcile backstop can
	// be re-added over the OC component list if drift reappears.

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

	// Internal S2S runner authorizer — re-keyed to executions (§9.2): the id in
	// the runner bearer is an execution id, and the publisher-cc branch resolves
	// the acting org by execution id.
	publisherVerifier := authn.NewPublisherTokenVerifier(thunderJWKS, cfg.PlatformIDP.Issuer, "aep-publisher-")
	runnerAuth := authn.NewRunnerAuthorizer(taskTokens, publisherVerifier, executionOrgLookup(db))

	// Validation-context runner callback: resolves the run's deployed endpoint
	// URLs so they never enter the public issue.
	validationContextSvc := validation.NewContextService(
		validationExecLocator{repo: executionRepo},
		validationEndpointResolver{store: artifactStore, comp: componentService},
	)
	// Test-credentials runner callback: the runner requests a login on demand
	// (only when a criterion needs one). v1 returns a shared mock account; the
	// execution→project fence + request contract are what real per-project user
	// provisioning slots into later.
	validationCredentialsSvc := validation.NewCredentialService(
		validationExecLocator{repo: executionRepo},
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
		TaskTokens: taskTokens,
		// The projects domain (project CRUD + component read/build + config) is
		// assembled below (params.Deps.Projects).
		// The delivery domain (build + task reads/promote + task-log stream) is
		// assembled below (params.Deps.Delivery), after the external-resource
		// provisioner exists — the build service's InputsCoordinator stages the
		// drawer's external-config secrets through that provisioner's SM-API write,
		// and its PreflightService reads the provisioning tri-state.
	}

	// Dependency-management MCP discovery readers (agnostic subset — Phase 4 of
	// the dependency-management migration). The MCP surface (surfaces.go) is
	// mounted behind the AgentsScopedVerifier; wire real backends for its four
	// read-only tools: the org external-resource catalog (DB) and the org
	// published endpoints + platform resource types (OC Resource-model client).
	// The provisioning surface (value/param collection + the aep:provision issue
	// funnel) is wired in the Phase-6 block further below.
	resourceClient := openchoreo.NewResourceClient(ocConfig)
	// The resolver collaborators (repo locator + design reader) let the endpoint
	// catalog discover each org-service's real OpenAPI contract + repo coords
	// (endpoint spec discovery). Wired here so the A3 MCP tool projects them;
	// the read-only List/Resolve* surface degrades gracefully if either is nil.
	orgEndpointCatalog := dependencies.NewCatalog(resourceClient,
		dependencies.WithRepoLocator(repoRepo),
		dependencies.WithDesignReader(artifactStore),
	)
	externalResourceRepo := dependencies.NewExternalResourceRepository(db)
	params.MCPExternalResources = externalResourceRepo
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
		GenAI:       genaiSvc,
		Files:       filesSvc,
		Artifacts:   artifactSvcGit,
		Skills:      skillSvc,
		SkillMut:    skillMutationSvc,
		SkillImport: skillImportSvc,
		CollabRepo:  repoService,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble spec domain: %w", err)
	}
	params.Deps.Spec = specHandlers

	// projects — the Projects, Components, Builds & Config domain (P7): project
	// CRUD + status, the component read + build + deploy surface, and the
	// component env-var config. Its slice handlers embed straight into the edge's
	// composite; the edge holds no project/component/config service.
	projectsHandlers, err := projectshttpapi.New(projects.Deps{
		ProjectSvc:   projectService,
		ComponentSvc: componentService,
		ConfigSvc:    configService,
		TurnUsage:    spec.NewUsageReader(turnRepo, repoRepo, gitOpsService),
		ExecUsage:    executionRepo,
		Pricer:       pricer,
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
	resourceTypeCatalog := dependencies.NewResourceTypeCatalog(resourceClient)
	params.MCPResourceTypes = resourceTypeCatalog
	// params.Deps.Dependencies (the strict ListPlatformResourceTypes + provisioning
	// ops) is assembled below, after provisioningSvc exists.
	// Endpoint spec discovery: the read-only remote-git reader an agent uses to
	// read a provider's OpenAPI file from its own repo (Contents + Code Search,
	// no clone). It resolves the org's credential (token + owner) from
	// credResolver and refuses any owner that is not the org's GitHub account.
	params.MCPRemoteGit = mcpdiscovery.NewRemoteGitClient(credResolver)
	// design-save keys end-user-auth derivation on the CRT role marker read from
	// this catalog (thunder-app generalization); wired consumer-side so design
	// holds only a narrow MarkersByName port. When the design declares a
	// platform-resource dependency and this catalog is unreachable, the save
	// fails closed (ErrResourceCatalogUnavailable → 503).
	designService.SetResourceCatalog(crtMarkerCatalog{resourceTypeCatalog})

	// Read-time org-service dependency resolution (dependency-management Phase 5):
	// the same endpoint catalog that backs the MCP list_org_endpoints tool marks
	// each design's `org-service` dependencies resolved/blocked/unresolved against
	// the live namespace-visible catalog. Consumer-side wiring — artifacts never
	// imports the dependencies feature (the *Catalog satisfies
	// spec.OrgServiceResolver structurally).
	artifactStore.SetOrgServiceResolver(orgEndpointCatalog)

	// Register each tagged design's `external` dependencies into the org's
	// external-resource catalog on save (best-effort). Consumer-side port —
	// design never imports repositories concretely.
	designService.SetExternalResourceRegistry(spec.ExternalResourceRegistrarFunc(
		func(ctx context.Context, orgID, name, description string, schema []spec.ConfigKey) error {
			_, err := externalResourceRepo.Upsert(ctx, orgID, name, description, schema)
			return err
		}))

	// Dependency provisioning (dependency-management Phase 6): the value/param
	// collection surface + the aep:provision gate funnel. The provisioner cores
	// author the OC Resource model; the service drives gate issues + provision
	// Executions (Kind=provision) and closes each issue with a no-secrets
	// reference; the readiness watcher observes platform-resource bindings'
	// native Ready condition out-of-band and releases gated consumer tasks.
	externalProvisioner := dependencies.NewExternalResourceProvisioner(externalResourceRepo, resourceClient, smWriter)
	// The public build surface: its InputsCoordinator runs the drawer inputs'
	// pre-tag work (collect external specs, derive end-user auth) and stages
	// external-config secrets to SM-API through externalProvisioner before the
	// tag-cut, carrying the resulting provision payload into the dev workflow.
	buildSvc := build.NewService(build.Deps{
		Runner: build.NewTemporalRunner(devflowRuntime),
		Store:  workflowRunRepo,
		Repos:  repoFullNameLookup{repos: repoRepo},
		Tagger: buildSpecTagger{art: artifactSvcGit},
		Usage:  executionRepo,
		Pricer: pricer,
		Tasks:  taskReads,
		Coord: build.NewInputsCoordinator(
			designService,                        // SpecCollector (CollectSpec)
			buildAuthDeriver{svc: designService}, // AuthDeriver (sentinel translation)
			buildSecretStager{prov: externalProvisioner},
			designComponents{store: artifactStore},
		),
	})
	platformProvisioner := dependencies.NewOCNativeProvisioner(resourceClient)
	provisioningSvc := provisioning.NewService(provisioning.Deps{
		Issues:    issueService,
		Execs:     executionRepo,
		Reeval:    funnel,
		Design:    designComponents{store: artifactStore},
		Repos:     repoNamer{repos: repoRepo, db: db},
		Catalog:   externalResourceRepo,
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
	deliveryHandlers, err := deliveryhttpapi.New(deliveryhttpapi.Deps{
		BuildSvc:     buildSvc,
		PreflightSvc: preflightSvc,
		TaskReads:    taskReads,
		TaskCommands: taskCommands,
		TaskStream:   taskStreamSvc,
	})
	if err != nil {
		return nil, fmt.Errorf("assemble delivery domain: %w", err)
	}
	params.Deps.Delivery = deliveryHandlers
	// Mint the project's single aep:validation Task in the PLANNING pass: the
	// plan session mints it right after the plan tap creates the implementation
	// issues, so it is born in the same phase as them (and never pollutes the
	// plan turn's existing-task context). It dependsOn every component, so the
	// funnel holds it until they all deploy (validation-phase).
	validationSvc := validation.NewService(validation.Deps{
		Issues:   issueService,
		Design:   designComponents{store: artifactStore},
		Criteria: validationCriteria{files: filesSvc},
	})
	taskPlan.SetValidationIssueMinter(validationSvc)
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

	// ADR-0004 declarative wiring: at coding dispatch the platform resolves the
	// component's dependency targets (org-service + sibling endpoints, external +
	// platform-resource binding outputs) and posts them as a comment the coding
	// agent copies into workload.yaml — the platform never patches the CR.
	codingExecutor.WithDependencyWiring(provisioning.NewWiringResolver(
		designComponents{store: artifactStore}, orgEndpointCatalog, resourceClient, issueService))
	// Mount the component's external-resource secrets into the coding runner so
	// the agent can integration-test against the live service.
	codingExecutor.WithRunnerSecrets(runnerSecretResolver{svc: provisioningSvc})

	// Runtime-config (env-config.js) emission — the SPA's `window._env_` (API URLs
	// + generic <DEP>_<OUTPUT> keys for its platform-resource deps) is materialised
	// onto each web-app ReleaseBinding.
	// Two triggers, mirroring the retired dispatch cascade:
	//   - ensure-time: at the coding-dispatch pre-flight, emit for the just-ensured
	//     component (self-no-ops for non-web-apps);
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
	codingExecutor.WithComponentRuntimeConfig(runtimeConfigSvc)
	// Fan the build-success deploy event out to both the cross-project access grant
	// AND env-config.js re-emission. Best-effort + error-isolated: one observer
	// failing never stops the other (matching the old cascade's warn-and-continue).
	execWatcher.WithDeployObserver(codingagent.NewMultiDeployObserver(
		provisioningSvc,
		spaDeployObserver{svc: runtimeConfigSvc},
		// api-configuration trait re-emit: land the jwtAuth/CORS
		// traitEnvironmentConfigs on each protected API's ReleaseBinding once it
		// (or a sibling SPA) deploys. EnsureComponent sets only the CR trait
		// shape at create; this deploy-time PATCH is what makes the gateway
		// enforce end-user auth (docs/design/api-platform-integration.md §6).
		traitDeployObserver{svc: traitSyncService},
	))

	slog.Info("OpenChoreo API", "baseURL", cfg.PlatformAPI.BaseURL)

	handler := edge.NewHandler(params)

	// Background watchers, launched by main under a shared cancellable context.
	// State lives in Postgres + GitHub, so a plain goroutine per watcher is
	// enough. The reconciliation sweep re-gates queued executions and picks up
	// missed command labels; the exec watcher turns OC WorkflowRun outcomes into
	// execution-row terminals; the trait-sync + credential-validator watchers
	// are unchanged.
	watchers := []Watcher{
		sweep,
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
		runtimeconfig.NewWatcher(executionRepo, runtimeConfigSvc, asServiceIdentity, 0),
		// Periodic credential validator — walks every active org_credentials row
		// once per cfg.CredentialValidatorInterval (default 24h), probes GitHub,
		// flags identity drift on confirmed unauthorised secrets.
		credValidator,
		// Disk-lifecycle reaper (design §14/D12): trash purge, snapshot
		// age-reap, DB↔disk orphan reconciliation, quota/LRU eviction. The
		// global passes self-elect via a non-blocking flock on the mount, so
		// running one per replica is correct.
		reaper.New(workspaceEngine, reaperRepoLister{repoRepo}, cfg.Workspace),
		// agent_turns crash-safety sweep (design D17): a stale-heartbeat
		// running turn is failed and the D18 one-active guard released;
		// locally-buffered streams get the terminal event.
		spec.NewTurnSweeper(turnRepo, turnBroker, 0, 0),
	}
	// JobWatcher polls the `ca-…` coding-agent Jobs and Finishes the coding
	// execution FAILED on Job failure (success rides the PR webhook), capturing
	// the pod's final log. Keyed on the proxy client alone: both dispatch paths
	// (proxy and direct K8sJobDispatcher) emit `ca-…` run names, so the watcher
	// reads job status + logs through the proxy stub regardless of dispatcher.
	if cgwClient != nil {
		jobWatcher := codingagent.NewJobWatcher(codingAgentLogRepo, orgRepo, cgwClient, executionRepo).
			WithWorkflowSignaler(devflowSignaler).
			WithTaskNotifier(taskStreamHub)
		// Per-run ExternalSecret teardown applies only to the proxy dispatch
		// path (which stages them); the direct K8s-Job path creates none.
		if cfg.SecretManagerAPIURL != "" {
			jobWatcher.WithExternalSecretCleanup()
		}
		watchers = append(watchers, jobWatcher)
		slog.Info("codingagent.JobWatcher: enabled (cluster-gateway-proxy configured)",
			"externalSecretCleanup", cfg.SecretManagerAPIURL != "")
	}
	// Temporal devflow worker. Registered only when Temporal is configured
	// (TEMPORAL_HOSTPORT set). The watcher dials in a retry loop, so a Temporal
	// server that is down at boot is not fatal — the worker connects when it
	// comes up and the devflow endpoints answer 503 until then. Activities are
	// thin adapters over the funnel (dispatch) + issue service (merge).
	if cfg.Temporal.Enabled() {
		devflowActs := devflow.NewActivities(devflow.Deps{
			Runs:               workflowRunRepo,
			Dispatcher:         codingDispatcher{funnel: funnel, execs: executionRepo},
			Merger:             prMerger{issues: issueService},
			Spec:               devflowSpecValidator{art: artifactSvcGit},
			Planner:            devflowPlanner{plan: taskPlan, reads: taskReads},
			Validator:          devflowValidator{store: artifactStore, comp: componentService},
			ValidationResolver: devflowValidationResolver{svc: validationSvc, art: artifactSvcGit},
			Provisioner:        buildProvisioner{design: designService, prov: provisioningSvc},
		})
		watchers = append(watchers, devflow.NewWorkerWatcher(devflowRuntime, devflowActs))
		slog.Info("devflow: temporal worker watcher registered", "hostPort", cfg.Temporal.HostPort)
	}

	return &App{
		Handler:      handler,
		Watchers:     watchers,
		degradations: computeDegradations(cfg, in),
	}, nil
}

// Degradation is one optional capability the assembled graph is running WITHOUT,
// with the config that would enable it. It is pure data, re-derived from cfg +
// Infra by computeDegradations: the if-cfg.X gates stay greppable two-liners in
// Assemble, and one assembly test enumerates the whole degraded-mode matrix off
// Degradations() — no capability/Profile abstraction.
type Degradation struct {
	Capability string // stable slug (e.g. "build-logs", "coding-dispatch-any")
	Reason     string // which config is missing and what it turns off
}

// Degradations reports every optional capability the assembled app is running
// without, and why. Required config (JWKSURL, TaskTokenSigningKey) is not listed:
// config.Validate boot-fails on it, so it can never be a degradation here.
func (a *App) Degradations() []Degradation { return a.degradations }

func computeDegradations(cfg config.Config, in Infra) []Degradation {
	var d []Degradation
	off := func(capability, reason string) { d = append(d, Degradation{capability, reason}) }

	if cfg.ServiceAuth.TokenURL == "" || cfg.ServiceAuth.ClientID == "" {
		off("m2m-service-auth", "SERVICE_AUTH_TOKEN_URL / SERVICE_AUTH_CLIENT_ID not set — OC calls carry no M2M token")
	}
	if cfg.Observability.BaseURL == "" {
		off("build-logs", "OBSERVABILITY_API_URL not set — build logs disabled")
	}
	if cfg.SecretManagerAPIURL == "" {
		off("sm-api-secret-writes", "SECRET_MANAGER_API_URL not set — Phase-1 secret writes + external-secret cleanup disabled")
	}
	if cfg.ClusterGatewayProxyURL == "" {
		off("cluster-gateway-proxy", "CLUSTER_GATEWAY_PROXY_URL not set — coding-agent live streaming + JobWatcher disabled")
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
	// Dispatch paths: the cloud proxy path needs cluster-gateway-proxy + SM-API;
	// the direct K8s-Job path needs the in-cluster client + runner image + platform
	// URL. Neither wired ⇒ the undocumented no-dispatch-path state: coding /
	// validation runs cannot be launched at all.
	proxyDispatch := cfg.ClusterGatewayProxyURL != "" && cfg.SecretManagerAPIURL != ""
	k8sDispatch := in.K8sClient != nil && cfg.AgentRunnerImage != "" && cfg.AgentPlatformURL != ""
	if !proxyDispatch {
		off("coding-dispatch-proxy", "cluster-gateway-proxy + SM-API not both set — cloud proxy dispatch path off")
	}
	if !k8sDispatch {
		off("coding-dispatch-k8s", "in-cluster k8s client / AGENT_RUNNER_IMAGE / AGENT_PLATFORM_URL not all set — direct K8s-Job dispatch off")
	}
	if !proxyDispatch && !k8sDispatch {
		off("coding-dispatch-any", "NO dispatch path wired — coding/validation runs cannot be launched")
	}
	if cfg.RCAAgentAnthropicPushNamespace == "" || cfg.RCAAgentAnthropicPushSecretName == "" {
		off("rca-agent-key-push", "RCA_AGENT_ANTHROPIC_PUSH_* not set — org Anthropic key not pushed to a consumer ExternalSecret")
	}
	if !cfg.Temporal.Enabled() {
		off("devflow-temporal", "TEMPORAL_HOSTPORT not set — devflow worker watcher not registered")
	}
	return d
}

// codingDispatcher adapts the execution funnel + repository onto the devflow
// CodingDispatcher port: trigger a coding attempt (funnel admission + gating +
// coding executor), then read the admitted coding execution's id back for the
// workflow's status. Wired at the composition root so devflow does not import
// the execution package.
type codingDispatcher struct {
	funnel *execution.Funnel
	execs  delivery.ExecutionRepository
}

func (d codingDispatcher) DispatchCoding(ctx context.Context, orgID, projectID, repo string, issue int) (string, error) {
	if err := d.funnel.OnExecuteIntent(ctx, repo, issue); err != nil {
		return "", err
	}
	execs, err := d.execs.LatestPerKind(ctx, repo, issue)
	if err != nil {
		return "", err
	}
	if coding := execs[string(taskmeta.KindCoding)]; coding != nil {
		return coding.ID, nil
	}
	// No coding row admitted (e.g. closed issue / provision gate) — not an
	// error; the workflow proceeds and the PR-wait times out if nothing runs.
	return "", nil
}

// prMerger adapts the issue service onto the devflow PRMerger port.
type prMerger struct {
	issues sourcecontrol.IssueService
}

func (m prMerger) MergePR(ctx context.Context, orgID, projectID string, prNumber int) error {
	return m.issues.MergePullRequest(ctx, orgID, projectID, prNumber)
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
