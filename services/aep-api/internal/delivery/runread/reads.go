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

import (
	"context"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// validationReportPath is where the validation runner commits its report. It is
// duplicated from delivery/validation's ReportFilePath rather than imported —
// that is a sibling slice — and the two are pinned together by
// TestValidationReportPathMatchesTheRunnersOwn.
const validationReportPath = "tests/validation/report.json"

// Reads serves a version's run story from the platform's own tables. No GitHub,
// no cluster, no Temporal: everything here is a row that a webhook or the
// supervisor already wrote, which is exactly what makes the console free to poll
// it at 5s while a run is live.
type Reads struct {
	runs   RunReader
	cycles CycleReader
	// recordings answers RunCycleView.recording. Optional: nil reports `none`
	// for every cycle, which is exactly right on a boot that records nothing.
	recordings RecordingReader
}

// NewReads wires the read service.
func NewReads(runs RunReader, cycles CycleReader) *Reads {
	return &Reads{runs: runs, cycles: cycles}
}

// WithRecordings attaches the recording-state reader so the cycle views can say
// what the platform can serve of each cycle's feed. Returns the receiver.
func (r *Reads) WithRecordings(rec RecordingReader) *Reads {
	r.recordings = rec
	return r
}

// RunsForTag returns every milestone run that has worked one spec version,
// newest first, each with its cycle records in dispatch order.
//
// The tag is resolved to a milestone number THROUGH THE RUN ROWS. That is the
// whole reason the run row keeps the milestone title: GitHub titles are
// renamable, so a title match against GitHub would silently answer about the
// wrong version, and the number is the only stable key.
func (r *Reads) RunsForTag(ctx context.Context, orgID, projectID, tag string) (*gen.BuildRunList, error) {
	if r == nil || r.runs == nil || r.cycles == nil {
		return nil, fmt.Errorf("runread: reads not configured")
	}
	number, found, err := r.runs.MilestoneNumberForTag(ctx, orgID, projectID, tag)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, ErrTagNotFound
	}
	rows, err := r.runs.ListByMilestone(ctx, orgID, projectID, number)
	if err != nil {
		return nil, err
	}
	out := &gen.BuildRunList{
		Tag:             tag,
		MilestoneNumber: int64(number),
		Runs:            make([]gen.MilestoneRunView, 0, len(rows)),
	}
	for i := range rows {
		cycles, cerr := r.cycles.ListByRun(ctx, orgID, rows[i].ID)
		if cerr != nil {
			return nil, cerr
		}
		out.Runs = append(out.Runs, runView(&rows[i], cycles, r.recordings))
	}
	return out, nil
}

// runView projects a run row plus its cycles onto the wire shape. It stores no
// loop position: that renders from the LATEST cycle, because fix and conflict
// cycles re-enter earlier phases and a flat phase enum would lie mid-loop.
func runView(row *delivery.MilestoneRun, cycles []delivery.RunCycle, recordings RecordingReader) gen.MilestoneRunView {
	view := gen.MilestoneRunView{
		ID:              row.ID,
		MilestoneNumber: int64(row.MilestoneNumber),
		MilestoneTitle:  row.MilestoneTitle,
		Kind:            gen.MilestoneRunViewKind(row.Kind),
		Origin:          gen.MilestoneRunViewOrigin(row.Origin),
		State:           gen.MilestoneRunViewState(row.State),
		TerminalReason:  row.TerminalReason,
		// Only the deploy gate sets these, and only while parked. They ride the
		// run story because that is what the build card renders: a `waiting` run
		// whose reason did not travel with it reads as a hung one.
		WaitingReason:        gen.MilestoneRunViewWaitingReason(row.WaitingReason),
		BlockingDependencies: row.BlockingDependencies,
		Budgets: gen.RunBudgets{
			CyclesTotal:      int64(row.CyclesTotal),
			CycleCeiling:     int64(row.CycleCeiling),
			FixCycles:        int64(row.FixCycles),
			ConflictCycles:   int64(row.ConflictCycles),
			BuildRetriggers:  int64(row.BuildRetriggers),
			ValidationCycles: int64(row.ValidationCycles),
		},
		Validation: validationView(row.ValidationVerdict, row.ValidationIssue),
		Failure:    failureView(row),
		Cycles:     make([]gen.RunCycleView, 0, len(cycles)),
		CreatedAt:  row.CreatedAt,
		StartedAt:  row.StartedAt,
		EndedAt:    row.EndedAt,
	}
	for i := range cycles {
		view.Cycles = append(view.Cycles, CycleView(&cycles[i], RecordingOf(recordings, &cycles[i])))
	}
	return view
}

