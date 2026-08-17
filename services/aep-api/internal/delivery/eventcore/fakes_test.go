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

package eventcore

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// In-memory stand-ins for the ports, holding the same RULES the real providers
// hold rather than canned answers — a fake that cannot reproduce the rule
// cannot fail the way production does. The three that matter:
//
//   - fakeRuns derives "live" from the state, so a terminal run is invisible
//     exactly as it is in the repository;
//   - fakeBuilds REMEMBERS what it triggered, so the re-trigger budget is
//     counted against a growing run list just as it is against OpenChoreo;
//   - fakeIssues dedupes on an open issue with the same key, which is the
//     issue service's real contract and the thing minting's idempotency rests
//     on.

const (
	testOrg     = "org1"
	testProject = "proj1"
	testRepo    = "acme/widgets"
)

// ---- runs -----------------------------------------------------------------

type fakeRuns struct {
	mu    sync.Mutex
	rows  []delivery.MilestoneRun
	bumps []delivery.RunBudget
	err   error
}

func newFakeRuns(rows ...delivery.MilestoneRun) *fakeRuns { return &fakeRuns{rows: rows} }

// aRun builds a live spec run over a milestone.
func aRun(id string, milestone int, state string) delivery.MilestoneRun {
	return delivery.MilestoneRun{
		ID: id, OrgID: testOrg, ProjectID: testProject,
		MilestoneNumber: milestone, MilestoneTitle: fmt.Sprintf("v%d", milestone),
		Origin: delivery.RunOriginSpecBuild, State: state,
		CycleCeiling: delivery.RunDefaultCycleCeiling,
	}
}

func (f *fakeRuns) LiveRunForMilestone(_ context.Context, _, _ string, milestone int) (*delivery.MilestoneRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].MilestoneNumber == milestone && !delivery.IsTerminalRunState(f.rows[i].State) {
			return &f.rows[i], nil
		}
	}
	return nil, nil
}

func (f *fakeRuns) LiveRunsForProject(context.Context, string, string) ([]delivery.MilestoneRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []delivery.MilestoneRun
	for i := range f.rows {
		if !delivery.IsTerminalRunState(f.rows[i].State) {
			out = append(out, f.rows[i])
		}
	}
	return out, nil
}

func (f *fakeRuns) DeployedMilestoneRun(context.Context, string, string) (*delivery.MilestoneRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rows {
		if f.rows[i].Origin == delivery.RunOriginSpecBuild && f.rows[i].State == delivery.RunStateSucceeded {
			return &f.rows[i], nil
		}
	}
	return nil, nil
}

func (f *fakeRuns) KnownMilestones(context.Context, string, string) ([]MilestoneRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	seen := map[int]bool{}
	var out []MilestoneRef
	for i := range f.rows {
		if seen[f.rows[i].MilestoneNumber] {
			continue
		}
		seen[f.rows[i].MilestoneNumber] = true
		out = append(out, MilestoneRef{Number: f.rows[i].MilestoneNumber, Title: f.rows[i].MilestoneTitle})
	}
	return out, nil
}

func (f *fakeRuns) BumpBudget(_ context.Context, _ string, counter delivery.RunBudget) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bumps = append(f.bumps, counter)
	return nil
}

// ---- cycles ---------------------------------------------------------------

type fakeCycles struct {
	mu      sync.Mutex
	latest  *delivery.RunCycle
	notedPR []string
	// decisions records every merge decision written, as
	// "<cycle>:<verdict>:<resolves…>" — the cycle's record of what it worked.
	decisions []string
	closed    []string
}

func newFakeCycles(cycle *delivery.RunCycle) *fakeCycles { return &fakeCycles{latest: cycle} }

func aCycle(id, runID string) *delivery.RunCycle {
	return &delivery.RunCycle{ID: id, OrgID: testOrg, ProjectID: testProject, RunID: runID, Kind: delivery.CycleKindCoding}
}

func (f *fakeCycles) Latest(context.Context, string, string) (*delivery.RunCycle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.latest, nil
}

