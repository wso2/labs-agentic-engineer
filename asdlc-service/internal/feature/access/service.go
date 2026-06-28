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

package access

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/connections"
	"github.com/wso2/asdlc/asdlc-service/internal/feature/gitrepo"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// ErrOrgServiceNotFound is returned when the requested org-service name resolves
// to no provider component in the org endpoint catalog (the `not-found` case —
// not requestable). Maps to a 404 at the endpoint.
var ErrOrgServiceNotFound = errors.New("org service not found in catalog")

// taskRepo is the subset of repositories.TaskRepository the access flow needs:
// it creates the provider's publish ComponentTask. Narrowed to keep the
// service's surface honest (it never reads/lists tasks).
type taskRepo interface {
	Create(ctx context.Context, task *models.ComponentTask) error
}

// accessStore is the subset of *Repository the request + sync flows need — the
// idempotency lookup, the row insert, and the provider-task fan-out + status
// flip used by the reject close-out. Narrowed to an interface so the flow is
// testable without a DB (the concrete *Repository satisfies it).
type accessStore interface {
	FindOpenForTarget(ctx context.Context, orgID, providerProjectID, providerComponentName string) (*models.AccessRequest, error)
	Create(ctx context.Context, ar *models.AccessRequest) error
	ListByProviderTask(ctx context.Context, providerTaskID string) ([]models.AccessRequest, error)
	ListByConsumerProject(ctx context.Context, orgID, projectID string) ([]models.AccessRequest, error)
	UpdateStatus(ctx context.Context, id, status string) error
}

// AccessService implements the cross-project access-request flow (marketplace
// P3.5). A consumer component depends on an org-service published only
// project-only; requesting access creates a publish ComponentTask + GitHub issue
// on the PROVIDER's project/repo (the provider's later dispatch is their
// consent) and writes an AccessRequest tracking row. Many consumers of one
// provider endpoint dedupe onto a single provider task (one task, N rows).
//
// This service only CREATES; status sync (grant/reject) is a later increment.
type AccessService struct {
	repo     accessStore
	catalog  *connections.OrgEndpointCatalog
	issueSvc gitrepo.IssueService
	taskRepo taskRepo
	store    *artifacts.ArtifactStore
}

// NewAccessService wires the access-request flow. catalog resolves the provider
// row; store reads the provider design for the component's app path; issueSvc +
// taskRepo create the provider publish task/issue; repo persists the row.
func NewAccessService(
	repo accessStore,
	catalog *connections.OrgEndpointCatalog,
	issueSvc gitrepo.IssueService,
	tr taskRepo,
	store *artifacts.ArtifactStore,
) *AccessService {
	return &AccessService{
		repo:     repo,
		catalog:  catalog,
		issueSvc: issueSvc,
		taskRepo: tr,
		store:    store,
	}
}

// RequestAccessInput is the request-flow's input. orgHandle is the OC namespace /
// org handle; consumerProject + consumerComponent identify who is asking;
// orgServiceName is the catalog key the consumer's dependency references (== the
// provider OC component name).
type RequestAccessInput struct {
	OrgHandle         string
	ConsumerProject   string
	ConsumerComponent string
	OrgServiceName    string
}

