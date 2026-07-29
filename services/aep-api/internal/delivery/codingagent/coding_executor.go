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

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// CodingExecutor is the coding-class executor. Run dispatches the right OC work
// for the Execution's kind (coding → coding-agent pod; build → build at the
// merge SHA), then StartWithRun records the run on the row. The job/exec
// watcher (and, for coding, the PR-opened webhook) Finish the row.
//
// Two coding-dispatch paths:
//   - the cluster-gateway-proxy path (per-org NS + per-run ExternalSecrets + a
//     K8s Job), used when the proxy dispatcher + the org's SM-API triplets are
//     configured — this is what the cloud / local-proxy plane exercises (`ca-…` jobs);
//   - the direct K8s Job fallback (K8sJobDispatcher) when the proxy path is not
//     configured — constructible, but Dispatch fails closed (direct k8s-job
//     secret delivery is disabled; configure cluster-gateway-proxy + secret refs).
type CodingExecutor struct {
	oc            openchoreo.ComponentClient
	repos         ProjectRepos
	identities    Identities
	anthropic     AnthropicProvisioner
	tokens        TokenIssuer
	execRows      delivery.ExecutionRepository
	gitServiceURL string
	platformURL   string

	// Proxy dispatch (nil → fall through to the direct k8sJob path).
	proxy *Dispatcher

	// Org-scoped repository reads, always wired at the composition root: the
	// per-org Anthropic/GitHub SM-API triplets + IDP publisher profile for the
	// proxy dispatch, and the org lookup for the data-plane UUID.
	orgs           organization.OrganizationRepository
	anthropicCreds organization.OrgAnthropicRepository
	githubCreds    organization.OrgCredentialRepository
	idpProfiles    organization.IDPRepository

	// k8sJob is the direct K8s Job dispatch path — the sole fallback when the
	// proxy path is not configured (nil → no fallback; dispatch errors out).
	// Requires the org repository to be set (for org UUID lookup).
	k8sJob             *K8sJobDispatcher
	idp                OrgPublisherProvisioner
	runnerImage        string
	clusterSecretStore string

	// validationImage is the Playwright-capable runner image for ClassValidation
	// tasks (Dockerfile.validation). Empty → validation dispatch fails loudly
	// (the alpine coding image cannot run chromium). Set via WithValidationImage.
	validationImage string

	// Build-secret staging (nil → unauthenticated clone, correct for public
	// repos). buildSecrets pre-stages the org's build git credential so a build's
	// checkout-source step can clone a private repo; authRetryBudget bounds the
	// git-clone-auth re-mint retries (§7).
	buildSecrets    BuildSecretStager
	authRetryBudget int

	// components ensures the OpenChoreo Component CR exists as a coding-dispatch
	// pre-flight, so the merged-PR build has a Component to build (nil → skipped).
	components ComponentEnsurer

	// wiring posts the ADR-0004 "Platform-resolved dependencies" comment on the
	// coding issue at dispatch (nil → skipped). Best-effort — it never fails the run.
	wiring DependencyWiring

	// runnerSecrets resolves the component's external-resource secret bundles so
	// the proxy dispatch mounts them into the runner (nil → none). Best-effort.
	runnerSecrets RunnerSecretResolver

	// skillsRepo resolves (provisioning if needed) the org's `org-skills` repo
	// row so its clone URL can be stamped as AEP_SKILLS_REPO_URL — the runner
	// clones it to resolve the design's applied skills locally (nil → the URL
	// is not stamped and the runner degrades to the base plugin). Best-effort.
	skillsRepo SkillsRepoResolver

	// runtimeConfig emits env-config.js onto a web-app's ReleaseBindings at the
	// component-ensure pre-flight (nil → skipped). Best-effort — a failure warns
	// but never fails the dispatch. The web-app gate lives in the emitter, so a
	// call for a non-web-app component is a self-no-op.
	runtimeConfig ComponentRuntimeConfigEmitter
}

// SkillsRepoResolver ensures the org's skills repo exists and returns its row.
// Satisfied at the composition root by the same EnsureProvisioned+GetRepo
// closure the genai + task-plan turns use, so this feature grows no skills edge.
type SkillsRepoResolver func(ctx context.Context, orgID string) (*sourcecontrol.GitRepository, error)

