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

package projects

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// The project badge's build stage, pinned — and pinned to AGREE with the version
// ledger's own mapping (build.statusFromRunState).
//
// The two aggregates are read off the same run row by different surfaces: this
// one drives the toolbar badge, that one the version ledger and the build page
// header. A difference between them is a contradiction a reader sees in one
// glance, and there was one — the header read Cancelled while the toolbar two
// centimetres away read "Build failed".
func TestBuildStageStatus(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  string
		why   string
	}{
		{delivery.RunStateSucceeded, buildSucceeded, ""},
		{delivery.RunStateCancelled, buildCancelled,
			"a person stopped it — not a failure, and not the same word the ledger would use for one"},
		{delivery.RunStateFailed, buildFailed, ""},
		{delivery.RunStateBlocked, buildFailed,
			"nobody chose blocked, which is the whole difference from a cancel"},
		{delivery.RunStateWaiting, buildRunning, "parked between cycles is still being delivered"},
		{delivery.RunStateRunning, buildRunning, ""},
		{delivery.RunStatePlanning, buildRunning, "the platform is filling the milestone"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			if got := buildStageStatus(tc.state); got != tc.want {
				t.Errorf("buildStageStatus(%q) = %q, want %q — %s", tc.state, got, tc.want, tc.why)
			}
		})
	}
}
