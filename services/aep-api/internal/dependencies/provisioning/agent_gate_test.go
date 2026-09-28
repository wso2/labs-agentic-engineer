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

package provisioning

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// fakeAgentRegistrar records what the gate asked it to do.
type fakeAgentRegistrar struct {
	components []string
	listErr    error
	outcome    AgentRegistrationOutcome
	ensureErr  error
	disabled   bool

	listCalls   int
	ensureCalls int
	gotComps    []string
}

func (f *fakeAgentRegistrar) Enabled() bool { return !f.disabled }

func (f *fakeAgentRegistrar) GovernedAgents(context.Context, string, string) ([]string, error) {
	f.listCalls++
	return f.components, f.listErr
}

func (f *fakeAgentRegistrar) RegisterAgentsForBuild(_ context.Context, _, _ string, components []string) (AgentRegistrationOutcome, error) {
	f.ensureCalls++
	f.gotComps = components
	return f.outcome, f.ensureErr
}

func newAgentGateService(reg AgentRegistrar, issues *fakeIssues) *Service {
	s := NewService(Deps{Issues: issues})
	s.SetAgentRegistrar(reg)
	return s
}

// A version that declares an agent gets an OPEN ticket first, then the work,
// then a close — the shape of every gate beside it.
func TestAgentGate_MintsOpenThenClosesWithTheOutcome(t *testing.T) {
	reg := &fakeAgentRegistrar{
		components: []string{"book-search-agent"},
		outcome: AgentRegistrationOutcome{
			Provider: "aep-default-anthropic",
			Agents: []RegisteredAgent{{
				Component: "book-search-agent",
				ProxyURL:  "http://ai-gateway:8084/aep-book-s-16cc/v1",
			}},
		},
	}
	issues := newFakeIssues(nil)

	if f := newAgentGateService(reg, issues).ensureAgentGate(
		context.Background(), "default", "very-book-search", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}

	if reg.listCalls != 1 || reg.ensureCalls != 1 {
		t.Fatalf("list=%d ensure=%d, want 1 each", reg.listCalls, reg.ensureCalls)
	}
	if len(issues.created) != 1 {
		t.Fatalf("created %d gates, want 1", len(issues.created))
	}
	got := issues.created[0]
	if got.Title != agentGateTitle {
		t.Errorf("title = %q, want %q", got.Title, agentGateTitle)
	}
	if got.DedupeKey != "gate:very-book-search:v1:agent-manager" {
		t.Errorf("dedupe key = %q", got.DedupeKey)
	}
	if got.Milestone == nil || *got.Milestone != 7 {
		t.Errorf("milestone = %v, want 7 at creation — the dispatch hold counts BY milestone", got.Milestone)
	}
	// The minted body says what is about to happen, never the outcome: it is
	// written before the work runs.
	if strings.Contains(got.Body, "aep-book-s-16cc") {
		t.Errorf("the minted body must not carry the outcome: %q", got.Body)
	}

	if len(issues.closed) != 1 {
		t.Fatalf("closed = %v, want exactly the gate it minted", issues.closed)
	}
	var closing string
	for _, c := range issues.closed {
		closing = c
	}
	// The proxy address is the reason a human opens this ticket: it is where a
	// guardrail is attached.
	if !strings.Contains(closing, "http://ai-gateway:8084/aep-book-s-16cc/v1") {
		t.Errorf("closing comment lost the agent's model endpoint: %q", closing)
	}
	if !strings.Contains(closing, "aep-default-anthropic") {
		t.Errorf("closing comment lost the provider: %q", closing)
	}
}

