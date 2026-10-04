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
