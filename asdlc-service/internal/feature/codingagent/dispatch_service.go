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

package codingagent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/contracts"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/component"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/connections"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/orgcreds"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/k8sname"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/tenant"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

// DispatchResult is the outcome of dispatching a single task. The value type
// lives on the contracts leaf so task's HTTP edge can consume []DispatchResult
// through a task-local port without importing codingagent (§4 cycle invariant);
// aliased here so this feature's producers keep constructing it unqualified.
type DispatchResult = contracts.DispatchResult

// DispatchService orchestrates dispatching pending tasks. Per task it:
//
//  1. Verifies a GitHub issue exists (created at task generation).
//  2. Ensures the OC Component exists (with AutoBuild=false).
//  3. Mints a fresh per-task RS256 JWT.
//  4. Creates a WorkflowRun of ClusterWorkflow `app-factory-coding-agent`
//     via WorkflowRunService.TriggerCodingAgent. The Argo pod clones
//     the project repo on its default branch and runs the Claude Agent
//     SDK with the asdlc skill loaded; the agent itself creates the
//     feature branch and opens the PR with `Closes #<issue>` so the
//     webhook handler can link the PR back to the task.
//
// Idempotency: dispatch is gated on `DispatchedAt` — once set, re-dispatch
// is a no-op. The agent owns branch+PR creation, and the
// pull_request.opened webhook persists `BranchName` and
// `PullRequestNumber` once the agent opens its PR.
type DispatchService interface {
	DispatchTasks(ctx context.Context, orgID, projectID string) ([]DispatchResult, error)
	// MarkVerificationFailed transitions a task in_progress →
	// verification_failed when the agent reports its pre-PR integration
	// check failed. The PR stays a draft; the operator reviews the
	// diagnostic from the issue comment and clicks Retry (RetryTask).
	// Idempotent: subsequent calls on an already-verification_failed
	// task are absorbed (logged as a late event, no error).
	MarkVerificationFailed(ctx context.Context, taskID, diagnostic string) error
	// RetryTask transitions verification_failed → in_progress, clears
	// DispatchedAt + LastCodingAgentRunName, and re-dispatches the task
	// so a fresh WorkflowRun is created with a freshly minted bearer.
	// Returns the resulting DispatchResult.
	RetryTask(ctx context.Context, taskID string) (DispatchResult, error)
}

// Consumer ports for collaborators owned by other features/platform layers
// (auth.TaskTokenManager, idp, runtimeconfig). Defined here (consumer side)
// and satisfied structurally by the concretes, wired at the composition
// root — so this service needn't import those packages directly.
type (
	// taskTokenIssuer mints the per-task RS256 bearer (auth.TaskTokenManager).
	taskTokenIssuer interface {
		Issue(taskID, ocOrgID, projectID string) (string, error)
	}
	// OrgPublisherProvisioner is idp's get-or-create publisher hook.
	OrgPublisherProvisioner interface {
		EnsureOrgPublisher(ctx context.Context, orgID, actor string) (clientID, clientSecret string, created bool, err error)
	}
	// RuntimeConfigEmitter writes env-config.js onto a web-app's ReleaseBindings.
	RuntimeConfigEmitter interface {
		EmitForComponent(ctx context.Context, orgID, projectID, componentName string) error
	}
	// OrgServiceResolver resolves a cross-project `org-service` name to its
	// namespace-visible provider endpoint (target project/component/endpoint).
	// Satisfied by *connections.OrgEndpointCatalog; used by the dispatch-time
	// consumer-dependency YAML renderer. ok=false ⇒ not yet published.
	OrgServiceResolver interface {
		ResolveNamespaceVisible(ctx context.Context, orgHandle, name string) (openchoreo.WorkloadEndpointInfo, bool, error)
	}
	// ConnectionBindingReader reads a connection's per-env
	// ResourceReleaseBinding so the renderer can enumerate its resolved
	// outputs. Satisfied by openchoreo.ResourceClient.GetBinding (narrowed).
	ConnectionBindingReader func(ctx context.Context, namespace, name string) (*openchoreo.ResourceReleaseBinding, error)
	// IssueCommenter posts a comment to a task's GitHub issue. Satisfied by
	// gitrepo.IssueService.CommentIssue (adapted at the composition root) — a
	// narrow func so this feature needn't import the full issue service.
	IssueCommenter func(ctx context.Context, orgID, projectID string, number int, body string) error
)

type dispatchService struct {
	taskRepo          repositories.TaskRepository
	repoSvc           gitrepo.RepoService
	credSvc           *orgcreds.CredentialService
	anthropicSvc      *orgcreds.AnthropicCredentialService
	repoBoardSvc      gitrepo.RepoBoardService
	componentSvc      component.ComponentService
	configSvc         component.ConfigService
	store             *artifacts.ArtifactStore
	taskTokens        taskTokenIssuer
	asServiceIdentity func(ctx context.Context) context.Context
	wfRunService      WorkflowRunService
	projector         TaskStateProjector
	gitServiceURL     string // URL the agent pod uses to reach git-service; cross-namespace FQDN in cluster
	platformURL       string // URL the agent pod uses to call the BFF verification-failed callback
	// traitSync, when non-nil, is invoked after CreateComponent to
	// reconcile per-environment trait configs (the part CreateComponent
	// can't pre-stamp because RBs are created asynchronously by OC's
	// autoDeploy controller). Set via WithTraitSync. Optional in tests.
	traitSync          *component.TraitSyncService
	connSecretResolver ConnectionSecretResolveFunc
	// codingAgentDispatcher, when non-nil, is the proxy-based dispatch
	// path (NS + SA + ExternalSecret×2 + Job). When the dispatcher is
	// wired AND the per-org SM-API triplets are populated, the new path
	// runs and the legacy wfRunService.TriggerCodingAgent is skipped.
	// Both being absent keeps the legacy ClusterWorkflow path live.
	codingAgentDispatcher *Dispatcher

	// db backs the SM-API triplet lookup; nil disables the new
	// dispatch path even when codingAgentDispatcher is set. Wired by
	// main.go.
	db *gorm.DB

	// clusterSecretStore is the ESO ClusterSecretStore name the
	// per-run ExternalSecrets target. On DP this is `secretstore-read`;
	// local k3d reuses `default` (see deployments/docker-compose.yml).
	clusterSecretStore string

	// runnerImage is the docker image the per-run Job uses. Pinned by
	// the BFF from cfg.AgentRunnerImage.
	runnerImage string

	// runtimeConfig, when non-nil, writes `env-config.js` onto each
	// web-app's ReleaseBindings after CreateComponent. The SPA loads
	// the file synchronously before its bundle so `window._env_` is
	// populated before any React module runs — no rebuild needed when
	// per-env values change. Wired via SetRuntimeConfig.
	runtimeConfig RuntimeConfigEmitter

	// idp, when non-nil, provisions the per-org Thunder publisher
	// client_credentials on demand so the coding-agent runner can
	// authenticate to the BFF through the IdP-authed gateway.
	// Decoupled from API security: trait_sync only provisions it on the
	// first protected deploy, but the runner needs it for every component,
	// so the dispatch pre-flight ensures it too. Wired via SetIDPService.
	idp OrgPublisherProvisioner

	// orgServiceResolver + connBindingReader + issueCommenter back the
	// additive dispatch-time consumer-dependency comment (Phase A): when a
	// task is dispatched, the BFF resolves the component's `org-service` and
	// `external` connection deps and posts the exact `workload.yaml`
	// `dependencies:` block to the task's GitHub issue so the coding agent
	// declares them. Best-effort — all three nil ⇒ no comment is attempted.
	// This does NOT replace the post-deploy cascade wiring, which keeps
	// patching the resolved Workload deps after the provider deploys. Wired
	// via SetConsumerDependencyResolver.
	orgServiceResolver OrgServiceResolver
	connBindingReader  ConnectionBindingReader
	issueCommenter     IssueCommenter
}

