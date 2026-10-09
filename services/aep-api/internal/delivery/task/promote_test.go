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
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// promote-task-from-issue is the SRE/RCA handoff's dispatch leg. It is ADOPTION
// and nothing more: the caller's issue body is left exactly as written (bodies
// are prose the agent reads, and nothing platform-side parses them), and the
// issue is handed to the coding agent through the event plane.

func newPromoteCommands(known []string, adopter *fakeAdopter) (*Commands, *fakeEnsurer) {
	ensurer := &fakeEnsurer{known: map[string]bool{}}
	for _, c := range known {
		ensurer.known[c] = true
	}
	return NewCommands(ensurer, adopter), ensurer
}

// An unknown component fails the CALL, synchronously. The check exists so a
// caller's prefix-stripping bug surfaces here rather than inside a cycle hours
// later, so nothing may be adopted when it fires.
func TestPromoteAndExecute_UnknownComponent_FailsSynchronously(t *testing.T) {
	adopter := &fakeAdopter{}
	cmds, ensurer := newPromoteCommands([]string{"user-service"}, adopter)

	err := cmds.PromoteAndExecute(context.Background(), "org1", "proj1", "typo-service", 42)
	if err == nil {
		t.Fatal("an unknown component must fail the promote call")
	}
	if !strings.Contains(err.Error(), "promote task from issue") {
		t.Errorf("error = %v, want it to name the operation", err)
	}
	if len(ensurer.calls) != 1 {
		t.Errorf("the pre-check must run exactly once, got %d calls", len(ensurer.calls))
	}
	if len(adopter.adopted) != 0 {
		t.Errorf("nothing may be adopted when the pre-check fails, got %v", adopter.adopted)
	}
}

func TestPromoteAndExecute_BlankComponent_ReturnsComponentNameRequired(t *testing.T) {
	adopter := &fakeAdopter{}
	cmds, ensurer := newPromoteCommands(nil, adopter)

	if err := cmds.PromoteAndExecute(context.Background(), "org1", "proj1", "  ", 42); !errors.Is(err, ErrComponentNameRequired) {
		t.Fatalf("err = %v, want ErrComponentNameRequired", err)
	}
	if len(ensurer.calls) != 0 || len(adopter.adopted) != 0 {
		t.Error("a blank component must be refused before any side effect")
	}
}

// The happy path: the component is ensured, then the issue is adopted — and the
// issue itself is untouched, because adoption is a milestone assignment, not a
// body rewrite.
func TestPromoteAndExecute_AdoptsTheIssue(t *testing.T) {
	adopter := &fakeAdopter{}
	cmds, ensurer := newPromoteCommands([]string{"user-service"}, adopter)

	if err := cmds.PromoteAndExecute(context.Background(), "org1", "proj1", "user-service", 42); err != nil {
		t.Fatalf("PromoteAndExecute: %v", err)
	}
	if len(ensurer.calls) != 1 || ensurer.calls[0] != "user-service" {
		t.Errorf("component ensure calls = %v, want [user-service]", ensurer.calls)
	}
	if len(adopter.adopted) != 1 || adopter.adopted[0] != 42 {
		t.Errorf("adopted = %v, want [42]", adopter.adopted)
	}
}

// Adoption is idempotent, so a retried handoff is a no-op rather than an error:
// the second call re-adopts an issue that is already in the milestone.
func TestPromoteAndExecute_RepeatedCallIsIdempotent(t *testing.T) {
	adopter := &fakeAdopter{}
	cmds, _ := newPromoteCommands([]string{"user-service"}, adopter)

	for i := 0; i < 2; i++ {
		if err := cmds.PromoteAndExecute(context.Background(), "org1", "proj1", "user-service", 42); err != nil {
			t.Fatalf("PromoteAndExecute #%d: %v", i+1, err)
		}
	}
	if len(adopter.adopted) != 2 {
		t.Errorf("both calls must reach the adopter (it is the idempotent one), got %v", adopter.adopted)
	}
}

