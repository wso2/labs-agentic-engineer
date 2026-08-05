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

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
)

//go:generate go run github.com/matryer/moq@v0.7.1 -rm -fmt goimports -pkg mocks -out mocks/component_client_mock.go . ComponentClient

// ComponentClient defines operations for managing OpenChoreo components.
// Every method that names a component takes the user-friendly componentName
// plus the projectName that scopes it. The client applies ScopedComponentName
// internally so callers never deal with the prefixed k8s name.
//
// Deploy chain: with AutoDeploy=true set on the Component (see
// dispatch_service.ensureOCComponent), OC's Component controller owns the
// Workload → ComponentRelease → ReleaseBinding fan-out. The build
// workflow's `generate-workload-cr` step POSTs the Workload; the
// controller picks it up, hashes the spec, creates a ComponentRelease,
// and binds it into the project's first environment. The BFF only reads
// the result back via ListDeployments. Wrappers for the write side of
// that chain are deliberately absent — no caller needs them yet.
type ComponentClient interface {
	ListComponents(ctx context.Context, orgName, projectName string, limit int, cursor string) (*gen.ComponentList, error)
	GetComponent(ctx context.Context, orgName, projectName, componentName string) (*gen.Component, error)
	CreateComponent(ctx context.Context, orgName, projectName string, req *CreateComponentRequest) (*gen.Component, error)
	// UpdateComponentWorkflowEnvVars writes per-component env vars onto each
	// of the component's ReleaseBindings at
	// `spec.workloadOverrides.container.env`. Per-env (one RB per
	// environment) so OC's controller renders the values straight into the
	// pod spec on the next reconcile — no rebuild required, matching how
	// PE-managed components (aep-api, agent-manager-service, etc.)
	// carry their env. ReleaseBindings are listed by component label and
	// each is updated independently; if no RBs exist yet (pre-first-deploy)
	// the call is a soft no-op and the caller is expected to retry once
	// the first build has produced RBs.
	UpdateComponentWorkflowEnvVars(ctx context.Context, orgName, projectName, componentName string, envVars []WorkflowEnvVarRef) error

	// UpdateComponentWorkflowFiles writes per-component literal files onto
	// each of the component's ReleaseBindings at
	// `spec.workloadOverrides.container.files`. Used by the runtime-config
	// pipeline to drop `env-config.js` (and any other literal file) into
	// the pod via an OC-rendered ConfigMap mounted at the declared
	// mountPath — no rebuild needed. As with UpdateComponentWorkflowEnvVars,
	// when no ReleaseBindings exist yet the call is a soft no-op and the
	// caller is expected to retry after the first build produces RBs.
	UpdateComponentWorkflowFiles(ctx context.Context, orgName, projectName, componentName string, files []WorkflowFileVar) error

	// DeleteComponent removes the Component CR. OC's controller GCs the
	// chain (Component → ReleaseBinding → RenderedRelease → Deployment /
	// Service / HTTPRoute) via k8s ownerReferences. NOTE: trait-emitted
	// resources (Backend, RestApi) DO NOT carry owner refs back to the
	// Component (the canonical `api-configuration` trait template's
	// `creates` block omits them — see deployments/manifests/api-platform/
	// api-configuration-trait.yaml). Cascade is therefore PARTIAL; the
	// dp-namespace may retain orphaned Backend/RestApi resources. The
	// caller (designService.DeleteComponent) emits an audit log entry
	// reflecting that gap and a follow-up sweep is required to clean
	// them up. Returns ErrComponentNotFound when the Component does not
	// exist (idempotent — 404 is treated as success).
	DeleteComponent(ctx context.Context, orgName, projectName, componentName string) error

	// UpdateComponentTraits replaces `spec.traits` on an existing Component
	// with the supplied slice. Passing an empty slice clears traits.
	// Returns ErrComponentNotFound when the Component does not exist (the
	// caller decides whether to recreate or no-op). Used by trait_sync.go
	// when a user toggles `exposesAPI.auth` on `design.json` after first deploy.
	UpdateComponentTraits(ctx context.Context, orgName, projectName, componentName string, traits []ComponentTrait) error

	// UpdateComponentTraitEnvironmentConfigs writes per-environment trait
	// configs onto each of the component's ReleaseBindings at
	// `spec.traitEnvironmentConfigs`. Configs is keyed by trait instance
	// name; the value is the parameters block (e.g. `{"jwtAuth": {"enabled": true}}`).
	// Passing an empty map clears the field. When no RBs exist yet (pre-
	// first-deploy) the call is a soft no-op — the caller retries via the
	// trait-sync watcher once the deploy chain catches up.
	UpdateComponentTraitEnvironmentConfigs(ctx context.Context, orgName, projectName, componentName string, configs map[string]map[string]interface{}) error

	// Deploy (read-only — auto-deploy on the Component drives the chain)
	ListDeployments(ctx context.Context, orgName, projectName, componentName string) (*gen.DeploymentList, error)

	// ListProjectReleaseBindings returns the org's ReleaseBindings owned by
	// projectName — all environments, all components, in ONE org-scoped list
	// (the API has no project filter; ownership is matched client-side on
	// spec.owner.projectName), following pagination. Each item carries the
	// aggregate Ready condition — the project-status deploy stage's source.
	ListProjectReleaseBindings(ctx context.Context, orgName, projectName string) ([]ReleaseBindingSummary, error)

	// Build (workflow runs). `runName` is the WorkflowRun metadata.name; if
	// empty the OC client auto-generates one via NewBuildRunName. Callers
	// that need to know the name ahead of time (so they can stage a
	// per-WorkflowRun build Secret) MUST pass it.
	// secretRef sets parameters.repository.secretRef so the dockerfile-builder
	// workflow synthesises the git Secret from the org's SecretReference
	// (provisioned by BuildCredentialsService). Empty leaves it blank — the
	// build clones unauthenticated (public repos only).
	TriggerBuild(ctx context.Context, orgName, projectName, componentName, secretRef, runName string) (*gen.WorkflowRun, error)
	// TriggerBuildAtCommit creates a WorkflowRun pinned to commitSHA via
	// params.repository.revision.commit. Mirrors agent-manager's pattern at
	// agent-manager-service/clients/openchoreosvc/client/builds.go:71-85.
	// See TriggerBuild for the `runName` + `secretRef` contracts.
	TriggerBuildAtCommit(ctx context.Context, orgName, projectName, componentName, commitSHA, secretRef, runName string) (*gen.WorkflowRun, error)
	// TriggerCodingAgent creates a WorkflowRun of ClusterWorkflow
	// `aep-coding-agent` for the per-task ephemeral pod that runs the
	// Claude Agent SDK against the task's feature branch. The label
	// `aep.openchoreo.dev/coding-agent-task` carries the taskId so
	// the BFF watcher can correlate runs back to the task.
	TriggerCodingAgent(ctx context.Context, params CodingAgentParams) (*gen.WorkflowRun, error)
	ListWorkflowRuns(ctx context.Context, orgName, projectName, componentName string, limit int, cursor string) (*gen.WorkflowRunList, error)
	// ListProjectWorkflowRuns is the same read widened to every component in
	// the project — one call instead of one per component. The run read uses it
	// to derive a cycle's builds from its merge SHA without first having to
	// learn which components the merge touched.
	ListProjectWorkflowRuns(ctx context.Context, orgName, projectName string, limit int, cursor string) (*gen.WorkflowRunList, error)
	GetWorkflowRun(ctx context.Context, orgName, runName string) (*gen.WorkflowRun, error)
}

