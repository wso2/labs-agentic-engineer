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
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
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
// Every mint passes a DedupeKey, which the issue service resolves against the
// OPEN issues already carrying the derived dedupe label. That is what makes
// minting safe under webhook redelivery: the second pass finds the first
// issue and files nothing. (The key never reaches GitHub — the service strips
// it and encodes it as a label.)

// mintFixIssue files the fix issue for a component whose build stayed red
// through its automatic re-trigger. It carries the three facts a coding agent
// needs to start: which component, which commit, and what the build said.
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

	return e.mint(ctx, run.OrgID, run.ProjectID, sourcecontrol.CreateIssueRequest{
		Title:     fmt.Sprintf("Fix the failing build for %s", ev.Component),
		Body:      body,
		Labels:    []string{delivery.LabelAgentWork},
		Milestone: &run.MilestoneNumber,
		DedupeKey: fmt.Sprintf("aep fix %s %s", ev.Component, delivery.ShortSHA(ev.CommitSHA)),
	})
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
func (e *Events) MintDeployFixIssues(ctx context.Context, orgID, projectID string, milestoneNumber int,
	components []string, reasons map[string]string, commitSHA string) ([]int, error) {
	filed := make([]int, 0, len(components))
	for _, component := range components {
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

		number, err := e.mint(ctx, orgID, projectID, sourcecontrol.CreateIssueRequest{
			Title:     fmt.Sprintf("Fix the failed deployment for %s", component),
			Body:      body,
			Labels:    []string{delivery.LabelAgentWork},
			Milestone: &milestoneNumber,
			DedupeKey: fmt.Sprintf("aep deploy %s %s", component, delivery.ShortSHA(commitSHA)),
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

	return e.mint(ctx, orgID, projectID, sourcecontrol.CreateIssueRequest{
		Title:     fmt.Sprintf("Resolve the merge conflict on pull request #%d", prNumber),
		Body:      body,
		Labels:    []string{delivery.LabelAgentWork},
		Milestone: &run.MilestoneNumber,
		DedupeKey: fmt.Sprintf("aep conflict pr-%d", prNumber),
	})
}

// mintRedMainIssue files the incident issue for a component whose build went
// red on main outside any run — the deployed version regressing.
//
// It is filed into the DEPLOYED version's milestone (that is the version that
// broke) and, alone among platform-minted issues, carries NO agent-work label:
// a red main is a human's call. Nobody is dispatched for it until somebody
// adopts it.
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
		orNone(ev.Reason), delivery.LabelAdopt)

	_, err = e.mint(ctx, deployed.OrgID, deployed.ProjectID, sourcecontrol.CreateIssueRequest{
		Title: fmt.Sprintf("Red main: %s", ev.Component),
		Body:  body,
		// No agent-work label, deliberately: never auto-dispatched.
		Milestone: &deployed.MilestoneNumber,
		DedupeKey: fmt.Sprintf("aep red-main %s %s", ev.Component, delivery.ShortSHA(ev.CommitSHA)),
	})
	return err
}

// mint is the one call into the issue service, so the dedupe contract and the
// logging are written once.
func (e *Events) mint(ctx context.Context, orgID, projectID string, req sourcecontrol.CreateIssueRequest) (int, error) {
	if e.p.Issues == nil {
		return 0, nil
	}
	res, err := e.p.Issues.CreateIssue(ctx, orgID, projectID, req)
	if err != nil {
		return 0, err
	}
	if res == nil {
		return 0, nil
	}
	if res.Deduped {
		slog.DebugContext(ctx, "eventcore: issue already open — not minting a second",
			"issue", res.Number, "title", req.Title)
		return res.Number, nil
	}
	slog.InfoContext(ctx, "eventcore: minted a platform issue",
		"issue", res.Number, "title", req.Title, "milestone", req.Milestone)
	return res.Number, nil
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