// SetConsumerDependencyResolver wires the dispatch-time consumer-dependency
// comment (Phase A, additive): the org-service resolver + connection-binding
// reader resolve the component's deps and issueCommenter posts the resolved
// `workload.yaml` block to the task's GitHub issue. Optional — leaving any
// arg nil disables the comment (the post-deploy cascade still wires deps).
func (s *dispatchService) SetConsumerDependencyResolver(
	r OrgServiceResolver,
	b ConnectionBindingReader,
	c IssueCommenter,
) {
	s.orgServiceResolver = r
	s.connBindingReader = b
	s.issueCommenter = c
}

// DispatchServiceWithConsumerDeps surfaces the consumer-dependency setter so
// the composition root wires it by type-assertion (drift = build failure).
type DispatchServiceWithConsumerDeps interface {
	SetConsumerDependencyResolver(OrgServiceResolver, ConnectionBindingReader, IssueCommenter)
}

// WithCodingAgentDispatcher wires the proxy-based dispatch path.
// db is required for the SM-API triplet lookup; clusterSecretStore +
// runnerImage are pinned by the caller. Returns the receiver for
// chained construction.
func (s *dispatchService) WithCodingAgentDispatcher(d *Dispatcher, db *gorm.DB, clusterSecretStore, runnerImage string) DispatchService {
	s.codingAgentDispatcher = d
	s.db = db
	s.clusterSecretStore = clusterSecretStore
	s.runnerImage = runnerImage
	return s
}

// DispatchServiceWithTraitSync surfaces the trait_sync setter without
// polluting the public DispatchService interface (parallels the
// DesignServiceWithTaskHook pattern in design_service.go).
type DispatchServiceWithTraitSync interface {
	DispatchService
	SetTraitSync(traitSync *component.TraitSyncService)
}

// SetIDPService wires the per-org Thunder publisher provisioning hook used
// by the proxy dispatch pre-flight (runner-auth). Optional — when
// unset (e.g. local k3d without Thunder), the dispatch path falls back to
// the per-task RS256 JWT.
func (s *dispatchService) SetIDPService(idp OrgPublisherProvisioner) {
	s.idp = idp
}

// SetRuntimeConfig installs the env-config.js emitter that writes
// per-env values onto each web-app's ReleaseBindings. Call after
// NewDispatchService in production wiring.
func (s *dispatchService) SetRuntimeConfig(r RuntimeConfigEmitter) {
	s.runtimeConfig = r
}

// SetTraitSync installs the shared trait_sync emitter. Call after
// NewDispatchService in production wiring.
func (s *dispatchService) SetTraitSync(traitSync *component.TraitSyncService) {
	s.traitSync = traitSync
}

// ConnectionSecretResolveFunc resolves the per-run ExternalSecret inputs for the
// external connections a task binds (vault path + secret keys), for one env. The
// composition root adapts connections.Provisioner.ResolveRunnerSecrets to it.
type ConnectionSecretResolveFunc func(ctx context.Context, orgHandle, projectName, env string, connNames []string) ([]ConnectionSecretInputs, error)

// SetConnectionSecretResolver wires the resolver so the runner pod receives the
// task's bound connection secrets via envFrom (the agent integration-tests
// against the live service). Optional — nil skips connection secrets.
func (s *dispatchService) SetConnectionSecretResolver(f ConnectionSecretResolveFunc) {
	s.connSecretResolver = f
}

// DispatchServiceWith* surface the optional setters the composition root
// wires by type-assertion onto the DispatchService interface. Naming them
// (and asserting the concrete satisfies them below) makes a setter-signature
// drift a build failure instead of a wire silently skipped at boot.
type DispatchServiceWithIDP interface {
	SetIDPService(OrgPublisherProvisioner)
}
type DispatchServiceWithRuntimeConfig interface {
	SetRuntimeConfig(RuntimeConfigEmitter)
}
type DispatchServiceWithCodingAgent interface {
	WithCodingAgentDispatcher(*Dispatcher, *gorm.DB, string, string) DispatchService
}

var (
	_ DispatchServiceWithTraitSync     = (*dispatchService)(nil)
	_ DispatchServiceWithIDP           = (*dispatchService)(nil)
	_ DispatchServiceWithRuntimeConfig = (*dispatchService)(nil)
	_ DispatchServiceWithCodingAgent   = (*dispatchService)(nil)
	_ DispatchServiceWithConsumerDeps  = (*dispatchService)(nil)
)

func NewDispatchService(
	taskRepo repositories.TaskRepository,
	repoSvc gitrepo.RepoService,
	credSvc *orgcreds.CredentialService,
	anthropicSvc *orgcreds.AnthropicCredentialService,
	repoBoardSvc gitrepo.RepoBoardService,
	componentSvc component.ComponentService,
	configSvc component.ConfigService,
	store *artifacts.ArtifactStore,
	taskTokens taskTokenIssuer,
	asServiceIdentity func(ctx context.Context) context.Context,
	wfRunService WorkflowRunService,
	projector TaskStateProjector,
	gitServiceURL string,
	platformURL string,
) DispatchService {
	return &dispatchService{
		taskRepo:          taskRepo,
		repoSvc:           repoSvc,
		credSvc:           credSvc,
		anthropicSvc:      anthropicSvc,
		repoBoardSvc:      repoBoardSvc,
		componentSvc:      componentSvc,
		configSvc:         configSvc,
		store:             store,
		taskTokens:        taskTokens,
		asServiceIdentity: asServiceIdentity,
		wfRunService:      wfRunService,
		projector:         projector,
		gitServiceURL:     gitServiceURL,
		platformURL:       platformURL,
	}
}

