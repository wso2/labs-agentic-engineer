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
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
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

// The adopter's refusal reaches the caller verbatim: the console and the MCP
// server both render it, and the message is what a human needs to act on.
func TestPromoteAndExecute_AdopterErrorPropagates(t *testing.T) {
	adopter := &fakeAdopter{err: delivery.ErrNoAdoptableMilestone}
	cmds, _ := newPromoteCommands([]string{"user-service"}, adopter)

	err := cmds.PromoteAndExecute(context.Background(), "org1", "proj1", "user-service", 42)
	if !errors.Is(err, delivery.ErrNoAdoptableMilestone) {
		t.Fatalf("err = %v, want the adopter's refusal verbatim", err)
	}
}

// THE REGRESSION at the HTTP edge. This refusal used to fall through to the
// default arm and leave as an opaque 500 with an "internal error" body — so the
// SRE/RCA handoff's only actionable failure was indistinguishable from a server
// fault, and the message written for a human never reached one.
func TestMapTaskCommandError_NoAdoptableMilestoneIsAConflict(t *testing.T) {
	var ae *apierr.Error
	if !errors.As(mapTaskCommandError(delivery.ErrNoAdoptableMilestone), &ae) {
		t.Fatal("the refusal must map to a typed transport error")
	}
	if ae.Status != http.StatusConflict {
		t.Errorf("status = %d, want %d — a project with nothing built is not a server fault",
			ae.Status, http.StatusConflict)
	}
	if !strings.Contains(ae.Message, delivery.ErrNoAdoptableMilestone.Error()) {
		t.Errorf("message = %q, want the actionable refusal, not an opaque one", ae.Message)
	}
}
