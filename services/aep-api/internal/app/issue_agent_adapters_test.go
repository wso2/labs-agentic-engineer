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

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/webhook"
)

type refusingAdopter struct{ err error }

func (a refusingAdopter) AdoptIssue(context.Context, string, string, int) error { return a.err }

// The event plane's "no deployed version" reaches the issue agent as the
// issues package's own sentinel; any other failure passes through as is.
func TestIssueAgentPromoter_TranslatesNoDeployedVersion(t *testing.T) {
	promote := func(err error) error {
		return issueAgentPromoter{commands: task.NewCommands(nil, refusingAdopter{err: err})}.
			PromoteAndExecute(context.Background(), "acme", "expenses", "api", 7)
	}
	if err := promote(delivery.ErrNoDeployedMilestone); !errors.Is(err, issues.ErrNoDeployedVersion) {
		t.Errorf("no deployed milestone: err = %v, want issues.ErrNoDeployedVersion", err)
	}
	other := errors.New("github down")
	if err := promote(other); !errors.Is(err, other) || errors.Is(err, issues.ErrNoDeployedVersion) {
		t.Errorf("other failure: err = %v, want it passed through", err)
	}
	if err := promote(nil); err != nil {
		t.Errorf("success: err = %v", err)
	}
}

type fakeRepoLocator map[string][2]string

func (l fakeRepoLocator) ByFullName(_ context.Context, fullName string) (string, string, error) {
	return l[fullName][0], l[fullName][1], nil
}

type recordingThreadRemover struct {
	calls  []string
	before []time.Time
	err    error
}

func (r *recordingThreadRemover) RemoveIssueThread(_ context.Context, org, project string, n int, before time.Time) error {
	r.calls = append(r.calls, fmt.Sprintf("%s/%s#%d", org, project, n))
	r.before = append(r.before, before)
	return r.err
}

var (
	closedAt   = time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	reopenedAt = time.Date(2026, 10, 8, 9, 30, 0, 0, time.UTC)
	receivedAt = time.Date(2026, 10, 8, 9, 45, 0, 0, time.UTC)
)

// issuePayload is an issues.closed / issues.reopened body. GitHub sets
// closed_at on a close and clears it on a reopen, whose updated_at is the
// reopen; stamped=false leaves both out.
func issuePayload(repo string, n int, pr bool, action string, stamped bool) []byte {
	issue := map[string]any{"number": n}
	if pr {
		issue["pull_request"] = map[string]any{"url": "x"}
	}
	if stamped {
		issue["closed_at"], issue["updated_at"] = closedAt, closedAt
		if action == "reopened" {
			issue["closed_at"], issue["updated_at"] = nil, reopenedAt
		}
	}
	b, _ := json.Marshal(map[string]any{"issue": issue, "repository": map[string]any{"full_name": repo}})
	return b
}

// GitHub's issues.closed and issues.reopened remove the issue's thread in the
// project its repository backs — whoever closed it, the platform included. A
// repository that is not one of ours, a pull request and a malformed delivery
// remove nothing; a removal failure fails the delivery so the delivery
// ledger's Replayer retries it (bounded attempts; the removal is idempotent).
func TestIssueThreadRemoval(t *testing.T) {
	locator := fakeRepoLocator{"acme/expenses": {"acme", "expenses"}}
	ctx := webhook.WithReceivedAt(context.Background(), receivedAt)
	cases := []struct {
		name string
		repo string
		n    int
		pr   bool
		want []string
	}{
		{"known repo", "acme/expenses", 7, false, []string{"acme/expenses#7"}},
		{"unknown repo", "someone/else", 7, false, nil},
		{"pull request", "acme/expenses", 7, true, nil},
		{"no number", "acme/expenses", 0, false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []string{"closed", "reopened"} {
				threads := &recordingThreadRemover{}
				h := issueThreadRemoval{repos: locator, threads: threads}
				if err := h.OnIssueEvent(ctx, "issues", action, issuePayload(tc.repo, tc.n, tc.pr, action, true)); err != nil {
					t.Fatalf("OnIssueEvent(%s): %v", action, err)
				}
				if !reflect.DeepEqual(threads.calls, tc.want) {
					t.Fatalf("%s: removals = %v, want %v", action, threads.calls, tc.want)
				}
			}
		})
	}
	threads := &recordingThreadRemover{}
	if err := (issueThreadRemoval{repos: locator, threads: threads}).OnIssueEvent(ctx, "issues", "closed", []byte("{")); err != nil || len(threads.calls) != 0 {
		t.Fatalf("malformed delivery: (%v, %v), want acked with no removal", err, threads.calls)
	}

	down := errors.New("db down")
	h := issueThreadRemoval{repos: locator, threads: &recordingThreadRemover{err: down}}
	if err := h.OnIssueEvent(ctx, "issues", "closed", issuePayload("acme/expenses", 7, false, "closed", true)); !errors.Is(err, down) {
		t.Fatalf("removal failure: err = %v, want it returned", err)
	}
}

// The removal is bounded by the event that caused it: a close by closed_at, a
// reopen by its updated_at (GitHub clears closed_at), and a payload without
// the field by the delivery's first receipt. With neither, the delivery fails
// rather than guess a bound.
func TestIssueThreadRemoval_BoundedByTheEvent(t *testing.T) {
	locator := fakeRepoLocator{"acme/expenses": {"acme", "expenses"}}
	received := webhook.WithReceivedAt(context.Background(), receivedAt)
	cases := []struct {
		name    string
		ctx     context.Context
		action  string
		stamped bool
		want    time.Time
	}{
		{"close", received, "closed", true, closedAt},
		{"reopen", received, "reopened", true, reopenedAt},
		{"close without closed_at", received, "closed", false, receivedAt},
		{"reopen without updated_at", received, "reopened", false, receivedAt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			threads := &recordingThreadRemover{}
			h := issueThreadRemoval{repos: locator, threads: threads}
			if err := h.OnIssueEvent(tc.ctx, "issues", tc.action, issuePayload("acme/expenses", 7, false, tc.action, tc.stamped)); err != nil {
				t.Fatalf("OnIssueEvent: %v", err)
			}
			if len(threads.before) != 1 || !threads.before[0].Equal(tc.want) {
				t.Fatalf("removal bound = %v, want %v", threads.before, tc.want)
			}
		})
	}

	threads := &recordingThreadRemover{}
	h := issueThreadRemoval{repos: locator, threads: threads}
	if err := h.OnIssueEvent(context.Background(), "issues", "closed", issuePayload("acme/expenses", 7, false, "closed", false)); err == nil || len(threads.calls) != 0 {
		t.Fatalf("no event time: (%v, %v), want an error and no removal", err, threads.calls)
	}
}
