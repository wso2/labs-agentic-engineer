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
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// RegisterFunc is the webhook-router registration seam (the same closure shape
// app.go passes so this package imports nothing from the webhook receiver).
type RegisterFunc func(event, action string, h func(ctx context.Context, event, action string, payload []byte) error)

// Ports is the event plane's whole outside world, named at the composition
// root. A struct rather than a positional constructor because every field is
// load-bearing and a nine-argument call site says nothing about which
// capability is which.
//
// Signaler and Starter are the two the run supervisor satisfies. They are the
// only optional ones: with either unset the corresponding handler still runs
// its detection and its GitHub writes, and simply has nobody to tell — which
// is exactly the state before a supervisor exists.
type Ports struct {
	Runs   RunStore
	Cycles CycleStore
	Issues IssueClient
	// Writer is the domain's issue-WRITE surface — every fix, deploy-fix,
	// conflict, red-main and wiring-conformance issue this package files. Nil is
	// tolerated (the writer degrades to filing nothing) for the same reason the
	// Issues port is checked before use: a partially wired root must not panic
	// on a webhook.
	Writer *delivery.IssueWriter
	PRs    PRReader
	Merger PRMerger
	Repos  RepoLookup
	Design DesignReader
	Builds BuildTrigger
	// Components provisions a component's OpenChoreo Component CR immediately
	// before its first build. Optional — an unwired ensurer means a first-ever
	// component's build fails "Component not found".
	Components ComponentEnsurer
	// Workloads reads the shipped workload.yaml for the wiring-conformance check
	// on the merged-PR fan-out. Optional — unwired means the check is skipped and
	// a component can again ship without declaring the resources it consumes.
	Workloads WorkloadReader
	// Criteria answers whether a project has an acceptance oracle at all — the
	// last of Revalidate's guards, and REQUIRED by it. Unwired, a revalidation
	// could start against a version with nothing to validate and settle `skipped`,
	// replacing a real verdict; Revalidate therefore refuses as unconfigured
	// rather than proceeding without the check.
	Criteria ValidationOracle
	Signaler RunSignaler
	Starter  RunStarter
	// PlatformSender is the platform's own GitHub login (the App bot,
	// "<slug>[bot]"). Empty disables echo suppression — correct for a dev
	// install with no App, where every write comes from a human PAT.
	PlatformSender string
}

// Events is the webhook half of the event plane.
type Events struct {
	p Ports
}

// New wires the event plane.
func New(p Ports) *Events { return &Events{p: p} }

// SetComponentEnsurer wires the pre-build component provisioning after
// construction. It is a setter rather than a Ports field for one reason: the
// runtime-config emitter it composes with is built AFTER the event plane at the
// composition root, and a half-wired ensurer would silently skip the emit.
func (e *Events) SetComponentEnsurer(c ComponentEnsurer) { e.p.Components = c }

// Compile-time proof the event plane is the build-terminal observer the
// watcher reports to (the root port that keeps them peer sub-packages).
var _ delivery.BuildTerminalObserver = (*Events)(nil)

// RegisterHandlers installs every routing-table row on the webhook router.
//
// pull_request fires the merge policy on open AND on every push to the branch
// (synchronize) — the agent force-pushes a rebase to clear a conflict, and
// that push is the only signal that the conflict is resolved. ready_for_review
// and reopened are the same decision arriving by another route.
//
// issues covers the six actions that can change a milestone's membership or an
// issue's state, which is what the dispatch predicate is computed over.
func (e *Events) RegisterHandlers(register RegisterFunc) {
	register("pull_request", "opened", e.OnPullRequest)
	register("pull_request", "synchronize", e.OnPullRequest)
	register("pull_request", "ready_for_review", e.OnPullRequest)
	register("pull_request", "reopened", e.OnPullRequest)
	register("pull_request", "closed", e.OnPullRequestClosed)
	for _, action := range []string{"closed", "reopened", "milestoned", "demilestoned", "labeled", "unlabeled"} {
		register("issues", action, e.OnIssues)
	}
}

// ---- payloads -------------------------------------------------------------
//
// Only the fields the handlers read are declared. A webhook payload is large
// and mostly irrelevant; naming the read set here is what makes the synthetic
// test payloads honest (they carry exactly this much and nothing more).

type pullRequestPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number int    `json:"number"`
		Merged bool   `json:"merged"`
		Draft  bool   `json:"draft"`
		State  string `json:"state"`
		Body   string `json:"body"`
		// HTMLURL is the pull request's page for a human. It is recorded on the
		// cycle so the console can link a build session's pull request without
		// composing a URL of its own.
		HTMLURL        string `json:"html_url"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		Head           struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// cyclePR is the pull request identity this delivery reports, in the shape a
// cycle record learns it. One projection, so the `opened` write and the `closed`
// backfill can never record the same pull request differently.
func (p pullRequestPayload) cyclePR() delivery.CyclePullRequest {
	return delivery.CyclePullRequest{
		Branch: p.PullRequest.Head.Ref,
		Number: p.PullRequest.Number,
		URL:    p.PullRequest.HTMLURL,
		Draft:  p.PullRequest.Draft,
	}
}

type issuesPayload struct {
	Action string `json:"action"`
	Issue  struct {
		Number    int    `json:"number"`
		State     string `json:"state"`
		Title     string `json:"title"`
		Milestone *struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		} `json:"milestone"`
		// Labels is the issue's WHOLE label set, not just the one that fired the
		// delivery. Adoption routes on the KIND the issue carries, and the label
		// being applied is not it — arming an issue that is already classified
		// sends `aep` in Label while the kind sits here.
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"issue"`
	// Milestone is the TOP-LEVEL milestone object GitHub adds on milestoned and
	// demilestoned. It is the only place a demilestone event names the
	// milestone it left — issue.milestone is already null by then.
	Milestone *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"milestone"`
	Label struct {
		Name string `json:"name"`
	} `json:"label"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

// milestone returns the milestone the event is about: the top-level object
// when GitHub sends one (milestoned / demilestoned), otherwise the issue's
// own. Every issues payload embeds the full issue, so keying an event to a run
// costs no extra read.
// issueLabels flattens the payload's label objects to the names every label
// predicate takes.
func (p issuesPayload) issueLabels() []string {
	out := make([]string, 0, len(p.Issue.Labels))
	for _, l := range p.Issue.Labels {
		out = append(out, l.Name)
	}
	return out
}

func (p issuesPayload) milestone() (MilestoneRef, bool) {
	if p.Milestone != nil && p.Milestone.Number > 0 {
		return MilestoneRef{Number: p.Milestone.Number, Title: p.Milestone.Title}, true
	}
	if p.Issue.Milestone != nil && p.Issue.Milestone.Number > 0 {
		return MilestoneRef{Number: p.Issue.Milestone.Number, Title: p.Issue.Milestone.Title}, true
	}
	return MilestoneRef{}, false
}

func (e *Events) isEcho(sender string) bool {
	return e.p.PlatformSender != "" && strings.EqualFold(sender, e.p.PlatformSender)
}

// ---- pull_request ---------------------------------------------------------

// OnPullRequest runs the auto-merge policy seam on a pull request that is open
// for business (§8 row 1). Drafts are RECORDED but not decided on — a draft is
// the agent saying it is not finished, and the cycle still learns which pull
// request it is waiting behind, because a cycle parked on a draft is otherwise
// indistinguishable from one whose agent never opened a pull request at all.
// `ready_for_review` brings the same pull request back through here.
func (e *Events) OnPullRequest(ctx context.Context, _, _ string, payload []byte) error {
	var p pullRequestPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil // malformed delivery — swallow (matches the router's no-op policy)
	}
	if p.PullRequest.Number == 0 || p.Repository.FullName == "" {
		return nil
	}
	owner, err := e.resolvePRRun(ctx, p.Repository.FullName, p.PullRequest.Head.Ref)
	if err != nil || owner.run == nil {
		return err // no run row → not ours → inert
	}
	// Learn the pull request onto the run's open cycle — but only for the AGENT's
	// pull request. The platform never dictates branch identity (the agent derives
	// it, and reuses one on crash resume) or the host's link, so the webhook is
	// the only way the cycle learns them; a human's pull request landing during
	// the same cycle is not that cycle's work and must not overwrite it.
	if owner.agentBranch {
		e.noteCyclePR(ctx, owner.run, p.cyclePR())
	}
	if p.PullRequest.Draft || e.p.Issues == nil {
		return nil
	}

	// Two parses, kept apart all the way into the policy: closing keywords are
	// what a coding cycle finishes, `Validates #N` is what a validation cycle
	// judges without ending. See decideAutoMerge.
	refs := parseResolvesRefs(p.PullRequest.Body)
	validates := parseValidatesRefs(p.PullRequest.Body)
	work, err := e.p.Issues.ListMilestoneIssues(ctx, owner.orgID, owner.projectID, milestoneOpenIssuesFilter(owner.run.MilestoneNumber))
	if err != nil {
		return err
	}
	decision := decideAutoMerge(refs, validates, work)
	// The verdict is recorded for the AGENT's pull request whichever way it went:
	// a declined merge is the loudest silence this loop has — the cycle sits at
	// its landing deadline with a green agent log and nothing else to say.
	if owner.agentBranch {
		e.noteCycleMergeDecision(ctx, owner.run, decision)
	}
	if !decision.Merge {
		slog.DebugContext(ctx, "eventcore: auto-merge declined", "pr", p.PullRequest.Number,
			"milestone", owner.run.MilestoneNumber, "reason", decision.Reason)
		return nil
	}
	return e.merge(ctx, owner.orgID, owner.projectID, owner.run, p.PullRequest.Number, p.PullRequest.Head.Ref, decision)
}

