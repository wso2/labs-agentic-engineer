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

package execution

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/feature/gitrepo"
	"github.com/wso2/aep/aep-api/models"
)

// provisionIssue builds an aep:provision gate issue for a dependency name.
func provisionIssue(number int, depName, gateKind, state string) gitrepo.IssueInfo {
	block := taskmeta.Block{
		Component: depName,
		GateKind:  gateKind,
		Origin:    taskmeta.OriginSpecPlan,
		DesignTag: "design-v1",
	}
	body := taskmeta.ComposeBody(block, taskmeta.Human{Rationale: "provision " + depName})
	return gitrepo.IssueInfo{
		Number: number,
		Title:  "Provision " + depName,
		Body:   body,
		State:  state,
		URL:    fmt.Sprintf("https://github.com/o/r/issues/%d", number),
		Labels: taskmeta.NewTaskLabels(taskmeta.ClassProvision, taskmeta.OriginSpecPlan),
	}
}

// newTestFunnelP is newTestFunnel with per-component provisioning deps supplied
// (the design-augmented half of the dependency-kind-aware gate).
func newTestFunnelP(store *fakeStore, issues *fakeIssues, names map[string]bool, provisionDep map[string][]string, exec Executor) *Funnel {
	reg := NewRegistry()
	if exec != nil {
		reg.Register(taskmeta.ClassCoding, exec)
	}
	return NewFunnel(store, issues, fakeRepos{orgID: "org1", projectID: "proj1"},
		fakeDesign{names: names, provisionDep: provisionDep}, reg)
}

// markProvisionDeployed seeds a succeeded provision Execution so the gate issue
// derives StatusDeployed.
func markProvisionDeployed(store *fakeStore, issueNumber int, depName string) {
	ctx := context.Background()
	_, p, _ := store.TryAdmit(ctx, &models.Execution{
		Repo: "o/r", IssueNumber: issueNumber, Kind: string(taskmeta.KindProvision), Component: depName,
	})
	_, _ = store.Finish(ctx, p.ID, string(taskmeta.ExecSucceeded), "")
}

func TestFunnel_ProvisionDepDeployed_Dispatches(t *testing.T) {
	store := newFakeStore()
	markProvisionDeployed(store, 1, "stripe") // external dep provisioned

	issues := newFakeIssues([]gitrepo.IssueInfo{
		provisionIssue(1, "stripe", taskmeta.GateConfigCollection, "open"),
		taskIssue(2, "order-service", nil, []string{taskmeta.LabelExecute}, "open"),
	})
	exec := &fakeExecutor{store: store, startOK: true}
	f := newTestFunnelP(store, issues,
		map[string]bool{"order-service": true},
		map[string][]string{"order-service": {"stripe"}}, exec)

	if err := f.OnExecuteIntent(context.Background(), "o/r", 2); err != nil {
		t.Fatalf("OnExecuteIntent: %v", err)
	}
	if len(exec.got) != 1 {
		t.Fatalf("expected dispatch once the provision dep is deployed, got %d", len(exec.got))
	}
}

// The readiness watcher CLOSES the aep:provision gate issue on success (§3.6
// close-with-reference), so the dispatch gate must still see a CLOSED issue
// backed by a succeeded provision as deployed. The earlier bug derived a closed
// issue as abandoned (never deployed), hanging the consumer's dispatch forever —
// caught live on a fresh cluster, not by the "open"-issue test above.
func TestFunnel_ProvisionDepDeployed_ClosedIssue_Dispatches(t *testing.T) {
	store := newFakeStore()
	markProvisionDeployed(store, 1, "orders-db") // platform resource provisioned + issue closed

	issues := newFakeIssues([]gitrepo.IssueInfo{
		provisionIssue(1, "orders-db", taskmeta.GateResourceProvisioning, "closed"),
		taskIssue(2, "order-service", nil, []string{taskmeta.LabelExecute}, "open"),
	})
	exec := &fakeExecutor{store: store, startOK: true}
	f := newTestFunnelP(store, issues,
		map[string]bool{"order-service": true},
		map[string][]string{"order-service": {"orders-db"}}, exec)

	if err := f.OnExecuteIntent(context.Background(), "o/r", 2); err != nil {
		t.Fatalf("OnExecuteIntent: %v", err)
	}
	if len(exec.got) != 1 {
		t.Fatalf("a CLOSED provision issue backed by a succeeded run must still dispatch the consumer, got %d", len(exec.got))
	}
}

