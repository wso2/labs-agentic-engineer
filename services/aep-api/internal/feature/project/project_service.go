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

package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

// Error sentinels for the project feature. ErrProjectNotFound is owned here.
// ErrUnauthorized / ErrForbidden are feature-local sentinels the controllers
// branch on.
var (
	ErrProjectNotFound = errors.New("project not found")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrForbidden       = errors.New("forbidden")
)

// ProjectService handles business logic for project operations.
type ProjectService interface {
	ListProjects(ctx context.Context, orgName string, limit int, cursor, search string) (*models.ProjectList, error)
	GetProject(ctx context.Context, orgName, projectName string) (*models.Project, error)
	CreateProject(ctx context.Context, orgName string, req *models.CreateProjectRequest) (*models.Project, error)
	DeleteProject(ctx context.Context, orgName, projectName string) error
	GetProjectStatus(ctx context.Context, orgName, projectName string) (*models.ProjectStatus, error)
}

type projectService struct {
	client        openchoreo.ProjectClient
	repoSvc       gitrepo.RepoService
	webhookSvc    gitrepo.WebhookService
	artifactSvc   artifacts.ArtifactService
	store         *artifacts.ArtifactStore
	execs         repositories.ExecutionRepository
	skillsProv    skillsProvisioner
	deprovisioner resourceDeprovisioner // dependency provisioning teardown; may be nil
	cycle         cycleFlowReader       // live Temporal cycle phase (§R2.2); may be nil
}

// cycleFlowReader is project_service's narrow consumer port for the cycle's
// live workflow phase (§R2.2 Project.Phase reconciliation). *cycle.Service
// satisfies it. Defined here — over the shared contract module, not
// feature/cycle's concrete type — so project holds no feature edge to cycle
// (internal/arch/arch_test.go featureEdgeAllowlist). Wired via
// SetCycleFlowReader at the composition root; nil disables CyclePhase.
type cycleFlowReader interface {
	GetFlowState(ctx context.Context, orgHandle, projectName string) (*contract.CycleStateView, error)
}

func (s *projectService) SetCycleFlowReader(c cycleFlowReader) { s.cycle = c }

// resourceDeprovisioner is project_service's narrow consumer port for the
// dependency-provisioning teardown: on project delete it deprovisions the
// project's OC Resource model (external + platform resources), which the OC
// Project delete does not cascade. *provisioning.Service satisfies it. Wired via
// SetResourceDeprovisioner at the composition root; nil is a no-op.
type resourceDeprovisioner interface {
	DeprovisionProject(ctx context.Context, orgID, projectID string) error
}

// skillsProvisioner is the narrow port for eagerly provisioning the org's
// skills repo on project creation. The full skills.SkillService satisfies it.
// Defined here so project doesn't import the skills package. §6.3/§10.2.
type skillsProvisioner interface {
	EnsureProvisioned(ctx context.Context, orgID string) error
}

func (s *projectService) SetSkillsProvisioner(p skillsProvisioner) { s.skillsProv = p }

func NewProjectService(
	client openchoreo.ProjectClient,
	repoSvc gitrepo.RepoService,
	webhookSvc gitrepo.WebhookService,
	artifactSvc artifacts.ArtifactService,
	store *artifacts.ArtifactStore,
	execs repositories.ExecutionRepository,
) *projectService {
	return &projectService{
		client:      client,
		repoSvc:     repoSvc,
		webhookSvc:  webhookSvc,
		artifactSvc: artifactSvc,
		store:       store,
		execs:       execs,
	}
}

func (s *projectService) ListProjects(ctx context.Context, orgName string, limit int, cursor, search string) (*models.ProjectList, error) {
	list, err := s.client.ListProjects(ctx, orgName, limit, cursor)
	if err != nil {
		return nil, translateHTTPError(err)
	}
	// Annotate each project with its repo URL from the BFF's own rows (#108
	// — the delete dialog names the repo the cascade destroys). Best-effort:
	// a failing join degrades to a repoUrl-less list, never a 500.
	if s.repoSvc != nil && len(list.Items) > 0 {
		if repos, repoErr := s.repoSvc.ListByOrg(ctx, orgName); repoErr != nil {
			slog.WarnContext(ctx, "repoUrl annotation skipped", "org", orgName, "error", repoErr)
		} else {
			byProject := make(map[string]string, len(repos))
			for _, r := range repos {
				byProject[r.ProjectID] = r.RepoURL
			}
			for i := range list.Items {
				list.Items[i].RepoURL = byProject[list.Items[i].Name]
			}
		}
	}
	// The search filter is applied Go-side over the FETCHED PAGE only — the
	// OpenChoreo list API takes just (limit, cursor), so a match on a later
	// page is not pulled forward; the caller pages via nextCursor and filters
	// each page. Acceptable for the console's org-sized project counts.
	if search != "" {
		needle := strings.ToLower(search)
		filtered := make([]models.Project, 0, len(list.Items))
		for _, p := range list.Items {
			if strings.Contains(strings.ToLower(p.Name), needle) ||
				strings.Contains(strings.ToLower(p.DisplayName), needle) {
				filtered = append(filtered, p)
			}
		}
		list.Items = filtered
	}
	return list, nil
}

