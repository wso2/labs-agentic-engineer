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

// The validation read model's rules, pinned here because they moved OFF the
// client to stop being derived twice. Each test names the mistake it prevents:
// these are the selections a console re-implementation got wrong.
package runread_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/delivery/runread"
	"github.com/wso2/aep/aep-api/internal/gen"
)

// ---- fixtures ---------------------------------------------------------------

const vOrg, vProject = "org-acme", "proj"

func at(min int) time.Time {
	return time.Date(2026, 5, 1, 12, min, 0, 0, time.UTC)
}

func ptr(t time.Time) *time.Time { return &t }

type vRuns struct{ rows []delivery.MilestoneRun }

func (f vRuns) MilestoneNumberForTag(_ context.Context, orgID, projectID, tag string) (int, bool, error) {
	if orgID != vOrg {
		return 0, false, nil
	}
	for i := range f.rows {
		if f.rows[i].ProjectID == projectID && f.rows[i].SpecTag() == tag {
			return f.rows[i].MilestoneNumber, true, nil
		}
	}
	return 0, false, nil
}

func (f vRuns) ListByMilestone(_ context.Context, orgID, projectID string, number int) ([]delivery.MilestoneRun, error) {
	var out []delivery.MilestoneRun
	if orgID != vOrg {
		return out, nil
	}
	for i := range f.rows {
		if f.rows[i].ProjectID == projectID && f.rows[i].MilestoneNumber == number {
			out = append(out, f.rows[i])
		}
	}
	return out, nil
}

func (f vRuns) ListByProject(_ context.Context, orgID, projectID string) ([]delivery.MilestoneRun, error) {
	var out []delivery.MilestoneRun
	if orgID != vOrg {
		return out, nil
	}
	for i := range f.rows {
		if f.rows[i].ProjectID == projectID {
			out = append(out, f.rows[i])
		}
	}
	return out, nil
}

type vCycles struct {
	byRun map[string][]delivery.RunCycle
}

func (f vCycles) ListByRun(_ context.Context, _, runID string) ([]delivery.RunCycle, error) {
	return f.byRun[runID], nil
}

