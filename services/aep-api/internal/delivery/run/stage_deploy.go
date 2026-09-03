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

package run

import (
	"errors"
	"fmt"
	"strings"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The DEPLOY STAGE: reconcile the VERSION. Plan what is behind and may be
// promoted, promote it wave by wave, wait for each wave to serve, and converge
// the wiring that only exists once everything is up.

// reconcileVersion moves the version towards what has been built (ADR-0026).
//
// It takes the version's STATE — every component in the design, classified as
// serving, behind, converging, held or unbuilt — and writes only the behind ones
// whose hard providers are met. What it replaced promoted the components the
// cycle's merge happened to touch, which made "what is serving" a function of
// which files a fix edited: a component whose build went green in a cycle a
// sibling failed was promoted by nothing, ever, because its files had stopped
// changing. One such component shipped as a delivered version with no
// ReleaseBinding at all.
//
// It therefore runs on a RED cycle as well as a green one, which is the
// deliberate half of the change. A red build has already minted its fix issue;
// its green siblings are still built, and holding their release hostage to a
// failure in another component is what created the stranding. The promotable
// rule is what makes that safe: the only components written are ones whose
// providers are serving or promoted ahead of them in the same pass.
//
// WAVE BY WAVE, then one CONVERGE — because the wiring between components splits
// into two kinds that want opposite treatment (spec.HardConfigEdges).
//
// A HARD edge is an address the platform must have before the consumer can
// serve a useful first byte: a web app's nginx reverse-proxies `/api` to a
// sibling Service URL injected as pod env. That address exists only once the
// provider has a rendered binding, so a SPA promoted alongside its API answers
// `/api` with 502 until the Service exists. Hard edges therefore ORDER the
// deploy: each wave waits for the last to serve.
//
// A SOFT edge runs the other way — a provider learning about its consumer. An
// OIDC resource wants the SPA's callback URL registered. That is not needed
// before the consumer serves, and requiring it would make the graph circular
// (the SPA needs the API's address, the IdP needs the SPA's). So they are not
// ordered at all: one converge at the end, when every address exists.
//
// The converge passes an EMPTY commit, and that is what makes it a converge and
// not a third promote: nothing is re-cut, so it cannot fail on a release that is
// already there, and no component's live release can move under a pass whose job
// is only to finish the wiring.
//
// Re-running it against a fully serving version writes NOTHING — every component
// is serving, so nothing is behind — which is what lets it run every cycle. That
// idempotence is not new: it is the property ADR-0019 already engineered into the
// verb, where EnsureRelease is idempotent by read-back and the binding write is
// an upsert.
func (l *loop) reconcileVersion(ctx workflow.Context, version delivery.VersionState) (cycleResult, error) {
	if !reconcileWork(version) {
		// Nothing behind and nothing converging: the version already serves what
		// has been built. One read, zero writes, no deadline started — and no
		// deploy gate consulted, because a pass that writes nothing must not park
		// on a credential it will not use.
		return cycleGreen, nil
	}

	plan, err := l.planDeployWaves(ctx, version)
	if err != nil {
		// An unsatisfiable ORDER is a deployment failure like any other, and has to
		// arrive as one. Returned raw it would fail the workflow outright: the
		// boundary returns on a cycle error before it can mint the fix work or
		// settle the row, leaving a non-terminal run that blocks every later build
		// on the project — the same wedge this stage exists to stop producing.
		//
		// Only a PERMANENT failure converts. A plan that could not be read is a
		// blip, and Temporal's retry is the right answer for it.
		if !isPermanentDeploy(err) {
			return cycleNone, err
		}
		// Attributed to what is BEHIND, and to every non-serving component when
		// nothing is behind. The second half is not tidiness: a pass is entered
		// when anything is behind OR converging, so a version whose components are
		// all converging can reach a permanent plan failure with an empty behind
		// set — and a failure attributed to nobody mints no issue, which settles
		// the run on the deploy budget with no named component and nothing to work.
		// That is exactly the wedge the paragraph above describes. An unsatisfiable
		// order is nobody's individual fault either way (see reasonForAll).
		failed := behindNames(version)
		if len(failed) == 0 {
			failed = notServingNames(version)
		}
		l.failDeploy(failed, version, err)
		return cycleDeployFailed, nil
	}
	if len(plan.Held) > 0 {
		// Reported and then left alone. A held component is not a failure and
		// nothing is filed against it — the provider it waits on has its own work
		// in the milestone, and the first pass that finds that provider serving
		// promotes this one.
		workflow.GetLogger(ctx).Info("some components are held behind a provider that is not serving",
			"held", heldNames(plan))
	}
	if !plan.Writes() && len(plan.Waited) == 0 {
		return cycleGreen, nil
	}

	// THE DEPLOY GATE (ADR-0023). Before anything is WRITTEN: every external
	// dependency configured, every platform resource provisioned.
	//
	// It sits after the plan and before the promote — the plan only reads the
	// design — so the run parks on a credential only when it is about to publish
	// something that needs one, and the failure it can produce is attributed to
	// the components this pass would have promoted rather than to the whole
	// design.
	//
	// It sits inside the stage rather than at settle because ADR-0017 put the
	// deploy between builds-green and validation, so validation asserts against a
	// version that is genuinely serving. A gate after validation would be gating
	// the wrong thing.
	if plan.Writes() {
		promoting := delivery.TargetNames(flattenWaves(plan.Waves))
		if res, err := l.awaitDeployable(ctx, promoting, version); err != nil || res != cycleGreen {
			return res, err
		}
	}

	// ONE deadline for the whole stage rather than one per wave. What a version
	// is owed is a time to be serving; a per-wave budget would silently multiply
	// that allowance by however many levels the design happens to have.
	//
	// It starts HERE — after the plan, at the first write — so a pass that only
	// waits on a converge somebody else started is still bounded, and a pass with
	// nothing behind (or with everything held) starts no timer at all.
	deadlineCtx, stopDeadline := workflow.WithCancel(ctx)
	defer stopDeadline()
	deadline := workflow.NewTimer(deadlineCtx, deployReadyTimeout)

	for _, wave := range plan.Waves {
		if err := l.promote(ctx, wave); err != nil {
			return cycleNone, err
		}
		res, err := l.awaitDeployments(ctx, delivery.TargetNames(wave), version, deadline)
		if err != nil || res != cycleGreen {
			return res, err
		}
	}
	// Everything this pass wrote, plus what was already converging: a component
	// promoted by an EARLIER pass may still be rolling out, and the version does
	// not serve until it does.
	if res, err := l.awaitDeployments(ctx, plan.Waited, version, deadline); err != nil || res != cycleGreen {
		return res, err
	}

	if !plan.Writes() {
		// Nothing was promoted, so there is no new address for a soft fact to
		// carry and nothing to converge. It also matters that this returns rather
		// than writing: the deploy gate is only consulted when the pass promotes,
		// and a converge is a write like any other — so a pass that skipped the
		// gate must not go on to write a binding.
		return cycleGreen, nil
	}
	converge := convergeSet(version, plan.Waited)
	if len(converge) == 0 {
		return cycleGreen, nil
	}
	if err := l.promote(ctx, delivery.ConvergeTargets(converge)); err != nil {
		return cycleNone, err
	}
	return l.awaitDeployments(ctx, converge, version, deadline)
}

// behindNames is every component the version state says is behind — the first
// choice of attribution for a failure of the PLAN, which is nobody's individual
// fault (see reasonForAll).
func behindNames(v delivery.VersionState) []string {
	var out []string
	for _, c := range v.Components {
		if c.State == delivery.ComponentStateBehind {
			out = append(out, c.Component)
		}
	}
	return out
}

// notServingNames is every component that is not serving — the attribution of
// last resort, so a stage-wide failure always names somebody and therefore
// always files work. A failure nobody is filed against is a run that settles on
// the deploy budget with an empty milestone behind it.
func notServingNames(v delivery.VersionState) []string {
	off := v.NotServing()
	out := make([]string, 0, len(off))
	for _, c := range off {
		out = append(out, c.Component)
	}
	return out
}

// flattenWaves is every target the plan will promote, in wave order.
func flattenWaves(waves [][]delivery.DeployTarget) []delivery.DeployTarget {
	var out []delivery.DeployTarget
	for _, wave := range waves {
		out = append(out, wave...)
	}
	return out
}

// failDeploy records a deploy failure for the boundary to file, pairing each
// component with the commit its release was being cut from.
//
// The commit is per component because the dedupe key is (component, commit): a
// redeploy of the same commit that fails the same way finds the open issue and
// files nothing, while the next version's failure is genuinely new work. A pass
// can promote two components at two different commits, so one commit for the
// whole list would key at least one of them wrongly.
func (l *loop) failDeploy(components []string, version delivery.VersionState, cause error) {
	l.deployFailed = targetsFor(components, version)
	if cause != nil {
		l.deployFailures = reasonForAll(components, cause)
	}
}

// awaitDeployable holds the deploy stage until the project may deploy, and is
// the only place in the loop where the two blockers behave differently.
//
//	platform resource provisioning ─► POLL   the platform is still working
//	external dependency unset      ─► PARK   a human has not acted yet
//
// Parking on the first would hang the run on something that resolves itself.
// Polling the second would burn the deploy stage's attention on a credential
// that arrives when somebody gets round to it — possibly tomorrow. The park is
// deliberately unbounded and OUTSIDE deployReadyTimeout: that budget bounds how
// long a BINDING may take to serve, and charging a person's credential lookup
// against it would settle `deploy-budget` on a run behaving exactly as designed.
//
// The POLL, on the other hand, is bounded, and by exactly that budget. "The
// platform is still working" is an assumption, not a fact: a binding whose
// ResourceType does not exist, or whose provisioner fails forever, never reaches
// Ready and never will. Polling it without an expiry would leave the run
// `running`/`deploying` for good — no waiting reason, no blocking names, no
// terminal reason — which is the silent hang the park was designed to avoid,
// reintroduced on the other branch. On expiry the cycle ends as a deploy failure
// naming the resources, exactly as awaitDeployments ends a binding that never
// served.
//
// It does NOT wait for the secret operator, and that is deliberate rather than
// an omission — this is the kind of thing a later reader "fixes" by adding a
// wait, so the reasoning lives here. The platform has no read path for it: its
// only read for external resources is control-plane custom resources, the
// data-plane proxy client has apply and delete but no get, and the binding's
// Ready condition says nothing about the operator. Nor is one needed, and that
// is structural, not lucky. An empty secret-store path produces NO Kubernetes
// secret at all, so at deploy the secret either exists holding real values or
// does not exist, and a pod referencing a missing secret retries until
// Kubernetes finds it. The dangerous case — a secret that exists holding STALE
// values, so the pod starts happily and never restarts — cannot arise. The cost
// is a few seconds of pod crash-looping after deploy, which is cosmetic.
//
// Returns cycleGreen when the gate opens, cycleCancelled if a human gave up,
// and cycleDeployFailed when the provisioning budget runs out — in which case
// promoting names the components the failure is filed against, since a resource
// that will not provision is a stage-wide failure of the whole pass rather than
// any one component's fault (see reasonForAll). Components this pass was NOT
// going to write are absent from that list on purpose: a held component gets no
// deploy-fix issue for a credential it was never waiting on.
func (l *loop) awaitDeployable(ctx workflow.Context, promoting []string,
	version delivery.VersionState) (cycleResult, error) {
	parked := false
	// leaveValuesPark undoes setWaitingOnValues, and every exit from the values
	// park goes through it — not only the one that finds the gate open. The park
	// stamps a reason and a list of dependency names on the run row; a pass that
	// moved on to provisioning while leaving them there would have the console
	// ask for a credential the developer has already supplied, naming a
	// dependency that is no longer the blocker.
	//
	// A no-op unless this call actually parked: the stage is already running
	// otherwise, and a redundant write would show the console a state transition
	// that did not happen. SetState(running) clears both columns on the row, and
	// setState mirrors that onto l.st, so restoring the state IS clearing the
	// park.
	leaveValuesPark := func() error {
		if !parked {
			return nil
		}
		if err := l.setState(ctx, delivery.RunStateRunning); err != nil {
			return err
		}
		l.st.Phase = delivery.RunPhaseDeploying
		parked = false
		return nil
	}

	budget := provisioningBudget{ctx: ctx}
	defer budget.drop()

	for {
		cancelled, err := l.cancelledAtGate(ctx, parked)
		if err != nil {
			return cycleNone, err
		}
		if cancelled {
			return cycleCancelled, nil
		}
		var verdict DeployGateVerdict
		if err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).CheckDeployReadiness,
			ProjectRef{OrgID: l.in.OrgID, ProjectID: l.in.ProjectID}).Get(ctx, &verdict); err != nil {
			return cycleNone, err
		}

		switch {
		case len(verdict.Unconfigured) > 0:
			// Whatever the platform had left to do, it is not what the run is
			// waiting for now — and the wait ahead is a person's, which the
			// budget must not measure.
			budget.drop()
			// Re-asserted on every pass, not only on entry: the set of blocking
			// dependencies shrinks as values arrive, and a stale list would name
			// dependencies the developer has already configured.
			if err := l.setWaitingOnValues(ctx, verdict.Unconfigured); err != nil {
				return cycleNone, err
			}
			parked = true
			if cancelled := l.await(ctx); cancelled {
				return cycleCancelled, nil
			}
		case len(verdict.Provisioning) > 0:
			if err := leaveValuesPark(); err != nil {
				return cycleNone, err
			}
			if budget.expired {
				// Out of time. Reported the way a binding that never served is:
				// the components this pass would have promoted could not be
				// delivered, and each fix issue carries the same stage-wide
				// cause, which names the resources rather than the component it
				// is filed against.
				l.failDeploy(promoting, version, errProvisioningTimedOut(verdict.Provisioning))
				return cycleDeployFailed, nil
			}
			workflow.GetLogger(ctx).Info("deploy gate: platform resources still provisioning",
				"resources", verdict.Provisioning)
			cancelled, hitDeadline := l.awaitWake(ctx, deployPollInterval, budget.deadline())
			if cancelled {
				return cycleCancelled, nil
			}
			budget.expired = budget.expired || hitDeadline
		default:
			if err := leaveValuesPark(); err != nil {
				return cycleNone, err
			}
			return cycleGreen, nil
		}
	}
}

