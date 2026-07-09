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

// Package codingagent is the one registered executor of the funnel (§3, §10.1):
// the coding class. It dispatches a coding-agent run for a coding Task and a
// build for its merged PR, writing execution rows through the execution
// repository (one discipline — no raw-gorm state writes) and letting the funnel
// end/spawn the rest. It implements the execution.Executor port; the ops
// executor is a later sibling package + registry entry.
//
// Scope note (docs/design/tasks-github-native.md, cutover): this pass ports the
// legacy ClusterWorkflow dispatch path (ocClient.TriggerCodingAgent /
// TriggerBuildAtCommit). The cluster-gateway-proxy dispatch path (per-run
// ExternalSecrets, publisher client-credentials, the OC Component provisioning
// pre-flight, and build-secret staging for private repos) is a known gap to
// re-attach before proxy/cloud dispatch and private-repo builds work.
package codingagent

import (
	"context"

	"github.com/wso2/aep/aep-api/models"
)

// Identities resolves the org's git identity (author/committer + login) for a
// coding-agent run. Wired from orgcreds.CredentialService at the composition
// root, so this feature holds no orgcreds import.
type Identities interface {
	IdentityFor(ctx context.Context, ocOrgID string) (name, email, login string, err error)
}

// DependencyWiring posts the ADR-0004 "Platform-resolved dependencies" comment on
// a coding Task's issue at dispatch: the platform resolves the component's
// dependency targets (sibling / org-service endpoints, external +
// platform-resource binding outputs) and posts them for the coding agent to copy
// into workload.yaml. The platform NEVER patches the Workload CR. Wired from the
// provisioning feature at the composition root; nil → skipped. Uses primitives so
// this feature holds no provisioning import.
type DependencyWiring interface {
	PostResolvedDeps(ctx context.Context, orgID, projectID string, issueNumber int, component string) error
}

// DeployObserver is notified when a component deploys (a build Execution
// succeeds). The provisioning feature uses it to grant any pending cross-project
// access request targeting the just-deployed provider component (the grant
// cascade). Wired at the composition root; nil → skipped. Primitives-only so this
// feature holds no provisioning import.
type DeployObserver interface {
	OnComponentDeployed(ctx context.Context, orgID, projectID, component string) error
}

// RunnerSecretResolver resolves the coding runner's per-run external-resource
// secret bundles for a component (SM-API vault path + secret keys) — the
// dispatcher materialises each into a per-run ExternalSecret so the agent can
// integration-test against the live external service. Wired from the
// provisioning feature at the composition root; nil → the runner gets no
// external-resource secrets. Returns the codingagent input type so this feature
// holds no provisioning/resources import.
type RunnerSecretResolver interface {
	ResolveRunnerSecrets(ctx context.Context, orgID, projectID, component, env string) ([]ExternalResourceSecretInputs, error)
}

// AnthropicProvisioner materializes the per-org Anthropic key Secret on the
// workflow plane and returns its ref. Best-effort at dispatch. Wired from
// orgcreds.AnthropicCredentialService.
type AnthropicProvisioner interface {
	ApplyWPSecret(ctx context.Context, ocOrgID string) (secretRef string, err error)
}

// TokenIssuer mints the runner's per-Execution bearer (the re-keyed runner
// token, §9.2: the id carried is the execution id). Wired from
// auth.TaskTokenManager (Issue(id, ocOrgID, projectID)).
type TokenIssuer interface {
	Issue(id, ocOrgID, projectID string) (string, error)
}

// ProjectRepos resolves a project's git repo row (RepoURL/RepoSlug). Wired from
// gitrepo.RepoService.
type ProjectRepos interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*models.GitRepository, error)
}

// Reevaluator re-runs the funnel gates — called after a build succeeds so Tasks
// whose dependency just deployed can dispatch (§5). The funnel satisfies it.
type Reevaluator interface {
	Reevaluate(ctx context.Context) error
}

// OrgPublisherProvisioner get-or-creates the org's Thunder publisher OAuth
// client (for the proxy-dispatched runner's cc auth through the cloud gateway).
// Optional — nil / a local http platform URL skips it. Wired from idp.IDPService.
type OrgPublisherProvisioner interface {
	EnsureOrgPublisher(ctx context.Context, orgID, actor string) (clientID, clientSecret string, created bool, err error)
}

// ComponentEnsurer idempotently provisions the OpenChoreo Component CR for a
// Task's component from the design facts. It is the coding-dispatch pre-flight
// (parity with the legacy dispatch service's ensureOCComponent): the Component CR
// must exist by the time the coding run's PR merges and the build spawns, or the
// build fails "Component not found". Wired at the composition root from
// component.ComponentService; design facts are read inside that service through
// its existing artifact-store + repo ports (§10 — codingagent holds no artifacts
// import). Optional — nil skips the pre-flight (public/unit flows).
type ComponentEnsurer interface {
	EnsureComponent(ctx context.Context, orgID, projectID, component string) error
}

// ComponentRuntimeConfigEmitter writes the per-web-app `env-config.js` file onto
// the component's ReleaseBindings so the SPA's `window._env_` is populated at
// request time. Called best-effort at OC-component-ensure time (coding dispatch
// pre-flight), mirroring the legacy dispatch service's ensureOCComponent hook.
// The web-app gate lives inside the emitter (it self-no-ops for non-web-app
// components), so this feature holds no design/artifacts import and passes only
// the component name. Wired at the composition root from
// runtimeconfig.RuntimeConfigService; nil → skipped.
type ComponentRuntimeConfigEmitter interface {
	EmitForComponent(ctx context.Context, orgID, projectID, componentName string) error
}

// BuildSecretStager pre-stages the org's build git credential on the workflow
// plane and returns the secretRef the build WorkflowRun consumes so its
// checkout-source step can clone a PRIVATE repo (the local plane sets
// GITHUB_REPO_VISIBILITY=private, so project builds need it). A nil error with
// an empty secretRef means degrade-to-unauthenticated (correct for the public
// repos aep creates by default); a non-nil error is an ownership/disconnect
// refusal or a transient failure that must block the build. Consumer-side port:
// the composition root maps the concrete *orgcreds.BuildCredentialsService's
// *StageResult onto the secretRef string (the same adapter feature/component
// uses), so this feature holds no orgcreds import. Optional — nil skips staging.
type BuildSecretStager interface {
	StageBuildSecret(ctx context.Context, ocOrgID, repoSlug, workflowRunName string) (secretRef string, err error)
}
