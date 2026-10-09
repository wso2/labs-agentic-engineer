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

package eventcore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// hookSpy is the HookEnsurer: it records each project it was asked to ensure
// and answers the error programmed for that project, if any.
type hookSpy struct {
	mu       sync.Mutex
	projects []string
	fail     map[string]error
}

func (h *hookSpy) Register(_ context.Context, _, projectID string) (*int64, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.projects = append(h.projects, projectID)
	if err := h.fail[projectID]; err != nil {
		return nil, err
	}
	id := int64(len(h.projects))
	return &id, nil
}

func (h *hookSpy) ensured() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.projects)
}

// hookLogs captures the default logger's JSON lines (eventcore tests are not
// parallel, so swapping the default is safe here).
type hookLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *hookLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func captureHookLogs(t *testing.T) *hookLogs {
	t.Helper()
	l := &hookLogs{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(l, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return l
}

func (l *hookLogs) events(t *testing.T, msg string) []map[string]any {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(l.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

func hookSweep(h *harness, hooks HookEnsurer, repos ...RepoRef) *Sweep {
	return NewSweep(h.events, fakeRepoLister{repos: repos}, 0).WithHookEnsurer(hooks)
}

// A ready row with no hook id is the one a failed create-time registration
// leaves behind: the sweep ensures its hook. A row that holds
// one is left alone, whatever its age (R12: no migration of old hooks).
func TestSweep_RepairsMissingHooks(t *testing.T) {
	h := newHarness(t)
	hooks := &hookSpy{}
	s := hookSweep(h, hooks,
		RepoRef{OrgID: "default", ProjectID: "p", FullName: "acme/g", HasHook: false},
		RepoRef{OrgID: "default", ProjectID: "q", FullName: "acme/h", HasHook: true},
	)
	_ = s.Once(context.Background())
	if !slices.Equal(hooks.ensured(), []string{"p"}) {
		t.Fatalf("ensured %v, want [p]", hooks.ensured())
	}
}

// A permanent refusal (the repository gone from GitHub: 404) must not make
// the sweep call GitHub every minute for the life of the process:
// the row is skipped from then on, with one value-free log line.
func TestSweep_HookRepairSkipsAPermanentRefusalForTheProcessLifetime(t *testing.T) {
	logs := captureHookLogs(t)
	h := newHarness(t)
	hooks := &hookSpy{fail: map[string]error{
		"gone": fmt.Errorf("register webhook: %w", &sourcecontrol.HTTPStatusError{StatusCode: 404}),
	}}
	s := hookSweep(h, hooks,
		RepoRef{OrgID: "default", ProjectID: "gone", FullName: "acme/gone"},
	)
	for range 3 {
		_ = s.Once(context.Background())
	}
	if got := hooks.ensured(); !slices.Equal(got, []string{"gone"}) {
		t.Fatalf("ensured %v, want one attempt only", got)
	}
	skipped := logs.events(t, "eventcore.hook_repair_skipped")
	if len(skipped) != 1 {
		t.Fatalf("hook_repair_skipped lines = %d, want 1", len(skipped))
	}
	if skipped[0]["org"] != "default" || skipped[0]["project"] != "gone" || skipped[0]["reason"] != "repo_not_found" {
		t.Fatalf("hook_repair_skipped = %v, want {org: default, project: gone, reason: repo_not_found}", skipped[0])
	}
	if _, ok := skipped[0]["error"]; ok {
		t.Fatalf("hook_repair_skipped must carry a reason, not the error text: %v", skipped[0])
	}
}

// An AE Studio that is not serving (restarting, provisioning) or absent (the
// org's GitHub is not connected) is the ORG's state: the rest of that org's
// rows are skipped this tick, and the next tick tries again, so a pod that
// comes back (or a reconnect) heals without a restart. Another org's rows are
// untouched.
func TestSweep_HookRepairSkipsAnOrgWithoutAServingStudioThisTickOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"unavailable", sourcecontrol.ErrAEStudioUnavailable},
		{"absent", sourcecontrol.ErrAEStudioAbsent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			hooks := &hookSpy{fail: map[string]error{"a1": fmt.Errorf("register webhook: %w", tc.err)}}
			s := hookSweep(h, hooks,
				RepoRef{OrgID: "down", ProjectID: "a1", FullName: "acme/a1"},
				RepoRef{OrgID: "down", ProjectID: "a2", FullName: "acme/a2"},
				RepoRef{OrgID: "up", ProjectID: "b1", FullName: "acme/b1"},
			)
			_ = s.Once(context.Background())
			if got := hooks.ensured(); !slices.Equal(got, []string{"a1", "b1"}) {
				t.Fatalf("first tick ensured %v, want [a1 b1] (a2 skipped with its org)", got)
			}
			_ = s.Once(context.Background())
			if got := hooks.ensured(); !slices.Equal(got, []string{"a1", "b1", "a1", "b1"}) {
				t.Fatalf("second tick ensured %v, want the org retried", got)
			}
		})
	}
}

