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

//go:build dbtest

package repositories_test

import (
	"context"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/dbtest"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/repositories"
)

// TestTaskRepository_GetByIDScoped_OrgIsolation guards against a by-UUID
// cross-org task IDOR: the operator task routes are path-scoped (orgHandle +
// taskId), so loading by UUID alone would let org A pass org B's {taskId} and
// operate on B's task. GetByIDScoped loads by (org_id, id) and returns
// (nil, nil) for both "no such task" and "task belongs to another org" — no
// existence leak. This test confirms the scoping against real Postgres (no mock).
func TestTaskRepository_GetByIDScoped_OrgIsolation(t *testing.T) {
	db := dbtest.Open(t)
	dbtest.Migrate(t, db, &models.ComponentTask{})
	dbtest.CleanupRows(t, db, &models.ComponentTask{}, "org_id LIKE ?", "dbtest-%")

	repo := repositories.NewTaskRepository(db)
	ctx := context.Background()

	mk := func(org, proj, component string) *models.ComponentTask {
		t.Helper()
		task := &models.ComponentTask{
			OrgID:         org,
			ProjectID:     proj,
			ComponentName: component,
			Status:        string(models.TaskStatusPending),
		}
		if err := repo.Create(ctx, task); err != nil {
			t.Fatalf("create (%s,%s,%s): %v", org, proj, component, err)
		}
		if task.ID == "" {
			t.Fatalf("create (%s,%s,%s): expected generated id", org, proj, component)
		}
		return task
	}
	// Same project slug under two different orgs is fine — tasks are isolated by org.
	taskA := mk("dbtest-orga", "dbtest-shared-proj", "svc-a")
	mk("dbtest-orgb", "dbtest-shared-proj", "svc-b")

	// Owner org resolves its own task.
	got, err := repo.GetByIDScoped(ctx, "dbtest-orga", taskA.ID)
	if err != nil {
		t.Fatalf("GetByIDScoped(orga, taskA): %v", err)
	}
	if got == nil || got.ID != taskA.ID || got.OrgID != "dbtest-orga" {
		t.Fatalf("expected taskA for owner org, got %+v", got)
	}

	// A different org asking for org-a's task id resolves to (nil, nil) — the
	// IDOR-closing behaviour. No existence leak: same result as a bogus id.
	cross, err := repo.GetByIDScoped(ctx, "dbtest-orgb", taskA.ID)
	if err != nil {
		t.Fatalf("GetByIDScoped(orgb, taskA): %v", err)
	}
	if cross != nil {
		t.Fatalf("cross-org lookup leaked a task: %+v", cross)
	}

	// Unknown id under the owner org is also (nil, nil) — indistinguishable
	// from the cross-org case.
	missing, err := repo.GetByIDScoped(ctx, "dbtest-orga", "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetByIDScoped(orga, nonexistent): %v", err)
	}
	if missing != nil {
		t.Fatalf("nonexistent id returned a task: %+v", missing)
	}
}
