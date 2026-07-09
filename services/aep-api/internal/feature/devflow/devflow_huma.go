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

package devflow

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"

	"github.com/wso2/aep/aep-api/internal/platform/humakit"
	"github.com/wso2/aep/aep-api/models"
)

// HumaStore is the read/lookup surface the API needs over workflow_runs.
// Satisfied by repositories.WorkflowRunRepository.
type HumaStore interface {
	RunningDevByProject(ctx context.Context, orgID, projectID string) (*models.DevflowRun, error)
	RunningTaskByIssue(ctx context.Context, repo string, issueNumber int) (*models.DevflowRun, error)
	GetByWorkflowID(ctx context.Context, orgID, workflowID string) (*models.DevflowRun, error)
	ListByProject(ctx context.Context, orgID, projectID, kind string) ([]models.DevflowRun, error)
}

// RepoLookup resolves a project's "owner/name" repo full name. Satisfied by
// an app-root adapter over the repo repository.
type RepoLookup interface {
	RepoFullName(ctx context.Context, orgID, projectID string) (string, error)
}

// HumaService backs the /devflows endpoints: start a dev workflow, list a
// project's runs, read one run's live status, and send a gate decision.
type HumaService struct {
	rt     *Runtime
	store  HumaStore
	repos  RepoLookup
	tagger Tagger
}

// NewHumaService wires the API service.
func NewHumaService(rt *Runtime, store HumaStore, repos RepoLookup, tagger Tagger) *HumaService {
	return &HumaService{rt: rt, store: store, repos: repos, tagger: tagger}
}

// --- inputs / outputs -------------------------------------------------------

type startInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	Body        struct {
		Tag   string     `json:"tag,omitempty" doc:"Version tag to build; created from requirements when omitted"`
		Gates GateConfig `json:"gates,omitempty" doc:"Per-gate auto/manual config (default: all auto)"`
	}
}

type startOutput struct {
	Body struct {
		WorkflowID string `json:"workflowId"`
		RunID      string `json:"runId"`
	}
}

type listInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type listOutput struct {
	Body []models.DevflowRun
}

type getInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	WorkflowID  string `path:"workflowId" doc:"Dev workflow id"`
}

type getOutput struct {
	Body DevFlowStatus
}

type gateInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	WorkflowID  string `path:"workflowId" doc:"Dev workflow id"`
	Gate        string `path:"gate" doc:"Gate name to decide"`
	Body        struct {
		Approve   bool   `json:"approve" doc:"Approve (true) or reject (false) the gate"`
		Note      string `json:"note,omitempty" doc:"Optional reviewer note"`
		TaskIssue int    `json:"taskIssue,omitempty" doc:"Target a task child workflow by its issue number instead of the dev workflow"`
	}
}

// RegisterDevflow registers the /devflows surface on the code-first Huma API.
func RegisterDevflow(api huma.API, svc *HumaService) {
	huma.Register(api, huma.Operation{
		OperationID: "start-devflow",
		Method:      http.MethodPost,
		Path:        "/projects/{projectName}/devflows",
		Summary:     "Start the development workflow for a project",
		Tags:        []string{"Devflows"},
		Security:    humakit.SecurityUserJWT,
		// 200, not 202: this returns a meaningful body (the workflow id + run
		// id) the caller needs. The console REST client drops 202/204 bodies
		// (they're used for bodyless async-accept elsewhere), so a 202 here
		// would strand the client with an undefined response.
	}, svc.start)

	huma.Register(api, huma.Operation{
		OperationID: "list-devflows",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/devflows",
		Summary:     "List a project's development workflow runs",
		Tags:        []string{"Devflows"},
		Security:    humakit.SecurityUserJWT,
	}, svc.list)

	huma.Register(api, huma.Operation{
		OperationID: "get-devflow",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/devflows/{workflowId}",
		Summary:     "Get a dev workflow's live status",
		Tags:        []string{"Devflows"},
		Security:    humakit.SecurityUserJWT,
	}, svc.get)

	huma.Register(api, huma.Operation{
		OperationID:   "decide-devflow-gate",
		Method:        http.MethodPost,
		Path:          "/projects/{projectName}/devflows/{workflowId}/gates/{gate}",
		Summary:       "Approve or reject a human-in-the-loop gate",
		Tags:          []string{"Devflows"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusNoContent,
	}, svc.decideGate)
}

// --- handlers ---------------------------------------------------------------

func (s *HumaService) start(ctx context.Context, in *startInput) (*startOutput, error) {
	c, err := s.temporal()
	if err != nil {
		return nil, err
	}
	// One dev workflow per project at a time.
	if running, lerr := s.store.RunningDevByProject(ctx, in.OrgHandle, in.ProjectName); lerr != nil {
		return nil, huma.Error500InternalServerError("lookup running devflow")
	} else if running != nil {
		return nil, huma.Error409Conflict("a development workflow is already running for this project")
	}

	repo, err := s.repos.RepoFullName(ctx, in.OrgHandle, in.ProjectName)
	if err != nil {
		return nil, huma.Error404NotFound("project repository not found")
	}

	tag := in.Body.Tag
	if tag == "" {
		if tag, err = s.tagger.CreateVersionTag(ctx, in.OrgHandle, in.ProjectName); err != nil {
			return nil, huma.Error500InternalServerError("create version tag")
		}
	}

	// One run suffix per start: it distinguishes each re-run of the same tag
	// (own workflow id + timeline) and is threaded to task children so the dev
	// run and its tasks share one lineage.
	suffix := newRunSuffix(time.Now())
	workflowID := DevWorkflowID(in.OrgHandle, in.ProjectName, tag, suffix)
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    workflowID,
		TaskQueue:             s.rt.TaskQueue(),
		WorkflowIDReusePolicy: enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	}, DevFlowWorkflowName, DevFlowInput{
		OrgID:     in.OrgHandle,
		ProjectID: in.ProjectName,
		Repo:      repo,
		Tag:       tag,
		Gates:     in.Body.Gates,
		RunSuffix: suffix,
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("start workflow: " + err.Error())
	}
	out := &startOutput{}
	out.Body.WorkflowID = run.GetID()
	out.Body.RunID = run.GetRunID()
	return out, nil
}