// CodingAgentParams is the input to TriggerCodingAgent. Mirrors the schema
// of `aep-coding-agent` ClusterWorkflow. All fields are required.
// The agent itself creates the feature branch and opens the PR (with
// `Closes #<issueNumber>` so the BFF webhook can link it back to the
// task), so no branch is plumbed through here.
type CodingAgentParams struct {
	OrgName       string
	ProjectName   string
	ComponentName string
	TaskID        string
	Prompt        string
	RepoURL       string
	IdentityName  string
	IdentityEmail string
	IdentityLogin string
	Bearer        string
	GitServiceURL string
	// PlatformURL is the BFF base URL the runner pod uses for its callbacks
	// (credentials refresh). Passed through to the ClusterWorkflow parameter
	// `bff.platformUrl` → env var AEP_PLATFORM_URL in the pod.
	PlatformURL string
	// AnthropicSecretRef is the name of the per-org K8s Secret in
	// workflows-<OrgName> carrying ANTHROPIC_API_KEY. Materialised by
	// AnthropicCredentialService.ApplyWPSecret in the dispatch pre-flight.
	// The ClusterWorkflow wires
	// it into the pod via `parameters.anthropic.secretRef` →
	// `secretKeyRef.name`. See docs/design/anthropic-key-dual-token.md §5.
	AnthropicSecretRef string
}

type componentClient struct {
	oc *ocgen.ClientWithResponses
}

func NewComponentClient(cfg Config) ComponentClient {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo component client: %w", err))
	}
	return &componentClient{oc: oc}
}

// -- Conversions -------------------------------------------------------------

// designComponentType maps an OpenChoreo ComponentType name to AEP's design
// vocabulary — the read-path inverse of the component feature's ocEntrypoint
// mapping. OC's names are AEP's kinds prefixed with "deployment/"
// (deployment/web-application → web-application); the prefix never leaves
// this client.
func designComponentType(ocType string) string {
	return strings.TrimPrefix(ocType, "deployment/")
}

func componentToModel(c ocgen.Component) gen.Component {
	var projectName, componentType string
	var autoBuild, autoDeploy bool
	if c.Spec != nil {
		projectName = c.Spec.Owner.ProjectName
		componentType = c.Spec.ComponentType.Name
		if c.Spec.AutoBuild != nil {
			autoBuild = *c.Spec.AutoBuild
		}
		if c.Spec.AutoDeploy != nil {
			autoDeploy = *c.Spec.AutoDeploy
		}
	}
	if projectName == "" {
		projectName = label(c.Metadata.Labels, string(LabelKeyProjectName))
	}
	if componentType == "" {
		componentType = label(c.Metadata.Labels, string(LabelKeyComponentType))
	}

	var status string
	if c.Status != nil {
		status = latestConditionReason(c.Status.Conditions)
	}

	return gen.Component{
		UID:         derefStr(c.Metadata.Uid),
		Name:        FriendlyComponentName(c.Metadata.Name, projectName),
		ProjectName: projectName,
		DisplayName: annotation(c.Metadata.Annotations, AnnotationKeyDisplayName),
		Description: annotation(c.Metadata.Annotations, AnnotationKeyDescription),
		Type:        designComponentType(componentType),
		AutoDeploy:  autoDeploy,
		AutoBuild:   autoBuild,
		CreatedAt:   derefTimeRFC3339(c.Metadata.CreationTimestamp),
		Status:      status,
	}
}

// workflowRunToModel derives the run's status from OC's conditions: Reason of
// WorkflowCompleted wins; otherwise WorkflowRunning sets "Running"; default
// "Pending". `Completed` flips when WorkflowCompleted has Status=True —
// watchers gate terminal transitions on this, not on substring-matching the
// Status string.
func workflowRunToModel(run ocgen.WorkflowRun) gen.WorkflowRun {
	var componentName, projectName string
	if run.Metadata.Labels != nil {
		componentName = label(run.Metadata.Labels, string(LabelKeyComponent))
		projectName = label(run.Metadata.Labels, string(LabelKeyProject))
	}

	status := "Pending"
	completed := false
	if run.Status != nil && run.Status.Conditions != nil {
		for _, cond := range *run.Status.Conditions {
			if cond.Type == WorkflowConditionCompleted {
				if cond.Status == "True" {
					completed = true
				}
				if cond.Reason != "" {
					status = cond.Reason
					break
				}
			}
			if cond.Type == WorkflowConditionRunning && cond.Status == "True" {
				status = "Running"
			}
		}
	}

	var tasks []gen.WorkflowRunTask
	if run.Status != nil && run.Status.Tasks != nil {
		tasks = make([]gen.WorkflowRunTask, 0, len(*run.Status.Tasks))
		for _, t := range *run.Status.Tasks {
			tasks = append(tasks, gen.WorkflowRunTask{
				Name:        t.Name,
				Phase:       derefStr(t.Phase),
				Message:     derefStr(t.Message),
				StartedAt:   derefTimeRFC3339(t.StartedAt),
				CompletedAt: derefTimeRFC3339(t.CompletedAt),
			})
		}
	}

	return gen.WorkflowRun{
		Name:          run.Metadata.Name,
		Status:        status,
		StartedAt:     derefTimeRFC3339(run.Metadata.CreationTimestamp),
		ComponentName: FriendlyComponentName(componentName, projectName),
		ProjectName:   projectName,
		Completed:     completed,
		Tasks:         tasks,
	}
}

// deploymentFromReleaseBinding pulls the first HTTP external URL from the
// binding's resolved endpoints via the typed `ExternalURLs.Http *EndpointURL`.
func deploymentFromReleaseBinding(rb ocgen.ReleaseBinding) gen.Deployment {
	var projectName, componentName, environment, releaseName string
	if rb.Spec != nil {
		projectName = rb.Spec.Owner.ProjectName
		componentName = rb.Spec.Owner.ComponentName
		environment = rb.Spec.Environment
		releaseName = derefStr(rb.Spec.ReleaseName)
	}

	var endpointURL string
	if rb.Status != nil && rb.Status.Endpoints != nil {
		for _, ep := range *rb.Status.Endpoints {
			if ep.ExternalURLs != nil && ep.ExternalURLs.Http != nil {
				endpointURL = formatEndpointURL(ep.ExternalURLs.Http)
				break
			}
		}
	}

	var status string
	if rb.Status != nil {
		status = latestConditionReason(rb.Status.Conditions)
	}

	return gen.Deployment{
		Name:          rb.Metadata.Name,
		Environment:   environment,
		ReleaseName:   releaseName,
		ComponentName: FriendlyComponentName(componentName, projectName),
		EndpointURL:   endpointURL,
		CreatedAt:     derefTimeRFC3339(rb.Metadata.CreationTimestamp),
		Status:        status,
	}
}

