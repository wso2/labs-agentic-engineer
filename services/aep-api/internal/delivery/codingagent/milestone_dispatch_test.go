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

import (
	"context"
	"strings"
	"testing"

	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// milestoneExecutor builds an executor with NO dispatch path wired, so
// Dispatch runs its whole shape-and-credentials chain and then fails at the
// launch stage with noDispatchPathErr. That failure is the seam these tests
// read: it proves the dispatch got all the way to the launch, and the shape it
// carried is asserted separately through milestoneDispatchShape.
func milestoneExecutor(execRows *fakeExecRepo) *CodingExecutor {
	return NewCodingExecutor(&ocmocks.ComponentClientMock{},
		fakeRepos{repo: &sourcecontrol.GitRepository{RepoURL: "https://github.com/acme/widgets.git"}},
		fakeIdentities{}, execRows, "http://git", "http://platform", nil, nil, nil, nil)
}

func milestoneDispatch(kind string) delivery.MilestoneDispatch {
	return delivery.MilestoneDispatch{
		OrgID: "acme", ProjectID: "widgets",
		MilestoneNumber: 4, MilestoneTitle: "v3",
		Kind:  kind,
		RunID: "run-1", CycleID: "11111111-1111-1111-1111-111111111111",
	}
}

// TestDispatch_EveryNonValidationKind_CarriesTheMilestoneReference: coding, fix
// and conflict cycles are all the ordinary milestone loop. A recovery cycle is
// deliberately NOT anchored to the issue that caused it — a fix issue is
// ordinary work that joins the working set the runner discovers for itself.
func TestDispatch_EveryNonValidationKind_CarriesTheMilestoneReference(t *testing.T) {
	for _, kind := range []string{delivery.CycleKindCoding, delivery.CycleKindFix, delivery.CycleKindConflict} {
		req := milestoneDispatch(kind)
		req.IssueNumber = 42 // even when one is present, it must not anchor the prompt

		shape, err := milestoneDispatchShape(req, "https://github.com/acme/widgets.git")
		if err != nil {
			t.Fatalf("%s: shape: %v", kind, err)
		}
		if !strings.Contains(shape.prompt, "milestone 4") || !strings.Contains(shape.prompt, `"v3"`) {
			t.Errorf("%s: prompt must name the milestone, got %q", kind, shape.prompt)
		}
		if strings.Contains(shape.prompt, "42") {
			t.Errorf("%s: a non-validation cycle must not anchor to an issue, got %q", kind, shape.prompt)
		}
		if shape.taskKind != "" {
			t.Errorf("%s: taskKind = %q, want the implementation default", kind, shape.taskKind)
		}
		if shape.componentName != milestoneComponentSentinel {
			t.Errorf("%s: componentName = %q, want the milestone sentinel", kind, shape.componentName)
		}
	}
}

// TestDispatch_ValidationCycle_AnchorsToItsIssue: validation is the one anchored
// kind — the aep-validation skill and a prompt pointing at the single issue.
func TestDispatch_ValidationCycle_AnchorsToItsIssue(t *testing.T) {
	req := milestoneDispatch(delivery.CycleKindValidation)
	req.IssueNumber = 9

	shape, err := milestoneDispatchShape(req, "https://github.com/acme/widgets.git")
	if err != nil {
		t.Fatalf("shape: %v", err)
	}
	if !strings.Contains(shape.prompt, "https://github.com/acme/widgets/issues/9") {
		t.Errorf("validation prompt must name the issue URL, got %q", shape.prompt)
	}
	// The non-closing reference: the platform owns this task's close.
	if !strings.Contains(shape.prompt, "Validates #9") {
		t.Errorf("validation prompt must carry its `Validates #N` link contract, got %q", shape.prompt)
	}
	if strings.Contains(shape.prompt, "Closes #9") {
		t.Errorf("validation prompt must not use a closing keyword, got %q", shape.prompt)
	}
	if strings.Contains(shape.prompt, "milestone 4") {
		t.Errorf("validation must stay issue-anchored, got %q", shape.prompt)
	}
	if shape.taskKind != "validation" || shape.deadline != validationDeadlineSeconds {
		t.Errorf("validation shape = kind %q deadline %d, want the aep-validation skill + 2h", shape.taskKind, shape.deadline)
	}
}

// TestDispatch_ValidationWithoutAnIssue_Refused: an anchored cycle with nothing
// to anchor to would launch an agent that cannot know what to validate.
func TestDispatch_ValidationWithoutAnIssue_Refused(t *testing.T) {
	req := milestoneDispatch(delivery.CycleKindValidation)
	if _, err := milestoneDispatchShape(req, "https://github.com/acme/widgets.git"); err == nil {
		t.Fatal("a validation cycle with no issue must be refused")
	}
}

// TestDispatch_WritesNoExecutionRow is the load-bearing one: the executions
// table dies at the flip, and a supervisor that minted rows into it would keep
// it alive. The launch is allowed to fail (no dispatch path is wired) — what
// matters is that nothing was written on the way there.
func TestDispatch_WritesNoExecutionRow(t *testing.T) {
	rows := newFakeExecRepo()
	e := milestoneExecutor(rows)

	_, err := e.Dispatch(context.Background(), milestoneDispatch(delivery.CycleKindCoding))
	if err == nil || !strings.Contains(err.Error(), noDispatchPathErr) {
		t.Fatalf("expected to reach the launch stage (no path configured), got %v", err)
	}
	if got := rows.count(); got != 0 {
		t.Fatalf("a milestone dispatch wrote %d execution row(s); it must write none", got)
	}
}

// TestDispatch_RequiresItsCorrelationKey: without the cycle id the launched Job
// could not be tied back to the cycle that dispatched it, so it is refused
// before any side effect.
func TestDispatch_RequiresItsCorrelationKey(t *testing.T) {
	rows := newFakeExecRepo()
	req := milestoneDispatch(delivery.CycleKindCoding)
	req.CycleID = ""

	if _, err := milestoneExecutor(rows).Dispatch(context.Background(), req); err == nil ||
		!strings.Contains(err.Error(), "CycleID") {
		t.Fatalf("a dispatch with no cycle id must be refused, got %v", err)
	}
	if got := rows.count(); got != 0 {
		t.Fatalf("the refusal must precede any write, got %d row(s)", got)
	}
}

func TestIssueURL_StripsTheCloneSuffix(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/widgets.git": "https://github.com/acme/widgets/issues/9",
		"https://github.com/acme/widgets":     "https://github.com/acme/widgets/issues/9",
		"https://github.com/acme/widgets/":    "https://github.com/acme/widgets/issues/9",
	} {
		if got := issueURL(in, 9); got != want {
			t.Errorf("issueURL(%q) = %q, want %q", in, got, want)
		}
	}
}
