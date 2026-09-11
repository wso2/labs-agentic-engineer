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

// cycle_log_source.go — the LIVE half of a cycle's log.
//
// While a cycle's Component exists, its pod's stdout is served straight from
// OpenChoreo. Nothing is stored on the way past: this read exists to answer one
// console poll, and the same bytes are re-read on the next one.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

// ErrComponentGone means the cycle's Component (or its release binding) no
// longer exists — deleted by retention or by a cancel. It is a FACT about the
// world, not a failure: the caller falls back to the archive, and failing that
// tells the user the logs are gone.
var ErrComponentGone = errors.New("codingagent: component no longer exists")

// logPageBytes bounds how much live pod stdout is kept AFTER the OpenChoreo
// read returns — the last 64KiB, so old lines scroll off and the console gets
// fresh content on every poll. The OC call itself is unbounded (sinceSeconds=0).
const logPageBytes = 64 * 1024

// OCLogSource reads a cycle pod's log through the OpenChoreo API.
type OCLogSource struct {
	runtime openchoreo.RuntimeClient

	// now is the clock the absolute read window is converted against. A field
	// only so a test can prove the conversion happens LATE — see sinceSecondsAt.
	now func() time.Time
}

// NewOCLogSource wires the live source.
func NewOCLogSource(runtime openchoreo.RuntimeClient) *OCLogSource {
	return &OCLogSource{runtime: runtime, now: time.Now}
}

// Tail reads at most maxBytes of the newest pod output for a cycle Component.
// It asks for the WHOLE log the platform still holds and cuts bytes off the end
// of it, which is a viewer's bargain: fresh content now, and whatever scrolled
// off comes back on the next poll.
func (s *OCLogSource) Tail(ctx context.Context, orgName, projectName, componentName string, maxBytes int) (LiveTail, error) {
	binding, err := s.Binding(ctx, orgName, projectName, componentName)
	if err != nil {
		return LiveTail{}, err
	}
	return s.read(ctx, orgName, binding, time.Time{}, maxBytes)
}

// Binding resolves a cycle Component's release binding in the run environment.
//
// It is a separate call because the answer is FIXED for the attempt, and the
// recorder polls once a second: resolving it on every read spent a whole
// OpenChoreo round trip re-deriving a constant, and — worse — spent it BETWEEN
// the moment the recorder chose its read window and the moment the log API
// applied one, which is how a slow list came to cost a run its events.
func (s *OCLogSource) Binding(ctx context.Context, orgName, projectName, componentName string) (string, error) {
	if s == nil || s.runtime == nil {
		return "", fmt.Errorf("codingagent: live log source not configured")
	}
	binding, err := s.runtime.ReleaseBindingName(ctx, orgName, projectName, componentName, openchoreo.DevEnvironmentName)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrComponentGone, componentName)
		}
		return "", fmt.Errorf("codingagent: resolve release binding for %s: %w", componentName, err)
	}
	return binding, nil
}

// ReadSince is the RECORDER's read: everything the pod logged at or after an
// ABSOLUTE instant (the zero time = the whole log), with NO byte cut.
//
// The cut is what Tail exists for and what a recorder must never do. A viewer
// that loses old bytes re-reads them next poll; a recorder that loses them
// writes the loss into a file nothing can rebuild — which is exactly the "a
// burst larger than 64KiB between two polls" failure the recording closes.
// The time cursor replaces the byte window: the recorder asks for what it has
// not seen, and dedupes what overlaps by seq.
func (s *OCLogSource) ReadSince(ctx context.Context, orgName, releaseBindingName string, since time.Time) (LiveTail, error) {
	return s.read(ctx, orgName, releaseBindingName, since, 0)
}

