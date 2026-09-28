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

// The validation read model: the ledger, one version's history, and one
// attempt's evidence.
//
// It is a sibling of the run story rather than a filter the client applies,
// because the filters ARE the platform's rules. Which kinds ask the criteria
// (delivery.RunValidates), which run holds the answer, and which commit an
// attempt's report is addressable at are all facts the supervisor acts on; a
// client re-deriving them is a second place for them to drift, and the surface
// that did exactly that read a newer non-validating run as the version's answer
// and hid a real verdict (#423).
//
// Its own type rather than methods on Reads, for the same reason CycleBuilds is
// its own: it needs collaborators the run story does not (a project-wide row
// read, and the Files API behind an attempt's evidence), and folding them into
// Reads would make the run story's boot depend on services it never calls.
type ValidationReads struct {
	runs      ValidationRunReader
	cycles    ValidationCycleReader
	snapshots ValidationSnapshotReader
	// recordings answers RunCycleView.recording. Optional: nil reports `none`,
	// which is the honest answer on a boot that records nothing.
	recordings RecordingReader
}

// NewValidationReads wires the validation read model. A nil snapshot reader
// leaves ValidationSnapshot erroring while the ledger and the history — pure
// row reads — keep working, which is this slice's degraded-boot contract.
func NewValidationReads(runs ValidationRunReader, cycles ValidationCycleReader, snapshots ValidationSnapshotReader) *ValidationReads {
	return &ValidationReads{runs: runs, cycles: cycles, snapshots: snapshots}
}

// WithRecordings attaches the recording-state reader so a cycle view can say
// what the platform can serve of its feed. Returns the receiver.
func (r *ValidationReads) WithRecordings(rec RecordingReader) *ValidationReads {
	r.recordings = rec
	return r
}

