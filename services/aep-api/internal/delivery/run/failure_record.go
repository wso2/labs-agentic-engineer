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
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The planning phase's failure record.
//
// Before this, a run that failed while provisioning its gates or planning its
// milestone left two words on its row (`failed`, `plan-failed`) and everything
// else — which dependency, the provisioner's own sentence, whether repeating
// could help, how many attempts it took — in one aep-api log line. The console
// showed the slug. These helpers turn the activity's error into a
// delivery.RunFailure and write it on the run row on EVERY attempt, so a run
// that is still `planning` reads "retrying, attempt 2 of 3" and a run that has
// failed reads the fault it failed on.
//
// The classification is not made here. Permanent is whatever the producer
// said — delivery.ErrProvisionPermanent, sourcecontrol.IsPermanent — the same
// answer provisionErr and planErr turn into a non-retryable error, so the
// record and the retry policy cannot disagree.

// activityAttempt is the attempt number Temporal is running this activity
// under, 1 outside an activity (tests).
func activityAttempt(ctx context.Context) int {
	if !activity.IsActivity(ctx) {
		return 1
	}
	return int(activity.GetInfo(ctx).Attempt)
}

// provisionFailure builds the record for a ProvisionGates error, nil for no
// error. A typed ProvisionFailedError names the subject — the first fault's
// component and dependency; a version rarely fails more than one at once, and
// the detail carries them all. Any other error is recorded without a subject.
func provisionFailure(err error, attempt int) *delivery.RunFailure {
	if err == nil {
		return nil
	}
	now := time.Now().UTC()
	f := &delivery.RunFailure{
		Code:        delivery.RunFailureCodeDependencyProvisionFailed,
		Phase:       delivery.RunPhasePlanning,
		Permanent:   errors.Is(err, delivery.ErrProvisionPermanent),
		Attempts:    attempt,
		MaxAttempts: gateActivityAttempts,
		FirstAt:     now,
		LastAt:      now,
		Detail:      delivery.ScrubFailureDetail(err.Error()),
	}
	var pf *delivery.ProvisionFailedError
	if errors.As(err, &pf) && len(pf.Faults) > 0 {
		f.Component = pf.Faults[0].Component
		f.Dependency = pf.Faults[0].Dependency
		// The provisioner's own words per dependency, without the sentinel
		// wraps ("platform provisioner failed permanently: …") the error chain
		// carries for errors.Is — those are classification, not information,
		// and the record already says Permanent.
		f.Detail = delivery.ScrubFailureDetail(faultDetail(pf.Faults))
	}
	if f.Permanent {
		f.Code = delivery.RunFailureCodeDependencyUnprovisionable
	}
	return f
}

// faultDetail joins each fault's reason as `dependency: reason`, stripping the
// sentinel prefixes the dependencies domain wraps around a provisioner error.
func faultDetail(faults []delivery.ProvisionFault) string {
	parts := make([]string, 0, len(faults))
	for _, f := range faults {
		reason := f.Reason
		for changed := true; changed; {
			changed = false
			for _, p := range sentinelPrefixes {
				if strings.HasPrefix(reason, p) {
					reason = strings.TrimPrefix(reason, p)
					changed = true
				}
			}
		}
		parts = append(parts, f.Dependency+": "+reason)
	}
	return strings.Join(parts, "; ")
}

// sentinelPrefixes are the dependencies domain's error sentinels as they read
// at the head of a wrapped message (see dependencies/errors.go).
var sentinelPrefixes = []string{
	"platform provisioner failed permanently: ",
	"platform provisioner failed: ",
}

// planFailure builds the record for a PlanMilestone error, nil for no error.
// The planning turn retries unbounded on a blip (MaxAttempts 0) and fails on
// the first permanent source-control answer.
func planFailure(err error, attempt int) *delivery.RunFailure {
	if err == nil {
		return nil
	}
	now := time.Now().UTC()
	f := &delivery.RunFailure{
		Code:      delivery.RunFailureCodePlanTurnFailed,
		Phase:     delivery.RunPhasePlanning,
		Attempts:  attempt,
		FirstAt:   now,
		LastAt:    now,
		Detail:    delivery.ScrubFailureDetail(err.Error()),
		Permanent: sourcecontrol.IsPermanent(err),
	}
	if f.Permanent {
		f.Code = delivery.RunFailureCodeRepositoryUnavailable
	}
	return f
}

// recordPlanningFault writes the record (or clears a stale one) on the run
// row. Best-effort: the fault the activity is about to return is the fact that
// matters, and a bookkeeping write must not mask or replace it — the same
// stance the activity feed takes.
//
// A nil failure on the first attempt is the ordinary success and writes
// nothing; a nil failure on a LATER attempt means an earlier one recorded a
// fault that has now healed, so the record is cleared. `attempt` is passed in
// rather than read here so the clear rule is testable outside Temporal.
func (a *Activities) recordPlanningFault(ctx context.Context, runID string, f *delivery.RunFailure, attempt int) {
	if a.runs == nil || runID == "" {
		return
	}
	if f == nil {
		if attempt > 1 {
			if err := a.runs.ClearFailure(ctx, runID); err != nil {
				slog.WarnContext(ctx, "run: clear failure record failed", "run", runID, "error", err)
			}
		}
		return
	}
	// FirstAt is the first attempt's stamp: the record on the row, when there
	// is one for the same fault, carries it. Reading it back would cost a round
	// trip the activity is about to lose anyway; the repository preserves it
	// on overwrite instead (RecordFailure).
	if err := a.runs.RecordFailure(ctx, runID, *f); err != nil {
		slog.WarnContext(ctx, "run: record failure failed", "run", runID, "code", f.Code, "error", err)
	}
}
