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

package openchoreo

import "time"

// -- Component ---------------------------------------------------------------

// -- Create Component --------------------------------------------------------

type WorkflowRevision struct {
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`
}

type WorkflowRepository struct {
	URL       string            `json:"url,omitempty"`
	SecretRef string            `json:"secretRef,omitempty"`
	Revision  *WorkflowRevision `json:"revision,omitempty"`
	AppPath   string            `json:"appPath,omitempty"`
}

type DockerParameters struct {
	Context  string `json:"context,omitempty"`
	FilePath string `json:"filePath,omitempty"`
}

// WorkflowEnvVarRef is the BFF-internal shape for a per-component env
// var. The componentClient maps it onto a ReleaseBinding's
// `spec.workloadOverrides.container.env` so OC's controller stamps the
// values into the rendered pod spec — no rebuild needed. Either Value
// or ValueFrom must be set, not both.
type WorkflowEnvVarRef struct {
	Key       string                  `json:"key"`
	Value     string                  `json:"value,omitempty"`
	ValueFrom *WorkflowEnvVarValueRef `json:"valueFrom,omitempty"`
}

type WorkflowEnvVarValueRef struct {
	SecretKeyRef *WorkflowSecretKeyRef `json:"secretKeyRef,omitempty"`
}

type WorkflowSecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// WorkflowFileVar is the BFF-internal shape for a literal file injected
// onto a ReleaseBinding's `spec.workloadOverrides.container.files`. OC's
// controller materialises it as a ConfigMap mounted at the declared
// mountPath, so the pod sees the file without any rebuild. Used by the
// runtime-config pipeline to write `env-config.js` into the SPA pod's
// `/usr/share/nginx/html/` directory (stock nginx serves it as plain
// static).
type WorkflowFileVar struct {
	Key       string `json:"key"`
	MountPath string `json:"mountPath"`
	Value     string `json:"value"`
}

type ComponentWorkflowParameters struct {
	Repository *WorkflowRepository `json:"repository,omitempty"`
	Docker     *DockerParameters   `json:"docker,omitempty"`
}

type ComponentWorkflowSpec struct {
	Kind       string                       `json:"kind,omitempty"`
	Name       string                       `json:"name,omitempty"`
	Parameters *ComponentWorkflowParameters `json:"parameters,omitempty"`
}

type CreateComponentRequest struct {
	Name        string                 `json:"name"`
	DisplayName string                 `json:"displayName"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	AutoBuild   bool                   `json:"autoBuild,omitempty"`
	AutoDeploy  bool                   `json:"autoDeploy,omitempty"`
	Workflow    *ComponentWorkflowSpec `json:"workflow,omitempty"`
	// Traits are ClusterTrait attachments emitted by the BFF based on
	// design.json (e.g. `api-configuration` when
	// `exposesAPI.auth: end-user-required`). projects.DesiredDeploymentFor is the
	// canonical projection — it computes this shape and the binding's matching
	// per-environment config together, so the two cannot disagree.
	Traits []ComponentTrait `json:"traits,omitempty"`
	// Labels are stamped onto metadata.labels (e.g. aep.wso2.com/* markers on
	// ephemeral coding-agent Components).
	Labels map[string]string `json:"labels,omitempty"`
	// Parameters are ComponentType parameter values (schema from the
	// referenced type — e.g. activeDeadlineSeconds for coding-agent).
	Parameters map[string]any `json:"parameters,omitempty"`
}

// InternalComponent is one aep-internal Component as the retention reaper and
// the cancel path see it: enough to decide whether it may be deleted, and by
// which name. Never served over HTTP.
type InternalComponent struct {
	// Name is the FRIENDLY name (project prefix stripped) — the argument
	// DeleteComponent takes, and the `ca-…` run name a cycle records as its
	// JobRef.
	Name string
	// TypeName is `spec.componentType.name` (e.g. "job/coding-agent"), so a
	// caller can act on one kind of internal component without touching
	// another's.
	TypeName string
	// CycleID is the `aep.wso2.com/cycle` marker: the run cycle that dispatched
	// this component, and the key that decides whether it is still live.
	CycleID string
	// RunName is the `aep.wso2.com/run-name` marker. Equal to Name in normal
	// operation; carried separately so a mismatch is observable rather than
	// assumed away.
	RunName string
	// CreatedAt is the CR's creation timestamp — the LRU order.
	CreatedAt time.Time
}

// WorkloadInput is the BFF-side payload for EnsureWorkload: image + env
// (plain values and secretKeyRef entries — never secret values).
type WorkloadInput struct {
	// ComponentName is the FRIENDLY component name; the client scopes it.
	ComponentName string
	Image         string
	Env           []WorkflowEnvVarRef
	Labels        map[string]string
}

// ComponentTrait is the BFF-internal shape of a ClusterTrait attachment
// on a Component. Mirrors OC's ComponentTrait gen-type but uses our own
// types so callers don't need to import the gen package.
type ComponentTrait struct {
	InstanceName string                 `json:"instanceName"`
	Kind         string                 `json:"kind"` // "ClusterTrait"
	Name         string                 `json:"name"` // e.g. "api-configuration"
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
}

