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
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/aestudiotest"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// Unregister is Register run backwards, and the property that matters most is
// PRECISION: it deletes the hook whose id the platform persisted at
// registration, never a hook it found by scanning. Repositories carry other
// integrations' webhooks, and a project delete has no business touching them.
//
// Everything else about it is total. A project with no repo row or no stored
// hook has nothing of ours installed, and a hook GitHub has already dropped is
// the post-state we wanted anyway (the pod answers it as success).

// registerHook drives a real registration so the hook id is persisted the way
// production persists it — the test then unregisters what registration stored,
// rather than a number the test made up.
func registerHook(t *testing.T, wh sourcecontrol.WebhookService) int64 {
	t.Helper()
	id, err := wh.Register(context.Background(), "org1", "proj1")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return *id
}

func TestWebhookUnregister_DeletesTheStoredHook(t *testing.T) {
	t.Parallel()
	wh, repo, f := newWebhookSvcOnFake(t)
	id := registerHook(t, wh)
	if got := storedWebhookID(t, repo, "org1", "proj1"); got == nil || *got != id {
		t.Fatalf("precondition: stored hook id = %v, want %d", got, id)
	}
	// Another integration's hook on the same repository.
	other := f.SeedHook(widgets, []string{"push"})

	if err := wh.Unregister(context.Background(), "org1", "proj1"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	hooks := f.HookEvents(widgets)
	if _, ok := hooks[id]; ok {
		t.Errorf("the stored hook %d survived: %v", id, hooks)
	}
	if _, ok := hooks[other]; !ok {
		t.Errorf("another integration's hook %d was removed: %v", other, hooks)
	}
}

func TestWebhookUnregister_IsIdempotent(t *testing.T) {
	t.Parallel()
	wh, _, _ := newWebhookSvcOnFake(t)
	registerHook(t, wh)

	// First call removes it; every call after that finds it gone.
	for attempt := 1; attempt <= 2; attempt++ {
		if err := wh.Unregister(context.Background(), "org1", "proj1"); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
}

// TestWebhookUnregister_NothingRegisteredIsANoOp: no hook id was ever persisted,
// so there is nothing of ours on the repo. It must not reach the pod at all —
// guessing which hook was "probably" ours is what this design refuses to do.
func TestWebhookUnregister_NothingRegisteredIsANoOp(t *testing.T) {
	t.Parallel()
	wh, _, f := newWebhookSvcOnFake(t)

	if err := wh.Unregister(context.Background(), "org1", "proj1"); err != nil {
		t.Fatalf("no stored hook must be a no-op, got %v", err)
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("Unregister made %d pod calls with no hook stored, want 0", n)
	}
}

// TestWebhookUnregister_UnknownProjectIsANoOp: the repo row is already gone, so
// the platform cannot name a hook. Nothing to do, and not an error — the project
// teardown re-runs this path.
func TestWebhookUnregister_UnknownProjectIsANoOp(t *testing.T) {
	t.Parallel()
	wh, _, _ := newWebhookSvcOnFake(t)

	if err := wh.Unregister(context.Background(), "org1", "no-such-project"); err != nil {
		t.Fatalf("an unresolvable project must be a no-op, got %v", err)
	}
}

// TestWebhookUnregister_PodFailureIsReported: only a live failure to reach the
// pod or GitHub is an error. The project teardown swallows it (see
// TestDeleteProject_WebhookUnregisterFailureIsSwallowed) — but it has to be
// told, or the log line naming the leftover hook could never be written.
func TestWebhookUnregister_PodFailureIsReported(t *testing.T) {
	t.Parallel()
	wh, _, f := newWebhookSvcOnFake(t)
	registerHook(t, wh)
	boom := &sourcecontrol.HTTPStatusError{StatusCode: 500, Body: "boom"}
	f.FailOp(aestudiotest.OpDeleteWebhook, boom)

	if err := wh.Unregister(context.Background(), "org1", "proj1"); !errors.As(err, new(*sourcecontrol.HTTPStatusError)) {
		t.Fatalf("a 500 from GitHub must be reported, got %v", err)
	}
}

// UnregisterOrg removes the hook of every project repository the org has
// (disconnect): one that fails does not stop the others, the failures
// are reported together, and the ids stay on the rows (ForgetOrg clears
// them once the pod is gone).
func TestWebhookUnregisterOrg_RemovesEveryHookAndReportsFailures(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	repo.preload(
		&sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "a", RepoURL: "https://github.com/acme/a", Status: "ready"},
		&sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "b", RepoURL: "https://github.com/acme/b", Status: "ready"},
		&sourcecontrol.GitRepository{OrgID: "org1", ProjectID: sourcecontrol.SkillsRepoSentinelProjectID, RepoURL: "https://github.com/acme/skills", Status: "ready"},
		&sourcecontrol.GitRepository{OrgID: "org2", ProjectID: "c", RepoURL: "https://github.com/other/c", Status: "ready"},
	)
	f := aestudiotest.New()
	repoSvc := sourcecontrol.NewRepoService(repo, f, f, fakeOwners{owner: "acme"}, "public")
	wh := sourcecontrol.NewWebhookService(repo, f, repoSvc)
	for _, p := range []struct{ org, project string }{{"org1", "a"}, {"org1", "b"}, {"org2", "c"}} {
		if _, err := wh.Register(context.Background(), p.org, p.project); err != nil {
			t.Fatalf("Register %s/%s: %v", p.org, p.project, err)
		}
	}
	a := sourcecontrol.RepoRef{Org: "org1", Owner: "acme", Repo: "a", DefaultBranch: "main"}
	c := sourcecontrol.RepoRef{Org: "org2", Owner: "other", Repo: "c", DefaultBranch: "main"}

	if err := wh.UnregisterOrg(context.Background(), "org1"); err != nil {
		t.Fatalf("UnregisterOrg: %v", err)
	}
	if hooks := f.HookEvents(a); len(hooks) != 0 {
		t.Fatalf("org1/a hooks = %v, want none", hooks)
	}
	if hooks := f.HookEvents(c); len(hooks) != 1 {
		t.Fatalf("another org's hook was removed: %v", hooks)
	}

	// A pod that fails is reported, and every row is still tried.
	f.FailOp(aestudiotest.OpDeleteWebhook, sourcecontrol.ErrAEStudioUnavailable)
	err := wh.UnregisterOrg(context.Background(), "org1")
	if !errors.Is(err, sourcecontrol.ErrAEStudioUnavailable) {
		t.Fatalf("UnregisterOrg err = %v, want the pod's failure reported", err)
	}
	deletes := 0
	for _, call := range f.Calls() {
		if call.Op == aestudiotest.OpDeleteWebhook && call.Ref.Org == "org1" {
			deletes++
		}
	}
	if deletes != 4 {
		t.Fatalf("delete-webhook calls for org1 = %d, want 4 (2 + both rows retried)", deletes)
	}
}

