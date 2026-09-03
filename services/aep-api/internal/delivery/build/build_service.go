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
// list-project-builds / get-build-preflight). POST validates the whole spec,
// cuts the single `v<N>` version tag, claims the version (supersede the
// previous milestone, mint this one, admit the run row that is the build
// mutex) and returns the tag — the one button that turns an approved spec into a
// delivery increment. Filling the milestone (gates, then the planning turn) is
// the run workflow's own first phase, not the click's. The list read is the
// version ledger, straight from the run rows.
package build

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"

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

// ErrBuildAlreadyRunning is the "a dev run is already live for this project"
// sentinel the core build sequence returns. The HTTP edge maps it to a 409; the
// non-HTTP StartProjectBuild entry point treats it as success (idempotent
// trigger).
var ErrBuildAlreadyRunning = errors.New("a build is already running for this project")

// ErrValidationRunLive is the "this project's deployed version is being judged"
// sentinel, and the second thing that refuses a build.
//
// It is a REFUSAL rather than a queue because the two activities contradict each
// other: a validation run asserts against what is deployed, and a delivery run
// merging and promoting underneath it would be judging a moving target — the
// verdict would name criteria that were true of neither the old release nor the
// new one. A validation run deliberately sits outside the build mutex (it
// re-judges a version that already shipped, so no index refuses this), which is
// why the guard is explicit here.
//
// The way past it is to cancel the validation, which is one click and already
// supported. Treated as a conflict at the edge, exactly like the sentinel above.
var ErrValidationRunLive = errors.New("this project's deployed version is being validated — cancel the validation run first")

// Service backs the build endpoints (strict entry points in strict.go).
type Service struct {
	repos  RepoLookup
	tagger SpecTagger
	coord  *InputsCoordinator
	design PreflightDesignReader
	// plan is the milestone plan path (milestone_plan.go), wired separately via
	// SetPlanPath because its gate resolver is built after this service. Nil
	// means the build stops at the tag cut.
	plan *planPath
}

// Deps carries the service's ports.
type Deps struct {
	Repos  RepoLookup
	Tagger SpecTagger
	// Coord runs the drawer inputs' pre-tag work + provision-payload assembly.
	// Nil-safe: a build with no coordinator (and no inputs) behaves exactly as
	// before this feature.
	Coord *InputsCoordinator
	// Design backs the build-time dependency hard gate (dependencyGateFailures):
	// the SAME PreflightDesignReader port (and, in production, the same
	// designComponents{store: artifactStore} adapter) PreflightSvc reads for the
	// GET-time drawer items — so the hard gate re-runs spec.ComputeDependencyStatus's
	// already-computed Status/Reason through a genuinely fresh read, never a
	// copy of the client's last preflight fetch. Nil-safe: a build with no
	// design reader wired fails OPEN (no dependency can be classified),
	// mirroring Coord/Tasks.
	Design PreflightDesignReader
}

// NewService wires the build service.
func NewService(d Deps) *Service {
	return &Service{repos: d.Repos, tagger: d.Tagger, coord: d.Coord, design: d.Design}
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
	// external-config request values are ignored. The server derives
	// unset/defaulted values from the design.
	Values []ConfigValue `json:"values,omitempty"`
	// external-spec: the pasted OpenAPI content, or a URL to fetch it from.
	SpecContent string `json:"specContent,omitempty"`
	SpecURL     string `json:"specUrl,omitempty"`
	// platform-resource: the provisioning params (mixed scalar types).
	Parameters map[string]any `json:"parameters,omitempty"`
	// platform-resource / org-service: the user's approval.
	Approved bool `json:"approved,omitempty"`
}

// ConfigValue is the wire-format shape for an external-config value. Build
// processing ignores these entries and derives authoring values from design.
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

