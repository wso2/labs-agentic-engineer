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

package projects

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ComponentService handles business logic for component operations.
// ComponentName parameters are the user-friendly name; the OC client prefixes
// with projectName internally (see ScopedComponentName) because OC components
// share a single k8s namespace across all projects in an org.
//
// Deploy chain: every Component is created with AutoDeploy=true (see
// dispatch_service.ensureOCComponent), so OC drives Workload →
// ComponentRelease → ReleaseBinding from the build. The build workflow's
// generate-workload-cr step is the only writer of the Workload CR. The
// BFF reads ReleaseBindings via ListDeployments.
type ComponentService interface {
	ListComponents(ctx context.Context, orgName, projectName string, limit int, cursor string) (*gen.ComponentList, error)
	GetComponent(ctx context.Context, orgName, projectName, componentName string) (*gen.Component, error)
	CreateComponent(ctx context.Context, orgName, projectName string, req *openchoreo.CreateComponentRequest) (*gen.Component, error)
	// EnsureComponent idempotently provisions the OpenChoreo Component CR for a
	// design component (by friendly name), so a merged-PR build has a Component to
	// build. It is the coding-dispatch pre-flight (tasks-github-native): the CR
	// must exist by merge/build time or the build fails "Component not found".
	// Idempotent — CreateComponent is 409-safe, so re-dispatch is a no-op.
	EnsureComponent(ctx context.Context, orgName, projectName, componentName string) error

	// Deploy (read-only — autoDeploy on the Component drives the chain)
	ListDeployments(ctx context.Context, orgName, projectName, componentName string) (*gen.DeploymentList, error)

	// OpenAPI for the Test tab. Reads the spec from
	// `specs/design/components/<name>/openapi.yaml`. The Test tab's
	// swagger-ui invokes the deployed endpoint directly; CORS is enabled
	// on the service ClusterComponentType's HTTPRoute.
	GetComponentOpenAPI(ctx context.Context, orgName, projectName, componentName string) (*gen.ComponentOpenAPI, error)

	// Build (workflow runs)
	TriggerBuild(ctx context.Context, orgName, projectName, componentName string) (*gen.WorkflowRun, error)
	ListBuilds(ctx context.Context, orgName, projectName, componentName string, limit int, cursor string) (*gen.WorkflowRunList, error)
	// GetBuildLogs reads one build's log from `sinceMillis` onward (0 = from the
	// beginning). The response says whether it is COMPLETE — the build is
	// terminal and there will never be more — so a caller tailing a live build
	// knows when to stop asking.
	GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string, sinceMillis int64) (*gen.BuildLogs, error)
}

// BuildSecretStager pre-stages the org's build git Secret on the workflow
// plane and returns the secretRef the build WorkflowRun consumes. Consumer-
// side port over BuildCredentialsService; a thin composition-root adapter
// maps the concrete *StageResult to the secretRef string so the component
// feature need not import that services type.
type BuildSecretStager interface {
	StageBuildSecret(ctx context.Context, ocOrgID, repoSlug, workflowRunName string) (secretRef string, err error)
}

type componentService struct {
	client        openchoreo.ComponentClient
	observClient  observability.Client
	artifactStore *spec.ArtifactStore
	// repoSvc + buildCredSvc are used by TriggerBuild to pre-stage the
	// per-WorkflowRun build Secret. Optional — nil means "no staging"
	// (tests / unit-only flows).
	repoSvc      sourcecontrol.RepoService
	buildCredSvc BuildSecretStager
}

// NewComponentService builds the component service. repoSvc + buildCredSvc
// may be nil in tests / unit-only flows; production wiring passes both so
// TriggerBuild can pre-stage the per-WorkflowRun build Secret.
func NewComponentService(client openchoreo.ComponentClient, observClient observability.Client, artifactStore *spec.ArtifactStore, repoSvc sourcecontrol.RepoService, buildCredSvc BuildSecretStager) ComponentService {
	return &componentService{
		client:        client,
		observClient:  observClient,
		artifactStore: artifactStore,
		repoSvc:       repoSvc,
		buildCredSvc:  buildCredSvc,
	}
}