// cancelledAtGate asks the cancel question the way the PREVIOUS pass waited.
//
// After a values park it has to be the row-deriving question (see cancelled):
// the cancel surface swallows a failed signal delivery so a dead engine cannot
// wedge the console, and the park is unbounded — so a run whose signal was lost
// would sit at the gate forever on a run the user has already cancelled, which
// is precisely the case that comment describes. After a provisioning poll it
// must NOT be: that wait is bounded by the provisioning budget, so a lost signal
// costs latency rather than a wedge, and a row read every fifteen seconds would
// spend an activity per poll to buy nothing.
func (l *loop) cancelledAtGate(ctx workflow.Context, parked bool) (bool, error) {
	if !parked {
		return l.cancelRequested(), nil
	}
	return l.cancelled(ctx)
}

// provisioningBudget is deployReadyTimeout, spent only while the PLATFORM is
// working.
//
// It is created lazily — on the first pass that finds a resource provisioning —
// and dropped again whenever the gate parks on values, because the two waits are
// charged differently. deployReadyTimeout bounds how long a binding may take to
// become usable; a developer taking a day to find an API key must not spend a
// second of it, or a run behaving exactly as ADR-0023 designed would settle
// `deploy-budget`. A timer started when the stage was entered would do exactly
// that, which is why this one is not.
type provisioningBudget struct {
	ctx     workflow.Context
	timer   workflow.Future
	stop    workflow.CancelFunc
	expired bool
}

