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

// build_progress.go — the VERSION progress SSE endpoint (contract: GET
// /projects/{p}/builds/{tag}/progress). ONE connection carries a version's whole
// narrative: every cycle of every run its milestone has seen, in chronological
// run order, with the run's KIND as the section marker.
//
// It exists because a version's story stopped being one execution. The delivery
// loop is three workflows — a dev run delivers the version, a task run works a
// defect inside it, a validation run re-judges it — so a version now spans
// several run rows, while people still think in VERSIONS. `stream-run-progress`
// stays exactly what it was: the right read for ONE execution, and what
// `get-task` and the per-run views are written against.
//
// The frames are that stream's, plus a `run` object naming the run each cycle
// belongs to. Two consequences worth stating outright:
//
// CYCLEINDEX STAYS RUN-RELATIVE. The contract says `cycleIndex` is the cycle's
// 1-based position IN ITS RUN, and both streams honour that, so the same cycle
// carries the same number whichever endpoint served it. Version-relative
// renumbering would have made the field mean one thing here and another there —
// the same dishonesty as reusing `state` below — and would have re-labelled a
// settled run's sections every time a later run was admitted. The collision the
// numbers do have across runs is resolved by the KEY gaining a component,
// `(run.id, cycleIndex)`, not by renumbering: the console already renders
// exactly that ("Run 2 · Cycle 1"), because two runs each hold a "Cycle 1".
//
// RUNS ARE EMITTED OLDEST FIRST so the narrative reads forwards. The repository
// answers newest-first (it is written for the run LIST, whose head is the only
// run that can be live), so this stream sorts deliberately rather than relying
// on that order: by creation time, tie-broken by id so two runs admitted in the
// same instant cannot swap places between ticks. A run admitted mid-stream
// appends at the end and takes the next index; every earlier run keeps the index
// it already had.

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// buildStreamSettleReason is the `done` frame's only value, and it is
// deliberately NOT a run state.
//
// "The version is finished" is not a fact this stream can state. Across runs it
// is not IsTerminalRunState of anything: the newest run going terminal does not
// mean no further run will be admitted on the milestone, and that is the NORMAL
// case: a dev run settles and a validation run starts later, when a validation
// issue opens — possibly much later, and possibly never.
// So the honest statement is the weaker one this names: no run on the milestone
// is live RIGHT NOW.
//
// `state` on the run stream's `done` frame is contract-defined as the run's
// terminal state, so reusing that field here would have published a run outcome
// as a verdict on the version.
const buildStreamSettleReason = "no_live_run"

// buildFrame is one SSE `data:` payload on the version stream. Hand-written
// rather than the generated gen.BuildProgressEvent for the same reason runFrame
// is: the frame's optional members must actually be omitted, and a generated
// struct-valued field with `omitempty` never is.
type buildFrame struct {
	Type   string            `json:"type"` // cycle | line | done
	Run    *buildFrameRun    `json:"run,omitempty"`
	Cycle  *gen.RunCycleView `json:"cycle,omitempty"`
	Line   *runLine          `json:"line,omitempty"`
	Reason string            `json:"reason,omitempty"`
}

// buildFrameRun is the run attribution the version stream adds. It rides on the
// FRAME rather than inside the line, so RunProgressLine stays one shape on both
// streams and the per-run endpoint gains no field it cannot fill.
type buildFrameRun struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Index int    `json:"index"`
}

// OpenBuildProgressStream runs the PRE-STREAM FENCES and returns the connection
// loop.
//
// Same discipline as OpenRunProgressStream, for the same reason: the fences run
// before the response object exists and therefore before any byte is written, so
// a bad path answers a normal JSON error envelope rather than a broken
// half-stream. The fence IS the tag resolution — MilestoneNumberForTag is
// org-and-project scoped, so a version of another org, or of another project in
// this org, resolves to not-found and surfaces as ErrTagNotFound. A cross-tenant
// probe therefore reads as 404 and never as 403, and cannot learn that the
// version exists.
func (s *ProgressService) OpenBuildProgressStream(ctx context.Context, orgID, projectID, tag string) (func(w io.Writer, flush func()), error) {
	if s == nil || s.runs == nil || s.cycles == nil {
		return nil, fmt.Errorf("runread: build progress stream not configured")
	}
	number, found, err := s.runs.MilestoneNumberForTag(ctx, orgID, projectID, tag)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrTagNotFound
	}
	return func(w io.Writer, flush func()) {
		s.version(ctx, w, flush, orgID, projectID, number)
	}, nil
}