func (f *fakeCycles) NotePullRequest(_ context.Context, id string, pr delivery.CyclePullRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The URL rides the recorded string: it is the fact the console links, so a
	// writer that drops it has to fail a test.
	f.notedPR = append(f.notedPR, fmt.Sprintf("%s:%s:%d:%s", id, pr.Branch, pr.Number, pr.URL))
	if f.latest != nil && f.latest.ID == id && f.latest.EndedAt == nil {
		f.latest.Branch, f.latest.PRNumber, f.latest.PRDraft = pr.Branch, pr.Number, pr.Draft
		f.latest.PRURL = pr.URL
	}
	return nil
}

func (f *fakeCycles) NoteMergeDecision(_ context.Context, id string, resolves []int, verdict, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisions = append(f.decisions, fmt.Sprintf("%s:%s:%v", id, verdict, resolves))
	if f.latest != nil && f.latest.ID == id && f.latest.EndedAt == nil {
		f.latest.Resolves = delivery.IssueNumbers(resolves)
		f.latest.MergeVerdict, f.latest.MergeReason = verdict, reason
	}
	return nil
}

func (f *fakeCycles) FinishCycle(_ context.Context, id, mergeSHA string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, fmt.Sprintf("%s:%s", id, mergeSHA))
	if f.latest != nil && f.latest.ID == id && f.latest.EndedAt == nil {
		now := time.Now().UTC()
		f.latest.MergeSHA, f.latest.EndedAt = mergeSHA, &now
	}
	return nil
}

// ---- issues ---------------------------------------------------------------

type fakeIssues struct {
	mu sync.Mutex
	// byMilestone is the milestone's open issues, keyed by milestone number.
	byMilestone map[int][]sourcecontrol.IssueInfo
	counts      map[int]*sourcecontrol.MilestoneIssueCounts
	// countsErr fails MilestoneIssueCounts when set — see failCounts.
	countsErr error
	created   []sourcecontrol.CreateIssueRequest
	assigned  []string
	// labelled records the labels stamped on an existing issue, keyed by issue
	// number — the write adoption makes to put an issue in the working set.
	labelled map[int][]string
	next     int
}

func newFakeIssues() *fakeIssues {
	return &fakeIssues{
		byMilestone: map[int][]sourcecontrol.IssueInfo{},
		counts:      map[int]*sourcecontrol.MilestoneIssueCounts{},
		labelled:    map[int][]string{},
		next:        100,
	}
}

// withWork puts open agent-work issues in a milestone.
func (f *fakeIssues) withWork(milestone int, numbers ...int) *fakeIssues {
	for _, n := range numbers {
		f.byMilestone[milestone] = append(f.byMilestone[milestone], sourcecontrol.IssueInfo{
			Number: n, State: "open", Labels: []string{delivery.LabelAgentWork},
		})
	}
	return f
}

// withValidationIssue puts the milestone's open VALIDATION issue in it, labelled
// the way the minter files it: `aep:validation` and nothing else. The absent
// `aep` label is the point — it keeps the issue out of the dispatch working set,
// and any read that narrows on `aep` cannot see this issue at all.
func (f *fakeIssues) withValidationIssue(milestone, number int) *fakeIssues {
	f.byMilestone[milestone] = append(f.byMilestone[milestone], sourcecontrol.IssueInfo{
		Number: number, State: "open", Labels: []string{delivery.LabelValidationWork},
	})
	return f
}

// withCounts gives a milestone its open-issue populations: gates, working set
// ("aep", non-gate, non-validation) and grand total. work and total are stated
// separately on purpose — the gap between them is the ledger, the population
// the dispatch predicate must ignore.
//
// The numbers are a DESCRIPTION of the milestone, not the counts themselves:
// they are turned into labelled issues and then counted the way the host counts
// them. A fake that let a test state the counts directly is what let the
// dispatch predicate ship with the wrong arithmetic.
func (f *fakeIssues) withCounts(milestone, provision, work, total int) *fakeIssues {
	ledger := total - provision - work
	if ledger < 0 {
		panic(fmt.Sprintf("fakeIssues.withCounts: total %d is smaller than %d gates plus %d work",
			total, provision, work))
	}
	labels := make([][]string, 0, total)
	for range provision {
		labels = append(labels, []string{delivery.LabelProvisionGate})
	}
	for range work {
		labels = append(labels, []string{delivery.LabelAgentWork})
	}
	for range ledger {
		labels = append(labels, nil) // human-filed: no "aep", never worked
	}
	f.counts[milestone] = hostCounts(labels...)
	return f
}