// OnPullRequestClosed reacts to a merged pull request: the cycle closes and
// every component the diff touched rebuilds at the merge SHA (§8 row 2).
//
// A pull request closed WITHOUT merging is deliberately not acted on here. It
// is a human saying "not this"; the run notices at its next cycle boundary,
// where the decision (re-dispatch or settle) belongs to the supervisor.
func (e *Events) OnPullRequestClosed(ctx context.Context, _, _ string, payload []byte) error {
	var p pullRequestPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	if !p.PullRequest.Merged || p.Repository.FullName == "" {
		return nil
	}
	owner, err := e.resolvePRRun(ctx, p.Repository.FullName, p.PullRequest.Head.Ref)
	if err != nil || owner.run == nil {
		return err
	}
	mergeSHA := p.PullRequest.MergeCommitSHA
	// Only the agent's own pull request closes the cycle. A human's merge moves
	// main (so it still rebuilds), but it is not the cycle's outcome.
	if owner.agentBranch {
		e.closeCycle(ctx, owner.run, p.cyclePR(), mergeSHA)
	}
	e.signal(ctx, owner.run, delivery.SigRunPRMerged, delivery.RunSignal{
		PRNumber: p.PullRequest.Number,
		Branch:   p.PullRequest.Head.Ref,
		MergeSHA: mergeSHA,
	})
	return e.fanOutBuilds(ctx, owner.orgID, owner.projectID, owner.run, p.PullRequest.Number, mergeSHA)
}

// prOwner is a pull request's run, plus whether the pull request is the run's
// own agent work — the distinction that decides which cycle writes are allowed.
type prOwner struct {
	orgID     string
	projectID string
	run       *delivery.MilestoneRun
	// agentBranch is true when the head ref is an `aep/m<milestone#>-…` branch
	// matching a live run: this pull request IS a cycle's work, so the cycle
	// record may learn from it.
	agentBranch bool
}

// resolvePRRun keys a pull request to the run it belongs to, and is the
// inertness gate for both pull_request handlers.
//
// The branch is the first key: the agent works `aep/m<milestone#>-c<k>`, so
// the milestone travels in every payload for free. A branch that names no
// milestone falls back to the project's live DEV run — that is what makes the
// merge path generic over a human's pull request, which lands in the same
// increment even though it followed none of the agent's conventions. The dev run
// is the only honest fallback: it is the one working the version the project is
// currently on, while a task or validation run works some older milestone.
func (e *Events) resolvePRRun(ctx context.Context, repoFullName, headRef string) (prOwner, error) {
	if e.p.Repos == nil || e.p.Runs == nil {
		return prOwner{}, nil
	}
	orgID, projectID, err := e.p.Repos.ByFullName(ctx, repoFullName)
	if err != nil {
		// An unknown repo is not an error worth a 500: the receiver already
		// resolved the delivery to an org, so this is a project the platform does
		// not track.
		slog.DebugContext(ctx, "eventcore: repo not resolvable", "repo", repoFullName, "error", err)
		return prOwner{}, nil
	}
	owner := prOwner{orgID: orgID, projectID: projectID}
	if number, ok := milestoneFromBranch(headRef); ok {
		run, rerr := e.p.Runs.LiveRunForMilestone(ctx, orgID, projectID, number)
		owner.run, owner.agentBranch = run, run != nil
		return owner, rerr
	}
	live, rerr := e.p.Runs.LiveRunsForProject(ctx, orgID, projectID)
	if rerr != nil {
		return owner, rerr
	}
	for i := range live {
		if live[i].Kind == delivery.RunKindDev {
			owner.run = &live[i]
			return owner, nil
		}
	}
	return owner, nil
}