func (s *dispatchService) DispatchTasks(ctx context.Context, orgID, projectID string) ([]DispatchResult, error) {
	tasks, err := s.taskRepo.ListByProjectID(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	repoInfo, err := s.repoSvc.GetRepo(ctx, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	if repoInfo == nil {
		return nil, fmt.Errorf("project repo not provisioned")
	}
	if repoInfo.DefaultBranch == "" {
		repoInfo.DefaultBranch = "main"
	}
	identity, err := s.credSvc.IdentityFor(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("get credential identity: %w", err)
	}

	// Deploy-gating. Build a {componentName → status} index for
	// dependsOn resolution. A task is dispatchable only when every task
	// it dependsOn (by component name) has reached `deployed`. This is
	// per-batch — DependsOnComponents lists names that map 1:1 to tasks
	// in the same batch (validated at persist time in task_stream.go).
	statusByComponent := make(map[string]string, len(tasks))
	statusByConnection := make(map[string]string, len(tasks))
	for _, t := range tasks {
		if t.Type == models.TaskTypeConfigCollection {
			if t.ConnectionName != "" {
				statusByConnection[t.ConnectionName] = t.Status
			}
			continue
		}
		statusByComponent[t.ComponentName] = t.Status
	}

	// Cross-project `org-service` gate (declarative-wiring refactor): a consumer
	// is held On Hold until each org-service target is deployed + namespace-
	// visible in the org catalog, so at dispatch the dependency is fully
	// resolvable (the issue comment + the agent's workload.yaml block can name a
	// real endpoint). nil ⇒ catalog not wired (tests) ⇒ org-service gate off.
	orgServiceVisible := s.orgServiceGates(ctx, orgID, tasks)

	var results []DispatchResult
	for i := range tasks {
		task := &tasks[i]
		// config-collection tasks are completed by the value-save endpoint, not
		// the coding agent — never dispatch them (reaching `deployed` = provisioned).
		if task.Type == models.TaskTypeConfigCollection {
			continue
		}
		if task.Status == string(models.TaskStatusOnHold) {
			if !depsAllDeployed(task, statusByComponent, statusByConnection, orgServiceVisible) {
				continue
			}
			task.Status = string(models.TaskStatusPending)
			if err := s.taskRepo.Update(ctx, task); err != nil {
				slog.WarnContext(ctx, "clear on_hold", "task", task.ID, "error", err)
				continue
			}
		}
		if task.Status != string(models.TaskStatusPending) {
			continue
		}

		if !depsAllDeployed(task, statusByComponent, statusByConnection, orgServiceVisible) {
			task.Status = string(models.TaskStatusOnHold)
			if err := s.taskRepo.Update(ctx, task); err != nil {
				slog.WarnContext(ctx, "set on_hold", "task", task.ID, "error", err)
			}
			if task.IssueURL != "" {
				if err := s.repoBoardSvc.MoveIssueToStatus(ctx, task.OrgID, task.ProjectID, task.IssueURL, "On Hold"); err != nil {
					slog.WarnContext(ctx, "failed to move board item to On Hold",
						"task", task.ID, "error", err)
				}
			}
			continue
		}

		res := s.dispatchOne(ctx, task, repoInfo, identity)
		results = append(results, res)
	}

	return results, nil
}

// orgServiceGates resolves, once per dispatch sweep, which distinct
// `org-service` names referenced by the project's tasks are currently
// namespace-visible in the org catalog (provider deployed + published). The
// result feeds depsAllDeployed's org-service gate. Returns nil when the catalog
// resolver isn't wired (tests/legacy) — nil disables the gate. Fail-closed on a
// per-name resolve error (left false → the consumer stays On Hold; the 10s
// on_hold watcher retries), so a transient OC blip delays rather than mis-fires.
func (s *dispatchService) orgServiceGates(ctx context.Context, orgID string, tasks []models.ComponentTask) map[string]bool {
	if s.orgServiceResolver == nil {
		return nil
	}
	visible := map[string]bool{}
	for ti := range tasks {
		for _, name := range tasks[ti].DependsOnOrgServices {
			if _, seen := visible[name]; seen {
				continue
			}
			_, ok, err := s.orgServiceResolver.ResolveNamespaceVisible(ctx, orgID, name)
			if err != nil {
				slog.WarnContext(ctx, "org-service gate: resolve failed (holding consumer)",
					"org", orgID, "orgService", name, "error", err)
				ok = false
			}
			visible[name] = ok
		}
	}
	return visible
}

// depsAllDeployed returns true when every blocker the task lists is satisfied:
// each DependsOnComponents name maps to a task whose Status == deployed, AND
// each DependsOnConnections name maps to a config-collection task whose
// Status == deployed (i.e. the connection's values were collected + the OC
// Resource model provisioned), AND — when orgServiceVisible is non-nil — each
// DependsOnOrgServices name resolves to a namespace-visible provider endpoint
// in the org catalog (i.e. the cross-project provider is deployed + published).
// Unknown names return false (fail closed; the persist-time validator in
// task_stream.go is the upstream guard). orgServiceVisible == nil disables the
// org-service gate (catalog not wired — tests/legacy), keeping the others.
func depsAllDeployed(task *models.ComponentTask, statusByComponent, statusByConnection map[string]string, orgServiceVisible map[string]bool) bool {
	for _, depComponent := range task.DependsOnComponents {
		if statusByComponent[depComponent] != string(models.TaskStatusDeployed) {
			return false
		}
	}
	for _, depConn := range task.DependsOnConnections {
		if statusByConnection[depConn] != string(models.TaskStatusDeployed) {
			return false
		}
	}
	if orgServiceVisible != nil {
		for _, depOrg := range task.DependsOnOrgServices {
			if !orgServiceVisible[depOrg] {
				return false
			}
		}
	}
	return true
}

// dispatchOne drives the idempotency contract for a single task.
func (s *dispatchService) dispatchOne(
	ctx context.Context,
	task *models.ComponentTask,
	repoInfo *models.GitRepository,
	identity *orgcreds.Identity,
) DispatchResult {
	res := DispatchResult{TaskID: task.ID, ComponentName: task.ComponentName}

	if task.IssueNumber == 0 || task.IssueURL == "" {
		s.markFailed(ctx, task, "no GitHub issue on task — generation must precede dispatch")
		return failResult(res, task.ErrorMessage)
	}

	if s.componentSvc != nil {
		if err := s.ensureOCComponent(ctx, task, repoInfo); err != nil {
			s.markFailed(ctx, task, fmt.Sprintf("ensure OC component: %v", err))
			return failResult(res, task.ErrorMessage)
		}
	}

	if s.taskTokens == nil {
		s.markFailed(ctx, task, "task token manager not configured")
		return failResult(res, task.ErrorMessage)
	}
	bearer, err := s.taskTokens.Issue(task.ID, task.OrgID, task.ProjectID)
	if err != nil {
		s.markFailed(ctx, task, fmt.Sprintf("issue task jwt: %v", err))
		return failResult(res, task.ErrorMessage)
	}

	if s.wfRunService == nil {
		s.markFailed(ctx, task, "workflow run service not configured")
		return failResult(res, task.ErrorMessage)
	}
	// Assert that every component this task depends on has a non-empty
	// external URL at dispatch time. Under deploy-gating, every dep is
	// `deployed` at this point so ListDeployments must
	// return a non-empty external URL — if any URL is empty, the deps
	// invariant is broken (probably a missing `visibility: external` on
	// the provider's spec.endpoints) and we fail loudly rather than
	// dispatching a task that will fail to verify.
	//
	// The resolved URLs are NOT passed through the prompt. SPAs receive
	// them at runtime via `window._env_` (BFF writes per-env values into
	// `env-config.js` on each ReleaseBinding). Keeping the prompt thin
	// matches both cluster and local flows.
	depEndpoints, err := s.resolveDependencyEndpoints(ctx, task)
	if err != nil {
		const deferDeadline = 2 * time.Minute
		now := time.Now()
		if task.DispatchDeferredAt != nil && time.Since(*task.DispatchDeferredAt) > deferDeadline {
			// Deadline exceeded — not a timing race, genuine misconfiguration.
			s.markFailed(ctx, task, fmt.Sprintf("resolve dependency endpoints: %v", err))
			return failResult(res, task.ErrorMessage)
		}
		// First attempt or still within deadline — the OC ReleaseBinding
		// controller may not have resolved the external URL yet (timing race
		// between build WorkflowRun completion and ingress provisioning).
		// Revert to on_hold; the on_hold_watcher retries every 10s.
		if task.DispatchDeferredAt == nil {
			task.DispatchDeferredAt = &now
		}
		task.Status = string(models.TaskStatusOnHold)
		task.ErrorMessage = fmt.Sprintf("resolve dependency endpoints: %v", err)
		if err := s.taskRepo.Update(ctx, task); err != nil {
			slog.WarnContext(ctx, "dispatchOne: revert to on_hold failed", "task", task.ID, "error", err)
		}
		if task.IssueURL != "" {
			if err := s.repoBoardSvc.MoveIssueToStatus(ctx, task.OrgID, task.ProjectID, task.IssueURL, "On Hold"); err != nil {
				slog.WarnContext(ctx, "dispatchOne: move board item to On Hold", "task", task.ID, "error", err)
			}
		}
		slog.WarnContext(ctx, "dispatch deferred: dep external URL not yet available",
			"task", task.ID, "deferredAt", task.DispatchDeferredAt, "deadline", deferDeadline)
		return failResult(res, task.ErrorMessage)
	}
	// URL resolved — clear the deferred timestamp from any prior attempts.
	task.DispatchDeferredAt = nil
	prompt := buildAgentPrompt(task)
	slog.InfoContext(ctx, "dispatched with dep endpoints",
		"task", task.ID,
		"component", task.ComponentName,
		"deps", depEndpoints,
	)

	// Per-dispatch pre-flight: ensure the org has an active Anthropic key,
	// then SSA-refresh the WP Secret. Returns orgcreds.ErrAnthropicKeyRequired when
	// the org row is missing or inactive; we surface that as a structured
	// failure rather than markFailed so the console can offer "configure
	// key" instead of "retry". See docs/design/anthropic-key-dual-token.md §6.2.
	anthropicRes, err := s.anthropicSvc.ApplyWPSecret(ctx, task.OrgID)
	if err != nil {
		if errors.Is(err, orgcreds.ErrAnthropicKeyRequired) {
			s.markFailed(ctx, task, "anthropic_key_required: configure an Anthropic API key in org settings before dispatching the remote coding agent")
			res = failResult(res, task.ErrorMessage)
			res.Error = "anthropic_key_required"
			return res
		}
		s.markFailed(ctx, task, fmt.Sprintf("apply anthropic wp-secret: %v", err))
		return failResult(res, task.ErrorMessage)
	}

	// New dispatch path. When the proxy-based dispatcher is wired AND
	// the per-org SM-API triplets are populated, dispatch goes through
	// cluster-gateway-proxy (NS + SA + ExternalSecret×2 + Job) instead
	// of the legacy ClusterWorkflow path. Fall back to the legacy
	// `wfRunService.TriggerCodingAgent` when the proxy path's
	// prerequisites aren't satisfied — keeps mixed dev environments
	// working.
	var runName string
	used, runName, err := s.tryDispatchViaProxy(ctx, task, repoInfo.RepoURL, prompt, identity, bearer)
	if err != nil {
		s.markFailed(ctx, task, fmt.Sprintf("dispatch via proxy: %v", err))
		return failResult(res, task.ErrorMessage)
	}
	if !used {
		runName, err = s.wfRunService.TriggerCodingAgent(ctx, CodingAgentTrigger{
			Task:               task,
			RepoURL:            repoInfo.RepoURL,
			IdentityName:       identity.Name,
			IdentityEmail:      identity.Email,
			IdentityLogin:      identity.Login,
			Prompt:             prompt,
			Bearer:             bearer,
			GitServiceURL:      s.gitServiceURL,
			PlatformURL:        s.platformURL,
			AnthropicSecretRef: anthropicRes.SecretRefName,
		})
		if err != nil {
			s.markFailed(ctx, task, fmt.Sprintf("trigger coding-agent: %v", err))
			return failResult(res, task.ErrorMessage)
		}
	}

	now := time.Now()
	task.DispatchedAt = &now
	task.LastCodingAgentRunName = runName
	task.Status = string(models.TaskStatusInProgress)
	if err := s.taskRepo.Update(ctx, task); err != nil {
		slog.ErrorContext(ctx, "failed to update task after dispatch",
			"task", task.ID, "error", err)
	}

	// Move the GitHub Project board item to "In Progress" so the console
	// kanban reflects dispatch state immediately (GitHub does not do this
	// automatically on WorkflowRun creation).
	if err := s.repoBoardSvc.MoveIssueToStatus(ctx, task.OrgID, task.ProjectID, task.IssueURL, "In Progress"); err != nil {
		slog.WarnContext(ctx, "failed to move board item to In Progress",
			"task", task.ID, "error", err)
	}

	slog.InfoContext(ctx, "task dispatched",
		"task", task.ID, "component", task.ComponentName, "run", runName)

	// Additive (Phase A): post the resolved `workload.yaml` `dependencies:`
	// block to the issue so the coding agent declares the consumer deps up
	// front. Best-effort — never fails the dispatch. The post-deploy cascade
	// still patches the resolved deps onto the Workload after the provider
	// deploys, so this is purely a head-start for the agent.
	s.postConsumerDependencyComment(ctx, task)

	res.RunName = runName
	res.Status = "running"
	return res
}

// tryDispatchViaProxy is the proxy-based dispatch attempt. Returns
// (used=true, runName, nil) when dispatch succeeded; (used=false, "", nil)
// when prerequisites aren't met so the caller falls back to the legacy
// ClusterWorkflow path; (used=false, "", err) on actual failure.
//
// Prerequisites:
//   - codingAgentDispatcher wired (cluster-gateway-proxy client present);
//   - db wired (for the SM-API triplet lookup);
//   - runnerImage + clusterSecretStore configured;
//   - the org's anthropic + github credential rows carry the SM-API
//     triplet (populated by the Connect flow);
//   - the BFF has an Organization row with the OrgUUID for the NS derivation.
//
// When any of these fail, the function returns used=false with nil
// error and the legacy path runs — operators see a single log line per
// dispatch noting the fallback reason.
func (s *dispatchService) tryDispatchViaProxy(
	ctx context.Context,
	task *models.ComponentTask,
	repoURL, prompt string,
	identity *orgcreds.Identity,
	bearer string,
) (bool, string, error) {
	slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] tryDispatchViaProxy entry",
		"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
		"task", task.ID, "orgID", task.OrgID,
		"dispatcherNil", s.codingAgentDispatcher == nil, "dbNil", s.db == nil,
		"imageEmpty", s.runnerImage == "", "storeEmpty", s.clusterSecretStore == "")
	if s.codingAgentDispatcher == nil || s.db == nil {
		slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] fallback: dispatcher or db nil",
			"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
			"task", task.ID, "dispatcherNil", s.codingAgentDispatcher == nil, "dbNil", s.db == nil)
		return false, "", nil
	}
	if s.runnerImage == "" || s.clusterSecretStore == "" {
		slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] fallback: image or store empty",
			"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
			"task", task.ID, "imageEmpty", s.runnerImage == "", "storeEmpty", s.clusterSecretStore == "")
		slog.WarnContext(ctx, "proxy dispatch: missing runnerImage or clusterSecretStore — falling back to legacy path",
			"task", task.ID)
		return false, "", nil
	}

	// SM-API triplets — fetched in one round-trip each from the
	// per-org credential rows. The Connect flow guarantees these are
	// stamped together (in the same tx as the encrypted blob), so a
	// half-populated row is not expected.
	var (
		anthropicRow models.OrgAnthropicCredential
		githubRow    models.OrgCredential
	)
	if err := s.db.WithContext(ctx).Where("oc_org_id = ?", task.OrgID).First(&anthropicRow).Error; err != nil {
		slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] fallback: anthropic row missing",
			"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
			"task", task.ID, "ocOrgId", task.OrgID, "error", err.Error())
		slog.InfoContext(ctx, "proxy dispatch: anthropic row missing; falling back",
			"task", task.ID, "ocOrgId", task.OrgID, "error", err)
		return false, "", nil
	}
	if err := s.db.WithContext(ctx).Where("oc_org_id = ?", task.OrgID).First(&githubRow).Error; err != nil {
		slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] fallback: github row missing",
			"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
			"task", task.ID, "ocOrgId", task.OrgID, "error", err.Error())
		slog.InfoContext(ctx, "proxy dispatch: github row missing; falling back",
			"task", task.ID, "ocOrgId", task.OrgID, "error", err)
		return false, "", nil
	}
	if anthropicRow.SMAPIKVPath == nil || githubRow.SMAPIKVPath == nil {
		slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] fallback: SM-API triplet nil",
			"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
			"task", task.ID,
			"anthropicMissing", anthropicRow.SMAPIKVPath == nil,
			"githubMissing", githubRow.SMAPIKVPath == nil)
		slog.InfoContext(ctx, "proxy dispatch: SM-API triplet missing on credential row(s); falling back",
			"task", task.ID,
			"anthropicMissing", anthropicRow.SMAPIKVPath == nil,
			"githubMissing", githubRow.SMAPIKVPath == nil)
		return false, "", nil
	}

	// Publisher cc creds. The runner authenticates to the BFF through
	// the IdP-authed gateway with a Thunder publisher client_credentials token
	// (a real platform-idp JWT). trait_sync only provisions that publisher on
	// the first *protected* deploy, but the coding-agent runs for every
	// component, so ensure it here too (idempotent get-or-create + SM-API
	// mirror). When the IDP profile carries the SM-API triplet, the dispatcher
	// emits a third per-run ExternalSecret materialising PUBLISHER_CLIENT_ID +
	// PUBLISHER_CLIENT_SECRET into the runner pod.
	var (
		publisherSR       *SecretRef
		publisherTokenURL string
	)
	var pubErr error
	if s.idp != nil {
		if _, _, _, perr := s.idp.EnsureOrgPublisher(ctx, task.OrgID, "dispatch"); perr != nil {
			pubErr = perr
			slog.ErrorContext(ctx, "proxy dispatch: EnsureOrgPublisher FAILED — the runner's publisher cc token may be invalid (stale/missing creds); cc auth will likely surface as invalid_client (401)",
				"task", task.ID, "org", task.OrgID, "error", perr)
		}
	}
	var idpRow models.OrganizationIDPProfile
	if err := s.db.WithContext(ctx).Where("org_id = ?", task.OrgID).First(&idpRow).Error; err == nil {
		if idpRow.SMAPIKVPath != nil && idpRow.SMAPISecretRefName != nil {
			publisherSR = &SecretRef{
				SecretRefName: derefStr(idpRow.SMAPISecretRefName),
				KVPath:        derefStr(idpRow.SMAPIKVPath),
				Property:      derefStr(idpRow.SMAPIProperty),
			}
			publisherTokenURL = deriveTokenURLFromJWKS(idpRow.JWKSURL)
			if publisherTokenURL == "" {
				slog.WarnContext(ctx, "proxy dispatch: publisher creds present but JWKS URL malformed; falling back to bearer only",
					"task", task.ID, "jwksURL", idpRow.JWKSURL)
				publisherSR = nil
			}
		}
	}

	// In cloud the runner reaches the BFF through the IdP-authed gateway, so a
	// per-task RS256 JWT is rejected there (401) — the publisher cc is
	// mandatory. Fail the dispatch loudly rather than launch a runner that
	// cannot authenticate (which would surface as an opaque
	// "git-service returned 401" deep in the runner). Local k3d (http platform
	// URL, no gateway) keeps using the per-task bearer fallback.
	slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] publisher cc check",
		"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
		"task", task.ID, "org", task.OrgID, "publisherSRPresent", publisherSR != nil)
	if publisherSR != nil {
		if pubErr != nil {
			slog.WarnContext(ctx, "proxy dispatch: publisher cc path active but provisioning FAILED this run — runner will use possibly-stale publisher creds (expect cc-token invalid_client if the OU/secret drifted)",
				"task", task.ID, "org", task.OrgID, "ensureErr", pubErr)
		} else {
			slog.InfoContext(ctx, "proxy dispatch: publisher cc path active",
				"task", task.ID, "org", task.OrgID)
		}
	} else if isGatewayPlatformURL(s.platformURL) {
		slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] fail: publisher cc missing on gateway platform",
			"gatewayPlatform", true, "task", task.ID, "org", task.OrgID)
		return false, "", fmt.Errorf("publisher cc not provisioned for org %q: the coding-agent runner cannot authenticate to the BFF through the gateway (a per-task JWT is rejected). Ensure Thunder + SM-API are healthy so the publisher can be provisioned and mirrored", task.OrgID)
	}

	// OrgUUID lookup. The BFF Organization row carries the UUID the
	// NS derivation needs (`wc-<orgUUID8>-<orgHash8>-remote-worker`).
	orgUUID, err := s.lookupOrgUUID(ctx, task.OrgID)
	if err != nil {
		slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] fallback: org UUID not found",
			"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
			"task", task.ID, "ocOrgId", task.OrgID, "error", err.Error())
		slog.InfoContext(ctx, "proxy dispatch: org UUID not found; falling back",
			"task", task.ID, "ocOrgId", task.OrgID, "error", err)
		return false, "", nil
	}
	slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] proxy dispatch proceeding",
		"gatewayPlatform", isGatewayPlatformURL(s.platformURL),
		"task", task.ID, "ocOrgId", task.OrgID, "orgUUID", orgUUID,
		"remoteWorkerNamespace", tenant.RemoteWorkerNamespace(orgUUID))

	runName := codingAgentRunName(task)
	job := JobInputs{
		RunName:       runName,
		TaskID:        task.ID,
		OrgID:         task.OrgID,
		ProjectID:     task.ProjectID,
		ComponentName: task.ComponentName,
		RunnerImage:   s.runnerImage,
		RepoURL:       repoURL,
		Prompt:        prompt,
		IdentityName:  identity.Name,
		IdentityEmail: identity.Email,
		IdentityLogin: identity.Login,
		GitServiceURL: s.gitServiceURL,
		CallbackURL:   s.platformURL,
		// `ASDLC_BEARER` carries the per-task RS256 JWT path on the new
		// dispatcher. The runner's `oneshot.ts` validates the env var at
		// startup and uses it for /credentials/refresh callbacks.
		// PublisherSR (below) populates a 3rd per-run ExternalSecret; the
		// TS runner prefers the publisher cc when PUBLISHER_CLIENT_ID is
		// present and falls back to Bearer otherwise.
		Bearer:            bearer,
		PublisherTokenURL: publisherTokenURL,
	}

	// External connections the task binds: materialise their secrets into the
	// runner via per-run ExternalSecrets so the agent integration-tests against
	// the live service. Best-effort — a resolve hiccup must not block dispatch.
	var connSRs []ConnectionSecretInputs
	if s.connSecretResolver != nil && len(task.DependsOnConnections) > 0 {
		if resolved, rerr := s.connSecretResolver(ctx, task.OrgID, task.ProjectID, "development", []string(task.DependsOnConnections)); rerr != nil {
			slog.WarnContext(ctx, "resolve connection runner secrets failed", "task", task.ID, "error", rerr)
		} else {
			connSRs = resolved
		}
	}

	rn, err := s.codingAgentDispatcher.Dispatch(ctx, Inputs{
		OrgUUID:                orgUUID,
		Job:                    job,
		AnthropicSR:            SecretRef{SecretRefName: derefStr(anthropicRow.SMAPISecretRefName), KVPath: derefStr(anthropicRow.SMAPIKVPath), Property: derefStr(anthropicRow.SMAPIProperty)},
		GitHubSR:               SecretRef{SecretRefName: derefStr(githubRow.SMAPISecretRefName), KVPath: derefStr(githubRow.SMAPIKVPath), Property: derefStr(githubRow.SMAPIProperty)},
		PublisherSR:            publisherSR,
		ConnectionSRs:          connSRs,
		ClusterSecretStoreName: s.clusterSecretStore,
	})
	if err != nil {
		return false, "", err
	}
	return true, rn, nil
}