// RecordingOf resolves one cycle's recording state through an optional reader.
// A nil reader answers `none`: the platform has no record of this cycle's feed,
// which is what a boot with no recording store honestly has.
func RecordingOf(recordings RecordingReader, c *delivery.RunCycle) gen.RunCycleViewRecording {
	if recordings == nil {
		return gen.RunCycleViewRecordingNone
	}
	return recordings.RecordingState(c)
}

// validationView carries the run's verdict — its LATEST attempt's — the issue
// behind it, and, when there is one to fetch, where the report lives. The
// deployment surface reads the verdict HERE; there is no separate validation
// endpoint.
//
// The path is a path and not a body because the run story is polled at 5s and a
// report body per run would ride every poll. The consumer pairs it with the LATEST
// validation cycle's mergeSha (CycleView) and reads it through read-file's `ref`:
// the report sits at ONE fixed path that every run — and every ATTEMPT within a run
// — overwrites, so reading the branch tip would hand a historical run the newest
// results. A run that validated more than once has several validation cycles, each
// with its own merge SHA and its own verdict, so "the validation cycle" is the last
// one for the run-level verdict and each one individually for a per-attempt read.
//
// A path is advertised only when a report can actually be there. `skipped` never
// ran a cycle, and `unreported` is precisely the verdict meaning nothing was
// committed — offering a path for either would send the console to a 404 and make
// a known-absent report look like a read failure.
func validationView(verdict string, issue int) gen.RunValidation {
	out := gen.RunValidation{}
	if verdict == "" {
		return out
	}
	out.Verdict = gen.RunValidationVerdict(verdict)
	out.Issue = int64(issue)
	switch verdict {
	case delivery.ValidationVerdictSkipped, delivery.ValidationVerdictUnreported:
	default:
		out.ReportPath = validationReportPath
	}
	return out
}

// CycleView projects one cycle record onto the wire shape. Exported because the
// progress stream emits the same shape in its `cycle` frames — one projection,
// so a list read and a stream frame can never describe the same cycle
// differently.
//
// `recording` is passed IN rather than derived here, and that is the seam: it is
// the only field on this shape that is not a column of the row. It answers what
// the PLATFORM can serve of the cycle's feed — `none` when it has no record at
// all, `lost` when it had one and cannot serve it — and those two are the same
// empty screen and very different bugs, so a projection that could not be told
// the difference would have to guess.
func CycleView(c *delivery.RunCycle, recording gen.RunCycleViewRecording) gen.RunCycleView {
	resolves := make([]int64, 0, len(c.Resolves))
	for _, n := range c.Resolves {
		resolves = append(resolves, int64(n))
	}
	return gen.RunCycleView{
		ID:           c.ID,
		Kind:         gen.RunCycleViewKind(c.Kind),
		Attempts:     int64(c.Attempts),
		JobRef:       c.JobRef,
		Branch:       c.Branch,
		PrNumber:     int64(c.PRNumber),
		PrURL:        c.PRURL,
		PrDraft:      c.PRDraft,
		Resolves:     resolves,
		MergeSha:     c.MergeSHA,
		MergeVerdict: gen.RunCycleViewMergeVerdict(c.MergeVerdict),
		MergeReason:  c.MergeReason,
		AgentReason:  c.AgentReason,
		// Set on validation cycles only. A run may validate more than once, so the
		// run's single verdict is the LATEST attempt's — this is how the timeline
		// shows that an earlier attempt failed and the next one passed.
		ValidationVerdict: gen.RunCycleViewValidationVerdict(c.ValidationVerdict),
		ValidationIssue:   int64(c.ValidationIssue),
		Recording:         recording,
		CreatedAt:         c.CreatedAt,
		EndedAt:           c.EndedAt,
	}
}

// failureView projects the run's failure record onto the wire, nil when the
// run has met no fault. The workflow id is derived here rather than stored:
// it is a function of facts the row already holds, and it is the handle an
// operator reads Temporal history by, so a support conversation can start
// from the card instead of from a log grep.
func failureView(row *delivery.MilestoneRun) *gen.RunFailure {
	f := row.Failure
	if f == nil || f.Code == "" {
		return nil
	}
	return &gen.RunFailure{
		Code:        f.Code,
		Phase:       f.Phase,
		Component:   f.Component,
		Dependency:  f.Dependency,
		Permanent:   f.Permanent,
		Attempts:    int64(f.Attempts),
		MaxAttempts: int64(f.MaxAttempts),
		FirstAt:     f.FirstAt,
		LastAt:      f.LastAt,
		Detail:      f.Detail,
		WorkflowID:  delivery.MilestoneRunWorkflowID(row.Kind, row.OrgID, row.ProjectID, row.MilestoneNumber),
	}
}
