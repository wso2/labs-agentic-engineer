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

package delivery

import (
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The label vocabulary is tested here rather than through the call sites
// because it is the one rule the whole loop routes on. A predicate that empties
// a working set does not fail loudly: it settles a version nobody built, and it
// does so on a live project (ADR-0011, "A lesson worth keeping").

// A milestone's issues, as label sets — the shape every case below is stated in,
// so no case can describe a population GitHub could not hold.
var (
	planned  = []string{LabelAgentWork, KindDevelopment}
	bug      = []string{LabelAgentWork, KindBug, SrcBuild}
	conflict = []string{LabelAgentWork, KindConflict}
	valid    = []string{LabelAgentWork, KindValidation}
	gate     = []string{KindProvision, "aep:dep/orders-db"}
	armed    = []string{LabelAgentWork} // a human armed it and classified nothing
	incident = []string{KindBug, SrcIncident}
	ledger   = []string(nil)
	// repair is a failed verdict's work: an ordinary armed bug whose SOURCE says
	// where it came from. Nothing routes on the source except one conditional —
	// whether draining a bug-fix run's working set reopens the version's
	// validation task.
	repair = []string{LabelAgentWork, KindBug, SrcValidation}
	// halted is work a failed run gave up on. Still armed and still of a workable
	// kind, because the marker is the reconcile SWEEP's to read: a live run's
	// cycle boundary never sees one (the run that halted them is terminal).
	halted = []string{LabelAgentWork, KindBug, LabelHalted}
)

func TestKindOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		labels []string
		want   string
	}{
		{"planned work", planned, KindDevelopment},
		{"a bug", bug, KindBug},
		{"a conflict", conflict, KindConflict},
		{"the validation task", valid, KindValidation},
		{"a dispatch gate", gate, KindProvision},
		{"armed but unclassified", armed, ""},
		{"a ledger issue", ledger, ""},
		{"case-insensitive, as GitHub matches labels", []string{"AEP", "Development"}, KindDevelopment},
		// Multi-kind is a corruption a human hand-stamped, never a state the
		// platform mints. The order is fixed so the answer is at least the same
		// every time it is asked.
		{"provision outranks everything", []string{KindDevelopment, KindProvision}, KindProvision},
		{"validation outranks development", []string{KindDevelopment, KindValidation}, KindValidation},
		{"conflict outranks bug", []string{KindBug, KindConflict}, KindConflict},
		{"bug outranks development", []string{KindDevelopment, KindBug}, KindBug},
	}
	for _, c := range cases {
		if got := KindOf(c.labels); got != c.want {
			t.Errorf("KindOf(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestWorkingSets pins what each loop may pick up. Every predicate is a POSITIVE
// membership test on a kind, which is the whole point of the vocabulary: the old
// model defined work by what it was NOT, and one mis-stated exclusion was enough
// to empty a live milestone.
// TestWorkKindOf pins the kind the PLATFORM works an issue as, which is KindOf
// with one substitution and one guard.
//
// The substitution — armed, no kind, read as a BUG — is the common human
// hand-over rather than an edge case: adoption stamps the arming switch and
// deliberately no kind. The guard is the arming switch itself, which is what
// makes this readable outside a working-set predicate: an UNARMED kindless issue
// is a ledger note and has no kind at all.
//
// Both halves are load-bearing at the same call site. Supersede decides which of
// a superseded version's issues are carried forward, and it must read the same
// kind the working set does — reading plain KindOf closed every human-adopted
// defect the moment the next version was cut, while defaulting a ledger note to
// `bug` would carry a human's note into a version it was never about.
func TestWorkKindOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		labels []string
		want   string
	}{
		{"planned work", planned, KindDevelopment},
		{"a bug", bug, KindBug},
		{"a conflict", conflict, KindConflict},
		{"the validation task", valid, KindValidation},
		{"a gate", gate, KindProvision},
		// The substitution.
		{"armed but unclassified is a defect", armed, KindBug},
		// The guard: unarmed is not the platform's, whatever else it carries.
		{"a bare ledger note has no kind", ledger, ""},
		{"an unarmed incident keeps the kind it states", incident, KindBug},
	}
	for _, c := range cases {
		if got := WorkKindOf(c.labels); got != c.want {
			t.Errorf("WorkKindOf(%s) = %q, want %q", c.name, got, c.want)
		}
	}
	// The property the working sets rest on: for anything ARMED, this and the
	// predicates cannot disagree about whether an issue is work.
	for _, labels := range [][]string{planned, bug, conflict, armed, repair, halted} {
		if WorkKindOf(labels) == "" {
			t.Errorf("armed work resolved to no kind at all (%v)", labels)
		}
	}
}

func TestWorkingSets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		labels   []string
		dev      bool
		task     bool
		gate     bool
		validate bool
	}{
		{"planned work is the dev loop's alone", planned, true, false, false, false},
		{"a bug is worked by both loops", bug, true, true, false, false},
		{"a conflict is worked by both loops", conflict, true, true, false, false},
		// Armed, because an agent IS dispatched at it and the platform must
		// auto-merge its pull request. In nobody's working set, because the
		// validation loop owns it and a working-set member would hold settle open.
		{"the validation task is nobody's working set", valid, false, false, false, true},
		// The case a naive "arm everything" would break. A gate carries no arming
		// label, so it is invisible to every working set — and the gate read is the
		// ONLY thing that can see it, which is what lets it hold dispatch.
		{"a gate is a hold, never work", gate, false, false, true, false},
		// Classification is not permission. A red-main incident is a bug so a human
		// can see what it is; nothing is dispatched at it until somebody arms it.
		{"an unarmed bug is ledger-only", incident, false, false, false, false},
		{"a bare human issue is ledger-only", ledger, false, false, false, false},
		// A human arming a bare issue is exactly this state. It reads as work to
		// both loops — the same answer the host's counts give for it, and the safe
		// direction: a stall is visible, a silent settle is not.
		{"armed but unclassified is work", armed, true, true, false, false},
	}
	for _, c := range cases {
		if got := InDevWorkingSet(c.labels); got != c.dev {
			t.Errorf("InDevWorkingSet(%s) = %v, want %v", c.name, got, c.dev)
		}
		if got := InTaskWorkingSet(c.labels); got != c.task {
			t.Errorf("InTaskWorkingSet(%s) = %v, want %v", c.name, got, c.task)
		}
		if got := IsDispatchGate(c.labels); got != c.gate {
			t.Errorf("IsDispatchGate(%s) = %v, want %v", c.name, got, c.gate)
		}
		if got := IsValidationWork(c.labels); got != c.validate {
			t.Errorf("IsValidationWork(%s) = %v, want %v", c.name, got, c.validate)
		}
	}
}

