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
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// CodingExecutor launches the coding agent. Its one dispatch entry point is
// Dispatch (milestone_dispatch.go), the run supervisor's
// delivery.MilestoneDispatcher: one cycle of a milestone run, one agent pod. It
// also owns the build-side retry the exec watcher asks for
// (RetryAuthFailedBuild), which is why it still holds the build-secret stager
// and the executions repository.
//
// One dispatch path: the cycle becomes an ephemeral OpenChoreo Component in the
// milestone's own project, and OpenChoreo renders the batch/v1 Job into that
// project's dataplane namespace. The executor holds no Kubernetes client and
// writes no secret material — the ComponentType's template renders the cycle's
// ExternalSecrets from the org's secret store.
type CodingExecutor struct {
	oc            openchoreo.ComponentClient
	repos         ProjectRepos
	identities    Identities
	execRows      delivery.ExecutionRepository
	gitServiceURL string
	platformURL   string

	// ocJobs is the OpenChoreo Component dispatch path — one Component per run
	// cycle in the milestone's own project.
	ocJobs *OCDispatcher

	// Org-scoped reads, always wired at the composition root: the per-org
	// GitHub SM-API triplet + IDP publisher profile the Workload's secret-env
	// refs are built from, and the org lookup for the data-plane UUID. The
	// Anthropic side goes through a resolver rather than a repository because
	// WHICH of the org's two possible keys a run bills is a domain decision,
	// not a row lookup.
	orgs         organization.OrganizationRepository
	anthropicKey CodingKeyResolver
	githubCreds  organization.OrgCredentialRepository
	idpProfiles  organization.IDPRepository

	// publisher is the Thunder publisher SecretReference resolver. Every
	// dispatch mounts PUBLISHER_* from it (local and cloud). Nil fail-louds.
	publisher         PublisherCredentialResolver
	publisherTokenURL string

	// Build-secret staging (nil → unauthenticated clone, correct for public
	// repos). buildSecrets pre-stages the org's build git credential so a build's
	// checkout-source step can clone a private repo; authRetryBudget bounds the
	// git-clone-auth re-mint retries (§7).
	buildSecrets    BuildSecretStager
	authRetryBudget int

	// runnerSecrets resolves the component's external-resource secret bundles so
	// the cycle's Workload references them (nil → none). Best-effort.
	runnerSecrets RunnerSecretResolver

	// wiring publishes the platform-resolved `endpoints:` block onto the working
	// set at dispatch (nil → skipped). Best-effort; see WiringPublisher.
	wiring      WiringPublisher
	skillMirror SkillMirror
}

// NewCodingExecutor wires the coding executor. Every dispatch goes through the
// OpenChoreo component path; there is no alternative path to enable.
func NewCodingExecutor(
	oc openchoreo.ComponentClient,
	repos ProjectRepos,
	identities Identities,
	execRows delivery.ExecutionRepository,
	gitServiceURL, platformURL string,
	orgs organization.OrganizationRepository,
	anthropicKey CodingKeyResolver,
	githubCreds organization.OrgCredentialRepository,
	idpProfiles organization.IDPRepository,
) *CodingExecutor {
	return &CodingExecutor{
		oc: oc, repos: repos, identities: identities,
		execRows: execRows, gitServiceURL: gitServiceURL, platformURL: platformURL,
		orgs: orgs, anthropicKey: anthropicKey, githubCreds: githubCreds, idpProfiles: idpProfiles,
	}
}