// hostCounts answers a milestone's open-issue populations the way the REAL host
// does, given each open issue's labels.
//
// The one rule it exists to hold: GitHub GraphQL's `labels:` argument is a
// UNION filter — an issue matches when it carries ANY of the listed labels. It
// is NOT an intersection (that is the REST `?labels=a,b` parameter, a different
// API over the same resource). A fake that modelled it as an intersection is
// precisely why a working set computed by inclusion-exclusion over label
// overlaps passed its tests and then read every real milestone as empty.
//
// Anything that wants a milestone's counts in this package goes through here,
// so no test can state a population the host could not produce.
func hostCounts(issues ...[]string) *sourcecontrol.MilestoneIssueCounts {
	anyOf := func(want ...string) int {
		n := 0
		for _, have := range issues {
			for _, w := range want {
				if delivery.HasLabel(have, w) {
					n++
					break
				}
			}
		}
		return n
	}
	return &sourcecontrol.MilestoneIssueCounts{
		OpenProvision: anyOf(delivery.LabelProvisionGate),
		OpenWorkOrExcluded: anyOf(delivery.LabelAgentWork,
			delivery.LabelProvisionGate, delivery.LabelValidationWork),
		OpenExcluded: anyOf(delivery.LabelProvisionGate, delivery.LabelValidationWork),
		OpenTotal:    len(issues),
	}
}

// CreateIssue reproduces the issue service's dedupe contract: a non-empty
// DedupeKey matching an OPEN issue already minted returns that issue instead
// of filing a second one.
func (f *fakeIssues) CreateIssue(_ context.Context, _, _ string, req sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if req.DedupeKey != "" {
		for i, prior := range f.created {
			if prior.DedupeKey == req.DedupeKey {
				return &sourcecontrol.IssueResult{Number: 100 + i + 1, Deduped: true}, nil
			}
		}
	}
	f.created = append(f.created, req)
	f.next++
	return &sourcecontrol.IssueResult{Number: f.next}, nil
}

// ListMilestoneIssues narrows the way the REST endpoint behind it does: its
// `?labels=a,b` parameter is AND — an issue must carry ALL of them.
//
// Note the ASYMMETRY with MilestoneIssueCounts below, whose GraphQL `labels:`
// argument over the same resource is a UNION. Two APIs, two rules; carrying one
// across to the other is the bug this fake pair exists to keep honest.
func (f *fakeIssues) ListMilestoneIssues(_ context.Context, _, _ string, filter sourcecontrol.MilestoneIssuesFilter) ([]sourcecontrol.IssueInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sourcecontrol.IssueInfo
	for _, issue := range f.byMilestone[filter.Number] {
		if filter.State != "" && filter.State != "all" && !strings.EqualFold(issue.State, filter.State) {
			continue
		}
		if !hasAllLabels(issue.Labels, filter.Labels) {
			continue
		}
		out = append(out, issue)
	}
	return out, nil
}

// hasAllLabels is the REST filter's AND rule.
func hasAllLabels(have, want []string) bool {
	for _, w := range want {
		if !delivery.HasLabel(have, w) {
			return false
		}
	}
	return true
}

func (f *fakeIssues) MilestoneIssueCounts(_ context.Context, _, _ string, number int) (*sourcecontrol.MilestoneIssueCounts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.countsErr != nil {
		return nil, f.countsErr
	}
	if c, ok := f.counts[number]; ok {
		return c, nil
	}
	return &sourcecontrol.MilestoneIssueCounts{}, nil
}

// failCounts makes the counts read fail, standing in for a transient host
// error on the ONE read adoption makes after its decisive writes have landed.
func (f *fakeIssues) failCounts(err error) *fakeIssues {
	f.countsErr = err
	return f
}

