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
)

// statusFromRunState maps a milestone run's state onto the version-ledger enum.
// A version's story is its run's story: while the run is waiting or running the
// version is in progress, and it completes or fails exactly when the run
// settles. A cancelled or blocked run reads as failed — the version did not
// finish; the run row's terminal reason explains which.
//
// The contract's "started" value never occurs here: it belonged to the retired
// live workflow query, and a ledger read has none.
func statusFromRunState(state string) string {
	switch state {
	case delivery.RunStateSucceeded:
		return statusCompleted
	case delivery.RunStateFailed, delivery.RunStateCancelled, delivery.RunStateBlocked:
		return statusFailed
	default: // waiting | running
		return statusInProgress
	}
}
