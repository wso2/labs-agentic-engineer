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

// Package build is the public build surface (contract: build-project /
// get-project-build). POST validates the whole spec, cuts the single `v<N>`
// version tag, starts the dev workflow asynchronously, and returns the tag —
// the one-button successor to the requirements-save → design-save → devflow
// sequence. GET maps the workflow's live status onto the contract's
// BuildStatus.
package build

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// EdgeError is the neutral transport error the build sequence returns for
// edge-mapped failures; the api layer (handlers_build.go) copies it onto the
// flat envelope. Neutral on purpose: this feature must not depend on an HTTP
// framework. Details carries the spec gate's per-file breakdown
// (field=path, message=CODE: msg).
type EdgeError struct {
	Status  int
	Message string
	Details []gen.ErrorDetail
}

func (e *EdgeError) Error() string { return e.Message }

// ErrBuildAlreadyRunning is the "a dev workflow is already running for this
// project" sentinel the core build sequence returns. The HTTP edge maps it
// to a 409; the non-HTTP StartProjectBuild entry point treats it as success
// (idempotent trigger).
var ErrBuildAlreadyRunning = errors.New("a build is already running for this project")

// Service backs the build endpoints (strict entry points in strict.go).
type Service struct {
	runner WorkflowRunner
	store  RunStore
	repos  RepoLookup
	tagger SpecTagger
	tasks  TaskReader
	coord  *InputsCoordinator
	usage  UsageReader
	pricer *modelcost.Pricer
}

// Deps carries the service's ports.
type Deps struct {
	Runner WorkflowRunner
	Store  RunStore
	Repos  RepoLookup
	Tagger SpecTagger
	Tasks  TaskReader
	// Coord runs the drawer inputs' pre-tag work + provision-payload assembly.
	// Nil-safe: a build with no coordinator (and no inputs) behaves exactly as
	// before this feature.
	Coord *InputsCoordinator
	// Usage + Pricer attach each build summary's captured spend (#245,
	// ADR-0011). Nil-safe: without them the list simply carries no usage.
	Usage  UsageReader
	Pricer *modelcost.Pricer
}

// NewService wires the build service.
func NewService(d Deps) *Service {
	return &Service{runner: d.Runner, store: d.Store, repos: d.Repos, tagger: d.Tagger, tasks: d.Tasks, coord: d.Coord, usage: d.Usage, pricer: d.Pricer}
}

// --- wire shapes (names drive the generated schema names — keep them exactly
// --- BuildRequest / BuildResponse / BuildStatus / BuildStatusTask) ----------

// BuildRequest is the build-project body: the drawer inputs the user supplied
// for the dependencies this build must provision (empty for a build with no
// outstanding dependency inputs).
type BuildRequest struct {
	Inputs []BuildInputItem `json:"inputs,omitempty"`
}

// BuildInputItem is one drawer entry's resolved input — the POST /build mirror
// of the GET-preflight PreflightItem. Exactly the fields relevant to its Kind
// are set.
type BuildInputItem struct {
	Component  string `json:"component" doc:"Owning component name"`
	Dependency string `json:"dependency" doc:"Dependency name"`
	Kind       string `json:"kind" enum:"external-config,external-spec,platform-resource,org-service"`
	// external-config: the collected key/value pairs (secret-vs-nonsecret is
	// decided server-side from the design's ConfigKey.Secret flags — never sent
	// by the client, never logged).
	Values []ConfigValue `json:"values,omitempty"`
	// external-spec: the pasted OpenAPI content, or a URL to fetch it from.
	SpecContent string `json:"specContent,omitempty"`
	SpecURL     string `json:"specUrl,omitempty"`
	// platform-resource: the provisioning params (mixed scalar types).
	Parameters map[string]any `json:"parameters,omitempty"`
	// platform-resource / org-service: the user's approval.
	Approved bool `json:"approved,omitempty"`
}