// A transient failure (a 5xx from GitHub through the pod) is retried on the
// next tick, never skipped for good.
func TestSweep_HookRepairRetriesATransientFailure(t *testing.T) {
	h := newHarness(t)
	hooks := &hookSpy{fail: map[string]error{"p": &sourcecontrol.HTTPStatusError{StatusCode: 502}}}
	s := hookSweep(h, hooks, RepoRef{OrgID: "default", ProjectID: "p", FullName: "acme/p"})
	_ = s.Once(context.Background())
	_ = s.Once(context.Background())
	if got := hooks.ensured(); !slices.Equal(got, []string{"p", "p"}) {
		t.Fatalf("ensured %v, want a retry on the next tick", got)
	}
}

// Without a HookEnsurer the sweep repairs nothing and still reconciles.
func TestSweep_NoHookEnsurerRepairsNothing(t *testing.T) {
	h := newHarness(t)
	s := NewSweep(h.events, fakeRepoLister{repos: []RepoRef{{OrgID: "default", ProjectID: "p", FullName: "acme/p"}}}, 0)
	if err := s.Once(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

// A transient failure logs a code and the GitHub status, never the error's
// text (a response body can ride it).
func TestSweep_HookRepairFailureLogsNoErrorText(t *testing.T) {
	logs := captureHookLogs(t)
	h := newHarness(t)
	hooks := &hookSpy{fail: map[string]error{"p": &sourcecontrol.HTTPStatusError{StatusCode: 502, Body: "upstream said secret-ish things"}}}
	_ = hookSweep(h, hooks, RepoRef{OrgID: "default", ProjectID: "p", FullName: "acme/p"}).Once(context.Background())
	failed := logs.events(t, "eventcore.hook_repair_failed")
	if len(failed) != 1 || failed[0]["reason"] != "github_error" || failed[0]["status"] != float64(502) {
		t.Fatalf("hook_repair_failed = %v", failed)
	}
	if _, ok := failed[0]["error"]; ok {
		t.Fatalf("the error text was logged: %v", failed[0])
	}
}

// A row skipped for good is forgotten once it leaves the listing (its project
// was deleted), so a project re-created under the same name is tried again.
func TestSweep_HookRepairForgetsASkippedRowThatLeftTheListing(t *testing.T) {
	h := newHarness(t)
	gone := RepoRef{OrgID: "default", ProjectID: "p", FullName: "acme/p"}
	hooks := &hookSpy{fail: map[string]error{"p": &sourcecontrol.HTTPStatusError{StatusCode: 404}}}
	lister := &fakeRepoLister{repos: []RepoRef{gone}}
	s := NewSweep(h.events, lister, 0).WithHookEnsurer(hooks)
	_ = s.Once(context.Background()) // skipped for good
	lister.repos = nil
	_ = s.Once(context.Background()) // the project was deleted
	lister.repos = []RepoRef{gone}
	hooks.fail = nil
	_ = s.Once(context.Background()) // re-created under the same name
	if got := hooks.ensured(); !slices.Equal(got, []string{"p", "p"}) {
		t.Fatalf("ensured %v, want the re-created project tried again", got)
	}
}
