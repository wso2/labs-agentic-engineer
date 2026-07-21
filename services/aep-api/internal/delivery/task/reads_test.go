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
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// TestComputeAttention_CleanTaskIsEmptyNonNilSlice guards the contract that
// TaskView.attention is "[] when clean", never nil. A nil slice marshals as
// JSON null, and the task-log stream forwards the `task` frame verbatim to the
// console, which maps over attention unconditionally — a null there blanked the
// whole detail page (found in live e2e). The REST path coerced null→[] on the
// client; the stream path trusted the contract, so the contract must hold here.
func TestComputeAttention_CleanTaskIsEmptyNonNilSlice(t *testing.T) {
	flags := computeAttention(taskmeta.ParsedLabels{}, taskmeta.Block{}, nil, "")
	if flags == nil {
		t.Fatal("computeAttention returned nil for a clean task — must be a non-nil empty slice ([], not null)")
	}
	if len(flags) != 0 {
		t.Fatalf("a clean task must have no attention flags, got %v", flags)
	}
	b, err := json.Marshal(flags)
	if err != nil {
		t.Fatalf("marshal attention: %v", err)
	}
	if strings.TrimSpace(string(b)) != "[]" {
		t.Errorf("clean attention marshaled as %s, want []", b)
	}
}

// TestListReads_ValidationTaskExcluded pins the list read-model boundary: the
// project's aep:validation Task is a phase of the run (surfaced via /status
// deploy.validation + validationUrl), NOT an implementation task, so
// List/ListByTag never return it — the console tasks page and the build's
// per-version task list get implementation tasks only. Get by issue number
// still serves it (the deployments chip links straight to the issue/PR).
func TestListReads_ValidationTaskExcluded(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(taggedIssue(10, "order-service", "v1"))
	issues.seed(validationIssue(30, "v1"))

	reads := NewReads(issues, fakeRepos{repo: defaultRepo()}, newFakeExecReader(), nil, nil, nil)

	views, err := reads.List(context.Background(), "org1", "proj1", "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	byNum := viewsByNumber(t, views)
	if _, ok := byNum[30]; ok {
		t.Error("List returned the validation task; list reads must exclude it")
	}
	if _, ok := byNum[10]; !ok {
		t.Error("List dropped the sibling coding task")
	}

	// The version-scoped list (the build's per-version task read) excludes it
	// too — the validation issue carries the same spec tag as the coding tasks.
	views, err = reads.ListByTag(context.Background(), "org1", "proj1", "all", "v1")
	if err != nil {
		t.Fatalf("ListByTag: %v", err)
	}
	byNum = viewsByNumber(t, views)
	if _, ok := byNum[30]; ok {
		t.Error("ListByTag returned the validation task; list reads must exclude it")
	}
	if _, ok := byNum[10]; !ok {
		t.Error("ListByTag dropped the sibling coding task")
	}

	// The exclusion is a LIST rule only: Get still serves the validation task.
	detail, err := reads.Get(context.Background(), "org1", "proj1", 30)
	if err != nil {
		t.Fatalf("Get(validation): %v", err)
	}
	if detail.ExecutorClass != string(taskmeta.ClassValidation) {
		t.Errorf("Get executorClass = %q, want %q", detail.ExecutorClass, taskmeta.ClassValidation)
	}
}

// TestReads_PRURLDerivation pins TaskView.prUrl: the task's PR link, recovered
// from the succeeded coding execution's "pr#N" reason (the same recovery
// /status uses for validationUrl) — never from a running/failed row's stale
// reason, and absent (omitempty) before a PR opens.
func TestReads_PRURLDerivation(t *testing.T) {
	coding := func(status, reason string) *delivery.Execution {
		return &delivery.Execution{Kind: string(taskmeta.KindCoding), Status: status, Reason: reason}
	}
	cases := []struct {
		name string
		exec *delivery.Execution
		want string
	}{
		{
			name: "succeeded coding run stamped pr#42 → PR link",
			exec: coding(string(taskmeta.ExecSucceeded), taskmeta.ReasonPROpenPrefix+"42"),
			want: "https://github.com/o/r/pull/42",
		},
		{name: "no coding execution → empty", exec: nil, want: ""},
		{
			name: "succeeded without a pr# reason → empty",
			exec: coding(string(taskmeta.ExecSucceeded), ""),
			want: "",
		},
		{
			name: "running row with a stale pr# reason → empty",
			exec: coding(string(taskmeta.ExecRunning), taskmeta.ReasonPROpenPrefix+"42"),
			want: "",
		},
		{
			name: "failed row with a stale pr# reason → empty",
			exec: coding(string(taskmeta.ExecFailed), taskmeta.ReasonPROpenPrefix+"42"),
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := newFakeIssues()
			issues.seed(taggedIssue(10, "order-service", "v1"))
			execs := newFakeExecReader()
			if tc.exec != nil {
				execs.put(10, *tc.exec)
			}
			reads := NewReads(issues, fakeRepos{repo: defaultRepo()}, execs, nil, nil, nil)

			views, err := reads.List(context.Background(), "org1", "proj1", "open")
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if got := viewsByNumber(t, views)[10].PRURL; got != tc.want {
				t.Errorf("List prUrl = %q, want %q", got, tc.want)
			}
			detail, err := reads.Get(context.Background(), "org1", "proj1", 10)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if detail.PRURL != tc.want {
				t.Errorf("Get prUrl = %q, want %q", detail.PRURL, tc.want)
			}
			if tc.want == "" {
				b, err := json.Marshal(detail.TaskView)
				if err != nil {
					t.Fatalf("marshal view: %v", err)
				}
				if strings.Contains(string(b), "prUrl") {
					t.Errorf("prUrl key present in JSON despite no PR: %s", b)
				}
			}
		})
	}
}