// -- WorkflowRun (builds) ----------------------------------------------------

// -- Deployment (ReleaseBinding) ---------------------------------------------

// DevEnvironmentName is the platform's fixed dev environment — the OC
// environment every project auto-deploys to. The single shared constant for
// what was previously pinned per-feature (runtimeconfig, provisioning,
// codingagent, project status).
const DevEnvironmentName = "development"

// ComponentSpecDesired is the platform-owned half of a Component's spec: the
// trait shape and the build/deploy policy.
//
// Both fields are plain bools with no "leave it alone" option, deliberately.
// Every component this platform creates is BFF-built and BFF-deployed, so
// AutoBuild/AutoDeploy have exactly one correct value; making them optional
// would only create a way for a component to keep an inherited setting that
// puts OpenChoreo's controller back in the deploy path.
type ComponentSpecDesired struct {
	Traits     []ComponentTrait
	AutoBuild  bool
	AutoDeploy bool
}

// ReleaseBindingDesired is the WHOLE binding a caller wants, in one value —
// the pin plus every field the platform owns on it. It exists because a
// ReleaseBinding is one object with one writer: composing the pin, the trait
// configs and the workload overrides into a single desired state is what makes
// the binding renderable at every instant, rather than briefly holding a trait
// whose per-environment config has not been written yet (which fails the whole
// render, not just that trait).
//
// The two authoritative fields are POINTER-shaped in effect: a nil map / nil
// pointer means "this caller does not manage that field, leave it alone", and a
// non-nil value REPLACES the field wholesale. That distinction is what lets the
// ephemeral coding-agent path (which owns only the pin) and the user-component
// deploy path (which owns everything) share one verb without either erasing the
// other's writes.
type ReleaseBindingDesired struct {
	// ComponentName is the FRIENDLY name; the client scopes it.
	ComponentName string
	Environment   string
	// ReleaseName is the pin — writing it IS the deploy.
	ReleaseName string
	// State is spec.state ("Active" / "Undeploy"). Empty leaves OC's default.
	State string
	// TraitEnvironmentConfigs, when non-nil, replaces spec.traitEnvironmentConfigs
	// entirely: an instance absent from the map is an instance the platform no
	// longer wants, so removal needs no tombstone value of its own.
	TraitEnvironmentConfigs map[string]map[string]interface{}
	// Env / Files, when non-nil, replace spec.workloadOverrides.container.{env,files}.
	// Non-nil-but-empty is meaningful: it clears the field.
	Env   []WorkflowEnvVarRef
	Files []WorkflowFileVar
	// Labels, when non-empty, are written to metadata.labels at CREATE only —
	// nil means unmanaged, the same rule the fields above follow. It exists for
	// the internal marker: a binding is the one link in the ephemeral
	// coding-agent chain that carried no marker, which left the project-status
	// read counting agent Jobs as the project's deployments.
	Labels map[string]string
}

// ReleaseBindingState values for ReleaseBindingDesired.State.
const (
	ReleaseBindingStateActive   = "Active"
	ReleaseBindingStateUndeploy = "Undeploy"
)

// ReleaseBindingSummary is one ReleaseBinding's identity plus its aggregate
// Ready condition — the minimal view the project-status deploy stage derives
// from. Internal to the BFF (never served raw); the stage predicates live in
// the project feature.
type ReleaseBindingSummary struct {
	ComponentName string // friendly name (project prefix stripped)
	Environment   string
	// Undeploy: spec.state == Undeploy — intentionally not deployed;
	// excluded from deploy-stage counts and status.
	Undeploy bool
	// ReadyStatus is the Ready-typed condition's status: "True", "False",
	// "Unknown", or "" when the condition is absent (still being evaluated).
	ReadyStatus string
	// ReadyReason is the Ready-typed condition's reason (OC copies the
	// failing sub-condition's reason into the aggregate).
	ReadyReason string
	// ReleaseName is the ComponentRelease the binding PINS (spec.releaseName),
	// "" when it pins none.
	//
	// It is what separates "this component is serving the release it should be"
	// from "this component is serving an older one": Ready alone says only that
	// whatever is pinned came up, and a version behind by one release is Ready
	// and wrong. The run supervisor compares it against the release the
	// component's newest succeeded build would cut (delivery.ReleaseNameFor).
	ReleaseName string
}

// -- ComponentOpenAPI (Test tab) ----------------------------------------------

// BuildRunSummary is one build WorkflowRun reduced to the facts the milestone
// loop decides on, with OpenChoreo's condition vocabulary already mapped.
//
// Status is carried verbatim beside the two booleans because OC's status is a
// condition REASON — an open string, not a closed set — so it is display and
// logging material, while Completed and Succeeded are what anything branches on.
type BuildRunSummary struct {
	Name      string
	Status    string
	Completed bool
	Succeeded bool
	// CommitSHA is the commit the run was pinned to, "" for a build of whatever
	// the branch tip was.
	CommitSHA string
	// StartedAt is the run's creation timestamp — what orders two succeeded
	// builds of the same component. The list is not ordered by the host, so
	// "the newest green build" has to be decided on a fact the run carries.
	StartedAt time.Time
}

// -- Build Logs ---------------------------------------------------------------
