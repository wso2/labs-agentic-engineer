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

package provisioning

import (
	"context"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The consumer ports the provisioning services + watcher drive. Each is the
// narrow slice of a larger collaborator; concrete providers are wired at the
// composition root. This package's feature-edge allowlist is
// {dependencies/resources, gitrepo} — everything else is a local port.

// IssueClient is the GitHub issue surface: list Task issues (to find/dedup
// `provision` gate issues and to reach the run's working set), create a gate
// issue, close it with a reference, comment (a failure, or the ADR-0004
// resolved-wiring block), and stamp the aep:wired/<slug> marker that keeps that
// comment idempotent. sourcecontrol.IssueService satisfies it.
type IssueClient interface {
	ListIssues(ctx context.Context, orgID, projectID string, labels []string) ([]sourcecontrol.IssueInfo, error)
	CreateIssue(ctx context.Context, orgID, projectID string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error)
	CloseIssue(ctx context.Context, orgID, projectID string, number int, comment string) error
	CommentIssue(ctx context.Context, orgID, projectID string, number int, body string) error
	AddLabels(ctx context.Context, orgID, projectID string, number int, labels []string) error
}

// ExecutionStore is the executions-rows repository slice the provision lifecycle
// drives: admit the provision row (the (repo, issue, kind) mutex), start it
// (queued → running, stamping the binding run name), finish it (→ deployed /
// failed), and list active rows (the readiness watcher's sweep).
// delivery.ExecutionRepository satisfies it.
type ExecutionStore interface {
	TryAdmit(ctx context.Context, e *delivery.Execution) (admitted bool, row *delivery.Execution, err error)
	StartWithRun(ctx context.Context, id, runName string) (*delivery.Execution, error)
	Finish(ctx context.Context, id, status, reason string) (*delivery.Execution, error)
	ListActive(ctx context.Context) ([]delivery.Execution, error)
}

// DesignReader reads a project's authored design components at HEAD. It returns
// ONLY models-typed data so this package needs no artifacts feature edge — the
// composition root adapts artifacts.ArtifactStore. (Minting runs right after
// approval, so HEAD == the just-tagged content; the gate issue still records its
// DesignTag for lineage.)
type DesignReader interface {
	ReadDesignComponents(ctx context.Context, orgID, projectID string) ([]spec.DesignComponent, error)
}

// RepoLocator resolves an org+project to its GitHub repo full name ("owner/name").
// The provision Execution row's Repo MUST match the gate issue's repo
// full name, or the funnel gate's LatestPerKind(repo, issue) cannot resolve the
// run and the consumer never releases. Wired from repositories.
type RepoLocator interface {
	RepoFullName(ctx context.Context, orgID, projectID string) (string, error)
	// ByFullName resolves a `<owner>/<repo>` full name back to its (orgID,
	// projectID) — the reverse direction, used by the issues/closed webhook to
	// locate the provider project of a declined org-publish gate issue.
	ByFullName(ctx context.Context, fullName string) (orgID, projectID string, err error)
}

// ExternalRTCatalog is the org-namespaced OpenChoreo ResourceType-backed
// external-resource registry the org-settings list+delete+register+update
// surface (ListExternalResources / DeleteExternalResource /
// RegisterExternalResource / UpdateExternalResource) reads and writes: List
// reconstructs each provisioned external's definition off its authored RT
// (deduped to the newest schema-version RT per name); Ensure get-or-creates
// the RT (no project Resource instance); Update PUTs an existing RT in place
// so catalog-field edits persist when the hashed name is unchanged; Delete
// removes every RT registered under a logical name (more than one
// schema-version RT can carry the same name — see openchoreo.ExternalResourceRTName).
// *dependencies.ExternalResourceCatalog satisfies it. The provision/value-
// collection paths build their RT-authoring definition straight off the
// project's committed design (build_provision.go / value_service.go), never
// a DB catalog — the external_resources table is gone.
type ExternalRTCatalog interface {
	List(ctx context.Context, orgID string) ([]openchoreo.ExternalResourceDefinition, error)
	Ensure(ctx context.Context, orgID string, rt *openchoreo.ResourceType) error
	Update(ctx context.Context, orgID string, rt *openchoreo.ResourceType) error
	Delete(ctx context.Context, orgID, name string) error
}

// CatalogValuePlane is the org value plane for Registered External resources.
// Production wires a process-local MemoryValuePlane so list-after-register
// shows envCells for the process lifetime. Tests inject Registered cells and
// instances through this seam. ListExternalResources copies cells/instances
// from the plane when non-nil; otherwise they stay empty and every live row
// remains Project External.
type CatalogValuePlane interface {
	EnvCells(orgID, name string) []EnvCell
	Instances(orgID, name string) []ResourceInstance
	PutEnvCells(orgID, name string, cells []EnvCell)
	PutInstances(orgID, name string, instances []ResourceInstance)
}

// OrgSecretWriter optionally persists Registered External secret bytes through
// the existing SM-API vault layout with projectName "org-catalog" (sentinel,
// not a real project) and returns the vault key the ResourceType CEL reads
// as secretStorePath. Nil is a documented no-op — HTTP tests leave it unwired
// and SecretStorePath then stays empty on the value plane.
type OrgSecretWriter interface {
	WriteOrgCatalogSecret(ctx context.Context, orgID, entityName string, data map[string]string) (vaultKey string, err error)
}

// OrgResourceDocs commits UTF-8 resource-docs files into the per-org
// org-resource-docs repo. Nil is a documented no-op for URL/path rows —
// HTTP tests leave it unwired except file-row tests. A file row with a
// nil store returns a 500-class wrap.
type OrgResourceDocs interface {
	CommitUTF8(ctx context.Context, orgID, logicalName, fileName, content string) (path string, err error)
}

// EnvironmentLister lists OpenChoreo Environment names for the org namespace.
// Empty org → empty slice, never nil error-for-empty.
type EnvironmentLister interface {
	ListNames(ctx context.Context, orgID string) ([]string, error)
}

// ProjectRef identifies one project (org + project id) for the cross-project
// design scan (external-resource consumers, teardown).
type ProjectRef struct {
	OrgID     string
	ProjectID string
}

// ProjectLister enumerates the org's projects so the service can scan their
// committed designs — the design-scan replacement for the dropped component_tasks
// consumer query (dependency-management §3.2 item 7). Wired from repositories.
type ProjectLister interface {
	ListProjects(ctx context.Context, orgID string) ([]ProjectRef, error)
}

// ExternalProvisioner authors the OC external Resource model + writes secrets to
// SM-API, and resolves the per-run secret bundles for the coding runner.
// *resources.ExternalResourceProvisioner satisfies it.
type ExternalProvisioner interface {
	Provision(ctx context.Context, orgHandle, ocOrgID, projectName string, er *dependencies.ExternalResource, byEnv map[string]dependencies.EnvValues) (*dependencies.ProvisionResult, error)
	// AuthorPreparedValues authors the OC external Resource model from
	// prepared plain values plus an optional secret reference — no SM-API write.
	// Builds use it to author empty/defaulted bindings and preserve any existing
	// configured values.
	AuthorPreparedValues(ctx context.Context, orgHandle, projectName string, er *dependencies.ExternalResource, byEnv map[string]dependencies.PreparedEnvValues) (*dependencies.ProvisionResult, error)
	Deprovision(ctx context.Context, orgHandle, projectName, name string, envs []string) error
	// ResolveRunnerSecrets returns the SM-API vault path + secret-key list for
	// each named external resource, read back off its per-env binding — the
	// inputs the coding dispatcher materialises into per-run ExternalSecrets so
	// the agent can integration-test against the live service.
	ResolveRunnerSecrets(ctx context.Context, orgHandle, projectName, env string, names []string) ([]dependencies.ExternalResourceRunnerSecret, error)
}

// PlatformProvisioner authors the OC Resource model for a platform-resource dep
// (async — returns before the binding is Ready). resources.ResourceProvisioner
// (impl *resources.OCNativeProvisioner) satisfies it.
type PlatformProvisioner = dependencies.ResourceProvisioner

// BindingReader reads an OC ResourceReleaseBinding for readiness + outputs.
// openchoreo.ResourceClient satisfies it.
type BindingReader interface {
	GetBinding(ctx context.Context, namespace, name string) (*openchoreo.ResourceReleaseBinding, error)
}

// WorkloadDepSource is the deployed-Workload consumer-dependency reader the
// Overview list uses. openchoreo.ResourceClient satisfies it (the same client
// Bindings and the external-resource catalog already share — not a second
// HTTP stack). List is org-scoped then filtered to projectName; GetResource
// 404s are dangling refs the service omits.
type WorkloadDepSource interface {
	ListWorkloadConsumerDeps(ctx context.Context, orgHandle, projectName string) ([]openchoreo.WorkloadConsumerDep, error)
	GetResource(ctx context.Context, namespace, name string) (*openchoreo.Resource, error)
	GetResourceType(ctx context.Context, namespace, name string) (*openchoreo.ResourceType, error)
}

// ProviderResolver resolves a dependency's provider endpoint in OpenChoreo. It
// has two readers with different visibility rules, so all three resolves live on
// one port (*dependencies.Catalog satisfies all of them):
//
//   - FindByComponent — ANY visibility, because an access request deliberately
//     targets a not-yet-published provider.
//   - ResolveNamespaceVisible / ResolveProjectEndpoint — the visibility-scoped
//     targets the ADR-0004 wiring comment names (wiring.go). A provider that is
//     not yet published at the required visibility simply misses, and its
//     endpoint is omitted from the block until it is.
type ProviderResolver interface {
	FindByComponent(ctx context.Context, orgHandle, name string) (openchoreo.WorkloadEndpointInfo, bool, error)
	ResolveNamespaceVisible(ctx context.Context, orgHandle, name string) (openchoreo.WorkloadEndpointInfo, bool, error)
	ResolveProjectEndpoint(ctx context.Context, orgHandle, project, ocComponent string) (openchoreo.WorkloadEndpointInfo, bool, error)
}

// ProviderBuildTrigger kicks the provider project's build so a not-yet-published
// org-service provider actually deploys (and, on deploy, publishes org-wide —
// resolving the consumer's visibility gate). Declared as a provisioning port so
// this feature never imports build/devflow (that would cycle); the app-root
// adapter (Task 5) wires the real build start. The trigger is idempotent: if a
// provider devflow is already running the adapter treats it as success. Nil is a
// documented best-effort no-op (logged) — the funnel still holds the consumer and
// the sweep heals once the provider deploys by any other path.
type ProviderBuildTrigger interface {
	TriggerBuild(ctx context.Context, orgID, projectID string) error
}

// AccessStore is the cross-project access-request tracking table.
// *repositories.AccessRequestRepository satisfies it.
type AccessStore interface {
	Create(ctx context.Context, ar *dependencies.AccessRequest) error
	ListByConsumerProject(ctx context.Context, orgID, projectID string) ([]dependencies.AccessRequest, error)
	FindOpenForTarget(ctx context.Context, orgID, providerProjectID, providerComponentName string) (*dependencies.AccessRequest, error)
	UpdateStatus(ctx context.Context, id, status string) error
	ListByProviderTask(ctx context.Context, providerTaskID string) ([]dependencies.AccessRequest, error)
}

// RolesEnsureOutcome is what one build-time roles ensure did.
//
// It carries no "did this project declare roles" flag: the gate asks
// DeclaresRoles FIRST and does not reach the ensure otherwise, so a second
// answer here would only be free to disagree with the one the gate acted on.
type RolesEnsureOutcome struct {
	// Summary is the human-readable account of what was created, reused, left
	// alone or refused — the gate's closing comment.
	Summary string
	// Refusals is true when something was left untouched because the platform
	// does not own it. Not a failure; a thing a human has to look at.
	Refusals bool
	// Credentials are the logins for the test accounts this project can sign in
	// as at this version. They are published in the gate's closing comment,
	// because that ticket is where the validation agent reads them from.
	//
	// This is the one field carrying a secret. It goes into the issue body and
	// NOWHERE else: never into a log line, never into Summary, never into a
	// ProvisionFailure reason.
	Credentials []RolesCredential
}

// RolesCredential is one published test-account login.
//
// An empty Password means the platform holds the account but could not open its
// seal. The comment says so per row rather than printing a blank, so a reader —
// human or agent — is never handed an empty string that looks like a password.
type RolesCredential struct {
	Username  string
	Password  string
	Role      string
	ColdStart bool
}

// RolesEnsurer makes the roles and test users a project's design declares real
// on the platform identity provider, at build time and with NO MODEL IN THE
// LOOP. Wired to the identity domain at the composition root.
//
// Enabled is false when the identity provider is not configured (a local stack
// without one); the roles gate is then skipped entirely rather than failing
// every build.
type RolesEnsurer interface {
	Enabled() bool
	// DeclaresRoles reports whether the design at the tag carries a roles
	// document. It is asked FIRST, so the gate can be minted open before the
	// work starts — the shape every other provisioning gate has. An error here
	// is a real failure, never "no roles".
	DeclaresRoles(ctx context.Context, orgID, projectID, tag string) (bool, error)
	EnsureRolesForBuild(ctx context.Context, orgID, projectID, tag string) (RolesEnsureOutcome, error)
}
