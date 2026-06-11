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

// OC's CRDs name resources via DNS-1123 strings, so the BFF ferries
// user-friendly display name + description through these annotations.
// Mirror agent-manager's `client/constants.go` so the wire shape stays
// identical across the two services.
const (
	AnnotationKeyDisplayName = "openchoreo.dev/display-name"
	AnnotationKeyDescription = "openchoreo.dev/description"
)

// LabelKeys is a typed-string alias for the `openchoreo.dev/*` label set we
// stamp on CRs. Matches agent-manager's pattern — typed string lets the
// label catalog stay a single source of truth and prevents stringly-typed
// drift at call sites.
type LabelKeys string

// SecretReference labels — stamped by EnsureSecretReference so the resulting
// K8s Secret routes onto the build pod's WorkflowPlane and is discoverable
// by kubectl `--selector` queries.
const (
	LabelKeyManagedBy         LabelKeys = "managed-by"
	LabelKeySecretType        LabelKeys = "kubernetes.io/secret-type"
	LabelKeyOCSecretType      LabelKeys = "openchoreo.dev/secret-type"
	LabelKeyWorkflowPlaneKind LabelKeys = "openchoreo.dev/workflow-plane-kind"
	LabelKeyWorkflowPlaneName LabelKeys = "openchoreo.dev/workflow-plane-name"
)

// Component / WorkflowRun labels — used for filtering list calls and
// resolving runs back to a project/component in the BFF webhook projector.
const (
	LabelKeyComponent     LabelKeys = "openchoreo.dev/component"
	LabelKeyProject       LabelKeys = "openchoreo.dev/project"
	LabelKeyComponentType LabelKeys = "openchoreo.dev/component-type"
	LabelKeyProjectName   LabelKeys = "openchoreo.dev/project-name"

	// app-factory-specific labels (NOT the openchoreo.dev/* prefix) carry
	// task / project / component identifiers on coding-agent WorkflowRuns
	// without triggering OC's ClusterWorkflow ↔ ClusterComponentType
	// allow-list validation (which keys off `openchoreo.dev/component`).
	LabelKeyAppFactoryCodingAgentTask LabelKeys = "app-factory.openchoreo.dev/coding-agent-task"
	LabelKeyAppFactoryProject         LabelKeys = "app-factory.openchoreo.dev/project"
	LabelKeyAppFactoryComponent       LabelKeys = "app-factory.openchoreo.dev/component"
)

// Stable label values we stamp on secret-references created by asdlc-service.
// Live in the same place as their keys so wire-shape changes are atomic.
const (
	LabelValueManagedBy           = "asdlc"
	LabelValueSecretTypeBasicAuth = "basic-auth"
	LabelValueSecretTypeGitCreds  = "git-credentials"
	LabelValueClusterWorkflowKind = "ClusterWorkflowPlane"
	LabelValueWorkflowPlaneName   = "default"
)

// WorkflowRun condition types — match OC's CRD's status.conditions[].type
// strings. workflowRunToModel scans for these to compute the run's
// canonical Status / Completed flags without substring-matching.
const (
	WorkflowConditionCompleted = "WorkflowCompleted"
	WorkflowConditionRunning   = "WorkflowRunning"
)

// WorkflowRun condition Reasons. Mirrors OC's
// internal/controller/workflowrun/controller_conditions.go reason constants.
// Watchers compare against these instead of substring-matching the reason
// string lifted into models.WorkflowRun.Status.
const (
	ReasonWorkflowSucceeded = "WorkflowSucceeded"
	ReasonWorkflowFailed    = "WorkflowFailed"
	ReasonWorkflowRunning   = "WorkflowRunning"
)