// withOpenIssues describes a milestone by each open issue's LABELS and counts it
// the way the host would — the same discipline withCounts follows, reached
// directly because a validation issue is a population withCounts cannot express
// (it takes gates, work and a total, and the validation issue is neither).
func (f *fakeIssues) withOpenIssues(milestone int, issues ...[]string) *fakeIssues {
	f.counts[milestone] = hostCounts(issues...)
	return f
}

// fakeOracle answers whether the project authored acceptance criteria.
type fakeOracle struct {
	has bool
	err error
}

func (f *fakeOracle) HasValidationCriteria(_ context.Context, _, _ string) (bool, error) {
	return f.has, f.err
}

func (f *fakeIssues) SetIssueMilestone(_ context.Context, _, _ string, number, milestoneNumber int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assigned = append(f.assigned, fmt.Sprintf("%d->%d", number, milestoneNumber))
	return nil
}

func (f *fakeIssues) AddLabels(_ context.Context, _, _ string, number int, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.labelled[number] = append(f.labelled[number], labels...)
	return nil
}

// labelsOn reports the labels stamped on one issue after the fact.
func (f *fakeIssues) labelsOn(number int) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.labelled[number]...)
}

func (f *fakeIssues) titles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.created))
	for _, c := range f.created {
		out = append(out, c.Title)
	}
	return out
}

// ---- pull requests --------------------------------------------------------

type fakePRs struct {
	mu     sync.Mutex
	states []*sourcecontrol.PullRequestState // consumed one per call, last repeats
	calls  int
	files  []string
	err    error
}

func openPR() *sourcecontrol.PullRequestState { return &sourcecontrol.PullRequestState{State: "open"} }
func mergedPR(sha string) *sourcecontrol.PullRequestState {
	return &sourcecontrol.PullRequestState{State: "closed", Merged: true, MergeCommitSHA: sha}
}

func (f *fakePRs) GetPullRequestState(context.Context, string, string, int) (*sourcecontrol.PullRequestState, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.states) == 0 {
		return openPR(), nil
	}
	i := f.calls
	if i >= len(f.states) {
		i = len(f.states) - 1
	}
	f.calls++
	return f.states[i], nil
}

func (f *fakePRs) ListPullRequestFiles(context.Context, string, string, int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files, nil
}

type fakeMerger struct {
	mu     sync.Mutex
	merged []int
	err    error
}

func (f *fakeMerger) MergePullRequest(_ context.Context, _, _ string, number int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.merged = append(f.merged, number)
	return nil
}

// ---- repos / design -------------------------------------------------------

type fakeRepoLookup struct{ err error }

func (f fakeRepoLookup) ByFullName(_ context.Context, fullName string) (string, string, error) {
	if f.err != nil {
		return "", "", f.err
	}
	if fullName != testRepo {
		return "", "", fmt.Errorf("repo %s not found", fullName)
	}
	return testOrg, testProject, nil
}

type fakeDesign struct {
	paths map[string]string
	// declared backs the wiring-conformance check; nil means the design declares
	// no resources, which is the pre-conformance status quo.
	declared map[string]ComponentResources
	err      error
}

func (f fakeDesign) ComponentPaths(context.Context, string, string) (map[string]string, error) {
	return f.paths, nil
}

func (f fakeDesign) DeclaredResources(context.Context, string, string) (map[string]ComponentResources, error) {
	return f.declared, f.err
}

// fakeWorkloads serves shipped workload.yaml content by repo path.
type fakeWorkloads struct {
	byPath map[string]string
	err    error
}

func (f fakeWorkloads) ReadFile(_ context.Context, _, _, path string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	content, ok := f.byPath[path]
	return content, ok, nil
}

type fakeRepoLister struct{ repos []RepoRef }

func (f fakeRepoLister) ListAll(context.Context) ([]RepoRef, error) { return f.repos, nil }

// ---- builds ---------------------------------------------------------------