func (s *projectService) GetProject(ctx context.Context, orgName, projectName string) (*models.Project, error) {
	project, err := s.client.GetProject(ctx, orgName, projectName)
	if err != nil {
		return nil, translateHTTPError(err)
	}
	return project, nil
}

func (s *projectService) CreateProject(ctx context.Context, orgName string, req *models.CreateProjectRequest) (*models.Project, error) {
	project, err := s.client.CreateProject(ctx, orgName, req)
	if err != nil {
		return nil, translateHTTPError(err)
	}

	// Eagerly provision the org's shared skills repo (+ seed built-ins) so the
	// first design run doesn't pay repo-creation latency. Async + best-effort;
	// reads self-heal via ensureSkillsRepo if this never ran. §6.3/§10.3.
	if s.skillsProv != nil {
		go func(orgID string) {
			bg, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if perr := s.skillsProv.EnsureProvisioned(bg, orgID); perr != nil {
				slog.WarnContext(bg, "skills repo provisioning failed (will self-heal on read)",
					"org", orgID, "error", perr)
			}
		}(orgName)
	}

	// Provision + clone the platform-owned git repo (async — polling via GetRepoStatus).
	if s.repoSvc != nil {
		repoInfo, createErr := s.repoSvc.CreateRepo(ctx, orgName, project.Name, req.Name, req.RepoName)
		if createErr != nil {
			// A repo name that already exists — user-chosen or derived from
			// the project name — can never succeed on retry: compensate the
			// OC project away and fail the create so the user picks another
			// name. Every other repo failure stays best-effort (clone happens
			// async and can be retried).
			if gitrepo.IsRepoNameConflict(createErr) {
				if delErr := s.client.DeleteProject(ctx, orgName, project.Name); delErr != nil {
					slog.ErrorContext(ctx, "failed to compensate project after repo name conflict",
						"project", project.Name, "error", delErr)
				}
				return nil, createErr
			}
			slog.ErrorContext(ctx, "failed to provision repo", "project", project.Name, "error", createErr)
			// Don't fail project creation — clone happens async and can be retried.
		} else {
			// Build credentials are now pre-staged per WorkflowRun as a K8s
			// Secret named `<workflowRunName>-git-secret` in
			// workflows-<orgID> immediately before each dispatch — see
			// docs/design/build-credential-injection.md. Project creation
			// no longer participates in any secret provisioning;
			// OcSecretRefName is unused on new flows.
			if repoInfo == nil {
				slog.ErrorContext(ctx, "nil repoInfo on CreateRepo", "project", project.Name)
			}
			// Register the per-repo webhook so the BFF starts receiving events
			// (pull_request, push, issue_comment) on this repo. Best-effort.
			if s.webhookSvc != nil {
				if _, hookErr := s.webhookSvc.Register(ctx, orgName, project.Name); hookErr != nil {
					slog.ErrorContext(ctx, "failed to register webhook on repo",
						"project", project.Name, "error", hookErr)
				}
			}
		}
	}

	return project, nil
}

// SetResourceDeprovisioner wires the dependency-provisioning teardown so a
// project delete deprovisions its OC Resource model. A nil deprovisioner is a
// documented no-op.
func (s *projectService) SetResourceDeprovisioner(d resourceDeprovisioner) {
	s.deprovisioner = d
}

func (s *projectService) DeleteProject(ctx context.Context, orgName, projectName string) error {
	// Deprovision the project's OC Resource model FIRST — while its design (the
	// dependency inventory) is still readable and before the OC Project delete,
	// which does not cascade the logically-owned Resources/bindings. Best-effort.
	if s.deprovisioner != nil {
		if err := s.deprovisioner.DeprovisionProject(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to deprovision project resources", "org", orgName, "project", projectName, "error", err)
		}
	}

	if err := translateHTTPError(s.client.DeleteProject(ctx, orgName, projectName)); err != nil {
		return err
	}

	// Clean up the git clone
	if s.repoSvc != nil {
		if err := s.repoSvc.DeleteRepo(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to delete git repo for project", "org", orgName, "project", projectName, "error", err)
		}
	}

	// Tasks are GitHub issues (deleted with the repo). The platform-owned
	// executions rows for the project ARE purged here — they are keyed to the
	// project and would otherwise orphan (no FK to cascade). Best-effort, like
	// the repo cleanup.
	if s.execs != nil {
		if err := s.execs.DeleteByProject(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to purge executions for project", "org", orgName, "project", projectName, "error", err)
		}
	}

	return nil
}