func (s *dispatchService) lookupOrgUUID(ctx context.Context, ocOrgID string) (string, error) {
	var org models.Organization
	if err := s.db.WithContext(ctx).Where("name = ?", ocOrgID).First(&org).Error; err != nil {
		return "", err
	}
	// Prefer the Thunder-issued ouId persisted on `thunder_org_uuid`
	// (the authoritative UUID that SM-API also derives NS from).
	// Fall back to the local PK `uuid` only when the row predates the
	// orgensure lazy-fill — in that case the NS will mismatch and the
	// proxy path will silently fail, but we let it through so legacy
	// callers don't lose dispatch capability mid-rollout.
	if org.ThunderOrgUUID != nil && *org.ThunderOrgUUID != uuid.Nil {
		slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] lookupOrgUUID using thunder_org_uuid",
			"ocOrgId", ocOrgID, "branch", "thunder-org-uuid", "returned", org.ThunderOrgUUID.String())
		return org.ThunderOrgUUID.String(), nil
	}
	if org.UUID == uuid.Nil {
		return "", fmt.Errorf("organization %s has no UUID", ocOrgID)
	}
	slog.InfoContext(ctx, "[SHAKEOUT:DISPATCH] lookupOrgUUID using local PK fallback",
		"ocOrgId", ocOrgID, "branch", "local-pk-fallback", "returned", org.UUID.String())
	slog.WarnContext(ctx, "dispatch: thunder_org_uuid missing on org row; falling back to local PK (NS derivation will likely mismatch SM-API)",
		"name", ocOrgID, "uuid", org.UUID.String())
	return org.UUID.String(), nil
}