func (s *componentService) ListComponents(ctx context.Context, orgName, projectName string, limit int, cursor string) (*gen.ComponentList, error) {
	list, err := s.client.ListComponents(ctx, orgName, projectName, limit, cursor)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *componentService) GetComponent(ctx context.Context, orgName, projectName, componentName string) (*gen.Component, error) {
	comp, err := s.client.GetComponent(ctx, orgName, projectName, componentName)
	if err != nil {
		return nil, err
	}
	return comp, nil
}

func (s *componentService) CreateComponent(ctx context.Context, orgName, projectName string, req *openchoreo.CreateComponentRequest) (*gen.Component, error) {
	comp, err := s.client.CreateComponent(ctx, orgName, projectName, req)
	if err != nil {
		return nil, err
	}
	return comp, nil
}

// EnsureComponent provisions the OpenChoreo Component CR (one per design
// component) needed for the build to fire when the merge push arrives.
// AutoBuild=false (every build is driven by the BFF pinning a WorkflowRun to the
// merge SHA) and AutoDeploy=false (every deploy is driven by the run
// supervisor's deploy stage pinning a ReleaseBinding).
//
// It is an UPSERT, not a create-if-absent. Two things depend on that:
//
//   - The trait SHAPE is frozen into the ComponentRelease cut from the build's
//     Workload, so a design edit that toggles `exposesAPI.auth` has to reach the
//     CR BEFORE the build. Asserting only at create meant the first component
//     ever built carried the right traits and every later edit silently did not.
//   - A component created while the platform still relied on AutoDeploy carries
//     autoDeploy=true. Left alone, OC's controller would keep promoting releases
//     underneath the deploy stage.
//
// Reads the design facts (app path, component type, api-security) via the
// artifact store and the repo row via repoSvc; both are the existing feature
// ports.
func (s *componentService) EnsureComponent(ctx context.Context, orgName, projectName, componentName string) error {
	if s.artifactStore == nil {
		return fmt.Errorf("ensure component: artifact store not configured")
	}
	if s.repoSvc == nil {
		return fmt.Errorf("ensure component: repo service not configured")
	}
	comp, err := spec.ResolveDesignComponent(ctx, s.artifactStore, orgName, projectName, componentName)
	if err != nil {
		return fmt.Errorf("ensure component: resolve design component %q: %w", componentName, err)
	}
	repo, err := s.repoSvc.GetRepo(ctx, orgName, projectName)
	if err != nil || repo == nil {
		return fmt.Errorf("ensure component: resolve project repo for %s/%s: %w", orgName, projectName, err)
	}

	k8sName := k8sname.ToK8sName(comp.Name)
	// Dockerfile context/path: the component's app path (repo-relative), or the
	// repo root when unset. Mirrors the legacy pre-flight.
	dockerContext := comp.AppPath
	dockerFilePath := "Dockerfile"
	if dockerContext != "" {
		dockerFilePath = dockerContext + "/Dockerfile"
	} else {
		dockerContext = "."
	}
	branch := repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	// The trait SHAPE only — the per-environment config half of the same
	// projection lands on the ReleaseBinding at deploy, because it needs a
	// release to bind to. One function computes both so they cannot disagree.
	desiredSpec := openchoreo.ComponentSpecDesired{
		Traits:     DesiredDeploymentFor(DeploymentInputs{Component: *comp, ComponentName: k8sName}).Traits,
		AutoBuild:  false,
		AutoDeploy: false,
	}

	// repository.secretRef stays empty: build credentials are pre-staged per
	// WorkflowRun (build-credential-injection.md), so the Component's workflow
	// param carries no SecretReference.
	if _, err := s.CreateComponent(ctx, orgName, projectName, &openchoreo.CreateComponentRequest{
		Name:        k8sName,
		DisplayName: comp.Name,
		Description: comp.Name,
		Type:        ocEntrypoint(comp.ComponentType),
		AutoBuild:   desiredSpec.AutoBuild,
		AutoDeploy:  desiredSpec.AutoDeploy,
		Workflow: &openchoreo.ComponentWorkflowSpec{
			Kind: "ClusterWorkflow",
			Name: "dockerfile-builder",
			Parameters: &openchoreo.ComponentWorkflowParameters{
				Repository: &openchoreo.WorkflowRepository{
					URL:       repo.RepoURL,
					SecretRef: "",
					AppPath:   comp.AppPath,
					Revision:  &openchoreo.WorkflowRevision{Branch: branch},
				},
				Docker: &openchoreo.DockerParameters{Context: dockerContext, FilePath: dockerFilePath},
			},
		},
		Traits: desiredSpec.Traits,
	}); err != nil {
		return fmt.Errorf("ensure component: create OC component %q: %w", k8sName, err)
	}
	// The create above is a no-op on an existing component (the client refetches
	// on 409), so the desired spec is re-asserted unconditionally rather than
	// only on the create path — see the upsert note above.
	if err := s.client.ApplyComponentSpec(ctx, orgName, projectName, k8sName, desiredSpec); err != nil {
		return fmt.Errorf("ensure component: apply spec for %q: %w", k8sName, err)
	}
	slog.InfoContext(ctx, "ensure component: OC Component ensured", "org", orgName, "project", projectName, "component", k8sName)
	return nil
}

