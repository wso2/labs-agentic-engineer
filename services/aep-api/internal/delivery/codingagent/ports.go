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

// Package codingagent launches the coding agent and watches what it launched.
//
// It has ONE dispatch entry point — delivery.MilestoneDispatcher: one cycle of
// a milestone run, one agent pod — and writes no platform state on that path,
// because the cycle record is the run supervisor's bookkeeping. What state it
// does write belongs to the two watchers it owns: the cycle watcher (pod-truth
// phase and the captured agent log) and the ExecWatcher (OpenChoreo WorkflowRun
// outcomes, including the git-clone-auth build retry).
//
// One dispatch path: an ephemeral OpenChoreo Component per run cycle, created
// through the OC API in the milestone's own project. OpenChoreo renders the
// batch/v1 Job into that project's dataplane namespace and materialises the
// cycle's secrets from the org's secret store — this package holds no
// Kubernetes client and writes no secret material.
package codingagent

import (
	"context"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Identities resolves the org's git identity (author/committer + login) for a
// coding-agent run. Wired from orgcreds.CredentialService at the composition
// root, so this feature holds no orgcreds import.
type Identities interface {
	IdentityFor(ctx context.Context, ocOrgID string) (name, email, login string, err error)
}

// DeployObserver is notified when a component deploys (a build Execution
// succeeds). The provisioning feature uses it to grant any pending cross-project
// access request targeting the just-deployed provider component (the grant
// cascade). Wired at the composition root; nil → skipped. Primitives-only so this
// feature holds no provisioning import.
type DeployObserver interface {
	OnComponentDeployed(ctx context.Context, orgID, projectID, component string) error
}

// AgentDeathNotifier is told that a dispatched cycle's agent ended without a
// pull request. The run supervisor satisfies it, so the watcher can wake a run
// without holding a workflow engine — the same split, and for the same reason,
// as eventcore.RunSignaler.
//
// It takes primitives rather than the run row because the watcher has a CYCLE in
// hand: resolving (org, run) to the row that carries the milestone number and
// the run's KIND — the workflow id's prefix, and a different vocabulary from a
// cycle's kind — is the composition root's job. Best-effort; nil → the run
// settles on its landing deadline as before.
type AgentDeathNotifier interface {
	AgentDied(ctx context.Context, orgID, runID, reason string) error
}

// RunFailureRecorder writes a run's failure record — the structured "why" the
// console's card reads. The watcher uses it for the one fault it learns first,
// a model provider limit, whose host and reset time only the runner's settle
// carries. The milestone run repository satisfies it; it writes only a run
// that is not yet terminal. Best-effort; nil → no record.
type RunFailureRecorder interface {
	RecordFailure(ctx context.Context, id string, failure delivery.RunFailure) (*delivery.MilestoneRun, error)
}

// jobSuspender suspends a coding cycle's Job binding in one environment, so the
// Job OpenChoreo re-creates after its TTL never runs the runner again.
// Satisfied by openchoreo.ComponentClient: a missing binding is
// openchoreo.ErrNotFound, and a release that predates the suspend schema is
// openchoreo.ErrSuspendUnsupported with nothing written.
type jobSuspender interface {
	SuspendJobBinding(ctx context.Context, org, project, component, environment string) error
}

// SecretRef is one org credential's SecretReference as a Job mounts it: the
// reference's name and the key it reads (C10).
type SecretRef struct {
	SecretRefName string
	Property      string
}

// ExternalResourceSecretInputs is one external resource's per-env secret bundle
// (vault path + secret keys) referenced by the cycle Workload.
type ExternalResourceSecretInputs struct {
	KVPath string
	Keys   []string
}

// RunnerSecretResolver resolves the coding runner's per-run external-resource
// secret bundles for a component (SM-API vault path + secret keys) so the cycle
// Workload can reference them. Wired from the provisioning feature at the
// composition root; nil → the runner gets no external-resource secrets.
// Returns the codingagent input type so this feature holds no
// provisioning/resources import.
type RunnerSecretResolver interface {
	ResolveRunnerSecrets(ctx context.Context, orgID, projectID, component, env string) ([]ExternalResourceSecretInputs, error)
}

// WiringPublisher publishes the platform-resolved `endpoints:` block for the
// project onto the run's working-set issues (ADR-0004). It is called ONCE per
// cycle dispatch, immediately before the Job is launched.
//
// Dispatch is the correct moment because the dispatch predicate already
// guarantees what the comment needs: no gate is open in the milestone (so every
// dependency that can resolve has) and the working set is non-empty (so there is
// somebody to tell). The previous trigger — gate resolution — had neither
// guarantee, and a project whose gates closed before its issues were planned got
// no comment at all and no retry.
//
// Best-effort and non-fatal: a GitHub hiccup must not fail a dispatch, and the
// publisher withholds its idempotency marker on a partial post so the next
// dispatch supersedes it. Wired from the provisioning feature at the composition
// root; nil → skipped.
type WiringPublisher interface {
	PublishResolvedWiring(ctx context.Context, orgID, projectID string)
}

// SkillMirror refreshes the project repo's `.claude/skills/` copies from the
// org library. Called before the Job launches so the clone the agent works in
// carries the guidance its build was designed against. Diff-first: an
// already-current repo costs a read and no commit. Wired from
// spec.SkillService at the composition root; nil → skipped.
type SkillMirror interface {
	SyncProjectSkills(ctx context.Context, orgID, projectID string) error
}

// CodingKeyResolver answers which credential a run on runtime must bill: the
// org's Claude subscription when it has one and the runtime is Claude Code, the
// connection's key otherwise. The choice is the organization domain's to make —
// dispatch only mounts what it is handed — so this port deliberately exposes no
// way to ask "is there a subscription?", which keeps the rule stated in exactly
// one place (ADR-0036). Wired from organization.ModelConnectionService.
//
// KeyRef is a SECOND question, not a way around the first: which key the
// build's agent-evaluation step bills. That step is an API call — it drives the
// generated agent's model and an LLM judge — so it cannot run on a Claude
// subscription token, and the connection's key is always an API key. Asking
// for it says nothing about whether a subscription exists, so the rule above
// stays in its one place. A NotFoundError means the org has connected no key at
// all; see evaluationKeyRef for why that is not a dispatch failure.
type CodingKeyResolver interface {
	organization.CodingCredentialResolver
	KeyRef(ctx context.Context, ocOrgID string) (modelconn.Connection, organization.SecretRefTriplet, error)
}

// CodingAgentSettings answers which runtime this org's next cycle runs on (the
// model is part of the connection, CodingKeyResolver's answer). The
// organization domain owns the choice — including the fact that an org which
// never opened the setting is on the platform's default — so dispatch asks for
// the EFFECTIVE value and never for a row, which is what keeps "nobody chose"
// from being a state dispatch has to know how to interpret.
//
// The value is COPIED onto the run at launch, so a change applies from the
// next cycle. Wired from organization.AgentSettingsService; nil → the platform
// default.
type CodingAgentSettings interface {
	Effective(ctx context.Context, ocOrgID string) (orgconfig.AgentsProjection, error)
}

// ProjectRepos resolves a project's git repo row (RepoURL/RepoSlug). Wired from
// sourcecontrol.RepoService.
type ProjectRepos interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*sourcecontrol.GitRepository, error)
}