func TestFunnel_ProvisionDepPending_Queues(t *testing.T) {
	store := newFakeStore()
	// The provision issue exists but is NOT deployed (no succeeded provision row).
	issues := newFakeIssues([]gitrepo.IssueInfo{
		provisionIssue(1, "orders-db", taskmeta.GateResourceProvisioning, "open"),
		taskIssue(2, "order-service", nil, []string{taskmeta.LabelExecute}, "open"),
	})
	exec := &fakeExecutor{store: store, startOK: true}
	f := newTestFunnelP(store, issues,
		map[string]bool{"order-service": true},
		map[string][]string{"order-service": {"orders-db"}}, exec)

	if err := f.OnExecuteIntent(context.Background(), "o/r", 2); err != nil {
		t.Fatalf("OnExecuteIntent: %v", err)
	}
	if len(exec.got) != 0 {
		t.Fatalf("consumer must hold until its provision dep deploys, got %d", len(exec.got))
	}
	execs, _ := store.LatestPerKind(context.Background(), "o/r", 2)
	if c := execs[string(taskmeta.KindCoding)]; c == nil || c.Status != string(taskmeta.ExecQueued) {
		t.Errorf("expected queued coding execution, got %+v", c)
	}
}

func TestFunnel_ProvisionIssueMissing_Queues(t *testing.T) {
	store := newFakeStore()
	// The design declares a provision dep but no aep:provision issue exists yet —
	// the consumer must hold (unmet), not dispatch blindly.
	issues := newFakeIssues([]gitrepo.IssueInfo{
		taskIssue(2, "order-service", nil, []string{taskmeta.LabelExecute}, "open"),
	})
	exec := &fakeExecutor{store: store, startOK: true}
	f := newTestFunnelP(store, issues,
		map[string]bool{"order-service": true},
		map[string][]string{"order-service": {"stripe"}}, exec)

	if err := f.OnExecuteIntent(context.Background(), "o/r", 2); err != nil {
		t.Fatalf("OnExecuteIntent: %v", err)
	}
	if len(exec.got) != 0 {
		t.Fatalf("consumer must hold when no provision issue exists yet, got %d", len(exec.got))
	}
}

func TestFunnel_ProvisionDep_ReleasedOnReevaluate(t *testing.T) {
	store := newFakeStore()
	issues := newFakeIssues([]gitrepo.IssueInfo{
		provisionIssue(1, "orders-db", taskmeta.GateResourceProvisioning, "open"),
		taskIssue(2, "order-service", nil, []string{taskmeta.LabelExecute}, "open"),
	})
	exec := &fakeExecutor{store: store, startOK: true}
	f := newTestFunnelP(store, issues,
		map[string]bool{"order-service": true},
		map[string][]string{"order-service": {"orders-db"}}, exec)

	_ = f.OnExecuteIntent(context.Background(), "o/r", 2) // queues behind the pending provision
	if len(exec.got) != 0 {
		t.Fatalf("must queue while provision pending, got %d", len(exec.got))
	}
	// The readiness watcher finishes the provision run → Reevaluate releases it.
	markProvisionDeployed(store, 1, "orders-db")
	if err := f.Reevaluate(context.Background()); err != nil {
		t.Fatalf("Reevaluate: %v", err)
	}
	if len(exec.got) != 1 {
		t.Fatalf("Reevaluate must release the consumer once its provision dep deploys, got %d", len(exec.got))
	}
}

