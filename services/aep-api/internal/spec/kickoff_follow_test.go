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

package spec_test

import (
	"context"
	"errors"
	"iter"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// endlessTurn is a pod turn that outlasts any budget: one keep-alive, then
// nothing until the caller's context ends the stream. closed records that the
// stream (the pod's response body, in the real Adapter) was released, and
// why.
type endlessTurn struct {
	closed atomic.Bool
	cause  atomic.Value
}

func (e *endlessTurn) StartTurn(ctx context.Context, _ aestudiotools.RepoRef, _ aestudiotools.TurnRequest) (iter.Seq2[aestudiotools.TurnEvent, error], error) {
	return func(yield func(aestudiotools.TurnEvent, error) bool) {
		defer e.closed.Store(true)
		if !yield(aestudiotools.TurnEvent{Type: aestudiotools.EventKeepAlive}, nil) {
			return
		}
		<-ctx.Done()
		e.cause.Store(ctx.Err())
		yield(aestudiotools.TurnEvent{}, ctx.Err())
	}, nil
}

// The kickoff follows an interview of minutes for its 20 s budget only:
// at the budget it stops reading and releases the stream, and not before.
func TestKickoff_FollowStopsAtTheBudgetAndClosesTheStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pod := &endlessTurn{}
		row := &sourcecontrol.GitRepository{OrgID: kickoffOrg, ProjectID: kickoffProj, RepoURL: "https://github.com/acme/greeter"}
		svc := spec.NewKickoffService(pod, kickoffRepos{row: row}, kickoffLedger{})

		svc.Kickoff(context.Background(), kickoffOrg, kickoffProj)

		time.Sleep(20*time.Second - time.Millisecond)
		synctest.Wait()
		if pod.closed.Load() {
			t.Fatal("the follow stopped before the 20 s budget")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if !pod.closed.Load() {
			t.Fatal("the stream is still open after the 20 s budget")
		}
		if err, _ := pod.cause.Load().(error); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("stream ended by %v, want the budget's deadline", err)
		}
	})
}