func (s *HumaService) list(ctx context.Context, in *listInput) (*listOutput, error) {
	rows, err := s.store.ListByProject(ctx, in.OrgHandle, in.ProjectName, models.WorkflowKindDev)
	if err != nil {
		return nil, huma.Error500InternalServerError("list devflows")
	}
	return &listOutput{Body: rows}, nil
}

func (s *HumaService) get(ctx context.Context, in *getInput) (*getOutput, error) {
	c, err := s.temporal()
	if err != nil {
		return nil, err
	}
	// Fence to the caller's org before querying Temporal.
	row, lerr := s.store.GetByWorkflowID(ctx, in.OrgHandle, in.WorkflowID)
	if lerr != nil {
		return nil, huma.Error500InternalServerError("lookup devflow")
	}
	if row == nil {
		return nil, huma.Error404NotFound("devflow not found")
	}
	resp, err := c.QueryWorkflow(ctx, in.WorkflowID, "", QueryStatus)
	if err != nil {
		return nil, huma.Error404NotFound("devflow status unavailable")
	}
	var st DevFlowStatus
	if err := resp.Get(&st); err != nil {
		return nil, huma.Error500InternalServerError("decode status")
	}
	return &getOutput{Body: st}, nil
}

func (s *HumaService) decideGate(ctx context.Context, in *gateInput) (*struct{}, error) {
	c, err := s.temporal()
	if err != nil {
		return nil, err
	}
	// Fence the dev workflow to the caller's org.
	row, lerr := s.store.GetByWorkflowID(ctx, in.OrgHandle, in.WorkflowID)
	if lerr != nil {
		return nil, huma.Error500InternalServerError("lookup devflow")
	}
	if row == nil {
		return nil, huma.Error404NotFound("devflow not found")
	}

	target := in.WorkflowID
	if in.Body.TaskIssue > 0 {
		// Route the decision to a task child workflow instead of the dev workflow.
		taskRow, terr := s.store.RunningTaskByIssue(ctx, row.Repo, in.Body.TaskIssue)
		if terr != nil {
			return nil, huma.Error500InternalServerError("lookup task workflow")
		}
		if taskRow == nil {
			return nil, huma.Error404NotFound("no running task workflow for that issue")
		}
		target = taskRow.WorkflowID
	}

	if err := c.SignalWorkflow(ctx, target, "", SigGateDecision, GateDecisionSignal{
		Gate:    in.Gate,
		Approve: in.Body.Approve,
		Note:    in.Body.Note,
	}); err != nil {
		return nil, huma.Error404NotFound("workflow not running")
	}
	return &struct{}{}, nil
}

// temporal returns the connected client or a 503 while it is unavailable.
func (s *HumaService) temporal() (client.Client, error) {
	c, err := s.rt.Client()
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("temporal_unavailable")
	}
	return c, nil
}

// DevWorkflowID builds the dev workflow id devflow-<org>-<project>-<tag>[-<suffix>].
// The tag (the spec version) scopes it per build; the run suffix (base36 epoch,
// see newRunSuffix) gives each re-run of the same tag its own distinct id and
// Temporal timeline. An empty suffix keeps the legacy format. The suffix is
// threaded to task children (DevFlowInput.RunSuffix) so a dev run and its tasks
// share one lineage.
func DevWorkflowID(orgID, projectID, tag, suffix string) string {
	if suffix == "" {
		return fmt.Sprintf("devflow-%s-%s-%s", orgID, projectID, tag)
	}
	return fmt.Sprintf("devflow-%s-%s-%s-%s", orgID, projectID, tag, suffix)
}

// newRunSuffix returns a compact, unique-per-run id: the base36 encoding of the
// instant's epoch-millis. Charset [0-9a-z] is Temporal- and DNS-label-safe.
// Millisecond resolution makes a collision between two re-runs of the same
// (org, project, tag) effectively impossible. Computed in the API handler (never
// inside workflow code, which must stay deterministic) and passed down as input.
func newRunSuffix(t time.Time) string {
	return strconv.FormatInt(t.UnixMilli(), 36)
}