// GetProjectStatus computes the artifact-derived status, then augments it
// with the live Temporal cycle phase (§R2.2 — split responsibility): Phase
// keeps reporting artifact facts unchanged regardless of the path below;
// CyclePhase is layered on afterward from a single exit point so every
// return in computeProjectStatus gets it without duplicating the call.
func (s *projectService) GetProjectStatus(ctx context.Context, orgName, projectName string) (*models.ProjectStatus, error) {
	status, err := s.computeProjectStatus(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}
	s.attachCyclePhase(ctx, orgName, projectName, status)
	return status, nil
}

// attachCyclePhase sets status.CyclePhase from the active development
// cycle's live workflow query, if any. Best-effort: no cycle reader wired, no
// active cycle (ErrNoActiveCycle), orchestration disabled
// (ErrOrchestrationDisabled), or a query failure all leave CyclePhase unset
// rather than failing the whole status read — the artifact-derived Phase
// above is always a valid status on its own.
func (s *projectService) attachCyclePhase(ctx context.Context, orgName, projectName string, status *models.ProjectStatus) {
	if s.cycle == nil {
		return
	}
	st, err := s.cycle.GetFlowState(ctx, orgName, projectName)
	if err != nil || st == nil {
		return
	}
	phase := string(st.Phase)
	status.CyclePhase = &phase
}

func (s *projectService) computeProjectStatus(ctx context.Context, orgName, projectName string) (*models.ProjectStatus, error) {
	status := &models.ProjectStatus{}

	// Check git repo
	if s.repoSvc == nil {
		status.Phase = "no-repo"
		return status, nil
	}

	repo, err := s.repoSvc.GetRepo(ctx, orgName, projectName)
	if err != nil {
		slog.ErrorContext(ctx, "get repo for project status", "error", err, "project", projectName)
		status.Phase = "no-repo"
		return status, nil
	}
	if repo == nil {
		status.Phase = "no-repo"
		return status, nil
	}

	if done := applyRepoToProjectStatus(status, repo); done {
		return status, nil
	}

	// Check requirements (any markdown doc under specs/requirements/ counts).
	files, err := s.store.ListRequirements(ctx, orgName, projectName)
	if err != nil && !artifacts.IsNotFound(err) {
		return nil, fmt.Errorf("list requirements: %w", err)
	}
	status.HasSpec = len(files) > 0

	if s.artifactSvc != nil {
		reqVersions, _ := s.artifactSvc.ListRequirementsVersions(ctx, orgName, projectName)
		designVersions, _ := s.artifactSvc.ListDesignVersions(ctx, orgName, projectName)

		if len(reqVersions) > 0 {
			status.SpecStatus = "approved"
		} else if status.HasSpec {
			status.SpecStatus = "draft"
		}
		if len(designVersions) > 0 {
			status.DesignStatus = "approved"
		}
	}

	if !status.HasSpec {
		status.Phase = "prompt"
		return status, nil
	}

	// Check design
	design, err := s.store.ReadDesign(ctx, orgName, projectName)
	if err != nil && !artifacts.IsNotFound(err) {
		return nil, fmt.Errorf("read design: %w", err)
	}
	status.HasDesign = design != nil

	if !status.HasDesign {
		status.Phase = "spec"
		return status, nil
	}

	// Tasks are GitHub issues now (no component_tasks table). The status phase
	// stops at "tasks" once a design exists; the console derives per-Task detail
	// live from the tasks API (§8). HasTasks is left false here — a live GitHub
	// count on every status poll is not worth the request.
	status.Phase = "tasks"
	return status, nil
}

// applyRepoToProjectStatus maps a provisioned git_repositories row onto the
// status fields that depend on repo lifecycle. Returns true when phase is
// fully determined (no-repo, cloning, or error) and artifact checks can stop.
func applyRepoToProjectStatus(status *models.ProjectStatus, repo *models.GitRepository) bool {
	if repo == nil {
		status.Phase = "no-repo"
		return true
	}

	status.RepoStatus = repo.Status
	status.RepoURL = repo.RepoURL

	switch repo.Status {
	case "pending", "cloning":
		status.Phase = "repo-cloning"
		return true
	case "error":
		status.Phase = "repo-error"
		status.RepoErrorMessage = repo.ErrorMessage
		return true
	}
	return false
}

// translateHTTPError lifts OC-level sentinel errors (openchoreo.ErrNotFound
// etc.) into the project-service vocabulary the controllers branch on. The
// underlying err is preserved in the chain so deeper layers can still
// errors.Is against openchoreo.* if they want richer context.
func translateHTTPError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, openchoreo.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrProjectNotFound, err)
	case errors.Is(err, openchoreo.ErrUnauthorized):
		return ErrUnauthorized
	case errors.Is(err, openchoreo.ErrForbidden):
		return ErrForbidden
	}
	return err
}
