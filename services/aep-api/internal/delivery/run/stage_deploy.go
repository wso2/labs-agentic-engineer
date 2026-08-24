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

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// The DEPLOY STAGE: plan the waves, promote them in order, wait for each to
// serve, and converge the wiring that only exists once everything is up.

// deployCycle promotes the cycle's components and waits for them to serve.
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
func (l *loop) deployCycle(ctx workflow.Context, components []string) (cycleResult, error) {
	if len(components) == 0 {
		// A merge that touched no component has nothing to promote and nothing to
		// wait for.
		return cycleGreen, nil
	}
	waves, err := l.planDeployWaves(ctx, components)
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
		l.deployFailed = components
		l.deployFailures = reasonForAll(components, err)
		return cycleDeployFailed, nil
	}

	// ONE deadline for the whole stage rather than one per wave. What a version
	// is owed is a time to be serving; a per-wave budget would silently multiply
	// that allowance by however many levels the design happens to have.
	deadlineCtx, stopDeadline := workflow.WithCancel(ctx)
	defer stopDeadline()
	deadline := workflow.NewTimer(deadlineCtx, deployReadyTimeout)

	for _, wave := range waves {
		if err := l.deploy(ctx, wave, l.mergeSHA); err != nil {
			return cycleNone, err
		}
		res, err := l.awaitDeployments(ctx, wave, deadline)
		if err != nil || res != cycleGreen {
			return res, err
		}
	}

	if err := l.deploy(ctx, components, convergeNoPromotion); err != nil {
		return cycleNone, err
	}
	return l.awaitDeployments(ctx, components, deadline)
}

// convergeNoPromotion is the commit a converge deploys at: none. The deployer
// reads an empty commit as "re-assert the wiring at whatever release is already
// serving", so this is the difference between finishing a component's wiring and
// promoting it again — named rather than spelled `""` at the call site, because
// which of those two a pass does is the whole point of the pass.
const convergeNoPromotion = ""

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
func (l *loop) awaitDeployments(ctx workflow.Context, components []string, deadline workflow.Future) (cycleResult, error) {
	expired := false

	for {
		state, err := l.pollDeployments(ctx, components)
		if err != nil {
			return cycleNone, err
		}
		if len(state.Failed) > 0 {
			l.deployFailures = state.Reasons
			l.deployFailed = state.Failed
			return cycleDeployFailed, nil
		}
		if state.Green() {
			return cycleGreen, nil
		}
		if expired {
			// Out of time. Reported as a failure of the components that have not
			// come up — and ONLY those: a cycle can expire with some components
			// serving and others still rolling out, and filing fix work against
			// one that deployed fine would send an agent after nothing.
			l.deployFailed = state.Pending
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

// deploy promotes at commitSHA, or converges when it is empty.
func (l *loop) deploy(ctx workflow.Context, components []string, commitSHA string) error {
	return workflow.ExecuteActivity(activityCtx(ctx), (*Activities).DeployCycle, DeployCycleInput{
		OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, Components: components, CommitSHA: commitSHA,
	}).Get(ctx, nil)
}

// planDeployWaves asks for the order. No commit rides the request: the order is
// a property of the DESIGN, not of the release being promoted, and sending one
// would imply the plan changes with the commit.
func (l *loop) planDeployWaves(ctx workflow.Context, components []string) ([][]string, error) {
	var waves [][]string
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PlanDeployWaves, DeployCycleInput{
		OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, Components: components,
	}).Get(ctx, &waves)
	return waves, err
}

func (l *loop) pollDeployments(ctx workflow.Context, components []string) (CycleDeployState, error) {
	var state CycleDeployState
	err := workflow.ExecuteActivity(activityCtx(ctx), (*Activities).PollCycleDeployments, DeployCycleInput{
		OrgID: l.in.OrgID, ProjectID: l.in.ProjectID, Components: components, CommitSHA: l.mergeSHA,
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
// cycle rather than the component it happens to be filed against.
func reasonForAll(components []string, err error) map[string]string {
	out := make(map[string]string, len(components))
	for _, name := range components {
		out[name] = err.Error()
	}
	return out
}