// version is one connection's lifetime: an initial full derive over every run on
// the milestone, then a live loop that re-derives on a tick and closes with
// `done` once no run is live.
//
// All dedup state is per-connection and keyed by CYCLE ID, which is unique across
// runs — so one pair of maps covers every run this connection stitches, and a
// reconnect re-derives from scratch while the client dedups by cycle id and
// (cycleId, seq).
func (s *ProgressService) version(ctx context.Context, w io.Writer, flush func(), orgID, projectID string, milestoneNumber int) {
	out := &frameWriter{w: w, flush: flush}

	lastCycleJSON := map[string]string{} // cycle id → last emitted cycle frame
	cursor := map[string]int64{}         // cycle id → last emitted log ts millis

	// derive re-reads the milestone's runs, walks each one's cycles in order, and
	// reports whether any run is still live.
	//
	// Three outcomes, and telling them apart is the whole contract:
	//   - alive=false — the client is gone, so stop writing.
	//   - live=true — at least one run on the milestone is non-terminal, so more
	//     is coming and the stream holds.
	//   - live=false — nothing is running; the caller settles.
	// A read that FAILED reports live=true: it says nothing about the milestone,
	// and settling on a database blip would end a live version's stream and make
	// the console redraw a moving version as a finished one.
	derive := func() (live, alive bool) {
		rows, err := s.runs.ListByMilestone(ctx, orgID, projectID, milestoneNumber)
		if err != nil {
			return true, true
		}
		chronological(rows)
		live = false
		for i := range rows {
			row := &rows[i]
			if !delivery.IsTerminalRunState(row.State) {
				live = true
			}
			cycles, cerr := s.cycles.ListByRun(ctx, orgID, row.ID)
			if cerr != nil {
				// One run's cycles are missing this pass. Keep the stream and the other
				// runs' frames — the next tick re-reads, and the client upserts.
				continue
			}
			run := &buildFrameRun{ID: row.ID, Kind: row.Kind, Index: i + 1}
			ok := s.emitCycles(ctx, cycles, lastCycleJSON, cursor,
				func(v *gen.RunCycleView) bool {
					return out.write(&buildFrame{Type: frameTypeCycle, Run: run, Cycle: v})
				},
				func(l *runLine) bool {
					return out.write(&buildFrame{Type: frameTypeLine, Run: run, Line: l})
				})
			if !ok {
				return live, false
			}
		}
		return live, true
	}

	// settle ends the stream. It runs only AFTER a full derive, so everything the
	// version has produced is on the wire before the connection closes — and the
	// reader's way back is the run-list poll it already makes every 5s: a new run
	// on the milestone reopens this stream. That is the whole reason the frame
	// says `reason` and not `state`.
	settle := func() {
		_ = out.write(&buildFrame{Type: frameTypeDone, Reason: buildStreamSettleReason})
		out.sentinel()
	}

	live, alive := derive()
	if !alive {
		return
	}
	if !live {
		settle()
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
			live, alive := derive()
			if !alive {
				return
			}
			if !live {
				settle()
				return
			}
		}
	}
}

// chronological orders a milestone's runs oldest first, IN PLACE.
//
// The repository answers newest-first because that is what the run LIST wants.
// A narrative wants the opposite, and it must not be a reversal of somebody
// else's promise: sorting on the rows' own creation time keeps this stream
// correct if that read order ever changes. The id tie-break is what makes the
// order STABLE — two runs created in the same instant would otherwise be free to
// swap places between ticks, and a run's index is a label the console prints.
func chronological(rows []delivery.MilestoneRun) {
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.Before(rows[j].CreatedAt)
		}
		return rows[i].ID < rows[j].ID
	})
}