// NewCodingExecutor wires the base coding executor. anthropic may be nil. Call
// WithProxy and/or WithK8sJobDispatch to enable a dispatch path.
func NewCodingExecutor(
	oc openchoreo.ComponentClient,
	repos ProjectRepos,
	identities Identities,
	anthropic AnthropicProvisioner,
	tokens TokenIssuer,
	execRows delivery.ExecutionRepository,
	gitServiceURL, platformURL string,
	orgs organization.OrganizationRepository,
	anthropicCreds organization.OrgAnthropicRepository,
	githubCreds organization.OrgCredentialRepository,
	idpProfiles organization.IDPRepository,
) *CodingExecutor {
	return &CodingExecutor{
		oc: oc, repos: repos, identities: identities, anthropic: anthropic,
		tokens: tokens, execRows: execRows, gitServiceURL: gitServiceURL, platformURL: platformURL,
		orgs: orgs, anthropicCreds: anthropicCreds, githubCreds: githubCreds, idpProfiles: idpProfiles,
	}
}

// WithProxy enables the cluster-gateway-proxy coding-agent dispatch path (the
// `ca-…` Job path the local plane uses). idp may be nil (publisher-cc skipped —
// required only on the cloud gateway, i.e. an https platform URL).
func (e *CodingExecutor) WithProxy(proxy *Dispatcher, idp OrgPublisherProvisioner, runnerImage, clusterSecretStore string) *CodingExecutor {
	e.proxy = proxy
	e.idp = idp
	e.runnerImage = runnerImage
	e.clusterSecretStore = clusterSecretStore
	return e
}

// WithValidationImage sets the Playwright-capable runner image used for
// ClassValidation tasks (the same executor serves both classes; validation
// swaps the image, sets AEP_TASK_KIND=validation, and skips the coding-only
// component-ensure/wiring pre-flight). Returns the receiver for chaining.
func (e *CodingExecutor) WithValidationImage(image string) *CodingExecutor {
	e.validationImage = image
	return e
}

// WithK8sJobDispatch wires the direct K8s Job dispatch path. The org UUID
// lookup (needed to derive the data-plane namespace) reads through the org
// repository wired at construction. Direct k8s-job secret delivery is disabled
// — Dispatch fails closed; use WithProxy for refs-only ExternalSecrets.
func (e *CodingExecutor) WithK8sJobDispatch(d *K8sJobDispatcher) *CodingExecutor {
	e.k8sJob = d
	return e
}

// WithBuildSecrets enables build-secret staging for the post-merge build path
// (private-repo clones) and sets the git-clone-auth retry budget (≤0 → the
// default). stager may be nil (builds clone unauthenticated — correct for public
// repos). Returns the receiver for chained construction.
func (e *CodingExecutor) WithBuildSecrets(stager BuildSecretStager, authRetryBudget int) *CodingExecutor {
	e.buildSecrets = stager
	if authRetryBudget <= 0 {
		authRetryBudget = defaultBuildAuthRetryBudget
	}
	e.authRetryBudget = authRetryBudget
	return e
}

// WithComponentEnsurer enables the coding-dispatch pre-flight that provisions the
// OpenChoreo Component CR before the coding run, so the merged-PR build finds it.
// Returns the receiver for chained construction.
func (e *CodingExecutor) WithComponentEnsurer(c ComponentEnsurer) *CodingExecutor {
	e.components = c
	return e
}

// WithDependencyWiring enables the ADR-0004 declarative-wiring comment at coding
// dispatch (nil → skipped). Returns the receiver for chained construction.
func (e *CodingExecutor) WithDependencyWiring(w DependencyWiring) *CodingExecutor {
	e.wiring = w
	return e
}

// WithRunnerSecrets enables mounting the component's external-resource secrets
// into the coding runner via per-run ExternalSecrets (nil → none). Returns the
// receiver for chained construction.
func (e *CodingExecutor) WithRunnerSecrets(r RunnerSecretResolver) *CodingExecutor {
	e.runnerSecrets = r
	return e
}

// WithSkillsRepo enables stamping AEP_SKILLS_REPO_URL onto the coding dispatch
// so the runner clones the org's `org-skills` repo to resolve applied skills
// locally (nil → not stamped; the runner degrades to the base plugin). Returns
// the receiver for chained construction.
func (e *CodingExecutor) WithSkillsRepo(r SkillsRepoResolver) *CodingExecutor {
	e.skillsRepo = r
	return e
}

