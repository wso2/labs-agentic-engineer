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
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// mergeBuildLimit is how many WorkflowRuns a (component, merge SHA) pair may
// have when the merge fan-out runs: one. A redelivered pull_request.closed
// therefore finds the run already there and triggers nothing — the same
// counting rule that enforces the re-trigger budget also makes the fan-out
// idempotent, so the two can never disagree.
const mergeBuildLimit = 1

// redBuildLimit is the ceiling once a build has gone red: the original attempt
// plus the single automatic re-trigger the model allows per component per SHA.
// Reaching it is what turns a red build from "retry" into "mint a fix issue".
const redBuildLimit = mergeBuildLimit + delivery.RunMaxBuildRetriggersPerComponentSHA

// fanOutBuilds rebuilds every component the merged pull request touched,
// pinned to the merge SHA, in parallel.
//
// It is generic over authorship: what makes a component stale is that a change
// landed under its App Path, not who wrote it. Paths matching no component are
// warned about rather than dropped silently — a file outside every App Path is
// either a repo-root concern or a design that has drifted from the tree, and
// the second one is a component that quietly stops being rebuilt.
//
// Deployment is deliberately NOT done here. Components carry AutoDeploy=false,
// and promoting a release is the run supervisor's deploy stage — a webhook
// handler cannot wait for a build to finish, let alone for a binding to become
// Ready, and the cycle's verdict depends on both.
func (e *Events) fanOutBuilds(ctx context.Context, orgID, projectID string, run *delivery.MilestoneRun,
	prNumber int, mergeSHA string) error {
	if e.p.Builds == nil || e.p.PRs == nil || e.p.Design == nil || mergeSHA == "" {
		return nil
	}
	files, err := e.p.PRs.ListPullRequestFiles(ctx, orgID, projectID, prNumber)
	if err != nil {
		return err
	}
	paths, err := e.p.Design.ComponentPaths(ctx, orgID, projectID)
	if err != nil {
		return err
	}
	diff := delivery.DiffComponents(files, paths)
	if len(diff.Unmatched) > 0 {
		slog.WarnContext(ctx, "eventcore: merged files match no component App Path — nothing rebuilt for them",
			"pr", prNumber, "merge", delivery.ShortSHA(mergeSHA), "files", diff.Unmatched)
	}
	if len(diff.Components) == 0 {
		slog.WarnContext(ctx, "eventcore: merged pull request touched no component",
			"pr", prNumber, "merge", delivery.ShortSHA(mergeSHA), "milestone", run.MilestoneNumber)
		return nil
	}

	// Stage the org's clone credential ONCE, before the fan-out, and hand every
	// component the same reference. The credential is per-org and every component
	// would stage byte-identical content, so staging inside the goroutines below
	// bought nothing and cost correctness: OpenChoreo has no update verb, so a
	// stage is delete-then-create against one shared object, and N of those in
	// parallel let one goroutine delete what another had just created. The loser
	// got an empty reference and built a component that could not clone.
	secretRef, err := e.p.Builds.StageBuildCredential(ctx, orgID, projectID,
		fmt.Sprintf("fanout %s@%s", projectID, delivery.ShortSHA(mergeSHA)))
	if err != nil {
		return fmt.Errorf("stage build credential for fan-out: %w", err)
	}

	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	for _, component := range diff.Components {
		wg.Add(1)
		go func(component string) {
			defer wg.Done()
			// Provision the Component CR first. A component the design gained this
			// cycle has never been built, so its CR does not exist yet and the
			// build would fail "Component not found". This is the last point that
			// knows which component is about to be built — a cycle spans the whole
			// milestone, so the dispatch path cannot do it.
			if cerr := e.ensureComponent(ctx, orgID, projectID, component); cerr != nil {
				mu.Lock()
				errs = append(errs, cerr)
				mu.Unlock()
				return
			}
			// Does what shipped actually consume what the design declares? A
			// component that quietly substituted its own persistence builds fine,
			// so nothing downstream would ever notice. Never blocks the build:
			// the build output is how the agent learns whether its code compiles.
			e.checkWiringConformance(ctx, run, component)

			attempt, berr := e.ensureBuildRun(ctx, orgID, projectID, component, mergeSHA, staged(secretRef), mergeBuildLimit)
			if berr != nil {
				mu.Lock()
				errs = append(errs, berr)
				mu.Unlock()
				return
			}
			if attempt > 0 {
				slog.InfoContext(ctx, "eventcore: build triggered at the merge SHA",
					"component", component, "merge", delivery.ShortSHA(mergeSHA), "attempt", attempt)
			}
		}(component)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// ensureComponent provisions a component's OpenChoreo Component CR before its
// build. Unwired (a boot without the projects component service) is a
// documented no-op rather than a failure: the build then behaves exactly as it
// did before this step existed.
func (e *Events) ensureComponent(ctx context.Context, orgID, projectID, component string) error {
	if e.p.Components == nil {
		return nil
	}
	if err := e.p.Components.EnsureComponent(ctx, orgID, projectID, component); err != nil {
		return fmt.Errorf("ensure component %q before build: %w", component, err)
	}
	return nil
}

// ensureBuildRun triggers the next build attempt for (component, commit)
// unless OpenChoreo already holds `limit` runs for that pair. It returns the
// attempt it triggered, or 0 when the allowance is spent.
//
// This is the single place both the automatic re-trigger budget AND fan-out
// idempotency live, and they are the same rule counted from the same fact: the
// WorkflowRuns themselves. Per-component build state is derived from
// OpenChoreo on read and never stored, so a counter column would be a second
// source of truth that a redelivery or a crashed handler could desynchronise
// from the cluster — whereas a run that exists cannot be un-counted.
//
// The attempt ordinal rides the run NAME, which is what makes the count
// possible: attempts of one (component, commit) share a name prefix and differ
// only in the trailing ordinal.
// stage supplies the clone credential, and is called ONLY once the budget has
// allowed a trigger. Both halves of that matter. Who stages depends on how many
// components the caller is about to build — the fan-out stages once for all of
// them and hands back the same reference, while a single-component caller
// stages on demand — and staging is a destructive delete-then-create against
// one per-org object, so a caller whose budget is already spent must not pay
// for it. Reporting a terminal build is idempotent and repeats every sweep
// pass; staging eagerly there would rewrite the org's live credential every
// pass, for a build that was never going to be triggered again.
func (e *Events) ensureBuildRun(ctx context.Context, orgID, projectID, component, commitSHA string, stage stageFunc, limit int) (int, error) {
	runs, err := e.p.Builds.ListBuildRuns(ctx, orgID, projectID, component)
	if err != nil {
		return 0, err
	}
	existing := attemptsFor(runs, delivery.BuildRunNamePrefix(projectID, component, commitSHA))
	if existing >= limit {
		return 0, nil
	}
	secretRef, err := stage(ctx)
	if err != nil {
		return 0, err
	}
	attempt := existing + 1
	name := delivery.BuildRunName(projectID, component, commitSHA, attempt)
	if err := e.p.Builds.TriggerBuildAtCommit(ctx, orgID, projectID, component, commitSHA, name, secretRef); err != nil {
		return 0, err
	}
	return attempt, nil
}

// stageFunc yields the clone credential a build should carry. An empty
// reference with a nil error means "clone unauthenticated", which is correct
// for a public repo.
type stageFunc func(context.Context) (string, error)

// staged wraps an already-resolved reference — the fan-out's case, where one
// stage serves every component.
func staged(secretRef string) stageFunc {
	return func(context.Context) (string, error) { return secretRef, nil }
}

// OnBuildTerminal is the build half of the routing table (§8 row 4): one
// component's build reached a terminal state at a commit.
//
// Green is reported straight through. Red spends the automatic re-trigger
// first — one per component per SHA, because a build that fails once and
// passes on an identical re-run failed for an infrastructure reason, not a
// code reason — and only a SECOND red is treated as the component's verdict:
// a fix issue joins the milestone and the supervisor is told.
//
// A red build with no live run is not this run's problem, it is main's: the
// deployed version's milestone gets an incident issue instead.
func (e *Events) OnBuildTerminal(ctx context.Context, ev delivery.BuildTerminal) error {
	if ev.Component == "" || ev.CommitSHA == "" || e.p.Runs == nil {
		return nil
	}
	run, err := e.runForCommit(ctx, ev.OrgID, ev.ProjectID, ev.CommitSHA)
	if err != nil {
		return err
	}
	if run == nil {
		if ev.Succeeded {
			return nil
		}
		return e.mintRedMainIssue(ctx, ev)
	}
	if ev.Succeeded {
		e.signal(ctx, run, delivery.SigRunBuildTerminal, delivery.RunSignal{
			Component: ev.Component,
			MergeSHA:  ev.CommitSHA,
			Succeeded: true,
		})
		return nil
	}
	if e.p.Builds != nil {
		// One component, so this stages for itself — there is no sibling to
		// contend with, unlike the merge fan-out which stages once for all. It is
		// deliberately lazy: this path re-runs on every sweep pass for as long as
		// the build stays terminal, and a component whose budget is spent must not
		// rewrite the org's credential each time.
		stage := func(ctx context.Context) (string, error) {
			ref, serr := e.p.Builds.StageBuildCredential(ctx, ev.OrgID, ev.ProjectID,
				fmt.Sprintf("retrigger %s@%s", ev.Component, delivery.ShortSHA(ev.CommitSHA)))
			if serr != nil {
				return "", fmt.Errorf("stage build credential for re-trigger: %w", serr)
			}
			return ref, nil
		}
		attempt, berr := e.ensureBuildRun(ctx, ev.OrgID, ev.ProjectID, ev.Component, ev.CommitSHA, stage, redBuildLimit)
		if berr != nil {
			return berr
		}
		if attempt > 0 {
			// The one automatic re-trigger. Tallied on the run so the terminal
			// reason behind a build-retrigger-budget failure is backed by a count.
			if err := e.p.Runs.BumpBudget(ctx, run.ID, delivery.RunBudgetBuildRetriggers); err != nil {
				slog.WarnContext(ctx, "eventcore: bump build-retrigger tally failed", "run", run.ID, "error", err)
			}
			slog.InfoContext(ctx, "eventcore: red build re-triggered once at the same SHA",
				"component", ev.Component, "commit", delivery.ShortSHA(ev.CommitSHA), "attempt", attempt)
			return nil
		}
	}
	issueNumber, err := e.mintFixIssue(ctx, run, ev)
	if err != nil {
		return err
	}
	e.signal(ctx, run, delivery.SigRunBuildTerminal, delivery.RunSignal{
		Component:   ev.Component,
		MergeSHA:    ev.CommitSHA,
		Succeeded:   false,
		IssueNumber: issueNumber,
		Message:     ev.Reason,
	})
	return nil
}

// runForCommit finds the live run whose current cycle landed this commit — the
// build terminal's inertness gate. A build at a commit no live cycle merged
// belongs to no run (a manual build, a legacy execution, main going red on its
// own).
func (e *Events) runForCommit(ctx context.Context, orgID, projectID, commitSHA string) (*delivery.MilestoneRun, error) {
	if e.p.Cycles == nil {
		return nil, nil
	}
	live, err := e.p.Runs.LiveRunsForProject(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i := range live {
		cycle, cerr := e.p.Cycles.Latest(ctx, live[i].OrgID, live[i].ID)
		if cerr != nil {
			return nil, cerr
		}
		if cycle != nil && cycle.MergeSHA != "" && cycle.MergeSHA == commitSHA {
			return &live[i], nil
		}
	}
	return nil, nil
}