// BuildSummary is one entry of list-project-builds: a spec version tag and the
// state of the newest milestone run that has worked it. It is a DB-only row —
// no GitHub, no cluster — which is what lets the console poll the ledger while
// a run is live without spending GitHub rate. Task counts are deliberately
// absent: their only honest source is GitHub, and the console already has them
// from the /tasks response on screen.
type BuildSummary struct {
	Tag string `json:"tag"`
	// MilestoneNumber is the platform key the tag resolves to — the handle the
	// version's run story (list-build-runs) is read by.
	MilestoneNumber int    `json:"milestoneNumber"`
	Status          string `json:"status" enum:"started,in_progress,completed,failed"`
	// Reason is the run's terminal reason for a failed version (empty
	// otherwise), surfaced beside the Failed badge in the console.
	Reason      string     `json:"reason,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	// WaitingReason says WHY an in-progress version is waiting rather than
	// moving (ADR-0023's deploy gate is `external-values`). Empty for the
	// ordinary between-cycles park. The ledger row reads as an agent still
	// working without it; the run row carrying it is already in hand, so this
	// costs the ledger no extra read. The dependency NAMES are deliberately not
	// here — see the contract.
	WaitingReason string `json:"waitingReason,omitempty" enum:"external-values"`
}

// BuildList is the list-project-builds response, newest build first.
type BuildList struct {
	Builds []BuildSummary `json:"builds"`
}

// StartProjectBuild is the non-HTTP entry point that starts a project build with
// NO drawer inputs — the provider-build auto-kick behind provisioning's
// ProviderBuildTrigger (issue #164, Task 4). It reuses the exact build sequence
// (mutex → repo → pre-tag → dependency gate → tag → claim → plan) and
// is idempotent: a build already in flight already satisfies the trigger, so
// ErrBuildAlreadyRunning is treated as success (nil). Any other failure
// propagates so the caller logs it and the reconcile sweep heals later.
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
// context (409 vs. idempotent success).
//
// The dependency hard gate (dependencyGateFailures) runs after the pre-tag
// inputs are applied but before the tag is cut — see the inline comment at
// its call site for why that ordering matters.
func (s *Service) Run(ctx context.Context, orgID, projectID string, inputs []BuildInputItem) (string, []InputFailure, error) {
	// One live DEV RUN per project — the milestone model's mutex (§5). The
	// partial unique index behind TryAdmit is the authority; this read is what
	// turns the race into a conflict that names itself, and it runs BEFORE the
	// tag is cut so a rejected second click claims no version.
	if err := s.activeDevRun(ctx, orgID, projectID); err != nil {
		return "", nil, err
	}
	// And no live VALIDATION run anywhere in the project: a merge and promote
	// under an in-flight verdict judges a moving target. Checked here, beside the
	// mutex and before the tag is cut, so a refused build claims no version.
	if err := s.activeValidationRun(ctx, orgID, projectID); err != nil {
		return "", nil, err
	}
	// The repo must exist and be resolvable before a version is claimed — every
	// GitHub write the plan path makes lands on it.
	if _, err := s.repos.RepoFullName(ctx, orgID, projectID); err != nil {
		return "", nil, &EdgeError{Status: 404, Message: "project repository not found"}
	}

	// Apply the relevant drawer inputs BEFORE the tag-cut: collect external specs + run the
	// design derivations (end-user auth and each resource dependency's wiring —
	// their commits must land on HEAD so the tag captures them), then derive
	// unset/defaulted external authoring and assemble the provision payload. A
	// fail-fast pre-tag failure returns {failures} and cuts NO tag.
	var provInputs []delivery.ProvisionInput
	if s.coord != nil {
		fails, aerr := s.coord.ApplyPreTag(ctx, orgID, projectID, inputs)
		if aerr != nil {
			return "", nil, mapPreTagError(aerr)
		}
		if len(fails) > 0 {
			return "", fails, nil
		}
		prov, pfails, perr := s.coord.BuildProvisionInputs(ctx, orgID, projectID, inputs)
		if perr != nil {
			return "", nil, &EdgeError{Status: 502, Message: "prepare inputs: " + perr.Error()}
		}
		if len(pfails) > 0 {
			return "", pfails, nil
		}
		provInputs = prov
	}

	// The dependency hard gate (dependency-management migration, restoring the
	// gate Task 1 orphaned): a doctored client must not be able to skip the
	// drawer. Runs AFTER ApplyPreTag so a legitimate resolution submitted with
	// THIS build request (e.g. a drawer-pasted external-spec, which ApplyPreTag
	// just committed to HEAD above) is reflected in the fresh read below, but
	// BEFORE the tag-cut so an unresolved external dependency never reaches it.
	gateFailures, gerr := s.dependencyGateFailures(ctx, orgID, projectID)
	if gerr != nil {
		return "", nil, &EdgeError{Status: 500, Message: "check dependency gate: " + gerr.Error()}
	}
	if len(gateFailures) > 0 {
		return "", gateFailures, nil
	}

	// The whole-spec hard gate runs INSIDE TagSpec, before the tag is cut —
	// the returned tag always names a validated requirements+design pair. An
	// unchanged spec returns the existing tag; the workflow still (re)runs.
	res, err := s.tagger.TagSpec(ctx, orgID, projectID)
	if err != nil {
		return "", nil, mapTagError(err)
	}
	// THE SPEC-SAVE STATUS PICKS THE BRANCH:
	//
	//	approved  → a new tag. The ordinary path: supersede the predecessor, mint
	//	            this version's milestone, plan it fresh.
	//	unchanged → the SAME tag, so the same milestone. Reopen it and exactly the
	//	            issues a cancel closed inside it.
	//
	// A version nobody cancelled takes the same branch and reopens nothing, which
	// is correct: rebuilding a delivered version re-derives no plan either way.
	unchanged := res.Status == spec.SpecSaveUnchanged

	// The milestone plan path (§5). Its synchronous half claims the version —
	// supersede the previous milestone, mint `v<N>`, admit the run row that IS
	// the build mutex — and its detached half plans the Tasks into it.
	if s.plan != nil {
		// The tag's story scope (#369) decides the milestone's identity: one
		// milestone per version. A scope read
		// failure degrades to the legacy tag-titled milestone rather than
		// failing a build the gate already validated.
		scope := spec.BuildScope{Tag: res.Tag}
		if sc, serr := s.tagger.BuildScopeAtTag(ctx, orgID, projectID, res.Tag); serr == nil {
			scope = sc
		} else {
			slog.WarnContext(ctx, "build: story scope read failed — using tag-scoped milestone",
				"project", projectID, "tag", res.Tag, "error", serr)
		}
		run, cerr := s.claimVersion(ctx, orgID, projectID, scope)
		if cerr != nil {
			return "", nil, cerr
		}
		// AFTER the admission, so a click the mutex refused reopens nothing: the
		// winner is the run that will work this milestone, and two entrants both
		// reopening it would race one another's writes.
		//
		// WHETHER THE RUN SKIPS PLANNING IS A SECOND QUESTION, and asking it of the
		// milestone rather than of the spec status is the whole point. `rebuild`
		// means "this milestone is already filled, so re-planning it would mint
		// nothing" — and an unchanged spec is NOT evidence of that. A run that died
		// in its planning phase (`plan-failed`, or a cancel that landed before the
		// planning turn) leaves the milestone holding its gates and no work at all;
		// skipping the plan there hands the loop an empty working set, which it
		// reads as "planning produced nothing to work" and settles the version
		// SUCCEEDED having built none of it (see run.onEmptyWorkingSet).
		//
		// So the milestone answers. reopenIncrement already holds its whole issue
		// list, and reports whether any planned work is in it.
		rebuild := false
		if unchanged {
			rebuild = s.reopenIncrement(ctx, orgID, projectID, run.MilestoneNumber)
		}
		// Synchronous, and fast: this hands the version to the supervisor, which
		// then fills the milestone as its own first phase. The click used to run
		// the planning turn itself, in a detached goroutine, because an LLM turn
		// must not hold a request open — and paid for it with a step that could
		// not survive a restart, could not retry a blip, and left no history.
		//
		// A start that did not happen is settled HERE. The row is the build
		// mutex; leaving it non-terminal with no workflow behind it would refuse
		// every later build on this project, and nothing would ever heal it (the
		// reconcile sweep reads such a row as live).
		if serr := s.startRun(ctx, orgID, projectID, res.Tag, run, provInputs, rebuild); serr != nil {
			return "", nil, serr
		}
	}

	slog.InfoContext(ctx, "build started",
		"org", orgID, "project", projectID, "tag", res.Tag, "specStatus", res.Status, "unchanged", unchanged)
	return res.Tag, nil, nil
}

// mapPreTagError maps the coordinator's pre-tag failures onto the edge
// vocabulary: an end-user-auth conflict is a 409 (the design self-contradicts),
// an unknown resourceType is a 409 (unsatisfiable on this cluster), an
// unreachable CRT catalog is a retryable 503, anything else a 500.
func mapPreTagError(err error) error {
	switch {
	case errors.Is(err, ErrEndUserAuthConflict):
		return &EdgeError{Status: 409, Message: err.Error()}
	case errors.Is(err, ErrUnknownResourceType):
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
