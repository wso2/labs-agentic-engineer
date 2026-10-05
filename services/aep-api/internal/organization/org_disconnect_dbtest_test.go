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
	"slices"
	"testing"

	"gorm.io/gorm"

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
	credSvc := newCredSvcDB(t, db, gh)
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
	credSvc := newCredSvcDB(t, db, gh)
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
	credSvc := newCredSvcDB(t, db, gh)
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

// disconnectSteps records the 06 §9 steps with the credential status each
// one saw, proving all ran before Phase D.
type disconnectSteps struct {
	t      *testing.T
	db     *gorm.DB
	order  []string
	errs   map[string]error
	holder bool // Remove called and Release not yet
}

func (d *disconnectSteps) step(name string) error {
	d.order = append(d.order, name+":"+getRow(d.t, d.db, "acme").Status)
	return d.errs[name]
}

func (d *disconnectSteps) UnregisterOrg(context.Context, string) error { return d.step("hooks") }
func (d *disconnectSteps) ForgetOrg(context.Context, string) error     { return d.step("forget") }
func (d *disconnectSteps) Remove(context.Context, string) error {
	d.holder = true
	return d.step("studio")
}
func (d *disconnectSteps) Release(string) {
	d.holder = false
	d.order = append(d.order, "release")
}
func (d *disconnectSteps) removeSecrets(context.Context, string) error { return d.step("secrets") }

func newDisconnectSteps(t *testing.T, db *gorm.DB, errs map[string]error) *disconnectSteps {
	return &disconnectSteps{t: t, db: db, errs: errs}
}

func connectedAcme(t *testing.T) (*gorm.DB, *organization.CredentialService) {
	t.Helper()
	db := dbtest.New(t)
	gh := patHappyGitHub(t, "ada", "Ada", "ada@x.io")
	credSvc := newCredSvcDB(t, db, gh)
	if _, err := credSvc.Connect(context.Background(), "acme", organization.ConnectRequest{Kind: "user-pat", PAT: "ghp", GitHubLogin: "ada"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	return db, credSvc
}

// 06 §9 gitpat disconnect, in order, all before Phase D: the hooks (best
// effort, so a failure goes on), the AE Studio Resource (held until the
// cascade ends), the gitpat rows and references, the hook ids.
func TestDisconnect_UnregistersHooksFirst(t *testing.T) {
	t.Parallel()
	db, credSvc := connectedAcme(t)
	steps := newDisconnectSteps(t, db, map[string]error{"hooks": errors.New("pod restarting")})
	svc := organization.NewOrgDisconnectService(credSvc, nil).
		WithRepoHooks(steps).WithStudioRemover(steps).WithGitHubSecretsRemover(steps.removeSecrets)

	if err := svc.Disconnect(context.Background(), "acme", "manual.disconnect"); err != nil {
		t.Fatalf("disconnect: a failed hook unregister is best effort, got %v", err)
	}
	want := []string{"hooks:active", "studio:active", "secrets:active", "forget:active", "release"}
	if !slices.Equal(steps.order, want) {
		t.Fatalf("order = %v, want %v", steps.order, want)
	}
	if row := getRow(t, db, "acme"); row.Status != "disconnected" {
		t.Fatalf("credential row status = %q, want disconnected", row.Status)
	}
}

// A step after the hooks that fails stops the cascade before Phase D (the
// credential stays, a retry repeats it) and still releases the studio hold.
func TestDisconnect_AFailedStepStopsBeforePhaseD(t *testing.T) {
	t.Parallel()
	for _, failing := range []string{"studio", "secrets", "forget"} {
		t.Run(failing, func(t *testing.T) {
			t.Parallel()
			db, credSvc := connectedAcme(t)
			boom := errors.New(failing + " failed")
			steps := newDisconnectSteps(t, db, map[string]error{failing: boom})
			svc := organization.NewOrgDisconnectService(credSvc, nil).
				WithRepoHooks(steps).WithStudioRemover(steps).WithGitHubSecretsRemover(steps.removeSecrets)

			if err := svc.Disconnect(context.Background(), "acme", "manual.disconnect"); !errors.Is(err, boom) {
				t.Fatalf("disconnect err = %v, want %v", err, boom)
			}
			if row := getRow(t, db, "acme"); row.Status != "active" {
				t.Fatalf("credential row status = %q, want active (Phase D not run)", row.Status)
			}
			if steps.holder {
				t.Fatal("the studio hold outlived the cascade")
			}
			// The retry repeats the whole cascade and finishes it.
			steps.errs = nil
			if err := svc.Disconnect(context.Background(), "acme", "manual.disconnect"); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if row := getRow(t, db, "acme"); row.Status != "disconnected" {
				t.Fatalf("after the retry: status %q, want disconnected", row.Status)
			}
		})
	}
}