// WithOCDispatch enables the OpenChoreo Component dispatch path (phase 08).
// Returns the receiver for chained construction.
func (e *CodingExecutor) WithOCDispatch(d *OCDispatcher) *CodingExecutor {
	e.ocJobs = d
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

// WithRunnerSecrets enables mounting the component's external-resource secrets
// into the coding runner via per-run ExternalSecrets (nil → none). Returns the
// receiver for chained construction.
func (e *CodingExecutor) WithRunnerSecrets(r RunnerSecretResolver) *CodingExecutor {
	e.runnerSecrets = r
	return e
}

// WithWiringPublisher enables publishing the platform-resolved `endpoints:`
// wiring comment on every cycle dispatch (nil → not published). Returns the
// receiver for chained construction.
func (e *CodingExecutor) WithWiringPublisher(w WiringPublisher) *CodingExecutor {
	e.wiring = w
	return e
}

// WithSkillMirror enables refreshing the project repo's `.claude/skills/`
// copies before each dispatch (nil → not refreshed; the clone keeps whatever
// copies it already has). Returns the receiver for chained construction.
func (e *CodingExecutor) WithSkillMirror(m SkillMirror) *CodingExecutor {
	e.skillMirror = m
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

// agentLaunch is ONE runner-Job launch with the reason for it stripped out: the
// image, namespace, secrets, tokens and dispatch chain are the same whatever
// asked for the launch, and only the correlation id stamped on the pod and the
// prompt shape differ.
//
// It performs NO state write: a cycle dispatch mints no execution row, because
// the cycle record is the run supervisor's own bookkeeping.
type agentLaunch struct {
	orgID     string
	projectID string

	// correlationID is the platform id the pod carries: it is stamped as
	// AEP_TASK_ID, seeds the `ca-…` run name, and is the subject of the runner
	// bearer. It is the dispatching CYCLE's id.
	correlationID string

	// runID is the milestone run the cycle belongs to. Carried for the created
	// Component's description only — nothing resolves state through it.
	runID string

	shape dispatchShape

	// secretComponent, when non-empty, mounts that component's external-resource
	// secrets into the runner. It is always empty today: a cycle spans the whole
	// milestone rather than one component, so there is no single component whose
	// secrets to mount.
	//
	//nolint:unused // a deliberate seam, kept per the repo's retain-with-a-marker
	// rule for unwired infra: it records WHY per-component secret mounting is not
	// wired, which is the question anyone adding it would ask first.
	secretComponent string

	// repo, when non-nil, is a repository row the caller already resolved (the
	// milestone dispatch reads it to anchor a validation prompt at the issue
	// URL), saving a second lookup. Nil means "resolve it here".
	repo *sourcecontrol.GitRepository
}

// launchAgent resolves the run's credentials and launches the runner Job,
// returning the launched run name. It writes no platform state: everything it
// touches is either a read or the cluster.
func (e *CodingExecutor) launchAgent(ctx context.Context, in agentLaunch) (string, error) {
	repo := in.repo
	if repo == nil {
		resolved, err := e.repos.GetRepo(ctx, in.orgID, in.projectID)
		if err != nil || resolved == nil {
			return "", fmt.Errorf("resolve project repo: %w", err)
		}
		repo = resolved
	}
	name, email, login, err := e.identities.IdentityFor(ctx, in.orgID)
	if err != nil {
		return "", fmt.Errorf("resolve git identity: %w", err)
	}
	// OpenChoreo Component dispatch: the only agent path. One Component per run
	// cycle in the milestone's own project.
	if e.ocJobs == nil {
		return "", fmt.Errorf("no coding-agent dispatch path configured: set AGENT_RUNNER_IMAGE")
	}
	return e.dispatchViaOC(ctx, in, repo, name, email, login)
}

// dispatchViaOC launches one cycle through the OpenChoreo Component chain.
//
// The executor's job here is credential and identity resolution — the org's
// refs-only secret triplets and the publisher SecretReference — and the
// dispatcher's job is the OC chain. The run name is derived from the CYCLE id,
// deterministically within a dispatch attempt, so a crashed dispatch resumes
// over the same Component instead of orphaning it.
//
// Credentials reach the pod through the ComponentType's ExternalSecret /
// Workload secretEnv (refs only). Publisher client_credentials are the Job's
// only platform credential (local and cloud).
func (e *CodingExecutor) dispatchViaOC(ctx context.Context, in agentLaunch, repo *sourcecontrol.GitRepository,
	name, email, login string) (string, error) {
	anthropicSR, githubSR, err := e.resolveRunnerSecretRefs(ctx, in.orgID)
	if err != nil {
		return "", err
	}
	disp := in.shape
	platform := strings.TrimRight(e.platformURL, "/")
	env := map[string]string{
		"AEP_TASK_ID":         in.correlationID,
		"AEP_ORG_ID":          in.orgID,
		"AEP_PROJECT_ID":      in.projectID,
		"AEP_COMPONENT_NAME":  disp.componentName,
		"AEP_REPO_URL":        repo.RepoURL,
		"AEP_PROMPT":          disp.prompt,
		"AEP_GIT_SERVICE_URL": e.gitServiceURL,
		"AEP_PLATFORM_URL":    e.platformURL,
		"AEP_MCP_URL":         platform + "/internal/v1/mcp",
		"AEP_IDENTITY_NAME":   name,
		"AEP_IDENTITY_EMAIL":  email,
		"AEP_IDENTITY_LOGIN":  login,
		"AEP_CORRELATION_ID":  in.correlationID,
		"AEP_TASK_KIND":       taskKindOrDefault(disp.taskKind),
		"WORKSPACE_BASE_PATH": codingAgentWorkspacePath,
	}
	secretEnv := []SecretEnvRef{
		{Key: anthropicEnvVarOrDefault(anthropicSR.EnvVar), SecretName: anthropicSR.SecretRefName, SecretKey: anthropicSR.Property},
		{Key: envGitHubToken, SecretName: githubSR.SecretRefName, SecretKey: githubSR.Property},
	}
	pub, tokenURL, err := e.publisherSecretEnv(ctx, in.orgID)
	if err != nil {
		return "", err
	}
	env[envPublisherTokenURL] = tokenURL
	secretEnv = append(secretEnv, pub...)
	return e.ocJobs.Dispatch(ctx, OCDispatchInputs{
		OrgID:                 in.orgID,
		ProjectID:             in.projectID,
		CycleID:               in.correlationID,
		RunID:                 in.runID,
		MilestoneNumber:       disp.milestoneNumber,
		MilestoneTitle:        disp.milestoneTitle,
		Kind:                  disp.taskKind,
		RunName:               codingAgentRunNameFor(in.projectID, in.correlationID),
		ActiveDeadlineSeconds: int(disp.deadline),
		Env:                   env,
		SecretEnv:             secretEnv,
	})
}

// stageBuildSecret pre-stages the org's build git credential and returns the
// secretRef to pass to the build WorkflowRun (its checkout-source step clones
// the project repo). Mirrors feature/component's TriggerBuild staging: no stager
// wired or no repo slug → clone unauthenticated (empty secretRef, correct for
// public repos); an ownership/disconnect/transient staging error blocks the
// retry. The local plane sets GITHUB_REPO_VISIBILITY=private, so this is what
// makes project builds clone at all.
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

// resolveRunnerSecretRefs resolves the two credentials every coding run mounts.
//
// The Anthropic side asks the organization domain WHICH key this org's coding
// runs bill — its coding-agent key when it configured one, its default key
// otherwise — and mounts whatever comes back under the variable name that came
// back WITH it, since a Claude Code OAuth token has to arrive as
// CLAUDE_CODE_OAUTH_TOKEN rather than ANTHROPIC_API_KEY. The runner therefore
// needs no notion of the split at all; it reads whichever of the two is
// present, exactly as Claude Code always has. The resolver fails
// closed on a configured-but-unusable coding key, so a run never silently bills
// the default key an org deliberately scoped away from its coding agent.
func (e *CodingExecutor) resolveRunnerSecretRefs(ctx context.Context, orgID string) (SecretRef, SecretRef, error) {
	triplet, err := e.anthropicKey.ResolveCodingSecretRef(ctx, orgID)
	if err != nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: %w", err)
	}
	anthropicSR := SecretRef{
		SecretRefName: triplet.Name,
		KVPath:        triplet.KVPath,
		Property:      triplet.Property,
		EnvVar:        triplet.EnvVar,
	}

	githubRow, err := e.githubCreds.GetByOrg(ctx, orgID)
	if err != nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: github credentials for org %q: %w", orgID, err)
	}
	if githubRow == nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: github secret reference missing for org %q: org_credentials row not found", orgID)
	}
	githubSR := SecretRef{
		SecretRefName: derefStr(githubRow.SecretRefName),
		KVPath:        derefStr(githubRow.SecretRefKVPath),
		Property:      derefStr(githubRow.SecretRefProperty),
	}
	if err := validateSecretRefTriplet("github", orgID, githubSR); err != nil {
		return SecretRef{}, SecretRef{}, fmt.Errorf("coding dispatch: %w", err)
	}
	return anthropicSR, githubSR, nil
}

