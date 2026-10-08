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

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/task"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
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
	calls []string
	err   error
}

func (r *recordingThreadRemover) RemoveIssueThread(_ context.Context, org, project string, n int) error {
	r.calls = append(r.calls, fmt.Sprintf("%s/%s#%d", org, project, n))
	return r.err
}

// GitHub's issues.closed and issues.reopened remove the issue's thread in the
// project its repository backs — whoever closed it, the platform included. A
// repository that is not one of ours, a pull request and a malformed delivery
// remove nothing; a removal failure fails the delivery so the delivery
// ledger's Replayer retries it (bounded attempts; the removal is idempotent).
func TestIssueThreadRemoval(t *testing.T) {
	locator := fakeRepoLocator{"acme/expenses": {"acme", "expenses"}}
	payload := func(repo string, n int, pr bool) []byte {
		p := map[string]any{"issue": map[string]any{"number": n}, "repository": map[string]any{"full_name": repo}}
		if pr {
			p["issue"].(map[string]any)["pull_request"] = map[string]any{"url": "x"}
		}
		b, _ := json.Marshal(p)
		return b
	}
	cases := []struct {
		name    string
		payload []byte
		want    []string
	}{
		{"known repo", payload("acme/expenses", 7, false), []string{"acme/expenses#7"}},
		{"unknown repo", payload("someone/else", 7, false), nil},
		{"pull request", payload("acme/expenses", 7, true), nil},
		{"no number", payload("acme/expenses", 0, false), nil},
		{"malformed", []byte("{"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []string{"closed", "reopened"} {
				threads := &recordingThreadRemover{}
				h := issueThreadRemoval{repos: locator, threads: threads}
				if err := h.OnIssueEvent(context.Background(), "issues", action, tc.payload); err != nil {
					t.Fatalf("OnIssueEvent(%s): %v", action, err)
				}
				if !reflect.DeepEqual(threads.calls, tc.want) {
					t.Fatalf("%s: removals = %v, want %v", action, threads.calls, tc.want)
				}
			}
		})
	}

	down := errors.New("db down")
	h := issueThreadRemoval{repos: locator, threads: &recordingThreadRemover{err: down}}
	if err := h.OnIssueEvent(context.Background(), "issues", "closed", payload("acme/expenses", 7, false)); !errors.Is(err, down) {
		t.Fatalf("removal failure: err = %v, want it returned", err)
	}
}
