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

// Package app is the composition root: it assembles the entire service graph
// (HTTP handler + background watchers) from config + an open DB, and owns the
// imperative first-boot steps (Bootstrap) main runs before serving. It lives
// under internal/ — not package main — so a component test can import it and
// assemble the same real handler with faked deps (the harness IS Build).
package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/api"
	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/clients/clustergatewayproxy"
	githubclient "github.com/wso2/aep/aep-api/internal/clients/github"
	k8sclient "github.com/wso2/aep/aep-api/internal/clients/k8s"
	"github.com/wso2/aep/aep-api/internal/clients/oauth"
	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc/providers/secretmanagerapi"
	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/credentials"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/build"
	"github.com/wso2/aep/aep-api/internal/feature/codingagent"
	"github.com/wso2/aep/aep-api/internal/feature/component"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/endpoints"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/internal/feature/design"
	"github.com/wso2/aep/aep-api/internal/feature/devflow"
	"github.com/wso2/aep/aep-api/internal/feature/execution"
	"github.com/wso2/aep/aep-api/internal/feature/files"
	"github.com/wso2/aep/aep-api/internal/feature/genai"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/feature/idp"
	"github.com/wso2/aep/aep-api/internal/feature/organization"
	"github.com/wso2/aep/aep-api/internal/feature/orgconfig"
	"github.com/wso2/aep/aep-api/internal/feature/orgcreds"
	"github.com/wso2/aep/aep-api/internal/feature/project"
	"github.com/wso2/aep/aep-api/internal/feature/provisioning"
	"github.com/wso2/aep/aep-api/internal/feature/rcaagent"
	"github.com/wso2/aep/aep-api/internal/feature/runtimeconfig"
	"github.com/wso2/aep/aep-api/internal/feature/skills"
	"github.com/wso2/aep/aep-api/internal/feature/task"
	"github.com/wso2/aep/aep-api/internal/feature/webhook"
	authn "github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs/reaper"
	"github.com/wso2/aep/aep-api/internal/seed"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
	"gorm.io/gorm"
)

// Watcher is a long-running background loop. Every watcher blocks on its
// context and returns nothing; main starts each in its own goroutine and
// cancels the shared context on shutdown.
type Watcher interface {
	Run(ctx context.Context)
}

// App is the assembled service graph: the HTTP handler plus the background
// watchers to launch. Bootstrap side effects (migrations, grants) already ran
// before Build; App holds only what main needs to serve and to shut down.
type App struct {
	Handler  http.Handler
	Watchers []Watcher
}