// resolveSkillsRepoURL returns the org's `org-skills` clone URL, provisioning
// the repo on first touch. Best-effort: any failure (no resolver wired,
// provisioning error, missing row) yields "" — the dispatch proceeds without
// the skills URL and the runner degrades to the base plugin rather than failing
// the coding run over a skills-guidance gap.
func (e *CodingExecutor) resolveSkillsRepoURL(ctx context.Context, orgID string) string {
	if e.skillsRepo == nil {
		return ""
	}
	repo, err := e.skillsRepo(ctx, orgID)
	if err != nil || repo == nil {
		slog.WarnContext(ctx, "coding executor: resolve skills repo failed — dispatching without AEP_SKILLS_REPO_URL", "org", orgID, "error", err)
		return ""
	}
	return repo.RepoURL
}

// WithComponentRuntimeConfig enables best-effort env-config.js emission at the
// component-ensure pre-flight (nil → skipped). Returns the receiver for chained
// construction.
func (e *CodingExecutor) WithComponentRuntimeConfig(r ComponentRuntimeConfigEmitter) *CodingExecutor {
	e.runtimeConfig = r
	return e
}

// AuthRetryBudget reports the configured git-clone-auth build retry budget
// (default when unset). The ExecWatcher reads it to bound its retry loop.
func (e *CodingExecutor) AuthRetryBudget() int {
	if e.authRetryBudget <= 0 {
		return defaultBuildAuthRetryBudget
	}
	return e.authRetryBudget
}

// Compile-time proof the executor satisfies the funnel's port.
var _ delivery.Executor = (*CodingExecutor)(nil)

// Run dispatches one Execution attempt. On a launch failure it returns the error
// (the funnel Finishes the row failed + flags attention).
func (e *CodingExecutor) Run(ctx context.Context, req delivery.DispatchRequest) error {
	switch req.Execution.Kind {
	case string(taskmeta.KindCoding):
		return e.runCoding(ctx, req)
	case string(taskmeta.KindBuild):
		return e.runBuild(ctx, req)
	default:
		return fmt.Errorf("coding executor: unsupported kind %q", req.Execution.Kind)
	}
}

