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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
)

// heartbeating is what makes a cancel reach the WORK rather than only the
// waiting: Temporal delivers an activity cancellation in the response to a
// heartbeat, so an activity that never beats cannot be told it was cancelled and
// runs its wait out unaware.
//
// These pin the two halves of that: it beats when it is inside an activity, and
// it is invisible when it is not.

// It beats. That is the whole point of the wrapper, and an activity that never
// beats cannot be told it was cancelled — it runs its wait out unaware and
// discovers the workflow is gone only when it tries to report.
//
// The beat is observed through a FAILING body, and that is the test environment's
// constraint rather than a choice: it batches heartbeats and calls the listener
// only when one would reach the server — periodically, and at the end only on a
// failure. A short successful activity may legitimately never flush one.
func TestHeartbeating_BeatsFromInsideAnActivity(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()

	var beats atomic.Int32
	env.SetOnActivityHeartbeatListener(func(*activity.Info, converter.EncodedValues) {
		beats.Add(1)
	})

	sentinel := errors.New("openchoreo unreachable")
	act := func(ctx context.Context) error {
		return heartbeating(ctx, func(context.Context) error { return sentinel })
	}
	env.RegisterActivity(act)

	_, err := env.ExecuteActivity(act)
	require.ErrorContains(t, err, sentinel.Error())
	require.GreaterOrEqual(t, beats.Load(), int32(1),
		"the wrapper must heartbeat, or a cancel can never reach the work")
}

// The body receives the ACTIVITY's own context, unchanged. That is the whole
// mechanism: the SDK cancels that context when a heartbeat comes back cancelled,
// so everything downstream needs no new plumbing — only to respect the context it
// already had. Wrapping it in a fresh one would break the chain silently.
func TestHeartbeating_PassesTheActivityContextStraightThrough(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestActivityEnvironment()

	var same bool
	act := func(ctx context.Context) error {
		return heartbeating(ctx, func(inner context.Context) error {
			same = inner == ctx
			return nil
		})
	}
	env.RegisterActivity(act)

	_, err := env.ExecuteActivity(act)
	require.NoError(t, err)
	require.True(t, same, "the body must see the activity's context, or a cancellation cannot reach it")
}

// Outside an activity it is a plain pass-through, and the guard that makes it so
// is what keeps the wrapper invisible to the tests that call these activity
// methods directly: activity.RecordHeartbeat panics on a context that is not an
// activity's.
func TestHeartbeating_OutsideAnActivityIsAPassThrough(t *testing.T) {
	ctx := context.Background()

	var ran bool
	var got context.Context
	err := heartbeating(ctx, func(inner context.Context) error {
		ran, got = true, inner
		return nil
	})

	require.NoError(t, err)
	require.True(t, ran)
	require.Equal(t, ctx, got)
}