// ocEntrypoint maps a design component type to its OC Component entrypoint
// type. AEP's component types ARE OpenChoreo's (minus the `deployment/`
// prefix — see spec.ComponentTypeWebApplication), so this is a prefix
// re-attachment, not a translation. Unknown kinds deliberately fall back to
// deployment/service.
func ocEntrypoint(componentType string) string {
	if componentType == spec.ComponentTypeWebApplication {
		return "deployment/web-application"
	}
	return "deployment/service"
}

// GetComponentOpenAPI reads the `specs/design/` tree via the ArtifactStore
// (assembling per-component design.json + openapi.yaml into the in-memory
// design) and returns the OpenAPI spec for the named component. The URL
// param is the k8s-shaped slug; we match it against k8sname.ToK8sName(design.Name)
// so callers can use the same identifier they use everywhere else (build,
// deploy, configs). Returns ErrComponentNotFound when no design exists or
// no component matches, ErrComponentNotService when the component exists
// but isn't a "service".
func (s *componentService) GetComponentOpenAPI(ctx context.Context, orgName, projectName, componentName string) (*gen.ComponentOpenAPI, error) {
	if s.artifactStore == nil {
		return nil, fmt.Errorf("artifact store not configured")
	}
	design, err := s.artifactStore.ReadDesign(ctx, orgName, projectName)
	if err != nil {
		if spec.IsNotFound(err) {
			return nil, ErrComponentNotFound
		}
		return nil, fmt.Errorf("read design: %w", err)
	}
	if design == nil {
		return nil, ErrComponentNotFound
	}
	for _, c := range design.Components {
		if k8sname.ToK8sName(c.Name) != componentName {
			continue
		}
		if c.ComponentType != spec.ComponentTypeService {
			return &gen.ComponentOpenAPI{
				ComponentName: componentName,
				ComponentType: c.ComponentType,
			}, ErrComponentNotService
		}
		return &gen.ComponentOpenAPI{
			ComponentName: componentName,
			ComponentType: c.ComponentType,
			Spec:          c.OpenAPISpec,
		}, nil
	}
	return nil, ErrComponentNotFound
}

func (s *componentService) ListDeployments(ctx context.Context, orgName, projectName, componentName string) (*gen.DeploymentList, error) {
	list, err := s.client.ListDeployments(ctx, orgName, projectName, componentName)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *componentService) TriggerBuild(ctx context.Context, orgName, projectName, componentName string) (*gen.WorkflowRun, error) {
	// Pre-stage the per-WorkflowRun build Secret in workflows-<orgID> so
	// the shared dockerfile-builder workflow's checkout-source mounts a
	// populated Secret (see docs/design/build-credential-injection.md).
	// Manual triggers from the console go through this path; the
	// webhook-driven dispatch path uses workflowRunService.dispatchBuild.
	runName := openchoreo.NewBuildRunName(projectName, componentName)
	buildSecretRef := ""
	if s.repoSvc != nil && s.buildCredSvc != nil {
		repo, err := s.repoSvc.GetRepo(ctx, orgName, projectName)
		switch {
		case err != nil:
			slog.WarnContext(ctx, "trigger-build: GetRepo failed; proceeding without git secret (build will fail at clone)",
				"orgName", orgName, "projectName", projectName, "error", err)
		case repo == nil || repo.RepoSlug == "":
			slog.WarnContext(ctx, "trigger-build: no repo / repoSlug; proceeding without git secret",
				"orgName", orgName, "projectName", projectName)
		default:
			secretRef, sErr := s.buildCredSvc.StageBuildSecret(ctx, orgName, repo.RepoSlug, runName)
			if sErr != nil {
				return nil, fmt.Errorf("trigger-build: stage-build-secret: %w", sErr)
			}
			buildSecretRef = secretRef
		}
	}
	run, err := s.client.TriggerBuild(ctx, orgName, projectName, componentName, buildSecretRef, runName)
	if err != nil {
		return nil, err
	}
	return run, nil
}