// A nil ensurer degrades to skipping the pre-check rather than failing every
// call — the same nil-tolerance the port has always had.
func TestPromoteAndExecute_NilEnsurer_SkipsPreCheck(t *testing.T) {
	adopter := &fakeAdopter{}
	cmds := NewCommands(nil, adopter)

	if err := cmds.PromoteAndExecute(context.Background(), "org1", "proj1", "anything", 7); err != nil {
		t.Fatalf("PromoteAndExecute: %v", err)
	}
	if len(adopter.adopted) != 1 {
		t.Errorf("adopted = %v, want the issue handed over", adopter.adopted)
	}
}

// The adopter's refusal reaches the caller unwrapped: the console and the
// issue agent's hand-off tool both render it as the sentence a human needs.
func TestPromoteAndExecute_AdopterErrorPropagates(t *testing.T) {
	adopter := &fakeAdopter{err: delivery.ErrNoDeployedMilestone}
	cmds, _ := newPromoteCommands([]string{"user-service"}, adopter)

	err := cmds.PromoteAndExecute(context.Background(), "org1", "proj1", "user-service", 42)
	if !errors.Is(err, delivery.ErrNoDeployedMilestone) {
		t.Fatalf("err = %v, want the adopter's refusal", err)
	}
}

// A project with no deployed version has no milestone to adopt into: the REST
// route answers 409 with the sentence the console shows the user as-is.
func TestPromoteTaskFromIssue_NoDeployedVersionIs409(t *testing.T) {
	cmds, _ := newPromoteCommands([]string{"user-service"}, &fakeAdopter{err: delivery.ErrNoDeployedMilestone})
	_, err := NewHandler(nil, cmds).PromoteTaskFromIssue(context.Background(), gen.PromoteTaskFromIssueRequestObject{
		ProjectName: "proj1", IssueNumber: 42, Body: &gen.PromoteTaskFromIssueJSONRequestBody{ComponentName: "user-service"},
	})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Fatalf("err = %v, want a 409", err)
	}
	if want := "Deploy a version first: the coding agent works in a deployed version's milestone."; apiErr.Message != want {
		t.Errorf("message = %q, want %q", apiErr.Message, want)
	}
}

// Every refusal of adoption is a 409 in the platform's own words: an issue it
// will not adopt is said so, never answered with a 202 that did nothing.
func TestPromoteTaskFromIssue_RefusalsAre409(t *testing.T) {
	for _, refusal := range []error{delivery.ErrIssueClosed, delivery.ErrNotCodingWork} {
		cmds, _ := newPromoteCommands([]string{"user-service"}, &fakeAdopter{err: refusal})
		_, err := NewHandler(nil, cmds).PromoteTaskFromIssue(context.Background(), gen.PromoteTaskFromIssueRequestObject{
			ProjectName: "proj1", IssueNumber: 42, Body: &gen.PromoteTaskFromIssueJSONRequestBody{ComponentName: "user-service"},
		})
		var apiErr *apierr.Error
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
			t.Fatalf("%v: err = %v, want a 409", refusal, err)
		}
		if apiErr.Message != refusal.Error() {
			t.Errorf("message = %q, want %q", apiErr.Message, refusal.Error())
		}
	}
}

// An issue the host does not have is a 404, not a server fault.
func TestPromoteTaskFromIssue_UnknownIssueIs404(t *testing.T) {
	cmds, _ := newPromoteCommands([]string{"user-service"}, &fakeAdopter{err: sourcecontrol.ErrIssueNotFound})
	_, err := NewHandler(nil, cmds).PromoteTaskFromIssue(context.Background(), gen.PromoteTaskFromIssueRequestObject{
		ProjectName: "proj1", IssueNumber: 42, Body: &gen.PromoteTaskFromIssueJSONRequestBody{ComponentName: "user-service"},
	})
	var apiErr *apierr.Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound || apiErr.Message != "issue not found" {
		t.Fatalf("err = %v, want a 404 \"issue not found\"", err)
	}
}