// Build wires the entire service graph from config + an open DB and returns
// the HTTP handler + background watchers. It performs NO process-lifecycle work
// (no os.Exit, no signal handling, no migrations) — every failure is returned —
// so a component test can assemble the same real handler with faked deps.
// Wiring order is load-bearing: several constructors read the value a prior one
// produced; the comments call out the couplings.
func Build(cfg config.Config, db *gorm.DB) (*App, error) {
	var err error

	// Skills are repo-backed now (one private org-skills repo per org —
	// docs/design/skills-repo-storage.md). The store needs gitOpsService +
	// repoService, so it is constructed below once those exist. No startup
	// bootstrap: built-ins seed/reconcile into each org's repo on demand.

	// Repositories
	executionRepo := repositories.NewExecutionRepository(db)
	configRepo := repositories.NewConfigRepository(db)
	repoRepo := repositories.NewRepoRepository(db)
	workflowRunRepo := repositories.NewWorkflowRunRepository(db)

	// Temporal devflow runtime. Constructed always, but connects lazily in the
	// worker watcher's retry loop (never at Build time), so aep-api boots and
	// serves everything else when Temporal is down. Its worker watcher is
	// appended to the watcher slice below only when Temporal is configured.
	// The signaler is nil-safe and no-ops while the runtime is not connected,
	// so webhook handlers/watchers hold it unconditionally.
	devflowRuntime := devflow.NewRuntime(cfg.Temporal)
	devflowSignaler := devflow.NewSignaler(devflowRuntime, workflowRunRepo)

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
	// request URL) to the org's UUID, for the X-Impersonate-Org header on M2M
	// OC calls. Reads the local organizations side-car; prefers the
	// Thunder-issued ouId.
	orgUUIDResolver := func(ctx context.Context, namespace string) (string, error) {
		// Authoritative path: a user-initiated request carries the caller's
		// Thunder org UUID in the JWT (ouId). When the JWT's handle matches the
		// namespace we're about to impersonate, use ouId directly — no DB
		// dependency, and it's the same value Thunder embeds. Async paths
		// (webhooks, watchers) have no JWT and fall through to the side-car.
		if claims := authn.ClaimsFromContext(ctx); claims != nil && claims.OuId != "" && authn.ResolveOuHandle(claims) == namespace {
			return claims.OuId, nil
		}
		// Side-car path: the organizations row is keyed by the org handle (the
		// same value the BFF puts in OC URLs). orgensure backfills it with the
		// Thunder UUID on the first authed request from the org.
		var org models.Organization
		if err := db.WithContext(ctx).Where("name = ?", namespace).First(&org).Error; err != nil {
			return "", fmt.Errorf("resolve impersonation org for namespace %q: %w", namespace, err)
		}
		if org.ThunderOrgUUID != nil {
			return org.ThunderOrgUUID.String(), nil
		}
		return org.UUID.String(), nil
	}

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
	smWriter := orgcreds.NewSMAPIWriter(smClient, db)

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

	// Credentials + git-service services and controllers.
	credKey, err := base64.StdEncoding.DecodeString(cfg.CredentialEncryptionKey)
	if err != nil || len(credKey) != 32 {
		// config.Validate guarantees this decodes to 32 bytes; kept as defense.
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key: %w", err)
	}
	credStore, err := credentials.NewDBStore(db, credKey)
	if err != nil {
		return nil, fmt.Errorf("credential store init: %w", err)
	}
	slog.Info("credential store: postgres (aes-256-gcm)")

	wpClient, err := k8sclient.NewInClusterClient()
	if err != nil {
		slog.Warn("k8s client init failed — mint-build will skip Secret writes; builds will fail at clone", "error", err)
		wpClient = nil
	}

	// App-token minter — best-effort App-key load. With no App key the minter
	// answers in no-app mode; the connect surface lights up the App path
	// lazily on first use.
	loadCtx, cancelLoad := context.WithTimeout(context.Background(), 10*time.Second)
	appKey, err := credentials.LoadAppKeyFromOpenBao(loadCtx, credStore)
	cancelLoad()
	if err != nil {
		slog.Warn("app key load failed; App-mode credentials will return ErrAppNotConfigured", "error", err)
		appKey = nil
	}
	minter, err := credentials.NewAppTokenMinter(appKey)
	if err != nil {
		return nil, fmt.Errorf("app token minter init: %w", err)
	}
	minter.WithOpenBao(credStore)

	// Dev-only app-platform seed (App private key + client_secret + webhook
	// HMAC). No-op outside DEPLOYMENT_TIER=dev.
	{
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := seed.AppPlatformFromEnv(c, credStore, cfg); err != nil {
			cancel()
			return nil, fmt.Errorf("app platform seed: %w", err)
		}
		cancel()
	}
	if appKey == nil {
		retryCtx, cancelRetry := context.WithTimeout(context.Background(), 10*time.Second)
		if reloaded, rerr := credentials.LoadAppKeyFromOpenBao(retryCtx, credStore); rerr == nil && reloaded != nil {
			cancelRetry()
			minter, err = credentials.NewAppTokenMinter(reloaded)
			if err != nil {
				return nil, fmt.Errorf("app token minter re-init: %w", err)
			}
			minter.WithOpenBao(credStore)
			slog.Info("github app loaded post-seed", "appId", reloaded.AppID)
		} else {
			cancelRetry()
		}
	}
	if minter.AppID() != 0 {
		idCtx, cancelID := context.WithTimeout(context.Background(), 10*time.Second)
		if err := minter.LoadAppBotIdentity(idCtx, "https://api.github.com"); err != nil {
			slog.Warn("app bot identity load failed; will retry on first connect", "error", err)
		}
		cancelID()
	}
	var appClientSecret string
	if minter.AppID() != 0 {
		csCtx, cancelCS := context.WithTimeout(context.Background(), 10*time.Second)
		if cs, err := minter.LoadAppClientSecret(csCtx); err != nil {
			slog.Warn("app oauth client_secret load failed; bind path disabled", "error", err)
		} else {
			appClientSecret = cs
		}
		cancelCS()
	}

	credResolver := credentials.NewOrgResolver(db, credStore, minter)

	// One git host, selected by GIT_PROVIDER, threaded into every gitrepo
	// domain service where it narrows to that service's capability port.
	gitHost, err := buildGitHost(cfg)
	if err != nil {
		return nil, err
	}

	// Workspace engine — the disk-backed git plumbing over the shared
	// /workspaces mount (docs/design/shared-volume-clone-architecture.md).
	// Fail fast on an unusable root: the volume is mounted in compose/k8s,
	// and dev runs override AEP_WORKSPACE_ROOT. It backs the disk-lifecycle
	// pieces (the two best-effort trash hooks below + the reaper watcher) and
	// the GitOpsService Workspace port (skills today; files/artifacts/genai
	// in Phases 2–3).
	workspaceEngine, err := gitfs.New(cfg.Workspace.Root)
	if err != nil {
		return nil, fmt.Errorf("workspace engine init (root %q): %w", cfg.Workspace.Root, err)
	}
	slog.Info("workspace engine", "root", workspaceEngine.Root())
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

	repoService := gitrepo.NewRepoService(repoRepo, gitHost, credResolver, cfg.GitHubRepoVisibility,
		gitrepo.WithWorkspaceTrash(trashWorkspaceRepo))
	gitOpsService := gitrepo.NewGitOpsService(credResolver, workspaceEngine)
	artifactSvcGit := artifacts.NewArtifactService(repoRepo, gitOpsService)
	issueService := gitrepo.NewIssueService(repoRepo, gitHost, credResolver)
	webhookRegService := gitrepo.NewWebhookService(repoRepo, gitHost, repoService, issueService, cfg.WebhookDeliveryURL, cfg.WebhookHMACSecret)
	credRefreshService := orgcreds.NewCredentialsRefreshService(credResolver)
	credService := orgcreds.NewCredentialService(db, credStore, minter, cfg.WebhookHMACSecret, cfg.GitHubAppClientID, appClientSecret, gitHost)
	buildCredService := orgcreds.NewBuildCredentialsService(repoRepo, credResolver, gitSecretClient)
	credService.WithBuildSecretCleaner(buildCredService)
	anthropicCredService := orgcreds.NewAnthropicCredentialService(db, credStore, wpClient)

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
	validatorProbes := orgcreds.NewValidatorProbes(credService, gitHost, credResolver, minter)
	credValidator := credentials.NewValidator(db, validatorProbes, nil, cfg.CredentialValidatorInterval)

	// Artifact store — in-process via artifactSvcGit. Adds the
	// external-API catalog + the `DesignFile` YAML split/assemble layer
	// on top of raw file I/O.
	artifactStore := artifacts.NewArtifactStore(artifactSvcGit)

	// Repo-backed skills store (single source of truth = per-org org-skills
	// repo). Reads walk the shared-volume mirror at branch tip and writes
	// commit to main through the Workspace port (shared-volume-clone
	// architecture, Phase 1). Built-ins + flow skills seed/reconcile from the
	// embedded files on demand. docs/design/skills-repo-storage.md.
	skillSvc := skills.NewSkillService(gitOpsService, repoService)
	skillMutationSvc := skills.NewSkillMutationService(skillSvc)
	skillImportSvc := skills.NewSkillImportService(skillSvc)

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
	filesSvc := files.NewService(repoService, gitOpsService)

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
	skillsRepoForTurns := func(ctx context.Context, orgID string) (*models.GitRepository, error) {
		if err := skillSvc.EnsureProvisioned(ctx, orgID); err != nil {
			return nil, err
		}
		return repoService.GetRepo(ctx, orgID, models.SkillsRepoSentinelProjectID)
	}
	turnRepo := genai.NewTurnRepository(db)
	turnBroker := genai.NewTurnBroker()
	genaiDeps := genai.ServiceDeps{
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
	genaiSvc := genai.NewService(genaiDeps)

	// Services. componentService is constructed before configService so
	// configService can call back into it to mirror env-var edits onto
	// the OC Component's workflow params.
	projectService := project.NewProjectService(projectClient, repoService, webhookRegService, artifactSvcGit, executionRepo)
	// Build/deploy stage sources for the status poll (#184): the
	// workflow_runs index (one row read) + the org-scoped release-binding
	// list — consumer-side ports wired here so project imports neither.
	projectService.SetStageSources(workflowRunRepo, componentClient)
	organizationService := organization.NewOrganizationService(db, namespaceClient)
	// componentService takes repoSvc + buildCredSvc so TriggerBuild can
	// pre-stage the per-WorkflowRun build Secret in workflows-<orgID>
	// before the WorkflowRun is created (see
	// docs/design/build-credential-injection.md).
	var buildStager component.BuildSecretStager
	if buildCredService != nil {
		buildStager = buildSecretStagerAdapter{svc: buildCredService}
	}
	componentService := component.NewComponentService(componentClient, observClient, artifactStore, repoService, buildStager)
	configService := component.NewConfigService(configRepo, componentService)
	designService := design.NewDesignService(artifactStore, artifactSvcGit)

	// Tasks are GitHub issues (the Task/Execution split, tasks-github-native):
	// the read + plan surface reads them live and fuses executions. The dispatch
	// half (funnel, coding executor, watchers) is wired below, after
	// asServiceIdentity. repoService/artifactStore/artifactSvcGit/gitOpsService
	// satisfy the task consumer ports directly.
	taskReads := task.NewReads(issueService, repoService, executionRepo, artifactSvcGit, designComponents{store: artifactStore})
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
		execProgressSvc.WithCodingProgress(codingagent.NewAgentProgressReader(cgwClient, db))
	}
	// The task-log SSE stream: one connection per open task-detail page carries
	// the Task's whole live state (status + executions + unified timeline across
	// attempts). The hub is the in-proc change bus the PR webhook + job/exec
	// watchers ping so an attached stream re-derives instantly; a slow re-derive
	// tick on each connection is the safety net (no durable event table).
	taskStreamHub := execution.NewTaskStreamHub()
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
	traitSyncService := component.NewTraitSyncService(componentClient, artifactStore)

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
	idpService := idp.NewIDPService(db, thunderAdminClient, idp.PlatformIDPConfig{
		Issuer:  cfg.PlatformIDP.Issuer,
		JWKSURL: cfg.PlatformIDP.JWKSURL,
	}).WithSMAPIWriter(smWriter)
	// Make idpService available to trait_sync so first-protected-deploy
	// provisions the publisher app lazily.
	traitSyncService.SetIDPService(idpService)

	// Connect-state JWT issuer (App-mode OAuth CSRF state). This HS256 signing
	// key only ever leaves the BFF as a JWT signature inside the GitHub OAuth
	// `state` query param. (Task JWTs use RS256 via taskTokens below.)
	bearerSvc := orgcreds.NewBearerService(cfg.OAuthStateSigningKey, 24*time.Hour)
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
	deliveryStore := webhook.NewDeliveryStore(db)
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
		cfg.AgentPlatformURL, cfg.AgentPlatformURL)
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
		codingExecutor.WithProxy(codingagent.New(cgwClient), db, idpService, cfg.AgentRunnerImage, cfg.AgentClusterSecretStore)
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
		codingExecutor.WithK8sJobDispatch(k8sJobDispatcher, db)
		slog.Info("coding executor: direct k8s-job dispatch path enabled", "runnerImage", cfg.AgentRunnerImage)
	}
	// Build-secret staging so the post-merge build clones a PRIVATE project repo
	// (the local plane sets GITHUB_REPO_VISIBILITY=private). Reuses the same
	// per-org build GitSecret stager feature/component uses for manual builds.
	// nil stager → builds clone unauthenticated (public repos).
	if buildStager != nil {
		codingExecutor.WithBuildSecrets(buildStager, 0)
		slog.Info("coding executor: build-secret staging enabled (private-repo builds)")
	}
	// Coding-dispatch pre-flight: provision the OpenChoreo Component CR from the
	// design facts before the coding run, so the merged-PR build has a Component
	// to build (else "Component not found"). Ported from the legacy dispatch
	// service's ensureOCComponent; componentService reads the design facts.
	codingExecutor.WithComponentEnsurer(componentService)
	registry.Register(taskmeta.ClassCoding, codingExecutor)

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
	webhook.RegisterInstallationHandlers(webhookRouter, db, credService, issueService, trashWorkspaceOrg)
	webhookCtrl := webhook.NewWebhookController(webhookVerifier, deliveryStore, webhookRouter, routingLookup, routingCache)

	// Reconciliation sweep (missed webhooks / requeue gating / PR-state healing /
	// disaster recovery, §5) + the exec watcher (OC WorkflowRun → execution-row
	// outcomes; build success re-evaluates the funnel).
	sweep := execution.NewSweep(funnel, execEvents, executionRepo, repoLister{repos: repoRepo}, issueService, 0)
	execWatcher := codingagent.NewExecWatcher(componentClient, executionRepo, funnel, asServiceIdentity, 0).
		WithWorkflowSignaler(devflowSignaler).
		WithTaskNotifier(taskStreamHub)
	// A build that fails at git-clone-auth within budget is re-minted + re-tried
	// (§7). Only meaningful when a credential can be staged; otherwise a git-auth
	// failure is terminal like any other.
	if buildStager != nil {
		execWatcher.WithBuildRetrier(codingExecutor, codingExecutor.AuthRetryBudget())
	}

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
	disconnectSvc := orgcreds.NewOrgDisconnectService(db, credService, issueService).
		WithWorkspaceTrash(trashWorkspaceOrg)
	orgGitHubCtrl := orgcreds.NewOrgGitHubController(
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
	authn.SetRunnerAuthorizer(authn.NewRunnerAuthorizer(taskTokens, publisherVerifier, executionOrgLookup(db)))

	// Controllers
	params := api.AppParams{
		Config: cfg,
		// Runner callbacks are the internal Huma surface (InternalDeps); only the
		// connect-callback + webhook controllers remain raw handlers. Every other
		// feature registers code-first via params.HumaDeps below.
		InternalDeps: api.InternalDeps{
			CredsRefresh: credRefreshService,
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
	orgConfigSvc := orgconfig.NewService(
		anthropicCredService,
		credService,
		disconnectSvc,
		bearerSvc,
		idpService,
		idp.PlatformIDPConfig{Issuer: cfg.PlatformIDP.Issuer, JWKSURL: cfg.PlatformIDP.JWKSURL},
		cfg.BFFPublicURL,
		cfg.GitHubAppClientID,
	)

	// Code-first OpenAPI (Huma) feature dependencies. api.NewHandler creates the
	// Huma API on apiMux and registers every migrated feature via
	// RegisterAllHuma. See docs/design/bff-openapi-huma-migration.md.
	params.HumaDeps = api.HumaDeps{
		ProjectSvc:       projectService,
		OrgSvc:           organizationService,
		ComponentSvc:     componentService,
		ConfigSvc:        configService,
		CollabRepo:       repoService,
		IssueSvc:         issueService,
		TaskReads:        taskReads,
		TaskCommands:     taskCommands,
		TaskPlan:         taskPlan,
		TaskStream:       taskStreamSvc,
		ComponentClient:  componentClient,
		IDPSvc:           idpService,
		CredentialSvc:    credService,
		DisconnectSvc:    disconnectSvc,
		BearerSvc:        bearerSvc,
		AnthropicSvc:     anthropicCredService,
		OrgConfigSvc:     orgConfigSvc,
		TaskTokens:       taskTokens,
		SkillSvc:         skillSvc,
		SkillMutationSvc: skillMutationSvc,
		SkillImportSvc:   skillImportSvc,
		FilesSvc:         filesSvc,
		ArtifactSvc:      artifactSvcGit,
		GenAISvc:         genaiSvc,
		// BuildSvc is assigned below (params.HumaDeps.BuildSvc), after the
		// external-resource provisioner exists — its InputsCoordinator stages the
		// drawer's external-config secrets through that provisioner's SM-API write.
		GitHubAppSlug:     cfg.GitHubAppSlug,
		BFFPublicURL:      cfg.BFFPublicURL,
		GitHubAppClientID: cfg.GitHubAppClientID,
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
	orgEndpointCatalog := endpoints.NewCatalog(resourceClient,
		endpoints.WithRepoLocator(repoRepo),
		endpoints.WithDesignReader(artifactStore),
	)
	externalResourceRepo := repositories.NewExternalResourceRepository(db)
	params.MCPExternalResources = externalResourceRepo
	// Alerts (console issues #154, #155, BE handshake #156): org-scoped store
	// for RCA-agent reports the console's notification bell and Alerts
	// list/stepper read. Write side is a plain userJWT-secured endpoint (no
	// separate service-auth scheme yet) — see rcaagent_huma.go.
	rcaAgentReportRepo := repositories.NewRcaAgentReportRepository(db)
	params.HumaDeps.RcaAgentReportSvc = rcaagent.NewRcaAgentReportService(rcaAgentReportRepo, executionRepo)
	params.MCPOrgEndpoints = orgEndpointCatalog
	resourceTypeCatalog := resources.NewResourceTypeCatalog(resourceClient)
	params.MCPResourceTypes = resourceTypeCatalog
	params.HumaDeps.ResourceTypeCatalog = resourceTypeCatalog
	// Endpoint spec discovery: the read-only remote-git reader an agent uses to
	// read a provider's OpenAPI file from its own repo (Contents + Code Search,
	// no clone). It resolves the org's credential (token + owner) from
	// credResolver and refuses any owner that is not the org's GitHub account.
	params.MCPRemoteGit = dependencies.NewRemoteGitClient(credResolver)
	// design-save keys end-user-auth derivation on the CRT role marker read from
	// this catalog (thunder-app generalization); wired consumer-side so design
	// holds only a narrow MarkersByName port. When the design declares a
	// platform-resource dependency and this catalog is unreachable, the save
	// fails closed (ErrResourceCatalogUnavailable → 503).
	designService.SetResourceCatalog(resourceTypeCatalog)

	// Read-time org-service dependency resolution (dependency-management Phase 5):
	// the same endpoint catalog that backs the MCP list_org_endpoints tool marks
	// each design's `org-service` dependencies resolved/blocked/unresolved against
	// the live namespace-visible catalog. Consumer-side wiring — artifacts never
	// imports the dependencies feature (the *Catalog satisfies
	// artifacts.OrgServiceResolver structurally).
	artifactStore.SetOrgServiceResolver(orgEndpointCatalog)

	// Register each tagged design's `external` dependencies into the org's
	// external-resource catalog on save (best-effort). Consumer-side port —
	// design never imports repositories concretely.
	designService.SetExternalResourceRegistry(externalResourceRepo)

	// Dependency provisioning (dependency-management Phase 6): the value/param
	// collection surface + the aep:provision gate funnel. The provisioner cores
	// author the OC Resource model; the service drives gate issues + provision
	// Executions (Kind=provision) and closes each issue with a no-secrets
	// reference; the readiness watcher observes platform-resource bindings'
	// native Ready condition out-of-band and releases gated consumer tasks.
	externalProvisioner := resources.NewExternalResourceProvisioner(externalResourceRepo, resourceClient, smWriter)
	// The public build surface: its InputsCoordinator runs the drawer inputs'
	// pre-tag work (collect external specs, derive end-user auth) and stages
	// external-config secrets to SM-API through externalProvisioner before the
	// tag-cut, carrying the resulting provision payload into the dev workflow.
	buildSvc := build.NewService(build.Deps{
		Runner: build.NewTemporalRunner(devflowRuntime),
		Store:  workflowRunRepo,
		Repos:  repoFullNameLookup{repos: repoRepo},
		Tagger: buildSpecTagger{art: artifactSvcGit},
		Tasks:  taskReads,
		Coord: build.NewInputsCoordinator(
			designService,                        // SpecCollector (CollectSpec)
			buildAuthDeriver{svc: designService}, // AuthDeriver (sentinel translation)
			buildSecretStager{prov: externalProvisioner},
			designComponents{store: artifactStore},
		),
	})
	params.HumaDeps.BuildSvc = buildSvc
	platformProvisioner := resources.NewOCNativeProvisioner(resourceClient)
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
		Access:    repositories.NewAccessRequestRepository(db),
		Providers: orgEndpointCatalog,
	})
	params.HumaDeps.ProvisioningSvc = provisioningSvc
	// The build dependency-drawer preflight (issue #164): walks the design at HEAD
	// and emits a drawer item per dependency that still needs input, filtering out
	// anything already provisioned OR in-flight (buildProvisionStatus collapses the
	// provisioning tri-state onto the "already handled" bool).
	params.HumaDeps.PreflightSvc = build.NewPreflightService(build.PreflightDeps{
		Design: designComponents{store: artifactStore},
		Status: buildProvisionStatus{svc: provisioningSvc},
	})
	// Mint aep:provision gate issues on design approval (before planning gates any
	// consumer coding task on them).
	designService.SetProvisionIssueMinter(provisioningSvc)
	// Committed-truth spec-collect write surface: CollectSpec fetches/validates an
	// external dependency's OpenAPI contract and atomically commits the spec file
	// + the design.json specPath edit (clearing the external-needs-spec gate) via
	// the Files API. Composition-root adapter keeps files out of the design feature.
	designService.SetFileCommitter(designFilesCommitter{files: filesSvc})
	// Grant cascade → design: commit the exposesAPI.orgPublished durability marker
	// on a provider component when its cross-project access request is granted.
	// The reverse edge (design→provisioning) is SetProvisionIssueMinter above;
	// both are setter-wired here so the mutual dependency stays at the root.
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

	handler := api.NewHandler(params)

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
		runtimeconfig.NewWatcher(db, runtimeConfigSvc, asServiceIdentity, 0),
		// Periodic credential validator — walks every active org_credentials row
		// once per cfg.CredentialValidatorInterval (default 24h), probes GitHub,
		// flags identity drift on confirmed unauthorised credentials.
		credValidator,
		// Disk-lifecycle reaper (design §14/D12): trash purge, snapshot
		// age-reap, DB↔disk orphan reconciliation, quota/LRU eviction. The
		// global passes self-elect via a non-blocking flock on the mount, so
		// running one per replica is correct.
		reaper.New(workspaceEngine, repoRepo, cfg.Workspace),
		// agent_turns crash-safety sweep (design D17): a stale-heartbeat
		// running turn is failed and the D18 one-active guard released;
		// locally-buffered streams get the terminal event.
		genai.NewTurnSweeper(turnRepo, turnBroker, 0, 0),
	}
	// JobWatcher polls the `ca-…` coding-agent Jobs and Finishes the coding
	// execution FAILED on Job failure (success rides the PR webhook), capturing
	// the pod's final log. Keyed on the proxy client alone: both dispatch paths
	// (proxy and direct K8sJobDispatcher) emit `ca-…` run names, so the watcher
	// reads job status + logs through the proxy stub regardless of dispatcher.
	if cgwClient != nil {
		jobWatcher := codingagent.NewJobWatcher(db, cgwClient, executionRepo).
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
			Runs:        workflowRunRepo,
			Dispatcher:  codingDispatcher{funnel: funnel, execs: executionRepo},
			Merger:      prMerger{issues: issueService},
			Spec:        devflowSpecValidator{art: artifactSvcGit},
			Planner:     devflowPlanner{plan: taskPlan, reads: taskReads},
			Validator:   devflowValidator{},
			Provisioner: buildProvisioner{design: designService, prov: provisioningSvc},
		})
		watchers = append(watchers, devflow.NewWorkerWatcher(devflowRuntime, devflowActs))
		slog.Info("devflow: temporal worker watcher registered", "hostPort", cfg.Temporal.HostPort)
	}

	return &App{Handler: handler, Watchers: watchers}, nil
}