func (s *componentService) ListBuilds(ctx context.Context, orgName, projectName, componentName string, limit int, cursor string) (*gen.WorkflowRunList, error) {
	list, err := s.client.ListWorkflowRuns(ctx, orgName, projectName, componentName, limit, cursor)
	if err != nil {
		return nil, err
	}
	return list, nil
}

// GetBuildLogs reads one build's log from a cursor, and reports whether that
// read is the whole of it.
//
// The build's terminal state is read BEFORE the log, deliberately. A build that
// finishes between the two reads is then reported as still running, and the
// caller polls once more and gets the tail — whereas reading the status last
// could declare a log complete while the lines written in between were never
// returned. One wasted poll is the right side of that trade.
func (s *componentService) GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string, sinceMillis int64) (*gen.BuildLogs, error) {
	if s.observClient == nil {
		return nil, ErrLogsUnavailable
	}
	completed := s.buildIsTerminal(ctx, orgName, buildName)

	var since time.Time
	if sinceMillis > 0 {
		since = time.UnixMilli(sinceMillis)
	}
	logs, err := s.observClient.GetBuildLogs(ctx, orgName, projectName, componentName, buildName, since)
	if err != nil {
		return nil, fmt.Errorf("get build logs: %w", err)
	}
	if logs == nil {
		logs = &gen.BuildLogs{}
	}
	logs.Logs = entriesAfter(logs.Logs, sinceMillis)
	logs.Complete = completed
	if next, ok := newestEntryMillis(logs.Logs); ok {
		logs.NextCursor = next
	}
	return logs, nil
}

// buildIsTerminal reports whether the build's WorkflowRun has completed. An
// unreadable run is reported as NOT terminal: the caller then keeps polling,
// which is recoverable, where a wrong "complete" would silently truncate the
// log at whatever had been written.
func (s *componentService) buildIsTerminal(ctx context.Context, orgName, buildName string) bool {
	if s.client == nil {
		return false
	}
	run, err := s.client.GetWorkflowRun(ctx, orgName, buildName)
	if err != nil || run == nil {
		return false
	}
	return run.Completed
}

// entriesAfter drops entries at or before the cursor. The observability query
// window is inclusive of its start, so without this the entry the cursor names
// would be re-emitted on every poll.
func entriesAfter(entries []gen.BuildLogEntry, sinceMillis int64) []gen.BuildLogEntry {
	if sinceMillis <= 0 || len(entries) == 0 {
		return entries
	}
	out := make([]gen.BuildLogEntry, 0, len(entries))
	for _, e := range entries {
		ms, ok := entryMillis(e)
		// An unparseable timestamp is kept: a duplicated line is a smaller harm
		// than a dropped one, and it cannot be ordered against the cursor.
		if !ok || ms > sinceMillis {
			out = append(out, e)
		}
	}
	return out
}

// newestEntryMillis is the cursor to hand back — the newest timestamp in this
// page. Entries arrive ascending, but the max is taken rather than the last so
// an out-of-order page cannot rewind the cursor and replay the log.
func newestEntryMillis(entries []gen.BuildLogEntry) (int64, bool) {
	var newest int64
	var found bool
	for _, e := range entries {
		if ms, ok := entryMillis(e); ok && ms > newest {
			newest, found = ms, true
		}
	}
	return newest, found
}

func entryMillis(e gen.BuildLogEntry) (int64, bool) {
	if e.Timestamp == "" {
		return 0, false
	}
	t, err := time.Parse(time.RFC3339Nano, e.Timestamp)
	if err != nil {
		return 0, false
	}
	return t.UnixMilli(), true
}
