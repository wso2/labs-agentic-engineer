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
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
)

func newExec(org, repo string, issue int, kind taskmeta.ExecutionKind) *models.Execution {
	return &models.Execution{
		OrgID:       org,
		ProjectID:   "proj",
		Repo:        repo,
		IssueNumber: issue,
		Kind:        string(kind),
	}
}

// TestExecutionRepository_AdmissionMutex is the load-bearing §5 property: the
// partial unique index admits exactly one active Execution per
// (repo, issue_number, kind), a different kind is independent, and finishing an
// Execution frees the slot for re-admission (retry).
func TestExecutionRepository_AdmissionMutex(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := repositories.NewExecutionRepository(db)
	ctx := context.Background()

	// Two concurrent admits for the SAME (repo, issue, kind) → exactly one wins.
	const n = 2
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, row, err := repo.TryAdmit(ctx, newExec("orga", "acme/repo", 42, taskmeta.KindCoding))
			if err != nil {
				t.Errorf("TryAdmit: %v", err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if ok {
				admitted++
				if row == nil || row.ID == "" {
					t.Errorf("admitted row missing id: %+v", row)
				}
			} else if row != nil {
				t.Errorf("non-admitted TryAdmit returned a row: %+v", row)
			}
		}()
	}
	wg.Wait()
	if admitted != 1 {
		t.Fatalf("admitted = %d; want exactly 1 (admission mutex breached)", admitted)
	}

	// A different KIND for the same issue is independent — admitted.
	ok, _, err := repo.TryAdmit(ctx, newExec("orga", "acme/repo", 42, taskmeta.KindBuild))
	if err != nil || !ok {
		t.Fatalf("different-kind admit = (%v, %v); want admitted", ok, err)
	}

	// A third coding admit while the first is still active → rejected.
	ok, _, err = repo.TryAdmit(ctx, newExec("orga", "acme/repo", 42, taskmeta.KindCoding))
	if err != nil {
		t.Fatalf("TryAdmit(third): %v", err)
	}
	if ok {
		t.Fatalf("second active coding Execution should be rejected by the mutex")
	}

	// Finish the active coding Execution, then a fresh admit succeeds (retry).
	active := findActiveCoding(t, repo, "acme/repo", 42)
	if _, err := repo.Finish(ctx, active.ID, string(taskmeta.ExecFailed), "boom"); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	ok, _, err = repo.TryAdmit(ctx, newExec("orga", "acme/repo", 42, taskmeta.KindCoding))
	if err != nil || !ok {
		t.Fatalf("re-admit after finish = (%v, %v); want admitted", ok, err)
	}
}

func findActiveCoding(t *testing.T, repo repositories.ExecutionRepository, r string, issue int) *models.Execution {
	t.Helper()
	m, err := repo.LatestPerKind(context.Background(), r, issue)
	if err != nil {
		t.Fatalf("LatestPerKind: %v", err)
	}
	e := m[string(taskmeta.KindCoding)]
	if e == nil {
		t.Fatalf("no coding execution found")
	}
	return e
}

// TestExecutionRepository_StartFinish covers the guarded transitions.
func TestExecutionRepository_StartFinish(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := repositories.NewExecutionRepository(db)
	ctx := context.Background()

	_, row, err := repo.TryAdmit(ctx, newExec("orga", "acme/repo", 1, taskmeta.KindCoding))
	if err != nil || row == nil {
		t.Fatalf("TryAdmit: %v / %v", row, err)
	}

	started, err := repo.StartWithRun(ctx, row.ID, "run-1")
	if err != nil || started == nil {
		t.Fatalf("StartWithRun: %v / %v", started, err)
	}
	if started.Status != string(taskmeta.ExecRunning) || started.StartedAt == nil || started.RunName != "run-1" {
		t.Fatalf("StartWithRun did not mark running/started_at/run_name: %+v", started)
	}

	// A second start is a no-op (not queued anymore) → (nil, nil).
	if again, err := repo.StartWithRun(ctx, row.ID, "run-2"); err != nil || again != nil {
		t.Fatalf("double StartWithRun = (%v, %v); want (nil, nil)", again, err)
	}

	finished, err := repo.Finish(ctx, row.ID, string(taskmeta.ExecSucceeded), "")
	if err != nil || finished == nil {
		t.Fatalf("Finish: %v / %v", finished, err)
	}
	if finished.Status != string(taskmeta.ExecSucceeded) || finished.EndedAt == nil {
		t.Fatalf("Finish did not mark succeeded/ended_at: %+v", finished)
	}

	// Finishing an already-terminal row is a guarded no-op → (nil, nil).
	if again, err := repo.Finish(ctx, row.ID, string(taskmeta.ExecFailed), "late"); err != nil || again != nil {
		t.Fatalf("double Finish = (%v, %v); want (nil, nil)", again, err)
	}
}