func (e *CodingExecutor) runCoding(ctx context.Context, req delivery.DispatchRequest) error {
	t := req.Task
	isValidation := t.Class == taskmeta.ClassValidation

	// Coding pre-flight (skipped for validation, which has no component to build):
	// provision the OpenChoreo Component CR from the design facts BEFORE the coding
	// run, so the PR it opens has a Component to build when it merges (otherwise
	// the spawned build fails "Component not found"). A provisioning failure blocks
	// dispatch — the funnel Finishes the row failed + flags attention.
	if !isValidation && e.components != nil {
		if err := e.components.EnsureComponent(ctx, t.OrgID, t.ProjectID, t.Component); err != nil {
			return fmt.Errorf("ensure component pre-flight: %w", err)
		}
	}
	// Web-apps only: emit env-config.js so the SPA's `window._env_` is populated
	// at request time (parity with the legacy dispatch service's ensureOCComponent
	// hook). The emitter self-no-ops for non-web-app components, so this is safe to
	// call unconditionally — the design/type read lives inside the emitter, keeping
	// this feature free of an artifacts import. Best-effort: an emit failure warns
	// but must never fail the coding dispatch (the deploy cascade re-fires it).
	if e.runtimeConfig != nil {
		if rcErr := e.runtimeConfig.EmitForComponent(ctx, t.OrgID, t.ProjectID, t.Component); rcErr != nil {
			slog.WarnContext(ctx, "coding executor: env-config.js emit failed (best-effort)", "component", t.Component, "error", rcErr)
		}
	}
	repo, err := e.repos.GetRepo(ctx, t.OrgID, t.ProjectID)
	if err != nil || repo == nil {
		return fmt.Errorf("resolve project repo: %w", err)
	}
	name, email, login, err := e.identities.IdentityFor(ctx, t.OrgID)
	if err != nil {
		return fmt.Errorf("resolve git identity: %w", err)
	}
	bearer, err := e.tokens.Issue(req.Execution.ID, t.OrgID, t.ProjectID)
	if err != nil {
		return fmt.Errorf("mint runner bearer: %w", err)
	}
	// Dedicated MCP identity token (aud aep-api-mcp): the runner bearer above
	// (aud git-service) is pinned-rejected by the MCP verifier, so the pod needs
	// a separate token to call the BFF's internal MCP surface (list endpoints /
	// read remote file / search remote code). One token stamped at dispatch,
	// TTL matching the runner bearer's 24h Job lifetime — no refresh route.
	mcpToken, err := e.tokens.IssueServiceToken(auth.AudienceMCP, t.OrgID, 24*time.Hour)
	if err != nil {
		return fmt.Errorf("mint MCP token: %w", err)
	}
	// Resolve the org's skills repo URL (provisioning on first touch) so the
	// runner can clone it and resolve applied skills locally. Best-effort — ""
	// on any failure, the runner then degrades to the base plugin.
	skillsRepoURL := e.resolveSkillsRepoURL(ctx, t.OrgID)

	// Per-class dispatch shape: validation swaps the Playwright image, the
	// project-scoped component sentinel, AEP_TASK_KIND=validation, a longer
	// deadline (browser boot + e2e authoring), and skips the coding-only wiring.
	disp := dispatchShape{
		prompt:        buildPrompt(t.IssueURL, t.IssueNumber),
		componentName: t.Component,
		image:         e.runnerImage,
		taskKind:      "",
		deadline:      0,
	}
	if isValidation {
		disp = dispatchShape{
			prompt:        buildValidationPrompt(t.IssueURL, t.IssueNumber),
			componentName: validationComponentSentinel,
			image:         e.validationImage,
			taskKind:      string(taskmeta.ClassValidation),
			deadline:      validationDeadlineSeconds,
		}
	} else if e.wiring != nil {
		// ADR-0004 declarative wiring: post the "Platform-resolved dependencies"
		// comment on the coding issue so the agent copies it into workload.yaml. The
		// gate held this consumer until its deps deployed, so their targets/outputs
		// resolve now. Best-effort — the platform never patches the CR, and a wiring
		// failure must not fail the dispatch.
		if werr := e.wiring.PostResolvedDeps(ctx, t.OrgID, t.ProjectID, t.IssueNumber, t.Component); werr != nil {
			slog.WarnContext(ctx, "coding executor: post resolved-deps comment failed", "issue", t.IssueNumber, "error", werr)
		}
	}

	// Proxy path (cloud / local-proxy plane): per-org NS + per-run ExternalSecrets
	// + K8s Job via the cluster-gateway-proxy. Falls back to the direct K8s Job
	// path below when the proxy / SM-API is not configured.
	used, runName, perr := e.dispatchViaProxy(ctx, req, repo, name, email, login, bearer, disp, mcpToken, skillsRepoURL)
	if perr != nil {
		return perr
	}
	if used {
		e.startRun(ctx, req.Execution.ID, runName)
		return nil
	}

	// Validation has no non-proxy fallback — it requires the proxy path
	// (Playwright image + AEP_TASK_KIND). Fail loudly rather than launch a
	// browserless coding agent via the K8s Job path.
	if isValidation {
		return fmt.Errorf("validation dispatch requires the cluster-gateway-proxy path and a VALIDATION_RUNNER_IMAGE; the direct K8s Job fallback does not support validation")
	}

	// Direct K8s Job path: Dispatch fails closed — secret delivery requires
	// cluster-gateway-proxy + secret refs (no Secret/ExternalSecret writes
	// here). Missing refs on the proxy path already errored above; this branch
	// only runs when the proxy path was not configured.
	if e.k8sJob != nil {
		orgUUID, uuidErr := e.lookupOrgUUID(ctx, t.OrgID)
		if uuidErr != nil {
			return fmt.Errorf("k8s-job dispatch: lookup org UUID for %q: %w", t.OrgID, uuidErr)
		}
		k8sRunName := codingAgentRunNameFor(req.Execution.ID)
		rn, k8serr := e.k8sJob.Dispatch(ctx, K8sJobInput{
			RunName:       k8sRunName,
			OrgID:         t.OrgID,
			OrgUUID:       orgUUID,
			ProjectID:     t.ProjectID,
			Component:     t.Component,
			ExecutionID:   req.Execution.ID,
			RepoURL:       repo.RepoURL,
			Prompt:        disp.prompt,
			IdentityName:  name,
			IdentityEmail: email,
			IdentityLogin: login,
			Bearer:        bearer,
			SkillsRepoURL: skillsRepoURL,
		})
		if k8serr != nil {
			return k8serr
		}
		e.startRun(ctx, req.Execution.ID, rn)
		return nil
	}

	return fmt.Errorf("no coding-agent dispatch path configured: set CLUSTER_GATEWAY_PROXY_URL or ensure in-cluster client + AGENT_RUNNER_IMAGE + AGENT_PLATFORM_URL are set")
}

