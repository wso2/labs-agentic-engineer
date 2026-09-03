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

package build

import "github.com/wso2/aep/aep-api/internal/delivery"

// Build status enum values (the contract's BuildSummary.status vocabulary).
const (
	statusInProgress = "in_progress"
	statusCompleted  = "completed"
	statusFailed     = "failed"
	statusCancelled  = "cancelled"
)

// statusFromRunState maps a milestone run's state onto the version-ledger enum.
// A version's story is its run's story: while the run is waiting or running the
// version is in progress, and it completes, fails or is cancelled exactly when
// the run settles.
//
// CANCELLED IS ITS OWN VALUE, and it used to be folded into `failed`. The two are
// different facts and a reader acts on them differently — a failure says the
// platform could not deliver the increment, a cancel says a person decided not
// to — so the fold was a lie told to whoever pressed the button. It was also
// self-contradictory on the page it rendered on: the version header read Failed
// with no reason beside it (a cancel writes none, having no fault to report)
// while the run row two lines below read Cancelled, off the same database row.
//
// It stayed hidden while a cancel was hard to reach. Cancel is now honoured in
// the planning phase too (ADR-0024), which makes a cancelled version the
// ordinary outcome of the button rather than a rarity.
//
// A BLOCKED run still reads as failed, and that is deliberate rather than
// unfinished business. Blocked means the platform stopped needing something a
// human has to supply — a credential, a quota — so "did not finish, here is the
// reason" is exactly what it is, and the terminal reason beside the badge says
// which. Nobody chose it, which is the whole difference from a cancel.
//
// The contract's "started" value never occurs here: it belonged to the retired
// live workflow query, and a ledger read has none.
func statusFromRunState(state string) string {
	switch state {
	case delivery.RunStateSucceeded:
		return statusCompleted
	case delivery.RunStateCancelled:
		return statusCancelled
	case delivery.RunStateFailed, delivery.RunStateBlocked:
		return statusFailed
	default: // waiting | running | planning
		return statusInProgress
	}
}

// waitingReasonFor surfaces a park's reason on the ledger, and only while the
// run is actually parked. The run row is already in hand (Service.List reads
// it for every summary), so this adds no request per row — which is what let
// the version ledger say "waiting on you" without the cost ADR-0021 §6
// forbids.
func waitingReasonFor(state, reason string) string {
	if state != delivery.RunStateWaiting {
		return ""
	}
	return reason
}
