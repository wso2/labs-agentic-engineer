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

// DBTEST tier: the OrgDisconnectService cascade under the tasks-github-native
// model, exercised with a REAL CredentialService (honest tier-fit — the service
// takes the concrete *CredentialService, so faking it would prove nothing; ADR-
// 0003 Pilot B) over a real Postgres. The old per-task abandon cascade (Phase
// B/C over component_tasks) is GONE: Tasks are GitHub issues, so disconnect only
// severs the credential (Phase A confirm → Phase D finalize) and leaves the
// platform-owned executions rows untouched — severing the credential makes the
// org's issues inert to the webhook router, which is the disconnect effect.
//
// External test package: credential_service_test.go (unit tier, package
// organization) imports dbtest, which imports migrate, which imports
// organization — an in-package dbtest file would be an import cycle.
// patHappyGitHub/newCredSvcDB/getRow come from credential_dbtest_test.go, same
// converted package.
package organization_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
)

func TestOrgDisconnect_SeversCredential_LeavesExecutions_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()

	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	credSvc, _ := newCredSvcDB(t, db, gh)
	if _, err := credSvc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Seed platform-owned executions rows for the org's Tasks. These must SURVIVE
	// the disconnect (the issues go inert; the rows are not purged — that is the
	// project-delete path, not disconnect).
	execRepo := delivery.NewExecutionRepository(db, nil)
	for _, issue := range []int{7, 8} {
		if _, _, err := execRepo.TryAdmit(ctx, &delivery.Execution{
			OrgID: "acme", ProjectID: "web", Repo: "acme/web", IssueNumber: issue,
			Kind: string(taskmeta.KindCoding),
		}); err != nil {
			t.Fatalf("seed execution #%d: %v", issue, err)
		}
	}

	// issueSvc is nil: the disconnect cascade no longer touches issues (the task
	// abandon cascade that used it is gone).
	svc := organization.NewOrgDisconnectService(credSvc, nil)
	if err := svc.Disconnect(ctx, "acme", "manual.disconnect"); err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	// Phase D: the credential row is finalized to disconnected.
	if row := getRow(t, db, "acme"); row.Status != "disconnected" {
		t.Fatalf("credential row status = %q, want disconnected", row.Status)
	}
	// The executions rows are untouched — disconnect severs credentials, it does
	// not purge platform state.
	for _, issue := range []int{7, 8} {
		if rows, _ := execRepo.ListByIssue(ctx, "acme/web", issue); len(rows) != 1 {
			t.Errorf("execution #%d must survive disconnect, got %d rows", issue, len(rows))
		}
	}
}

func TestOrgDisconnect_UnknownOrg_ReturnsNotFound_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()

	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	credSvc, _ := newCredSvcDB(t, db, gh)
	svc := organization.NewOrgDisconnectService(credSvc, nil)

	// Phase A existence check: no credential row → organization.ErrOrgNotFound (the controller
	// maps it to an idempotent 200).
	if err := svc.Disconnect(ctx, "ghost", "manual.disconnect"); !errors.Is(err, organization.ErrOrgNotFound) {
		t.Fatalf("disconnect unknown org: err = %v, want organization.ErrOrgNotFound", err)
	}
}

func TestOrgDisconnect_AlreadyDisconnected_NoOp_DB(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()

	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	credSvc, _ := newCredSvcDB(t, db, gh)
	if _, err := credSvc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	svc := organization.NewOrgDisconnectService(credSvc, nil)

	if err := svc.Disconnect(ctx, "acme", "manual.disconnect"); err != nil {
		t.Fatalf("first disconnect: %v", err)
	}
	// A second disconnect on an already-finalized row is a clean no-op.
	if err := svc.Disconnect(ctx, "acme", "manual.disconnect"); err != nil {
		t.Fatalf("second disconnect must be idempotent, got %v", err)
	}
	if row := getRow(t, db, "acme"); row.Status != "disconnected" {
		t.Fatalf("credential row status = %q, want disconnected", row.Status)
	}
}

// 06 §9 gitpat disconnect, in order: the repo hooks are unregistered while
// the pod still holds the gitpat (best effort), then the org's AE Studio
// Resource is deleted, then Phase D removes the credential. Each step reads
// the credential row to prove it ran before Phase D.
func TestDisconnect_UnregistersHooksFirst(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()

	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	credSvc, _ := newCredSvcDB(t, db, gh)
	if _, err := credSvc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}

	var order []string
	step := func(name string, err error) func(context.Context, string) error {
		return func(_ context.Context, org string) error {
			if org != "acme" {
				t.Errorf("%s: org %q, want acme", name, org)
			}
			order = append(order, name+":"+getRow(t, db, "acme").Status)
			return err
		}
	}
	svc := organization.NewOrgDisconnectService(credSvc, nil).
		WithHookUnregistrar(step("hooks", errors.New("pod restarting"))).
		WithStudioRemover(step("studio", nil))

	if err := svc.Disconnect(ctx, "acme", "manual.disconnect"); err != nil {
		t.Fatalf("disconnect: a failed hook unregister is best effort, got %v", err)
	}
	want := []string{"hooks:active", "studio:active"}
	if len(order) != len(want) || order[0] != want[0] || order[1] != want[1] {
		t.Fatalf("order = %v, want %v (both before Phase D)", order, want)
	}
	if row := getRow(t, db, "acme"); row.Status != "disconnected" {
		t.Fatalf("credential row status = %q, want disconnected", row.Status)
	}
}

// A Resource that could not be deleted fails the disconnect before Phase D:
// the credential stays, so a retry repeats the cascade rather than leaving a
// pod running with the gitpat of an org that reads as disconnected.
func TestDisconnect_StudioRemoveFailureStopsBeforePhaseD(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()

	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	credSvc, _ := newCredSvcDB(t, db, gh)
	if _, err := credSvc.Connect(ctx, "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	boom := errors.New("openchoreo unreachable")
	svc := organization.NewOrgDisconnectService(credSvc, nil).
		WithStudioRemover(func(context.Context, string) error { return boom })

	if err := svc.Disconnect(ctx, "acme", "manual.disconnect"); !errors.Is(err, boom) {
		t.Fatalf("disconnect err = %v, want the Resource delete failure", err)
	}
	if row := getRow(t, db, "acme"); row.Status != "active" {
		t.Fatalf("credential row status = %q, want active (Phase D not run)", row.Status)
	}
}
