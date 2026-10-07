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

package getreport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/ops"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

type fakeRepo struct {
	byID map[string]*ops.RcaAgentReport
}

func (f *fakeRepo) Get(_ context.Context, _, id string) (*ops.RcaAgentReport, error) {
	return f.byID[id], nil
}
func (f *fakeRepo) List(context.Context, string, string, int) ([]ops.RcaAgentReport, string, error) {
	return nil, "", nil
}

// fakeExecs records what the slice asked for, so the tests can pin that the
// repo slug and issue number are derived correctly.
type fakeExecs struct {
	perKind  map[string]ops.ExecutionFact
	err      error
	gotRepo  string
	gotIssue int
}

func (f *fakeExecs) LatestExecutionPerKind(_ context.Context, _, repo string, issueNumber int) (map[string]ops.ExecutionFact, error) {
	f.gotRepo, f.gotIssue = repo, issueNumber
	return f.perKind, f.err
}

func ctxWithOrg(org string) context.Context {
	return tenant.WithBoundOrg(context.Background(), org)
}

func issueReport(id string) *ops.RcaAgentReport {
	n := int64(42)
	return &ops.RcaAgentReport{
		ID: id, OrgID: "acme", Project: "proj", Title: "t", Summary: "s",
		Classification: "code-level", Diagnosis: "d",
		IssueNumber: &n,
		IssueURL:    "https://github.com/acme/proj/issues/42",
	}
}

func get(t *testing.T, h *Handler, id string) gen.RcaAgentReport {
	t.Helper()
	resp, err := h.GetRcaAgentReport(ctxWithOrg("acme"), gen.GetRcaAgentReportRequestObject{ReportID: id})
	if err != nil {
		t.Fatalf("GetRcaAgentReport: %v", err)
	}
	out, ok := resp.(gen.GetRcaAgentReport200JSONResponse)
	if !ok {
		t.Fatalf("response = %T, want 200", resp)
	}
	return gen.RcaAgentReport(out)
}

// TestGetReport_correlatesExecutionState pins the read-time reconciliation: a
// report written before the agent was dispatched must not keep serving that
// stale snapshot.
func TestGetReport_correlatesExecutionState(t *testing.T) {
	ended := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	repo := &fakeRepo{byID: map[string]*ops.RcaAgentReport{"r1": issueReport("r1")}}
	execs := &fakeExecs{perKind: map[string]ops.ExecutionFact{
		string(taskmeta.KindCoding): {Kind: string(taskmeta.KindCoding), Status: string(taskmeta.ExecSucceeded)},
		string(taskmeta.KindBuild):  {Kind: string(taskmeta.KindBuild), Status: string(taskmeta.ExecSucceeded), EndedAt: &ended},
	}}

	got := get(t, New(repo, execs), "r1")

	if !got.Dispatched {
		t.Error("Dispatched = false, want true (a coding execution exists)")
	}
	if !got.Deployed {
		t.Error("Deployed = false, want true (the build execution succeeded)")
	}
	if got.DeployedAt == nil || !got.DeployedAt.Equal(ended) {
		t.Errorf("DeployedAt = %v, want the build's EndedAt %v", got.DeployedAt, ended)
	}
	// The slug the executions store is keyed by, parsed out of the issue URL.
	if execs.gotRepo != "acme/proj" || execs.gotIssue != 42 {
		t.Errorf("looked up (%q, %d), want (acme/proj, 42)", execs.gotRepo, execs.gotIssue)
	}
}

// TestGetReport_deployedNeedsSuccess pins the "Verify Fix" threshold: a build
// that ran but did not succeed is NOT deployed. Getting this wrong would tell a
// user their incident is fixed when it is not.
func TestGetReport_deployedNeedsSuccess(t *testing.T) {
	repo := &fakeRepo{byID: map[string]*ops.RcaAgentReport{"r1": issueReport("r1")}}
	execs := &fakeExecs{perKind: map[string]ops.ExecutionFact{
		string(taskmeta.KindBuild): {Kind: string(taskmeta.KindBuild), Status: string(taskmeta.ExecFailed)},
	}}

	if got := get(t, New(repo, execs), "r1"); got.Deployed {
		t.Error("Deployed = true for a FAILED build — the threshold is success, not merely a build")
	}
}

// TestGetReport_correlationIsMonotonic pins false→true only: a writer that
// already knew the agent was dispatched must never be contradicted by a missing
// execution row.
func TestGetReport_correlationIsMonotonic(t *testing.T) {
	stored := issueReport("r1")
	stored.Dispatched = true
	stored.Deployed = true
	repo := &fakeRepo{byID: map[string]*ops.RcaAgentReport{"r1": stored}}

	got := get(t, New(repo, &fakeExecs{perKind: map[string]ops.ExecutionFact{}}), "r1")

	if !got.Dispatched || !got.Deployed {
		t.Errorf("correlation downgraded a stored true: Dispatched=%v Deployed=%v", got.Dispatched, got.Deployed)
	}
}

// TestGetReport_correlationIsBestEffort: a reader failure serves the snapshot
// rather than failing the read.
func TestGetReport_correlationIsBestEffort(t *testing.T) {
	repo := &fakeRepo{byID: map[string]*ops.RcaAgentReport{"r1": issueReport("r1")}}
	execs := &fakeExecs{err: errors.New("executions unavailable")}

	if got := get(t, New(repo, execs), "r1"); got.Dispatched {
		t.Error("a correlation error must leave the stored snapshot untouched")
	}
}

func TestGetReport_noReader_servesStoredSnapshot(t *testing.T) {
	repo := &fakeRepo{byID: map[string]*ops.RcaAgentReport{"r1": issueReport("r1")}}

	// nil reader = correlation disabled; must not panic.
	if got := get(t, New(repo, nil), "r1"); got.Dispatched || got.Deployed {
		t.Error("correlation ran with a nil reader")
	}
}

// TestGetReport_noIssueSkipsCorrelation: a config-only handoff has no linked
// issue, so there is nothing to correlate against.
func TestGetReport_noIssueSkipsCorrelation(t *testing.T) {
	r := issueReport("r1")
	r.IssueNumber = nil
	repo := &fakeRepo{byID: map[string]*ops.RcaAgentReport{"r1": r}}
	execs := &fakeExecs{}

	get(t, New(repo, execs), "r1")

	if execs.gotRepo != "" {
		t.Errorf("correlated a report with no linked issue (looked up %q)", execs.gotRepo)
	}
}

func TestGetReport_NotFound(t *testing.T) {
	_, err := New(&fakeRepo{byID: map[string]*ops.RcaAgentReport{}}, nil).
		GetRcaAgentReport(ctxWithOrg("acme"), gen.GetRcaAgentReportRequestObject{ReportID: "nope"})

	var ae *apierr.Error
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("err = %v, want a 404 apierr", err)
	}
}

func TestRepoFromIssueURL(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/proj/issues/42": "acme/proj",
		"https://github.com/acme/proj":           "acme/proj",
		"https://gitlab.com/acme/proj/issues/1":  "",
		"not a url":                              "",
		"":                                       "",
		"https://github.com/acme":                "",
	}
	for in, want := range cases {
		if got := repoFromIssueURL(in); got != want {
			t.Errorf("repoFromIssueURL(%q) = %q, want %q", in, got, want)
		}
	}
}
