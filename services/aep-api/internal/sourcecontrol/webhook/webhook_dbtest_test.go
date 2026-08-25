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

package webhook

// DBTEST tier (skips under -short; the DB lane runs it): the SQL-shaped webhook
// behavior against a pristine per-test Postgres (dbtest.New). Two centerpieces:
//
//   - sourcecontrol.DeliveryStore dedup — the PK-on-delivery_id "at-most-once" guarantee that
//     is inherently SQL-shaped (a unique-violation on the second INSERT is what
//     distinguishes a fresh delivery from a replay). Cannot be faked.
//   - The App-installation lifecycle handlers over a REAL organization.CredentialService
//     + REAL TaskRepository — status flips, selected-repo merges, and the
//     installation.deleted → disconnect cascade that abandons the org's in-flight
//     tasks. The cascade's org scoping and terminal-cause stamping are proven
//     against real rows.
//
// SEAM (honest tier-fit): the reach-reconciliation cascade in
// handleReposRemoved gates task abandonment behind ListInstallationRepos, which
// mints an App installation token against a HARD-CODED api.github.com URL (no
// injectable transport on AppTokenMinter.httpClient). That confirm leg is
// therefore un-interceptable in-process → integration-owned. We prove the
// reachable halves here: Phase A (selected-repo merge) runs, and the safety
// property that a FAILED GitHub confirmation skips the cascade (tasks untouched).
// The positive abandon-projection it would run is the same org+project-scoped
// ApplyTaskEvent proven by handleDeleted below + task_dbtest_test.go +
// orgcreds/org_disconnect_dbtest_test.go.

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ============================================================================
// sourcecontrol.DeliveryStore — PK-on-delivery_id dedup
// ============================================================================

func TestDeliveryStore_Persist_FirstDeliveryIsCreated(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	res, err := store.Persist(ctx, "delivery-1", "org-acme", "push", "", []byte(`{"ref":"refs/heads/main"}`))
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if !res.Created || res.AlreadyProcessed {
		t.Fatalf("first delivery must be Created (not AlreadyProcessed), got %+v", res)
	}

	// The dedup row AND the split payload row are both written.
	var deliv sourcecontrol.WebhookDelivery
	if err := db.Where("delivery_id = ?", "delivery-1").First(&deliv).Error; err != nil {
		t.Fatalf("delivery row must exist: %v", err)
	}
	if deliv.OcOrgID != "org-acme" || deliv.Event != "push" || deliv.ProcessedAt != nil {
		t.Fatalf("delivery row shape wrong: %+v", deliv)
	}
	var payload sourcecontrol.WebhookPayload
	if err := db.Where("delivery_id = ?", "delivery-1").First(&payload).Error; err != nil {
		t.Fatalf("payload row must exist alongside the delivery: %v", err)
	}
	// jsonb canonicalizes formatting on write, so compare JSON semantics, not
	// bytes (the model documents "Handlers re-parse on read").
	var got, want map[string]any
	if err := json.Unmarshal(payload.Payload, &got); err != nil {
		t.Fatalf("persisted payload is not valid JSON: %v (%s)", err, payload.Payload)
	}
	if err := json.Unmarshal([]byte(`{"ref":"refs/heads/main"}`), &want); err != nil {
		t.Fatalf("unmarshal expectation: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload not persisted (JSON-equal): got %s", payload.Payload)
	}
}

func TestDeliveryStore_Persist_DuplicateBeforeProcessingReRuns(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "dup-1", "org-acme", "push", "", []byte(`{}`)); err != nil {
		t.Fatalf("first Persist: %v", err)
	}
	// Same delivery_id, processed_at still NULL (handler hasn't finished): the PK
	// unique-violation is caught and reported as neither Created nor
	// AlreadyProcessed → the receiver RE-RUNS the (idempotent) handler.
	res, err := store.Persist(ctx, "dup-1", "org-acme", "push", "", []byte(`{}`))
	if err != nil {
		t.Fatalf("duplicate Persist must not error, got %v", err)
	}
	if res.Created || res.AlreadyProcessed {
		t.Fatalf("un-processed duplicate must be (Created=false, AlreadyProcessed=false), got %+v", res)
	}
}