// fakeBuilds models OpenChoreo closely enough for the budget to be real: a
// trigger APPENDS a run, so the next attempt count reflects it.
type fakeBuilds struct {
	mu        sync.Mutex
	runs      map[string][]BuildRun
	triggered []string
	err       error

	// staged counts StageBuildCredential calls and secretRefs records the
	// reference each trigger carried — the pair the fan-out's "stage once, reuse
	// everywhere" invariant is asserted on.
	staged    int
	stageRef  string
	stageErr  error
	secretRef []string
}

func newFakeBuilds() *fakeBuilds {
	return &fakeBuilds{runs: map[string][]BuildRun{}, stageRef: "org-git-secret"}
}

func (f *fakeBuilds) StageBuildCredential(_ context.Context, _, _, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stageErr != nil {
		return "", f.stageErr
	}
	f.staged++
	return f.stageRef, nil
}

func (f *fakeBuilds) TriggerBuildAtCommit(_ context.Context, _, _, component, _, runName, secretRef string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.triggered = append(f.triggered, runName)
	f.secretRef = append(f.secretRef, secretRef)
	f.runs[component] = append(f.runs[component], BuildRun{Name: runName, Status: "Running"})
	return nil
}

func (f *fakeBuilds) ListBuildRuns(_ context.Context, _, _, component string) ([]BuildRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[component], nil
}

func (f *fakeBuilds) triggeredFor(component string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, name := range f.triggered {
		if strings.Contains(name, component) {
			out = append(out, name)
		}
	}
	return out
}

// ---- supervisor -----------------------------------------------------------

type fakeSupervisor struct {
	mu      sync.Mutex
	signals []delivery.RunSignal
	started []delivery.StartRunRequest
	// admits is the run store the real supervisor writes to as part of starting a
	// run. Modelled because a caller that needs the run's id reads it back from
	// there, and a fake that started nothing would make that read look broken when
	// it is the one honest signal that nothing started.
	admits *fakeRuns
}

func (f *fakeSupervisor) SignalRun(_ context.Context, _ *delivery.MilestoneRun, _ string, payload delivery.RunSignal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = append(f.signals, payload)
	return nil
}

func (f *fakeSupervisor) StartRun(_ context.Context, req delivery.StartRunRequest) error {
	f.mu.Lock()
	f.started = append(f.started, req)
	f.mu.Unlock()
	if f.admits == nil {
		return nil
	}
	f.admits.mu.Lock()
	defer f.admits.mu.Unlock()
	f.admits.rows = append(f.admits.rows, delivery.MilestoneRun{
		ID: fmt.Sprintf("run-started-%d", len(f.admits.rows)+1), OrgID: testOrg, ProjectID: testProject,
		MilestoneNumber: req.MilestoneNumber, MilestoneTitle: req.MilestoneTitle,
		Origin: req.Origin, State: delivery.RunStateWaiting,
		// The budgets are SNAPSHOTTED onto the row in production, and the workflow
		// reads them from there rather than from the request — so a fake that
		// dropped them would let a test pass while the values never reached the run.
		CycleCeiling: req.CycleCeiling, ValidationAttempts: req.ValidationAttempts,
	})
	return nil
}

func (f *fakeSupervisor) named(name string) []delivery.RunSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []delivery.RunSignal
	for _, s := range f.signals {
		if s.Signal == name {
			out = append(out, s)
		}
	}
	return out
}

// fakeComponents records the pre-build component ensures the fan-out runs, and
// can be scripted to refuse one. EnsureComponent is called once per component
// from fanOutBuilds' per-component goroutine, so the recorded state needs the
// same mutex discipline as every other fake in this file. Assertions on
// `ensured` still read the field directly (matching fakeBuilds.triggered and
// friends elsewhere in this file) because fanOutBuilds' wg.Wait happens-before
// any test observes the result — the mutex is here to serialise the
// goroutines against EACH OTHER, not against the test.
type fakeComponents struct {
	mu      sync.Mutex
	ensured []string
	failFor string
}

func (f *fakeComponents) EnsureComponent(_ context.Context, _, _, component string) error {
	f.mu.Lock()
	f.ensured = append(f.ensured, component)
	fail := component == f.failFor
	f.mu.Unlock()
	if fail {
		return fmt.Errorf("design has no component %q", component)
	}
	return nil
}