// dispatchViaProxy runs the cluster-gateway-proxy apply-chain for one coding
// Execution — the same recipe as the legacy dispatch service's
// tryDispatchViaProxy, re-keyed off the execution + Task facts. used=false ⇒ not
// configured for the proxy path (fall back). The runner env AEP_TASK_ID carries
// the EXECUTION id (JobInputs.TaskID) and the bearer's task claim is the
// execution id — the re-keyed runner contract (§9.2).
func (e *CodingExecutor) dispatchViaProxy(ctx context.Context, req delivery.DispatchRequest, repo *sourcecontrol.GitRepository, name, email, login, bearer string, disp dispatchShape, mcpToken, skillsRepoURL string) (bool, string, error) {
	t := req.Task
	if e.proxy == nil || disp.image == "" || e.clusterSecretStore == "" {
		return false, "", nil
	}
	anthropicSR, githubSR, err := e.resolveRunnerSecretRefs(ctx, t.OrgID)
	if err != nil {
		return false, "", err
	}

	// Publisher cc (cloud gateway only). Best-effort provision + SM-API triplet.
	var (
		publisherSR       *SecretRef
		publisherTokenURL string
	)
	if e.idp != nil {
		if _, _, _, perr := e.idp.EnsureOrgPublisher(ctx, t.OrgID, "dispatch"); perr != nil {
			slog.ErrorContext(ctx, "proxy dispatch: EnsureOrgPublisher failed — runner cc may be invalid", "org", t.OrgID, "error", perr)
		}
	}
	if idpRow, err := e.idpProfiles.GetProfileByOrgID(ctx, t.OrgID); err == nil && idpRow != nil {
		if idpRow.ResolvedSecretRefKVPath() != nil && idpRow.ResolvedSecretRefName() != nil {
			publisherSR = &SecretRef{
				SecretRefName: derefStr(idpRow.ResolvedSecretRefName()),
				KVPath:        derefStr(idpRow.ResolvedSecretRefKVPath()),
				Property:      derefStr(idpRow.ResolvedSecretRefProperty()),
			}
			publisherTokenURL = deriveTokenURLFromJWKS(idpRow.JWKSURL)
			if publisherTokenURL == "" {
				publisherSR = nil
			}
		}
	}
	// On the cloud gateway (https) a per-task JWT is rejected — publisher cc is
	// mandatory. Local k3d (http) keeps the bearer fallback.
	if publisherSR == nil && isGatewayPlatformURL(e.platformURL) {
		return false, "", fmt.Errorf("publisher cc not provisioned for org %q: the coding-agent runner cannot authenticate through the gateway", t.OrgID)
	}

	orgUUID, err := e.lookupOrgUUID(ctx, t.OrgID)
	if err != nil {
		slog.InfoContext(ctx, "proxy dispatch: org UUID not found; falling back", "org", t.OrgID, "error", err)
		return false, "", nil
	}
	runName := codingAgentRunNameFor(req.Execution.ID)
	job := JobInputs{
		RunName: runName,
		// NAMING DEBT: JobInputs.TaskID → the Job's AEP_TASK_ID env carries the
		// EXECUTION id (see the CodingAgentParams note in runCoding). Un-renamed
		// this pass to avoid a cluster-workflow + runner rename before Phase 4.
		TaskID:                req.Execution.ID,
		OrgID:                 t.OrgID,
		ProjectID:             t.ProjectID,
		ComponentName:         disp.componentName,
		RunnerImage:           disp.image,
		TaskKind:              disp.taskKind,
		ActiveDeadlineSeconds: disp.deadline,
		RepoURL:               repo.RepoURL,
		Prompt:                disp.prompt,
		IdentityName:          name,
		IdentityEmail:         email,
		IdentityLogin:         login,
		GitServiceURL:         e.gitServiceURL,
		CallbackURL:           e.platformURL,
		Bearer:                bearer,
		MCPToken:              mcpToken,
		SkillsRepoURL:         skillsRepoURL,
		PublisherTokenURL:     publisherTokenURL,
	}
	// Resolve the component's external-resource secret bundles so the dispatcher
	// materialises each into a per-run ExternalSecret the runner mounts (the agent
	// integration-tests against the live service). Best-effort: on failure the run
	// dispatches without them (identical to no secret-bearing external deps).
	// Component tasks only — a validation task is project-scoped (no component,
	// no component-bound external resources).
	var extResSRs []ExternalResourceSecretInputs
	if e.runnerSecrets != nil && t.Component != "" {
		if srs, rerr := e.runnerSecrets.ResolveRunnerSecrets(ctx, t.OrgID, t.ProjectID, t.Component, openchoreo.DevEnvironmentName); rerr != nil {
			slog.WarnContext(ctx, "coding executor: resolve external-resource runner secrets failed — dispatching without", "component", t.Component, "error", rerr)
		} else {
			extResSRs = srs
		}
	}
	rn, err := e.proxy.Dispatch(ctx, Inputs{
		OrgUUID:                orgUUID,
		Job:                    job,
		AnthropicSR:            anthropicSR,
		GitHubSR:               githubSR,
		PublisherSR:            publisherSR,
		ExternalResourceSRs:    extResSRs,
		ClusterSecretStoreName: e.clusterSecretStore,
	})
	if err != nil {
		return false, "", err
	}
	return true, rn, nil
}