// Validations is the VERSION LEDGER seen from validation: one row per spec
// version the platform has worked, newest first.
//
// The row set is the build ledger's, deliberately — first row per SpecTag from
// the project's runs, newest first. A version that was never validated still
// gets a row: an absent row and a never-validated one are indistinguishable to
// a reader, and "never validated" is the most actionable state this page shows.
// Two endpoints computing their own idea of which versions exist would be two
// pages that can disagree about it.
//
// Two queries, whatever the project's size: the run rows, and every validation
// cycle in the project. Per-milestone reads were the reason validation stayed
// off the build ledger, and repeating them here would have moved the cost
// rather than removed it.
func (r *ValidationReads) Validations(ctx context.Context, orgID, projectID string) (*gen.ValidationList, error) {
	if r == nil || r.runs == nil || r.cycles == nil {
		return nil, fmt.Errorf("runread: validation reads not configured")
	}
	rows, err := r.runs.ListByProject(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	cycles, err := r.cycles.ListValidationCyclesByProject(ctx, orgID, projectID)
	if err != nil {
		return nil, err
	}
	// Oldest first from the query, so the LAST write per run wins and each run
	// maps to its newest validation attempt.
	latestByRun := make(map[string]*delivery.RunCycle, len(cycles))
	for i := range cycles {
		latestByRun[cycles[i].RunID] = &cycles[i]
	}

	out := &gen.ValidationList{Validations: make([]gen.ValidationSummary, 0, len(rows))}
	seen := make(map[string]bool, len(rows))
	for i := range rows {
		tag := rows[i].SpecTag()
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		out.Validations = append(out.Validations, validationSummary(tag, rows, &rows[i], latestByRun))
	}
	return out, nil
}

// validationSummary builds one ledger row. `ref` is the version's newest run of
// any kind — the row that named the tag — and the state comes from the newest
// run on its milestone that could have produced a verdict, which is a different
// row whenever something later re-judged the version.
func validationSummary(
	tag string,
	rows []delivery.MilestoneRun,
	ref *delivery.MilestoneRun,
	latestByRun map[string]*delivery.RunCycle,
) gen.ValidationSummary {
	answering := delivery.AnsweringRunOnMilestone(rows, ref.MilestoneNumber)
	state, decided := delivery.ValidationStageFromRun(answering)
	if !decided {
		state = delivery.ValidationStageWithCycle(answering, latestCycleOfRun(answering, latestByRun))
	}
	row := gen.ValidationSummary{
		Tag:             tag,
		MilestoneNumber: int64(ref.MilestoneNumber),
		State:           gen.ValidationState(state),
	}
	// The ATTEMPT's clock, across every run on the milestone: a version's
	// attempts can span several runs, and the ledger's duration column is about
	// the last attempt rather than the last run that happened to hold one.
	if newest := newestValidationCycleOnMilestone(rows, ref, latestByRun); newest != nil {
		row.StartedAt = &newest.CreatedAt
		row.EndedAt = newest.EndedAt
	}
	return row
}

// latestCycleOfRun is the run's newest VALIDATION cycle, which is what the
// undecided half of the stage derivation asks about. It is not the run's newest
// cycle of any kind: a live run repairing a failed verdict has a coding cycle in
// flight, and that is precisely the awaiting-fix case the caller must reach.
func latestCycleOfRun(run *delivery.MilestoneRun, latestByRun map[string]*delivery.RunCycle) *delivery.RunCycle {
	if run == nil {
		return nil
	}
	return latestByRun[run.ID]
}

// newestValidationCycleOnMilestone is the last attempt made against a version,
// wherever it happened — the self-heal loop repeats within one run, a
// revalidation opens a new one, and the ledger's "last validated" means neither
// of those distinctions.
func newestValidationCycleOnMilestone(
	rows []delivery.MilestoneRun,
	ref *delivery.MilestoneRun,
	latestByRun map[string]*delivery.RunCycle,
) *delivery.RunCycle {
	var newest *delivery.RunCycle
	for i := range rows {
		if rows[i].MilestoneNumber != ref.MilestoneNumber {
			continue
		}
		c := latestByRun[rows[i].ID]
		if c == nil {
			continue
		}
		if newest == nil || c.CreatedAt.After(newest.CreatedAt) {
			newest = c
		}
	}
	return newest
}

// ValidationForTag is one version's validation history: every run that could
// produce a verdict, newest first, each carrying its VALIDATION cycles only.
//
// Both filters happen here rather than in the client. They are the same
// selections the status aggregate makes, and the point of serving them is that
// the two surfaces stop deriving them separately.
func (r *ValidationReads) ValidationForTag(ctx context.Context, orgID, projectID, tag string) (*gen.ValidationDetail, error) {
	if r == nil || r.runs == nil || r.cycles == nil {
		return nil, fmt.Errorf("runread: validation reads not configured")
	}
	number, rows, err := r.milestoneRuns(ctx, orgID, projectID, tag)
	if err != nil {
		return nil, err
	}
	deployed, err := r.isDeployed(ctx, orgID, projectID, number)
	if err != nil {
		return nil, err
	}

	out := &gen.ValidationDetail{
		Tag:             tag,
		MilestoneNumber: int64(number),
		Deployed:        deployed,
		Runs:            make([]gen.MilestoneRunView, 0, len(rows)),
	}
	answering := delivery.AnsweringRunOnMilestone(rows, number)
	var answeringLatest *delivery.RunCycle
	for i := range rows {
		if !delivery.IsTerminalRunState(rows[i].State) {
			// Any live run, of any kind. A coding run rebuilding the version is as
			// much a reason to refuse a fresh validation as a validation run is, and
			// this is the ONE refusal the console pre-empts.
			out.Live = true
		}
		cycles, cerr := r.cycles.ListByRun(ctx, orgID, rows[i].ID)
		if cerr != nil {
			return nil, cerr
		}
		validation := validationCyclesOf(cycles)
		if answering != nil && rows[i].ID == answering.ID && len(validation) > 0 {
			answeringLatest = &validation[len(validation)-1]
		}
		// A run is listed because it ATTEMPTED validation, not because of its
		// kind. The kind says what a run is FOR, and the two do not line up: a
		// dev run carries the version's first attempt while RunValidates names
		// only the kind whose whole purpose is judging. Filtering on the fact
		// cannot be wrong either way — a run holding a validation cycle asked the
		// criteria, and a task run never holds one.
		if len(validation) == 0 {
			continue
		}
		out.Runs = append(out.Runs, runView(&rows[i], validation, r.recordings))
	}

	state, decided := delivery.ValidationStageFromRun(answering)
	if !decided {
		state = delivery.ValidationStageWithCycle(answering, answeringLatest)
	}
	out.State = gen.ValidationState(state)
	return out, nil
}

// isDeployed reports whether this milestone holds the version currently
// serving — the one condition, beside a live run, that decides whether a
// revalidation may be offered.
//
// It reads the project's rows rather than this milestone's: "deployed" is a
// comparison, and a milestone cannot tell from its own rows whether something
// newer has since shipped. The rule itself is delivery's (DeployedRun), beside
// the verdict vocabulary rather than here, so the answer this flag gives and
// the one the trigger is refused by come from one place.
func (r *ValidationReads) isDeployed(ctx context.Context, orgID, projectID string, milestoneNumber int) (bool, error) {
	rows, err := r.runs.ListByProject(ctx, orgID, projectID)
	if err != nil {
		return false, err
	}
	deployed := delivery.DeployedRun(rows)
	return deployed != nil && deployed.MilestoneNumber == milestoneNumber, nil
}

// milestoneRuns resolves a tag to its milestone and that milestone's runs,
// newest first. Shared by the detail read and the snapshot read so a cycle can
// only ever be found under the version it actually belongs to.
func (r *ValidationReads) milestoneRuns(ctx context.Context, orgID, projectID, tag string) (int, []delivery.MilestoneRun, error) {
	number, found, err := r.runs.MilestoneNumberForTag(ctx, orgID, projectID, tag)
	if err != nil {
		return 0, nil, err
	}
	if !found {
		return 0, nil, ErrTagNotFound
	}
	rows, err := r.runs.ListByMilestone(ctx, orgID, projectID, number)
	if err != nil {
		return 0, nil, err
	}
	return number, rows, nil
}

// validationCyclesOf keeps a run's validation cycles in dispatch order. Coding,
// fix and conflict cycles are dropped: they belong to how the version was
// delivered, which is the build page's story, and a verdict shown in two places
// invites the two to disagree.
func validationCyclesOf(cycles []delivery.RunCycle) []delivery.RunCycle {
	out := make([]delivery.RunCycle, 0, len(cycles))
	for i := range cycles {
		if cycles[i].Kind == delivery.CycleKindValidation {
			out = append(out, cycles[i])
		}
	}
	return out
}

// ValidationSnapshot is one attempt's report and the acceptance criteria it was
// judged against, read at a single commit.
//
// Three cases, decided by the cycle's own record and nothing else:
//
//	MERGED    both halves at its merge SHA — how the attempt was actually judged.
//	RUNNING   no commit exists yet, so the criteria come from HEAD, which is
//	          genuinely what the runner is driving (it reads them at HEAD when it
//	          executes), and the report is nil because none has been committed.
//	ENDED, UNMERGED
//	          the attempt never landed. 404 rather than an empty snapshot: "this
//	          attempt produced nothing" and "this attempt never finished" are
//	          different sentences and the client renders them differently.
func (r *ValidationReads) ValidationSnapshot(ctx context.Context, orgID, projectID, tag, cycleID string) (*gen.ValidationSnapshot, error) {
	if r == nil || r.runs == nil || r.cycles == nil {
		return nil, fmt.Errorf("runread: validation reads not configured")
	}
	if r.snapshots == nil {
		return nil, fmt.Errorf("runread: validation snapshot reader not configured")
	}
	_, rows, err := r.milestoneRuns(ctx, orgID, projectID, tag)
	if err != nil {
		return nil, err
	}
	cycle, err := r.validationCycleOn(ctx, orgID, rows, cycleID)
	if err != nil {
		return nil, err
	}
	if cycle.MergeSHA == "" && cycle.EndedAt != nil {
		return nil, ErrCycleNotFound
	}

	out := &gen.ValidationSnapshot{Commit: cycle.MergeSHA}
	criteria, err := r.snapshots.CriteriaAt(ctx, orgID, projectID, cycle.MergeSHA)
	if err != nil {
		return nil, err
	}
	out.Criteria = criteria
	if cycle.MergeSHA == "" {
		// Still running: nothing has been committed, and an absent report is not
		// the same fact as an empty one.
		return out, nil
	}
	content, found, err := r.snapshots.ReportAt(ctx, orgID, projectID, cycle.MergeSHA)
	if err != nil {
		return nil, err
	}
	if found {
		out.Report = &content
	}
	return out, nil
}

// validationCycleOn finds the cycle under the VERSION it was asked for. The
// scoping is the fence: a cycle id is globally unique, so resolving it directly
// would let a caller read any version's attempt through any version's path, and
// the org-scoped miss that makes cross-tenant probes indistinguishable from
// typos would not cover a same-org, wrong-version read.
func (r *ValidationReads) validationCycleOn(ctx context.Context, orgID string, rows []delivery.MilestoneRun, cycleID string) (*delivery.RunCycle, error) {
	for i := range rows {
		cycles, err := r.cycles.ListByRun(ctx, orgID, rows[i].ID)
		if err != nil {
			return nil, err
		}
		for j := range cycles {
			if cycles[j].ID == cycleID && cycles[j].Kind == delivery.CycleKindValidation {
				return &cycles[j], nil
			}
		}
	}
	return nil, ErrCycleNotFound
}
