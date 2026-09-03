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
// Deploy chain: the BFF owns it, for every component. AutoDeploy is false
// everywhere, so no controller promotes a release on its own — the platform
// decides when a build becomes a running deployment, which is what lets the run
// supervisor order validation AFTER the version is actually serving.
//
// The two halves differ only in who posts the Workload:
//
//   - USER components: the build's last step posts the Workload; the deploy
//     stage then calls EnsureRelease + ApplyReleaseBinding at the merge SHA.
//   - Ephemeral platform components (coding-agent): no build exists, so the
//     dispatcher drives EnsureWorkload as well, then EnsureRelease +
//     EnsureReleaseBinding.
type ComponentClient interface {
	ListComponents(ctx context.Context, orgName, projectName string, limit int, cursor string) (*gen.ComponentList, error)
	GetComponent(ctx context.Context, orgName, projectName, componentName string) (*gen.Component, error)
	CreateComponent(ctx context.Context, orgName, projectName string, req *CreateComponentRequest) (*gen.Component, error)

	// EnsureComponentType converges a namespaced ComponentType onto body.
	// Idempotent on (orgName, metadata.name): HTTP 409 re-reads the existing
	// type and PUTs body only if the stored spec has drifted from it, so an org
	// seeded by an older platform build stops validating today's dispatches
	// against yesterday's schema. body is the raw CR map (e.g.
	// CodingAgentComponentType()) posted via the gen client's WithBody path —
	// no typed converter.
	EnsureComponentType(ctx context.Context, orgName string, body map[string]any) error

	// ListInternalComponents returns the project's aep-internal coding-agent
	// Components — the ONLY read that can see what ListComponents filters out.
	// Drives the retention reaper.
	ListInternalComponents(ctx context.Context, orgName, projectName string) ([]InternalComponent, error)

	// The explicit deploy chain. EnsureWorkload is the ephemeral half — an
	// agent cycle has no build to post a Workload, so the dispatcher posts it —
	// while a user component's Workload arrives from its build. Every one
	// treats 409 as success so a crashed dispatch resumes.
	EnsureWorkload(ctx context.Context, orgName, projectName string, in WorkloadInput) error
	EnsureRelease(ctx context.Context, orgName, projectName, componentName, releaseName string) (releaseNameOut string, err error)
	EnsureReleaseBinding(ctx context.Context, orgName, projectName, componentName, environment, releaseName string) error

	// ApplyReleaseBinding converges a USER component's binding onto the desired
	// state — the pin plus every field the platform owns on it, in one write.
	// Distinct from EnsureReleaseBinding above, which is create-only because an
	// ephemeral component's binding is never re-pinned; this one re-pins on
	// every cycle, which is what a deploy IS.
	ApplyReleaseBinding(ctx context.Context, orgName, projectName string, in ReleaseBindingDesired) error

	// GetReleaseBindingStatus reads one binding's aggregate Ready condition, or
	// (nil, nil) when it does not exist yet. The deploy stage's readiness poll:
	// the component-scoped read, where ListProjectReleaseBindings is the
	// project-status page's.
	GetReleaseBindingStatus(ctx context.Context, orgName, projectName, componentName, environment string) (*ReleaseBindingSummary, error)

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

	// ApplyComponentSpec re-asserts the platform-owned half of an existing
	// Component's spec — its traits and its build/deploy policy — in one
	// GET-then-PUT. Returns ErrComponentNotFound when the Component does not
	// exist (the caller decides whether to recreate or no-op).
	//
	// Called on EVERY component ensure, not only when something changed: the
	// trait shape is frozen into the next ComponentRelease, so a design edit
	// that has not reached the CR before the build is an edit the release
	// silently drops.
	ApplyComponentSpec(ctx context.Context, orgName, projectName, componentName string, desired ComponentSpecDesired) error

	// UpdateComponentTraitEnvironmentConfigs writes per-environment trait
	// configs onto each of the component's ReleaseBindings at
	// `spec.traitEnvironmentConfigs`. Configs is keyed by trait instance
	// name; the value is the parameters block (e.g. `{"jwtAuth": {"enabled": true}}`).
	// Passing an empty map clears the field. When no RBs exist yet (pre-
	// first-deploy) the call is a soft no-op.
	//
	// Superseded for user components by ApplyReleaseBinding, which writes the
	// trait configs in the SAME object write as the release pin — that is what
	// closes the window where a binding was renderable with a trait attached and
	// its config missing. Nothing retries this call any more: the trait-sync
	// watcher it used to lean on is gone, and the deploy stage composes the whole
	// binding at once (ADR-0017).
	UpdateComponentTraitEnvironmentConfigs(ctx context.Context, orgName, projectName, componentName string, configs map[string]map[string]interface{}) error

	// Deploy (read-only). The platform drives the chain itself — components carry
	// autoDeploy: false and the run supervisor performs the promote (ADR-0017) —
	// so this reads back what OpenChoreo resolved, including the external URLs the
	// deploy order depends on.
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
	ListWorkflowRuns(ctx context.Context, orgName, projectName, componentName string, limit int, cursor string) (*gen.WorkflowRunList, error)
	// ListBuildRuns is the same read reduced to the facts the milestone loop
	// decides on: the run's name, whether it is terminal, whether it succeeded,
	// and the COMMIT it built.
	//
	// It exists beside ListWorkflowRuns because the two answer different
	// audiences. That one returns the served contract model, which the console
	// renders; this one returns the commit, which the contract does not carry and
	// which both halves of the loop need — the event plane to count a commit's
	// attempts, the supervisor to learn which release a component's newest green
	// build should be serving. Mapping OpenChoreo's condition vocabulary once,
	// here, is what keeps two adapters from spelling "succeeded" differently.
	ListBuildRuns(ctx context.Context, orgName, projectName, componentName string) ([]BuildRunSummary, error)
	// ListProjectWorkflowRuns is the same read widened to every component in
	// the project — one call instead of one per component. The run read uses it
	// to derive a cycle's builds from its merge SHA without first having to
	// learn which components the merge touched.
	ListProjectWorkflowRuns(ctx context.Context, orgName, projectName string, limit int, cursor string) (*gen.WorkflowRunList, error)
	GetWorkflowRun(ctx context.Context, orgName, runName string) (*gen.WorkflowRun, error)
}