func (e *CodingExecutor) runBuild(ctx context.Context, req delivery.DispatchRequest) error {
	t := req.Task
	if req.MergeSHA == "" {
		return fmt.Errorf("build execution has no merge SHA")
	}
	runName := openchoreo.NewBuildRunName(t.ProjectID, t.Component)
	secretRef, err := e.stageBuildSecret(ctx, t.OrgID, t.ProjectID, runName)
	if err != nil {
		return err
	}
	run, err := e.oc.TriggerBuildAtCommit(ctx, t.OrgID, t.ProjectID, t.Component, req.MergeSHA, secretRef, runName)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			// The Component CR is missing — the coding-dispatch pre-flight
			// (ensureComponent) never provisioned it. The build path does NOT
			// upsert (the legacy build path didn't either); surface a clear,
			// actionable error so a human re-runs the coding delivery.
			return fmt.Errorf("trigger build: OpenChoreo Component for %q (%s/%s) not found — its coding execution must run first to provision the Component (re-execute the Task); %w",
				t.Component, t.OrgID, t.ProjectID, err)
		}
		return fmt.Errorf("trigger build: %w", err)
	}
	e.startRun(ctx, req.Execution.ID, run.Name)
	return nil
}

// stageBuildSecret pre-stages the org's build git credential and returns the
// secretRef to pass to the build WorkflowRun (its checkout-source step clones
// the project repo). Mirrors feature/component's TriggerBuild staging: no stager
// wired or no repo slug → clone unauthenticated (empty secretRef, correct for
// public repos); an ownership/disconnect/transient staging error blocks the
// build (returned to the funnel, which Finishes the row failed + flags
// attention). The local plane sets GITHUB_REPO_VISIBILITY=private, so this is
// what makes project builds clone at all.
func (e *CodingExecutor) stageBuildSecret(ctx context.Context, orgID, projectID, runName string) (string, error) {
	if e.buildSecrets == nil {
		return "", nil
	}
	repo, err := e.repos.GetRepo(ctx, orgID, projectID)
	if err != nil {
		slog.WarnContext(ctx, "build: resolve repo for secret staging failed — cloning unauthenticated", "org", orgID, "project", projectID, "error", err)
		return "", nil
	}
	if repo == nil || repo.RepoSlug == "" {
		slog.WarnContext(ctx, "build: no repo slug — cloning unauthenticated", "org", orgID, "project", projectID)
		return "", nil
	}
	secretRef, err := e.buildSecrets.StageBuildSecret(ctx, orgID, repo.RepoSlug, runName)
	if err != nil {
		return "", fmt.Errorf("stage build secret: %w", err)
	}
	return secretRef, nil
}

