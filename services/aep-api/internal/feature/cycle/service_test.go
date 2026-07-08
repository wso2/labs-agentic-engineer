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

package cycle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/feature/cycle"
	"github.com/wso2/aep/aep-api/models"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

// --- fakes -----------------------------------------------------------------

type signalCall struct{ wfID, name string }

type fakeClient struct {
	startInput contract.DevelopmentFlowInput
	signals    []signalCall
	queryState contract.CycleStateView
}

func (f *fakeClient) StartCycle(_ context.Context, in contract.DevelopmentFlowInput) (string, error) {
	f.startInput = in
	return contract.DevFlowWorkflowID(in.Org, in.Project, in.CycleID), nil
}

func (f *fakeClient) Signal(_ context.Context, wfID, name string, _ any) error {
	f.signals = append(f.signals, signalCall{wfID, name})
	return nil
}

func (f *fakeClient) QueryCycle(_ context.Context, _ string) (contract.CycleStateView, error) {
	return f.queryState, nil
}

type fakeRepo struct{ rows []*models.DevelopmentCycle }

func (r *fakeRepo) Ensure(_ context.Context, c *models.DevelopmentCycle) (*models.DevelopmentCycle, error) {
	for _, e := range r.rows {
		if e.WorkflowID == c.WorkflowID {
			return e, nil
		}
	}
	if c.Status == "" {
		c.Status = string(models.CycleActive)
	}
	r.rows = append(r.rows, c)
	return c, nil
}

func (r *fakeRepo) GetByWorkflowID(_ context.Context, wfID string) (*models.DevelopmentCycle, error) {
	for _, e := range r.rows {
		if e.WorkflowID == wfID {
			return e, nil
		}
	}
	return nil, nil
}

func (r *fakeRepo) ListByProject(_ context.Context, org, proj string) ([]*models.DevelopmentCycle, error) {
	var out []*models.DevelopmentCycle
	for i := len(r.rows) - 1; i >= 0; i-- { // newest first
		if e := r.rows[i]; e.OrgID == org && e.ProjectID == proj {
			out = append(out, e)
		}
	}
	return out, nil
}

func (r *fakeRepo) SetStatus(_ context.Context, wfID string, status models.CycleStatus) (*models.DevelopmentCycle, error) {
	for _, e := range r.rows {
		if e.WorkflowID == wfID {
			e.Status = string(status)
			return e, nil
		}
	}
	return nil, nil
}

// --- tests -----------------------------------------------------------------

// TestDisabledServiceDegradesGracefully: with no Temporal client the save-path
// hooks no-op (never fail the save) and the console surface reports disabled.
func TestDisabledServiceDegradesGracefully(t *testing.T) {
	t.Parallel()
	svc := cycle.NewService(nil, &fakeRepo{})
	ctx := context.Background()

	if err := svc.OnRequirementsApproved(ctx, "acme", "web", "v1"); err != nil {
		t.Errorf("OnRequirementsApproved (disabled) = %v, want nil", err)
	}
	if err := svc.OnDesignApproved(ctx, "acme", "web", 1); err != nil {
		t.Errorf("OnDesignApproved (disabled) = %v, want nil", err)
	}
	if _, err := svc.GetFlowState(ctx, "acme", "web"); !errors.Is(err, cycle.ErrOrchestrationDisabled) {
		t.Errorf("GetFlowState (disabled) = %v, want ErrOrchestrationDisabled", err)
	}
	if err := svc.Signal(ctx, "acme", "web", contract.SignalMarkComplete); !errors.Is(err, cycle.ErrOrchestrationDisabled) {
		t.Errorf("Signal (disabled) = %v, want ErrOrchestrationDisabled", err)
	}
}

// TestOnRequirementsApprovedStartsCycle: requirements approval starts the cycle
// for that version in the design phase and records the read-model row.
func TestOnRequirementsApprovedStartsCycle(t *testing.T) {
	t.Parallel()
	fc, fr := &fakeClient{}, &fakeRepo{}
	svc := cycle.NewService(fc, fr)

	if err := svc.OnRequirementsApproved(context.Background(), "acme", "web", "v3"); err != nil {
		t.Fatalf("OnRequirementsApproved: %v", err)
	}
	if fc.startInput.CycleID != "v3" || fc.startInput.StartPhase != contract.PhaseDesign {
		t.Errorf("start input = %+v, want CycleID=v3 StartPhase=design", fc.startInput)
	}
	if fc.startInput.Source != contract.SourceRequirement {
		t.Errorf("start source = %q, want requirement", fc.startInput.Source)
	}
	wantWf := contract.DevFlowWorkflowID("acme", "web", "v3")
	if len(fr.rows) != 1 || fr.rows[0].WorkflowID != wantWf {
		t.Errorf("expected one cycle row for %s, got %+v", wantWf, fr.rows)
	}
}

// TestOnDesignApprovedSignalsApproveDesign: design approval advances the
// requirement version's cycle via ApproveDesign.
func TestOnDesignApprovedSignalsApproveDesign(t *testing.T) {
	t.Parallel()
	fc := &fakeClient{}
	svc := cycle.NewService(fc, &fakeRepo{})

	if err := svc.OnDesignApproved(context.Background(), "acme", "web", 2); err != nil {
		t.Fatalf("OnDesignApproved: %v", err)
	}
	wantWf := contract.DevFlowWorkflowID("acme", "web", "v2")
	if len(fc.signals) != 1 || fc.signals[0].wfID != wantWf || fc.signals[0].name != contract.SignalApproveDesign {
		t.Errorf("signals = %+v, want one ApproveDesign to %s", fc.signals, wantWf)
	}
}