// deadline is the budget's timer, started on first use.
func (b *provisioningBudget) deadline() workflow.Future {
	if b.timer == nil {
		timerCtx, stop := workflow.WithCancel(b.ctx)
		b.timer, b.stop = workflow.NewTimer(timerCtx, deployReadyTimeout), stop
	}
	return b.timer
}

// drop abandons an unspent budget so the next provisioning stretch starts a
// fresh one, and releases the timer. Idempotent: it is both the values park's
// reset and the stage's cleanup.
func (b *provisioningBudget) drop() {
	if b.stop != nil {
		b.stop()
	}
	b.timer, b.stop, b.expired = nil, nil, false
}

// errProvisioningTimedOut is the cause a cycle carries when a platform resource
// never became usable. It names the resources — the one fact a reader of the fix
// issue cannot derive from anything else — and the budget it outlived.
func errProvisioningTimedOut(resources []string) error {
	return fmt.Errorf("platform resources were still provisioning after %s: %s",
		deployReadyTimeout, strings.Join(resources, ", "))
}

// setWaitingOnValues parks the row with the reason AND the dependency names.
// `waiting` is unbounded and only cancellation exits it, so a park that does not
// say what it is waiting for is indistinguishable from a hung run — and runs
// pile up behind it while nobody knows there is something to do.
func (l *loop) setWaitingOnValues(ctx workflow.Context, deps []string) error {
	reason := delivery.RunWaitingOnExternalValues
	if err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).SetRunState,
		SetRunStateInput{
			RunID:                l.in.RunID,
			State:                delivery.RunStateWaiting,
			WaitingReason:        reason,
			BlockingDependencies: deps,
		}).Get(ctx, nil); err != nil {
		return err
	}
	l.st.State = delivery.RunStateWaiting
	l.st.Phase = delivery.RunPhaseWaiting
	l.st.WaitingReason = reason
	l.st.BlockingDependencies = deps
	return nil
}