func (f vCycles) ListValidationCyclesByProject(_ context.Context, _, _ string) ([]delivery.RunCycle, error) {
	var out []delivery.RunCycle
	for _, cs := range f.byRun {
		for i := range cs {
			if cs[i].Kind == delivery.CycleKindValidation {
				out = append(out, cs[i])
			}
		}
	}
	// The repository orders oldest first; the read model relies on it.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.Before(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

// vFiles answers the snapshot reads, recording the commit it was asked for so a
// test can prove the pinning rather than the content.
type vFiles struct {
	reportAt   map[string]string
	criteriaAt map[string][]gen.AcceptanceCriteriaFile
	askedAt    []string
}

func (f *vFiles) ReportAt(_ context.Context, _, _, at string) (string, bool, error) {
	f.askedAt = append(f.askedAt, "report@"+at)
	c, ok := f.reportAt[at]
	return c, ok, nil
}

func (f *vFiles) CriteriaAt(_ context.Context, _, _, at string) ([]gen.AcceptanceCriteriaFile, error) {
	f.askedAt = append(f.askedAt, "criteria@"+at)
	return f.criteriaAt[at], nil
}

func devRun(id string, milestone int, tag, state, verdict string) delivery.MilestoneRun {
	return delivery.MilestoneRun{
		ID: id, OrgID: vOrg, ProjectID: vProject,
		MilestoneNumber: milestone, MilestoneTitle: tag,
		Kind: delivery.RunKindDev, Origin: delivery.RunOriginSpecBuild,
		State: state, ValidationVerdict: verdict, CreatedAt: at(milestone),
	}
}

func vCycle(id, runID string, created int, ended *time.Time, mergeSHA, verdict string) delivery.RunCycle {
	return delivery.RunCycle{
		ID: id, OrgID: vOrg, ProjectID: vProject, RunID: runID,
		Kind: delivery.CycleKindValidation, CreatedAt: at(created), EndedAt: ended,
		MergeSHA: mergeSHA, ValidationVerdict: verdict,
	}
}

func reads(rows []delivery.MilestoneRun, cycles map[string][]delivery.RunCycle, files runread.ValidationSnapshotReader) *runread.ValidationReads {
	return runread.NewValidationReads(vRuns{rows: rows}, vCycles{byRun: cycles}, files)
}

// ---- the ledger -------------------------------------------------------------

func TestLedgerKeepsOneRowPerVersionIncludingNeverValidatedOnes(t *testing.T) {
	rows := []delivery.MilestoneRun{
		devRun("r3", 3, "v3", delivery.RunStateSucceeded, ""),
		devRun("r2b", 2, "v2", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed),
		devRun("r2a", 2, "v2", delivery.RunStateSucceeded, delivery.ValidationVerdictFailed),
	}
	out, err := reads(rows, nil, nil).Validations(context.Background(), vOrg, vProject)
	if err != nil {
		t.Fatalf("validations: %v", err)
	}
	if len(out.Validations) != 2 {
		t.Fatalf("want one row per version, got %d: %+v", len(out.Validations), out.Validations)
	}
	if out.Validations[0].Tag != "v3" || out.Validations[1].Tag != "v2" {
		t.Fatalf("want newest first, got %q then %q", out.Validations[0].Tag, out.Validations[1].Tag)
	}
	// A version nobody validated still gets a row: an absent row and a
	// never-validated one are the same screen to a reader, and this page exists
	// to tell them apart.
	if out.Validations[0].State != gen.ValidationStateNone {
		t.Fatalf("never-validated version: want none, got %q", out.Validations[0].State)
	}
	if out.Validations[0].StartedAt != nil || out.Validations[0].EndedAt != nil {
		t.Fatalf("never-validated version must carry no attempt clock: %+v", out.Validations[0])
	}
}

// #423: the newest run on a milestone is routinely one whose verdict means "I
// was never asked". Reading it reported a genuinely passed version as
// unvalidated.
func TestLedgerStateComesFromTheAnsweringRunNotTheNewestOne(t *testing.T) {
	incident := delivery.MilestoneRun{
		ID: "task1", OrgID: vOrg, ProjectID: vProject,
		MilestoneNumber: 1, MilestoneTitle: "v1",
		Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption,
		State: delivery.RunStateSucceeded, ValidationVerdict: delivery.ValidationVerdictSkipped,
		CreatedAt: at(9),
	}
	rows := []delivery.MilestoneRun{incident, devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed)}

	out, err := reads(rows, nil, nil).Validations(context.Background(), vOrg, vProject)
	if err != nil {
		t.Fatalf("validations: %v", err)
	}
	if len(out.Validations) != 1 {
		t.Fatalf("want one row, got %d", len(out.Validations))
	}
	if out.Validations[0].State != gen.ValidationStatePassed {
		t.Fatalf("an adopted incident must not hide the dev run's verdict: got %q", out.Validations[0].State)
	}
}

// A version's attempts can span several runs — the self-heal loop repeats
// within one row, a revalidation opens a new one — so "last validated" is the
// newest attempt on the MILESTONE, not the newest run that happens to hold one.
func TestLedgerTimingIsTheNewestAttemptAcrossRuns(t *testing.T) {
	revalidation := delivery.MilestoneRun{
		ID: "rv", OrgID: vOrg, ProjectID: vProject,
		MilestoneNumber: 1, MilestoneTitle: "v1",
		Kind: delivery.RunKindValidation, Origin: delivery.RunOriginRevalidate,
		State: delivery.RunStateSucceeded, ValidationVerdict: delivery.ValidationVerdictFailed,
		CreatedAt: at(30),
	}
	rows := []delivery.MilestoneRun{revalidation, devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed)}
	cycles := map[string][]delivery.RunCycle{
		"r1": {vCycle("c1", "r1", 10, ptr(at(12)), "sha-old", delivery.ValidationVerdictPassed)},
		"rv": {vCycle("c2", "rv", 31, ptr(at(40)), "sha-new", delivery.ValidationVerdictFailed)},
	}
	out, err := reads(rows, cycles, nil).Validations(context.Background(), vOrg, vProject)
	if err != nil {
		t.Fatalf("validations: %v", err)
	}
	got := out.Validations[0]
	if got.StartedAt == nil || !got.StartedAt.Equal(at(31)) {
		t.Fatalf("want the newest attempt's start, got %v", got.StartedAt)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(at(40)) {
		t.Fatalf("want the newest attempt's end, got %v", got.EndedAt)
	}
	if got.State != gen.ValidationStateFailed {
		t.Fatalf("the revalidation owns the answer now: got %q", got.State)
	}
}