// ForgetOrg clears every hook id of the org and no other org's, so a
// reconnect's hook repair installs a hook for each row whether or not the
// disconnect reached GitHub.
func TestWebhookForgetOrg_ClearsOnlyThatOrgsIDs(t *testing.T) {
	t.Parallel()
	repo := newFakeRepoRepo()
	one, two := int64(1), int64(2)
	repo.preload(
		&sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "a", RepoURL: "https://github.com/acme/a", Status: "ready", WebhookID: &one},
		&sourcecontrol.GitRepository{OrgID: "org2", ProjectID: "c", RepoURL: "https://github.com/other/c", Status: "ready", WebhookID: &two},
	)
	f := aestudiotest.New()
	wh := sourcecontrol.NewWebhookService(repo, f, sourcecontrol.NewRepoService(repo, f, f, fakeOwners{owner: "acme"}, "public"))
	if err := wh.ForgetOrg(context.Background(), "org1"); err != nil {
		t.Fatalf("ForgetOrg: %v", err)
	}
	if got := storedWebhookID(t, repo, "org1", "a"); got != nil {
		t.Fatalf("org1/a keeps hook id %d", *got)
	}
	if got := storedWebhookID(t, repo, "org2", "c"); got == nil {
		t.Fatal("another org's hook id was cleared")
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("ForgetOrg made %d pod calls, want 0", n)
	}
}

// A row whose project's delete has started takes no hook (the sweep's hook
// repair must never install one for a project being deleted).
func TestWebhookRegister_RefusesARowBeingDeleted(t *testing.T) {
	t.Parallel()
	wh, repo, f := newWebhookSvcOnFake(t)
	if _, err := repo.SetStatusIf(context.Background(), "org1", "proj1", "ready", sourcecontrol.RepoStatusDeleting); err != nil {
		t.Fatal(err)
	}
	if _, err := wh.Register(context.Background(), "org1", "proj1"); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("Register on a deleting row: %v, want ErrRepoNotFound", err)
	}
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("Register reached the pod %d times for a deleting row", n)
	}
}

// goneOnPersist is a row the project delete marks (or drops) while the hook
// is being installed: the id cannot be stored.
type goneOnPersist struct{ *fakeRepoRepo }

func (goneOnPersist) SetWebhookIDIfReady(context.Context, string, string, int64) (bool, error) {
	return false, nil
}

// The delete window: a registration whose row is marked or dropped between
// the hook's install and the id's store removes the hook it installed, so no
// hook outlives the row that names it.
func TestWebhookRegister_RowGoneMidRegisterRemovesTheHook(t *testing.T) {
	t.Parallel()
	base := newFakeRepoRepo()
	base.preload(&sourcecontrol.GitRepository{OrgID: "org1", ProjectID: "proj1", RepoURL: "https://github.com/acme/widgets", Status: "ready"})
	repo := goneOnPersist{base}
	f := aestudiotest.New()
	wh := sourcecontrol.NewWebhookService(repo, f, sourcecontrol.NewRepoService(repo, f, f, fakeOwners{owner: "acme"}, "public"))

	if _, err := wh.Register(context.Background(), "org1", "proj1"); !errors.Is(err, sourcecontrol.ErrRepoNotFound) {
		t.Fatalf("Register err = %v, want ErrRepoNotFound", err)
	}
	if hooks := f.HookEvents(widgets); len(hooks) != 0 {
		t.Fatalf("hooks = %v, want the installed one removed", hooks)
	}
}