// awaitDeployments waits for the promoted components to reach a verdict.
//
// Unlike awaitBuilds this stage HAS a deadline, and the difference is not
// arbitrary. A WorkflowRun always terminates — the platform gives every build an
// active deadline — so a build poll cannot wait forever by accident. A
// ReleaseBinding is a continuously reconciled level with no terminal state at
// all: an image that will never pull and a rollout that is thirty seconds from
// Ready look identical from here, and the only thing that separates them is how
// long you are prepared to wait.
//
// On expiry the cycle is treated as a deploy failure rather than a hang, which
// puts a fix issue in the milestone and lets the loop's ordinary recovery run.
//
// The deadline is the STAGE's and is passed in, so a run cannot buy itself more
// time by having more waves to wait through.
func (l *loop) awaitDeployments(ctx workflow.Context, components []string,
	version delivery.VersionState, deadline workflow.Future) (cycleResult, error) {
	if len(components) == 0 {
		return cycleGreen, nil
	}
	expired := false

	for {
		state, err := l.pollDeployments(ctx, components)
		if err != nil {
			return cycleNone, err
		}
		if len(state.Failed) > 0 {
			l.failDeploy(state.Failed, version, nil)
			l.deployFailures = state.Reasons
			return cycleDeployFailed, nil
		}
		if state.Green() {
			return cycleGreen, nil
		}
		if expired {
			// Out of time. Reported as a failure of the components that have not
			// come up — and ONLY those: a pass can expire with some components
			// serving and others still rolling out, and filing fix work against
			// one that deployed fine would send an agent after nothing.
			l.failDeploy(state.Pending, version, nil)
			return cycleDeployFailed, nil
		}

		cancelled, hitDeadline := l.awaitWake(ctx, deployPollInterval, deadline)
		if cancelled {
			return cycleCancelled, nil
		}
		expired = expired || hitDeadline
	}
}

