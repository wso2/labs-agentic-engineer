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
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

func TestOCLogSource_TailReturnsPodTextAndPhase(t *testing.T) {
	rt := &fakeRuntime{
		pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"},
		logs: []openchoreo.PodLogLine{
			{Timestamp: time.Date(2026, 8, 6, 10, 0, 1, 0, time.UTC), Log: "first"},
			{Timestamp: time.Date(2026, 8, 6, 10, 0, 2, 0, time.UTC), Log: "second"},
		},
	}

	got, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", logPageBytes)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got.Pod.Phase != "Running" {
		t.Fatalf("Pod = %+v", got.Pod)
	}
	// The platform's timestamps are re-emitted in the k8s `timestamps=true`
	// shape the progress parser already splits, so one parser serves both eras.
	if !strings.HasPrefix(got.Text, "2026-08-06T10:00:01Z first\n") {
		t.Fatalf("unexpected text %q", got.Text)
	}
	if !strings.Contains(got.Text, "2026-08-06T10:00:02Z second") {
		t.Fatalf("unexpected text %q", got.Text)
	}
}

func TestOCLogSource_TailKeepsTheNewestBytes(t *testing.T) {
	rt := &fakeRuntime{
		pod: openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"},
		logs: []openchoreo.PodLogLine{
			{Timestamp: time.Date(2026, 8, 6, 10, 0, 1, 0, time.UTC), Log: strings.Repeat("a", 500)},
			{Timestamp: time.Date(2026, 8, 6, 10, 0, 2, 0, time.UTC), Log: "newest"},
		},
	}

	got, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", 64)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(got.Text) > 64 {
		t.Fatalf("tail = %d bytes, want <= 64", len(got.Text))
	}
	if !strings.Contains(got.Text, "newest") {
		t.Fatalf("the tail must keep the newest lines, got %q", got.Text)
	}
}

// A deleted Component is a fact the reader turns into an "unavailable" empty
// state, so it has to be distinguishable from a transport error.
func TestOCLogSource_MissingComponentIsComponentGone(t *testing.T) {
	rt := &fakeRuntime{bindingErr: fmt.Errorf("%w: gone", openchoreo.ErrNotFound)}

	_, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", logPageBytes)
	if !errors.Is(err, ErrComponentGone) {
		t.Fatalf("err = %v, want ErrComponentGone", err)
	}
}

func TestOCLogSource_UnscheduledPodIsEmptyNotAnError(t *testing.T) {
	rt := &fakeRuntime{pod: openchoreo.RuntimePod{}}

	got, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", logPageBytes)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if got.Pod.Found || got.Text != "" {
		t.Fatalf("unexpected tail: %+v", got)
	}
}

// TestOCLogSource_TheLogWindowIsConvertedAfterTheCallsInFrontOfIt is the eleven
// lost events, pinned at the line where they were lost.
//
// `sinceSeconds` counts back from whenever the API reads it, so a value computed
// earlier describes a window that has since slid forward. The recorder used to
// compute one and then make its way through a release-binding list and a
// resource tree before the log call consumed it; the worst measured poll spent
// 6.4 s doing that, which moved a 5-second window's START past the last line
// anybody had read. Nothing asked for the two events in between ever again.
//
// The window handed in here is an ABSOLUTE instant, and the conversion happens
// in the breath before the call — so latency in front of the call can only make
// the window wider, and a wider window costs a re-read that dedupe throws away.
func TestOCLogSource_TheLogWindowIsConvertedAfterTheCallsInFrontOfIt(t *testing.T) {
	clock := newFakeClock(time.Date(2026, 9, 8, 9, 29, 41, 800000000, time.UTC))
	rt := &fakeRuntime{
		pod:   openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"},
		logs:  []openchoreo.PodLogLine{{Timestamp: time.Date(2026, 9, 8, 9, 29, 44, 0, time.UTC), Log: "line"}},
		delay: func() { clock.advance(6400 * time.Millisecond) }, // the measured resource-tree read
	}
	src := NewOCLogSource(rt)
	src.now = clock.now

	// The last line the recorder had read, from the run's own log.
	since := time.Date(2026, 9, 8, 9, 29, 34, 294000000, time.UTC)
	if _, err := src.ReadSince(context.Background(), "acme", "rb-dev", since); err != nil {
		t.Fatalf("ReadSince: %v", err)
	}
	// 09:29:34.294 is 13.9 s before the instant the log call was actually made
	// (09:29:48.2), so the honest ask is 14. Converted when the window was
	// CHOSEN it would have been 8 — and the two events at 09:29:41.3 fell in the
	// six seconds of difference.
	if rt.logSince != 14 {
		t.Errorf("sinceSeconds = %d, want 14 — the window must be measured at the call, not before it", rt.logSince)
	}
}

// TestOCLogSource_TailStillAsksForTheWholeLog pins the viewer's semantics, which
// this change deliberately leaves alone: Tail asks for everything the platform
// holds and cuts BYTES off the end of it, because a viewer that loses old bytes
// gets them back on the next poll.
func TestOCLogSource_TailStillAsksForTheWholeLog(t *testing.T) {
	rt := &fakeRuntime{
		pod:  openchoreo.RuntimePod{Found: true, Name: "p1", Phase: "Running"},
		logs: []openchoreo.PodLogLine{{Timestamp: time.Date(2026, 9, 8, 9, 29, 44, 0, time.UTC), Log: "line"}},
	}
	if _, err := NewOCLogSource(rt).Tail(context.Background(), "acme", "shop", "ca-abc", logPageBytes); err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if rt.logSince != 0 {
		t.Errorf("sinceSeconds = %d, want 0 (the whole log)", rt.logSince)
	}
}