// RetryAuthFailedBuild re-mints the build clone credential and re-triggers the
// build at the row's pinned CommitSHA under a fresh run name, returning that
// name for the caller (ExecWatcher) to thread onto the row. It is the
// git-clone-auth retry the legacy build watcher's RetryAuthFailedBuild provided,
// re-keyed to the execution row. A staging refusal (org disconnected / repo not
// in org) aborts the retry — the watcher exhausts the budget instead.
func (e *CodingExecutor) RetryAuthFailedBuild(ctx context.Context, row *delivery.Execution) (string, error) {
	if row == nil {
		return "", fmt.Errorf("retry-auth-failed: nil execution")
	}
	if row.CommitSHA == "" {
		return "", fmt.Errorf("retry-auth-failed: execution %s has no commit SHA", row.ID)
	}
	if row.Component == "" {
		return "", fmt.Errorf("retry-auth-failed: execution %s has no component", row.ID)
	}
	runName := openchoreo.NewBuildRunName(row.ProjectID, row.Component)
	secretRef, err := e.stageBuildSecret(ctx, row.OrgID, row.ProjectID, runName)
	if err != nil {
		return "", err
	}
	run, err := e.oc.TriggerBuildAtCommit(ctx, row.OrgID, row.ProjectID, row.Component, row.CommitSHA, secretRef, runName)
	if err != nil {
		return "", fmt.Errorf("retry-auth-failed: trigger build: %w", err)
	}
	return run.Name, nil
}

func (e *CodingExecutor) startRun(ctx context.Context, id, runName string) {
	if _, err := e.execRows.StartWithRun(ctx, id, runName); err != nil {
		slog.WarnContext(ctx, "coding executor: StartWithRun failed", "execution", id, "run", runName, "error", err)
	}
}

func (e *CodingExecutor) resolveRunnerSecretRefs(ctx context.Context, orgID string) (SecretRef, SecretRef, error) {
	anthropicRow, err := e.anthropicCreds.GetByOrg(ctx, orgID)
	if err != nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: anthropic credentials for org %q: %w", orgID, err)
	}
	if anthropicRow == nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: anthropic secret reference missing for org %q: org_anthropic_credentials row not found", orgID)
	}
	anthropicSR := SecretRef{
		SecretRefName: derefStr(anthropicRow.ResolvedSecretRefName()),
		KVPath:        derefStr(anthropicRow.ResolvedSecretRefKVPath()),
		Property:      derefStr(anthropicRow.ResolvedSecretRefProperty()),
	}
	if err := validateSecretRefTriplet("anthropic", orgID, anthropicSR); err != nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: %w", err)
	}

	githubRow, err := e.githubCreds.GetByOrg(ctx, orgID)
	if err != nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: github credentials for org %q: %w", orgID, err)
	}
	if githubRow == nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: github secret reference missing for org %q: org_credentials row not found", orgID)
	}
	githubSR := SecretRef{
		SecretRefName: derefStr(githubRow.ResolvedSecretRefName()),
		KVPath:        derefStr(githubRow.ResolvedSecretRefKVPath()),
		Property:      derefStr(githubRow.ResolvedSecretRefProperty()),
	}
	if err := validateSecretRefTriplet("github", orgID, githubSR); err != nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: %w", err)
	}
	return anthropicSR, githubSR, nil
}

func validateSecretRefTriplet(credential, orgID string, ref SecretRef) error {
	if ref.SecretRefName == "" {
		return fmt.Errorf("%s secret reference missing for org %q: secret_ref_name not populated", credential, orgID)
	}
	if ref.KVPath == "" {
		return fmt.Errorf("%s secret reference missing for org %q: secret_ref_kv_path not populated", credential, orgID)
	}
	if ref.Property == "" {
		return fmt.Errorf("%s secret reference missing for org %q: secret_ref_property not populated", credential, orgID)
	}
	return nil
}

