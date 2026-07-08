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

// Package cycle drives the Temporal DevelopmentFlowWorkflow from aep-api's
// development-flow surface. A cycle is anchored to a requirement version: when
// requirements are approved (the v<N> tag cut) the cycle starts in the design
// phase; when the design is approved it advances to implement; console gate
// buttons and the flow-state read map to workflow signals + queries. The BFF is
// a dial-only client — the workflow itself runs in services/orchestrator.
package cycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/wso2/aep/aep-api/models"
	"github.com/wso2/aep/aep-api/repositories"
	contract "github.com/wso2/labs-agentic-engineer/packages/contracts/orchestration"
)

// ErrOrchestrationDisabled is returned by the read/signal surface when the BFF
// booted without a reachable Temporal (dial failed) — the feature is off.
var ErrOrchestrationDisabled = errors.New("orchestration is disabled (no Temporal connection)")

// ErrNoActiveCycle is returned when a project has no development cycle to
// signal or query yet.
var ErrNoActiveCycle = errors.New("no active development cycle for project")

// WorkflowClient is the narrow Temporal control surface the cycle service needs.
// The dial-only orchestration client satisfies it; tests supply a fake. Keeping
// it a consumer-side port here means cycle does not import the orchestration
// feature package (only the shared contract module for the DTOs).
type WorkflowClient interface {
	StartCycle(ctx context.Context, in contract.DevelopmentFlowInput) (string, error)
	Signal(ctx context.Context, workflowID, signalName string, arg any) error
	QueryCycle(ctx context.Context, workflowID string) (contract.CycleStateView, error)
}

// Service coordinates the BFF's view of development cycles: it starts/advances
// the workflow from the requirement/design approval hooks and resolves a
// project's active cycle for the console's signal + flow-state surface.
type Service struct {
	client WorkflowClient // nil when Temporal is unreachable → feature disabled
	cycles repositories.DevelopmentCycleRepository
}

// NewService builds the cycle service. A nil client is allowed (orchestration
// disabled); every method degrades gracefully in that case. Callers MUST pass a
// true nil interface (not a typed-nil concrete pointer) to disable — see the
// composition root.
func NewService(client WorkflowClient, cycles repositories.DevelopmentCycleRepository) *Service {
	return &Service{client: client, cycles: cycles}
}

func (s *Service) enabled() bool { return s != nil && s.client != nil }

// OnRequirementsApproved starts the development cycle for a requirement version
// (idempotently). reqTag is the freshly cut requirements tag ("v3"); it is the
// cycle ID, so a retried approval resolves to the same workflow + row. The cycle
// starts in the design phase — the requirements gate is satisfied by the tag
// cut. Best-effort: called from the requirements save path, never fatal to it.
func (s *Service) OnRequirementsApproved(ctx context.Context, orgHandle, projectName, reqTag string) error {
	if !s.enabled() {
		return nil
	}
	in := contract.DevelopmentFlowInput{
		Org:        orgHandle,
		Project:    projectName,
		CycleID:    reqTag,
		Source:     contract.SourceRequirement,
		StartPhase: contract.PhaseDesign, // requirements approved by the v<N> tag cut
		GatePolicy: contract.GatePolicy{
			Requirements: contract.GateHuman,
			Design:       contract.GateHuman,
			CodeReview:   contract.GateHuman,
		},
	}
	wfID, err := s.client.StartCycle(ctx, in)
	if err != nil {
		return fmt.Errorf("start cycle for %s/%s %s: %w", orgHandle, projectName, reqTag, err)
	}
	// Record (or confirm) the read-model row (StartCycle is idempotent, so a
	// retried approval lands the same row).
	if _, derr := s.cycles.Ensure(ctx, &models.DevelopmentCycle{
		OrgID:              orgHandle,
		ProjectID:          projectName,
		RequirementVersion: reqTag,
		WorkflowID:         wfID,
	}); derr != nil {
		return fmt.Errorf("record cycle %s: %w", wfID, derr)
	}
	slog.InfoContext(ctx, "development cycle started",
		"org", orgHandle, "project", projectName, "cycle", reqTag, "workflow", wfID)
	return nil
}

