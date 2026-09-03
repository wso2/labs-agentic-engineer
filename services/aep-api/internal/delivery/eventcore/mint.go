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
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// Issue minting — the uniform rule: EVERY platform-detected failure becomes an
// issue in a milestone. Recovery work then looks exactly like ordinary work,
// which is why the loop needs no separate recovery machinery: the next cycle
// picks a fix or conflict issue out of the working set like any other.
//
// Bodies are PROSE. Nothing parses an issue body platform-side — not this
// package, not the supervisor, not the reads. The one structured fact any of
// them carries is a conflict issue naming its pull request, and even that is a
// reference for the AGENT (it derives its rebase target from it), not a field
// the platform reads back.
//
// What lives HERE is the detection and the prose. The write itself belongs to
// delivery.IssueWriter, the domain's one issue-write surface, so the label
// vocabulary and the mint logging are decided once for the whole domain rather
// than once per minter. Every mint below passes a DedupeKey from the root's
// key vocabulary, which the issue service resolves against the OPEN issues
// already carrying the derived dedupe label: that is what makes minting safe
// under webhook redelivery, because the second pass finds the first issue and
// files nothing. (The key never reaches GitHub — the service strips it and
// encodes it as a label.)

// mintFixIssue files the bug issue for a component whose build stayed red
// through its automatic re-trigger. It carries the three facts a coding agent
// needs to start: which component, which commit, and what the build said.
//
// It is a `bug` sourced `src/build`, which is what puts it in both working sets:
// a red build is worth fixing whether the version is still being built or is
// already deployed.
func (e *Events) mintFixIssue(ctx context.Context, run *delivery.MilestoneRun, ev delivery.BuildTerminal) (int, error) {
	body := fmt.Sprintf(
		"The build for component **%s** failed at merge commit `%s`, and failed again on an automatic re-trigger at the same commit. It is not a flake — the code needs a fix.\n\n"+
			"Build details:\n\n"+
			"- Component: %s\n"+
			"- Merge commit: %s\n"+
			"- OpenChoreo WorkflowRun: %s\n\n"+
			"Failure output:\n\n```\n%s\n```\n\n"+
			"Fix the component so it builds, then include this issue in your pull request's Resolves list.",
		ev.Component, delivery.ShortSHA(ev.CommitSHA), ev.Component, ev.CommitSHA, orNone(ev.RunName), orNone(ev.Reason))

	number, _, err := e.p.Writer.Mint(ctx, run.OrgID, run.ProjectID, delivery.IssueSpec{
		Title:     fmt.Sprintf("Fix the failing build for %s", ev.Component),
		Body:      body,
		Labels:    []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcBuild},
		Milestone: run.MilestoneNumber,
		DedupeKey: delivery.DedupeKeyFix(ev.Component, delivery.ShortSHA(ev.CommitSHA)),
	})
	return number, err
}

// MintDeployFixIssues files one issue per component whose deployment never came
// up, and returns the issue numbers.
//
// EXPORTED, unlike every other minter here, because its trigger is the only
// platform failure with no webhook behind it. A build going red arrives as an
// event this package observes; a ReleaseBinding that never reaches Ready is a
// level nobody delivers, so the run supervisor is the only thing that ever
// learns it — and it reaches this package through a port rather than minting
// the issue itself, which is what keeps issue writing in one place with one
// dedupe convention.
//
// The dedupe key is (component, commit): a redeploy of the same commit that
// fails the same way finds the open issue and files nothing, while the next
// version's failure is genuinely new work.
//
// The commit rides each FAILURE rather than the call, because one reconcile pass
// promotes each component at its own newest green build (delivery.DeployTarget):
// a single commit for the whole list would key at least one issue against a
// commit that component was never built at.
func (e *Events) MintDeployFixIssues(ctx context.Context, orgID, projectID string, milestoneNumber int,
	failed []delivery.DeployTarget, reasons map[string]string) ([]int, error) {
	filed := make([]int, 0, len(failed))
	for _, target := range failed {
		component, commitSHA := target.Component, target.CommitSHA
		reason := reasons[component]
		body := fmt.Sprintf(
			"Component **%s** built successfully at merge commit `%s`, but its deployment never became ready. "+
				"The image exists — what does not work is running it.\n\n"+
				"Deployment details:\n\n"+
				"- Component: %s\n"+
				"- Merge commit: %s\n"+
				"- Environment: %s\n"+
				"- OpenChoreo reported: %s\n\n"+
				"Look for a runtime problem rather than a compile one: a container that exits at startup, a missing "+
				"or misnamed environment variable, a port that does not match the declared endpoint, a health check "+
				"the app never satisfies, or a declared dependency the workload cannot reach. Fix it, then include "+
				"this issue in your pull request's Resolves list.",
			component, delivery.ShortSHA(commitSHA), component, commitSHA,
			openchoreoDevEnvironment, orNone(reason))

		number, _, err := e.p.Writer.Mint(ctx, orgID, projectID, delivery.IssueSpec{
			Title:     fmt.Sprintf("Fix the failed deployment for %s", component),
			Body:      body,
			Labels:    []string{delivery.LabelAgentWork, delivery.KindBug, delivery.SrcDeploy},
			Milestone: milestoneNumber,
			DedupeKey: delivery.DedupeKeyDeploy(component, delivery.ShortSHA(commitSHA)),
		})
		if err != nil {
			return filed, err
		}
		if number > 0 {
			filed = append(filed, number)
		}
	}
	return filed, nil
}