func TestLedgerReportsARunningAttemptWithNoEnd(t *testing.T) {
	live := devRun("r1", 1, "v1", delivery.RunStateRunning, "")
	cycles := map[string][]delivery.RunCycle{
		"r1": {vCycle("c1", "r1", 10, nil, "", "")},
	}
	out, err := reads([]delivery.MilestoneRun{live}, cycles, nil).Validations(context.Background(), vOrg, vProject)
	if err != nil {
		t.Fatalf("validations: %v", err)
	}
	got := out.Validations[0]
	if got.State != gen.ValidationStateRunning {
		t.Fatalf("want running, got %q", got.State)
	}
	if got.EndedAt != nil {
		t.Fatalf("an open attempt has no end; the column renders a dash: %v", got.EndedAt)
	}
	if got.StartedAt == nil {
		t.Fatal("an open attempt still has a start, which is what the duration counts from")
	}
}

// ---- one version's history --------------------------------------------------

func TestDetailKeepsValidatingRunsAndValidationCyclesOnly(t *testing.T) {
	incident := delivery.MilestoneRun{
		ID: "task1", OrgID: vOrg, ProjectID: vProject,
		MilestoneNumber: 1, MilestoneTitle: "v1",
		Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption,
		State: delivery.RunStateSucceeded, CreatedAt: at(9),
	}
	rows := []delivery.MilestoneRun{incident, devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed)}
	cycles := map[string][]delivery.RunCycle{
		"r1": {
			{ID: "coding", OrgID: vOrg, RunID: "r1", Kind: delivery.CycleKindCoding, CreatedAt: at(1)},
			vCycle("val", "r1", 5, ptr(at(8)), "sha1", delivery.ValidationVerdictPassed),
		},
		"task1": {{ID: "fix", OrgID: vOrg, RunID: "task1", Kind: delivery.CycleKindCoding, CreatedAt: at(9)}},
	}
	out, err := reads(rows, cycles, nil).ValidationForTag(context.Background(), vOrg, vProject, "v1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "r1" {
		t.Fatalf("only runs that attempted validation belong here: %+v", out.Runs)
	}
	if len(out.Runs[0].Cycles) != 1 || out.Runs[0].Cycles[0].ID != "val" {
		t.Fatalf("want the validation cycle alone, got %+v", out.Runs[0].Cycles)
	}
	if out.State != gen.ValidationStatePassed {
		t.Fatalf("want passed, got %q", out.State)
	}
	if out.Live {
		t.Fatal("everything settled; live must be false")
	}
}

// `live` is the ONE refusal the console pre-empts, and it is about the
// milestone rather than about validation: a coding run rebuilding the version
// blocks a fresh validation exactly as a validation run does.
func TestDetailReportsLiveForANonValidatingRunToo(t *testing.T) {
	building := delivery.MilestoneRun{
		ID: "task1", OrgID: vOrg, ProjectID: vProject,
		MilestoneNumber: 1, MilestoneTitle: "v1",
		Kind: delivery.RunKindTask, Origin: delivery.RunOriginIncidentAdoption,
		State: delivery.RunStateRunning, CreatedAt: at(9),
	}
	rows := []delivery.MilestoneRun{building, devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed)}
	cycles := map[string][]delivery.RunCycle{
		"r1":    {vCycle("val", "r1", 5, ptr(at(8)), "sha1", delivery.ValidationVerdictPassed)},
		"task1": {{ID: "fix", OrgID: vOrg, RunID: "task1", Kind: delivery.CycleKindCoding, CreatedAt: at(9)}},
	}
	out, err := reads(rows, cycles, nil).ValidationForTag(context.Background(), vOrg, vProject, "v1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !out.Live {
		t.Fatal("a live task run still blocks a fresh validation")
	}
	if len(out.Runs) != 1 || out.Runs[0].ID != "r1" {
		t.Fatalf("live is not a reason to list a run that attempted nothing: %+v", out.Runs)
	}
}