// ---- deploy activity calls -------------------------------------------------

// promote writes one wave — or the converge, whose targets carry no commit.
func (l *loop) promote(ctx workflow.Context, targets []delivery.DeployTarget) error {
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PromoteWave, PromoteInput{
		OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, Targets: targets,
	}).Get(ctx, nil)
}

// planDeployWaves asks what this pass should do. The VERSION STATE rides the
// request and no commit does: the order is a property of the design and of what
// is already serving, not of any one release being promoted.
func (l *loop) planDeployWaves(ctx workflow.Context, version delivery.VersionState) (delivery.DeployPlan, error) {
	var plan delivery.DeployPlan
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PlanDeployWaves, PlanDeployInput{
		OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, Version: version,
	}).Get(ctx, &plan)
	return plan, err
}

func (l *loop) pollDeployments(ctx workflow.Context, components []string) (CycleDeployState, error) {
	var state CycleDeployState
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PollDeployments, WaitSetInput{
		OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, Components: components,
	}).Get(ctx, &state)
	return state, err
}

// isPermanentDeploy reports whether a deploy-stage activity failed for a reason
// repeating cannot change. Same mechanism as isAgentQuotaBlocked and for the same
// reason: deployErr stamps the TYPE on the way out, and a sentinel does not
// survive Temporal's error round trip.
func isPermanentDeploy(err error) bool {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Type() == errTypePermanentDeploy
	}
	return false
}

// reasonForAll attributes one stage-wide failure to every component in it.
//
// An unsatisfiable order is nobody's individual fault — the components are stuck
// on each other — so each fix issue carries the same cause, which names the whole
// pass rather than the component it happens to be filed against.
func reasonForAll(components []string, err error) map[string]string {
	out := make(map[string]string, len(components))
	for _, name := range components {
		out[name] = err.Error()
	}
	return out
}
