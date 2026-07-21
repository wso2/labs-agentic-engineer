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

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ErrTemporalUnavailable is the runner's "cannot start/observe workflows right
// now" — mapped to a 503 at the edge, BEFORE any tag is cut.
var ErrTemporalUnavailable = errors.New("temporal unavailable")

// ErrEndUserAuthConflict and ErrResourceCatalogUnavailable are the build-local
// pre-tag sentinels the handler maps to 409 / 503. build cannot import the
// design feature (arch allowlist), so the AuthDeriver adapter wired at the
// composition root translates design's equivalents into these before they cross
// the port boundary.
var (
	ErrEndUserAuthConflict        = errors.New("end-user auth conflict")
	ErrResourceCatalogUnavailable = errors.New("resource-type catalog unavailable")
)

// SpecCollector fetches/accepts an external dependency's OpenAPI contract and
// commits it (clearing the needs-spec proceed-gate) — the same CollectSpec the
// design feature exposes. Satisfied by the app-root design adapter.
type SpecCollector interface {
	CollectSpec(ctx context.Context, orgID, projectID, component, depName string, rawSpec []byte, specURL string) (string, error)
}

// AuthDeriver runs the end-user-auth derivation against the design at HEAD and
// commits any stamped exposesAPI.auth BEFORE the tag-cut. The adapter maps
// design's conflict/catalog sentinels onto ErrEndUserAuthConflict /
// ErrResourceCatalogUnavailable.
type AuthDeriver interface {
	DeriveEndUserAuthAtHead(ctx context.Context, orgID, projectID string) error
}

// SecretStager writes an external dependency's secret values to SM-API and
// returns the reference per env (NOT the value). Satisfied by the app-root
// adapter over resources.ExternalResourceProvisioner.StageSecrets.
type SecretStager interface {
	StageExternalSecrets(ctx context.Context, orgID, ocOrgID, projectID, depName string, secretsByEnv map[string]map[string]string) (refByEnv map[string]string, err error)
}

// RunStore is the workflow_runs surface the build endpoints need: the
// one-dev-run-per-project guard, the org fence for status reads, and the
// synchronous row record on start (so a GET issued right after the POST
// returns never races the workflow's own RecordWorkflowRun activity — both
// upsert the same (workflowID, runID) row). Satisfied by
// delivery.WorkflowRunRepository.
type RunStore interface {
	RunningDevByProject(ctx context.Context, orgID, projectID string) (*delivery.DevflowRun, error)
	GetByWorkflowID(ctx context.Context, orgID, workflowID string) (*delivery.DevflowRun, error)
	Record(ctx context.Context, row *delivery.DevflowRun) error
	// ListByProject enumerates a project's run rows newest-first, optionally
	// filtered to one kind — the builds-history read behind list-project-builds.
	ListByProject(ctx context.Context, orgID, projectID, kind string) ([]delivery.DevflowRun, error)
}

// UsageReader is the per-spec-tag execution-usage rollup (#245) behind the build
// summaries' cost figures. Satisfied by delivery.ExecutionRepository.
type UsageReader interface {
	SumUsageBySpecTag(ctx context.Context, orgID, projectID string) (map[string]contracts.TokenUsage, error)
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
}

// WorkflowRunner starts and observes dev workflows. The real implementation
// wraps the devflow Temporal runtime; tests fake it.
type WorkflowRunner interface {
	// Ready reports whether workflows can be started right now
	// (ErrTemporalUnavailable while the Temporal client is down) — probed
	// BEFORE the tag is cut so an unstartable build never claims a version.
	Ready() error
	// StartBuild starts the dev workflow (start-and-return) and reports the
	// accepted execution's run id.
	StartBuild(ctx context.Context, workflowID string, in delivery.DevFlowInput) (runID string, err error)
	BuildStatus(ctx context.Context, workflowID string) (delivery.DevFlowStatus, error)
}

// TaskReader is the DURABLE task source behind a build's task list: the live
// GitHub ⋈ executions read (the same one behind GET /tasks), scoped by the build
// to its own lineage tag via the aep:spec/<tag> label. It survives an archived
// Temporal run — the workflow query only refines in-flight status on top of it.
// Returns implementation Tasks only: the aep:validation Task is excluded at the
// read-model boundary, matching the build tally that never counts it.
// Satisfied by *task.Reads (the taskflow sub-package), wired at the composition
// root; build names only the root DTO delivery.TaskView, never the sibling.
type TaskReader interface {
	ListByTag(ctx context.Context, orgID, projectID, state, tag string) ([]delivery.TaskView, error)
}
