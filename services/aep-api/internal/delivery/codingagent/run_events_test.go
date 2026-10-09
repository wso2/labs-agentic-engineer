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

package codingagent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/gen"
)

// TestBootstrapRunEventNarratesTheDarkZone covers the stretch before the runner
// writes anything. The states are the same ones the v1 render reports — the two
// share bootstrapState — but the SHAPE is deliberately different: a pod that has
// not started is not an agent, so this is a platform notice and never an
// agent_progress phrase put in the mouth of an agent that does not exist yet.
func TestBootstrapRunEventNarratesTheDarkZone(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		podFound      bool
		phase, reason string
		message       string
		wantSeq       int64
		wantCode      gen.RunEventCode
		wantLevel     gen.RunEventLevel
	}{
		{"no pod yet", false, "", "", "", seqBootScheduling, gen.RunEventCodeRunnerScheduling, gen.RunEventLevelInfo},
		{"pulling", true, "Pending", "ContainerCreating", "", seqBootPulling, gen.RunEventCodeRunnerPullingImage, gen.RunEventLevelInfo},
		{"pull backoff", true, "Pending", "ImagePullBackOff", "", seqBootBackoff, gen.RunEventCodeRunnerImagePullBackOff, gen.RunEventLevelWarn},
		{"secrets", true, "Pending", "CreateContainerConfigError", "", seqBootConfig, gen.RunEventCodeRunnerConfigError, gen.RunEventLevelWarn},
		{"no capacity", true, "Pending", "Unschedulable", "0/3 nodes are available: Too many pods.", seqBootUnschedulable, gen.RunEventCodeRunnerUnschedulable, gen.RunEventLevelWarn},
		{"booting", true, "Running", "", "", seqBootStarting, gen.RunEventCodeRunnerStarting, gen.RunEventLevelInfo},
	}
	observedAt := time.Date(2026, 9, 9, 9, 15, 47, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := bootstrapRunEvent(observedAt, tc.podFound, tc.phase, tc.reason, tc.message)
			if ev.Kind != gen.RunEventKindNotice || ev.AgentID != leadAgentID {
				t.Errorf("dark-zone event = %+v, want a notice on the lead", ev)
			}
			if ev.Seq != tc.wantSeq {
				t.Errorf("seq = %d, want the stable %d — the console dedups on it", ev.Seq, tc.wantSeq)
			}
			// The CODE is the whole message. Wording lives in @aep/progress-view,
			// keyed off it; a producer that shipped the sentence would be a second
			// place the copy could change.
			if ev.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", ev.Code, tc.wantCode)
			}
			if ev.Level != tc.wantLevel {
				t.Errorf("level = %q, want %q", ev.Level, tc.wantLevel)
			}
			if !ev.TS.Equal(observedAt) {
				t.Errorf("ts = %s, want the instant the platform read the pod (%s) — see TestPlatformNoticeCarriesARealInstant", ev.TS, observedAt)
			}
		})
	}
}

// TestPlatformNoticeCarriesARealInstant pins the ONE fact every platform-minted
// event on this feed has to get right, and it is pinned on the WIRE FORM because
// that is where it went wrong.
//
// `RunEvent.ts` is required by the contract and generates as a `time.Time`, so a
// notice built without one does not omit the field: it marshals Go's zero value
// into `0001-01-01T00:00:00Z`, a perfectly well-formed date that no consumer can
// tell from a real one. A console did exactly what it should with it and
// subtracted it from the clock, and because these notices belong to the lead,
// the lead's age column read `1065409035m47s` — the 2026 years from Go's zero
// time to the afternoon someone was watching a live run.
//
// So: a real instant, or the field would have to be absent, and the contract
// does not allow absent. The platform is the producer of these events and it
// knows when it derived each one, which makes the real instant the honest answer
// rather than merely the safe one.
func TestPlatformNoticeCarriesARealInstant(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 9, 9, 15, 47, 0, time.UTC)
	notices := map[string]gen.RunEvent{
		// The dark zone, which is at the head of very nearly every feed: it is
		// served before the runner has said anything at all.
		"dark zone":       bootstrapRunEvent(at, true, "Pending", "ContainerCreating", ""),
		"log unavailable": logsUnavailableRunEvent(at, "the log could not be read"),
		"gap in the feed": feedGapNotice(at, 7, 3),
		"bare":            platformNotice(at, 9, gen.RunEventLevelInfo, "something the platform wants to say"),
	}
	for name, ev := range notices {
		t.Run(name, func(t *testing.T) {
			if !ev.TS.Equal(at) {
				t.Errorf("%s notice is stamped %s, want the instant its caller derived it (%s)", name, ev.TS, at)
			}
			// The wire form, because a struct field that reads fine in Go is what
			// marshalled into a date two millennia old.
			b, err := json.Marshal(ev)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(b), "0001-01-01") {
				t.Errorf("%s notice on the wire carries Go's zero time: %s", name, b)
			}
		})
	}
}

// TestBootstrapRunEvent_DetailOnlyWhereTheCodeCannotSpeak pins the two
// exceptions to "no prose": which resource the scheduler ran out of, and a
// waiting reason this build has never seen. Everything else says it with a code.
func TestBootstrapRunEvent_DetailOnlyWhereTheCodeCannotSpeak(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 9, 9, 15, 47, 0, time.UTC)
	if ev := bootstrapRunEvent(at, true, "Running", "", ""); ev.Detail != "" {
		t.Errorf("booting notice carried prose %q — the code says it", ev.Detail)
	}
	full := bootstrapRunEvent(at, true, "Pending", "Unschedulable", "0/3 nodes are available: Too many pods.\nsecond line")
	if !strings.Contains(full.Detail, "Too many pods") || strings.Contains(full.Detail, "second line") {
		t.Errorf("unschedulable detail = %q, want the scheduler's FIRST line", full.Detail)
	}
	odd := bootstrapRunEvent(at, true, "Pending", "SomeBrandNewReason", "")
	if odd.Code != gen.RunEventCodeRunnerPullingImage || odd.Detail != "SomeBrandNewReason" {
		t.Errorf("unknown waiting reason = %+v, want it bucketed under pulling with the reason as detail", odd)
	}
}