type componentClient struct {
	oc *ocgen.ClientWithResponses
	// preferPlainHTTP picks the http external URL when a binding advertises both
	// (Config.PreferPlainHTTPEndpoints explains why).
	preferPlainHTTP bool
}

func NewComponentClient(cfg Config) ComponentClient {
	oc, err := newGenClient(cfg)
	if err != nil {
		panic(fmt.Errorf("init openchoreo component client: %w", err))
	}
	return &componentClient{oc: oc, preferPlainHTTP: cfg.PreferPlainHTTPEndpoints}
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

// deploymentFromReleaseBinding pulls the first public URL from the binding's
// resolved endpoints. Cloud gateways populate `externalURLs.https` only;
// local/HTTP listeners populate `http`. Prefer https when both exist.
func deploymentFromReleaseBinding(rb ocgen.ReleaseBinding, preferPlainHTTP bool) gen.Deployment {
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
			if u := publicEndpointURL(ep.ExternalURLs, preferPlainHTTP); u != "" {
				endpointURL = u
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

// publicEndpointURL prefers the HTTPS gateway URL when OpenChoreo resolved
// one, else HTTP. Empty when the binding has no external URL at all.
func publicEndpointURL(urls *ocgen.EndpointGatewayURLs, preferPlainHTTP bool) string {
	if urls == nil {
		return ""
	}
	// Whichever is preferred FIRST, then the other as a fallback — a binding may
	// advertise only one, and answering "" when the unpreferred scheme is the only
	// one present would lose a URL that works.
	first, second := urls.Https, urls.Http
	if preferPlainHTTP {
		first, second = urls.Http, urls.Https
	}
	if first != nil {
		return formatEndpointURL(first)
	}
	if second != nil {
		return formatEndpointURL(second)
	}
	return ""
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

const annotationInternal = "aep.wso2.com/internal"

func isInternalComponent(annotations, labels map[string]string) bool {
	if annotations[annotationInternal] == "true" {
		return true
	}
	return labels[annotationInternal] == "true"
}

// internalMarkerLabels is the label set that marks an object as the platform's
// own ephemeral scaffolding rather than one of the user's components. One
// spelling for the pair, because the writer (EnsureReleaseBinding) and the
// reader (ListProjectReleaseBindings) have to agree on it or a marked object is
// counted as a deployment anyway.
func internalMarkerLabels() map[string]string {
	return map[string]string{string(LabelKeyAepInternal): LabelValueAepInternal}
}

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

	items := make([]gen.Component, 0, len(resp.JSON200.Items))
	for _, comp := range resp.JSON200.Items {
		var ann, lbls map[string]string
		if comp.Metadata.Annotations != nil {
			ann = *comp.Metadata.Annotations
		}
		if comp.Metadata.Labels != nil {
			lbls = *comp.Metadata.Labels
		}
		if isInternalComponent(ann, lbls) {
			continue
		}
		items = append(items, componentToModel(comp))
	}
	return &gen.ComponentList{Items: items}, nil
}

// internalComponentFrom lifts the reaper's view off the CR. The project is read
// from spec.owner (authoritative) and the identity from the marker labels.
func internalComponentFrom(c ocgen.Component) InternalComponent {
	var projectName, typeName string
	if c.Spec != nil {
		projectName = c.Spec.Owner.ProjectName
		typeName = c.Spec.ComponentType.Name
	}
	var created time.Time
	if c.Metadata.CreationTimestamp != nil {
		created = c.Metadata.CreationTimestamp.UTC()
	}
	return InternalComponent{
		Name:      FriendlyComponentName(c.Metadata.Name, projectName),
		TypeName:  typeName,
		CycleID:   label(c.Metadata.Labels, string(LabelKeyAepCycle)),
		RunName:   label(c.Metadata.Labels, string(LabelKeyAepRunName)),
		CreatedAt: created,
	}
}

// IsCodingAgentTypeName reports whether typeName is a coding-agent ComponentType
// reference. OC may surface either the bare name (`coding-agent`) or the
// workload-qualified form (`job/coding-agent`). Shared by the internal lister
// and retention so a surface that returns one form cannot be silently pruned
// by a check that only accepts the other.
func IsCodingAgentTypeName(typeName string) bool {
	return typeName == CodingAgentComponentTypeRef || typeName == CodingAgentComponentTypeName
}

// ListInternalComponents returns the project's aep-internal coding-agent
// Components, following pagination. It selects on the internal MARKER (not on
// the project label) and matches ownership client-side against
// spec.owner.projectName, for the reason ListProjectReleaseBindings does:
// ownership is the authoritative fact, and a label OC did or did not copy onto
// the CR is not.
//
// This is the deliberate counterpart to ListComponents' filter: one method hides
// internal components from users, the other is the only way the platform's own
// machinery can see them. Only coding-agent typed internals are returned — the
// retention reaper must not touch other future internal kinds.
func (c *componentClient) ListInternalComponents(ctx context.Context, orgName, projectName string) ([]InternalComponent, error) {
	sel := ocgen.LabelSelectorParam(string(LabelKeyAepInternal) + "=" + LabelValueAepInternal)
	params := &ocgen.ListComponentsParams{LabelSelector: &sel}
	var out []InternalComponent
	for {
		resp, err := c.oc.ListComponentsWithResponse(ctx, orgName, params)
		if err != nil {
			return nil, fmt.Errorf("failed to list internal components: %w", err)
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, handleErrorResponse(resp.StatusCode(), ErrorResponses{
				JSON400: resp.JSON400,
				JSON401: resp.JSON401,
				JSON403: resp.JSON403,
				JSON500: resp.JSON500,
			})
		}
		for _, comp := range resp.JSON200.Items {
			var ann, lbls map[string]string
			if comp.Metadata.Annotations != nil {
				ann = *comp.Metadata.Annotations
			}
			if comp.Metadata.Labels != nil {
				lbls = *comp.Metadata.Labels
			}
			if !isInternalComponent(ann, lbls) {
				continue
			}
			if comp.Spec == nil || comp.Spec.Owner.ProjectName != projectName {
				continue
			}
			if !IsCodingAgentTypeName(comp.Spec.ComponentType.Name) {
				continue
			}
			out = append(out, internalComponentFrom(comp))
		}
		next := resp.JSON200.Pagination.NextCursor
		if next == nil || *next == "" {
			return out, nil
		}
		// A non-advancing cursor would spin this loop forever — fail instead.
		if params.Cursor != nil && *next == string(*params.Cursor) {
			return nil, fmt.Errorf("list internal components: pagination did not advance (cursor %q)", *next)
		}
		cur := ocgen.CursorParam(*next)
		params.Cursor = &cur
	}
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
	// Refuse a coding-agent Component whose SCOPED name leaves OpenChoreo no
	// room for `-{env}-{hash8}` inside the Kubernetes label-value limit. Same
	// failure class as an overlong WorkflowRun name: OC accepts the parent CR,
	// then ResourceApplyFailed on the Job, then no runner pod — only the Job
	// path surfaces on the ReleaseBinding, which the console's progress dark
	// zone does not read. Catch it here, where the cause is still known.
	if req != nil && req.Type == CodingAgentComponentTypeRef {
		scoped := ScopedComponentName(projectName, req.Name)
		if len(scoped) > CodingAgentComponentNameBudget {
			return nil, fmt.Errorf(
				"create component: coding-agent name %q is %d chars after project scoping, over the %d-char budget "+
					"(OpenChoreo appends -%s-<hash8> into a pod label, so this Component would be accepted and then never schedule a runner)",
				scoped, len(scoped), CodingAgentComponentNameBudget, DevEnvironmentName)
		}
	}

	resp, err := c.oc.CreateComponentWithResponse(ctx, orgName, buildCreateComponentBody(projectName, req))
	if err != nil {
		return nil, fmt.Errorf("failed to create component: %w", err)
	}

	switch {
	case resp.StatusCode() == http.StatusPaymentRequired:
		// Org is at its agent-concurrency cap. Own sentinel so the dispatcher
		// can map this to blocked-not-failed (never a retryable create failure).
		return nil, fmt.Errorf("%w: create component %q: %s",
			ErrPaymentRequired, req.Name, strings.TrimSpace(string(resp.Body)))
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

// ApplyComponentSpec re-asserts traits + build/deploy policy on the named
// Component. GET-then-PUT to satisfy OC's full-object update semantics; an
// empty trait slice clears traits.
func (c *componentClient) ApplyComponentSpec(ctx context.Context, orgName, projectName, componentName string, desired ComponentSpecDesired) error {
	scopedComp := ScopedComponentName(projectName, componentName)
	// GET and PUT both inside the retried closure: a retry has to re-read, or
	// it replays the same stale resourceVersion. See stale_write.go.
	return retryStaleWrite(ctx, "component/"+scopedComp+" spec", func(ctx context.Context) error {
		getResp, err := c.oc.GetComponentWithResponse(ctx, orgName, scopedComp)
		if err != nil {
			return fmt.Errorf("failed to get component for spec update: %w", err)
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
		comp.Spec.Traits = componentTraitsToGen(desired.Traits)
		// autoDeploy is re-asserted on every pass, not only at create. A
		// component created before the platform owned deploy still carries
		// autoDeploy=true, and leaving it would have OC's controller promoting
		// releases underneath the deploy stage — two writers racing over one
		// binding's pin, with the loser's release silently serving.
		autoBuild, autoDeploy := desired.AutoBuild, desired.AutoDeploy
		comp.Spec.AutoBuild, comp.Spec.AutoDeploy = &autoBuild, &autoDeploy

		updResp, err := c.oc.UpdateComponentWithResponse(ctx, orgName, ocgen.ComponentNameParam(scopedComp), ocgen.UpdateComponentJSONRequestBody(comp))
		if err != nil {
			return fmt.Errorf("failed to update component spec: %w", err)
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
		// The deploy stage creates the binding complete; the converge sweep
		// re-asserts it afterwards.
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

	if len(req.Labels) > 0 {
		labels := make(map[string]string, len(req.Labels))
		for k, v := range req.Labels {
			labels[k] = v
		}
		body.Metadata.Labels = &labels
	}
	if len(req.Parameters) > 0 {
		params := cloneParameterMap(req.Parameters)
		body.Spec.Parameters = &params
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
		items[i] = deploymentFromReleaseBinding(rb, c.preferPlainHTTP)
	}
	return &gen.DeploymentList{Items: items}, nil
}

// ListProjectReleaseBindings lists the org's ReleaseBindings and keeps the
// project's USER-COMPONENT ones, following pagination — the status poll's single
// OC call.
//
// Internally marked bindings are skipped, the same exclusion ListComponents
// makes: every coding-agent cycle creates a real dev-environment binding owned
// by the user's project, and because that binding wraps a batch/v1 Job (which
// OpenChoreo registers no health check for) it reports Ready=True regardless of
// the Job's state. Folded into the deploy stage those bindings reported a
// project as "deployed" before anything had been deployed — and the project's
// first run could never report "none" at all, since one finished cycle is
// enough to leave a permanently-Ready binding behind.
//
// The predicate is client-side rather than a labelSelector on the request: this
// loop already filters by project here, so one more test is free, and the 5s
// status poll must not depend on openchoreo-api honouring a selector on
// releasebindings. Bindings created before the marker existed carry no label and
// are still returned; they age out with the retention pass.
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
			// The same predicate the Component read uses — it tests the marker,
			// not a kind, and reusing it is what keeps the two exclusions from
			// drifting onto different spellings of "internal".
			var lbls map[string]string
			if rb.Metadata.Labels != nil {
				lbls = *rb.Metadata.Labels
			}
			if isInternalComponent(nil, lbls) {
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
		s.ReleaseName = derefStr(rb.Spec.ReleaseName)
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

// createWorkflowRun is the shared POST path for WorkflowRun creates. opName
// goes into the network-error wrap to keep slog logs distinguishable
// (e.g. trigger build).
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

// ListBuildRuns lists a component's build WorkflowRuns with the commit each one
// built, FOLLOWING PAGINATION to the end.
//
// Every page, not the first, and that is a correctness requirement rather than
// thoroughness. The caller picks the newest SUCCEEDED run to decide which
// release a component should be serving, and this list is paged with Kubernetes
// continuation tokens: a page is a slice of the collection in the API server's
// own order, so a newer green run can sit on a later page. Reading one page
// would then name an older release as the desired one — and the deploy stage
// would faithfully promote the component BACKWARDS onto it.
//
// The cost is bounded by the page size rather than by the component's history
// being small: one round trip per buildRunPageSize runs, once per version-state
// read. The re-trigger budget's own read (eventcore) is a different question —
// it counts attempts at ONE commit, which are always among the newest runs — and
// it is free to keep taking the first page.
func (c *componentClient) ListBuildRuns(ctx context.Context, orgName, projectName, componentName string) ([]BuildRunSummary, error) {
	scopedComp := ScopedComponentName(projectName, componentName)
	sel := ocgen.LabelSelectorParam(fmt.Sprintf("%s=%s", string(LabelKeyComponent), scopedComp))
	limit := ocgen.LimitParam(buildRunPageSize)
	params := &ocgen.ListWorkflowRunsParams{LabelSelector: &sel, Limit: &limit}

	var out []BuildRunSummary
	for {
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
		for _, run := range resp.JSON200.Items {
			model := workflowRunToModel(run)
			out = append(out, BuildRunSummary{
				Name:      model.Name,
				Status:    model.Status,
				Completed: model.Completed,
				Succeeded: model.Status == ReasonWorkflowSucceeded,
				CommitSHA: buildRunCommit(run),
				StartedAt: derefTime(run.Metadata.CreationTimestamp),
			})
		}
		next := resp.JSON200.Pagination.NextCursor
		if next == nil || *next == "" {
			return out, nil
		}
		// A non-advancing cursor would spin this loop forever — fail instead,
		// exactly as the release-binding listing does.
		if params.Cursor != nil && *next == string(*params.Cursor) {
			return nil, fmt.Errorf("list workflow runs for %q: pagination did not advance (cursor %q)",
				componentName, *next)
		}
		cur := ocgen.CursorParam(*next)
		params.Cursor = &cur
	}
}

// buildRunPageSize is how many WorkflowRuns one page of the build listing asks
// for. Large enough that an ordinary component's whole history arrives in a
// single round trip, small enough that one response stays a sensible size.
const buildRunPageSize = 100

// buildRunCommit reads back the commit the trigger pinned the run to —
// `spec.workflow.parameters.repository.revision.commit`, the exact path
// injectCommitSHA writes.
//
// Read from the spec rather than parsed out of the run's NAME, which also
// embeds a short commit: that name is composed through k8sname.Bounded, which
// truncates a long readable head and appends a digest, so the commit is not
// recoverable from it for every project and component. A build triggered
// without a pinned commit (a manual build of the branch tip) answers "".
func buildRunCommit(run ocgen.WorkflowRun) string {
	if run.Spec == nil || run.Spec.Workflow.Parameters == nil {
		return ""
	}
	repo, ok := (*run.Spec.Workflow.Parameters)["repository"].(map[string]interface{})
	if !ok {
		return ""
	}
	rev, ok := repo["revision"].(map[string]interface{})
	if !ok {
		return ""
	}
	commit, _ := rev["commit"].(string)
	return commit
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
