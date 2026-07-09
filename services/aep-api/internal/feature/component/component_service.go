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

package component

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/clients/observability"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/internal/platform/k8sname"
	"github.com/wso2/aep/aep-api/models"
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
	ListComponents(ctx context.Context, orgName, projectName string, limit int, cursor string) (*models.ComponentList, error)
	GetComponent(ctx context.Context, orgName, projectName, componentName string) (*models.Component, error)
	CreateComponent(ctx context.Context, orgName, projectName string, req *models.CreateComponentRequest) (*models.Component, error)
	// EnsureComponent idempotently provisions the OpenChoreo Component CR for a
	// design component (by friendly name), so a merged-PR build has a Component to
	// build. It is the coding-dispatch pre-flight (tasks-github-native): the CR
	// must exist by merge/build time or the build fails "Component not found".
	// Idempotent — CreateComponent is 409-safe, so re-dispatch is a no-op.
	EnsureComponent(ctx context.Context, orgName, projectName, componentName string) error
	UpdateWorkflowEnvVars(ctx context.Context, orgName, projectName, componentName string, envVars []models.WorkflowEnvVarRef) error

	// Deploy (read-only — autoDeploy on the Component drives the chain)
	ListDeployments(ctx context.Context, orgName, projectName, componentName string) (*models.DeploymentList, error)

	// OpenAPI for the Test tab. Reads the spec from
	// `specs/design/components/<name>/openapi.yaml`. The Test tab's
	// swagger-ui invokes the deployed endpoint directly; CORS is enabled
	// on the service ClusterComponentType's HTTPRoute.
	GetComponentOpenAPI(ctx context.Context, orgName, projectName, componentName string) (*models.ComponentOpenAPI, error)

	// Build (workflow runs)
	TriggerBuild(ctx context.Context, orgName, projectName, componentName string) (*models.WorkflowRun, error)
	ListBuilds(ctx context.Context, orgName, projectName, componentName string, limit int, cursor string) (*models.WorkflowRunList, error)
	GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string) (*models.BuildLogs, error)
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
	artifactStore *artifacts.ArtifactStore
	// repoSvc + buildCredSvc are used by TriggerBuild to pre-stage the
	// per-WorkflowRun build Secret. Optional — nil means "no staging"
	// (tests / unit-only flows).
	repoSvc      gitrepo.RepoService
	buildCredSvc BuildSecretStager
}

// NewComponentService builds the component service. repoSvc + buildCredSvc
// may be nil in tests / unit-only flows; production wiring passes both so
// TriggerBuild can pre-stage the per-WorkflowRun build Secret.
func NewComponentService(client openchoreo.ComponentClient, observClient observability.Client, artifactStore *artifacts.ArtifactStore, repoSvc gitrepo.RepoService, buildCredSvc BuildSecretStager) ComponentService {
	return &componentService{
		client:        client,
		observClient:  observClient,
		artifactStore: artifactStore,
		repoSvc:       repoSvc,
		buildCredSvc:  buildCredSvc,
	}
}

func (s *componentService) ListComponents(ctx context.Context, orgName, projectName string, limit int, cursor string) (*models.ComponentList, error) {
	list, err := s.client.ListComponents(ctx, orgName, projectName, limit, cursor)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *componentService) GetComponent(ctx context.Context, orgName, projectName, componentName string) (*models.Component, error) {
	comp, err := s.client.GetComponent(ctx, orgName, projectName, componentName)
	if err != nil {
		return nil, err
	}
	return comp, nil
}

func (s *componentService) CreateComponent(ctx context.Context, orgName, projectName string, req *models.CreateComponentRequest) (*models.Component, error) {
	comp, err := s.client.CreateComponent(ctx, orgName, projectName, req)
	if err != nil {
		return nil, err
	}
	return comp, nil
}