// TestReads_PRURLForValidationTask covers the consumer this field was added
// for: the console's validation log page links to the validation PR via
// Get(validation issue).prUrl.
func TestReads_PRURLForValidationTask(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(validationIssue(30, "v1"))
	execs := newFakeExecReader().put(30, delivery.Execution{
		Kind:   string(taskmeta.KindCoding),
		Status: string(taskmeta.ExecSucceeded),
		Reason: taskmeta.ReasonPROpenPrefix + "7",
	})
	reads := NewReads(issues, fakeRepos{repo: defaultRepo()}, execs, nil, nil, nil)

	detail, err := reads.Get(context.Background(), "org1", "proj1", 30)
	if err != nil {
		t.Fatalf("Get(validation): %v", err)
	}
	if want := "https://github.com/o/r/pull/7"; detail.PRURL != want {
		t.Errorf("validation prUrl = %q, want %q", detail.PRURL, want)
	}
}

// ---- dependency-gated on_hold reconciliation (issue #164 follow-up) ---------

// newReadsWithDesign wires a read path with a DesignReader so the second pass
// resolves provision / org-service deps.
func newReadsWithDesign(issues *fakeIssues, execs *fakeExecReader, design fakeDesign) *Reads {
	return NewReads(issues, fakeRepos{repo: defaultRepo()}, execs, nil, design, nil)
}

func viewsByNumber(t *testing.T, views []delivery.TaskView) map[int]delivery.TaskView {
	t.Helper()
	m := map[int]delivery.TaskView{}
	for _, v := range views {
		m[v.IssueNumber] = v
	}
	return m
}

// TestReconcileBlocked_UnmetProvisionDepDerivesOnHold: a coding Task with a
// queued (not running) coding execution — which today derives the misleading
// in_progress — flips to on_hold and names the blocking provision dep when that
// dep's aep:provision gate is not deployed.
func TestReconcileBlocked_UnmetProvisionDepDerivesOnHold(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(10, "order-service", "design-v1")) // consumer coding task
	issues.seed(provisionGateIssue(20, "payments-db"))          // gate, no exec → pending (not deployed)

	execs := newFakeExecReader()
	// A queued coding row: admitted but gated → derives in_progress today.
	execs.put(10, delivery.Execution{ID: "c10", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecQueued), CreatedAt: time.Now()})

	design := fakeDesign{provision: map[string][]string{"order-service": {"payments-db"}}}
	views, err := newReadsWithDesign(issues, execs, design).List(context.Background(), "org1", "proj1", "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := viewsByNumber(t, views)[10]
	if got.DerivedStatus != string(taskmeta.StatusOnHold) {
		t.Fatalf("consumer status = %q, want on_hold", got.DerivedStatus)
	}
	if len(got.BlockedBy) != 1 || got.BlockedBy[0] != "payments-db" {
		t.Errorf("BlockedBy = %v, want [payments-db]", got.BlockedBy)
	}
}