// RequestAccess creates (or dedupes onto) the provider publish task + issue and
// records an AccessRequest. Steps:
//
//	a. Resolve the provider catalog row by org-service name (404 if absent).
//	   Derive provider project + the provider's LOGICAL component name.
//	b. Read the provider design to find the component's app path (best-effort —
//	   the issue body degrades gracefully without it).
//	c. Idempotency: if an open request/task already targets this provider
//	   component, ride on the SAME provider task/issue — create only a new
//	   AccessRequest row for THIS consumer and return it.
//	d. Otherwise create the GitHub issue on the provider repo + the org-publish
//	   ComponentTask on the provider project.
//	e. Write the AccessRequest (status requested) and return it.
func (s *AccessService) RequestAccess(ctx context.Context, in RequestAccessInput) (*models.AccessRequest, error) {
	if in.OrgHandle == "" || in.ConsumerProject == "" || in.ConsumerComponent == "" || in.OrgServiceName == "" {
		return nil, fmt.Errorf("access: orgHandle, consumerProject, consumerComponent and orgServiceName are required")
	}

	// (a) Resolve the provider row. The org-service name is the OC component
	// name (catalog key). The provider project is the row's owner project; the
	// provider's LOGICAL component name (what the design + repo know it as) is
	// the OC component name with the project prefix trimmed.
	row, ok, err := s.catalog.FindByComponent(ctx, in.OrgHandle, in.OrgServiceName)
	if err != nil {
		return nil, fmt.Errorf("access: resolve provider for %q: %w", in.OrgServiceName, err)
	}
	if !ok {
		return nil, ErrOrgServiceNotFound
	}
	providerProject := row.Project
	providerLogicalComponent := deriveLogicalComponent(row.Project, row.Component)

	// (b) App path from the provider design (best-effort). Used to point the
	// agent at the right workload.yaml; the issue body degrades without it.
	appPath := s.lookupAppPath(ctx, in.OrgHandle, providerProject, providerLogicalComponent)

	// (c) Idempotency: ride on an existing open provider task if one exists.
	if existing, ferr := s.repo.FindOpenForTarget(ctx, in.OrgHandle, providerProject, providerLogicalComponent); ferr != nil {
		return nil, ferr
	} else if existing != nil {
		ar := &models.AccessRequest{
			OrgID:                 in.OrgHandle,
			ConsumerProjectID:     in.ConsumerProject,
			ConsumerComponentName: in.ConsumerComponent,
			OrgServiceName:        in.OrgServiceName,
			ProviderProjectID:     providerProject,
			ProviderComponentName: providerLogicalComponent,
			ProviderTaskID:        existing.ProviderTaskID,
			ProviderIssueNumber:   existing.ProviderIssueNumber,
			ProviderIssueURL:      existing.ProviderIssueURL,
			Status:                models.AccessRequestStatusRequested,
		}
		if err := s.repo.Create(ctx, ar); err != nil {
			return nil, err
		}
		slog.InfoContext(ctx, "access request deduped onto existing provider task",
			"orgService", in.OrgServiceName, "providerTask", existing.ProviderTaskID,
			"consumer", in.ConsumerProject+"/"+in.ConsumerComponent)
		return ar, nil
	}

	// (d) Create the provider GitHub issue + the org-publish ComponentTask.
	body := gitrepo.BuildOrgPublishIssueBody(providerLogicalComponent, appPath, in.ConsumerProject)
	title := fmt.Sprintf("Publish %s org-wide: add namespace visibility", providerLogicalComponent)
	issue, err := s.issueSvc.CreateIssue(ctx, in.OrgHandle, providerProject, gitrepo.CreateIssueRequest{
		Title:  title,
		Body:   body,
		Labels: []string{"asdlc", "access-request"},
	})
	if err != nil {
		return nil, fmt.Errorf("access: create provider issue on %s: %w", providerProject, err)
	}

	task := &models.ComponentTask{
		ProjectID:       providerProject,
		OrgID:           in.OrgHandle,
		Type:            models.TaskTypeOrgPublish,
		ComponentName:   providerLogicalComponent,
		Title:           title,
		Rationale:       fmt.Sprintf("Cross-project access request from %s: add namespace visibility so %s is consumable org-wide.", in.ConsumerProject, providerLogicalComponent),
		Status:          string(models.TaskStatusPending),
		LifecycleStatus: string(models.TaskLifecycleGhIssueCreated),
		ExecType:        "WORKER",
		IssueURL:        issue.URL,
		IssueNumber:     issue.Number,
		Labels:          models.StringSlice{"asdlc", "access-request"},
	}
	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, fmt.Errorf("access: create provider publish task: %w", err)
	}

	// (e) Persist the tracking row.
	ar := &models.AccessRequest{
		OrgID:                 in.OrgHandle,
		ConsumerProjectID:     in.ConsumerProject,
		ConsumerComponentName: in.ConsumerComponent,
		OrgServiceName:        in.OrgServiceName,
		ProviderProjectID:     providerProject,
		ProviderComponentName: providerLogicalComponent,
		ProviderTaskID:        task.ID,
		ProviderIssueNumber:   issue.Number,
		ProviderIssueURL:      issue.URL,
		Status:                models.AccessRequestStatusRequested,
	}
	if err := s.repo.Create(ctx, ar); err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "created cross-project access request",
		"orgService", in.OrgServiceName, "providerProject", providerProject,
		"providerComponent", providerLogicalComponent, "providerIssue", issue.URL,
		"providerTask", task.ID, "consumer", in.ConsumerProject+"/"+in.ConsumerComponent)
	return ar, nil
}

