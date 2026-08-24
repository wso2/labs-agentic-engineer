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

package runread

// frames.go — what the two progress streams SHARE: the SSE framing on the wire,
// and the walk that turns one run's cycle rows into frames.
//
// There are two streams because there are two questions. `stream-run-progress`
// reports ONE execution; `stream-build-progress` reports a VERSION, whose story
// is spread across every run its milestone has seen. They differ in what they
// wrap a payload in and in when they end — never in which payloads a cycle owes
// the reader, so that walk lives here once. Forking it is how the two would come
// to describe the same cycle differently, which is the same failure
// CycleView already exists to prevent between the list read and a stream frame.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// Frame discriminators. They ride INSIDE the JSON payload (`type`), never as an
// SSE `event:` name — the console's shared agent-stream parser keeps only `data:`
// lines, so a self-describing payload is what it can read.
const (
	frameTypeCycle = "cycle"
	frameTypeLine  = "line"
	frameTypeDone  = "done"
)

// frameWriter stamps ONE connection's frames: an `id: <seq>` header and one
// `data:` line per frame, flushed as it goes.
//
// The sequence is a connection-local counter and nothing more. The server keeps
// no history and no cursor, so a reconnect starts counting from zero and
// re-derives — which is why the client's dedup key is (cycleId, the RUNNER's
// seq) and never this one.
type frameWriter struct {
	w     io.Writer
	flush func()
	seq   int
}

// write marshals and sends one frame. A frame that will not marshal is SKIPPED
// rather than ending the stream — one bad payload is worth less than the rest of
// the timeline. It reports false only when the CLIENT is gone, which is the one
// condition that must stop the loop.
func (f *frameWriter) write(frame any) bool {
	b, err := json.Marshal(frame)
	if err != nil {
		return true
	}
	if _, err := fmt.Fprintf(f.w, "id: %d\ndata: %s\n\n", f.seq, b); err != nil {
		return false
	}
	f.seq++
	f.flush()
	return true
}

// sentinel writes the `[DONE]` marker the shared console parser stops on. The
// `done` FRAME goes first and is the caller's to compose, because what a stream
// has to say when it ends is exactly what differs between the two: a run reports
// its terminal state, a version reports that no run is live.
func (f *frameWriter) sentinel() {
	_, _ = io.WriteString(f.w, "data: [DONE]\n\n")
	f.flush()
}

// keepAlive writes the comment that holds an idle connection open through a
// proxy and notices a dead client. False = the client is gone.
func (f *frameWriter) keepAlive() bool {
	if _, err := io.WriteString(f.w, ": keep-alive\n\n"); err != nil {
		return false
	}
	f.flush()
	return true
}

// emitCycles walks one run's cycles OLDEST FIRST, sending a `cycle` frame for
// every record whose projection CHANGED since this connection last sent it, then
// every log line the connection has not sent yet. A cycle frame therefore always
// precedes its own lines, so the console can open the section before filling it.
//
// last and cursor are the CONNECTION's dedup state, both keyed by cycle id.
// A cycle id is unique across runs, so the version stream shares one pair of maps
// across every run it stitches rather than keeping a set per run — and a cycle
// re-read on the next tick emits nothing unless a webhook actually changed it.
//
// index is the cycle's 1-based position WITHIN THIS RUN, which is what the
// contract's `cycleIndex` says it is. The version stream deliberately does not
// renumber it: see build_progress.go.
//
// False means the client is gone.
func (s *ProgressService) emitCycles(ctx context.Context, cycles []delivery.RunCycle,
	last map[string]string, cursor map[string]int64,
	emitCycle func(*gen.RunCycleView) bool, emitLine func(*runLine) bool) bool {
	for i := range cycles {
		c := &cycles[i]
		view := CycleView(c)
		b, _ := json.Marshal(view)
		if last[c.ID] != string(b) {
			last[c.ID] = string(b)
			if !emitCycle(&view) {
				return false
			}
		}
		if !s.emitLines(ctx, c, i+1, cursor, emitLine) {
			return false
		}
	}
	return true
}