// formatEndpointURL renders ocgen.EndpointURL as scheme://host:port/path. Path
// of "" or "/" yields a trailing "/" for stable display.
func formatEndpointURL(u *ocgen.EndpointURL) string {
	if u == nil {
		return ""
	}
	scheme := derefStr(u.Scheme)
	path := derefStr(u.Path)
	port := 0
	if u.Port != nil {
		port = int(*u.Port)
	}
	if path == "" || path == "/" {
		return fmt.Sprintf("%s://%s:%d/", scheme, u.Host, port)
	}
	return fmt.Sprintf("%s://%s:%d%s", scheme, u.Host, port, path)
}

// -- Component CRUD ----------------------------------------------------------

func (c *componentClient) ListComponents(ctx context.Context, orgName, projectName string, limit int, cursor string) (*gen.ComponentList, error) {
	params := &ocgen.ListComponentsParams{}
	sel := ocgen.LabelSelectorParam(fmt.Sprintf("%s=%s", string(LabelKeyProject), projectName))
	params.LabelSelector = &sel
	if limit > 0 {
		l := ocgen.LimitParam(limit)
		params.Limit = &l
	}
	if cursor != "" {
		cur := ocgen.CursorParam(cursor)
		params.Cursor = &cur
	}

	resp, err := c.oc.ListComponentsWithResponse(ctx, orgName, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list components: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON500: resp.JSON500,
		})
	}

	items := make([]gen.Component, len(resp.JSON200.Items))
	for i, comp := range resp.JSON200.Items {
		items[i] = componentToModel(comp)
	}
	return &gen.ComponentList{Items: items}, nil
}

func (c *componentClient) GetComponent(ctx context.Context, orgName, projectName, componentName string) (*gen.Component, error) {
	k8sName := ScopedComponentName(projectName, componentName)
	resp, err := c.oc.GetComponentWithResponse(ctx, orgName, k8sName)
	if err != nil {
		return nil, fmt.Errorf("failed to get component: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	comp := componentToModel(*resp.JSON200)
	return &comp, nil
}

func (c *componentClient) CreateComponent(ctx context.Context, orgName, projectName string, req *CreateComponentRequest) (*gen.Component, error) {
	resp, err := c.oc.CreateComponentWithResponse(ctx, orgName, buildCreateComponentBody(projectName, req))
	if err != nil {
		return nil, fmt.Errorf("failed to create component: %w", err)
	}

	switch {
	case resp.StatusCode() == http.StatusCreated && resp.JSON201 != nil:
		comp := componentToModel(*resp.JSON201)
		return &comp, nil
	case resp.StatusCode() == http.StatusConflict:
		// Idempotent on (ocOrgId, project, componentName): a 409 means the
		// component already exists, so fetch and return it.
		existing, gerr := c.GetComponent(ctx, orgName, projectName, req.Name)
		if gerr != nil {
			return nil, fmt.Errorf("create component returned conflict; refetch failed: %w", gerr)
		}
		return existing, nil
	}

	return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
		JSON400: resp.JSON400,
		JSON401: resp.JSON401,
		JSON403: resp.JSON403,
		JSON409: resp.JSON409,
		JSON500: resp.JSON500,
	})
}

// UpdateComponentWorkflowEnvVars lists the component's ReleaseBindings
// and writes the env vars onto each one at
// `spec.workloadOverrides.container.env`. One RB per environment — OC's
// controller renders the value into the pod spec on the next reconcile,
// so changing env vars no longer requires a rebuild.
//
// When no ReleaseBindings exist yet (the first build hasn't produced
// one), the call is a soft no-op: the caller is expected to retry after
// a successful deploy. An empty `envVars` slice clears any previously
// set env block on each binding.
func (c *componentClient) UpdateComponentWorkflowEnvVars(ctx context.Context, orgName, projectName, componentName string, envVars []WorkflowEnvVarRef) error {
	scopedComp := ScopedComponentName(projectName, componentName)
	componentQ := ocgen.ComponentQueryParam(scopedComp)
	listResp, err := c.oc.ListReleaseBindingsWithResponse(ctx, orgName, &ocgen.ListReleaseBindingsParams{
		Component: &componentQ,
	})
	if err != nil {
		return fmt.Errorf("failed to list release bindings for env-var update: %w", err)
	}
	if listResp.StatusCode() != http.StatusOK || listResp.JSON200 == nil {
		return handleErrorResponse(listResp.StatusCode(), ErrorResponses{
			JSON401: listResp.JSON401,
			JSON403: listResp.JSON403,
			JSON500: listResp.JSON500,
		})
	}

	rbs := listResp.JSON200.Items
	if len(rbs) == 0 {
		// First build hasn't produced a ReleaseBinding yet — nothing to
		// patch. The caller retries once the deploy chain catches up.
		return nil
	}

	envList := workflowEnvVarRefsToGen(envVars)
	for _, rb := range rbs {
		if rb.Spec == nil {
			rb.Spec = &ocgen.ReleaseBindingSpec{}
		}
		if rb.Spec.WorkloadOverrides == nil {
			rb.Spec.WorkloadOverrides = &ocgen.WorkloadOverrides{}
		}
		if rb.Spec.WorkloadOverrides.Container == nil {
			rb.Spec.WorkloadOverrides.Container = &ocgen.ContainerOverride{}
		}
		rb.Spec.WorkloadOverrides.Container.Env = envList

		updResp, uerr := c.oc.UpdateReleaseBindingWithResponse(ctx, orgName, ocgen.ReleaseBindingNameParam(rb.Metadata.Name), ocgen.UpdateReleaseBindingJSONRequestBody(rb))
		if uerr != nil {
			return fmt.Errorf("failed to update release binding %s: %w", rb.Metadata.Name, uerr)
		}
		if updResp.StatusCode() != http.StatusOK && updResp.StatusCode() != http.StatusCreated {
			return handleErrorResponse(updResp.StatusCode(), ErrorResponses{
				JSON400: updResp.JSON400,
				JSON401: updResp.JSON401,
				JSON403: updResp.JSON403,
				JSON404: updResp.JSON404,
				JSON500: updResp.JSON500,
			})
		}
	}
	return nil
}

