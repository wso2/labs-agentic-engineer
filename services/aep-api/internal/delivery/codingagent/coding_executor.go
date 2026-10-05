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
	"strconv"
	"strings"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
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
	oc          openchoreo.ComponentClient
	repos       ProjectRepos
	identities  Identities
	execRows    delivery.ExecutionRepository
	platformURL string

	// ocJobs is the OpenChoreo Component dispatch path — one Component per run
	// cycle in the milestone's own project.
	ocJobs *OCDispatcher

	// Org-scoped reads, always wired at the composition root: the org lookup
	// for the data-plane UUID and the org secret rows the Workload's
	// secret-env refs are built from. The model key goes through a resolver
	// rather than a repository because WHICH of the org's two possible keys a
	// run bills is a domain decision, not a row lookup.
	orgs         organization.OrganizationRepository
	anthropicKey CodingKeyResolver

	// orgSecrets reads the github-pat row, the reference every run mounts
	// (R7). Required: the PAT lives only in vault, and the row is the only
	// record of its reference.
	orgSecrets organization.OrgSecretRefReader

	// githubOwners answers the GitHub account the org's repositories live
	// under: the reference the runner's in-process remote-git tools hold every
	// requested owner against (AEP_GITHUB_OWNER). Nil, or an org it cannot
	// answer for, fails the dispatch: a runner without it cannot guard them.
	githubOwners sourcecontrol.OwnerLookup

	// codingAgent answers which runtime and model this org's runs use. Nil is
	// the platform defaults, which is exactly what every dispatch carried before
	// the setting existed — so an unwired resolver changes nothing rather than
	// failing a build.
	codingAgent CodingAgentSettings

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
// orgSecrets must be non-nil: there is no reading the GitHub PAT's reference
// without its row.
func NewCodingExecutor(
	oc openchoreo.ComponentClient,
	repos ProjectRepos,
	identities Identities,
	execRows delivery.ExecutionRepository,
	platformURL string,
	orgs organization.OrganizationRepository,
	anthropicKey CodingKeyResolver,
	orgSecrets organization.OrgSecretRefReader,
) *CodingExecutor {
	if orgSecrets == nil {
		panic("codingagent: NewCodingExecutor needs the org secret rows")
	}
	return &CodingExecutor{
		oc: oc, repos: repos, identities: identities,
		execRows: execRows, platformURL: platformURL,
		orgs: orgs, anthropicKey: anthropicKey, orgSecrets: orgSecrets,
	}
}

// WithOCDispatch enables the OpenChoreo Component dispatch path (phase 08).
// Returns the receiver for chained construction.
func (e *CodingExecutor) WithOCDispatch(d *OCDispatcher) *CodingExecutor {
	e.ocJobs = d
	return e
}

// WithGitHubOwners attaches the org GitHub-owner lookup every dispatch stamps
// as AEP_GITHUB_OWNER. Returns the receiver for chained construction.
func (e *CodingExecutor) WithGitHubOwners(o sourcecontrol.OwnerLookup) *CodingExecutor {
	e.githubOwners = o
	return e
}

