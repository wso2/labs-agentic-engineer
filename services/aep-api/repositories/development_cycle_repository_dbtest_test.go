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

package repositories_test

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
)

func newCycle(org, project, reqVer, workflowID string) *models.DevelopmentCycle {
	return &models.DevelopmentCycle{
		OrgID:              org,
		ProjectID:          project,
		RequirementVersion: reqVer,
		WorkflowID:         workflowID,
	}
}

// TestDevelopmentCycleRepository_EnsureIsIdempotent is the load-bearing R1
// property: Ensure INSERTs on first call and, keyed by the unique WorkflowID,
// returns the SAME row (no duplicate) on a retried start trigger.
func TestDevelopmentCycleRepository_EnsureIsIdempotent(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := repositories.NewDevelopmentCycleRepository(db)
	ctx := context.Background()

	first, err := repo.Ensure(ctx, newCycle("orga", "web", "v1", "devflow:orga:web:cycle-1"))
	if err != nil {
		t.Fatalf("Ensure (first): %v", err)
	}
	if first == nil || first.ID == "" {
		t.Fatalf("first Ensure returned no row: %+v", first)
	}
	if first.Status != string(models.CycleActive) {
		t.Errorf("status = %q, want %q", first.Status, models.CycleActive)
	}

	// Retried start with the same WorkflowID → same row, no duplicate.
	second, err := repo.Ensure(ctx, newCycle("orga", "web", "v1", "devflow:orga:web:cycle-1"))
	if err != nil {
		t.Fatalf("Ensure (retry): %v", err)
	}
	if second == nil || second.ID != first.ID {
		t.Fatalf("retry Ensure returned a different row: first=%+v second=%+v", first, second)
	}

	cycles, err := repo.ListByProject(ctx, "orga", "web")
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(cycles) != 1 {
		t.Fatalf("expected exactly 1 cycle after idempotent Ensure, got %d", len(cycles))
	}
}

// TestDevelopmentCycleRepository_GetAndSetStatus covers the miss convention
// (nil,nil), org-scoped listing, and the terminal status transition.
func TestDevelopmentCycleRepository_GetAndSetStatus(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := repositories.NewDevelopmentCycleRepository(db)
	ctx := context.Background()

	// Unknown workflow ID misses with (nil, nil) — never ErrRecordNotFound.
	got, err := repo.GetByWorkflowID(ctx, "devflow:nope:nope:nope")
	if err != nil {
		t.Fatalf("GetByWorkflowID (miss): %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for unknown workflow, got %+v", got)
	}

	// SetStatus on an unknown workflow also misses with (nil, nil).
	if row, err := repo.SetStatus(ctx, "devflow:nope:nope:nope", models.CycleCompleted); err != nil || row != nil {
		t.Fatalf("SetStatus (unknown) = (%+v, %v), want (nil, nil)", row, err)
	}

	if _, err := repo.Ensure(ctx, newCycle("orgb", "api", "v2", "devflow:orgb:api:cycle-1")); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// A different org's cycle must not leak into orgb's project listing.
	if _, err := repo.Ensure(ctx, newCycle("orgz", "api", "v9", "devflow:orgz:api:cycle-1")); err != nil {
		t.Fatalf("Ensure (other org): %v", err)
	}
	scoped, err := repo.ListByProject(ctx, "orgb", "api")
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(scoped) != 1 || scoped[0].OrgID != "orgb" {
		t.Fatalf("org-scoped listing leaked: %+v", scoped)
	}

	done, err := repo.SetStatus(ctx, "devflow:orgb:api:cycle-1", models.CycleCompleted)
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if done == nil || done.Status != string(models.CycleCompleted) {
		t.Fatalf("SetStatus result = %+v, want status %q", done, models.CycleCompleted)
	}
	if done.CompletedAt == nil {
		t.Errorf("SetStatus did not stamp completed_at")
	}
}
