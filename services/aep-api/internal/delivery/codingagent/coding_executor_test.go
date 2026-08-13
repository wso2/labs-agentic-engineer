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

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/contracts/taskmeta"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

// stagedRef is the secretRef our fake stager returns (mirrors the real per-org
// build GitSecret name, orgcreds.BuildGitSecretName — a literal here so the
// codingagent package holds no orgcreds import).
const stagedRef = "aep-component-build-git-secret"

// buildTrigger captures the args of the last TriggerBuildAtCommit call.
type buildTrigger struct {
	called    bool
	sha       string
	secretRef string
	component string
	runName   string
}

func ocWithBuildCapture(cap *buildTrigger) *ocmocks.ComponentClientMock {
	return &ocmocks.ComponentClientMock{
		TriggerBuildAtCommitFunc: func(_ context.Context, _, _, componentName, commitSHA, secretRef, runName string) (*gen.WorkflowRun, error) {
			cap.called = true
			cap.sha, cap.secretRef, cap.component, cap.runName = commitSHA, secretRef, componentName, runName
			return &gen.WorkflowRun{Name: runName}, nil
		},
	}
}

func buildRow(id string) *delivery.Execution {
	return &delivery.Execution{
		ID: id, OrgID: "acme", ProjectID: "widgets", Repo: "acme/widgets", IssueNumber: 7,
		Kind: string(taskmeta.KindBuild), Status: string(taskmeta.ExecQueued),
		Component: "order-service", CommitSHA: "deadbeef",
	}
}

func newBuildExecutor(oc openchoreo.ComponentClient, repo *sourcecontrol.GitRepository, execRows *fakeExecRepo) *CodingExecutor {
	return NewCodingExecutor(oc, fakeRepos{repo: repo}, nil, nil, execRows, "http://git", "http://platform", nil, nil, nil, nil)
}

// The build path that survives the flip is the exec watcher's git-clone-auth
// RETRY: the post-merge build itself is triggered by the event plane's fan-out,
// but a build that died at clone-auth is re-minted and re-triggered here. The
// tests below therefore cover the secret-staging ladder through the retry.

func TestRetryAuthFailedBuild_ReMintsAndReTriggersAtCommit(t *testing.T) {
	cap := &buildTrigger{}
	stager := &fakeStager{ref: stagedRef}
	e := newBuildExecutor(ocWithBuildCapture(cap), &sourcecontrol.GitRepository{RepoSlug: "acme-widgets"}, newFakeExecRepo()).
		WithBuildSecrets(stager, 0)

	row := buildRow("e1")
	row.Status = string(taskmeta.ExecRunning)
	newRun, err := e.RetryAuthFailedBuild(context.Background(), row)
	if err != nil {
		t.Fatalf("RetryAuthFailedBuild: %v", err)
	}
	if newRun == "" || newRun != cap.runName {
		t.Errorf("retry must return the fresh run name, got %q (trigger run %q)", newRun, cap.runName)
	}
	if cap.sha != "deadbeef" || cap.component != "order-service" || cap.secretRef != stagedRef {
		t.Errorf("retry re-triggered wrong: sha=%q component=%q ref=%q", cap.sha, cap.component, cap.secretRef)
	}
	if stager.calls() != 1 {
		t.Errorf("retry must re-mint the secret once, got %d", stager.calls())
	}
}

// --- §9 runner contract: the milestone-keyed dispatch prompt ----------------

// TestBuildPrompt_IsAMilestoneReferenceOnly pins the §9 contract: the dispatch
// prompt names the milestone and defers EVERY step to the versioned `aep`
// skill. It must not name an issue, a branch, or a PR-body token — those would
// version with the BFF binary instead of with the skill.
func TestBuildPrompt_IsAMilestoneReferenceOnly(t *testing.T) {
	got := buildPrompt(12, "v3")

	if !strings.Contains(got, "milestone 12") {
		t.Errorf("prompt must name the milestone number, got %q", got)
	}
	if !strings.Contains(got, `"v3"`) {
		t.Errorf("prompt must name the milestone title (quoted, as gh --milestone matches it), got %q", got)
	}
	if !strings.Contains(got, "`aep` skill") {
		t.Errorf("prompt must defer the procedure to the aep skill, got %q", got)
	}
	for _, banned := range []string{"issue:", "issues/", "Closes #", "Resolves #", "git checkout", "gh pr create"} {
		if strings.Contains(got, banned) {
			t.Errorf("prompt must carry no procedure/issue anchor, but contains %q: %q", banned, got)
		}
	}
}

// TestBuildValidationPrompt_StaysIssueAnchored pins the other half of §9: the
// validation cycle stays issue-anchored — one validation issue, one run.
func TestBuildValidationPrompt_StaysIssueAnchored(t *testing.T) {
	got := buildValidationPrompt("https://github.com/acme/widgets/issues/9", 9)

	if !strings.Contains(got, "https://github.com/acme/widgets/issues/9") {
		t.Errorf("validation prompt must name its issue URL, got %q", got)
	}
	if !strings.Contains(got, "Closes #9") {
		t.Errorf("validation prompt must keep its Closes #N link contract, got %q", got)
	}
	if strings.Contains(got, "milestone") {
		t.Errorf("validation dispatch must stay issue-anchored, got %q", got)
	}
}

func TestRetryAuthFailedBuild_MissingFacts_Errors(t *testing.T) {
	e := newBuildExecutor(ocWithBuildCapture(&buildTrigger{}), &sourcecontrol.GitRepository{RepoSlug: "acme-widgets"}, newFakeExecRepo()).
		WithBuildSecrets(&fakeStager{}, 0)

	if _, err := e.RetryAuthFailedBuild(context.Background(), &delivery.Execution{ID: "e1", Component: "x"}); err == nil {
		t.Error("retry without CommitSHA must error")
	}
	if _, err := e.RetryAuthFailedBuild(context.Background(), &delivery.Execution{ID: "e1", CommitSHA: "sha"}); err == nil {
		t.Error("retry without Component must error")
	}
}