// hostCounts answers the populations the REAL host reports for a milestone
// holding these open issues: one label per field, exactly as the counts query
// filters one label per alias.
//
// It is spelled here as well as in sourcecontrol's own tests on purpose. The
// point of THIS copy is the comparison below — a fake that agreed with the
// arithmetic it was checking would prove nothing, so this one is written from
// the labels alone and never consults a predicate.
func hostCounts(issues ...[]string) *sourcecontrol.MilestoneIssueCounts {
	carrying := func(want string) int {
		n := 0
		for _, have := range issues {
			if HasLabel(have, want) {
				n++
			}
		}
		return n
	}
	return &sourcecontrol.MilestoneIssueCounts{
		OpenProvision:         carrying(KindProvision),
		OpenAgentWork:         carrying(LabelAgentWork),
		OpenDevelopment:       carrying(KindDevelopment),
		OpenValidation:        carrying(KindValidation),
		OpenValidationRepairs: carrying(SrcValidation),
		OpenTotal:             len(issues),
	}
}

// TestWorkingSetsAgreeWithTheHostCounts is the most load-bearing test in this
// file. The working set is computed TWICE by design — once per issue from its
// own labels (InDevWorkingSet, which the planner, the wiring publisher and the
// task reads use) and once as a COUNT in a single GraphQL round trip
// (OpenDevWork, which the dispatch predicate and the settle check use) — because
// no host call can both count cheaply and hand back the labels.
//
// Two computations of one rule is how a loop learns two different things about
// the same milestone. So they are checked against each other over every
// population a milestone can hold, including the ones a milestone should never
// hold.
func TestWorkingSetsAgreeWithTheHostCounts(t *testing.T) {
	t.Parallel()
	milestones := [][][]string{
		{planned, planned, planned},
		{planned, gate},
		{planned, planned, gate, valid, ledger, ledger},
		{bug, conflict, valid},
		{ledger, ledger, incident},
		{armed, planned, bug},
		{valid},
		{gate, gate},
		// A repair bug is an ORDINARY armed bug
		// in both working sets — its source is provenance, counted on its own
		// alias and subtracted from nothing — and a halted issue is still in the
		// working set of a live run, because the mark is the reconcile sweep's to
		// read and never the cycle boundary's.
		{repair, planned},
		{repair, repair, valid},
		{halted, planned, bug},
		// The milestone a dev run leaves when it gives up: planned work open, and
		// nothing at all for a task run.
		{planned, planned},
		{},
	}
	for _, milestone := range milestones {
		counts := hostCounts(milestone...)
		var dev, task, gates, repairs int
		for _, labels := range milestone {
			if InDevWorkingSet(labels) {
				dev++
			}
			if InTaskWorkingSet(labels) {
				task++
			}
			if IsDispatchGate(labels) {
				gates++
			}
			if HasLabel(labels, SrcValidation) {
				repairs++
			}
			// The narrowing that makes a budget mean something, asserted per issue
			// rather than only in the totals: a dev run that gave up leaves its
			// planned work open, and no task run may continue it.
			if KindOf(labels) == KindDevelopment && InTaskWorkingSet(labels) {
				t.Errorf("planned work is in a task run's working set (%v)", labels)
			}
		}
		if got := counts.OpenDevWork(); got != dev {
			t.Errorf("dev working set: counts say %d, labels say %d (milestone %v)", got, dev, milestone)
		}
		if got := counts.OpenTaskWork(); got != task {
			t.Errorf("task working set: counts say %d, labels say %d (milestone %v)", got, task, milestone)
		}
		if counts.OpenProvision != gates {
			t.Errorf("gates: counts say %d, labels say %d (milestone %v)", counts.OpenProvision, gates, milestone)
		}
		if counts.OpenValidationRepairs != repairs {
			t.Errorf("verdict-sourced repairs: counts say %d, labels say %d (milestone %v)",
				counts.OpenValidationRepairs, repairs, milestone)
		}
	}
}