// codingAgentRunName derives a deterministic run name from the task ID
// + a UTC minute bucket. Same task dispatched twice in the same minute
// reuses the run name (the Job is immutable so ApplyJob does a
// DELETE+POST, restarting the agent). Bucket is intentionally coarse
// so retries within a minute are idempotent.
func codingAgentRunName(task *models.ComponentTask) string {
	min := time.Now().UTC().Format("0601021504")
	shortID := task.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	name := fmt.Sprintf("ca-%s-%s", shortID, min)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// isGatewayPlatformURL reports whether the runner reaches the BFF through the
// IdP-authed cloud gateway (https) rather than an internal http URL (local
// k3d). On the gateway path a per-task JWT is rejected, so the publisher cc is
// required; off it (http) the bearer fallback still works.
func isGatewayPlatformURL(u string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(u)), "https://")
}

// deriveTokenURLFromJWKS swaps the trailing `/oauth2/jwks` path on the
// org's JWKS URL for `/oauth2/token`. The two endpoints live on the same
// Thunder host so this is safe; returns "" on shapes we don't recognise
// (caller skips the publisher path in that case).
func deriveTokenURLFromJWKS(jwksURL string) string {
	const suffix = "/oauth2/jwks"
	if !strings.HasSuffix(jwksURL, suffix) {
		return ""
	}
	return jwksURL[:len(jwksURL)-len(suffix)] + "/oauth2/token"
}