// ConfigValue is one external-config key/value pair the drawer collected.
type ConfigValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// InputFailure reports one drawer input the build could not apply (e.g. an
// invalid/unfetchable spec) — a fail-fast pre-tag result: the build cuts no tag
// and starts no workflow.
type InputFailure struct {
	Component  string `json:"component,omitempty"`
	Dependency string `json:"dependency"`
	Kind       string `json:"kind,omitempty"`
	Reason     string `json:"reason"`
}

// BuildResponse returns the spec version tag the build runs for, OR the
// per-input failures that blocked it (never both — failures mean no tag).
type BuildResponse struct {
	Tag      string         `json:"tag,omitempty"`
	Failures []InputFailure `json:"failures,omitempty"`
}

// BuildStatusTask is one task's progress inside a build. IssueNumber links the
// row to its GitHub issue (and the task-log stream) — the identity that
// replaced the old title-join.
type BuildStatusTask struct {
	IssueNumber int64  `json:"issueNumber"`
	Title       string `json:"title"`
	Status      string `json:"status" enum:"started,in_progress,completed,failed"`
}

// BuildStatus is the get-project-build response.
type BuildStatus struct {
	Status         string `json:"status" enum:"started,in_progress,completed,failed"`
	WorkflowStatus string `json:"workflow_status"`
	// Reason is the failure detail for a failed build (empty otherwise) — the
	// devflow's recorded error, so the console can show WHY it failed.
	Reason string            `json:"reason,omitempty"`
	Tasks  []BuildStatusTask `json:"tasks,omitempty"`
}

// BuildTally is one build's frozen task counts (the dev run's own tally,
// written by the workflow — not a live task recount).
type BuildTally struct {
	Total  int64 `json:"total"`
	Done   int64 `json:"done"`
	Failed int64 `json:"failed"`
	Active int64 `json:"active"`
}