// ListByConsumerProject returns every access request a consumer project's
// components have raised, newest first — the data the console reads to render
// per-dependency request status chips (P3.5 §6).
func (s *AccessService) ListByConsumerProject(ctx context.Context, orgHandle, projectName string) ([]models.AccessRequest, error) {
	if orgHandle == "" || projectName == "" {
		return nil, fmt.Errorf("access: orgHandle and projectName are required")
	}
	return s.repo.ListByConsumerProject(ctx, orgHandle, projectName)
}

// RejectByProviderTask is the P3.5 reject close-out: the provider's
// `org-publish` task was rejected (its PR closed unmerged), so every consumer
// AccessRequest riding on that provider task is flipped to `rejected`. Already-
// terminal rows (granted/rejected) are skipped — only still-open requests move.
// Best-effort per row: a single UpdateStatus failure is logged and the loop
// continues; the method returns the first error so the caller can log it, but
// callers treat it as advisory (webhook processing must not fail on it).
func (s *AccessService) RejectByProviderTask(ctx context.Context, providerTaskID string) error {
	if providerTaskID == "" {
		return fmt.Errorf("access: providerTaskID is required")
	}
	rows, err := s.repo.ListByProviderTask(ctx, providerTaskID)
	if err != nil {
		return fmt.Errorf("access: list rejected provider task %q: %w", providerTaskID, err)
	}
	var firstErr error
	flipped := 0
	for i := range rows {
		if rows[i].Status == models.AccessRequestStatusGranted || rows[i].Status == models.AccessRequestStatusRejected {
			continue
		}
		if uerr := s.repo.UpdateStatus(ctx, rows[i].ID, models.AccessRequestStatusRejected); uerr != nil {
			slog.WarnContext(ctx, "access reject: UpdateStatus failed",
				"accessRequest", rows[i].ID, "error", uerr)
			if firstErr == nil {
				firstErr = uerr
			}
			continue
		}
		flipped++
	}
	slog.InfoContext(ctx, "access requests rejected on provider task rejection",
		"providerTask", providerTaskID, "rejected", flipped)
	return firstErr
}

// lookupAppPath reads the provider project's design and returns the app path of
// the component matching the logical name (case-insensitive). Best-effort: on
// any error or a missing component it returns "" — the issue body degrades
// gracefully (it tells the agent to open "the component's workload.yaml").
func (s *AccessService) lookupAppPath(ctx context.Context, orgHandle, providerProject, logicalComponent string) string {
	if s.store == nil {
		return ""
	}
	design, err := s.store.ReadDesign(ctx, orgHandle, providerProject)
	if err != nil || design == nil {
		if err != nil {
			slog.WarnContext(ctx, "access: read provider design for app path failed",
				"providerProject", providerProject, "error", err)
		}
		return ""
	}
	for i := range design.Components {
		if strings.EqualFold(design.Components[i].Name, logicalComponent) {
			return design.Components[i].AppPath
		}
	}
	return ""
}

// deriveLogicalComponent maps the provider OC component name (the catalog key,
// e.g. `hr-directory-employee-api`) to the LOGICAL component name the design +
// repo know it as (e.g. `employee-api`) by trimming the `<project>-` prefix. The
// OC component name is conventionally `<project>-<logical>`; when the prefix
// isn't present (no convention match), the OC name is returned unchanged.
func deriveLogicalComponent(project, ocComponent string) string {
	prefix := project + "-"
	if strings.HasPrefix(ocComponent, prefix) {
		return strings.TrimPrefix(ocComponent, prefix)
	}
	return ocComponent
}

// compile-time assertions: the concrete repos satisfy the narrowed interfaces.
var (
	_ taskRepo    = (repositories.TaskRepository)(nil)
	_ accessStore = (*Repository)(nil)
)