// BuildSecretStager returns the secretRef the build WorkflowRun consumes (the
// org's github-pat SecretReference name) so its checkout-source step can clone
// a PRIVATE repo (the local plane sets GITHUB_REPO_VISIBILITY=private, so
// project builds need it). A nil error always carries a reference; a non-nil
// error is an ownership/disconnect refusal or a transient failure that must
// block the build. Consumer-side port:
// the composition root maps the concrete *orgcreds.BuildCredentialsService's
// *StageResult onto the secretRef string (the same adapter feature/component
// uses), so this feature holds no orgcreds import. Optional — nil skips staging.
type BuildSecretStager interface {
	StageBuildSecret(ctx context.Context, ocOrgID, repoSlug, workflowRunName string) (secretRef string, err error)
}

// LiveTail is one read of a cycle pod's log, plus the pod state that read it.
// The pod travels with the text because "no output yet" and "no pod yet" are
// different things to the console, and only the pod says which one happened.
type LiveTail struct {
	Text string
	Pod  openchoreo.RuntimePod
}

// LiveLogSource is the running agent's log, read while its Component still
// exists. Satisfied by *OCLogSource. A wrapped ErrComponentGone means the
// Component has been deleted — the archive's turn, or an unavailable state.
// The environment is the one the cycle's Job was bound into.
type LiveLogSource interface {
	Tail(ctx context.Context, orgName, projectName, componentName, environment string, maxBytes int) (LiveTail, error)
}

// RecordingLogSource is the same pod log read for the RECORDER rather than for
// a viewer, and three differences are the whole point of a separate port.
//
// It reads with a TIME cursor instead of a byte window, and it applies NO byte
// cut. LiveLogSource keeps the newest 64KiB because a viewer wants fresh content
// and re-reads two seconds later; that same cut silently DROPPED a burst larger
// than 64KiB between two polls, which is one of the five losses the recording
// exists to close. A recorder that cut bytes would write the loss into the file,
// where it can never be recovered.
//
// The cursor is an ABSOLUTE INSTANT, not the OpenChoreo API's coarse
// `sinceSeconds`. That is a measured fix, not a tidy-up: the recorder used to
// compute `sinceSeconds` and then spend three sequential OpenChoreo round trips
// getting to the log call, so a slow binding list or resource tree moved the
// window's START past lines nobody had read — a permanent hole, since the cursor
// only ever moves forward. Handing over an instant makes the conversion the
// source's job, done in the breath before the log call, and no latency in front
// of it can eat the window.
//
// The BINDING is resolved separately and by the caller, because it is FIXED for
// the attempt: a session resolves it once and re-resolves only when a read says
// it is gone, which takes a whole round trip out of every poll.
//
// Satisfied by *OCLogSource.
type RecordingLogSource interface {
	// Binding resolves the cycle Component's release binding in the
	// environment its Job was bound into. A wrapped ErrComponentGone means the
	// Component (or its binding) has been deleted, which is a fact about the
	// world.
	Binding(ctx context.Context, orgName, projectName, componentName, environment string) (string, error)

	// ReadSince reads everything the pod logged at or after `since` (the zero
	// time = the whole log the platform still holds), with no byte cut.
	ReadSince(ctx context.Context, orgName, releaseBindingName string, since time.Time) (LiveTail, error)
}

// ArchiveScope names one cycle's archived log: its component, the environment
// its Job was bound into, and the window the cycle ran in. The window matters —
// the observer has no cursor, so the only way to bound a read is to ask for the
// time the work happened.
type ArchiveScope struct {
	OrgName       string
	ProjectName   string
	ComponentName string
	Environment   string
	From          time.Time
	To            time.Time
}

// ArchiveLogSource is a finished cycle's log, read from the observability plane
// while its Component is still retained. Satisfied by *ObserverArchive; nil at
// the composition root when no observer is configured, which the reader
// reports to the console as "unavailable" rather than as an empty log.
type ArchiveLogSource interface {
	CycleArchive(ctx context.Context, scope ArchiveScope) (string, error)
}
