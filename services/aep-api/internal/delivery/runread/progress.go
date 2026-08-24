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

// progress.go — the run progress SSE endpoint (contract: GET
// /projects/{p}/runs/{runId}/progress). ONE connection carries the whole run:
// a `cycle` frame per cycle record (upserted by id) and a `line` frame per agent
// log entry, every line stamped with the cycle that produced it so the console
// renders one accordion section per cycle, and with an emitter chip saying
// whether the run's main agent or one of its Task subagents produced it.
//
// Frame types ride INSIDE the JSON payload (`type`), never as an SSE `event:`
// name — the console's shared parser keeps only `data:` lines, so a
// self-describing payload is what it can read.
//
// ONLY A TERMINAL RUN SETTLES THE STREAM. That is not just a UX rule: it is what
// makes the endpoint testable, because a terminal run's stream is finite and an
// httptest recorder can capture the whole of it.
//
// This is the read for ONE EXECUTION and stays that. A VERSION's story spans
// several runs, and that read is
// build_progress.go — which shares this file's frame walk (frames.go) and
// settles on a different rule, because a terminal run says nothing about whether
// the version is done.
//
// The one other ending is a run row that STOPS EXISTING — a project delete purges
// its runs — because such a run never goes terminal and the stream would otherwise
// tick forever. It settles with a `done` frame carrying no state: the platform has
// no outcome to report for a row it no longer holds.

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

const (
	// runStreamTick re-derives the run row, its cycles and their new log lines.
	// Everything it touches is DB or OpenChoreo / the observer — never GitHub —
	// so it can be fast without spending rate.
	runStreamTick = 2 * time.Second
	// runStreamKeepAlive paces `: keep-alive` comments so proxies keep an idle
	// stream open and a dead client is noticed. A run parked in `waiting` emits
	// nothing for as long as the gate holds it, which is exactly the case this
	// covers.
	runStreamKeepAlive = 15 * time.Second
)

// Emitter values. The runner stamps `subagent` on lines it forwards from inside
// a Task tool call and stamps nothing otherwise, so an unlabelled line is the
// main agent's.
const (
	emitterMain     = "main"
	emitterSubagent = "subagent"
)

// ProgressService serves the run progress stream. It holds only readers: the
// stream reports, it never advances anything.
type ProgressService struct {
	runs   RunReader
	cycles CycleReader
	logs   CycleLogReader

	// tick / keepAlive are the loop's two cadences, held as fields purely so a
	// same-package test can drive the live loop in milliseconds instead of
	// waiting out the real ones. Production never overrides them.
	tick      time.Duration
	keepAlive time.Duration
}

// NewProgressService wires the stream service. logs may be nil (no progress
// reader wired): the stream then carries cycle frames and settles normally,
// just with no lines.
func NewProgressService(runs RunReader, cycles CycleReader, logs CycleLogReader) *ProgressService {
	return &ProgressService{runs: runs, cycles: cycles, logs: logs, tick: runStreamTick, keepAlive: runStreamKeepAlive}
}

// runFrame is one SSE `data:` payload, discriminated by Type. It is hand-written
// rather than the generated gen.RunProgressEvent because the frame's optional
// members must actually be omitted, and a generated struct-valued field with
// `omitempty` never is.
type runFrame struct {
	Type  string            `json:"type"` // cycle | line | done
	Cycle *gen.RunCycleView `json:"cycle,omitempty"`
	Line  *runLine          `json:"line,omitempty"`
	State string            `json:"state,omitempty"`
}

// runLine is one agent-log line with the attribution the console groups on.
// contracts.ProgressEvent is EMBEDDED, so its fields flatten into the JSON and
// the wire shape matches the contract's RunProgressLine.
type runLine struct {
	contracts.ProgressEvent
	CycleID    string `json:"cycleId"`
	CycleKind  string `json:"cycleKind"`
	CycleIndex int    `json:"cycleIndex"`
	Emitter    string `json:"emitter"`
}

