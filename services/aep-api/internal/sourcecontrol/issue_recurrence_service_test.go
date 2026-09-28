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
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

type recurrenceHost struct {
	*fakeGitHub
	writeErr, reopenErr error
}

func (h *recurrenceHost) EditIssueBody(_ context.Context, _, _ string, _ secrets.Credential, number int, body string) error {
	if h.writeErr != nil {
		return h.writeErr
	}
	for i := range h.issues {
		if h.issues[i].Number == number {
			h.issues[i].Body = body
			return nil
		}
	}
	return ErrIssueNotFound
}

func (h *recurrenceHost) GetIssue(_ context.Context, _, _ string, _ secrets.Credential, number int) (*IssueInfo, error) {
	for _, issue := range h.issues {
		if issue.Number == number {
			return &issue, nil
		}
	}
	return nil, ErrIssueNotFound
}

func (h *recurrenceHost) ReopenIssue(ctx context.Context, owner, repo string, cred secrets.Credential, number int) error {
	if h.reopenErr != nil {
		return h.reopenErr
	}
	if err := h.fakeGitHub.ReopenIssue(ctx, owner, repo, cred, number); err != nil {
		return err
	}
	for i := range h.issues {
		if h.issues[i].Number == number {
			h.issues[i].StateReason = "reopened"
			h.issues[i].ClosedAt = ""
		}
	}
	return nil
}

func TestRecurrenceCompletedPreservesEvidenceAndEscalates(t *testing.T) {
	host := &recurrenceHost{fakeGitHub: &fakeGitHub{}}
	adopter := &incidentAdopter{}
	svc := NewIssueService(fakeRepoRepo{}, host, fakeResolver{}, IncidentPorts{Adopter: adopter})
	ctx := WithIncidentContext(context.Background(), "alert-123")
	req := CreateIssueRequest{Title: "timeout", Body: "new handoff evidence", ComponentName: "checkout"}
	first, err := svc.CreateIssue(ctx, "org", "proj", req)
	if err != nil {
		t.Fatal(err)
	}
	host.issues[0].Body = "Original RCA evidence"
	for recurrence := int64(1); recurrence <= 4; recurrence++ {
		host.issues[0].State, host.issues[0].StateReason = "closed", "completed"
		host.issues[0].ClosedAt = fmt.Sprintf("2026-09-18T08:00:0%dZ", recurrence)
		result, err := svc.CreateIssue(ctx, "org", "proj", req)
		if err != nil {
			t.Fatal(err)
		}
		if !result.Reopened || !result.Adopted || result.Number != first.Number || result.RecurrenceCount != recurrence {
			t.Fatalf("recurrence %d: %+v", recurrence, result)
		}
		issue, err := svc.GetIssue(ctx, "org", "proj", first.Number)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(issue.Body, "Original RCA evidence\n") || !strings.Contains(issue.Body, fmt.Sprintf("## Recurrence %d\n", recurrence)) ||
			!strings.Contains(issue.Body, "The earlier merged fix failed to resolve this incident.") || strings.Count(issue.Body, "new handoff evidence") != int(recurrence) {
			t.Fatalf("evidence lost or duplicated: %s", issue.Body)
		}
		want := "unverified_fix"
		if recurrence >= 3 {
			want = "escalated"
		}
		if issue.State != "open" || issue.AttentionReason != want {
			t.Fatalf("detail=%+v, want %s", issue, want)
		}
		listed, err := svc.ListIssues(ctx, "org", "proj", []string{"incident"})
		if err != nil || len(listed) != 1 || listed[0].AttentionReason != want {
			t.Fatalf("list=%+v err=%v", listed, err)
		}
	}
	if host.createCount != 1 || len(adopter.numbers) != 5 {
		t.Fatalf("creates=%d adoptions=%v", host.createCount, adopter.numbers)
	}
}

func TestRecurrenceEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, reason       string
		ordinary, suppress bool
	}{
		{name: "terminal", reason: "not_planned", suppress: true},
		{name: "unknown closure", reason: ""},
		{name: "non SRE", reason: "completed", ordinary: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			host := &recurrenceHost{fakeGitHub: &fakeGitHub{}}
			svc := NewIssueService(fakeRepoRepo{}, host, fakeResolver{})
			ctx := WithIncidentContext(context.Background(), "alert-123")
			req := CreateIssueRequest{Title: "timeout", Body: "evidence", ComponentName: "checkout"}
			_, err := svc.CreateIssue(ctx, "org", "proj", req)
			if err != nil {
				t.Fatal(err)
			}
			host.issues[0].State, host.issues[0].StateReason, host.issues[0].ClosedAt = "closed", tc.reason, "2026-09-18T08:00:00Z"
			if tc.ordinary {
				host.issues[0].Labels = host.issues[0].Labels[2:]
			}
			result, err := svc.CreateIssue(ctx, "org", "proj", req)
			if tc.suppress {
				if err != nil || !result.Suppressed {
					t.Fatalf("terminal result=%+v err=%v", result, err)
				}
			} else if !errors.Is(err, ErrIncidentRecurrenceIneligible) {
				t.Fatalf("ineligible closure: result=%+v err=%v", result, err)
			}
			if host.issues[0].State != "closed" || host.issues[0].Body != "" || host.createCount != 1 {
				t.Fatalf("ineligible issue changed: %+v", host.issues)
			}
		})
	}
}

// An ineligible closed duplicate (legacy, or closed by a human as a duplicate)
// must not hide a completed match that can recur.
func TestRecurrenceSkipsIneligibleClosedMatchForEligibleOne(t *testing.T) {
	host := &recurrenceHost{fakeGitHub: &fakeGitHub{}}
	svc := NewIssueService(fakeRepoRepo{}, host, fakeResolver{})
	ctx := WithIncidentContext(context.Background(), "alert-123")
	req := CreateIssueRequest{Title: "timeout", Body: "evidence", ComponentName: "checkout"}
	if _, err := svc.CreateIssue(ctx, "org", "proj", req); err != nil {
		t.Fatal(err)
	}
	completed := host.issues[0]
	completed.Number, completed.State, completed.StateReason, completed.ClosedAt = 2, "closed", "completed", "2026-09-18T08:00:00Z"
	host.issues[0].State, host.issues[0].StateReason, host.issues[0].ClosedAt = "closed", "duplicate", "2026-09-18T07:00:00Z"
	host.issues = append(host.issues, completed)

	result, err := svc.CreateIssue(ctx, "org", "proj", req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Reopened || result.Number != 2 || result.RecurrenceCount != 1 {
		t.Fatalf("the completed match must recur: %+v", result)
	}
	if host.issues[0].State != "closed" || host.issues[0].Body != "" || host.createCount != 1 {
		t.Fatalf("ineligible issue changed or a new issue was filed: %+v creates=%d", host.issues, host.createCount)
	}
}

func TestRecurrenceRetryAfterWriteOrReopenFailure(t *testing.T) {
	for _, stage := range []string{"write", "reopen"} {
		t.Run(stage, func(t *testing.T) {
			host := &recurrenceHost{fakeGitHub: &fakeGitHub{}}
			svc := NewIssueService(fakeRepoRepo{}, host, fakeResolver{})
			ctx := WithIncidentContext(context.Background(), "alert-123")
			req := CreateIssueRequest{Title: "timeout", Body: "handoff", ComponentName: "checkout"}
			_, err := svc.CreateIssue(ctx, "org", "proj", req)
			if err != nil {
				t.Fatal(err)
			}
			host.issues[0].State, host.issues[0].StateReason, host.issues[0].ClosedAt = "closed", "completed", "2026-09-18T08:00:00Z"
			if stage == "write" {
				host.writeErr = errors.New("write unavailable")
			} else {
				host.reopenErr = errors.New("reopen unavailable")
			}
			if _, err := svc.CreateIssue(ctx, "org", "proj", req); err == nil {
				t.Fatal("failure not surfaced")
			}
			if host.issues[0].State != "closed" {
				t.Fatal("failed recurrence reopened issue")
			}
			host.writeErr, host.reopenErr = nil, nil
			// Reconstruct the service: retry safety must survive process restart.
			svc = NewIssueService(fakeRepoRepo{}, host, fakeResolver{})
			result, err := svc.CreateIssue(ctx, "org", "proj", req)
			if err != nil {
				t.Fatal(err)
			}
			if result.RecurrenceCount != 1 || strings.Count(host.issues[0].Body, "## Recurrence 1") != 1 || strings.Count(host.issues[0].Body, "handoff") != 1 {
				t.Fatalf("retry duplicated evidence: %+v body=%s", result, host.issues[0].Body)
			}
		})
	}
}
