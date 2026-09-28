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

package sourcecontrol

import (
	"context"
	"errors"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"strings"
	"sync"
	"testing"
)

func TestSRECreateStampsLabelsAndClassifies(t *testing.T) {
	gh := &fakeGitHub{}
	svc := newDedupService(gh)
	result, err := svc.CreateIssue(WithIncidentContext(context.Background(), "alert-123"), "org", "proj", CreateIssueRequest{
		Title: "checkout times out", ComponentName: " Checkout ", ActionStatuses: []*string{nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Classification != "code-level" {
		t.Fatalf("classification = %q", result.Classification)
	}
	if !hasLabel(gh.issues[0], "bug") || !hasLabel(gh.issues[0], "incident") {
		t.Fatalf("SRE labels missing: %v", gh.issues[0].Labels)
	}
	if result.Adopted || result.AdoptionError == "" {
		t.Fatalf("unwired adoption must be reported: %+v", result)
	}
}

type incidentRecurrence struct {
	count int64
	err   error
}

func (r incidentRecurrence) RecordRecurrence(context.Context, string, string, IssueInfo, CreateIssueRequest) (int64, error) {
	return r.count, r.err
}

func (f *fakeGitHub) ReopenIssue(_ context.Context, _, _ string, _ secrets.Credential, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.issues {
		if f.issues[i].Number == number {
			f.issues[i].State = "open"
			f.issues[i].StateReason = ""
			return nil
		}
	}
	return ErrIssueNotFound
}

func TestSRESuppressAndRecurrenceOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, reason                    string
		recurrence                      IncidentRecurrence
		suppressed, reopened, wantError bool
	}{
		{name: "not planned suppresses", reason: "not_planned", suppressed: true},
		{name: "completed reopens with recurrence", reason: "completed", recurrence: incidentRecurrence{count: 3}, reopened: true},
		{name: "recurrence failure leaves closed", reason: "completed", recurrence: incidentRecurrence{err: errors.New("evidence unavailable")}, wantError: true},
		{name: "missing closure identity leaves closed", reason: "completed", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{}
			adopter := &incidentAdopter{}
			svc := NewIssueService(fakeRepoRepo{}, gh, fakeResolver{}, IncidentPorts{Adopter: adopter, Recurrence: tc.recurrence})
			ctx := WithIncidentContext(context.Background(), "alert-123")
			req := CreateIssueRequest{Title: "timeout", ComponentName: "checkout"}
			first, err := svc.CreateIssue(ctx, "org", "proj", req)
			if err != nil {
				t.Fatal(err)
			}
			gh.issues[0].State, gh.issues[0].StateReason = "closed", tc.reason
			result, err := svc.CreateIssue(ctx, "org", "proj", req)
			if tc.wantError {
				if err == nil || gh.issues[0].State != "closed" || len(adopter.numbers) != 1 || gh.createCount != 1 {
					t.Fatalf("unsafe recurrence: result=%+v err=%v", result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.Number != first.Number || result.Suppressed != tc.suppressed || result.Reopened != tc.reopened || result.Deduped {
				t.Fatalf("outcome=%+v", result)
			}
			if tc.reopened && (result.RecurrenceCount != 3 || !result.Adopted || gh.issues[0].State != "open" || len(adopter.numbers) != 2) {
				t.Fatalf("recurrence=%+v", result)
			}
			if tc.suppressed && (result.Adopted || len(adopter.numbers) != 1) {
				t.Fatalf("suppression dispatched: %+v", result)
			}
			if gh.createCount != 1 {
				t.Fatalf("created duplicate: %d", gh.createCount)
			}
		})
	}
}

type incidentAdopter struct {
	numbers []int
	err     error
}

func (a *incidentAdopter) AdoptIssue(_ context.Context, _, _ string, number int) error {
	a.numbers = append(a.numbers, number)
	return a.err
}

func TestSREDedupUsesTrustedIdentityAndDoesNotDispatchTwice(t *testing.T) {
	gh := &fakeGitHub{}
	adopter := &incidentAdopter{}
	svc := NewIssueService(fakeRepoRepo{}, gh, fakeResolver{}, IncidentPorts{Adopter: adopter})
	ctx := WithIncidentContext(context.Background(), "alert-123")
	req := CreateIssueRequest{Title: "timeout", ComponentName: " Checkout ", DedupeKey: "client-first"}
	first, err := svc.CreateIssue(ctx, "org", "proj", req)
	if err != nil {
		t.Fatal(err)
	}
	req.DedupeKey, req.ComponentName = "client-second", "checkout"
	second, err := svc.CreateIssue(ctx, "org", "proj", req)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Adopted || !second.Deduped || second.Number != first.Number || second.Classification != "none" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if gh.createCount != 1 || len(adopter.numbers) != 1 {
		t.Fatalf("creates=%d dispatches=%v", gh.createCount, adopter.numbers)
	}
}

func TestSREConfigNamespaceCannotSuppressCodeWork(t *testing.T) {
	gh := &fakeGitHub{}
	adopter := &incidentAdopter{err: errors.New("no deployed milestone")}
	svc := NewIssueService(fakeRepoRepo{}, gh, fakeResolver{}, IncidentPorts{Adopter: adopter})
	ctx := WithIncidentContext(context.Background(), "alert-123")
	status := "applied"
	req := CreateIssueRequest{Title: "timeout", ComponentName: "checkout", ActionStatuses: []*string{&status}, Labels: []string{"aep", "dedupe:spoof"}}
	config, err := svc.CreateIssue(ctx, "org", "proj", req)
	if err != nil {
		t.Fatal(err)
	}
	if config.Classification != "config-level" || config.Adopted || config.AdoptionError != "" || len(adopter.numbers) != 0 {
		t.Fatalf("config should be recorded only: %+v", config)
	}
	if hasLabel(gh.issues[0], "aep") || hasLabel(gh.issues[0], "dedupe:spoof") {
		t.Fatalf("client controls scheduling/identity: %v", gh.issues[0].Labels)
	}
	gh.issues[0].State, gh.issues[0].StateReason = "closed", "not_planned"
	req.ActionStatuses = []*string{nil}
	code, err := svc.CreateIssue(ctx, "org", "proj", req)
	if err != nil {
		t.Fatal(err)
	}
	if code.Number == config.Number || code.Suppressed || code.Deduped || code.Adopted || code.AdoptionError != "no deployed milestone" {
		t.Fatalf("code result must preserve adoption failure: %+v", code)
	}
}

func TestSRERequiresTrustedContextAndComponent(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  context.Context
		req  CreateIssueRequest
	}{
		{"component without context", context.Background(), CreateIssueRequest{Title: "timeout", ComponentName: "checkout", DedupeKey: "spoof"}},
		{"statuses without context", context.Background(), CreateIssueRequest{Title: "timeout", ActionStatuses: []*string{nil}}},
		{"SRE label without context", context.Background(), CreateIssueRequest{Title: "timeout", Labels: []string{"incident"}}},
		{"missing component", WithIncidentContext(context.Background(), "alert-1"), CreateIssueRequest{Title: "timeout"}},
		{"empty context identity", WithIncidentContext(context.Background(), " "), CreateIssueRequest{Title: "timeout", ComponentName: "checkout"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{}
			_, err := newDedupService(gh).CreateIssue(tc.ctx, "org", "proj", tc.req)
			if err == nil || gh.createCount != 0 {
				t.Fatalf("invalid SRE request filed: err=%v creates=%d", err, gh.createCount)
			}
		})
	}
}

func TestSREDistinctIncidentComponentAndTenantRemainIndependent(t *testing.T) {
	gh := &fakeGitHub{}
	svc := newDedupService(gh)
	for _, tc := range []struct{ incident, component, org, project string }{
		{"alert-1", "checkout", "org", "proj"},
		{"ALERT-1", "checkout", "org", "proj"},
		{"alert-1", "inventory", "org", "proj"},
		{"alert-1", "checkout", "other", "proj"},
		{"alert-1", "checkout", "org", "other"},
	} {
		result, err := svc.CreateIssue(WithIncidentContext(context.Background(), tc.incident), tc.org, tc.project, CreateIssueRequest{Title: "timeout", ComponentName: tc.component, DedupeKey: "same-spoof"})
		if err != nil || result.Deduped {
			t.Fatalf("distinct identity %+v collapsed: %+v %v", tc, result, err)
		}
	}
	if gh.createCount != 5 {
		t.Fatalf("creates=%d", gh.createCount)
	}
}

type failingIncidentHost struct {
	*fakeGitHub
	listErr, labelErr error
}

func (h failingIncidentHost) ListIssues(ctx context.Context, owner, repo string, cred secrets.Credential, labels []string) ([]IssueInfo, error) {
	if h.listErr != nil {
		return nil, h.listErr
	}
	return h.fakeGitHub.ListIssues(ctx, owner, repo, cred, labels)
}
func (h failingIncidentHost) EnsureLabel(context.Context, string, string, secrets.Credential, string, string) error {
	return h.labelErr
}

func TestSREIdentityFailuresDoNotFileUntrackedIssues(t *testing.T) {
	for _, tc := range []struct {
		name              string
		listErr, labelErr error
	}{
		{"lookup failure", errors.New("lookup unavailable"), nil},
		{"label failure", nil, errors.New("labels unavailable")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{}
			svc := NewIssueService(fakeRepoRepo{}, failingIncidentHost{gh, tc.listErr, tc.labelErr}, fakeResolver{})
			_, err := svc.CreateIssue(WithIncidentContext(context.Background(), "alert-1"), "org", "proj", CreateIssueRequest{Title: "timeout", ComponentName: "checkout"})
			if err == nil || gh.createCount != 0 {
				t.Fatalf("untracked incident created: err=%v creates=%d", err, gh.createCount)
			}
		})
	}
}

func TestSREClientCannotSetDeliveryLabels(t *testing.T) {
	gh := &fakeGitHub{}
	status := "applied"
	_, err := newDedupService(gh).CreateIssue(WithIncidentContext(context.Background(), "alert-1"), "org", "proj", CreateIssueRequest{
		Title: "timeout", ComponentName: "checkout", ActionStatuses: []*string{&status},
		Labels: []string{" AEP ", "Development", "validation", "provision", "conflict", "aep:status/running", "customer-visible"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range gh.issues[0].Labels {
		if label != "bug" && label != "incident" && label != "customer-visible" && !strings.HasPrefix(label, "dedupe:sre-config-") {
			t.Fatalf("client delivery label retained: %q", label)
		}
	}
}

func TestSREConcurrentDedupDispatchesOnce(t *testing.T) {
	gh := &fakeGitHub{}
	adopter := &incidentAdopter{}
	svc := NewIssueService(fakeRepoRepo{}, gh, fakeResolver{}, IncidentPorts{Adopter: adopter})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.CreateIssue(WithIncidentContext(context.Background(), "alert-1"), "org", "proj", CreateIssueRequest{Title: "timeout", ComponentName: "checkout"})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if gh.createCount != 1 || len(adopter.numbers) != 1 {
		t.Fatalf("creates=%d dispatches=%v", gh.createCount, adopter.numbers)
	}
}

// TestWithAdopterInjectsAfterConstruction pins the composition-root pattern
// internal/app/app.go relies on: the event plane (the only IssueAdopter
// implementation) is itself built from issueService-derived services, so it
// cannot be passed into NewIssueService's IncidentPorts at construction time.
// Before WithAdopter existed, aep-api's real composition root never wired an
// adopter at all — every SRE-filed code-level issue got AdoptionError
// "incident adoption is not configured" in production, silently.
func TestWithAdopterInjectsAfterConstruction(t *testing.T) {
	gh := &fakeGitHub{}
	adopter := &incidentAdopter{}
	svc := NewIssueService(fakeRepoRepo{}, gh, fakeResolver{})
	svc.WithAdopter(adopter)

	result, err := svc.CreateIssue(WithIncidentContext(context.Background(), "sre-handoff"), "org", "proj", CreateIssueRequest{
		Title: "checkout times out", ComponentName: "checkout", ActionStatuses: []*string{nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Adopted || result.AdoptionError != "" || len(adopter.numbers) != 1 {
		t.Fatalf("adopter not consulted after WithAdopter: result=%+v adopted=%v", result, adopter.numbers)
	}
}

func TestSREIdentityCannotBeMintedThroughLegacyCreate(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  CreateIssueRequest
	}{
		{"code label", CreateIssueRequest{Title: "counterfeit", Labels: []string{"dedupe:sre-code-0123456789abcdef"}}},
		{"config label", CreateIssueRequest{Title: "counterfeit", Labels: []string{"dedupe:sre-config-0123456789abcdef"}}},
		{"normalized label", CreateIssueRequest{Title: "counterfeit", Labels: []string{" DEDUPE:SRE-CODE-0123456789ABCDEF "}}},
		{"code key", CreateIssueRequest{Title: "counterfeit", DedupeKey: "sre-code-0123456789abcdef"}},
		{"config key", CreateIssueRequest{Title: "counterfeit", DedupeKey: "sre-config-0123456789abcdef"}},
		{"normalized key", CreateIssueRequest{Title: "counterfeit", DedupeKey: " SRE CODE 0123456789ABCDEF "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := &fakeGitHub{}
			_, err := newDedupService(gh).CreateIssue(context.Background(), "org", "proj", tc.req)
			if !errors.Is(err, ErrIncidentContextRequired) || gh.createCount != 0 {
				t.Fatalf("legacy caller minted reserved identity: err=%v creates=%d", err, gh.createCount)
			}
		})
	}
}

func TestSREIdentityReservationPreservesOtherLegacyDedup(t *testing.T) {
	gh := &fakeGitHub{}
	svc := newDedupService(gh)
	req := CreateIssueRequest{Title: "ordinary issue", Labels: []string{"dedupe:custom"}, DedupeKey: "sre-rca/checkout"}
	first, err := svc.CreateIssue(context.Background(), "org", "proj", req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateIssue(context.Background(), "org", "proj", req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Deduped || !second.Deduped || first.Number != second.Number || gh.createCount != 1 {
		t.Fatalf("legacy dedupe changed: first=%+v second=%+v creates=%d", first, second, gh.createCount)
	}
}

// TestSREHandoffShapedRequestNeedsIncidentContext pins the exact contract
// internal/edge/sre_handoff_gate.go relies on: aep-mcp-server sends
// componentName + actionStatuses straight from the model's tool-call
// arguments (handoffContext.ts's resolveHandoff), with no dedupe key and no
// trusted-identity header — there is no generic-extensions transport left to
// carry one (docs/design/draft/2026-09-17-sre-agent-extensions-handoff.md
// §2/§5). Without SOME incident context bound onto ctx first, that shape is
// indistinguishable from a spoofed legacy call and CreateIssue's own
// anti-spoofing guard rejects it — this reproduces the exact failure the SRE
// agent hit in production before the edge gate bound one.
func TestSREHandoffShapedRequestNeedsIncidentContext(t *testing.T) {
	req := CreateIssueRequest{
		Title:          "service1 logs an error",
		Body:           "RCA report body",
		ComponentName:  "service1",
		ActionStatuses: []*string{nil},
	}

	t.Run("no incident context: rejected exactly like a spoofed call", func(t *testing.T) {
		gh := &fakeGitHub{}
		_, err := newDedupService(gh).CreateIssue(context.Background(), "org", "hello", req)
		if !errors.Is(err, ErrIncidentContextRequired) || gh.createCount != 0 {
			t.Fatalf("want ErrIncidentContextRequired and no create, got err=%v creates=%d", err, gh.createCount)
		}
	})

	t.Run("bound incident context: the same request succeeds", func(t *testing.T) {
		gh := &fakeGitHub{}
		// "sre-handoff" — must match internal/edge/sre_handoff_gate.go's
		// sreHandoffIncidentID constant; that file is the one production caller.
		ctx := WithIncidentContext(context.Background(), "sre-handoff")
		result, err := newDedupService(gh).CreateIssue(ctx, "org", "hello", req)
		if err != nil {
			t.Fatalf("want success once a trusted transport bound an incident context, got %v", err)
		}
		if gh.createCount != 1 || result.Number == 0 {
			t.Fatalf("issue not created: result=%+v creates=%d", result, gh.createCount)
		}
	})
}