// OpenRunProgressStream runs the PRE-STREAM FENCES and returns the connection
// loop.
//
// The fences run here, before the response object exists and therefore before
// any byte is written, so a bad path answers a normal JSON error envelope rather
// than a broken half-stream. A run belonging to another org — or to another
// project in the same org — resolves to nil and surfaces as ErrRunNotFound, so
// a cross-tenant probe reads as 404 and never as 403.
func (s *ProgressService) OpenRunProgressStream(ctx context.Context, orgID, projectID, runID string) (func(w io.Writer, flush func()), error) {
	if s == nil || s.runs == nil || s.cycles == nil {
		return nil, fmt.Errorf("runread: progress stream not configured")
	}
	row, err := s.runs.GetByIDScoped(ctx, orgID, runID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.ProjectID != projectID {
		return nil, ErrRunNotFound
	}
	return func(w io.Writer, flush func()) {
		s.run(ctx, w, flush, orgID, row)
	}, nil
}

// run is one connection's lifetime: an initial full derive, then a live loop
// that re-derives on a tick and closes with `done` once the run row is terminal.
// All dedup state is per-connection; a reconnect re-derives from scratch and the
// client dedups by cycle id and (cycleId, seq).
func (s *ProgressService) run(ctx context.Context, w io.Writer, flush func(), orgID string, first *delivery.MilestoneRun) {
	out := &frameWriter{w: w, flush: flush}
	writeFrame := func(f *runFrame) bool { return out.write(f) }
	writeDone := func(state string) {
		_ = writeFrame(&runFrame{Type: frameTypeDone, State: state})
		out.sentinel()
	}

	// Per-connection dedup + cursor state.
	lastCycleJSON := map[string]string{} // cycle id → last emitted cycle frame
	cursor := map[string]int64{}         // cycle id → last emitted log ts millis

	// derive re-reads the run row, walks its cycles oldest-first emitting changed
	// `cycle` frames and new `line` frames, and reports the run's state.
	//
	// Three outcomes, and telling them apart is the whole contract:
	//   - alive=false — the client is gone, so stop writing.
	//   - gone=true — the run ROW is gone (a project delete purges its runs).
	//   - otherwise — state is what the row says, and a transient read failure
	//     keeps the stream and retries next tick.
	derive := func(row *delivery.MilestoneRun) (state string, gone, alive bool) {
		if row == nil {
			fresh, err := s.runs.GetByIDScoped(ctx, orgID, first.ID)
			if err != nil {
				// A read that FAILED says nothing about the row. Hold the stream open
				// and ask again next tick.
				return "", false, true
			}
			if fresh == nil {
				return "", true, true
			}
			row = fresh
		}
		cycles, err := s.cycles.ListByRun(ctx, orgID, row.ID)
		if err != nil {
			return row.State, false, true
		}
		alive = s.emitCycles(ctx, cycles, lastCycleJSON, cursor,
			func(v *gen.RunCycleView) bool { return writeFrame(&runFrame{Type: frameTypeCycle, Cycle: v}) },
			func(l *runLine) bool { return writeFrame(&runFrame{Type: frameTypeLine, Line: l}) })
		return row.State, false, alive
	}

	// settled reports whether this pass ends the stream, and closes it when it
	// does. A VANISHED row settles with no state: the platform has no outcome to
	// report for a row it no longer holds, and `state` on the `done` frame is
	// contract-defined as the run's TERMINAL state — so naming the last state seen
	// would render a run that was deleted mid-flight as one that finished in it.
	// Holding the connection open instead is the failure this replaced: a deleted
	// run never goes terminal, so the stream ticked forever and the console spun
	// on a run that was never coming back.
	settled := func(state string, gone bool) bool {
		switch {
		case gone:
			writeDone("")
			return true
		case delivery.IsTerminalRunState(state):
			writeDone(state)
			return true
		}
		return false
	}

	// Initial full derive (reuses the pre-stream row — no double read).
	state, gone, alive := derive(first)
	if !alive {
		return
	}
	if settled(state, gone) {
		return
	}

	tick := time.NewTicker(s.tick)
	defer tick.Stop()
	keepAlive := time.NewTicker(s.keepAlive)
	defer keepAlive.Stop()

	for {
		select {
		case <-ctx.Done():
			return // client disconnected
		case <-keepAlive.C:
			if !out.keepAlive() {
				return
			}
		case <-tick.C:
			state, gone, alive := derive(nil)
			if !alive {
				return
			}
			// A terminal run has just had its final derive, so everything the run
			// ever produced is on the wire before the stream closes.
			if settled(state, gone) {
				return
			}
		}
	}
}

// emitLines pulls one cycle's NEW log lines and writes them, attributed to the
// cycle and to whichever agent produced them. A source hiccup degrades to no new
// lines — it never kills the stream, because a run's cycle timeline is worth
// more to the reader than its pod tail.
func (s *ProgressService) emitLines(ctx context.Context, c *delivery.RunCycle, index int, cursor map[string]int64, emit func(*runLine) bool) bool {
	if s.logs == nil || c.JobRef == "" {
		return true
	}
	resp, err := s.logs.CycleProgress(ctx, c, cursor[c.ID])
	if err != nil || resp == nil {
		return true
	}
	for i := range resp.Lines {
		line := &runLine{
			ProgressEvent: resp.Lines[i],
			CycleID:       c.ID,
			CycleKind:     c.Kind,
			CycleIndex:    index,
			Emitter:       emitterOf(resp.Lines[i].Emitter),
		}
		// The attribution is carried on the wrapper, not twice.
		line.ProgressEvent.Emitter = ""
		if !emit(line) {
			return false
		}
	}
	if resp.CursorMillis > cursor[c.ID] {
		cursor[c.ID] = resp.CursorMillis
	}
	return true
}

// emitterOf normalises the runner's optional attribution. An unstamped line is
// the main agent's: the runner marks only what it forwards from inside a Task
// tool call, so absence is a positive fact and not an unknown.
func emitterOf(raw string) string {
	if raw == emitterSubagent {
		return emitterSubagent
	}
	return emitterMain
}
