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

package sourcecontrol_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// newWebhookSvcOnFake wires a REAL webhookService with a REAL repoService (so
// SetWebhookID's lookup+persist body actually executes) at the in-memory pod.
// Everything shares ONE fake repo store, whose record is how tests assert
// persistence.
func newWebhookSvcOnFake(t *testing.T) (sourcecontrol.WebhookService, *fakeRepoRepo, *aestudiotest.Fake) {
	t.Helper()
	repo := newFakeRepoRepo()
	repo.preload(&sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/acme/widgets"})
	f := aestudiotest.New()
	repoSvc := sourcecontrol.NewRepoService(repo, f, f, fakeOwners{owner: "acme"}, "public")
	return sourcecontrol.NewWebhookService(repo, f, repoSvc), repo, f
}

// storedWebhookID reads the persisted WebhookID off the shared fake repo record.
func storedWebhookID(t *testing.T, repo *fakeRepoRepo, org, proj string) *int64 {
	t.Helper()
	rec, err := repo.GetByOrgAndProjectID(context.Background(), org, proj)
	if err != nil || rec == nil {
		t.Fatalf("repo record %s/%s: rec=%v err=%v", org, proj, rec, err)
	}
	return rec.WebhookID
}

// The hook asks the org's pod for the four project events (the pod owns the
// delivery URL and the signing secret) and persists the hook id.
func TestWebhookRegister_HappyPathRegistersTheEventsAndPersistsID(t *testing.T) {
	t.Parallel()
	wh, repo, f := newWebhookSvcOnFake(t)

	hookID, err := wh.Register(testContext(), "org1", "proj1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	hooks := f.HookEvents(widgets)
	if hookID == nil || len(hooks) != 1 {
		t.Fatalf("hookID = %v, hooks = %v, want one hook", hookID, hooks)
	}
	if got := hooks[*hookID]; !slices.Equal(got, []string{"pull_request", "push", "issue_comment", "issues"}) {
		t.Fatalf("events = %v, want [pull_request push issue_comment issues]", got)
	}
	// Persistence asserted through the REAL repoService.SetWebhookID body:
	// lookup + pointer-set + repo.Update all executed against the shared store.
	if got := storedWebhookID(t, repo, "org1", "proj1"); got == nil || *got != *hookID {
		t.Fatalf("persisted WebhookID = %v, want %d", got, *hookID)
	}
}

// RegisterWebhook answers an existing hook as is, without touching its events,
// so every register reconciles the event list (§9.2 cutover).
func TestWebhookRegister_ReconcilesTheEventsOnTheHook(t *testing.T) {
	t.Parallel()
	wh, _, f := newWebhookSvcOnFake(t)

	if _, err := wh.Register(testContext(), "org1", "proj1"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if want := []string{aestudiotest.OpRegisterWebhook, aestudiotest.OpUpdateWebhookEvents}; !slices.Equal(ops(f), want) {
		t.Fatalf("ops = %v, want %v", ops(f), want)
	}
}

// Registering again answers the same hook (the pod ensures it) and
// reconciles its events.
func TestWebhookRegister_IsIdempotent(t *testing.T) {
	t.Parallel()
	wh, repo, f := newWebhookSvcOnFake(t)
	first, err := wh.Register(testContext(), "org1", "proj1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := wh.Register(testContext(), "org1", "proj1")
	if err != nil || *second != *first {
		t.Fatalf("second register = (%v, %v), want hook %d", second, err, *first)
	}
	if hooks := f.HookEvents(widgets); len(hooks) != 1 {
		t.Fatalf("hooks = %v, want one", hooks)
	}
	if got := storedWebhookID(t, repo, "org1", "proj1"); got == nil || *got != *first {
		t.Fatalf("persisted WebhookID = %v, want %d", got, *first)
	}
}

// A failed reconcile does not undo a registration that succeeded.
func TestWebhookRegister_ReconcileFailureIsNotFatal(t *testing.T) {
	t.Parallel()
	wh, repo, f := newWebhookSvcOnFake(t)
	f.FailOp(aestudiotest.OpUpdateWebhookEvents, sourcecontrol.ErrAEStudioUnavailable)

	hookID, err := wh.Register(testContext(), "org1", "proj1")
	if err != nil || hookID == nil {
		t.Fatalf("Register = (%v, %v), want the hook", hookID, err)
	}
	if got := storedWebhookID(t, repo, "org1", "proj1"); got == nil || *got != *hookID {
		t.Fatalf("persisted WebhookID = %v, want %d", got, *hookID)
	}
}

// The pod being down fails the registration loudly and persists nothing.
func TestWebhookRegister_PodFailureIsReported(t *testing.T) {
	t.Parallel()
	wh, repo, f := newWebhookSvcOnFake(t)
	f.FailOrg("org1", sourcecontrol.ErrAEStudioUnavailable)

	if _, err := wh.Register(testContext(), "org1", "proj1"); !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("err = %v, want ErrAEStudioUnavailable", err)
	}
	if got := storedWebhookID(t, repo, "org1", "proj1"); got != nil {
		t.Fatalf("WebhookID = %v after a failed register, want unset", *got)
	}
}

func TestWebhookRegister_UnknownProjectIsRepoNotFound(t *testing.T) {
	t.Parallel()
	wh, _, f := newWebhookSvcOnFake(t)

	if _, err := wh.Register(testContext(), "org1", "no-such-project"); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("err = %v, want ErrRepoNotFound", err)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("the pod was called %d times, want 0", n)
	}
}