// anthropicEnvVarOrDefault names the Job's Anthropic SecretEnv entry from
// organization.SecretRefTriplet.EnvVar (ANTHROPIC_API_KEY or
// CLAUDE_CODE_OAUTH_TOKEN — ADR-0016), falling back to the runner's default
// only if a resolver ever returns the zero value.
func anthropicEnvVarOrDefault(envVar string) string {
	if envVar == "" {
		return envAnthropicAPIKey
	}
	return envVar
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

// codingAgentRunPrefix marks a run name as a coding-agent cycle run (owned by
// the cycle watcher) rather than an OpenChoreo build WorkflowRun. It is the ONE
// discriminator both watchers key on so they never poll each other's runs.
const codingAgentRunPrefix = "ca-"

// isCodingAgentRun reports whether runName is a coding-agent cycle run (vs a
// build WorkflowRun). Shared by the ExecWatcher (skips) and the cycle watcher
// (claims).
func isCodingAgentRun(runName string) bool {
	return strings.HasPrefix(runName, codingAgentRunPrefix)
}

// codingAgentRunNameFor derives a stable `ca-…` Component / JobRef name from
// the project + cycle id. Delegates to openchoreo.NewCodingAgentRunName so the
// SCOPED Component name leaves room for OpenChoreo's Job label decoration
// (see CodingAgentComponentNameBudget). Stability matters: CreateComponent
// treats 409 as success and re-reads, so a Temporal retry after a crash must
// hit the same name — a wall-clock suffix would mint a second billed Component.
func codingAgentRunNameFor(projectID, cycleID string) string {
	return openchoreo.NewCodingAgentRunName(projectID, cycleID)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// buildPrompt is the coding-agent directive (§9): a MILESTONE REFERENCE and
// nothing else. The agent discovers its own working set from the live issues
// API and follows the versioned `aep` skill for ordering, fan-out, branch
// identity, verification and the PR contract — the platform deliberately
// carries no procedure in the prompt, so the workflow versions with the skill
// rather than with the BFF binary.
func buildPrompt(milestoneNumber int, milestoneTitle string) string {
	return fmt.Sprintf("Work the issues for milestone %d (%q). External credentials may not yet be configured; their environment variables may be empty, so live calls may not succeed. Follow the `aep` skill loaded in your session — it defines discovery, ordering, fan-out, branch identity, verification, the PR contract and the deny-list.", milestoneNumber, milestoneTitle)
}

// validationComponentSentinel is the AEP_COMPONENT_NAME a validation Job carries.
// A validation Task is project-scoped (no component); this is a valid k8s label
// value the Job/pod is stamped with.
const validationComponentSentinel = "aep-validation"

// validationTaskKind is the runner's AEP_TASK_KIND for a validation cycle: it
// is what makes the runner preload the `aep-validation` skill instead of `aep`.
const validationTaskKind = "validation"

// validationDeadlineSeconds bounds a validation run (2h): browser boot + live
// exploration + authoring/healing e2e specs is longer than a coding run.
const validationDeadlineSeconds int64 = 7200

// codingDeadlineSeconds bounds an ordinary coding run (3h). A coding cycle no
// longer ends at "the code compiles": on top of working the milestone's whole
// issue set it boots what it built and drives it through a browser
// verification wave, then heals whatever that wave finds. That does not fit in
// an hour, and the old 1h bound did not fail the run honestly — it reaped the
// pod mid-verification, so a cycle that was still making progress died with no
// verdict of its own. 3h is also the ComponentType schema's maximum
// (openchoreo.CodingAgentComponentType): a longer value here would be rejected
// at dispatch, not silently clamped.
const codingDeadlineSeconds int64 = 10800

// taskKindOrDefault normalizes the runner's AEP_TASK_KIND: empty → the coding
// default so existing (implementation) dispatch is unchanged.
func taskKindOrDefault(kind string) string {
	if kind == "" {
		return "implementation"
	}
	return kind
}

// dispatchShape carries the per-class knobs the milestone dispatch hands to the
// OpenChoreo path: coding and validation share the executor, differing only in
// these values.
type dispatchShape struct {
	prompt        string
	componentName string
	taskKind      string // "" (coding) | "validation"
	// deadline is the cycle's activeDeadlineSeconds. Every dispatch names one
	// (0 would fall back to the ComponentType schema's 1h default, which is too
	// short for either kind); it must stay within the schema's maximum.
	deadline int64
	// The milestone the cycle works, carried for the human-facing display name
	// on the created Component. A cycle spans a milestone's whole working set,
	// so the milestone (and the cycle kind) is what names it — not an issue.
	milestoneNumber int
	milestoneTitle  string
}

// buildValidationPrompt is the validation-runner directive: it points at the
// validation issue and defers the workflow to the aep-validation skill (the
// runner preloads it because AEP_TASK_KIND=validation).
//
// It names NO milestone, and that is load-bearing: a validation cycle is
// issue-anchored — one issue, one run — where a coding prompt's milestone
// reference is an instruction to discover a whole working set. The skill still
// needs the milestone for its branch identity (the platform keys a merged pull
// request back to its run by an `aep/m<milestone#>-…` branch); it reads it off
// the issue, which is filed under that milestone at mint time.
//
// The reference is `Validates #N` and NOT a GitHub closing keyword. The platform
// owns the validation task's lifecycle — it reopens the task for the next attempt
// and must close it even on an ending where no pull request merged at all — and a
// closing keyword would put two owners on one issue. The reference still has to
// be there: the auto-merge policy requires a pull request to name an armed issue
// in the milestone, so a body referencing nothing is read as somebody else's work
// and never merges. See eventcore/resolves.go.
func buildValidationPrompt(issueURL string, issueNumber int) string {
	return fmt.Sprintf("This is a validation task. Work on this GitHub validation issue: %s\n\nFollow the `aep-validation` skill's workflow: read the validation context, author and run the e2e tests against the deployed system, commit the tests and report, and open a PR whose body includes `Validates #%d` so the platform links it back. Use `Validates`, never a closing keyword such as `Closes` or `Fixes`: the platform closes this task itself.", issueURL, issueNumber)
}
