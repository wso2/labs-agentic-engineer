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
	"time"

	"github.com/wso2/asdlc/asdlc-service/clients/openchoreo"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
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
	ListProjects(ctx context.Context, orgName string, limit int, cursor string) (*models.ProjectList, error)
	GetProject(ctx context.Context, orgName, projectName string) (*models.Project, error)
	CreateProject(ctx context.Context, orgName string, req *models.CreateProjectRequest) (*models.Project, error)
	DeleteProject(ctx context.Context, orgName, projectName string) error
	GetProjectStatus(ctx context.Context, orgName, projectName string) (*models.ProjectStatus, error)
}

type projectService struct {
	client      openchoreo.ProjectClient
	repoSvc     gitrepo.RepoService
	webhookSvc  gitrepo.WebhookService
	artifactSvc artifacts.ArtifactService
	store       *artifacts.ArtifactStore
	taskRepo    repositories.TaskRepository
	skillsProv  skillsProvisioner
}

// skillsProvisioner is the narrow port for eagerly provisioning the org's
// skills repo on project creation. The full skills.SkillService satisfies it.
// Defined here so project doesn't import the skills package. §6.3/§10.2.
type skillsProvisioner interface {
	EnsureProvisioned(ctx context.Context, orgID string) error
}

// ProjectServiceWithSkills surfaces the skills-provisioner setter so main can
// wire the skills store without widening the constructor signature.
type ProjectServiceWithSkills interface {
	SetSkillsProvisioner(p skillsProvisioner)
}

func (s *projectService) SetSkillsProvisioner(p skillsProvisioner) { s.skillsProv = p }

var _ ProjectServiceWithSkills = (*projectService)(nil)

func NewProjectService(
	client openchoreo.ProjectClient,
	repoSvc gitrepo.RepoService,
	webhookSvc gitrepo.WebhookService,
	artifactSvc artifacts.ArtifactService,
	store *artifacts.ArtifactStore,
	taskRepo repositories.TaskRepository,
) ProjectService {
	return &projectService{
		client:      client,
		repoSvc:     repoSvc,
		webhookSvc:  webhookSvc,
		artifactSvc: artifactSvc,
		store:       store,
		taskRepo:    taskRepo,
	}
}

func (s *projectService) ListProjects(ctx context.Context, orgName string, limit int, cursor string) (*models.ProjectList, error) {
	list, err := s.client.ListProjects(ctx, orgName, limit, cursor)
	if err != nil {
		return nil, translateHTTPError(err)
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
		repoInfo, createErr := s.repoSvc.CreateRepo(ctx, orgName, project.Name, req.Name)
		if createErr != nil {
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

func (s *projectService) DeleteProject(ctx context.Context, orgName, projectName string) error {
	if err := translateHTTPError(s.client.DeleteProject(ctx, orgName, projectName)); err != nil {
		return err
	}

	// Clean up the git clone
	if s.repoSvc != nil {
		if err := s.repoSvc.DeleteRepo(ctx, orgName, projectName); err != nil {
			slog.ErrorContext(ctx, "failed to delete git repo for project", "org", orgName, "project", projectName, "error", err)
		}
	}

	return nil
}

func (s *projectService) GetProjectStatus(ctx context.Context, orgName, projectName string) (*models.ProjectStatus, error) {
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

	// Check tasks.
	tasks, err := s.taskRepo.ListByProjectID(ctx, orgName, projectName)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	status.HasTasks = len(tasks) > 0

	if !status.HasTasks {
		status.Phase = "tasks"
		return status, nil
	}

	status.Phase = "components"
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
