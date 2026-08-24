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

package delivery

// MilestoneWork is a milestone reduced to the two numbers the dispatch decision
// is made from: how many gates hold it, and how big the working set is.
//
// It lives at the domain root for the same reason labels.go does. BOTH halves of
// the loop ask the dispatch question — the event plane, deciding whether a
// webhook is worth waking a waiting run for, and the supervisor, at every cycle
// boundary — and neither may import the other. Neither holds its own copy of the
// expression, because a copy of a rule is a rule that will eventually be changed
// in one place only.
//
// The two callers reach it from different shapes (host counts on one side, an
// activity's snapshot on the other), so what they share is the RULE, not the
// carrier. Mapping into this type is two named fields at each call site,
// which is the whole point: a mismatch is visible at the mapping rather than
// buried in a boolean that reads plausibly either way.
type MilestoneWork struct {
	// Gates is the count of open dispatch holds — issues of kind `provision`.
	// They carry no arming label, so they are counted on their own and are never
	// subtracted from the working set: a gate holds the next dispatch, it must
	// never erase the work waiting behind it.
	Gates int
	// Work is the size of the working set: open, armed, and of a kind this loop
	// works (see InDevWorkingSet, and MilestoneIssueCounts.OpenDevWork for the
	// same rule counted host-side).
	Work int
}

// Dispatchable is THE dispatch predicate: no gate is open, and there is
// something to work.
//
// The second clause is the WORKING SET, never "some issue is open". A milestone
// holding only ledger issues has nothing to work, and calling it dispatchable
// would wake a run whose first act is to find an empty working set.
//
// A gate is therefore a brake on the NEXT DISPATCH and only that. It never
// blocks settle, because settle is reached through the empty-working-set branch
// that runs before this predicate: gates hold dispatch, and with nothing to
// dispatch they hold nothing.
func (m MilestoneWork) Dispatchable() bool {
	return m.Gates == 0 && m.Work > 0
}
