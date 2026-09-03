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

package codingagent

// milestone_dispatch.go — the executor's ONE dispatch entry point.
//
// A dispatch serves one CYCLE of a milestone run and writes NOTHING: the cycle
// record is the supervisor's bookkeeping, and no execution row is minted for
// agent work any more.

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// Compile-time proof the executor satisfies the run supervisor's root port.
var _ delivery.MilestoneDispatcher = (*CodingExecutor)(nil)

// milestoneComponentSentinel is the AEP_COMPONENT_NAME a milestone-loop Job
// carries. A cycle is scoped to a MILESTONE, not a component — the agent works
// every open issue in it and may touch several components — so there is no real
// component to name, and the Job/pod still needs a valid k8s label value. Same
// role as validationComponentSentinel.
//
// It is a LABEL VALUE ONLY. Nothing may resolve project content through it:
// there is no specs/design/components/aep-milestone/. The runner accordingly
// takes its applied skills from the union across every component's design.json
// (skills_resolver.ts, SkillsScope) rather than from this name.
const milestoneComponentSentinel = "aep-milestone"

// Dispatch launches ONE agent run over a milestone and returns the launched
// Job's name.
//
// It returns as soon as the Job is applied: everything after the launch — the
// pull request, the merge, the builds — reaches the supervisor as a
// webhook-derived signal, so waiting here would only hold a Temporal activity
// open for hours.
func (e *CodingExecutor) Dispatch(ctx context.Context, req delivery.MilestoneDispatch) (string, error) {
	if req.OrgID == "" || req.ProjectID == "" {
		return "", fmt.Errorf("milestone dispatch: OrgID and ProjectID are required")
	}
	if req.CycleID == "" {
		// The cycle id is the pod's correlation key (AEP_TASK_ID, the run-name
		// seed, the bearer subject). Without it the launched Job could not be
		// tied back to the cycle record that dispatched it.
		return "", fmt.Errorf("milestone dispatch: CycleID is required — it is the launched Job's correlation key")
	}

	repo, err := e.repos.GetRepo(ctx, req.OrgID, req.ProjectID)
	if err != nil || repo == nil {
		return "", fmt.Errorf("milestone dispatch: resolve project repo: %w", err)
	}

	shape, err := milestoneDispatchShape(req, repo.RepoURL)
	if err != nil {
		return "", err
	}

	// Publish the platform-resolved `endpoints:` wiring BEFORE the Job launches,
	// so the agent finds it on the issues it is about to read. This is the moment
	// the dispatch predicate has already made safe — no gate open, working set
	// non-empty — which is why it lives here and not on the gate-resolution path
	// that used to own it (see WiringPublisher). A validation cycle needs no
	// wiring: it authors tests, not workload.yaml.
	if e.wiring != nil && req.Kind != delivery.CycleKindValidation {
		e.wiring.PublishResolvedWiring(ctx, req.OrgID, req.ProjectID)
	}

	// Refresh the repo's `.claude/skills/` copies from the org library, for the
	// same reason and at the same moment as the wiring above: the clone the
	// agent is about to work in must carry the guidance its build was designed
	// against. Unlike wiring this runs for EVERY cycle kind — a validation run
	// works in the same clone, and the refresh is diff-first, so an
	// already-current repo costs a read and no commit. Best-effort: stale
	// skills degrade a build, a failed dispatch loses it entirely.
	if e.skillMirror != nil {
		if err := e.skillMirror.SyncProjectSkills(ctx, req.OrgID, req.ProjectID); err != nil {
			slog.WarnContext(ctx, "skills: project mirror refresh failed; dispatching with the copies already in the repo",
				"org", req.OrgID, "project", req.ProjectID, "error", err)
		}
	}

	return e.launchAgent(ctx, agentLaunch{
		orgID:         req.OrgID,
		projectID:     req.ProjectID,
		correlationID: req.CycleID,
		runID:         req.RunID,
		shape:         shape,
		repo:          repo,
		// No secretComponent: a cycle spans the milestone, not one component.
	})
}

// milestoneDispatchShape picks the runner's prompt, skill and deadline for a
// cycle kind.
//
// Validation is the only anchored kind: it swaps in the `aep-validation` skill
// (via AEP_TASK_KIND) and points the agent at its single issue. Every other kind
// — coding, fix, conflict — is the ordinary milestone loop, deliberately NOT
// anchored: a fix or a conflict issue is ordinary work that joins the working
// set the runner discovers for itself.
func milestoneDispatchShape(req delivery.MilestoneDispatch, repoURL string) (dispatchShape, error) {
	if req.Kind == delivery.CycleKindValidation {
		if req.IssueNumber <= 0 {
			return dispatchShape{}, fmt.Errorf("milestone dispatch: a validation cycle must name its issue")
		}
		return dispatchShape{
			prompt:          buildValidationPrompt(issueURL(repoURL, req.IssueNumber), req.IssueNumber),
			componentName:   validationComponentSentinel,
			taskKind:        validationTaskKind,
			deadline:        validationDeadlineSeconds,
			milestoneNumber: req.MilestoneNumber,
			milestoneTitle:  req.MilestoneTitle,
		}, nil
	}
	if req.MilestoneNumber <= 0 {
		return dispatchShape{}, fmt.Errorf("milestone dispatch: cycle kind %q requires a milestone reference", req.Kind)
	}
	return dispatchShape{
		prompt:          buildPrompt(req.MilestoneNumber, req.MilestoneTitle),
		componentName:   milestoneComponentSentinel,
		deadline:        codingDeadlineSeconds,
		milestoneNumber: req.MilestoneNumber,
		milestoneTitle:  req.MilestoneTitle,
	}, nil
}

// issueURL derives an issue's browser URL from the repository's clone URL. The
// validation prompt names the issue by URL, and the run path has no issue row to
// read one off — the funnel path gets it from the Task facts.
func issueURL(repoURL string, number int) string {
	return fmt.Sprintf("%s/issues/%d", strings.TrimSuffix(strings.TrimSuffix(repoURL, "/"), ".git"), number)
}
