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
	"time"

	"go.temporal.io/sdk/activity"
)

// MAKING A CANCEL REACH THE WORK, not just the waiting.
//
// The workflow side of a cancel is cheap: cancel the activity's context and the
// future resolves at once, so the run settles and the console goes quiet within
// the second. What that does NOT do is stop the attempt already running in the
// worker. Temporal delivers an activity cancellation only in the RESPONSE to a
// heartbeat — an activity that never heartbeats cannot be told, so it runs to
// completion, keeps authoring the resources it was half-way through, and only
// discovers the workflow is gone when it tries to report ("workflow execution
// already completed").
//
// For a short round trip that is fine and the reason almost nothing here
// heartbeats. It is not fine for the two activities that WAIT: ProvisionGates
// blocks up to a minute per platform resource for OpenChoreo to cut a release,
// and PlanMilestone holds an agent turn open for minutes. Those are exactly the
// stretches a person is looking at when they press cancel.
const (
	// activityHeartbeatTimeout is how long Temporal waits for a heartbeat before
	// it declares the attempt lost and (retry policy permitting) schedules
	// another. It goes on the ActivityOptions of every activity wrapped below.
	//
	// GENEROUS ON PURPOSE, and the reason is worth stating because the instinct is
	// to make it tight. It does NOT bound how quickly a cancel reaches the work —
	// heartbeatInterval does, because a cancellation rides the response to the next
	// beat. All this bounds is how long a SILENT attempt is tolerated, and the cost
	// of getting that wrong is severe for the activity with the longest body: a
	// starved worker (a CPU-saturated host, a long GC pause) that misses its beats
	// has its attempt declared lost and re-run, which for PlanMilestone means
	// re-running a 30-minute agent turn whose answer was on its way. That is the
	// exact pathology planActivityTimeout's own comment records. Twelve beats per
	// window buys the headroom for nothing that matters.
	activityHeartbeatTimeout = 2 * time.Minute

	// heartbeatInterval is how often heartbeating beats, and therefore the real
	// bound on cancellation latency: a cancel is delivered in the response to the
	// next beat, so this is the longest a cancelled activity keeps working. It MUST
	// stay well under activityHeartbeatTimeout — inverting that ratio turns every
	// long activity into a timeout loop.
	heartbeatInterval = 10 * time.Second
)

// heartbeating runs fn as the body of an activity, beating in the background so
// that a cancellation of this activity reaches fn as an ordinary context
// cancellation.
//
// fn receives the ACTIVITY's context unchanged, which is the point: the SDK
// cancels that context itself when a heartbeat comes back cancelled, so
// everything downstream needs no new plumbing — it only has to respect the
// context it was already given. openchoreo.WaitForReleaseChange, the minute-long
// poll this was written for, already selects on ctx.Done().
//
// Outside an activity it is a plain pass-through. Tests call these activity
// methods directly, and activity.RecordHeartbeat panics on a context that is not
// an activity's — so the guard is what keeps the wrapper invisible to them
// rather than a reason for them to know it exists.
//
// The FIRST beat is synchronous, before fn starts, and that is not just for the
// tests it makes deterministic. HeartbeatTimeout runs from the moment the attempt
// starts, so an activity whose first beat is one interval in spends that interval
// unable to be told anything — the cancellation window opens late for no reason.
// Beating once up front closes that gap and costs one call.
//
// The beat stops when fn returns, on the deferred close: a goroutine still
// heartbeating for a finished activity would keep the attempt looking alive.
func heartbeating(ctx context.Context, fn func(context.Context) error) error {
	if !activity.IsActivity(ctx) {
		return fn(ctx)
	}
	activity.RecordHeartbeat(ctx)
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				activity.RecordHeartbeat(ctx)
			}
		}
	}()
	return fn(ctx)
}