func (e *CodingExecutor) lookupOrgUUID(ctx context.Context, ocOrgID string) (string, error) {
	org, err := e.orgs.GetByName(ctx, ocOrgID)
	if err != nil {
		return "", err
	}
	if org == nil {
		return "", fmt.Errorf("organization %s not found", ocOrgID)
	}
	if org.ThunderOrgUUID != nil && *org.ThunderOrgUUID != uuid.Nil {
		return org.ThunderOrgUUID.String(), nil
	}
	if org.UUID == uuid.Nil {
		return "", fmt.Errorf("organization %s has no UUID", ocOrgID)
	}
	return org.UUID.String(), nil
}

// proxyJobRunPrefix marks a run name as a cluster-gateway-proxy coding-agent Job
// (a K8s Job owned by the JobWatcher) rather than an OpenChoreo WorkflowRun. It
// is the ONE discriminator both watchers key on so they never poll each other's
// runs: the JobWatcher processes ONLY these, the ExecWatcher skips them.
const proxyJobRunPrefix = "ca-"

// isProxyJobRun reports whether runName is a proxy-dispatched coding-agent Job
// (vs an OpenChoreo WorkflowRun). Shared by the ExecWatcher (skips) and the
// JobWatcher (claims).
func isProxyJobRun(runName string) bool {
	return strings.HasPrefix(runName, proxyJobRunPrefix)
}

// codingAgentRunNameFor derives a deterministic `ca-…` run name from an id + a
// UTC minute bucket (same shape as the legacy codingAgentRunName; a re-dispatch
// within the minute reuses the name and the immutable Job is DELETE+POST'd).
func codingAgentRunNameFor(id string) string {
	minute := time.Now().UTC().Format("0601021504")
	shortID := id
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	name := fmt.Sprintf("%s%s-%s", proxyJobRunPrefix, shortID, minute)
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
// IdP-authed cloud gateway (https) rather than an internal http URL (local k3d).
func isGatewayPlatformURL(u string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(u)), "https://")
}

// deriveTokenURLFromJWKS swaps a trailing `/oauth2/jwks` for `/oauth2/token`
// (same Thunder host). Returns "" on unrecognized shapes.
func deriveTokenURLFromJWKS(jwksURL string) string {
	const suffix = "/oauth2/jwks"
	j := strings.TrimSpace(jwksURL)
	if !strings.HasSuffix(j, suffix) {
		return ""
	}
	return strings.TrimSuffix(j, suffix) + "/oauth2/token"
}

// buildPrompt is the coding-agent directive: work the issue and open a PR that
// closes it (so the pull_request webhook links the PR back to the Task).
func buildPrompt(issueURL string, issueNumber int) string {
	return fmt.Sprintf("Work on this GitHub issue: %s\n\nWhen you open the pull request, include `Closes #%d` in its body so the platform links the PR back to this task. The full workflow, constraints, and deny-list are in the `aep` skill loaded in your session.", issueURL, issueNumber)
}

// validationComponentSentinel is the AEP_COMPONENT_NAME a validation Job carries.
// A validation Task is project-scoped (no component); this is a valid k8s label
// value the Job/pod is stamped with.
const validationComponentSentinel = "aep-validation"

// validationDeadlineSeconds bounds a validation run (2h): browser boot + live
// exploration + authoring/healing e2e specs is longer than a coding run.
const validationDeadlineSeconds int64 = 7200

// dispatchShape carries the per-class knobs runCoding hands to the proxy
// dispatch: the coding class and the validation class share the executor and
// the proxy plumbing, differing only in these values.
type dispatchShape struct {
	prompt        string
	componentName string
	image         string
	taskKind      string // "" (coding) | "validation"
	deadline      int64  // 0 → job_template's 1h default
}

// buildValidationPrompt is the validation-runner directive: it points at the
// validation issue and defers the workflow to the aep-validation skill (the
// runner preloads it because AEP_TASK_KIND=validation).
func buildValidationPrompt(issueURL string, issueNumber int) string {
	return fmt.Sprintf("This is a validation task. Work on this GitHub validation issue: %s\n\nFollow the `aep-validation` skill's workflow: fetch the validation context, author and run the e2e tests against the deployed system, commit the tests and report, and open a PR whose body includes `Closes #%d` so the platform links it back.", issueURL, issueNumber)
}