func failResult(r DispatchResult, msg string) DispatchResult {
	r.Status = "failed"
	r.Error = msg
	return r
}

func (s *dispatchService) markFailed(ctx context.Context, task *models.ComponentTask, msg string) {
	task.Status = string(models.TaskStatusFailed)
	task.ErrorMessage = msg
	if err := s.taskRepo.Update(ctx, task); err != nil {
		slog.ErrorContext(ctx, "failed to mark task failed", "task", task.ID, "error", err)
	}
	slog.ErrorContext(ctx, "dispatch step failed", "task", task.ID, "error", msg)
	// Sync the GitHub project board item so it surfaces in the Failed column.
	if task.IssueURL != "" {
		if err := s.repoBoardSvc.MoveIssueToStatus(ctx, task.OrgID, task.ProjectID, task.IssueURL, "Failed"); err != nil {
			slog.WarnContext(ctx, "markFailed: move board item to Failed", "task", task.ID, "error", err)
		}
	}
}

// MarkVerificationFailed transitions a task from in_progress to
// verification_failed under the projector's per-task lock. The
// diagnostic is persisted to ErrorMessage so it surfaces on the board
// card alongside the Retry button.
func (s *dispatchService) MarkVerificationFailed(ctx context.Context, taskID, diagnostic string) error {
	if s.projector == nil {
		return fmt.Errorf("verification-failed: projector not configured")
	}
	// Trim very long diagnostics; ErrorMessage is operator-visible.
	if len(diagnostic) > 4000 {
		diagnostic = diagnostic[:4000] + "…(truncated)"
	}
	if err := s.projector.ApplyBuildResult(ctx, taskID, contracts.TaskEventVerificationFailed, diagnostic); err != nil {
		return fmt.Errorf("apply verification-failed: %w", err)
	}
	slog.InfoContext(ctx, "task marked verification_failed",
		"task", taskID, "diagnostic", diagnostic)
	return nil
}

// RetryTask is the operator-driven retry path for a task in
// `verification_failed`. It:
//
//  1. Transitions verification_failed → in_progress via the projector
//     (TaskEventRetry).
//  2. Clears DispatchedAt + LastCodingAgentRunName + ErrorMessage so a
//     fresh WorkflowRun is created.
//  3. Calls dispatchOne to mint a new bearer + trigger a new agent pod
//     against the same component / issue / branch.
//
// The PR (if any) stays a draft; the new agent run pushes additional
// commits to the same feature branch. Idempotent on the retry trigger
// — calling twice in close succession re-applies the transition (the
// second call hits ErrInvalidTransition, treated as a no-op by the
// projector) but only the first dispatchOne wins on DispatchedAt.
func (s *dispatchService) RetryTask(ctx context.Context, taskID string) (DispatchResult, error) {
	if s.projector == nil {
		return DispatchResult{}, fmt.Errorf("retry: projector not configured")
	}
	if err := s.projector.ApplyBuildResult(ctx, taskID, contracts.TaskEventRetry, ""); err != nil {
		return DispatchResult{}, fmt.Errorf("apply retry: %w", err)
	}
	// Load fresh and clear the dispatch idempotency fields so the next
	// trigger creates a new WorkflowRun.
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("retry: load task: %w", err)
	}
	if task == nil {
		return DispatchResult{}, fmt.Errorf("retry: task not found")
	}
	task.DispatchedAt = nil
	task.LastCodingAgentRunName = ""
	task.ErrorMessage = ""
	// Transition above already wrote Status=in_progress; persist the
	// idempotency-field clears alongside it.
	if err := s.taskRepo.Update(ctx, task); err != nil {
		return DispatchResult{}, fmt.Errorf("retry: clear dispatch fields: %w", err)
	}
	// Re-dispatch — mirrors the DispatchTasks dispatchOne path. We don't
	// reuse DispatchTasks because that batches across the project and
	// would skip our just-cleared task (it's in_progress, not pending).
	repoInfo, err := s.repoSvc.GetRepo(ctx, task.OrgID, task.ProjectID)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("retry: get repo: %w", err)
	}
	if repoInfo == nil {
		return DispatchResult{}, fmt.Errorf("retry: project repo not provisioned")
	}
	if repoInfo.DefaultBranch == "" {
		repoInfo.DefaultBranch = "main"
	}
	identity, err := s.credSvc.IdentityFor(ctx, task.OrgID)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("retry: get identity: %w", err)
	}
	// dispatchOne doesn't gate on Status (the gating lives in
	// DispatchTasks); it triggers a fresh WorkflowRun and persists
	// DispatchedAt + LastCodingAgentRunName + Status=in_progress on success.
	res := s.dispatchOne(ctx, task, repoInfo, identity)
	slog.InfoContext(ctx, "task retried after verification_failed",
		"task", taskID, "status", res.Status)
	return res, nil
}