// codingDispatcher adapts the execution funnel + repository onto the devflow
// CodingDispatcher port: trigger a coding attempt (funnel admission + gating +
// coding executor), then read the admitted coding execution's id back for the
// workflow's status. Wired at the composition root so devflow does not import
// the execution package.
type codingDispatcher struct {
	funnel *execution.Funnel
	execs  repositories.ExecutionRepository
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
	issues gitrepo.IssueService
}

func (m prMerger) MergePR(ctx context.Context, orgID, projectID string, prNumber int) error {
	return m.issues.MergePullRequest(ctx, orgID, projectID, prNumber)
}

// buildSecretStagerAdapter maps the concrete *orgcreds.BuildCredentialsService
// (StageBuildSecret → *StageResult) onto the component feature's
// BuildSecretStager port (→ secretRef string), so the component package need
// not import the services StageResult type. The adapter satisfies the consumer
// port at the composition root, not in the feature (§6.8).
type buildSecretStagerAdapter struct {
	svc *orgcreds.BuildCredentialsService
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
// returns it as gitrepo.Host. This is the only place a concrete provider client
// is constructed; every gitrepo domain service narrows Host to its own
// capability port. Deliberately a plain switch — NOT a registry or capability
// framework (see docs/design/aep-api-target-structure.md, "Explicitly NOT a
// framework"). A GitLab impl later is one new clients/gitlab package + one case.
// cfg.Validate() already rejects unknown providers at boot; the default arm is
// defensive.
func buildGitHost(cfg config.Config) (gitrepo.Host, error) {
	switch cfg.GitProvider {
	case "github":
		return githubclient.NewClient(), nil
	default:
		return nil, fmt.Errorf("unknown GIT_PROVIDER %q — supported: github", cfg.GitProvider)
	}
}