// UpdateComponentWorkflowFiles lists the component's ReleaseBindings and
// writes the literal files onto each one at
// `spec.workloadOverrides.container.files`. Per-env (one RB per
// environment) so OC's controller materialises a ConfigMap mounted at
// the declared mountPath on the next reconcile — no rebuild required.
//
// When no ReleaseBindings exist yet (the first build hasn't produced
// one), the call is a soft no-op: the caller is expected to retry after
// a successful deploy. An empty `files` slice clears any previously set
// files block on each binding.
func (c *componentClient) UpdateComponentWorkflowFiles(ctx context.Context, orgName, projectName, componentName string, files []WorkflowFileVar) error {
	scopedComp := ScopedComponentName(projectName, componentName)
	componentQ := ocgen.ComponentQueryParam(scopedComp)
	listResp, err := c.oc.ListReleaseBindingsWithResponse(ctx, orgName, &ocgen.ListReleaseBindingsParams{
		Component: &componentQ,
	})
	if err != nil {
		return fmt.Errorf("failed to list release bindings for file update: %w", err)
	}
	if listResp.StatusCode() != http.StatusOK || listResp.JSON200 == nil {
		return handleErrorResponse(listResp.StatusCode(), ErrorResponses{
			JSON401: listResp.JSON401,
			JSON403: listResp.JSON403,
			JSON500: listResp.JSON500,
		})
	}

	rbs := listResp.JSON200.Items
	if len(rbs) == 0 {
		// First build hasn't produced a ReleaseBinding yet — soft no-op.
		return nil
	}

	fileList := workflowFileVarsToGen(files)
	for _, rb := range rbs {
		if rb.Spec == nil {
			rb.Spec = &ocgen.ReleaseBindingSpec{}
		}
		if rb.Spec.WorkloadOverrides == nil {
			rb.Spec.WorkloadOverrides = &ocgen.WorkloadOverrides{}
		}
		if rb.Spec.WorkloadOverrides.Container == nil {
			rb.Spec.WorkloadOverrides.Container = &ocgen.ContainerOverride{}
		}
		rb.Spec.WorkloadOverrides.Container.Files = fileList

		updResp, uerr := c.oc.UpdateReleaseBindingWithResponse(ctx, orgName, ocgen.ReleaseBindingNameParam(rb.Metadata.Name), ocgen.UpdateReleaseBindingJSONRequestBody(rb))
		if uerr != nil {
			return fmt.Errorf("failed to update release binding %s files: %w", rb.Metadata.Name, uerr)
		}
		if updResp.StatusCode() != http.StatusOK && updResp.StatusCode() != http.StatusCreated {
			return handleErrorResponse(updResp.StatusCode(), ErrorResponses{
				JSON400: updResp.JSON400,
				JSON401: updResp.JSON401,
				JSON403: updResp.JSON403,
				JSON404: updResp.JSON404,
				JSON500: updResp.JSON500,
			})
		}
	}
	return nil
}

// workflowFileVarsToGen converts the BFF-internal file model into
// the ocgen.FileVar slice for ReleaseBinding workloadOverrides. An empty
// `files` returns a pointer to an empty slice so the server-side patch
// clears the field rather than leaving stale values in place.
func workflowFileVarsToGen(files []WorkflowFileVar) *[]ocgen.FileVar {
	out := make([]ocgen.FileVar, 0, len(files))
	for _, f := range files {
		v := f.Value
		out = append(out, ocgen.FileVar{
			Key:       f.Key,
			MountPath: f.MountPath,
			Value:     &v,
		})
	}
	return &out
}

// DeleteComponent issues DELETE against OC's Component endpoint. Returns
// nil on 200/204 OR 404 (idempotent — deleting a non-existent component
// is a success). OC's controller cascades the chain via k8s ownerRefs;
// trait-emitted Backend / RestApi resources are NOT covered by that
// cascade (see interface comment), so callers must surface that gap to
// the operator. The Component CR's RB list is GC'd by OC itself — the
// BFF doesn't need to delete each RB individually.
func (c *componentClient) DeleteComponent(ctx context.Context, orgName, projectName, componentName string) error {
	scopedComp := ScopedComponentName(projectName, componentName)
	resp, err := c.oc.DeleteComponentWithResponse(ctx, orgName, ocgen.ComponentNameParam(scopedComp))
	if err != nil {
		return fmt.Errorf("failed to delete component: %w", err)
	}
	switch resp.StatusCode() {
	case http.StatusOK, http.StatusNoContent, http.StatusAccepted:
		return nil
	case http.StatusNotFound:
		// Idempotent — caller's intent is satisfied.
		return nil
	}
	return handleErrorResponse(resp.StatusCode(), ErrorResponses{
		JSON401: resp.JSON401,
		JSON403: resp.JSON403,
		JSON404: resp.JSON404,
		JSON500: resp.JSON500,
	})
}

// UpdateComponentTraits replaces spec.traits on the named Component. GET-
// then-PUT to satisfy OC's full-object update semantics. Pass an empty
// slice to clear traits.
func (c *componentClient) UpdateComponentTraits(ctx context.Context, orgName, projectName, componentName string, traits []ComponentTrait) error {
	scopedComp := ScopedComponentName(projectName, componentName)
	// GET and PUT both inside the retried closure: a retry has to re-read, or
	// it replays the same stale resourceVersion. See stale_write.go.
	return retryStaleWrite(ctx, "component/"+scopedComp+" spec.traits", func(ctx context.Context) error {
		getResp, err := c.oc.GetComponentWithResponse(ctx, orgName, scopedComp)
		if err != nil {
			return fmt.Errorf("failed to get component for traits update: %w", err)
		}
		if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
			return handleErrorResponse(getResp.StatusCode(), ErrorResponses{
				JSON401: getResp.JSON401,
				JSON403: getResp.JSON403,
				JSON404: getResp.JSON404,
				JSON500: getResp.JSON500,
			})
		}
		comp := *getResp.JSON200
		if comp.Spec == nil {
			comp.Spec = &ocgen.ComponentSpec{}
		}
		comp.Spec.Traits = componentTraitsToGen(traits)

		updResp, err := c.oc.UpdateComponentWithResponse(ctx, orgName, ocgen.ComponentNameParam(scopedComp), ocgen.UpdateComponentJSONRequestBody(comp))
		if err != nil {
			return fmt.Errorf("failed to update component traits: %w", err)
		}
		if updResp.StatusCode() != http.StatusOK && updResp.StatusCode() != http.StatusCreated {
			return handleErrorResponse(updResp.StatusCode(), ErrorResponses{
				JSON400: updResp.JSON400,
				JSON401: updResp.JSON401,
				JSON403: updResp.JSON403,
				JSON404: updResp.JSON404,
				JSON500: updResp.JSON500,
			})
		}
		return nil
	})
}