// DependencyEndpoint is one row used by resolveDependencyEndpoints — the
// dispatch-time §1.3 invariant guard ("every dep this task lists has a
// non-empty external URL"). The URL handoff to the SPA flows through the
// ReleaseBinding `env-config.js` (BFF emits per-env values into
// workloadOverrides.container.files).
type DependencyEndpoint struct {
	Component string
	URL       string
}

// buildAgentPrompt returns the user prompt given to the Claude agent. The
// full task context lives in the GitHub issue body
// (services/issue_body.go); the prompt only points the agent at the issue
// and reminds it how to link the PR back. Dependency URLs reach the SPA
// at runtime via `window._env_` (written into `env-config.js` by the BFF
// at ReleaseBinding time) — not through prompts or issue comments.
func buildAgentPrompt(task *models.ComponentTask) string {
	return fmt.Sprintf(
		"Work on this GitHub issue: %s\n\n"+
			"You are at the project repo root, on its default branch. Create your "+
			"own feature branch, implement the task, and open a PR whose body "+
			"includes the literal text `Closes #%d` so the platform links the "+
			"PR back to this task.",
		task.IssueURL, task.IssueNumber,
	)
}

// resolveDependencyEndpoints turns the task's DependsOnComponents list into
// a slice of (component, url) pairs by calling ComponentService.ListDeployments
// — the same path that powers the Deploy page (single source of truth). Under
// deploy-gating every dep is `deployed` at dispatch time, so each
// ListDeployments call MUST return a non-empty external URL. An empty URL
// means the provider component is missing `visibility: external` on its
// `spec.endpoints` — that is the §1.3 invariant breaking. Fail loudly here.
func (s *dispatchService) resolveDependencyEndpoints(
	ctx context.Context,
	task *models.ComponentTask,
) ([]DependencyEndpoint, error) {
	if len(task.DependsOnComponents) == 0 || s.componentSvc == nil {
		return nil, nil
	}
	if s.asServiceIdentity != nil {
		ctx = s.asServiceIdentity(ctx)
	}
	out := make([]DependencyEndpoint, 0, len(task.DependsOnComponents))
	for _, depComponent := range task.DependsOnComponents {
		ocName := k8sname.ToK8sName(depComponent)
		list, err := s.componentSvc.ListDeployments(ctx, task.OrgID, task.ProjectID, ocName)
		if err != nil {
			return nil, fmt.Errorf("list deployments for %q: %w", depComponent, err)
		}
		url := firstExternalURL(list)
		if url == "" {
			return nil, fmt.Errorf(
				"dep %q has no external URL — confirm the provider's `workload.yaml` spec.endpoints declares `visibility: external` (see docs/design/cross-component-wiring-gaps.md §1.3)",
				depComponent,
			)
		}
		out = append(out, DependencyEndpoint{Component: depComponent, URL: url})
	}
	return out, nil
}

func firstExternalURL(list *models.DeploymentList) string {
	if list == nil {
		return ""
	}
	for _, d := range list.Items {
		if d.EndpointURL != "" {
			return d.EndpointURL
		}
	}
	return ""
}

// ---- Phase A: dispatch-time consumer-dependency comment --------------------
//
// The post-deploy cascade (ConsumerWiring.WireConsumer / WireOrgServiceConsumer)
// already patches the resolved deps onto the consumer Workload after the
// provider deploys — that stays. The comment below is ADDITIVE: it hands the
// coding agent the exact `dependencies:` block to author in its own
// `workload.yaml` up front, so the consumer's Workload declares the deps the
// moment it's built rather than only after the cascade re-drives. The two
// converge on the same OC flat WorkloadDescriptor shape.

// workloadDeps is the flat OC WorkloadDescriptor `dependencies:` block, rendered
// to the YAML the coding agent merges into its component's `workload.yaml`.
// Sub-keys are emitted only when non-empty (`omitempty`) so a connection-only
// component gets `resources:` without an empty `endpoints:` and vice-versa.
type workloadDeps struct {
	Endpoints []workloadEndpointDepYAML `yaml:"endpoints,omitempty"`
	Resources []workloadResourceDepYAML `yaml:"resources,omitempty"`
}

type workloadEndpointDepYAML struct {
	Project     string            `yaml:"project,omitempty"` // omit if same project
	Component   string            `yaml:"component"`
	Name        string            `yaml:"name"`
	Visibility  string            `yaml:"visibility"`
	EnvBindings map[string]string `yaml:"envBindings"` // {address: <ENV>}
}

type workloadResourceDepYAML struct {
	Ref         string            `yaml:"ref"`
	EnvBindings map[string]string `yaml:"envBindings"` // {<output>: <ENV>}
}

// resolveConsumerDependenciesYAML resolves the task component's consumer deps —
// cross-project `org-service` endpoints + bound `external` connection resources
// — into the YAML `dependencies:` block the coding agent should add to its
// `workload.yaml`. Mirrors connections.WireOrgServiceConsumer / WireConsumer
// exactly (same resolution, same env-var/ref derivation) so the comment and the
// post-deploy cascade can't diverge. Returns "" when nothing resolves (no deps,
// or providers not yet published / connections not yet provisioned). orgHandle
// is task.OrgID (the OC namespace, per the connections convention).
func (s *dispatchService) resolveConsumerDependenciesYAML(
	ctx context.Context,
	task *models.ComponentTask,
) (string, error) {
	comp, err := artifacts.ResolveDesignComponent(ctx, s.store, task)
	if err != nil {
		return "", fmt.Errorf("resolve design component: %w", err)
	}

	var deps workloadDeps

	// org-service endpoints (cross-project, visibility namespace). Skip any
	// whose provider hasn't published namespace-visible yet — the cascade
	// re-drives, and the agent can add it later.
	if s.orgServiceResolver != nil {
		for _, name := range comp.OrgServiceDependsOn() {
			target, ok, rerr := s.orgServiceResolver.ResolveNamespaceVisible(ctx, task.OrgID, name)
			if rerr != nil {
				return "", fmt.Errorf("resolve org-service %q: %w", name, rerr)
			}
			if !ok {
				continue
			}
			deps.Endpoints = append(deps.Endpoints, workloadEndpointDepYAML{
				Project:     target.Project,
				Component:   target.Component,
				Name:        target.Name,
				Visibility:  "namespace",
				EnvBindings: map[string]string{"address": connections.OrgServiceURLEnv(name)},
			})
		}
	}

	// external connection resources. Read each connection's development binding
	// for its resolved outputs (== env var names; mirror WireConsumer's
	// output.Name → output.Name mapping). Skip any not provisioned yet.
	if s.connBindingReader != nil {
		for _, conn := range task.DependsOnConnections {
			b, berr := s.connBindingReader(ctx, task.OrgID, connections.ConnectionBindingName(task.ProjectID, conn, "development"))
			if berr != nil || b == nil || b.Status == nil || len(b.Status.Outputs) == 0 {
				// Not provisioned yet / transient — skip; the cascade re-drives.
				continue
			}
			envBindings := make(map[string]string, len(b.Status.Outputs))
			for _, out := range b.Status.Outputs {
				envBindings[out.Name] = out.Name
			}
			deps.Resources = append(deps.Resources, workloadResourceDepYAML{
				Ref:         connections.ConnectionResourceName(task.ProjectID, conn),
				EnvBindings: envBindings,
			})
		}
	}

	if len(deps.Endpoints) == 0 && len(deps.Resources) == 0 {
		return "", nil
	}

	out, err := yaml.Marshal(map[string]workloadDeps{"dependencies": deps})
	if err != nil {
		return "", fmt.Errorf("marshal dependencies yaml: %w", err)
	}
	return string(out), nil
}