// A live run holding a fatal verdict is mid-repair, not finished. Rendering the
// bare verdict would say the version failed while the platform is fixing it.
func TestDetailReportsAwaitingFixWhileTheLoopRepairs(t *testing.T) {
	repairing := devRun("r1", 1, "v1", delivery.RunStateRunning, delivery.ValidationVerdictFailed)
	cycles := map[string][]delivery.RunCycle{
		"r1": {
			vCycle("val1", "r1", 5, ptr(at(8)), "sha1", delivery.ValidationVerdictFailed),
			{ID: "fix", OrgID: vOrg, RunID: "r1", Kind: delivery.CycleKindFix, CreatedAt: at(9)},
		},
	}
	out, err := reads([]delivery.MilestoneRun{repairing}, cycles, nil).ValidationForTag(context.Background(), vOrg, vProject, "v1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if out.State != gen.ValidationStateAwaitingFix {
		t.Fatalf("want awaiting-fix, got %q", out.State)
	}
}

// `deployed` is what lets the console stop offering a revalidation it cannot
// honestly make: the runner drives whatever is serving, so only the deployed
// version can be judged. It is a comparison across the PROJECT's rows — a
// milestone cannot tell from its own rows whether something newer has shipped.
func TestDetailReportsWhetherThisVersionIsTheDeployedOne(t *testing.T) {
	rows := []delivery.MilestoneRun{
		devRun("r9", 9, "v9", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed),
		devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed),
	}
	cycles := map[string][]delivery.RunCycle{
		"r9": {vCycle("c9", "r9", 20, ptr(at(22)), "sha9", delivery.ValidationVerdictPassed)},
		"r1": {vCycle("c1", "r1", 5, ptr(at(8)), "sha1", delivery.ValidationVerdictPassed)},
	}

	newest, err := reads(rows, cycles, nil).ValidationForTag(context.Background(), vOrg, vProject, "v9")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !newest.Deployed {
		t.Fatal("the newest succeeded version is the deployed one")
	}

	older, err := reads(rows, cycles, nil).ValidationForTag(context.Background(), vOrg, vProject, "v1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if older.Deployed {
		t.Fatal("a superseded version is not deployed, and must not offer a revalidation")
	}
}

// A running newer version does not unseat the live one — `deployed` follows the
// newest SUCCEEDED dev run. Otherwise the only judgeable version would go
// unofferable for the whole of every build.
func TestDetailKeepsTheDeployedFlagWhileANewerVersionBuilds(t *testing.T) {
	rows := []delivery.MilestoneRun{
		devRun("r9", 9, "v9", delivery.RunStateRunning, ""),
		devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed),
	}
	cycles := map[string][]delivery.RunCycle{
		"r1": {vCycle("c1", "r1", 5, ptr(at(8)), "sha1", delivery.ValidationVerdictPassed)},
	}
	out, err := reads(rows, cycles, nil).ValidationForTag(context.Background(), vOrg, vProject, "v1")
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	if !out.Deployed {
		t.Fatal("a building v9 does not unseat a live v1")
	}
}

func TestDetailIsNotFoundForAVersionNoRunEverWorked(t *testing.T) {
	_, err := reads(nil, nil, nil).ValidationForTag(context.Background(), vOrg, vProject, "v9")
	if !errors.Is(err, runread.ErrTagNotFound) {
		t.Fatalf("want ErrTagNotFound, got %v", err)
	}
}

// ---- one attempt's evidence -------------------------------------------------

func TestSnapshotReadsBothHalvesAtTheAttemptsOwnCommit(t *testing.T) {
	rows := []delivery.MilestoneRun{devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed)}
	cycles := map[string][]delivery.RunCycle{
		"r1": {vCycle("val", "r1", 5, ptr(at(8)), "sha-attempt", delivery.ValidationVerdictPassed)},
	}
	files := &vFiles{
		reportAt:   map[string]string{"sha-attempt": `{"scenarios":[]}`},
		criteriaAt: map[string][]gen.AcceptanceCriteriaFile{"sha-attempt": {{Path: "specs/validation/acceptance/a.feature", Content: "Feature: a"}}},
	}
	out, err := reads(rows, cycles, files).ValidationSnapshot(context.Background(), vOrg, vProject, "v1", "val")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if out.Commit != "sha-attempt" {
		t.Fatalf("want the attempt's commit, got %q", out.Commit)
	}
	if out.Report == nil || *out.Report != `{"scenarios":[]}` {
		t.Fatalf("want the committed report, got %v", out.Report)
	}
	if len(out.Criteria) != 1 {
		t.Fatalf("want the criteria at that commit, got %+v", out.Criteria)
	}
	// The pairing is the point: both halves at ONE commit. Reading criteria at
	// the tip would report scenarios authored since as unanswered.
	for _, asked := range files.askedAt {
		if asked != "report@sha-attempt" && asked != "criteria@sha-attempt" {
			t.Fatalf("both halves must be pinned to the attempt's commit, got %q", asked)
		}
	}
}