// OnIssueTaskOpened starts a development cycle via the GitHub-issue fast-path
// (§R2.1): a Task issue opened on a project that has never started a cycle
// through the requirements/design approval flow bootstraps one directly at
// PhaseImplement, skipping the requirements/design gates entirely — there is
// no requirement/design version to anchor a "v<N>" cycle ID to, so the issue
// number anchors it instead. DevelopmentFlowWorkflow needs no special-casing
// for this (confirmed by the orchestrator's TestDevFlow_IssueFastPath):
// starting at PhaseImplement already skips straight to planning tasks from
// the project's current approved design (if any).
//
// Idempotent per project: a project with ANY existing cycle row — from this
// path or a prior requirements approval — never re-triggers here. The fast
// path exists only to bootstrap an issue-first project once; a project that
// already went through requirements/design (or a prior fast-path bootstrap)
// drives further cycles through that existing cycle instead. Best-effort:
// called from the issues.opened webhook, never fatal to it.
func (s *Service) OnIssueTaskOpened(ctx context.Context, orgHandle, projectName string, issueNumber int) error {
	if !s.enabled() {
		return nil
	}
	existing, err := s.cycles.ListByProject(ctx, orgHandle, projectName)
	if err != nil {
		return fmt.Errorf("list cycles for %s/%s: %w", orgHandle, projectName, err)
	}
	if len(existing) > 0 {
		return nil // not an issue-first project — a cycle already anchors it
	}
	cycleID := fmt.Sprintf("issue-%d", issueNumber)
	in := contract.DevelopmentFlowInput{
		Org:        orgHandle,
		Project:    projectName,
		CycleID:    cycleID,
		Source:     contract.SourceIssue,
		StartPhase: contract.PhaseImplement,
		GatePolicy: contract.GatePolicy{
			Requirements: contract.GateHuman,
			Design:       contract.GateHuman,
			CodeReview:   contract.GateHuman,
		},
	}
	wfID, err := s.client.StartCycle(ctx, in)
	if err != nil {
		return fmt.Errorf("start issue-fast-path cycle for %s/%s issue#%d: %w", orgHandle, projectName, issueNumber, err)
	}
	if _, derr := s.cycles.Ensure(ctx, &models.DevelopmentCycle{
		OrgID:              orgHandle,
		ProjectID:          projectName,
		RequirementVersion: cycleID,
		WorkflowID:         wfID,
	}); derr != nil {
		return fmt.Errorf("record issue-fast-path cycle %s: %w", wfID, derr)
	}
	slog.InfoContext(ctx, "development cycle started via issue fast-path",
		"org", orgHandle, "project", projectName, "issue", issueNumber, "workflow", wfID)
	return nil
}

// OnDesignApproved advances the requirement version's cycle to implement by
// signaling ApproveDesign. reqVersion is the parent requirements version the
// design tag was cut under. Best-effort: called from the design save path.
func (s *Service) OnDesignApproved(ctx context.Context, orgHandle, projectName string, reqVersion int) error {
	if !s.enabled() {
		return nil
	}
	cycleID := fmt.Sprintf("v%d", reqVersion)
	wfID := contract.DevFlowWorkflowID(orgHandle, projectName, cycleID)
	if err := s.client.Signal(ctx, wfID, contract.SignalApproveDesign, nil); err != nil {
		return fmt.Errorf("approve-design signal to %s: %w", wfID, err)
	}
	slog.InfoContext(ctx, "development cycle design approved",
		"org", orgHandle, "project", projectName, "cycle", cycleID)
	return nil
}

// Signal sends a gate signal (by contract signal name) to a project's active
// cycle — the console button surface. Returns ErrNoActiveCycle when there is no
// cycle to signal, or ErrOrchestrationDisabled when Temporal is unreachable.
func (s *Service) Signal(ctx context.Context, orgHandle, projectName, signalName string) error {
	if !s.enabled() {
		return ErrOrchestrationDisabled
	}
	wfID, err := s.activeWorkflowID(ctx, orgHandle, projectName)
	if err != nil {
		return err
	}
	if wfID == "" {
		return ErrNoActiveCycle
	}
	return s.client.Signal(ctx, wfID, signalName, nil)
}

// GetFlowState returns a project's active cycle position (phase, gates, tasks)
// via the workflow query. Returns ErrNoActiveCycle / ErrOrchestrationDisabled as
// above.
func (s *Service) GetFlowState(ctx context.Context, orgHandle, projectName string) (*contract.CycleStateView, error) {
	if !s.enabled() {
		return nil, ErrOrchestrationDisabled
	}
	wfID, err := s.activeWorkflowID(ctx, orgHandle, projectName)
	if err != nil {
		return nil, err
	}
	if wfID == "" {
		return nil, ErrNoActiveCycle
	}
	st, err := s.client.QueryCycle(ctx, wfID)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// activeWorkflowID returns the newest cycle's workflow ID for a project, or ""
// when the project has none. Cycles are listed newest-first; the newest is the
// one the console acts on.
func (s *Service) activeWorkflowID(ctx context.Context, orgHandle, projectName string) (string, error) {
	cycles, err := s.cycles.ListByProject(ctx, orgHandle, projectName)
	if err != nil {
		return "", fmt.Errorf("list cycles for %s/%s: %w", orgHandle, projectName, err)
	}
	if len(cycles) == 0 {
		return "", nil
	}
	return cycles[0].WorkflowID, nil
}
