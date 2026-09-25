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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wso2/aep/aep-api/internal/delivery"
)

func TestNoWorkSREIssueResolvedWithoutMergeDoesNotRedispatch(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(MilestoneSnapshot{DevWork: 1, TaskWork: 1, Total: 1}, MilestoneSnapshot{})
	h.mergesAt("")
	h.signal("run-no-work", time.Second)
	h.run(delivery.RunKindTask, 0)
	h.assertSettled(t, h.result(t), delivery.RunStateSucceeded, "")
	require.Equal(t, 1, h.dispatchCount())
	require.Empty(t, h.finishes[0].MergeSHA)
}

func TestNoWorkSignalIsIgnoredWhenWorkRemains(t *testing.T) {
	h := newHarness(t)
	h.milestoneIs(
		MilestoneSnapshot{DevWork: 1, TaskWork: 1, Total: 1},
		MilestoneSnapshot{DevWork: 1, TaskWork: 1, Total: 1},
		MilestoneSnapshot{},
	)
	h.factsAre(
		CycleFacts{CycleID: testCycleID},
		CycleFacts{CycleID: testCycleID},
		CycleFacts{CycleID: testCycleID, MergeSHA: testMergeSHA, PRNumber: testPRNumber, Ended: true},
	)
	h.signal(delivery.SigRunNoWork, time.Second)
	h.signal(delivery.SigRunPRMerged, 2*time.Second)

	h.run(delivery.RunKindTask, 0)

	h.assertSettled(t, h.result(t), delivery.RunStateSucceeded, "")
	require.Equal(t, 1, h.dispatchCount())
	require.Equal(t, testMergeSHA, h.finishes[0].MergeSHA)
}