// postConsumerDependencyComment renders the resolved consumer-dependency block
// (if any) and posts it to the task's GitHub issue. Best-effort: a resolve or
// post failure is logged and swallowed — it must never fail the dispatch.
func (s *dispatchService) postConsumerDependencyComment(ctx context.Context, task *models.ComponentTask) {
	if s.issueCommenter == nil || task.IssueNumber == 0 {
		return
	}
	block, err := s.resolveConsumerDependenciesYAML(ctx, task)
	if err != nil {
		slog.WarnContext(ctx, "consumer-dependency comment: resolve failed",
			"task", task.ID, "component", task.ComponentName, "error", err)
		return
	}
	if block == "" {
		return
	}
	body := "**Platform-resolved dependencies** — add the following to this component's " +
		"`workload.yaml` (merge into any existing `dependencies:`). The platform has " +
		"already resolved the targets + env-var bindings; copy it verbatim. OpenChoreo " +
		"injects these addresses/outputs into your pod env at runtime:\n\n```yaml\n" + block + "```"
	if err := s.issueCommenter(ctx, task.OrgID, task.ProjectID, task.IssueNumber, body); err != nil {
		slog.WarnContext(ctx, "consumer-dependency comment: post failed",
			"task", task.ID, "component", task.ComponentName, "issue", task.IssueNumber, "error", err)
	}
}

// ocEntrypoint maps a design component type to its OpenChoreo deployment
// entrypoint.
func ocEntrypoint(componentType string) string {
	switch strings.ToLower(componentType) {
	case "web-app":
		return "deployment/web-application"
	default:
		return "deployment/service"
	}
}

// ensureOCComponent creates the OC Component (one per task component) needed
// for the build to fire when the merge push arrives. AutoBuild=false — every
// build is driven by the BFF's push-webhook handler creating a WorkflowRun
// pinned to the merge SHA. AutoDeploy=true — OC's Component controller
// watches the Workload the build's generate-workload-cr step posts and
// creates the ReleaseBinding into the first environment of the project's
// DeploymentPipeline (development) with empty ComponentTypeEnvironmentConfigs;
// schema defaults on the `service` ClusterComponentType supply replicas,
// resources, and imagePullPolicy.
func (s *dispatchService) ensureOCComponent(
	ctx context.Context,
	task *models.ComponentTask,
	repoInfo *models.GitRepository,
) error {
	if s.asServiceIdentity != nil {
		ctx = s.asServiceIdentity(ctx)
	}
	componentName := k8sname.ToK8sName(task.ComponentName)
	slog.InfoContext(ctx, "ensureOCComponent: creating OC component via service identity",
		"task", task.ID, "org", task.OrgID, "project", task.ProjectID, "component", componentName)

	comp, err := artifacts.ResolveDesignComponent(ctx, s.store, task)
	if err != nil {
		return fmt.Errorf("resolve component: %w", err)
	}

	dockerContext := comp.AppPath
	dockerFilePath := "Dockerfile"
	if dockerContext != "" {
		dockerFilePath = dockerContext + "/Dockerfile"
	} else {
		dockerContext = "."
	}

	// Per docs/design/build-credential-injection.md: build credentials are
	// pre-staged per WorkflowRun as a K8s Secret in workflows-<orgID> by
	// git-service. The Component's `repository.secretRef` parameter stays
	// empty so the upstream dockerfile-builder ClusterWorkflow skips its
	// SecretReference / ExternalSecret synth path entirely.
	const secretRefName = ""
	if repoInfo == nil {
		return fmt.Errorf("repo info missing for project=%s", task.ProjectID)
	}

	branch := repoInfo.DefaultBranch
	if branch == "" {
		branch = "main"
	}

	description := task.Title
	if task.Rationale != "" {
		description = task.Title + " — " + task.Rationale
	}

	// Per-component env vars live on the per-environment ReleaseBindings
	// (spec.workloadOverrides.container.env), not on the Component's
	// workflow parameters. configService.UpdateEnvVars writes them out via
	// componentSvc.UpdateWorkflowEnvVars, which patches each ReleaseBinding
	// for this component. On first dispatch the ReleaseBindings don't exist
	// yet — OC creates them after autoDeploy observes the build's Workload —
	// so the next config save (or the caller's post-dispatch reconcile) is
	// what lands env vars into them.
	// Derive the `api-configuration` trait from design.md's optional
	// `exposesAPI.auth` block. nil/none ⇒ no trait, no AP hop;
	// `required` ⇒ trait attached with cors+jwtAuth enabled in every env.
	// See services/trait_sync.go for the canonical emitter.
	apiSecurityEnabled := models.ResolveAPISecurityEnabled(*comp)
	traits, _ := component.DesiredAPIConfigurationTrait(componentName, apiSecurityEnabled)

	_, err = s.componentSvc.CreateComponent(ctx, task.OrgID, task.ProjectID, &models.CreateComponentRequest{
		Name:        componentName,
		DisplayName: task.ComponentName,
		Description: description,
		Type:        ocEntrypoint(comp.ComponentType),
		AutoBuild:   false,
		AutoDeploy:  true,
		Workflow: &models.ComponentWorkflowSpec{
			Kind: "ClusterWorkflow",
			Name: "dockerfile-builder",
			Parameters: &models.ComponentWorkflowParameters{
				Repository: &models.WorkflowRepository{
					URL:       repoInfo.RepoURL,
					SecretRef: secretRefName,
					AppPath:   comp.AppPath,
					Revision:  &models.WorkflowRevision{Branch: branch},
				},
				Docker: &models.DockerParameters{
					Context:  dockerContext,
					FilePath: dockerFilePath,
				},
			},
		},
		Traits: traits,
	})
	if err != nil {
		return fmt.Errorf("create component: %w", err)
	}

	// Best-effort post-create sync. Idempotent — the Component already has
	// traits set via CreateComponent above; this call resolves the
	// per-environment ReleaseBinding configs once OC creates them. When no
	// RBs exist yet the call is a soft no-op (the trait_sync watcher will
	// catch up).
	if apiSecurityEnabled && s.traitSync != nil {
		if syncErr := s.traitSync.SyncComponentTraits(ctx, task.OrgID, task.ProjectID, componentName); syncErr != nil {
			slog.WarnContext(ctx, "ensureOCComponent: trait_sync best-effort failed",
				"orgID", task.OrgID,
				"projectID", task.ProjectID,
				"componentName", componentName,
				"error", syncErr,
			)
		}
	}

	// Web-apps only: emit env-config.js into each ReleaseBinding so the
	// SPA's `window._env_` is populated at request time. Idempotent — the
	// OC client soft no-ops when no RBs exist yet; the cascade re-fires
	// after the first deploy lands a binding.
	if comp.ComponentType == "web-app" && s.runtimeConfig != nil {
		if rcErr := s.runtimeConfig.EmitForComponent(ctx, task.OrgID, task.ProjectID, componentName); rcErr != nil {
			slog.WarnContext(ctx, "ensureOCComponent: runtime_config best-effort failed",
				"orgID", task.OrgID,
				"projectID", task.ProjectID,
				"componentName", componentName,
				"error", rcErr,
			)
		}
	}

	return nil
}