// UpdateComponentTraitEnvironmentConfigs iterates the Component's
// ReleaseBindings and writes the supplied trait-instance keyed configs
// onto each one's `spec.traitEnvironmentConfigs`. Existing entries are
// preserved when not named in `configs` (the typical add-trait case
// shouldn't strip other traits' env configs). Pass a nil/empty value
// for an instance to clear that instance's config.
func (c *componentClient) UpdateComponentTraitEnvironmentConfigs(ctx context.Context, orgName, projectName, componentName string, configs map[string]map[string]interface{}) error {
	scopedComp := ScopedComponentName(projectName, componentName)
	componentQ := ocgen.ComponentQueryParam(scopedComp)
	listResp, err := c.oc.ListReleaseBindingsWithResponse(ctx, orgName, &ocgen.ListReleaseBindingsParams{
		Component: &componentQ,
	})
	if err != nil {
		return fmt.Errorf("failed to list release bindings for trait env config update: %w", err)
	}
	if listResp.StatusCode() != http.StatusOK || listResp.JSON200 == nil {
		return handleErrorResponse(listResp.StatusCode(), ErrorResponses{
			JSON401: listResp.JSON401,
			JSON403: listResp.JSON403,
			JSON500: listResp.JSON500,
		})
	}

	rbs := listResp.JSON200.Items
	if len(rbs) == 0 {
		// First build hasn't produced a ReleaseBinding yet — soft no-op.
		// The trait_sync watcher will retry once the deploy chain catches up.
		return nil
	}

	// The list supplies the RB NAMES only. Each write re-GETs its binding
	// inside the retried closure, because OC's Component controller rewrites
	// `spec.releaseName` on the very bindings we are patching — a trait change
	// on the Component produces a new ComponentRelease, and that rewrite lands
	// in the window between a list and a PUT. See stale_write.go.
	for _, rb := range rbs {
		name := rb.Metadata.Name
		if err := retryStaleWrite(ctx, "releasebinding/"+name+" spec.traitEnvironmentConfigs", func(ctx context.Context) error {
			return c.putTraitEnvironmentConfigs(ctx, orgName, name, configs)
		}); err != nil {
			return err
		}
	}
	return nil
}

// putTraitEnvironmentConfigs is one read-modify-write attempt against a single
// ReleaseBinding: re-read it, merge the desired trait-instance configs into
// `spec.traitEnvironmentConfigs`, write it back. Instances absent from
// `configs` are preserved; an instance mapped to an empty value is deleted.
func (c *componentClient) putTraitEnvironmentConfigs(ctx context.Context, orgName, bindingName string, configs map[string]map[string]interface{}) error {
	getResp, err := c.oc.GetReleaseBindingWithResponse(ctx, orgName, ocgen.ReleaseBindingNameParam(bindingName))
	if err != nil {
		return fmt.Errorf("failed to get release binding %s for trait env config update: %w", bindingName, err)
	}
	if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
		return handleErrorResponse(getResp.StatusCode(), ErrorResponses{
			JSON401: getResp.JSON401,
			JSON403: getResp.JSON403,
			JSON404: getResp.JSON404,
			JSON500: getResp.JSON500,
		})
	}
	rb := *getResp.JSON200
	if rb.Spec == nil {
		rb.Spec = &ocgen.ReleaseBindingSpec{}
	}
	// Merge: preserve any pre-existing instance keys we don't touch.
	merged := map[string]interface{}{}
	if rb.Spec.TraitEnvironmentConfigs != nil {
		merged = *rb.Spec.TraitEnvironmentConfigs
	}
	for inst, params := range configs {
		if len(params) == 0 {
			delete(merged, inst)
			continue
		}
		merged[inst] = cloneParameterMap(params)
	}
	rb.Spec.TraitEnvironmentConfigs = &merged

	updResp, uerr := c.oc.UpdateReleaseBindingWithResponse(ctx, orgName, ocgen.ReleaseBindingNameParam(bindingName), ocgen.UpdateReleaseBindingJSONRequestBody(rb))
	if uerr != nil {
		return fmt.Errorf("failed to update release binding %s trait env config: %w", bindingName, uerr)
	}
	if updResp.StatusCode() != http.StatusOK && updResp.StatusCode() != http.StatusCreated {
		return handleErrorResponse(updResp.StatusCode(), ErrorResponses{
			JSON400: updResp.JSON400,
			JSON401: updResp.JSON401,
			JSON403: updResp.JSON403,
			JSON404: updResp.JSON404,
			JSON500: updResp.JSON500,
		})
	}
	return nil
}

// buildCreateComponentBody assembles the CreateComponent request body.
// gen's ComponentSpec.{ComponentType,Owner} are inline anonymous structs;
// we materialize them with composite literals to stay clear of pointer
// gymnastics.
func buildCreateComponentBody(projectName string, req *CreateComponentRequest) ocgen.CreateComponentJSONRequestBody {
	ann := map[string]string{
		AnnotationKeyDisplayName: req.DisplayName,
		AnnotationKeyDescription: req.Description,
	}
	// Reference the per-org NAMESPACED ComponentType (not the cluster-scoped
	// ClusterComponentType). Why: in dev cloud the `deployment/service`
	// ClusterComponentType is the OpenChoreo built-in, whose render template has
	// NO `registry-pull-secret` / `imagePullSecrets` — so the deployed workload
	// can't pull its image from the per-org ECR repo and the Release sits at
	// ResourcesProgressing (ImagePullBackOff) forever. The platform provisions a
	// per-org namespaced ComponentType (named `service`/`web-application`, via
	// platform-api ProvisionOrgUnit) that DOES carry the registry-pull-secret +
	// imagePullSecrets AND still allows the dockerfile-builder ClusterWorkflow.
	// Devant and agent-manager both reference the namespaced `ComponentType`
	// (kind=ComponentType) for the same reason; aep was the outlier.
	//
	// Verified local + dev cloud: in cloud, platform-api's ProvisionOrgUnit
	// creates the per-org namespaced `service`/`web-application` ComponentTypes;
	// locally, deployments/scripts/setup-aep.sh provisions the same namespaced
	// types in the org ns (derived from the cluster-scoped definitions). So the
	// kind=ComponentType reference resolves in both environments — no env branch.
	// The type NAME (`deployment/service` etc.) is identical for both kinds.
	ctKind := ocgen.ComponentSpecComponentTypeKindComponentType
	body := ocgen.Component{
		Metadata: ocgen.ObjectMeta{
			Name:        ScopedComponentName(projectName, req.Name),
			Annotations: &ann,
		},
		Spec: &ocgen.ComponentSpec{
			AutoBuild:  &req.AutoBuild,
			AutoDeploy: &req.AutoDeploy,
			Owner: struct {
				ProjectName string `json:"projectName"`
			}{ProjectName: projectName},
			ComponentType: struct {
				Kind *ocgen.ComponentSpecComponentTypeKind `json:"kind,omitempty"`
				Name string                                `json:"name"`
			}{
				Kind: &ctKind,
				Name: req.Type,
			},
		},
	}

	if req.Workflow != nil {
		wfKind := ocgen.ComponentWorkflowConfigKindClusterWorkflow
		if req.Workflow.Kind != "" {
			k := ocgen.ComponentWorkflowConfigKind(req.Workflow.Kind)
			wfKind = k
		}
		wf := &ocgen.ComponentWorkflowConfig{
			Kind: &wfKind,
			Name: req.Workflow.Name,
		}
		if params := workflowParametersToMap(req.Workflow.Parameters); params != nil {
			wf.Parameters = &params
		}
		body.Spec.Workflow = wf
	}
	if traits := componentTraitsToGen(req.Traits); traits != nil {
		body.Spec.Traits = traits
	}
	return body
}

