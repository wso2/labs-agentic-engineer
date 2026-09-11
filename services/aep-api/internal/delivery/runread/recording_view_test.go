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
	return gen.RunCycleViewRecordingNone
}

// TestCycleView_CarriesTheRecordingState pins the one field on this projection
// that is not a column of the row. It answers what the PLATFORM can serve of the
// cycle's feed, which a client has to know before it presents a feed as the
// story of the cycle.
func TestCycleView_CarriesTheRecordingState(t *testing.T) {
	t.Parallel()

	row := &delivery.RunCycle{ID: "c1", Kind: delivery.CycleKindCoding, Attempts: 1}
	for _, state := range []gen.RunCycleViewRecording{
		gen.RunCycleViewRecordingNone,
		gen.RunCycleViewRecordingRecording,
		gen.RunCycleViewRecordingComplete,
		gen.RunCycleViewRecordingGaps,
		gen.RunCycleViewRecordingLost,
	} {
		if got := runread.CycleView(row, state).Recording; got != state {
			t.Errorf("CycleView(_, %q).Recording = %q", state, got)
		}
	}
}

// TestRecordingOf_NilReaderIsNone pins the degraded boot. A platform that
// records nothing must SAY so — `none` is "there is no record of this cycle's
// feed", which is exactly true — rather than leave the field empty and let a
// console guess.
func TestRecordingOf_NilReaderIsNone(t *testing.T) {
	t.Parallel()

	row := &delivery.RunCycle{ID: "c1"}
	if got := runread.RecordingOf(nil, row); got != gen.RunCycleViewRecordingNone {
		t.Errorf("RecordingOf(nil) = %q, want none", got)
	}
	states := fakeRecordings{"c1": gen.RunCycleViewRecordingGaps}
	if got := runread.RecordingOf(states, row); got != gen.RunCycleViewRecordingGaps {
		t.Errorf("RecordingOf = %q, want gaps", got)
	}
	if got := runread.RecordingOf(states, &delivery.RunCycle{ID: "unknown"}); got != gen.RunCycleViewRecordingNone {
		t.Errorf("RecordingOf(unknown cycle) = %q, want none", got)
	}
}