// A version with no ai-agent files NO ticket. This is the only silent path, and
// it is what keeps every non-agent project's milestone free of a gate that
// would hold its dispatch for nothing.
func TestAgentGate_NoAgentsFilesNothing(t *testing.T) {
	reg := &fakeAgentRegistrar{components: nil}
	issues := newFakeIssues(nil)

	if f := newAgentGateService(reg, issues).ensureAgentGate(
		context.Background(), "default", "plain-service", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	if len(issues.created) != 0 || reg.ensureCalls != 0 {
		t.Errorf("created=%d ensure=%d, want nothing for a version with no agents",
			len(issues.created), reg.ensureCalls)
	}
}

// A design that cannot be READ is a failure, never "no agents". Conflating the
// two would let a build register nothing, file no ticket, and report success.
func TestAgentGate_AnUnreadableDesignFails(t *testing.T) {
	reg := &fakeAgentRegistrar{listErr: errors.New("design unavailable")}
	issues := newFakeIssues(nil)

	f := newAgentGateService(reg, issues).ensureAgentGate(
		context.Background(), "default", "very-book-search", "v1", 7)
	if f == nil {
		t.Fatal("want a failure when the design cannot be read")
	}
	if f.Dependency != agentGate {
		t.Errorf("failure dependency = %q, want %q", f.Dependency, agentGate)
	}
	if len(issues.created) != 0 {
		t.Errorf("filed a ticket before knowing there was anything to do")
	}
}

// When the ensure fails the gate STAYS OPEN carrying the cause, and the build
// fails at planning. An open `provision` gate holds the next dispatch, and the
// failure is what settles the run — nothing downstream starts.
func TestAgentGate_AFailedEnsureLeavesTheGateOpenWithTheCause(t *testing.T) {
	reg := &fakeAgentRegistrar{
		components: []string{"book-search-agent"},
		ensureErr:  errors.New("amp unreachable"),
	}
	issues := newFakeIssues(nil)

	f := newAgentGateService(reg, issues).ensureAgentGate(
		context.Background(), "default", "very-book-search", "v1", 7)
	if f == nil {
		t.Fatal("want a failure when registration fails")
	}
	if len(issues.closed) != 0 {
		t.Errorf("closed the gate on a failed ensure: %v", issues.closed)
	}
	comments := allComments(issues)
	if len(comments) != 1 || !strings.Contains(comments[0], "amp unreachable") {
		t.Errorf("comments = %v, want one carrying the cause", comments)
	}
}

// A RETRIED ProvisionGates must not file a second ticket. The gate is closed by
// the same call that files it, so its DedupeKey — resolved host-side against
// OPEN issues only — cannot see the one the previous attempt left behind.
func TestAgentGate_ARetryReusesTheTicketItAlreadyClosed(t *testing.T) {
	existing := []sourcecontrol.IssueInfo{{
		Number: 41,
		Title:  agentGateTitle,
		Labels: withGateVersion(platformGateLabels(agentGate), "v1"),
	}}
	reg := &fakeAgentRegistrar{components: []string{"book-search-agent"}}
	issues := newFakeIssues(existing)

	if f := newAgentGateService(reg, issues).ensureAgentGate(
		context.Background(), "default", "very-book-search", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	if len(issues.created) != 0 {
		t.Errorf("filed a second ticket for a version that already has one: %+v", issues.created)
	}
	if _, closed := issues.closed[41]; !closed {
		t.Errorf("closed = %v, want the EXISTING gate 41 reused and closed", issues.closed)
	}
}

// A NEW version files its own ticket — the previous version's closed gate must
// not be mistaken for this one's.
func TestAgentGate_ANewVersionFilesItsOwnTicket(t *testing.T) {
	existing := []sourcecontrol.IssueInfo{{
		Number: 41,
		Title:  agentGateTitle,
		Labels: withGateVersion(platformGateLabels(agentGate), "v1"),
	}}
	reg := &fakeAgentRegistrar{components: []string{"book-search-agent"}}
	issues := newFakeIssues(existing)

	if f := newAgentGateService(reg, issues).ensureAgentGate(
		context.Background(), "default", "very-book-search", "v2", 8); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	if len(issues.created) != 1 {
		t.Fatalf("created %d, want v2 to file its own gate", len(issues.created))
	}
	if issues.created[0].DedupeKey != "gate:very-book-search:v2:agent-manager" {
		t.Errorf("dedupe key = %q", issues.created[0].DedupeKey)
	}
}

// An agent the platform deliberately did not govern is STATED on the ticket,
// not omitted. A reader has to be able to tell "governed" from "nothing
// happened" — that distinction is the whole point of the record.
func TestAgentGate_ASkippedAgentSaysWhy(t *testing.T) {
	reg := &fakeAgentRegistrar{
		components: []string{"book-search-agent"},
		outcome: AgentRegistrationOutcome{
			Agents: []RegisteredAgent{{
				Component: "book-search-agent",
				Skipped:   true,
				Reason:    "org has no connected Anthropic key",
			}},
		},
	}
	issues := newFakeIssues(nil)

	if f := newAgentGateService(reg, issues).ensureAgentGate(
		context.Background(), "default", "very-book-search", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	var closing string
	for _, c := range issues.closed {
		closing = c
	}
	if !strings.Contains(closing, "not governed") ||
		!strings.Contains(closing, "org has no connected Anthropic key") {
		t.Errorf("closing comment hides the skip: %q", closing)
	}
}

// No registrar wired at all is the whole pre-Agent-Manager deployment: no
// ticket, no work, no failure.
func TestAgentGate_DisabledDoesNothing(t *testing.T) {
	reg := &fakeAgentRegistrar{disabled: true, components: []string{"book-search-agent"}}
	issues := newFakeIssues(nil)

	if f := newAgentGateService(reg, issues).ensureAgentGate(
		context.Background(), "default", "very-book-search", "v1", 7); f != nil {
		t.Fatalf("unexpected failure: %+v", f)
	}
	if len(issues.created) != 0 || reg.listCalls != 0 {
		t.Errorf("a disabled registrar must cost nothing")
	}
}
