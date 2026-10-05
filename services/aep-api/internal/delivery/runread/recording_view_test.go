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

package runread_test

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// fakeRecordings answers a fixed recording state per cycle id.
type fakeRecordings map[string]gen.RunCycleViewRecording

func (f fakeRecordings) RecordingState(c *delivery.RunCycle) gen.RunCycleViewRecording {
	if st, ok := f[c.ID]; ok {
		return st
	}
	return gen.RunCycleViewRecordingUnavailable
}

// TestCycleView_CarriesTheRecordingState pins the one field on this projection
// that is not a column of the row. It answers what the PLATFORM can serve of the
// cycle's feed, which a client has to know before it presents a feed as the
// story of the cycle.
func TestCycleView_CarriesTheRecordingState(t *testing.T) {
	t.Parallel()

	row := &delivery.RunCycle{ID: "c1", Kind: delivery.CycleKindCoding, Attempts: 1}
	for _, state := range []gen.RunCycleViewRecording{
		gen.RunCycleViewRecordingLive,
		gen.RunCycleViewRecordingKept,
		gen.RunCycleViewRecordingExpired,
		gen.RunCycleViewRecordingUnavailable,
	} {
		if got := runread.CycleView(row, state).Recording; got != state {
			t.Errorf("CycleView(_, %q).Recording = %q", state, got)
		}
	}
}

// TestRecordingOf_NilReaderIsUnavailable pins the degraded boot. A platform
// with no feed must SAY it can serve no cycle's log rather than leave the field
// empty and let a console guess.
func TestRecordingOf_NilReaderIsUnavailable(t *testing.T) {
	t.Parallel()

	row := &delivery.RunCycle{ID: "c1"}
	if got := runread.RecordingOf(nil, row); got != gen.RunCycleViewRecordingUnavailable {
		t.Errorf("RecordingOf(nil) = %q, want unavailable", got)
	}
	states := fakeRecordings{"c1": gen.RunCycleViewRecordingKept}
	if got := runread.RecordingOf(states, row); got != gen.RunCycleViewRecordingKept {
		t.Errorf("RecordingOf = %q, want kept", got)
	}
}