func TestDeliveryStore_Persist_DuplicateAfterProcessingIsDeduped(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "done-1", "org-acme", "pull_request", "closed", []byte(`{}`)); err != nil {
		t.Fatalf("first Persist: %v", err)
	}
	if err := store.MarkProcessed(ctx, "done-1"); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}
	// Replay of finished work: the existing row has processed_at set → dedup.
	// This is the assertion that catches a broken PK-dedup (a mutation that lets
	// the second INSERT succeed would re-run the handler → double-process).
	res, err := store.Persist(ctx, "done-1", "org-acme", "pull_request", "closed", []byte(`{}`))
	if err != nil {
		t.Fatalf("replay Persist: %v", err)
	}
	if !res.AlreadyProcessed {
		t.Fatalf("a replay of processed work must be AlreadyProcessed, got %+v", res)
	}
}

func TestDeliveryStore_MarkProcessed_ClearsErrorAndStampsTime(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "mp-1", "org-acme", "push", "", []byte(`{}`)); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkFailed(ctx, "mp-1", "boom"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	if err := store.MarkProcessed(ctx, "mp-1"); err != nil {
		t.Fatalf("MarkProcessed: %v", err)
	}

	var row sourcecontrol.WebhookDelivery
	if err := db.Where("delivery_id = ?", "mp-1").First(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.ProcessedAt == nil {
		t.Fatal("MarkProcessed must stamp processed_at")
	}
	if row.ProcessError != "" {
		t.Fatalf("MarkProcessed must clear a prior process_error, got %q", row.ProcessError)
	}
}

func TestDeliveryStore_MarkFailed_RecordsErrorLeavesUnprocessed(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "mf-1", "org-acme", "push", "", []byte(`{}`)); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if err := store.MarkFailed(ctx, "mf-1", "image push denied"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}

	var row sourcecontrol.WebhookDelivery
	if err := db.Where("delivery_id = ?", "mf-1").First(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	// process_error recorded for audit; processed_at stays NULL so a GitHub
	// redelivery re-enters the handler.
	if row.ProcessError != "image push denied" {
		t.Fatalf("process_error = %q; want the recorded message", row.ProcessError)
	}
	if row.ProcessedAt != nil {
		t.Fatal("a failed delivery must remain unprocessed (processed_at NULL)")
	}
}

func TestDeliveryStore_Persist_EmptyDeliveryIDRejected(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	ctx := context.Background()
	store := sourcecontrol.NewDeliveryStore(db)

	if _, err := store.Persist(ctx, "", "org-acme", "push", "", []byte(`{}`)); err == nil {
		t.Fatal("an empty delivery id must be rejected (it is the dedup PK)")
	}
}

// ============================================================================
// App-installation lifecycle handlers — real CredentialService + TaskRepository
// ============================================================================

// fakeIssueSvc records CommentIssue calls; the rest of the IssueService surface
// is not reached by these cascades and panics if called.
type fakeIssueSvc struct {
	comments []string
}

func (f *fakeIssueSvc) CommentIssue(_ context.Context, _ string, _ string, _ int, body string) error {
	f.comments = append(f.comments, body)
	return nil
}
func (f *fakeIssueSvc) CreateIssue(context.Context, string, string, sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	panic("fakeIssueSvc: CreateIssue not expected")
}
func (f *fakeIssueSvc) ListIssues(context.Context, string, string, []string) ([]sourcecontrol.IssueInfo, error) {
	panic("fakeIssueSvc: ListIssues not expected")
}
func (f *fakeIssueSvc) GetIssue(context.Context, string, string, int) (*sourcecontrol.IssueInfo, error) {
	panic("fakeIssueSvc: GetIssue not expected")
}
func (f *fakeIssueSvc) CloseIssue(context.Context, string, string, int, string) error {
	panic("fakeIssueSvc: CloseIssue not expected")
}
func (f *fakeIssueSvc) ReopenIssue(context.Context, string, string, int) error {
	panic("fakeIssueSvc: ReopenIssue not expected")
}
func (f *fakeIssueSvc) EditIssueBody(context.Context, string, string, int, string) error {
	panic("fakeIssueSvc: EditIssueBody not expected")
}
func (f *fakeIssueSvc) EditIssueTitle(context.Context, string, string, int, string) error {
	panic("fakeIssueSvc: EditIssueTitle not expected")
}
func (f *fakeIssueSvc) SetIssueMilestone(context.Context, string, string, int, int) error {
	panic("fakeIssueSvc: SetIssueMilestone not expected")
}
func (f *fakeIssueSvc) AddLabels(context.Context, string, string, int, []string) error {
	panic("fakeIssueSvc: AddLabels not expected")
}
func (f *fakeIssueSvc) RemoveLabel(context.Context, string, string, int, string) error {
	panic("fakeIssueSvc: RemoveLabel not expected")
}
func (f *fakeIssueSvc) SetLabels(context.Context, string, string, int, []string) error {
	panic("fakeIssueSvc: SetLabels not expected")
}
func (f *fakeIssueSvc) GetPullRequestState(context.Context, string, string, int) (*sourcecontrol.PullRequestState, error) {
	panic("fakeIssueSvc: GetPullRequestState not expected")
}
func (f *fakeIssueSvc) MergePullRequest(context.Context, string, string, int) error {
	panic("fakeIssueSvc: MergePullRequest not expected")
}
func (f *fakeIssueSvc) ListPullRequestFiles(context.Context, string, string, int) ([]string, error) {
	panic("fakeIssueSvc: ListPullRequestFiles not expected")
}
func (f *fakeIssueSvc) CreateMilestone(context.Context, string, string, sourcecontrol.CreateMilestoneRequest) (*sourcecontrol.MilestoneResult, error) {
	panic("fakeIssueSvc: CreateMilestone not expected")
}
func (f *fakeIssueSvc) ReopenMilestone(context.Context, string, string, int) error {
	return nil
}

func (f *fakeIssueSvc) CloseMilestone(context.Context, string, string, int) error {
	panic("fakeIssueSvc: CloseMilestone not expected")
}
func (f *fakeIssueSvc) ListMilestones(context.Context, string, string, string) ([]sourcecontrol.Milestone, error) {
	panic("fakeIssueSvc: ListMilestones not expected")
}
func (f *fakeIssueSvc) ListMilestoneIssues(context.Context, string, string, sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	panic("fakeIssueSvc: ListMilestoneIssues not expected")
}
func (f *fakeIssueSvc) MilestoneIssueCounts(context.Context, string, string, int) (*sourcecontrol.MilestoneIssueCounts, error) {
	panic("fakeIssueSvc: MilestoneIssueCounts not expected")
}

func (f *fakeIssueSvc) ListMilestoneIssueComments(context.Context, string, string, int, int) (map[int][]sourcecontrol.IssueComment, error) {
	panic("fakeIssueSvc: ListMilestoneIssueComments not expected")
}

// credAESKey is a fixed 32-byte AES-256 key for the real credential store. The
// tested handler paths never read/write it (app-installation Disconnect GCs only
// user-pat store keys), but wiring the real store keeps the CredentialService
// exactly as production constructs it.
const credAESKey = "0123456789abcdef0123456789abcdef"

// newInstallCredSvc builds a real CredentialService over the dbtest DB in
// no-App mode (nil key material): the DB-only routing/status/merge methods the
// installation handlers use run for real; the GitHub-touching mint path returns
// ErrAppNotConfigured (never nil-derefs), which is exactly the un-interceptable
// confirm leg documented above.
func newInstallCredSvc(t *testing.T, db *gorm.DB) *organization.CredentialService {
	t.Helper()
	store, err := secrets.NewDBStore(db, []byte(credAESKey))
	if err != nil {
		t.Fatalf("NewDBStore: %v", err)
	}
	minter, err := secrets.NewAppTokenMinter(nil)
	if err != nil {
		t.Fatalf("NewAppTokenMinter: %v", err)
	}
	return organization.NewCredentialService(organization.NewOrgCredentialRepository(db, nil), store, minter, "", "", "", nil)
}

// insertAppRow inserts an app-installation org_credentials row directly,
// satisfying the schema CHECKs (installation_id NOT NULL, webhook_secrets NULL).
func insertAppRow(t *testing.T, db *gorm.DB, ocOrgID string, installID int64, status string, selected []string) {
	t.Helper()
	id := installID
	row := organization.OrgCredential{
		OcOrgID:        ocOrgID,
		Kind:           "app-installation",
		GitHubLogin:    ocOrgID + "-org",
		IdentityName:   "AEP Bot",
		IdentityEmail:  "bot@aep.dev",
		IdentityLogin:  "aep[bot]",
		InstallationID: &id,
		SelectedRepos:  organization.JSONStringList(selected),
		Status:         status,
		ConnectedAt:    time.Now().UTC(),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("insert app row %s: %v", ocOrgID, err)
	}
}

func loadCred(t *testing.T, db *gorm.DB, ocOrgID string) organization.OrgCredential {
	t.Helper()
	var row organization.OrgCredential
	if err := db.Where("oc_org_id = ?", ocOrgID).First(&row).Error; err != nil {
		t.Fatalf("load cred %s: %v", ocOrgID, err)
	}
	return row
}

// dispatch wires the real installation handlers on a fresh Router and dispatches
// one event through it — exactly the receiver's dispatch leg.
func dispatchInstall(t *testing.T, db *gorm.DB, credSvc *organization.CredentialService, issues sourcecontrol.IssueService, event, payload string) error {
	t.Helper()
	router := NewRouter()
	RegisterInstallationHandlers(router, credSvc, issues, nil)
	return router.Dispatch(context.Background(), event, []byte(payload))
}

func TestInstall_Suspend_Unsuspend_FlipStatus(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	credSvc := newInstallCredSvc(t, db)
	insertAppRow(t, db, "acme", 999, "active", nil)

	if err := dispatchInstall(t, db, credSvc, &fakeIssueSvc{}, "installation", `{"action":"suspend","installation":{"id":999}}`); err != nil {
		t.Fatalf("suspend dispatch: %v", err)
	}
	if got := loadCred(t, db, "acme").Status; got != "suspended" {
		t.Fatalf("installation.suspend must flip status to suspended, got %q", got)
	}

	if err := dispatchInstall(t, db, credSvc, &fakeIssueSvc{}, "installation", `{"action":"unsuspend","installation":{"id":999}}`); err != nil {
		t.Fatalf("unsuspend dispatch: %v", err)
	}
	if got := loadCred(t, db, "acme").Status; got != "active" {
		t.Fatalf("installation.unsuspend must flip status back to active, got %q", got)
	}
}

func TestInstall_ReposAdded_MergesSelectedRepos(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	credSvc := newInstallCredSvc(t, db)
	insertAppRow(t, db, "acme", 888, "active", []string{"acme/web"})

	err := dispatchInstall(t, db, credSvc, &fakeIssueSvc{}, "installation_repositories",
		`{"action":"added","installation":{"id":888},"repositories_added":[{"full_name":"acme/new"}]}`)
	if err != nil {
		t.Fatalf("added dispatch: %v", err)
	}

	got := map[string]bool{}
	for _, r := range loadCred(t, db, "acme").SelectedRepos {
		got[r] = true
	}
	if !got["acme/web"] || !got["acme/new"] {
		t.Fatalf("added repo must be merged into selected_repos, got %v", got)
	}
}

func TestInstall_ReposRemoved_PhaseAMergesSelectedRepos(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	credSvc := newInstallCredSvc(t, db)
	insertAppRow(t, db, "acme", 777, "active", []string{"acme/web", "acme/api"})

	if err := sourcecontrol.NewRepoRepository(db).Create(context.Background(), &sourcecontrol.GitRepository{
		OrgID: "acme", ProjectID: "web", Status: "ready", RepoURL: "https://github.com/acme/web.git",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	err := dispatchInstall(t, db, credSvc, &fakeIssueSvc{}, "installation_repositories",
		`{"action":"removed","installation":{"id":777},"repositories_removed":[{"full_name":"acme/web"}]}`)
	if err != nil {
		t.Fatalf("removed dispatch: %v", err)
	}

	// Phase A ran: acme/web is gone from selected_repos. (Tasks are GitHub issues
	// now — there is no task-abandonment cascade to assert.)
	for _, r := range loadCred(t, db, "acme").SelectedRepos {
		if r == "acme/web" {
			t.Fatal("Phase A must remove acme/web from selected_repos")
		}
	}
}

func TestInstall_Deleted_FinalizesDisconnect(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	credSvc := newInstallCredSvc(t, db)
	insertAppRow(t, db, "acme", 555, "active", nil)

	issues := &fakeIssueSvc{}
	if err := dispatchInstall(t, db, credSvc, issues, "installation", `{"action":"deleted","installation":{"id":555}}`); err != nil {
		t.Fatalf("deleted dispatch: %v", err)
	}
	// The org's credential row is finalized to disconnected (Phase D). Tasks are
	// GitHub issues now (no cascade); severing the credential makes them inert.
	if got := loadCred(t, db, "acme").Status; got != "disconnected" {
		t.Fatalf("installation.deleted must finalize the row to disconnected, got %q", got)
	}
}

func TestInstall_Deleted_NoMatchingOrgAcksNoop(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	credSvc := newInstallCredSvc(t, db)
	// No org bound to installation 12345 → the handler resolves NotFound and
	// acks noop (returns nil) rather than erroring.
	err := dispatchInstall(t, db, credSvc, &fakeIssueSvc{}, "installation", `{"action":"deleted","installation":{"id":12345}}`)
	if err != nil {
		t.Fatalf("installation.deleted for an unknown install must ack noop, got %v", err)
	}
}
