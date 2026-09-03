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

package build

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ErrEndUserAuthConflict, ErrUnknownResourceType, and ErrResourceCatalogUnavailable
// are the build-local pre-tag sentinels the handler maps to 409 / 409 / 503.
// build cannot import the design feature (arch allowlist), so the DesignFactDeriver
// adapter wired at the composition root translates design's equivalents into these
// before they cross the port boundary.
var (
	ErrEndUserAuthConflict        = errors.New("end-user auth conflict")
	ErrResourceCatalogUnavailable = errors.New("resource-type catalog unavailable")
	ErrUnknownResourceType        = errors.New("resourceType is not installed on this cluster")
)

// SpecCollector fetches/accepts an external dependency's OpenAPI contract and
// commits it (clearing the needs-spec proceed-gate) — the same CollectSpec the
// design feature exposes. Satisfied by the app-root design adapter.
type SpecCollector interface {
	CollectSpec(ctx context.Context, orgID, projectID, component, depName string, rawSpec []byte, specURL string) (string, error)
}

// DesignFactDeriver runs the end-user-auth and catalog-membership derivations
// against the design at HEAD and commits any stamped exposesAPI.auth BEFORE the
// tag-cut. The adapter maps design's conflict / unknown-type / catalog sentinels
// onto ErrEndUserAuthConflict / ErrUnknownResourceType /
// ErrResourceCatalogUnavailable.
type DesignFactDeriver interface {
	DerivePlatformResourceFactsAtHead(ctx context.Context, orgID, projectID string) error
}

// RepoLookup resolves a project's "owner/name" repo full name. Satisfied by
// the app-root repoFullNameLookup adapter.
type RepoLookup interface {
	RepoFullName(ctx context.Context, orgID, projectID string) (string, error)
}

// SpecTagger runs the whole-spec hard gate and cuts the next `v<N>` tag
// (spec.SaveSpec). Implementations MUST preserve error identity — the
// handler unwraps *spec.SpecValidationError into the 422 detail.
type SpecTagger interface {
	TagSpec(ctx context.Context, orgID, projectID string) (*spec.SpecSaveResult, error)
	// BuildScopeAtTag reads the tag's story scope (#369): the milestone the
	// claim mints is the version's.
	BuildScopeAtTag(ctx context.Context, orgID, projectID, tag string) (spec.BuildScope, error)
}

// --- the milestone plan path's ports ------------------------------------------
//
// Everything the build click does AFTER the tag is cut — supersede the previous
// version, mint this version's milestone, plan its Tasks into it, mint its
// gates, admit the run row, start the supervisor — reaches out through the five
// ports below. They are consumer ports over services this sub-package may not
// name: a sibling (`task`, the planner), another domain (`sourcecontrol`,
// `dependencies/provisioning`), the domain root's repository, and the run
// supervisor that does not exist yet.

// MilestoneClient is the GitHub MILESTONE surface the plan path drives: mint
// the version's milestone (idempotently), read the PREVIOUS version's open
// issues, and close the milestone itself. sourcecontrol.IssueService satisfies
// it.
//
// Closing those issues is not here: an issue write is a write like any other
// and goes through delivery.IssueWriter. A milestone is not an issue, which is
// why the milestone verbs stay.
type MilestoneClient interface {
	// CreateMilestone mints the version's milestone and returns its NUMBER — the
	// platform key. It is idempotent: a title that already exists (case-
	// insensitively) returns the existing number rather than failing.
	CreateMilestone(ctx context.Context, orgID, projectID string, req sourcecontrol.CreateMilestoneRequest) (*sourcecontrol.MilestoneResult, error)
	// CloseMilestone closes a milestone. Display only — its issues are untouched
	// and it still accepts new ones, which is why superseding closes the issues
	// explicitly rather than relying on this.
	CloseMilestone(ctx context.Context, orgID, projectID string, number int) error
	// ReopenMilestone reopens the milestone this claim resolved onto. A rebuild of
	// an UNCHANGED spec works the same version again, and the milestone it works
	// may have been closed — by a cancel that abandoned the increment, or by the
	// delivered run before it. A version being worked under a heading that says
	// "closed" is a lie the console renders, so the claim re-opens it.
	ReopenMilestone(ctx context.Context, orgID, projectID string, number int) error
	// ListMilestoneIssues reads a milestone's issues by state and label; pull
	// requests are excluded by the host.
	ListMilestoneIssues(ctx context.Context, orgID, projectID string, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error)
}

// MilestoneRunStore is the run-row surface the plan path needs: the pre-check
// behind the endpoint's 409, and the admission that arms the DB-level mutex.
// delivery.MilestoneRunRepository satisfies it.
type MilestoneRunStore interface {
	// ActiveDevRunByProject returns the project's live dev run, or (nil, nil)
	// when it is free. This is the read behind a clean 409; the partial unique
	// index TryAdmit hits is the authority under concurrency.
	ActiveDevRunByProject(ctx context.Context, orgID, projectID string) (*delivery.MilestoneRun, error)
	// ActiveValidationRunByProject returns a live validation run anywhere in the
	// project, or (nil, nil). It is the build's second refusal, and unlike the
	// first it has no index behind it — a validation run sits outside the build
	// mutex, so this read is the rule itself.
	ActiveValidationRunByProject(ctx context.Context, orgID, projectID string) (*delivery.MilestoneRun, error)
	// TryAdmit inserts the run unless the mutex says another one is live.
	TryAdmit(ctx context.Context, run *delivery.MilestoneRun) (admitted bool, row *delivery.MilestoneRun, err error)
	// Settle ends a run — the plan path's own error handler, so a planning turn
	// that dies does not wedge the project behind the mutex it armed.
	Settle(ctx context.Context, id, state, terminalReason string) (*delivery.MilestoneRun, error)
	// ListByProject returns the project's runs newest-first: how the plan path
	// finds the PREVIOUS milestone to supersede. Never by title match — GitHub
	// titles are renamable, so the run rows are the only sound index.
	ListByProject(ctx context.Context, orgID, projectID string) ([]delivery.MilestoneRun, error)
}

// SpecPlanner runs the version's planning turn, minting one prose issue per
// planned Task straight into the milestone (assignment rides creation, so the
// plan costs 1+N calls). Satisfied by *task.PlanService at the composition root
// — build names no sibling, exactly as it reaches the task reads through
// TaskReader.
//
// It BLOCKS for the length of an LLM turn, which is why the plan path runs it
// detached from the HTTP request.
type SpecPlanner interface {
	PlanIntoMilestone(ctx context.Context, orgID, projectID string, milestoneNumber int) error
}

// GateResolver authors the version's dependencies and mints the `provision`
// gate issues into its milestone. Gates are never agent work: they hold the
// next dispatch until the platform (drawer submission, readiness watcher)
// resolves them. Satisfied by an app-root adapter over the provisioning
// service.
type GateResolver interface {
	ProvisionForBuild(ctx context.Context, orgID, projectID, tag string, milestoneNumber int, inputs []delivery.ProvisionInput) error
}

// RunStarter hands the admitted run to the supervisor. It is an interface, not
// a call into the supervisor package, because that is what keeps the build path
// free of a workflow engine — and because the supervisor lands in a later
// increment behind the same seam the event plane already uses.
type RunStarter interface {
	StartRun(ctx context.Context, req delivery.StartRunRequest) error
}