// read snapshots the pod and renders its log. maxBytes <= 0 keeps the whole
// page (the recorder); a positive value keeps the newest bytes (a viewer's
// page).
func (s *OCLogSource) read(ctx context.Context, orgName, releaseBindingName string, since time.Time, maxBytes int) (LiveTail, error) {
	if s == nil || s.runtime == nil {
		return LiveTail{}, fmt.Errorf("codingagent: live log source not configured")
	}
	pod, err := s.runtime.PodSnapshot(ctx, orgName, releaseBindingName)
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			return LiveTail{}, fmt.Errorf("%w: %s", ErrComponentGone, releaseBindingName)
		}
		return LiveTail{}, fmt.Errorf("codingagent: read resource tree for %s: %w", releaseBindingName, err)
	}
	if !pod.Found {
		// The Job is applied but nothing is scheduled yet. The caller narrates
		// the dark zone from the pod state; there is no text to read.
		return LiveTail{Pod: pod}, nil
	}
	// The coarse window is computed HERE, in the breath before the call that
	// uses it, and nowhere earlier. See sinceSecondsAt.
	lines, err := s.runtime.PodLogs(ctx, orgName, releaseBindingName, pod.Name, sinceSecondsAt(since, s.clock()))
	if err != nil {
		if errors.Is(err, openchoreo.ErrNotFound) {
			// The binding is there but the pod's log is not — a container that
			// has not started, or one already reaped. Not gone; just silent.
			return LiveTail{Pod: pod}, nil
		}
		return LiveTail{}, fmt.Errorf("codingagent: read pod log for %s: %w", pod.Name, err)
	}
	return LiveTail{Text: tailText(lines, maxBytes), Pod: pod}, nil
}

// clock reads the source's clock, tolerating a zero-value OCLogSource.
func (s *OCLogSource) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

// sinceSecondsAt turns an absolute window start into the `sinceSeconds` the
// OpenChoreo log API takes — a count of seconds BACK FROM WHENEVER THE API READS
// IT, which is why this conversion may not be done early.
//
// A measured run lost three bursts of events to exactly that. The recorder
// computed `sinceSeconds` and then made three sequential OpenChoreo calls before
// the log call consumed it; the worst poll spent 6.4 s in front of a 5-second
// window, so the window it asked for began 1.4 s AFTER the last line anybody had
// read, and the two events in between were never asked for again by anything.
// Converting in the breath before the call means in-flight latency can only ever
// make the window WIDER than needed, and a wider window costs a re-read that
// dedupe throws away.
//
// The seconds are rounded UP (the +1) for the same reason: a truncating divide
// would ask for a window that starts a fraction of a second late, and the line
// sitting in that fraction would be gone for good.
func sinceSecondsAt(since, now time.Time) int64 {
	if since.IsZero() {
		return 0 // the whole log
	}
	back := now.Sub(since)
	if back < 0 {
		// A window start in the future: nothing sane asks for this, and asking
		// the API for the smallest window it has is safer than asking for none
		// (0 means "everything", which on a long run is a needless full re-read).
		return 1
	}
	return int64(back/time.Second) + 1
}

// tailText renders log lines in the `timestamps=true` shape the progress parser
// already understands (`RFC3339Nano <line>`), keeping at most maxBytes from the
// END so a long-running agent's newest output is what survives the cap.
// maxBytes <= 0 keeps everything, which is what the recorder asks for.
func tailText(lines []openchoreo.PodLogLine, maxBytes int) string {
	var b strings.Builder
	for i := range lines {
		if !lines[i].Timestamp.IsZero() {
			b.WriteString(lines[i].Timestamp.UTC().Format(time.RFC3339Nano))
			b.WriteByte(' ')
		}
		b.WriteString(lines[i].Log)
		b.WriteByte('\n')
	}
	text := b.String()
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	// Cut on a line boundary so the oldest surviving line is not half a line.
	cut := text[len(text)-maxBytes:]
	if nl := strings.IndexByte(cut, '\n'); nl >= 0 && nl+1 < len(cut) {
		cut = cut[nl+1:]
	}
	return cut
}