// componentTraitsToGen converts the BFF-internal slice into the gen shape.
// Returns nil for an empty input so we don't stamp an empty traits array
// onto Components without API security configured.
func componentTraitsToGen(traits []ComponentTrait) *[]ocgen.ComponentTrait {
	if len(traits) == 0 {
		return nil
	}
	out := make([]ocgen.ComponentTrait, 0, len(traits))
	for _, t := range traits {
		entry := ocgen.ComponentTrait{
			InstanceName: t.InstanceName,
			Name:         t.Name,
		}
		if t.Kind != "" {
			k := ocgen.ComponentTraitKind(t.Kind)
			entry.Kind = &k
		}
		if len(t.Parameters) > 0 {
			p := cloneParameterMap(t.Parameters)
			entry.Parameters = &p
		}
		out = append(out, entry)
	}
	return &out
}

// workflowParametersToMap shapes our typed CreateComponentRequest.Workflow.Parameters
// into the dynamic `map[string]interface{}` gen uses. Returns nil when no
// fields are set so we don't leak an empty `parameters` object.
func workflowParametersToMap(p *ComponentWorkflowParameters) map[string]interface{} {
	if p == nil {
		return nil
	}
	out := map[string]interface{}{}
	if p.Repository != nil {
		repo := map[string]interface{}{}
		if p.Repository.URL != "" {
			repo["url"] = p.Repository.URL
		}
		if p.Repository.SecretRef != "" {
			repo["secretRef"] = p.Repository.SecretRef
		}
		if p.Repository.AppPath != "" {
			repo["appPath"] = p.Repository.AppPath
		}
		if p.Repository.Revision != nil {
			rev := map[string]interface{}{}
			if p.Repository.Revision.Branch != "" {
				rev["branch"] = p.Repository.Revision.Branch
			}
			if p.Repository.Revision.Commit != "" {
				rev["commit"] = p.Repository.Revision.Commit
			}
			if len(rev) > 0 {
				repo["revision"] = rev
			}
		}
		if len(repo) > 0 {
			out["repository"] = repo
		}
	}
	if p.Docker != nil {
		docker := map[string]interface{}{}
		if p.Docker.Context != "" {
			docker["context"] = p.Docker.Context
		}
		if p.Docker.FilePath != "" {
			docker["filePath"] = p.Docker.FilePath
		}
		if len(docker) > 0 {
			out["docker"] = docker
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// workflowEnvVarRefsToGen converts the BFF-internal env-var model into
// the ocgen.EnvVar slice that goes onto a ReleaseBinding's
// `spec.workloadOverrides.container.env`. An empty `envVars` returns a
// pointer to an empty slice so the server-side patch clears the field
// rather than leaving stale values in place.
func workflowEnvVarRefsToGen(envVars []WorkflowEnvVarRef) *[]ocgen.EnvVar {
	out := make([]ocgen.EnvVar, 0, len(envVars))
	for _, ev := range envVars {
		entry := ocgen.EnvVar{Key: ev.Key}
		if ev.ValueFrom != nil && ev.ValueFrom.SecretKeyRef != nil {
			name := ev.ValueFrom.SecretKeyRef.Name
			key := ev.ValueFrom.SecretKeyRef.Key
			entry.ValueFrom = &ocgen.EnvVarValueFrom{
				SecretKeyRef: &struct {
					Key  *string `json:"key,omitempty"`
					Name *string `json:"name,omitempty"`
				}{
					Key:  &key,
					Name: &name,
				},
			}
		} else {
			v := ev.Value
			entry.Value = &v
		}
		out = append(out, entry)
	}
	return &out
}

// -- Deployments (read-only) -------------------------------------------------

func (c *componentClient) ListDeployments(ctx context.Context, orgName, projectName, componentName string) (*gen.DeploymentList, error) {
	scopedComp := ScopedComponentName(projectName, componentName)
	componentQ := ocgen.ComponentQueryParam(scopedComp)
	resp, err := c.oc.ListReleaseBindingsWithResponse(ctx, orgName, &ocgen.ListReleaseBindingsParams{
		Component: &componentQ,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list release bindings: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON500: resp.JSON500,
		})
	}

	items := make([]gen.Deployment, len(resp.JSON200.Items))
	for i, rb := range resp.JSON200.Items {
		items[i] = deploymentFromReleaseBinding(rb)
	}
	return &gen.DeploymentList{Items: items}, nil
}

// ListProjectReleaseBindings lists the org's ReleaseBindings and keeps the
// project's, following pagination — the status poll's single OC call.
func (c *componentClient) ListProjectReleaseBindings(ctx context.Context, orgName, projectName string) ([]ReleaseBindingSummary, error) {
	var out []ReleaseBindingSummary
	params := &ocgen.ListReleaseBindingsParams{}
	for {
		resp, err := c.oc.ListReleaseBindingsWithResponse(ctx, orgName, params)
		if err != nil {
			return nil, fmt.Errorf("failed to list release bindings: %w", err)
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
				JSON401: resp.JSON401,
				JSON403: resp.JSON403,
				JSON500: resp.JSON500,
			})
		}
		for _, rb := range resp.JSON200.Items {
			if rb.Spec == nil || rb.Spec.Owner.ProjectName != projectName {
				continue
			}
			out = append(out, releaseBindingSummary(rb))
		}
		next := resp.JSON200.Pagination.NextCursor
		if next == nil || *next == "" {
			return out, nil
		}
		// A non-advancing cursor would spin this poll-path loop forever —
		// fail instead (the console keeps last-good data on error).
		if params.Cursor != nil && *next == string(*params.Cursor) {
			return nil, fmt.Errorf("list release bindings: pagination did not advance (cursor %q)", *next)
		}
		cur := ocgen.CursorParam(*next)
		params.Cursor = &cur
	}
}

// releaseBindingSummary extracts the deploy-stage facts: identity, undeploy
// intent, and the aggregate Ready-typed condition (never the last-array-entry
// heuristic — condition array order is not guaranteed).
func releaseBindingSummary(rb ocgen.ReleaseBinding) ReleaseBindingSummary {
	s := ReleaseBindingSummary{}
	var projectName, componentName string
	if rb.Spec != nil {
		projectName = rb.Spec.Owner.ProjectName
		componentName = rb.Spec.Owner.ComponentName
		s.Environment = rb.Spec.Environment
		s.Undeploy = rb.Spec.State != nil && *rb.Spec.State == ocgen.ReleaseBindingSpecStateUndeploy
	}
	s.ComponentName = FriendlyComponentName(componentName, projectName)
	if rb.Status != nil && rb.Status.Conditions != nil {
		for _, cond := range *rb.Status.Conditions {
			if cond.Type == "Ready" {
				s.ReadyStatus = string(cond.Status)
				s.ReadyReason = cond.Reason
				break
			}
		}
	}
	return s
}