// TestExecutionRepository_Reads covers LatestPerKind, ListByIssue, ListActive
// and the org fence.
func TestExecutionRepository_Reads(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := repositories.NewExecutionRepository(db)
	ctx := context.Background()

	// Two coding attempts (first failed, then a retry) + a build, all on one Task.
	_, first, _ := repo.TryAdmit(ctx, newExec("orga", "acme/repo", 5, taskmeta.KindCoding))
	if _, err := repo.Finish(ctx, first.ID, string(taskmeta.ExecFailed), "x"); err != nil {
		t.Fatalf("finish first: %v", err)
	}
	_, retry, _ := repo.TryAdmit(ctx, newExec("orga", "acme/repo", 5, taskmeta.KindCoding))
	_, build, _ := repo.TryAdmit(ctx, newExec("orga", "acme/repo", 5, taskmeta.KindBuild))
	// A decoy under a DIFFERENT org on the SAME repo/issue but a different kind
	// (so it does not collide with the org-agnostic admission mutex). The scoped
	// reads must fence it out; ListActive (unscoped) must include it.
	if ok, _, err := repo.TryAdmit(ctx, newExec("orgb", "acme/repo", 5, taskmeta.KindOps)); err != nil || !ok {
		t.Fatalf("decoy admit failed: ok=%v err=%v", ok, err)
	}

	// LatestPerKind returns the retry (not the earlier failed attempt) for coding,
	// the build for build, and never the other org's ops decoy (fence).
	m, err := repo.LatestPerKindScoped(ctx, "orga", "acme/repo", 5)
	if err != nil {
		t.Fatalf("LatestPerKindScoped: %v", err)
	}
	if got := m[string(taskmeta.KindCoding)]; got == nil || got.ID != retry.ID {
		t.Fatalf("latest coding = %+v; want the retry %s", got, retry.ID)
	}
	if got := m[string(taskmeta.KindBuild)]; got == nil || got.ID != build.ID {
		t.Fatalf("latest build = %+v; want %s", got, build.ID)
	}
	if _, leaked := m[string(taskmeta.KindOps)]; leaked {
		t.Fatalf("LatestPerKindScoped leaked the other org's ops decoy")
	}

	// ListByIssueScoped returns all of this org's rows for the Task (3), oldest
	// first, and excludes the other org's decoy.
	rows, err := repo.ListByIssueScoped(ctx, "orga", "acme/repo", 5)
	if err != nil {
		t.Fatalf("ListByIssueScoped: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("ListByIssueScoped len = %d; want 3 (org fence leaked?)", len(rows))
	}

	// ListActive spans orgs; the finished first attempt is excluded, its retry +
	// build + the orgb decoy remain (3 active).
	active, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 3 {
		t.Fatalf("ListActive len = %d; want 3", len(active))
	}

	// GetByIDScoped fences by org.
	if got, _ := repo.GetByIDScoped(ctx, "orga", retry.ID); got == nil {
		t.Fatalf("GetByIDScoped(owner) returned nil")
	}
	if got, _ := repo.GetByIDScoped(ctx, "orgb", retry.ID); got != nil {
		t.Fatalf("GetByIDScoped(other org) leaked a row: %+v", got)
	}

	// LatestPerKindForRepo(Scoped) is the batch form: same latest-per-kind rows
	// keyed by issue number, one query for the whole repo, org fence honored.
	batch, err := repo.LatestPerKindForRepoScoped(ctx, "orga", "acme/repo")
	if err != nil {
		t.Fatalf("LatestPerKindForRepoScoped: %v", err)
	}
	byKind := batch[5]
	if byKind == nil {
		t.Fatalf("batch missing issue 5: %+v", batch)
	}
	if got := byKind[string(taskmeta.KindCoding)]; got == nil || got.ID != retry.ID {
		t.Fatalf("batch latest coding = %+v; want the retry %s", got, retry.ID)
	}
	if got := byKind[string(taskmeta.KindBuild)]; got == nil || got.ID != build.ID {
		t.Fatalf("batch latest build = %+v; want %s", got, build.ID)
	}
	if _, leaked := byKind[string(taskmeta.KindOps)]; leaked {
		t.Fatalf("LatestPerKindForRepoScoped leaked the other org's ops decoy")
	}
	// The unscoped form spans orgs — the decoy appears.
	all, err := repo.LatestPerKindForRepo(ctx, "acme/repo")
	if err != nil {
		t.Fatalf("LatestPerKindForRepo: %v", err)
	}
	if all[5] == nil || all[5][string(taskmeta.KindOps)] == nil {
		t.Fatalf("LatestPerKindForRepo should include the orgb ops row: %+v", all[5])
	}
}

// TestExecutionRepository_UpsertReadModel covers the §R3.3 read-model path:
// one row per WorkflowID, idempotent on repeat writes (Version bumps, no
// duplicate row), and disjoint from the admission-mutex path (a WorkflowID-
// less TryAdmit row never collides with a read-model upsert for the same
// repo/issue).
func TestExecutionRepository_UpsertReadModel(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := repositories.NewExecutionRepository(db)
	ctx := context.Background()

	wfID := "task:orga:proj1:comp-a"
	row := newExec("orga", "acme/repo", 7, taskmeta.KindCoding)
	row.WorkflowID = wfID
	row.Status = string(taskmeta.ExecRunning)

	first, err := repo.UpsertReadModel(ctx, row)
	if err != nil || first == nil {
		t.Fatalf("UpsertReadModel(first): row=%v err=%v", first, err)
	}
	if first.Version != 1 {
		t.Fatalf("first upsert Version = %d; want 1", first.Version)
	}

	// A second upsert for the same WorkflowID updates in place — no new row,
	// Version bumps.
	again := newExec("orga", "acme/repo", 7, taskmeta.KindBuild)
	again.WorkflowID = wfID
	again.Status = string(taskmeta.ExecSucceeded)
	again.RunName = "build-42"
	second, err := repo.UpsertReadModel(ctx, again)
	if err != nil || second == nil {
		t.Fatalf("UpsertReadModel(second): row=%v err=%v", second, err)
	}
	if second.ID != first.ID {
		t.Fatalf("second upsert created a new row: first=%s second=%s", first.ID, second.ID)
	}
	if second.Version != 2 {
		t.Fatalf("second upsert Version = %d; want 2", second.Version)
	}
	if second.Status != string(taskmeta.ExecSucceeded) || second.RunName != "build-42" {
		t.Fatalf("second upsert did not update status/run_name: %+v", second)
	}

	got, err := repo.GetByWorkflowID(ctx, wfID)
	if err != nil || got == nil || got.ID != first.ID {
		t.Fatalf("GetByWorkflowID: row=%v err=%v", got, err)
	}

	if _, err := repo.GetByWorkflowID(ctx, "no-such-workflow"); err != nil {
		t.Fatalf("GetByWorkflowID(missing) should not error: %v", err)
	} else if got, _ := repo.GetByWorkflowID(ctx, "no-such-workflow"); got != nil {
		t.Fatalf("GetByWorkflowID(missing) = %+v; want nil", got)
	}

	// Empty WorkflowID is rejected — it would collide with every other
	// WorkflowID-less row were it not for the partial index excluding them.
	if _, err := repo.UpsertReadModel(ctx, newExec("orga", "acme/repo", 8, taskmeta.KindCoding)); err == nil {
		t.Fatalf("UpsertReadModel with empty WorkflowID should error")
	}

	// A plain TryAdmit row (no WorkflowID) for the same repo/issue coexists
	// fine — the two partial indexes are disjoint.
	if ok, _, err := repo.TryAdmit(ctx, newExec("orga", "acme/repo", 7, taskmeta.KindCoding)); err != nil || !ok {
		t.Fatalf("TryAdmit alongside a read-model row: ok=%v err=%v", ok, err)
	}
}
