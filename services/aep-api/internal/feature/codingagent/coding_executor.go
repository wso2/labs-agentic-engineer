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
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/execution"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
)

// CodingExecutor is the coding-class executor. Run dispatches the right OC work
// for the Execution's kind (coding → coding-agent pod; build → build at the
// merge SHA), then StartWithRun records the run on the row. The job/exec
// watcher (and, for coding, the PR-opened webhook) Finish the row.
//
// Two coding-dispatch paths, mirroring the legacy dispatch service:
//   - the cluster-gateway-proxy path (per-org NS + per-run ExternalSecrets + a
//     K8s Job), used when the proxy dispatcher + the org's SM-API triplets are
//     configured — this is what the local plane exercises (`ca-…` jobs);
//   - the ClusterWorkflow fallback (ocClient.TriggerCodingAgent) when the proxy
//     path is not fully configured.
type CodingExecutor struct {
	oc            openchoreo.ComponentClient
	repos         ProjectRepos
	identities    Identities
	anthropic     AnthropicProvisioner
	tokens        TokenIssuer
	execRows      repositories.ExecutionRepository
	gitServiceURL string
	platformURL   string

	// Proxy dispatch (nil → ClusterWorkflow only).
	proxy              *Dispatcher
	db                 *gorm.DB
	idp                OrgPublisherProvisioner
	runnerImage        string
	clusterSecretStore string

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
type SkillsRepoResolver func(ctx context.Context, orgID string) (*models.GitRepository, error)

// NewCodingExecutor wires the ClusterWorkflow-path executor. anthropic may be
// nil. Call WithProxy to enable the cluster-gateway-proxy dispatch path.
func NewCodingExecutor(
	oc openchoreo.ComponentClient,
	repos ProjectRepos,
	identities Identities,
	anthropic AnthropicProvisioner,
	tokens TokenIssuer,
	execRows repositories.ExecutionRepository,
	gitServiceURL, platformURL string,
) *CodingExecutor {
	return &CodingExecutor{
		oc: oc, repos: repos, identities: identities, anthropic: anthropic,
		tokens: tokens, execRows: execRows, gitServiceURL: gitServiceURL, platformURL: platformURL,
	}
}

// WithProxy enables the cluster-gateway-proxy coding-agent dispatch path (the
// `ca-…` Job path the local plane uses). idp may be nil (publisher-cc skipped —
// required only on the cloud gateway, i.e. an https platform URL).
func (e *CodingExecutor) WithProxy(proxy *Dispatcher, db *gorm.DB, idp OrgPublisherProvisioner, runnerImage, clusterSecretStore string) *CodingExecutor {
	e.proxy = proxy
	e.db = db
	e.idp = idp
	e.runnerImage = runnerImage
	e.clusterSecretStore = clusterSecretStore
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
var _ execution.Executor = (*CodingExecutor)(nil)

// Run dispatches one Execution attempt. On a launch failure it returns the error
// (the funnel Finishes the row failed + flags attention).
func (e *CodingExecutor) Run(ctx context.Context, req execution.DispatchRequest) error {
	switch req.Execution.Kind {
	case string(taskmeta.KindCoding):
		return e.runCoding(ctx, req)
	case string(taskmeta.KindBuild):
		return e.runBuild(ctx, req)
	default:
		return fmt.Errorf("coding executor: unsupported kind %q", req.Execution.Kind)
	}
}

func (e *CodingExecutor) runCoding(ctx context.Context, req execution.DispatchRequest) error {
	t := req.Task
	// Pre-flight (parity with the legacy dispatch service's ensureOCComponent):
	// provision the OpenChoreo Component CR from the design facts BEFORE the coding
	// run, so the PR it opens has a Component to build when it merges (otherwise
	// the spawned build fails "Component not found"). A provisioning failure blocks
	// dispatch — the funnel Finishes the row failed + flags attention.
	if e.components != nil {
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
	// Resolve the org's skills repo URL (provisioning on first touch) so the
	// runner can clone it and resolve applied skills locally. Best-effort — ""
	// on any failure, the runner then degrades to the base plugin.
	skillsRepoURL := e.resolveSkillsRepoURL(ctx, t.OrgID)
	prompt := buildPrompt(t.IssueURL, t.IssueNumber)

	// ADR-0004 declarative wiring: post the "Platform-resolved dependencies"
	// comment on the coding issue so the agent copies it into workload.yaml. The
	// gate held this consumer until its deps deployed, so their targets/outputs
	// resolve now. Best-effort — the platform never patches the CR, and a wiring
	// failure must not fail the dispatch.
	if e.wiring != nil {
		if werr := e.wiring.PostResolvedDeps(ctx, t.OrgID, t.ProjectID, t.IssueNumber, t.Component); werr != nil {
			slog.WarnContext(ctx, "coding executor: post resolved-deps comment failed", "issue", t.IssueNumber, "error", werr)
		}
	}

	// Proxy path first (what local uses). Falls back to ClusterWorkflow when the
	// proxy dispatcher / SM-API triplets are not fully configured.
	used, runName, perr := e.dispatchViaProxy(ctx, req, repo, prompt, name, email, login, bearer, skillsRepoURL)
	if perr != nil {
		return perr
	}
	if used {
		e.startRun(ctx, req.Execution.ID, runName)
		return nil
	}

	// ClusterWorkflow fallback.
	var anthropicRef string
	if e.anthropic != nil {
		if ref, aerr := e.anthropic.ApplyWPSecret(ctx, t.OrgID); aerr != nil {
			slog.WarnContext(ctx, "coding executor: apply anthropic secret failed — dispatching without", "org", t.OrgID, "error", aerr)
		} else {
			anthropicRef = ref
		}
	}
	run, err := e.oc.TriggerCodingAgent(ctx, openchoreo.CodingAgentParams{
		OrgName:       t.OrgID,
		ProjectName:   t.ProjectID,
		ComponentName: t.Component,
		// NAMING DEBT (residual): the OC param `TaskID` (and the runner env
		// AEP_TASK_ID it maps to) CARRIES the EXECUTION id, not a task id. Left
		// un-renamed — the ClusterWorkflow definition on the cluster and the
		// runner both bind these names; rename with the cluster workflow +
		// runner in a coordinated pass. The S2S routes the runner calls with
		// this id (skills + credentials refresh) are already execution-keyed.
		TaskID:             req.Execution.ID,
		Prompt:             prompt,
		RepoURL:            repo.RepoURL,
		IdentityName:       name,
		IdentityEmail:      email,
		IdentityLogin:      login,
		Bearer:             bearer,
		SkillsRepoURL:      skillsRepoURL,
		GitServiceURL:      e.gitServiceURL,
		PlatformURL:        e.platformURL,
		AnthropicSecretRef: anthropicRef,
	})
	if err != nil {
		return fmt.Errorf("trigger coding-agent: %w", err)
	}
	e.startRun(ctx, req.Execution.ID, run.Name)
	return nil
}

// dispatchViaProxy runs the cluster-gateway-proxy apply-chain for one coding
// Execution — the same recipe as the legacy dispatch service's
// tryDispatchViaProxy, re-keyed off the execution + Task facts. used=false ⇒ not
// configured for the proxy path (fall back). The runner env AEP_TASK_ID carries
// the EXECUTION id (JobInputs.TaskID) and the bearer's task claim is the
// execution id — the re-keyed runner contract (§9.2).
func (e *CodingExecutor) dispatchViaProxy(ctx context.Context, req execution.DispatchRequest, repo *models.GitRepository, prompt, name, email, login, bearer, skillsRepoURL string) (bool, string, error) {
	t := req.Task
	if e.proxy == nil || e.db == nil || e.runnerImage == "" || e.clusterSecretStore == "" {
		return false, "", nil
	}
	var (
		anthropicRow models.OrgAnthropicCredential
		githubRow    models.OrgCredential
	)
	if err := e.db.WithContext(ctx).Where("oc_org_id = ?", t.OrgID).First(&anthropicRow).Error; err != nil {
		slog.InfoContext(ctx, "proxy dispatch: anthropic row missing; falling back", "org", t.OrgID, "error", err)
		return false, "", nil
	}
	if err := e.db.WithContext(ctx).Where("oc_org_id = ?", t.OrgID).First(&githubRow).Error; err != nil {
		slog.InfoContext(ctx, "proxy dispatch: github row missing; falling back", "org", t.OrgID, "error", err)
		return false, "", nil
	}
	if anthropicRow.SMAPIKVPath == nil || githubRow.SMAPIKVPath == nil {
		slog.InfoContext(ctx, "proxy dispatch: SM-API triplet missing; falling back", "org", t.OrgID)
		return false, "", nil
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
	var idpRow models.OrganizationIDPProfile
	if err := e.db.WithContext(ctx).Where("org_id = ?", t.OrgID).First(&idpRow).Error; err == nil {
		if idpRow.SMAPIKVPath != nil && idpRow.SMAPISecretRefName != nil {
			publisherSR = &SecretRef{
				SecretRefName: derefStr(idpRow.SMAPISecretRefName),
				KVPath:        derefStr(idpRow.SMAPIKVPath),
				Property:      derefStr(idpRow.SMAPIProperty),
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
		TaskID:            req.Execution.ID,
		OrgID:             t.OrgID,
		ProjectID:         t.ProjectID,
		ComponentName:     t.Component,
		RunnerImage:       e.runnerImage,
		RepoURL:           repo.RepoURL,
		Prompt:            prompt,
		IdentityName:      name,
		IdentityEmail:     email,
		IdentityLogin:     login,
		GitServiceURL:     e.gitServiceURL,
		CallbackURL:       e.platformURL,
		Bearer:            bearer,
		SkillsRepoURL:     skillsRepoURL,
		PublisherTokenURL: publisherTokenURL,
	}
	// Resolve the component's external-resource secret bundles so the dispatcher
	// materialises each into a per-run ExternalSecret the runner mounts (the agent
	// integration-tests against the live service). Best-effort: on failure the run
	// dispatches without them (identical to no secret-bearing external deps).
	var extResSRs []ExternalResourceSecretInputs
	if e.runnerSecrets != nil {
		if srs, rerr := e.runnerSecrets.ResolveRunnerSecrets(ctx, t.OrgID, t.ProjectID, t.Component, "development"); rerr != nil {
			slog.WarnContext(ctx, "coding executor: resolve external-resource runner secrets failed — dispatching without", "component", t.Component, "error", rerr)
		} else {
			extResSRs = srs
		}
	}
	rn, err := e.proxy.Dispatch(ctx, Inputs{
		OrgUUID:                orgUUID,
		Job:                    job,
		AnthropicSR:            SecretRef{SecretRefName: derefStr(anthropicRow.SMAPISecretRefName), KVPath: derefStr(anthropicRow.SMAPIKVPath), Property: derefStr(anthropicRow.SMAPIProperty)},
		GitHubSR:               SecretRef{SecretRefName: derefStr(githubRow.SMAPISecretRefName), KVPath: derefStr(githubRow.SMAPIKVPath), Property: derefStr(githubRow.SMAPIProperty)},
		PublisherSR:            publisherSR,
		ExternalResourceSRs:    extResSRs,
		ClusterSecretStoreName: e.clusterSecretStore,
	})
	if err != nil {
		return false, "", err
	}
	return true, rn, nil
}

func (e *CodingExecutor) runBuild(ctx context.Context, req execution.DispatchRequest) error {
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
			// actionable error so a human re-runs the coding execution.
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
func (e *CodingExecutor) RetryAuthFailedBuild(ctx context.Context, row *models.Execution) (string, error) {
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

func (e *CodingExecutor) lookupOrgUUID(ctx context.Context, ocOrgID string) (string, error) {
	var org models.Organization
	if err := e.db.WithContext(ctx).Where("name = ?", ocOrgID).First(&org).Error; err != nil {
		return "", err
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