// -- WorkflowRuns (builds + coding-agent) ------------------------------------

func (c *componentClient) TriggerBuild(ctx context.Context, orgName, projectName, componentName, secretRef, runName string) (*gen.WorkflowRun, error) {
	return c.triggerBuildInner(ctx, orgName, projectName, componentName, "", secretRef, runName)
}

func (c *componentClient) TriggerBuildAtCommit(ctx context.Context, orgName, projectName, componentName, commitSHA, secretRef, runName string) (*gen.WorkflowRun, error) {
	return c.triggerBuildInner(ctx, orgName, projectName, componentName, commitSHA, secretRef, runName)
}

// triggerBuildInner fetches the Component to grab its declared Workflow
// (kind+name+parameters) and POSTs a fresh WorkflowRun. When commitSHA is
// non-empty it's stamped onto `parameters.repository.revision.commit` so the
// build pod clones the exact merge SHA — webhook-driven builds set this
// from `pull_request.closed`'s merge_commit_sha.
//
// When runName is empty the BFF gets a fresh NewBuildRunName-shaped name —
// retained for tests / call sites that don't need to pre-stage anything.
// Production callers (dispatch path, console "Build" button) pass runName
// because they staged the per-WorkflowRun build Secret with that name
// upfront.
func (c *componentClient) triggerBuildInner(ctx context.Context, orgName, projectName, componentName, commitSHA, secretRef, runName string) (*gen.WorkflowRun, error) {
	scopedComp := ScopedComponentName(projectName, componentName)

	compResp, err := c.oc.GetComponentWithResponse(ctx, orgName, scopedComp)
	if err != nil {
		return nil, fmt.Errorf("failed to get component for build trigger: %w", err)
	}
	if compResp.StatusCode() != http.StatusOK || compResp.JSON200 == nil {
		return nil, handleErrorResponse(compResp.StatusCode(), ErrorResponses{
			JSON401: compResp.JSON401,
			JSON403: compResp.JSON403,
			JSON404: compResp.JSON404,
			JSON500: compResp.JSON500,
		})
	}

	wf := buildWorkflowFromComponent(compResp.JSON200, commitSHA, secretRef)
	if wf.Name == "" {
		return nil, fmt.Errorf("trigger build: component %s/%s has no workflow configured", projectName, componentName)
	}

	if runName == "" {
		runName = NewBuildRunName(projectName, componentName)
	}
	labels := map[string]string{
		string(LabelKeyComponent): scopedComp,
		string(LabelKeyProject):   projectName,
	}
	body := ocgen.CreateWorkflowRunJSONRequestBody{
		Metadata: ocgen.ObjectMeta{
			Name:   runName,
			Labels: &labels,
		},
		Spec: &ocgen.WorkflowRunSpec{Workflow: wf},
	}

	return c.createWorkflowRun(ctx, orgName, body, "trigger build")
}

// buildWorkflowFromComponent lifts the Component's declared Workflow into a
// WorkflowRunConfig, optionally injecting commitSHA on the parameters map.
// Parameters is shaped as `map[string]interface{}` end-to-end on the gen
// side; we deep-copy the slice keys we touch to avoid mutating shared maps
// returned by the cache layer (which would race with concurrent triggers).
func buildWorkflowFromComponent(comp *ocgen.Component, commitSHA, secretRef string) ocgen.WorkflowRunConfig {
	if comp == nil || comp.Spec == nil || comp.Spec.Workflow == nil {
		return ocgen.WorkflowRunConfig{}
	}
	src := comp.Spec.Workflow
	runKind := ocgen.WorkflowRunConfigKindClusterWorkflow
	if src.Kind != nil {
		runKind = ocgen.WorkflowRunConfigKind(*src.Kind)
	}

	out := ocgen.WorkflowRunConfig{
		Kind: &runKind,
		Name: src.Name,
	}
	if src.Parameters == nil {
		return out
	}

	params := cloneParameterMap(*src.Parameters)
	if commitSHA != "" {
		injectCommitSHA(params, commitSHA)
	}
	// Set repository.secretRef explicitly at trigger time. When non-empty
	// (the BFF provisioned the per-org GitSecret), the dockerfile-builder
	// workflow synthesises the git ExternalSecret from that SecretReference;
	// when empty it clones unauthenticated (public repos). Setting it here —
	// rather than trusting whatever the Component stored — keeps the build's
	// credential a property of the dispatch (which provisioned the secret),
	// and overwrites any stale value left by an earlier flow.
	setRepoSecretRef(params, secretRef)
	out.Parameters = &params
	return out
}

// setRepoSecretRef sets params["repository"]["secretRef"] = secretRef.
// No-op for components that never had a repository block.
func setRepoSecretRef(params map[string]interface{}, secretRef string) {
	repo, ok := params["repository"].(map[string]interface{})
	if !ok {
		return
	}
	repo["secretRef"] = secretRef
}

// injectCommitSHA stamps params["repository"]["revision"]["commit"] = sha,
// materialising the nested maps if missing. The branch stays untouched so
// OC's clone path keeps working.
func injectCommitSHA(params map[string]interface{}, sha string) {
	repo, ok := params["repository"].(map[string]interface{})
	if !ok {
		repo = map[string]interface{}{}
		params["repository"] = repo
	}
	rev, ok := repo["revision"].(map[string]interface{})
	if !ok {
		rev = map[string]interface{}{}
		repo["revision"] = rev
	}
	rev["commit"] = sha
}

// cloneParameterMap deep-clones a map[string]interface{} value tree. Only
// handles the shapes we put into Workflow.Parameters (nested maps + scalars
// + string slices); other shapes pass through by reference.
func cloneParameterMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		switch tv := v.(type) {
		case map[string]interface{}:
			out[k] = cloneParameterMap(tv)
		case []string:
			cp := append([]string(nil), tv...)
			out[k] = cp
		default:
			out[k] = v
		}
	}
	return out
}