// openchoreoDevEnvironment is named here rather than imported so this package
// keeps no dependency on the OpenChoreo client for one string in one issue body.
const openchoreoDevEnvironment = "development"

// mintConflictIssue files the conflict issue for a pull request that would not
// merge. It NAMES the pull request — the single structured reference the issue
// model allows — because the agent derives its rebase target from it: it works
// that PR's branch, rebases onto main, resolves semantically, re-runs its build
// gate and force-pushes, rather than opening a rival pull request.
func (e *Events) mintConflictIssue(ctx context.Context, orgID, projectID string, run *delivery.MilestoneRun,
	prNumber int, branch string, mergeErr error) (int, error) {
	body := fmt.Sprintf(
		"Pull request #%d could not be merged into `main`. Rebase it and resolve the conflicts.\n\n"+
			"- Pull request: #%d\n"+
			"- Branch: `%s`\n\n"+
			"Work that pull request's branch: rebase it onto `origin/main`, resolve the conflicts by intent rather than by hunk, re-run the build gate, and force-push. Do not open a second pull request for this work.\n\n"+
			"The host reported:\n\n```\n%s\n```",
		prNumber, prNumber, orNone(branch), orNone(errText(mergeErr)))

	number, _, err := e.p.Writer.Mint(ctx, orgID, projectID, delivery.IssueSpec{
		Title:     fmt.Sprintf("Resolve the merge conflict on pull request #%d", prNumber),
		Body:      body,
		Labels:    []string{delivery.LabelAgentWork, delivery.KindConflict},
		Milestone: run.MilestoneNumber,
		DedupeKey: delivery.DedupeKeyConflict(prNumber),
	})
	return number, err
}

// mintRedMainIssue files the incident issue for a component whose build went
// red on main outside any run — the deployed version regressing.
//
// It is filed into the DEPLOYED version's milestone (that is the version that
// broke) and, alone among platform-minted issues, carries NO ARMING LABEL: a red
// main is a human's call. It is classified — a `bug` sourced `src/incident`, so
// the console and a triaging human see what it is — but classification is not
// permission, and nothing is dispatched for it until somebody arms it with the
// agent-work label.
//
// Resolving the deployed version through run rows is also what keeps this path
// inert until the platform has actually shipped a version.
func (e *Events) mintRedMainIssue(ctx context.Context, ev delivery.BuildTerminal) error {
	if e.p.Runs == nil {
		return nil
	}
	deployed, err := e.p.Runs.DeployedMilestoneRun(ctx, ev.OrgID, ev.ProjectID)
	if err != nil {
		return err
	}
	if deployed == nil {
		slog.DebugContext(ctx, "eventcore: red build outside a run and no deployed version — nothing to attribute it to",
			"component", ev.Component, "commit", delivery.ShortSHA(ev.CommitSHA))
		return nil
	}
	body := fmt.Sprintf(
		"The build of component **%s** on `main` is red, outside any platform run. The deployed version (%s) is affected.\n\n"+
			"- Component: %s\n"+
			"- Commit: %s\n"+
			"- OpenChoreo WorkflowRun: %s\n\n"+
			"Failure output:\n\n```\n%s\n```\n\n"+
			"This issue is a ledger entry: no agent is dispatched for it. Add the `%s` label to hand it to the coding agent.",
		ev.Component, orNone(deployed.SpecTag()), ev.Component, ev.CommitSHA, orNone(ev.RunName),
		orNone(ev.Reason), delivery.LabelAgentWork)

	_, _, err = e.p.Writer.Mint(ctx, deployed.OrgID, deployed.ProjectID, delivery.IssueSpec{
		Title: fmt.Sprintf("Red main: %s", ev.Component),
		Body:  body,
		// Classified but NOT armed, deliberately: never auto-dispatched.
		Labels:    []string{delivery.KindBug, delivery.SrcIncident},
		Milestone: deployed.MilestoneNumber,
		DedupeKey: delivery.DedupeKeyRedMain(ev.Component, delivery.ShortSHA(ev.CommitSHA)),
	})
	return err
}

// orNone keeps a body readable when a fact is missing rather than leaving a
// blank the reader has to interpret.
func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none reported)"
	}
	return s
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