// TestGateSignalResolvesActiveCycle: a console gate signal routes to the newest
// cycle; with no cycle it reports ErrNoActiveCycle.
func TestGateSignalResolvesActiveCycle(t *testing.T) {
	t.Parallel()
	fc, fr := &fakeClient{}, &fakeRepo{}
	svc := cycle.NewService(fc, fr)
	ctx := context.Background()

	if err := svc.Signal(ctx, "acme", "web", contract.SignalMarkComplete); !errors.Is(err, cycle.ErrNoActiveCycle) {
		t.Errorf("Signal with no cycle = %v, want ErrNoActiveCycle", err)
	}

	// Seed two cycles; the newest (v2) is the one the console acts on.
	_, _ = fr.Ensure(ctx, &models.DevelopmentCycle{OrgID: "acme", ProjectID: "web", WorkflowID: contract.DevFlowWorkflowID("acme", "web", "v1")})
	_, _ = fr.Ensure(ctx, &models.DevelopmentCycle{OrgID: "acme", ProjectID: "web", WorkflowID: contract.DevFlowWorkflowID("acme", "web", "v2")})

	if err := svc.Signal(ctx, "acme", "web", contract.SignalMarkComplete); err != nil {
		t.Fatalf("Signal: %v", err)
	}
	wantWf := contract.DevFlowWorkflowID("acme", "web", "v2")
	last := fc.signals[len(fc.signals)-1]
	if last.wfID != wantWf || last.name != contract.SignalMarkComplete {
		t.Errorf("gate signal = %+v, want MarkComplete to %s", last, wantWf)
	}
}

// TestGetFlowStateQueriesActiveCycle: flow-state reads the newest cycle's query.
func TestGetFlowStateQueriesActiveCycle(t *testing.T) {
	t.Parallel()
	fc := &fakeClient{queryState: contract.CycleStateView{Phase: contract.PhaseImplement}}
	fr := &fakeRepo{}
	svc := cycle.NewService(fc, fr)
	ctx := context.Background()
	_, _ = fr.Ensure(ctx, &models.DevelopmentCycle{OrgID: "acme", ProjectID: "web", WorkflowID: contract.DevFlowWorkflowID("acme", "web", "v1")})

	st, err := svc.GetFlowState(ctx, "acme", "web")
	if err != nil {
		t.Fatalf("GetFlowState: %v", err)
	}
	if st == nil || st.Phase != contract.PhaseImplement {
		t.Errorf("flow state = %+v, want phase=implement", st)
	}
}

// TestOnIssueTaskOpened_BootstrapsFirstCycle: an issue-first project (no
// existing cycle row) starts one directly at PhaseImplement via SourceIssue.
func TestOnIssueTaskOpened_BootstrapsFirstCycle(t *testing.T) {
	t.Parallel()
	fc, fr := &fakeClient{}, &fakeRepo{}
	svc := cycle.NewService(fc, fr)

	if err := svc.OnIssueTaskOpened(context.Background(), "acme", "web", 42); err != nil {
		t.Fatalf("OnIssueTaskOpened: %v", err)
	}
	if fc.startInput.Source != contract.SourceIssue {
		t.Errorf("start source = %q, want issue", fc.startInput.Source)
	}
	if fc.startInput.StartPhase != contract.PhaseImplement {
		t.Errorf("start phase = %q, want implement", fc.startInput.StartPhase)
	}
	if fc.startInput.CycleID != "issue-42" {
		t.Errorf("cycle id = %q, want issue-42", fc.startInput.CycleID)
	}
	wantWf := contract.DevFlowWorkflowID("acme", "web", "issue-42")
	if len(fr.rows) != 1 || fr.rows[0].WorkflowID != wantWf {
		t.Errorf("expected one cycle row for %s, got %+v", wantWf, fr.rows)
	}
}

// TestOnIssueTaskOpened_SkipsWhenCycleExists: a project that already has a
// cycle (from a prior requirements approval, or a prior fast-path bootstrap)
// never re-triggers — the fast path only bootstraps an issue-first project
// once.
func TestOnIssueTaskOpened_SkipsWhenCycleExists(t *testing.T) {
	t.Parallel()
	fc, fr := &fakeClient{}, &fakeRepo{}
	svc := cycle.NewService(fc, fr)
	ctx := context.Background()

	if err := svc.OnRequirementsApproved(ctx, "acme", "web", "v1"); err != nil {
		t.Fatalf("OnRequirementsApproved: %v", err)
	}
	fc.startInput = contract.DevelopmentFlowInput{} // reset so a second call would be visible

	if err := svc.OnIssueTaskOpened(ctx, "acme", "web", 42); err != nil {
		t.Fatalf("OnIssueTaskOpened: %v", err)
	}
	if fc.startInput != (contract.DevelopmentFlowInput{}) {
		t.Errorf("OnIssueTaskOpened should not start a second cycle, got %+v", fc.startInput)
	}
	if len(fr.rows) != 1 {
		t.Errorf("expected still exactly one cycle row, got %+v", fr.rows)
	}
}

// TestOnIssueTaskOpened_DisabledIsNoop mirrors TestDisabledServiceDegradesGracefully.
func TestOnIssueTaskOpened_DisabledIsNoop(t *testing.T) {
	t.Parallel()
	svc := cycle.NewService(nil, &fakeRepo{})
	if err := svc.OnIssueTaskOpened(context.Background(), "acme", "web", 1); err != nil {
		t.Errorf("OnIssueTaskOpened (disabled) = %v, want nil", err)
	}
}