// A running attempt has no commit. HEAD is genuinely what the runner is driving
// — it reads the criteria at HEAD when it executes — and no report exists yet.
func TestSnapshotOfARunningAttemptReadsCriteriaAtHeadAndHasNoReport(t *testing.T) {
	rows := []delivery.MilestoneRun{devRun("r1", 1, "v1", delivery.RunStateRunning, "")}
	cycles := map[string][]delivery.RunCycle{"r1": {vCycle("val", "r1", 5, nil, "", "")}}
	files := &vFiles{
		criteriaAt: map[string][]gen.AcceptanceCriteriaFile{"": {{Path: "specs/validation/acceptance/a.feature", Content: "Feature: a"}}},
	}
	out, err := reads(rows, cycles, files).ValidationSnapshot(context.Background(), vOrg, vProject, "v1", "val")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if out.Report != nil {
		t.Fatalf("a running attempt has committed nothing: %v", *out.Report)
	}
	if len(out.Criteria) != 1 {
		t.Fatalf("want the criteria being driven right now, got %+v", out.Criteria)
	}
	for _, asked := range files.askedAt {
		if asked == "report@" {
			t.Fatal("a running attempt must not be asked for a report at the tip")
		}
	}
}

// Ended without merging: the attempt never landed. That is a different sentence
// from "produced an empty report", so it is a miss rather than a blank answer.
func TestSnapshotOfAnAttemptThatNeverLandedIsNotFound(t *testing.T) {
	rows := []delivery.MilestoneRun{devRun("r1", 1, "v1", delivery.RunStateFailed, "")}
	cycles := map[string][]delivery.RunCycle{"r1": {vCycle("val", "r1", 5, ptr(at(8)), "", "")}}
	_, err := reads(rows, cycles, &vFiles{}).ValidationSnapshot(context.Background(), vOrg, vProject, "v1", "val")
	if !errors.Is(err, runread.ErrCycleNotFound) {
		t.Fatalf("want ErrCycleNotFound, got %v", err)
	}
}

// The cycle is looked for UNDER the version asked for. A cycle id is globally
// unique, so resolving it directly would let any version's path read any other
// version's attempt.
func TestSnapshotRefusesACycleBelongingToAnotherVersion(t *testing.T) {
	rows := []delivery.MilestoneRun{
		devRun("r2", 2, "v2", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed),
		devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed),
	}
	cycles := map[string][]delivery.RunCycle{
		"r1": {vCycle("val-v1", "r1", 5, ptr(at(8)), "sha1", delivery.ValidationVerdictPassed)},
		"r2": {vCycle("val-v2", "r2", 15, ptr(at(18)), "sha2", delivery.ValidationVerdictPassed)},
	}
	_, err := reads(rows, cycles, &vFiles{}).ValidationSnapshot(context.Background(), vOrg, vProject, "v2", "val-v1")
	if !errors.Is(err, runread.ErrCycleNotFound) {
		t.Fatalf("want ErrCycleNotFound for a cycle of another version, got %v", err)
	}
}

// A coding cycle is not an attempt, even under the right version.
func TestSnapshotRefusesANonValidationCycle(t *testing.T) {
	rows := []delivery.MilestoneRun{devRun("r1", 1, "v1", delivery.RunStateSucceeded, delivery.ValidationVerdictPassed)}
	cycles := map[string][]delivery.RunCycle{
		"r1": {{ID: "coding", OrgID: vOrg, RunID: "r1", Kind: delivery.CycleKindCoding, CreatedAt: at(1), MergeSHA: "sha1"}},
	}
	_, err := reads(rows, cycles, &vFiles{}).ValidationSnapshot(context.Background(), vOrg, vProject, "v1", "coding")
	if !errors.Is(err, runread.ErrCycleNotFound) {
		t.Fatalf("want ErrCycleNotFound, got %v", err)
	}
}