// TriggerCodingAgent creates a WorkflowRun of ClusterWorkflow
// `aep-coding-agent`. Each call creates a fresh run; idempotency is
// the caller's responsibility (see DispatchService.dispatchOne which gates
// on task.LastCodingAgentRunName + DispatchedAt).
//
// NOTE: deliberately NOT setting `openchoreo.dev/component` /
// `openchoreo.dev/project` labels. OC validates the
// ClusterWorkflow ↔ ClusterComponentType allowed-workflow pair when a
// WorkflowRun carries the `openchoreo.dev/component` label, which would
// reject `aep-coding-agent` because the user's component is
// `deployment/service` (allowed only the builder ClusterWorkflows). The
// agent pod has no need to be tied to the user's Component for OC's
// purposes — the project + component identifiers flow in via the
// `parameters.task.*` fields that the runner reads. The `aep.*`
// label catalog carries them for the BFF watcher instead.
func (c *componentClient) TriggerCodingAgent(ctx context.Context, params CodingAgentParams) (*gen.WorkflowRun, error) {
	scopedComp := ScopedComponentName(params.ProjectName, params.ComponentName)

	// Run name shape: coding-agent-<short-task>-<unixMs>. K8s names must be
	// ≤63 chars and start with a letter. Truncate the taskID to 8 to stay
	// safely inside the budget. The unixMs suffix makes re-dispatch unique.
	shortTask := params.TaskID
	if len(shortTask) > 8 {
		shortTask = shortTask[:8]
	}
	runName := fmt.Sprintf("coding-agent-%s-%d", shortTask, time.Now().UnixMilli())

	labels := map[string]string{
		string(LabelKeyAepCodingAgentTask): params.TaskID,
		string(LabelKeyAepProject):         params.ProjectName,
		string(LabelKeyAepComponent):       scopedComp,
	}

	wfKind := ocgen.WorkflowRunConfigKindClusterWorkflow
	parameters := codingAgentParameters(params)
	body := ocgen.CreateWorkflowRunJSONRequestBody{
		Metadata: ocgen.ObjectMeta{
			Name:   runName,
			Labels: &labels,
		},
		Spec: &ocgen.WorkflowRunSpec{
			Workflow: ocgen.WorkflowRunConfig{
				Kind:       &wfKind,
				Name:       "aep-coding-agent",
				Parameters: &parameters,
			},
		},
	}

	return c.createWorkflowRun(ctx, params.OrgName, body, "trigger coding-agent")
}

// codingAgentParameters builds the `parameters.*` map that the
// aep-coding-agent ClusterWorkflow's openAPIV3Schema expects. The
// runner image reads AEP_* env vars substituted from these keys.
func codingAgentParameters(p CodingAgentParams) map[string]interface{} {
	return map[string]interface{}{
		"task": map[string]interface{}{
			"id":            p.TaskID,
			"orgId":         p.OrgName,
			"projectId":     p.ProjectName,
			"componentName": p.ComponentName,
			"prompt":        p.Prompt,
		},
		"repository": map[string]interface{}{
			"url":       p.RepoURL,
			"identity": map[string]interface{}{
				"name":  p.IdentityName,
				"email": p.IdentityEmail,
				"login": p.IdentityLogin,
			},
		},
		"bff": map[string]interface{}{
			"bearer":      p.Bearer,
			"platformUrl": p.PlatformURL,
		},
		"gitService": map[string]interface{}{
			"url": p.GitServiceURL,
		},
		"anthropic": map[string]interface{}{
			"secretRef": p.AnthropicSecretRef,
		},
	}
}

// createWorkflowRun is the shared POST path for both trigger flows. opName
// goes into the network-error wrap to keep slog logs distinguishable
// (trigger build / trigger coding-agent).
func (c *componentClient) createWorkflowRun(ctx context.Context, orgName string, body ocgen.CreateWorkflowRunJSONRequestBody, opName string) (*gen.WorkflowRun, error) {
	// Refuse a name OpenChoreo would accept and then never build. This is the
	// one choke point every WorkflowRun create passes through, and the check is
	// here rather than in the name generators because it has to hold for names
	// they do not own — a caller-supplied runName included. The failure it
	// replaces is the worst kind: a 201 Created, then no build pod, then a run
	// stuck at WorkflowPending forever with nothing on its status explaining it.
	if n := body.Metadata.Name; len(n) > k8sname.MaxLabelValueLen {
		return nil, fmt.Errorf(
			"%s: WorkflowRun name %q is %d chars, over the %d-char Kubernetes label-value limit "+
				"(OpenChoreo and Argo both copy it into a label, so this run would be accepted and then never render)",
			opName, n, len(n), k8sname.MaxLabelValueLen)
	}

	resp, err := c.oc.CreateWorkflowRunWithResponse(ctx, orgName, body)
	if err != nil {
		return nil, fmt.Errorf("failed to %s: %w", opName, err)
	}
	if resp.StatusCode() != http.StatusCreated && resp.StatusCode() != http.StatusOK {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	if resp.JSON201 == nil {
		return nil, fmt.Errorf("%s: empty WorkflowRun in response", opName)
	}
	run := workflowRunToModel(*resp.JSON201)
	return &run, nil
}

func (c *componentClient) ListWorkflowRuns(ctx context.Context, orgName, projectName, componentName string, limit int, cursor string) (*gen.WorkflowRunList, error) {
	scopedComp := ScopedComponentName(projectName, componentName)
	return c.listWorkflowRunsBySelector(ctx, orgName,
		fmt.Sprintf("%s=%s", string(LabelKeyComponent), scopedComp), limit, cursor)
}

func (c *componentClient) ListProjectWorkflowRuns(ctx context.Context, orgName, projectName string, limit int, cursor string) (*gen.WorkflowRunList, error) {
	return c.listWorkflowRunsBySelector(ctx, orgName,
		fmt.Sprintf("%s=%s", string(LabelKeyProject), projectName), limit, cursor)
}

// listWorkflowRunsBySelector is the shared body of the two listings above; only
// the label selector differs, so the response handling lives once.
func (c *componentClient) listWorkflowRunsBySelector(ctx context.Context, orgName, selector string, limit int, cursor string) (*gen.WorkflowRunList, error) {
	sel := ocgen.LabelSelectorParam(selector)
	params := &ocgen.ListWorkflowRunsParams{LabelSelector: &sel}
	if limit > 0 {
		l := ocgen.LimitParam(limit)
		params.Limit = &l
	}
	if cursor != "" {
		cur := ocgen.CursorParam(cursor)
		params.Cursor = &cur
	}

	resp, err := c.oc.ListWorkflowRunsWithResponse(ctx, orgName, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list workflow runs: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON500: resp.JSON500,
		})
	}

	items := make([]gen.WorkflowRun, len(resp.JSON200.Items))
	for i, run := range resp.JSON200.Items {
		items[i] = workflowRunToModel(run)
	}
	return &gen.WorkflowRunList{Items: items}, nil
}

func (c *componentClient) GetWorkflowRun(ctx context.Context, orgName, runName string) (*gen.WorkflowRun, error) {
	resp, err := c.oc.GetWorkflowRunWithResponse(ctx, orgName, runName)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow run: %w", err)
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON404: resp.JSON404,
			JSON500: resp.JSON500,
		})
	}
	run := workflowRunToModel(*resp.JSON200)
	return &run, nil
}