// BuildSummary is one entry of list-project-builds: the newest run for a spec
// version tag. Status shares get-project-build's vocabulary; a list read has
// no live workflow query, so "started" never occurs here.
type BuildSummary struct {
	Tag    string `json:"tag"`
	Status string `json:"status" enum:"started,in_progress,completed,failed"`
	// Reason is the failure detail for a failed build (empty otherwise) — the
	// devflow's recorded error, surfaced beside the Failed badge in the console.
	Reason      string     `json:"reason,omitempty"`
	Tasks       BuildTally `json:"tasks"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// Usage is the tag lineage's captured execution spend (#245), accruing
	// while the build runs; absent when nothing was captured.
	Usage *delivery.UsageView `json:"usage,omitempty"`
}

// BuildList is the list-project-builds response, newest build first.
type BuildList struct {
	Builds []BuildSummary `json:"builds"`
}

// StartProjectBuild is the non-HTTP entry point that starts a project build with
// NO drawer inputs — the provider-build auto-kick behind provisioning's
// ProviderBuildTrigger (issue #164, Task 4). It reuses the exact build sequence
// (Ready → one-run guard → repo → pre-tag → tag → StartBuild → Record) and is
// idempotent: an already-running dev workflow already satisfies the trigger, so
// ErrBuildAlreadyRunning is treated as success (nil). Any other failure
// propagates so the funnel logs it and the sweep heals later.
func (s *Service) StartProjectBuild(ctx context.Context, orgID, projectID string) error {
	_, failures, err := s.Run(ctx, orgID, projectID, nil)
	if err != nil {
		if errors.Is(err, ErrBuildAlreadyRunning) {
			return nil
		}
		return err
	}
	if len(failures) > 0 {
		// No drawer inputs were supplied, so a pre-tag input failure is not
		// expected here — surface it rather than silently succeeding.
		return fmt.Errorf("start project build: %d input failure(s), first: %s/%s: %s",
			len(failures), failures[0].Component, failures[0].Dependency, failures[0].Reason)
	}
	return nil
}

// Run is the whole build sequence — the strict build-project entry
// (handlers_build.go) and the provider-build trigger share it. It returns the cut tag on success, OR per-input
// failures (tag == "", no error — a fail-fast pre-tag result that cut no tag),
// OR an error. Errors are already edge-mapped *EdgeError values EXCEPT the
// ErrBuildAlreadyRunning sentinel, which each caller interprets for its own
// context (409 vs. idempotent success). NOTE: inputs may carry raw secret
// values — it must never be logged.
func (s *Service) Run(ctx context.Context, orgID, projectID string, inputs []BuildInputItem) (string, []InputFailure, error) {
	// An unstartable workflow must never claim a version tag — probe first.
	if err := s.runner.Ready(); err != nil {
		return "", nil, &EdgeError{Status: 503, Message: "temporal_unavailable"}
	}
	// One dev workflow per project at a time.
	if running, lerr := s.store.RunningDevByProject(ctx, orgID, projectID); lerr != nil {
		return "", nil, &EdgeError{Status: 500, Message: "lookup running build"}
	} else if running != nil {
		return "", nil, ErrBuildAlreadyRunning
	}
	repo, err := s.repos.RepoFullName(ctx, orgID, projectID)
	if err != nil {
		return "", nil, &EdgeError{Status: 404, Message: "project repository not found"}
	}

	// Apply the drawer inputs BEFORE the tag-cut: collect external specs +
	// derive end-user auth (their commits must land on HEAD so the tag captures
	// them), then stage external-config secrets to SM-API and assemble the
	// provision payload. A fail-fast pre-tag failure returns {failures} and cuts
	// NO tag.
	var provInputs []delivery.ProvisionInput
	if s.coord != nil {
		fails, aerr := s.coord.ApplyPreTag(ctx, orgID, projectID, inputs)
		if aerr != nil {
			return "", nil, mapPreTagError(aerr)
		}
		if len(fails) > 0 {
			return "", fails, nil
		}
		prov, pfails, perr := s.coord.BuildProvisionInputs(ctx, orgID, orgID, projectID, inputs)
		if perr != nil {
			return "", nil, &EdgeError{Status: 502, Message: "stage inputs: " + perr.Error()}
		}
		if len(pfails) > 0 {
			return "", pfails, nil
		}
		provInputs = prov
	}

	// The whole-spec hard gate runs INSIDE TagSpec, before the tag is cut —
	// the returned tag always names a validated requirements+design pair. An
	// unchanged spec returns the existing tag; the workflow still (re)runs.
	res, err := s.tagger.TagSpec(ctx, orgID, projectID)
	if err != nil {
		return "", nil, mapTagError(err)
	}

	workflowID := delivery.DevWorkflowID(orgID, projectID, res.Tag)
	runID, err := s.runner.StartBuild(ctx, workflowID, delivery.DevFlowInput{
		OrgID:     orgID,
		ProjectID: projectID,
		Repo:      repo,
		Tag:       res.Tag,
		Gates:     delivery.GateConfig{}, // all gates auto
		Provision: provInputs,
	})
	if err != nil {
		if errors.Is(err, ErrTemporalUnavailable) {
			return "", nil, &EdgeError{Status: 503, Message: "temporal_unavailable"}
		}
		return "", nil, &EdgeError{Status: 500, Message: "start build workflow: " + err.Error()}
	}

	// Record the run row NOW so a status GET issued right after this response
	// never races the workflow's own RecordWorkflowRun activity (both upsert
	// the same (workflowID, runID) row). Best-effort: the activity re-records.
	if err := s.store.Record(ctx, &delivery.DevflowRun{
		WorkflowID: workflowID,
		RunID:      runID,
		Kind:       delivery.WorkflowKindDev,
		OrgID:      orgID,
		ProjectID:  projectID,
		Tag:        res.Tag,
		Repo:       repo,
		Status:     delivery.WorkflowStatusRunning,
	}); err != nil {
		slog.WarnContext(ctx, "build: record workflow run failed (activity will re-record)",
			"workflowId", workflowID, "error", err)
	}

	slog.InfoContext(ctx, "build started",
		"org", orgID, "project", projectID, "tag", res.Tag, "specStatus", res.Status)
	return res.Tag, nil, nil
}

// taskStatuses builds the version's task list, DURABLE-first: the source is the
// lineage-tag read (every task stamped with this build's tag), so an archived
// run still answers with its full list and every row carries its issueNumber.
// The live workflow refs (empty for an archived/failed query) then REFINE the
// status of tasks still in flight. A durable-read hiccup degrades to whatever
// the workflow refs carry (numbered placeholders) — build status must never
// 500 because a GitHub read stumbled.
func (s *Service) taskStatuses(ctx context.Context, orgID, projectID, tag string, refs []delivery.DevTaskRef) []BuildStatusTask {
	byIssue := map[int]*BuildStatusTask{}
	order := make([]int, 0)

	// 1. Durable base: the tasks stamped with this build's lineage tag. The
	// aep:spec/<tag> label scopes the read server-side, so every returned row
	// already belongs to this version.
	if s.tasks != nil {
		views, err := s.tasks.ListByTag(ctx, orgID, projectID, "all", tag)
		if err != nil {
			slog.WarnContext(ctx, "build status: durable task read failed",
				"org", orgID, "project", projectID, "error", err)
		}
		for _, v := range views {
			bt := BuildStatusTask{
				IssueNumber: int64(v.IssueNumber),
				Title:       v.Title,
				Status:      statusFromDerived(v.DerivedStatus),
			}
			byIssue[v.IssueNumber] = &bt
			order = append(order, v.IssueNumber)
		}
	}

	// 2. In-flight refinement: the running workflow's own view of each task
	// wins while it is live; a ref for an issue the durable read has not yet
	// surfaced is appended as a numbered placeholder.
	for _, ref := range refs {
		if bt, ok := byIssue[ref.Issue]; ok {
			bt.Status = taskStatus(ref)
			continue
		}
		bt := BuildStatusTask{
			IssueNumber: int64(ref.Issue),
			Title:       fmt.Sprintf("Task #%d", ref.Issue),
			Status:      taskStatus(ref),
		}
		byIssue[ref.Issue] = &bt
		order = append(order, ref.Issue)
	}

	if len(order) == 0 {
		return nil
	}
	sort.Ints(order)
	out := make([]BuildStatusTask, 0, len(order))
	for _, n := range order {
		out = append(out, *byIssue[n])
	}
	return out
}

// mapPreTagError maps the coordinator's pre-tag failures onto the edge
// vocabulary: an end-user-auth conflict is a 409 (the design self-contradicts),
// an unreachable CRT catalog is a retryable 503, anything else a 500.
func mapPreTagError(err error) error {
	switch {
	case errors.Is(err, ErrEndUserAuthConflict):
		return &EdgeError{Status: 409, Message: err.Error()}
	case errors.Is(err, ErrResourceCatalogUnavailable):
		return &EdgeError{Status: 503, Message: err.Error()}
	default:
		return &EdgeError{Status: 500, Message: "apply build inputs: " + err.Error()}
	}
}

// mapTagError maps SaveSpec failures onto the edge vocabulary: the spec gate
// is a 400 validation failure carrying per-file detail; missing/not-ready
// repos are 404/409.
func mapTagError(err error) error {
	var se *spec.SpecValidationError
	switch {
	case errors.As(err, &se):
		details := make([]gen.ErrorDetail, 0, len(se.Files))
		for _, f := range se.Files {
			details = append(details, gen.ErrorDetail{
				Field:   f.Path,
				Message: f.Code + ": " + f.Message,
			})
		}
		return &EdgeError{Status: 400, Message: "spec validation failed", Details: details}
	case errors.Is(err, sourcecontrol.ErrRepoNotFound):
		return &EdgeError{Status: 404, Message: "project repository not found"}
	case errors.Is(err, sourcecontrol.ErrRepoNotReady):
		return &EdgeError{Status: 409, Message: "project repository is not ready yet"}
	default:
		return &EdgeError{Status: 500, Message: "tag spec: " + err.Error()}
	}
}
