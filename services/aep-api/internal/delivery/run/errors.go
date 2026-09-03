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

// Which activity failures are worth repeating.
//
// The supervisor's activities run under Temporal's DEFAULT retry policy —
// unbounded, with backoff — and that is deliberate (see activityCtx): a
// supervisor that cannot reach GitHub should stall visibly rather than settle a
// run on a network blip. Unbounded is the right answer for a blip.
//
// It is the wrong answer for an ANSWER. When a project is deleted its repo row
// goes with it, and every boundary poll thereafter fails identically; the
// activity was observed live at attempt 325, a day after the project was gone,
// with nothing but log volume to show for it. Retrying a fact does not change
// it — it only hides the one failure that mattered behind a thousand copies.
//
// So the two are told apart wherever the supervisor touches source control, and
// only the permanent half is marked non-retryable. Which half that is belongs to
// sourcecontrol.IsPermanent, not here: this package knows Temporal, that one
// knows the host.
//
// sourceControlErr is applied per call site rather than at the port boundary,
// which is the one weakness worth naming: an activity added later that returns a
// source-control error raw is silently back on unbounded retry. The activities
// that return one all wrap it today; CloseMilestone is the deliberate exception,
// because it swallows its error by contract and so never retries at all.

import (
	"errors"

	"go.temporal.io/sdk/temporal"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// errTypePermanentSourceControl is the ApplicationError type a permanent source
// control failure carries. It is what a reader of a failed workflow sees first,
// so it names the CLASS ("this run's repository, issue or credential is gone")
// rather than the call that happened to notice.
const errTypePermanentSourceControl = "PermanentSourceControlFailure"

// sourceControlErr classifies one source-control round trip's error for
// Temporal: permanent failures come back non-retryable, so the activity fails
// on its first attempt and the workflow fails with the cause still legible;
// everything else is returned untouched and keeps the unbounded retry that is
// right for it.
//
// The original error is carried as the ApplicationError's CAUSE, so an
// errors.Is/As on the way out still sees ErrRepoNotFound, an HTTPStatusError or
// a GraphQLError rather than a flattened string.
func sourceControlErr(err error) error {
	if !sourcecontrol.IsPermanent(err) {
		return err
	}
	return temporal.NewNonRetryableApplicationError(err.Error(), errTypePermanentSourceControl, err)
}

// errTypePermanentPlan is the ApplicationError type a permanent planning failure
// carries.
const errTypePermanentPlan = "PermanentPlanFailure"

// planErr classifies a planning round trip, and is the whole point of moving
// planning into the workflow.
//
// The detached goroutine this replaced had no such distinction: a seven-second
// TCP connect timeout to GitHub and "the repository was deleted" both settled
// the version `plan-failed`. Now the first is a blip that Temporal retries under
// the default unbounded policy, and only the second — which repeating cannot
// change — comes back non-retryable and fails the run on its first attempt.
//
// The classification is sourcecontrol's, not this package's: planning is an LLM
// turn wrapped around git and GitHub calls, so the failures worth telling apart
// are exactly the ones sourceControlErr already names.
func planErr(err error) error {
	if err == nil || !sourcecontrol.IsPermanent(err) {
		return err
	}
	return temporal.NewNonRetryableApplicationError(err.Error(), errTypePermanentPlan, err)
}

// errTypePermanentDeploy is the ApplicationError type a permanent deploy failure
// carries, named for the class a reader of a failed workflow needs first: the
// component this run was promoting is gone.
const errTypePermanentDeploy = "PermanentDeployFailure"

// deployErr is sourceControlErr's twin for the deploy stage, and exists for the
// same reason: an answer must not be retried like a blip. WHICH deploy failures
// are permanent is the projects domain's to say (delivery.ErrDeployPermanent) —
// this package only knows how to say it to Temporal.
func deployErr(err error) error {
	if err == nil || !errors.Is(err, delivery.ErrDeployPermanent) {
		return err
	}
	return temporal.NewNonRetryableApplicationError(err.Error(), errTypePermanentDeploy, err)
}

// errTypePermanentProvision is the ApplicationError type a permanent provision
// failure carries: the ClusterResourceType is missing, or the Resource never
// cuts a release.
const errTypePermanentProvision = "PermanentProvisionFailure"

// provisionErr is deployErr's twin for ProvisionGates: an answer must not be
// retried like a blip. WHICH provision failures are permanent is the
// dependencies domain's to say (delivery.ErrProvisionPermanent) — this package
// only knows how to say it to Temporal.
func provisionErr(err error) error {
	if err == nil || !errors.Is(err, delivery.ErrProvisionPermanent) {
		return err
	}
	return temporal.NewNonRetryableApplicationError(err.Error(), errTypePermanentProvision, err)
}
