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

package task

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// fakeCycleStarter records OnIssueTaskOpened calls and can be told to fail.
type fakeCycleStarter struct {
	calls []int // issue numbers
	err   error
}

func (f *fakeCycleStarter) OnIssueTaskOpened(_ context.Context, orgHandle, projectName string, issueNumber int) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, issueNumber)
	return nil
}

func taskIssuePayload(action string, number int, body string) []byte {
	p := map[string]any{
		"action": action,
		"issue": map[string]any{
			"number": number,
			"state":  "open",
			"title":  "some task",
			"body":   body,
			"labels": []map[string]string{{"name": "aep:task"}},
		},
		"repository": map[string]any{"full_name": "acme/repo"},
		"sender":     map[string]any{"login": "human"},
	}
	b, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}
	return b
}

// TestOnOpenedOrEdited_IssueFastPath_TriggersOnOpen covers the §R2.1 wiring:
// a genuinely new Task issue delivery starts a cycle via the CycleStarter
// hook, even though the issue has no machine block (which independently
// triggers the pre-existing attention-flagging path — both must run).
func TestOnOpenedOrEdited_IssueFastPath_TriggersOnOpen(t *testing.T) {
	cycle := &fakeCycleStarter{}
	issues := newFakeIssues()
	e := NewWebhookEvents(issues, fakeRepoLocator{}, "").WithCycle(cycle)

	if err := e.OnOpenedOrEdited(context.Background(), "issues", "opened", taskIssuePayload("opened", 42, "no block here")); err != nil {
		t.Fatalf("OnOpenedOrEdited: %v", err)
	}
	if len(cycle.calls) != 1 || cycle.calls[0] != 42 {
		t.Fatalf("cycle.calls = %v; want [42]", cycle.calls)
	}
}

// TestOnOpenedOrEdited_IssueFastPath_NotOnEdit ensures an "edited" delivery
// (the handler is registered for both opened and edited) never re-triggers
// the fast path.
func TestOnOpenedOrEdited_IssueFastPath_NotOnEdit(t *testing.T) {
	cycle := &fakeCycleStarter{}
	issues := newFakeIssues()
	e := NewWebhookEvents(issues, fakeRepoLocator{}, "").WithCycle(cycle)

	if err := e.OnOpenedOrEdited(context.Background(), "issues", "edited", taskIssuePayload("edited", 42, "no block here")); err != nil {
		t.Fatalf("OnOpenedOrEdited: %v", err)
	}
	if len(cycle.calls) != 0 {
		t.Fatalf("cycle.calls = %v; want none on an edit delivery", cycle.calls)
	}
}

// TestOnOpenedOrEdited_IssueFastPath_NilCycleIsNoop covers the disabled case
// (WithCycle never called) — the handler must not panic or behave
// differently from before the fast path existed.
func TestOnOpenedOrEdited_IssueFastPath_NilCycleIsNoop(t *testing.T) {
	issues := newFakeIssues()
	e := NewWebhookEvents(issues, fakeRepoLocator{}, "")

	if err := e.OnOpenedOrEdited(context.Background(), "issues", "opened", taskIssuePayload("opened", 42, "no block here")); err != nil {
		t.Fatalf("OnOpenedOrEdited: %v", err)
	}
}

// TestOnOpenedOrEdited_IssueFastPath_FailureIsNonFatal mirrors the
// best-effort convention every other webhook-driven cycle hook follows: a
// failed cycle start logs but does not fail the webhook delivery (retrying
// the delivery would just repeat the same failure; GitHub would eventually
// give up and disable the hook).
func TestOnOpenedOrEdited_IssueFastPath_FailureIsNonFatal(t *testing.T) {
	cycle := &fakeCycleStarter{err: fmt.Errorf("boom")}
	issues := newFakeIssues()
	e := NewWebhookEvents(issues, fakeRepoLocator{}, "").WithCycle(cycle)

	if err := e.OnOpenedOrEdited(context.Background(), "issues", "opened", taskIssuePayload("opened", 42, "no block here")); err != nil {
		t.Fatalf("OnOpenedOrEdited should swallow the cycle-start error, got: %v", err)
	}
}