// ---- issues ---------------------------------------------------------------

// OnIssues re-evaluates the dispatch predicate for the run waiting on this
// milestone, and carries the adoption trigger (§8 rows 5 and 6).
//
// Both jobs ride one handler because both start from the same three facts —
// repo, issue, milestone — and re-parsing the payload twice to register two
// handlers on the same key would only make the ordering implicit.
func (e *Events) OnIssues(ctx context.Context, _, action string, payload []byte) error {
	var p issuesPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil
	}
	if p.Repository.FullName == "" || e.isEcho(p.Sender.Login) {
		// Echo suppression, issues.* only: every label / comment / milestone
		// assignment the platform writes fires an issues.* delivery back at it.
		return nil
	}
	if e.p.Repos == nil || e.p.Runs == nil || e.p.Issues == nil {
		return nil
	}
	orgID, projectID, err := e.p.Repos.ByFullName(ctx, p.Repository.FullName)
	if err != nil {
		slog.DebugContext(ctx, "eventcore: repo not resolvable", "repo", p.Repository.FullName, "error", err)
		return nil
	}

	// The ARMING SWITCH is the adoption trigger: a human adding `aep` to an issue
	// hands it to the agent. Platform-written labels never reach here — the echo
	// suppression above drops any delivery this platform's own sender caused —
	// so every arming that arrives is a human's act, which is what makes "who
	// adopted this" answerable from the issue timeline alone.
	//
	// Adoption does NOT short-circuit the predicate below, and that matters: the
	// two jobs answer different states of the same milestone. Adoption starts a
	// run where there is none and is a deliberate no-op where one is already
	// live — but a live run PARKED IN WAITING has no next cycle boundary at
	// which to notice, so the arming that just made its milestone workable is
	// exactly the event that has to wake it. Returning here would leave a run
	// asleep on work a human had just handed it.
	if action == "labeled" && strings.EqualFold(p.Label.Name, delivery.LabelAgentWork) {
		target := AdoptTarget{Number: p.Issue.Number, Labels: p.issueLabels()}
		if ms, ok := p.milestone(); ok {
			target.MilestoneNumber, target.MilestoneTitle = ms.Number, ms.Title
		}
		if aerr := e.AdoptIssue(ctx, orgID, projectID, target); aerr != nil {
			// Adoption problems are the human's to see, and the console dispatch
			// path returns them synchronously. Failing the delivery here would only
			// make GitHub redeliver a label that is already applied.
			slog.WarnContext(ctx, "eventcore: adoption declined", "repo", p.Repository.FullName,
				"issue", p.Issue.Number, "error", aerr)
			return nil
		}
	}

	ms, ok := p.milestone()
	if !ok {
		return nil // an issue outside every milestone belongs to no run
	}
	run, err := e.p.Runs.LiveRunForMilestone(ctx, orgID, projectID, ms.Number)
	if err != nil || run == nil {
		return err
	}
	if run.State != delivery.RunStateWaiting {
		// A running run re-reads the milestone at its own cycle boundary; waking
		// it mid-cycle would only race the agent that is already working. A
		// PLANNING run has no supervisor yet to wake, and the first thing that
		// supervisor does is poll the milestone — so the signal would be lost
		// and is not needed.
		return nil
	}
	counts, err := e.p.Issues.MilestoneIssueCounts(ctx, orgID, projectID, ms.Number)
	if err != nil {
		return err
	}
	if !dispatchable(counts) {
		return nil
	}
	e.signal(ctx, run, delivery.SigRunWorkable, delivery.RunSignal{})
	return nil
}

// ---- signalling -----------------------------------------------------------

// signal tells the supervisor a fact. Best-effort by contract: the supervisor
// re-derives from ground truth at every cycle boundary, so a lost signal costs
// latency, never correctness — and a webhook must not fail because a workflow
// engine is down.
func (e *Events) signal(ctx context.Context, run *delivery.MilestoneRun, name string, payload delivery.RunSignal) {
	if e.p.Signaler == nil || run == nil {
		return
	}
	payload.Signal = name
	payload.MilestoneNumber = run.MilestoneNumber
	if err := e.p.Signaler.SignalRun(ctx, run, name, payload); err != nil {
		slog.WarnContext(ctx, "eventcore: signal run failed", "run", run.ID, "signal", name, "error", err)
	}
}