func TestFunnel_ExecuteOnProvisionIssue_NoOp(t *testing.T) {
	store := newFakeStore()
	issues := newFakeIssues([]gitrepo.IssueInfo{
		provisionIssue(1, "stripe", taskmeta.GateConfigCollection, "open"),
	})
	// A stray aep:execute on a provision issue.
	issues.list[0].Labels = append(issues.list[0].Labels, taskmeta.LabelExecute)
	exec := &fakeExecutor{store: store, startOK: true}
	f := newTestFunnelP(store, issues, map[string]bool{}, nil, exec)

	if err := f.OnExecuteIntent(context.Background(), "o/r", 1); err != nil {
		t.Fatalf("OnExecuteIntent: %v", err)
	}
	if len(exec.got) != 0 {
		t.Fatalf("aep:execute on a provision issue must not dispatch, got %d", len(exec.got))
	}
	// No execution row admitted (provisioning is drawer-driven).
	rows, _ := store.ListByIssue(context.Background(), "o/r", 1)
	if len(rows) != 0 {
		t.Fatalf("no execution row must be admitted for an aep:execute on a provision issue, got %d", len(rows))
	}
	// The stray label is consumed.
	if got := issues.removed[1]; len(got) == 0 || got[0] != taskmeta.LabelExecute {
		t.Errorf("expected aep:execute consumed on the provision issue, got %v", got)
	}
	// A one-time guidance comment is posted, pointing at the design page.
	if got := issues.comments[1]; len(got) != 1 {
		t.Fatalf("expected exactly one provision-gate notice comment, got %d: %v", len(got), got)
	} else if !strings.Contains(got[0], "dependency panel") {
		t.Errorf("notice comment does not point at the dependency panel: %q", got[0])
	}
	// ...and the one-time marker is stamped so a repeat stamp stays quiet.
	if got := issues.added[1]; len(got) != 1 || got[0] != taskmeta.LabelProvisionNoted {
		t.Errorf("expected aep:provision-noted stamped, got %v", got)
	}
}

// TestFunnel_ExecuteOnProvisionIssue_NoDuplicateNotice pins the one-time
// guarantee: a provision gate already carrying aep:provision-noted (a prior
// Execute stamp already explained it) does not get a second comment — the label
// is still consumed and no row is admitted.
func TestFunnel_ExecuteOnProvisionIssue_NoDuplicateNotice(t *testing.T) {
	store := newFakeStore()
	issues := newFakeIssues([]gitrepo.IssueInfo{
		provisionIssue(1, "stripe", taskmeta.GateConfigCollection, "open"),
	})
	// A repeated stray aep:execute on a gate that was already explained once.
	issues.list[0].Labels = append(issues.list[0].Labels, taskmeta.LabelExecute, taskmeta.LabelProvisionNoted)
	exec := &fakeExecutor{store: store, startOK: true}
	f := newTestFunnelP(store, issues, map[string]bool{}, nil, exec)

	if err := f.OnExecuteIntent(context.Background(), "o/r", 1); err != nil {
		t.Fatalf("OnExecuteIntent: %v", err)
	}
	if len(exec.got) != 0 {
		t.Fatalf("aep:execute on a provision issue must not dispatch, got %d", len(exec.got))
	}
	// No duplicate comment on the already-noted gate.
	if got := issues.comments[1]; len(got) != 0 {
		t.Errorf("expected no duplicate notice on an already-noted gate, got %v", got)
	}
	// The stray label is still consumed.
	if got := issues.removed[1]; len(got) == 0 || got[0] != taskmeta.LabelExecute {
		t.Errorf("expected aep:execute consumed, got %v", got)
	}
	// No execution row admitted.
	rows, _ := store.ListByIssue(context.Background(), "o/r", 1)
	if len(rows) != 0 {
		t.Fatalf("no execution row must be admitted, got %d", len(rows))
	}
}