// TestInWorkingSet is the mapping from a run SPECIES to the population it works,
// which is what the halt reads to decide whose leftovers it may mark.
func TestInWorkingSet(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		kind   string
		labels []string
		want   bool
	}{
		{"a build works planned work", RunKindDev, planned, true},
		{"a build works its bugs", RunKindDev, bug, true},
		// The narrowing that makes the two sets different. A dev run that gave up leaves planned
		// work open; a bug-fix run works the DEPLOYED version and must never
		// continue it with different budgets.
		{"a bug-fix run never works planned work", RunKindTask, planned, false},
		{"a bug-fix run works bugs", RunKindTask, bug, true},
		{"a bug-fix run works conflicts", RunKindTask, conflict, true},
		// Not a gap: a validation run polls no working set, and the repair issues a
		// failed verdict files are deliberately an ordinary run's work. Halting them
		// would break the repair chain.
		{"a validation run works no issue at all", RunKindValidation, bug, false},
		{"a validation run does not even work its own task", RunKindValidation, valid, false},
		// A kind nobody recognises answers the empty set, because the consumer marks
		// issues as abandoned: a wrong "yes" halts work nobody gave up on.
		{"an unknown kind works nothing", "sideways", bug, false},
	}
	for _, c := range cases {
		if got := InWorkingSet(c.kind, c.labels); got != c.want {
			t.Errorf("InWorkingSet(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestInCancelledWork pins what a CANCEL abandons per species, and the asymmetry
// with the halt above is the whole point of having two predicates.
//
// A halt says "this run gave up; another attempt at the same version may be worth
// making" — so the gates survive, because a retry still needs its dependencies
// resolved and closing them would erase the record of what the version was waiting
// on. A cancel says "this increment is abandoned" — so a BUILD's cancel takes the
// gates as well as the working set, and the milestone closes behind them.
//
// Two populations survive a build's cancel, and each is a POSITIVE statement
// rather than an oversight: the version's validation task (a handle on software
// still deployed) and the LEDGER (never the platform's to touch).
func TestInCancelledWork(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		kind   string
		labels []string
		want   bool
	}{
		// A cancelled build abandons EVERYTHING open in its milestone.
		{"a cancelled build closes its planned work", RunKindDev, planned, true},
		{"a cancelled build closes its bugs", RunKindDev, bug, true},
		{"a cancelled build closes its conflicts", RunKindDev, conflict, true},
		// The one thing a cancelled build spares. The task is a handle on software
		// that is already DEPLOYED — cancel reverts nothing — so closing it would
		// discard a pending judgement of what is still running.
		{"a cancelled build leaves the version's validation task open", RunKindDev, valid, false},
		// The difference from a halt, which leaves these alone: a halted run may be
		// retried in the same version, so its gates still name dependencies
		// somebody must resolve.
		{"a cancelled build closes the dispatch gates too", RunKindDev, gate, true},
		// An armed issue carrying no kind is the human hand-over, and it is in
		// flight like any other defect.
		{"a cancelled build closes an armed unclassified issue", RunKindDev, armed, true},
		// The LEDGER is never touched, by any cancel. Not armed, so not the
		// platform's: closing a human's note would put a machine comment on
		// somebody's own record, and the suppression never needed it — the sweep
		// skips a cancelled increment whole, and a note is not work to it anyway.
		{"a cancelled build leaves the ledger alone", RunKindDev, ledger, false},
		{"a cancelled build leaves an unarmed incident note alone", RunKindDev, incident, false},
		// A bug-fix run works the DEPLOYED version, which is not being withdrawn:
		// its plan, its gates and its verdict handle are untouched.
		{"a cancelled bug-fix run closes its bugs", RunKindTask, bug, true},
		{"a cancelled bug-fix run closes its conflicts", RunKindTask, conflict, true},
		{"a cancelled bug-fix run leaves planned work alone", RunKindTask, planned, false},
		{"a cancelled bug-fix run leaves the gates alone", RunKindTask, gate, false},
		{"a cancelled bug-fix run leaves the validation task alone", RunKindTask, valid, false},
		// A validation run's own consequence is the task it ADOPTED, closed by the
		// workflow on every ending. Reaching the milestone from here would close a
		// task a run cancelled before its first read never adopted.
		{"a cancelled validation closes nothing here", RunKindValidation, valid, false},
		{"a cancelled validation leaves the repair work alone", RunKindValidation, repair, false},
		{"an unknown kind closes nothing", "sideways", bug, false},
	}
	for _, c := range cases {
		if got := InCancelledWork(c.kind, c.labels); got != c.want {
			t.Errorf("InCancelledWork(%s) = %v, want %v", c.name, got, c.want)
		}
	}
	// The milestone rule, per kind: only an abandoned INCREMENT closes one.
	if !CancelClosesTheMilestone(RunKindDev) {
		t.Error("a cancelled build abandons the increment, so its milestone closes")
	}
	for _, kind := range []string{RunKindTask, RunKindValidation} {
		if CancelClosesTheMilestone(kind) {
			t.Errorf("a cancelled %s run withdraws no release — the milestone stays open", kind)
		}
	}
}

// TestDispatchable pins the ONE dispatch rule both halves of the loop read: the
// event plane, deciding whether a webhook is worth waking a waiting run for, and
// the supervisor, at every cycle boundary. They reach it from different shapes,
// so what they share is this function.
func TestDispatchable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		work MilestoneWork
		want bool
	}{
		{"work and no gate", MilestoneWork{Gates: 0, Work: 1}, true},
		{"an open gate holds work back", MilestoneWork{Gates: 1, Work: 3}, false},
		// Not "some issue is open": a milestone holding only ledger issues has
		// nothing to work, and waking a run for it costs a cycle boundary that
		// finds an empty working set.
		{"nothing to work", MilestoneWork{Gates: 0, Work: 0}, false},
		{"a gate with nothing behind it holds nothing worth waking", MilestoneWork{Gates: 1, Work: 0}, false},
	}
	for _, c := range cases {
		if got := c.work.Dispatchable(); got != c.want {
			t.Errorf("Dispatchable(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestDispatchable_AGateHoldsWorkItDoesNotEraseIt is the live failure in one
// assertion pair, told from the counts a freshly planned milestone produces: one
// planned task and one open gate. The working set is ONE throughout — the gate
// holds the dispatch and releases it on close. Read as ZERO, the run settles a
// version nobody built.
func TestDispatchable_AGateHoldsWorkItDoesNotEraseIt(t *testing.T) {
	t.Parallel()
	gated := hostCounts(planned, gate)
	if got := gated.OpenDevWork(); got != 1 {
		t.Fatalf("working set behind the gate = %d, want 1 (counts %+v)", got, gated)
	}
	if (MilestoneWork{Gates: gated.OpenProvision, Work: gated.OpenDevWork()}).Dispatchable() {
		t.Fatal("an open gate must hold the dispatch")
	}
	released := hostCounts(planned)
	if got := released.OpenDevWork(); got != 1 {
		t.Fatalf("working set after the gate closed = %d, want 1", got)
	}
	if !(MilestoneWork{Gates: released.OpenProvision, Work: released.OpenDevWork()}).Dispatchable() {
		t.Fatal("the closed gate must release the task the milestone was holding")
	}
}

// TestAdoptableByATaskRun is the guard that has to hold WITHOUT echo
// suppression. An install with no GitHub App writes through a human PAT, so
// every label the platform stamps returns as a human-sender delivery that
// suppression cannot tell from a real human's — and with `aep` as the arming
// trigger, the platform minting its own validation task adopted it as a bug-fix
// run. That run then parked on an empty working set and held the per-milestone
// live-run index, so the validation run could never be admitted and the version
// was never judged. Routing by kind is sender-independent.
func TestAdoptableByATaskRun(t *testing.T) {
	for _, c := range []struct {
		name   string
		labels []string
		want   bool
	}{
		// The live failure, exactly as it stood: the platform's own validation task.
		{"the version's validation task is not a bug-fix run's work",
			[]string{LabelAgentWork, KindValidation}, false},
		{"a dispatch gate is nobody's work", []string{KindProvision}, false},
		{"planned work belongs to the run holding the build mutex",
			[]string{LabelAgentWork, KindDevelopment}, false},
		{"a defect is adoptable", []string{LabelAgentWork, KindBug}, true},
		{"a conflict is adoptable", []string{LabelAgentWork, KindConflict}, true},
		// The ordinary human path: arming a bare issue, and the console's dispatch
		// button handing over one that carries nothing at all.
		{"an armed issue with no kind is adoptable", []string{LabelAgentWork}, true},
		{"an unlabelled issue is adoptable", nil, true},
	} {
		if got := AdoptableByATaskRun(c.labels); got != c.want {
			t.Errorf("AdoptableByATaskRun(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