// EnsureComponent provisions the OpenChoreo Component CR (one per design
// component) needed for the build to fire when the merge push arrives. Ported
// from the legacy dispatch service's ensureOCComponent pre-flight (the piece the
// tasks-github-native rebuild dropped): AutoBuild=false (every build is driven by
// the BFF pinning a WorkflowRun to the merge SHA), AutoDeploy=true (OC's
// controller creates the ReleaseBinding into the first pipeline environment once
// the build posts a Workload). Idempotent — the OC client refetches on 409, so a
// re-dispatch of the same component is a no-op. Reads the design facts (app path,
// component type, api-security) via the artifact store and the repo row via
// repoSvc; both are the existing feature ports.
func (s *componentService) EnsureComponent(ctx context.Context, orgName, projectName, componentName string) error {
	if s.artifactStore == nil {
		return fmt.Errorf("ensure component: artifact store not configured")
	}
	if s.repoSvc == nil {
		return fmt.Errorf("ensure component: repo service not configured")
	}
	comp, err := artifacts.ResolveDesignComponent(ctx, s.artifactStore, orgName, projectName, componentName)
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
	// api-configuration trait derived from design.md's exposesAPI.auth (none →
	// no trait). Set at create time; per-env reconcile is the trait_sync path's job.
	apiSecurityEnabled := models.ResolveAPISecurityEnabled(*comp)
	traits, _ := DesiredAPIConfigurationTrait(k8sName, apiSecurityEnabled)

	// repository.secretRef stays empty: build credentials are pre-staged per
	// WorkflowRun (build-credential-injection.md), so the Component's workflow
	// param carries no SecretReference.
	if _, err := s.CreateComponent(ctx, orgName, projectName, &models.CreateComponentRequest{
		Name:        k8sName,
		DisplayName: comp.Name,
		Description: comp.Name,
		Type:        ocEntrypoint(comp.ComponentType),
		AutoBuild:   false,
		AutoDeploy:  true,
		Workflow: &models.ComponentWorkflowSpec{
			Kind: "ClusterWorkflow",
			Name: "dockerfile-builder",
			Parameters: &models.ComponentWorkflowParameters{
				Repository: &models.WorkflowRepository{
					URL:       repo.RepoURL,
					SecretRef: "",
					AppPath:   comp.AppPath,
					Revision:  &models.WorkflowRevision{Branch: branch},
				},
				Docker: &models.DockerParameters{Context: dockerContext, FilePath: dockerFilePath},
			},
		},
		Traits: traits,
	}); err != nil {
		return fmt.Errorf("ensure component: create OC component %q: %w", k8sName, err)
	}
	slog.InfoContext(ctx, "ensure component: OC Component ensured", "org", orgName, "project", projectName, "component", k8sName)
	return nil
}

// ocEntrypoint maps a design component type to its OC Component entrypoint
// type. AEP's component types ARE OpenChoreo's (minus the `deployment/`
// prefix — see models.ComponentTypeWebApplication), so this is a prefix
// re-attachment, not a translation. Unknown kinds deliberately fall back to
// deployment/service.
func ocEntrypoint(componentType string) string {
	if componentType == models.ComponentTypeWebApplication {
		return "deployment/web-application"
	}
	return "deployment/service"
}

// UpdateWorkflowEnvVars writes per-component env vars onto each of the
// component's ReleaseBindings (one per environment) at
// `spec.workloadOverrides.container.env`. OC's controller picks them up
// on the next reconcile — no rebuild required. When no ReleaseBindings
// exist yet (the user is editing env vars before first deploy) the
// underlying client returns nil and the caller is expected to retry
// after the first build has produced a binding.
func (s *componentService) UpdateWorkflowEnvVars(ctx context.Context, orgName, projectName, componentName string, envVars []models.WorkflowEnvVarRef) error {
	if err := s.client.UpdateComponentWorkflowEnvVars(ctx, orgName, projectName, componentName, envVars); err != nil {
		return err
	}
	return nil
}

// GetComponentOpenAPI reads the `specs/design/` tree via the ArtifactStore
// (assembling per-component design.json + openapi.yaml into the in-memory
// design) and returns the OpenAPI spec for the named component. The URL
// param is the k8s-shaped slug; we match it against k8sname.ToK8sName(design.Name)
// so callers can use the same identifier they use everywhere else (build,
// deploy, configs). Returns ErrComponentNotFound when no design exists or
// no component matches, ErrComponentNotService when the component exists
// but isn't a "service".
func (s *componentService) GetComponentOpenAPI(ctx context.Context, orgName, projectName, componentName string) (*models.ComponentOpenAPI, error) {
	if s.artifactStore == nil {
		return nil, fmt.Errorf("artifact store not configured")
	}
	design, err := s.artifactStore.ReadDesign(ctx, orgName, projectName)
	if err != nil {
		if artifacts.IsNotFound(err) {
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
		if c.ComponentType != models.ComponentTypeService {
			return &models.ComponentOpenAPI{
				ComponentName: componentName,
				ComponentType: c.ComponentType,
			}, ErrComponentNotService
		}
		return &models.ComponentOpenAPI{
			ComponentName: componentName,
			ComponentType: c.ComponentType,
			Spec:          c.OpenAPISpec,
		}, nil
	}
	return nil, ErrComponentNotFound
}

func (s *componentService) ListDeployments(ctx context.Context, orgName, projectName, componentName string) (*models.DeploymentList, error) {
	list, err := s.client.ListDeployments(ctx, orgName, projectName, componentName)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *componentService) TriggerBuild(ctx context.Context, orgName, projectName, componentName string) (*models.WorkflowRun, error) {
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

func (s *componentService) ListBuilds(ctx context.Context, orgName, projectName, componentName string, limit int, cursor string) (*models.WorkflowRunList, error) {
	list, err := s.client.ListWorkflowRuns(ctx, orgName, projectName, componentName, limit, cursor)
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (s *componentService) GetBuildLogs(ctx context.Context, orgName, projectName, componentName, buildName string) (*models.BuildLogs, error) {
	if s.observClient == nil {
		return nil, ErrLogsUnavailable
	}
	logs, err := s.observClient.GetBuildLogs(ctx, orgName, projectName, componentName, buildName)
	if err != nil {
		return nil, fmt.Errorf("get build logs: %w", err)
	}
	return logs, nil
}