// TestReconcileBlocked_AllDepsDeployedUnchanged: when every dep is deployed the
// consumer keeps its underlying derived status and carries no BlockedBy.
func TestReconcileBlocked_AllDepsDeployedUnchanged(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(10, "order-service", "design-v1"))
	issues.seed(provisionGateIssue(20, "payments-db"))

	execs := newFakeExecReader()
	execs.put(10, delivery.Execution{ID: "c10", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecQueued), CreatedAt: time.Now()})
	// The gate resolved: a succeeded provision execution → derives deployed.
	execs.put(20, delivery.Execution{ID: "p20", Kind: string(taskmeta.KindProvision), Status: string(taskmeta.ExecSucceeded), CreatedAt: time.Now()})

	design := fakeDesign{provision: map[string][]string{"order-service": {"payments-db"}}}
	views, err := newReadsWithDesign(issues, execs, design).List(context.Background(), "org1", "proj1", "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := viewsByNumber(t, views)[10]
	if got.DerivedStatus != string(taskmeta.StatusInProgress) {
		t.Errorf("consumer status = %q, want in_progress (deps all deployed)", got.DerivedStatus)
	}
	if len(got.BlockedBy) != 0 {
		t.Errorf("BlockedBy = %v, want empty when nothing blocks", got.BlockedBy)
	}
}

// TestReconcileBlocked_RunningCodingNotOverridden: a genuinely running coding
// Task keeps in_progress even with an unmet dep — running is never overridden.
func TestReconcileBlocked_RunningCodingNotOverridden(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(10, "order-service", "design-v1"))
	issues.seed(provisionGateIssue(20, "payments-db")) // not deployed

	execs := newFakeExecReader()
	execs.put(10, delivery.Execution{ID: "c10", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecRunning), CreatedAt: time.Now()})

	design := fakeDesign{provision: map[string][]string{"order-service": {"payments-db"}}}
	views, err := newReadsWithDesign(issues, execs, design).List(context.Background(), "org1", "proj1", "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := viewsByNumber(t, views)[10]
	if got.DerivedStatus != string(taskmeta.StatusInProgress) {
		t.Errorf("running task status = %q, want in_progress (not overridden)", got.DerivedStatus)
	}
	if len(got.BlockedBy) != 0 {
		t.Errorf("running task must carry no BlockedBy, got %v", got.BlockedBy)
	}
}

// TestReconcileBlocked_DepWithNoGateDoesNotBlock: a resolved org-service dep and
// an already-ready provision dep — neither of which has an aep:provision gate
// issue — must NOT block the consumer (the funnel's conditional-gate rule).
func TestReconcileBlocked_DepWithNoGateDoesNotBlock(t *testing.T) {
	issues := newFakeIssues()
	issues.seed(gitrepoIssue(10, "order-service", "design-v1")) // no gate issues seeded

	execs := newFakeExecReader()
	execs.put(10, delivery.Execution{ID: "c10", Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecQueued), CreatedAt: time.Now()})

	design := fakeDesign{
		orgService: map[string][]string{"order-service": {"catalog-api"}}, // resolved, no gate
		provision:  map[string][]string{"order-service": {"ready-cache"}}, // ready, no gate
	}
	views, err := newReadsWithDesign(issues, execs, design).List(context.Background(), "org1", "proj1", "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := viewsByNumber(t, views)[10]
	if got.DerivedStatus != string(taskmeta.StatusInProgress) {
		t.Errorf("consumer status = %q, want in_progress (no gate → not blocked)", got.DerivedStatus)
	}
	if len(got.BlockedBy) != 0 {
		t.Errorf("BlockedBy = %v, want empty (deps have no gate)", got.BlockedBy)
	}
}