// WithCodingAgentSettings enables the org's runtime/model setting. Nil (or not
// calling this) leaves every run on the platform defaults. Returns the receiver
// for chained construction.
func (e *CodingExecutor) WithCodingAgentSettings(s CodingAgentSettings) *CodingExecutor {
	e.codingAgent = s
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
// returning the launched run name and the model host it runs on. It writes no
// platform state: everything it touches is either a read or the cluster.
func (e *CodingExecutor) launchAgent(ctx context.Context, in agentLaunch) (delivery.AgentLaunch, error) {
	repo := in.repo
	if repo == nil {
		resolved, err := e.repos.GetRepo(ctx, in.orgID, in.projectID)
		if err != nil || resolved == nil {
			return delivery.AgentLaunch{}, fmt.Errorf("resolve project repo: %w", err)
		}
		repo = resolved
	}
	name, email, login, err := e.identities.IdentityFor(ctx, in.orgID)
	if err != nil {
		return delivery.AgentLaunch{}, fmt.Errorf("resolve git identity: %w", err)
	}
	// OpenChoreo Component dispatch: the only agent path. One Component per run
	// cycle in the milestone's own project.
	if e.ocJobs == nil {
		return delivery.AgentLaunch{}, fmt.Errorf("no coding-agent dispatch path configured: set AGENT_RUNNER_IMAGE")
	}
	return e.dispatchViaOC(ctx, in, repo, name, email, login)
}

// dispatchViaOC launches one cycle through the OpenChoreo Component chain.
//
// The executor's job here is credential and identity resolution — the org's
// secret references (names the org_secrets rows record) — and the
// dispatcher's job is the OC chain. The run name is derived from the CYCLE id,
// deterministically within a dispatch attempt, so a crashed dispatch resumes
// over the same Component instead of orphaning it.
//
// Credentials reach the pod through the ComponentType's ExternalSecret /
// Workload secretEnv (refs only). Publisher client_credentials are the Job's
// only platform credential (local and cloud).
//
// The launch reports the host of the connection whose credential it mounted and
// the environment its Job was bound into: the supervisor copies both onto the
// cycle, so the host a cycle's usage is priced on is the one this Job was
// launched against, and its readers look for the Job where it was bound.
func (e *CodingExecutor) dispatchViaOC(ctx context.Context, in agentLaunch, repo *sourcecontrol.GitRepository,
	name, email, login string) (delivery.AgentLaunch, error) {
	// The organization's agent setting, copied onto THIS run. Copied, not
	// referenced: a change applies from the next cycle, and a run that re-read
	// the setting halfway through would produce a feed whose model names
	// disagree with the tokens they were billed for. Read first because the
	// runtime decides which credential the run may mount.
	agent, err := e.codingAgentEnv(ctx, in.orgID)
	if err != nil {
		return delivery.AgentLaunch{}, err
	}
	creds, err := e.resolveRunnerSecretRefs(ctx, in.orgID, agent.Runtime)
	if err != nil {
		return delivery.AgentLaunch{}, err
	}
	githubSR := creds.github
	disp := in.shape
	platform := strings.TrimRight(e.platformURL, "/")
	env := map[string]string{
		"AEP_TASK_ID":        in.correlationID,
		"AEP_ORG_ID":         in.orgID,
		"AEP_PROJECT_ID":     in.projectID,
		"AEP_COMPONENT_NAME": disp.componentName,
		"AEP_REPO_URL":       repo.RepoURL,
		"AEP_PROMPT":         disp.prompt,
		"AEP_PLATFORM_URL":   e.platformURL,
		"AEP_MCP_URL":        platform + "/internal/v1/mcp",
		"AEP_IDENTITY_NAME":  name,
		"AEP_IDENTITY_EMAIL": email,
		"AEP_IDENTITY_LOGIN": login,
		// The owner-must-match-org guard's reference for the runner's
		// in-process remote-git tools — the org's GitHub account, the same
		// value its AE Studio pod is given as AE_GITHUB_OWNER.
		"AEP_GITHUB_OWNER":    creds.githubOwner,
		"AEP_CORRELATION_ID":  in.correlationID,
		"AEP_TASK_KIND":       taskKindOrDefault(disp.taskKind),
		"WORKSPACE_BASE_PATH": codingAgentWorkspacePath,
		// Unconditional, and deliberately not tied to whether a key was resolved
		// below — see envEvalKeyManaged.
		envEvalKeyManaged: "1",
		// The run's OWN deadline, so it can end itself rather than be ended.
		// The same number this dispatch puts on the Job's activeDeadlineSeconds
		// below: when that one passes, the pod is killed mid-sentence and
		// explains nothing — no result line, no watchdog snapshot, and for a run
		// with background subagents no way to tell "still working" from
		// "wedged". Handing it to the runner is what buys the moment it needs to
		// stop its tasks and say so on the feed. The runner subtracts its own
		// margin (`runDeadlineFromEnv`); the margin is not this layer's to know,
		// and duplicating it here would let the two drift apart silently.
		"AEP_RUN_DEADLINE_SECONDS": strconv.FormatInt(disp.deadline, 10),
	}
	env[envAgentRuntime] = string(agent.Runtime)
	// The model connection the credential is for, and its model, copied onto
	// the run for the same reason: a run in flight keeps the endpoint and the
	// model its usage is billed against.
	env[envAgentModel] = creds.model.Conn.Model
	connEnv, modelKeyVar := modelEnv(creds.model)
	for k, v := range connEnv {
		env[k] = v
	}
	// Only a validation cycle is issue-anchored, so only it can name an issue.
	// Absent rather than "0" for every other kind: the runner reads presence, and
	// a stamped zero would be a number it has to know is not one.
	if disp.validationIssue > 0 {
		env[envValidationIssue] = strconv.Itoa(disp.validationIssue)
	}
	modelRef := creds.model.Ref
	secretEnv := []SecretEnvRef{
		{Key: modelKeyVar, SecretName: modelRef.Name, SecretKey: modelRef.Property},
		{Key: envGitHubToken, SecretName: githubSR.SecretRefName, SecretKey: githubSR.Property},
	}
	// The evaluation key and the connection it is for travel together or not
	// at all — see envEvalModelFormat.
	if evalConn, evalSR, ok := e.evaluationKeyRef(ctx, in.orgID); ok {
		secretEnv = append(secretEnv, evalSR)
		for k, v := range evalModelEnv(evalConn) {
			env[k] = v
		}
	}
	pub, tokenURL, err := e.publisherSecretEnv(ctx, in.orgID)
	if err != nil {
		return delivery.AgentLaunch{}, err
	}
	env[envPublisherTokenURL] = tokenURL
	secretEnv = append(secretEnv, pub...)
	res, err := e.ocJobs.Dispatch(ctx, OCDispatchInputs{
		OrgID:                 in.orgID,
		ProjectID:             in.projectID,
		CycleID:               in.correlationID,
		RunID:                 in.runID,
		MilestoneNumber:       disp.milestoneNumber,
		MilestoneTitle:        disp.milestoneTitle,
		Kind:                  disp.taskKind,
		RunName:               codingAgentRunNameFor(in.projectID, in.correlationID),
		Runtime:               agent.Runtime,
		ActiveDeadlineSeconds: int(disp.deadline),
		Env:                   env,
		SecretEnv:             secretEnv,
	})
	if err != nil {
		return delivery.AgentLaunch{}, err
	}
	return delivery.AgentLaunch{JobRef: res.RunName, ModelHost: creds.model.Conn.Host, Environment: res.Environment, ComponentUID: res.ComponentUID}, nil
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

// runnerCredentials is what every coding run mounts: the model credential,
// with the connection it is for and its kind, and the org's GitHub credential
// with the account it is for.
type runnerCredentials struct {
	model       organization.CodingCredential
	github      SecretRef
	githubOwner string
}

// resolveRunnerSecretRefs resolves the two credentials every coding run mounts.
//
// The model side asks the organization domain WHICH credential a run on
// runtime bills — its Claude subscription when it has one and the runtime is
// Claude Code, the connection's key otherwise — and which connection it is
// for. The domain answers in its own terms (a kind, never a variable name);
// modelEnv maps that to the runner's env contract. The resolver fails closed
// on a configured-but-unusable subscription, so a run never silently bills API
// credits an org chose to replace with its plan.
//
// The GitHub side is the github-pat reference (githubSecretRef) and the org's
// GitHub owner (githubOwner).
func (e *CodingExecutor) resolveRunnerSecretRefs(ctx context.Context, orgID string, runtime orgconfig.AgentRuntime) (runnerCredentials, error) {
	cred, err := e.anthropicKey.ResolveCodingCredential(ctx, orgID, runtime)
	if err != nil {
		return runnerCredentials{}, fmt.Errorf("coding dispatch: %w", err)
	}
	githubSR, err := e.githubSecretRef(ctx, orgID)
	if err != nil {
		return runnerCredentials{}, fmt.Errorf("coding dispatch: %w", err)
	}
	owner, err := e.githubOwner(ctx, orgID)
	if err != nil {
		return runnerCredentials{}, fmt.Errorf("coding dispatch: %w", err)
	}
	return runnerCredentials{model: cred, github: githubSR, githubOwner: owner}, nil
}

// githubOwner is the GitHub account the org's repositories live under, the
// one the runner's remote-git tools may read. No answer is no dispatch.
func (e *CodingExecutor) githubOwner(ctx context.Context, orgID string) (string, error) {
	if e.githubOwners == nil {
		return "", fmt.Errorf("github owner for org %q: no owner lookup configured", orgID)
	}
	owner, err := e.githubOwners.GitHubOwner(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("github owner for org %q: %w", orgID, err)
	}
	if owner == "" {
		return "", fmt.Errorf("github owner for org %q: empty", orgID)
	}
	return owner, nil
}

// githubSecretRef is the org's GitHub PAT reference: the name its github-pat
// row records, with the token key (R7), so a rotation never leaves a Job
// mounting the reference the write already deleted. The Job carries only
// SecretKeyRef{Name, Key}; OpenChoreo resolves the reference itself (C10).
// No row: the org has no PAT reference (the PAT lives only in vault), and the
// dispatch fails.
func (e *CodingExecutor) githubSecretRef(ctx context.Context, orgID string) (SecretRef, error) {
	name, ok, err := organization.RecordedOrgSecretRef(ctx, e.orgSecrets, orgID, organization.OrgSecretGitHubPAT)
	if err != nil {
		return SecretRef{}, fmt.Errorf("github secret reference for org %q: %w", orgID, err)
	}
	if !ok {
		return SecretRef{}, fmt.Errorf("github secret reference missing for org %q: no %s row (reconnect GitHub)", orgID, organization.OrgSecretGitHubPAT)
	}
	return SecretRef{SecretRefName: name, Property: organization.OrgSecretGitHubPAT.ValueKey()}, nil
}

// evaluationKeyRef resolves the org's connection key as the build's
// agent-evaluation credential, with the connection it is for, reporting whether
// there is one to mount.
//
// A build that generates an ai-agent evaluates it before opening its PR, and
// that step needs a model twice over — once for the agent it boots, once for the
// judge that grades it. Both are API calls, so the credential has to be an API
// key; the connection's key always is (ADR-0036), while the coding credential
// may be a Claude subscription token that authenticates neither. Any format
// will do: the harness reaches the connection the way the deployed agent will.
//
// An unresolvable key is NOT a dispatch failure, which is the one thing that
// makes this different from every other credential here. Evaluation reports; it
// never fails a build. An org with no usable key still gets its work done and
// its PR opened — the evaluation step simply reports that it could not run —
// so a missing key must not cost the org a delivery. It is logged rather than
// swallowed silently, because "the harness never became ready" is otherwise a
// puzzling thing to read in a build report.
func (e *CodingExecutor) evaluationKeyRef(ctx context.Context, orgID string) (modelconn.Connection, SecretEnvRef, bool) {
	conn, triplet, err := e.anthropicKey.KeyRef(ctx, orgID)
	if err != nil {
		slog.InfoContext(ctx, "coding dispatch: no model connection key — the build will run without agent evaluation",
			"org", orgID, "error", err)
		return modelconn.Connection{}, SecretEnvRef{}, false
	}
	// A half-mirrored row resolves to a triplet ESO cannot follow. Mounting it
	// would put the variable on the pod pointing at nothing, and the harness
	// would report the agent as misbehaving rather than as unconfigured.
	if triplet.Name == "" || triplet.Property == "" {
		slog.WarnContext(ctx, "coding dispatch: the connection key's secret reference is incomplete — the build will run without agent evaluation",
			"org", orgID)
		return modelconn.Connection{}, SecretEnvRef{}, false
	}
	return conn, SecretEnvRef{Key: envEvalModelAPIKey, SecretName: triplet.Name, SecretKey: triplet.Property}, true
}

// codingAgentEnv resolves the runtime this run is launched with (the model
// comes with the connection, from resolveRunnerSecretRefs).
//
// A missing resolver, or an org that never chose, both mean the platform
// defaults. A resolver that ERRORS
// is different and fails the dispatch: the org did choose something, we cannot
// read what, and launching on the defaults would bill it for a model it moved
// off without ever saying so.
func (e *CodingExecutor) codingAgentEnv(ctx context.Context, orgID string) (orgconfig.AgentsProjection, error) {
	if e.codingAgent == nil {
		return orgconfig.DefaultAgents(), nil
	}
	proj, err := e.codingAgent.Effective(ctx, orgID)
	if err != nil {
		return orgconfig.AgentsProjection{}, fmt.Errorf("coding dispatch: coding-agent setting for org %q: %w", orgID, err)
	}
	return proj, nil
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
// is what makes the runner preload the `validation-task` skill instead of `aep`.
const validationTaskKind = "validation"

// envValidationIssue names the validation issue to the pod. A validation run
// posts that issue's status line as it works — see the runner's
// validation_status_line.ts — and nothing else in the pod's environment answers
// "which issue": AEP_TASK_ID is the cycle's uuid, and the number reaches the
// agent only as prose inside AEP_PROMPT.
const envValidationIssue = "AEP_VALIDATION_ISSUE"

// validationDeadlineSeconds bounds a validation run (2h): a browser boots once
// and every scenario in specs/validation/acceptance/ is then driven through it
// in sequence, which is longer than a coding run.
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
	// validationIssue is the issue a VALIDATION cycle is anchored to, and zero
	// for every other kind. It reaches the pod as AEP_VALIDATION_ISSUE because
	// the runner posts that issue's status line itself, and it cannot name an
	// issue it was only told about in prose: AEP_TASK_ID is the cycle's uuid,
	// and the number is otherwise buried in the free text of AEP_PROMPT.
	validationIssue int
}

// buildValidationPrompt is the validation-runner directive, mirroring
// buildPrompt: it names the issue, and the preloaded `validation-task` skill is
// the procedure.
//
// It names NO milestone, and that is load-bearing: a validation cycle is
// issue-anchored — one issue, one run — where a coding prompt's milestone
// reference is an instruction to discover a whole working set. The skill reads
// the milestone off the issue for its branch identity.
func buildValidationPrompt(issueURL string) string {
	return fmt.Sprintf("This is a validation task. Work on this GitHub validation issue: %s\n\nFollow the `validation-task` skill loaded in your session — it defines the run, the report and the PR contract.", issueURL)
}
