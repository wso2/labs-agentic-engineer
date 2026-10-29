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

package validation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
)

// ReportFilePath is the run report the acceptance run commits, and the console
// renders. It is the counterpart of the feature files: the scenarios say what
// must hold, the report says what did.
const ReportFilePath = "tests/acceptance/report.json"

// SnapshotDirPath is where the assembler writes each failure's page tree. The
// report names those files rather than carrying them, so they are committed
// with it.
const SnapshotDirPath = "tests/acceptance/snapshots"

// reportDoc is the slice of the runner's report a VERDICT is derived from. The
// console reads the whole document; the run only needs to know whether anything
// the agent could decide came out negative.
type reportDoc struct {
	Scenarios []reportScenario `json:"scenarios"`
}

// reportScenario is one scenario the agent drove. There is no generated test
// code, so the steps ARE the evidence: each one carries the command that ran it
// and, where the exit code is not the verdict, what was observed.
type reportScenario struct {
	Feature     string `json:"feature"`
	FeatureFile string `json:"featureFile"`
	Line        int    `json:"line"`
	Rule        string `json:"rule"`
	Scenario    string `json:"scenario"`
	// Outcome is the agent's per-scenario verdict: passed | failed | blocked |
	// unjudgeable.
	Outcome string       `json:"outcome"`
	Steps   []reportStep `json:"steps"`
	// Reproduced says whether the outcome held when the scenario was driven a
	// second time: yes | no | unattempted. `no` is the one that redirects a
	// repair — a failure that passes on a second pass is a race rather than a
	// logic defect, and nothing else in the report separates them.
	Reproduced string `json:"reproduced"`
	// Note is one sentence on what the run saw that no other field holds. It
	// exists because three real reports put that class of observation in keys
	// nobody reads (`blockedAt`, `reason`) or smuggled it into a command string.
	// Observation only: the agent does not read the implementation, so a causal
	// claim from it is a guess about internals it has not seen.
	Note string `json:"note"`
	// Evidence is what the agent read AT THE MOMENT the scenario failed, while
	// the page was still open. Required on every non-passed outcome and FORBIDDEN
	// on a pass — the report checker holds the agent to that, because after the
	// run the page is gone and nothing can be recovered, while capture on a pass
	// is read by nobody.
	Evidence reportEvidence `json:"evidence"`
}

// reportEvidence is the failure-time capture: what the system was doing when the
// assertion lost.
//
// Network is the discriminating half. A request that left and came back 201 with
// the page unchanged is a different defect from no request at all, and they need
// opposite fixes.
//
// An EMPTY Network slice is evidence: nothing left the page. An absent one is a
// hole, and the checker refuses it. NotCaptured is the honest escape — set when
// the capture genuinely could not happen (the page had already navigated away),
// so the gap is stated rather than filled with something plausible.
type reportEvidence struct {
	Network []networkRequest `json:"network"`
	Console []string         `json:"console"`
	// SnapshotFile is where the assembler wrote the page tree, relative to the
	// repo root; see SnapshotDirPath. The report NAMES it rather than carrying it.
	SnapshotFile string `json:"snapshotFile"`
	// Snapshot carried the tree inline before the assembler split it out. Still
	// read so a report merged under the older schema renders rather than losing
	// its evidence pointer.
	Snapshot    string `json:"snapshot"`
	NotCaptured string `json:"notCaptured"`
}

// networkRequest is one request the page made around the deciding step.
type networkRequest struct {
	Method string `json:"method"`
	URL    string `json:"url"`
	Status int    `json:"status"`
}

// reportStep is one Gherkin step as executed.
type reportStep struct {
	Text    string `json:"text"`
	Keyword string `json:"keyword"`
	Command string `json:"command"`
	// Exit is a POINTER so "the command was not run" is distinguishable from
	// "it exited 0". A blocked scenario's later steps carry neither.
	Exit *int `json:"exit"`
	// Observed is what the agent read, required wherever the exit code does not
	// settle the step — a nonzero exit, a step with no command, or a command
	// that prints a value (`get count`) and so exits 0 merely by running.
	Observed string `json:"observed"`
}

// id is the scenario's natural key: Gherkin carries no ids, so identity is what
// the scenario IS — its feature by ID (B4: "F2", from `Feature: F2 Approvals`,
// so renaming a feature splits no history and opens no second repair issue),
// its rule and its name. ASCII-joined on purpose — this string becomes a
// GitHub dedupe label, which issue_service.go normalises and, past 50 chars,
// hashes.
func (s reportScenario) id() string {
	parts := make([]string, 0, 3)
	for _, p := range []string{featureID(s.Feature, s.FeatureFile), s.Rule, s.Scenario} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, strings.TrimSpace(p))
		}
	}
	return strings.Join(parts, " / ")
}

// deciding returns the INDEX of the step that settled the scenario, or -1. A
// nonzero exit wins; otherwise the first `Then` carrying an observation, which
// is how a value-returning command (`get count`) records its verdict.
//
// An index rather than the step itself, because the whole trace is now rendered
// and the deciding step has to be MARKED within it — and two steps of one
// scenario can carry the same text.
//
// **A `Then` wins over position.** Only a `Then` decides anything; a `When` may
// record what it saw on the way past, and taking the first observation of any
// keyword let one win on position alone — which also fed ReportDigest the
// request rather than the assertion.
func (s reportScenario) deciding() int {
	for i, st := range s.Steps {
		if st.Exit != nil && *st.Exit != 0 {
			return i
		}
	}
	keywords := s.effectiveKeywords()
	for i, st := range s.Steps {
		if keywords[i] == keywordThen && strings.TrimSpace(st.Observed) != "" {
			return i
		}
	}
	// No `Then` observed anything: fall back to whatever did, so a scenario that
	// records its reason on a `When` is still answerable rather than silent.
	for i, st := range s.Steps {
		if strings.TrimSpace(st.Observed) != "" {
			return i
		}
	}
	return -1
}

const keywordThen = "Then"

// effectiveKeywords resolves each step's keyword, carrying `And` / `But` / `*`
// onto the one they inherit — which is what Gherkin means by them, and what the
// run skill tells the agent they mean. Without this a `Then` continued by `And`
// is invisible to any predicate asking which steps assert, and the assertion
// that actually settled a scenario is routinely the continuation.
func (s reportScenario) effectiveKeywords() []string {
	out := make([]string, len(s.Steps))
	current := ""
	for i, st := range s.Steps {
		switch k := strings.TrimSpace(st.Keyword); k {
		case "And", "But", "*", "":
			// inherits whatever came before, including "" at the top of a scenario
		default:
			current = k
		}
		out[i] = current
	}
	return out
}

// FailedScenario is one scenario the report says the app did not satisfy, with
// everything a repair issue needs — so the issue is answerable from one read and
// never sends its reader back to the specification or to another ticket.
type FailedScenario struct {
	ID string
	// FeatureID is the scenario's feature ("F2"); Feature is its whole
	// `Feature:` line ("F2 Approvals").
	FeatureID string
	// Stories are the stories its rule stands for, from the rule's tags; nil
	// when the report was not read against a scope.
	Stories     []string
	Feature     string
	Rule        string
	Scenario    string
	FeatureFile string
	Line        int
	// Steps is the scenario's own Given/When/Then AS EXECUTED — every step, with
	// the command that ran it and what that command said.
	//
	// Every step, not just the one that settled it: only the whole trace
	// distinguishes "the `When` never happened" from "the `When` happened and the
	// app disagreed with the `Then`", and those need opposite fixes.
	Steps []FailedStep
	// Deciding indexes the step that settled the scenario, or -1.
	Deciding int
	// Reproduced is yes | no | unattempted — whether the failure held on a second
	// pass. It changes the issue's opening claim rather than appearing beside it,
	// because a reader should know whether it is chasing a race before it spends
	// attention on the trace.
	Reproduced string
	// Note is the run's one sentence on what the other fields do not hold.
	Note string
	// Evidence is what the run saw when it failed.
	Evidence FailedEvidence
}

// FailedStep is one Gherkin step as executed.
type FailedStep struct {
	Keyword  string
	Text     string
	Command  string
	Exit     *int
	Observed string
}

// FailedEvidence is the failure-time capture, as the report recorded it.
type FailedEvidence struct {
	Network []NetworkRequest
	Console []string
	// SnapshotFile is the repo-relative path the page tree was written to, which
	// the issue body points a reader at. Empty when the run captured none.
	SnapshotFile string
	// Snapshot is the inline tree an older report carried instead.
	Snapshot string
	// NotCaptured is why there is no capture, when the run said so explicitly.
	// It is rendered rather than hidden: a stated gap is information, and the
	// alternative to stating it is an agent inventing a plausible trace.
	NotCaptured string
}

// NetworkRequest is one request the page made around the deciding step.
type NetworkRequest struct {
	Method string
	URL    string
	Status int
}

// Report is a committed report as one version reads it: parsed once, and
// narrowed to the version's scope (Within) before any verdict, digest or
// failure is drawn from it.
type Report struct {
	doc reportDoc
	// scope is what Within narrowed it to; nil when it was not narrowed.
	scope *Scope
}

// ParseReport reads a committed report. An absent or unparseable one is a
// Report with nothing in it, whose verdict is `unreported`.
func ParseReport(raw []byte) Report {
	if len(raw) == 0 {
		return Report{}
	}
	var doc reportDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return Report{}
	}
	return Report{doc: doc}
}

// Within keeps the scenarios the version validates (B4). An agent that drove
// a scenario outside the scope reported on code nobody asked to build, so its
// result counts for nothing. A nil scope keeps everything: a version cut
// before builds were selections validates the whole oracle.
func (r Report) Within(s *Scope) Report {
	if s == nil {
		return r
	}
	out := Report{scope: s}
	for _, sc := range r.doc.Scenarios {
		if s.runs(sc) {
			out.doc.Scenarios = append(out.doc.Scenarios, sc)
		}
	}
	return out
}

// outcomes maps each scenario's key to its outcome.
func (r Report) outcomes() map[string]string {
	out := make(map[string]string, len(r.doc.Scenarios))
	for _, sc := range r.doc.Scenarios {
		out[sc.id()] = sc.Outcome
	}
	return out
}

// Digest fingerprints WHAT A REPORT CONCLUDED, so two validation attempts
// can be compared. Empty for an absent or unparseable report — there is nothing to
// compare, and two empty digests must not read as "the same answer twice".
//
// It covers the scenarios only: each one's id, outcome, and what was observed at
// the step that settled it, sorted by id. Explicitly NOT the file bytes — the
// report stamps `commit` and `generatedAt`, so a whole-file hash would change on
// every attempt and make the identical-answer check dead code that never fires.
//
// The observation is part of the answer: the same scenario failing for a different
// reason means the repair changed something, even if it is still red.
//
// Sorted because report order is the agent's file-discovery order, which is not a
// promise; two attempts that found the same outcomes in a different order reached
// the same answer.
func (r Report) Digest() string {
	if len(r.doc.Scenarios) == 0 {
		return ""
	}
	lines := make([]string, 0, len(r.doc.Scenarios))
	for _, s := range r.doc.Scenarios {
		observed := ""
		if i := s.deciding(); i >= 0 {
			observed = s.Steps[i].Observed
		}
		lines = append(lines, s.id()+"\x00"+s.Outcome+"\x00"+observed)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\x1e")))
	return hex.EncodeToString(sum[:])
}

// Failed returns the scenarios the report records as failed, in report
// order. Empty for an absent, unparseable or all-green report — every case where
// there is nothing to repair.
//
// `blocked` is deliberately NOT included. The agent cannot tell an app that
// correctly refuses an action from one too broken to perform it, so filing repair
// work on a block would have a coding run add an affordance the requirement never
// asked for. A block is reported and left for a person.
//
// It is deliberately separate from VerdictFromReport rather than folded into it.
// The verdict is a single value the run stores; this is a list the supervisor turns
// into issues, and the two are read by different callers at different moments (the
// second only when the first came back `failed`).
func (r Report) Failed() []FailedScenario {
	var out []FailedScenario
	for _, s := range r.doc.Scenarios {
		if s.Outcome != outcomeFailed {
			continue
		}
		f := FailedScenario{
			ID:          s.id(),
			FeatureID:   featureID(s.Feature, s.FeatureFile),
			Stories:     r.scope.stories(s),
			Feature:     s.Feature,
			Rule:        s.Rule,
			Scenario:    s.Scenario,
			FeatureFile: s.FeatureFile,
			Line:        s.Line,
			Deciding:    s.deciding(),
			Reproduced:  s.Reproduced,
			Note:        s.Note,
			Evidence: FailedEvidence{
				Console:      s.Evidence.Console,
				SnapshotFile: s.Evidence.SnapshotFile,
				Snapshot:     s.Evidence.Snapshot,
				NotCaptured:  s.Evidence.NotCaptured,
			},
		}
		for _, st := range s.Steps {
			f.Steps = append(f.Steps, FailedStep{
				Keyword: st.Keyword, Text: st.Text,
				Command: st.Command, Exit: st.Exit, Observed: st.Observed,
			})
		}
		// Allocated before the loop, so an EMPTY list survives the copy as empty
		// rather than arriving nil. The distinction is the whole point of the
		// field: `"network": []` says nothing left the page — a wiring defect —
		// and `append` into a nil slice over zero elements erases that into "the
		// run captured nothing", which renders as no evidence section at all. The
		// one case where "no request left the page" is the ONLY thing the issue
		// had to say was the one case it went unsaid.
		if s.Evidence.Network != nil {
			f.Evidence.Network = make([]NetworkRequest, 0, len(s.Evidence.Network))
			for _, r := range s.Evidence.Network {
				f.Evidence.Network = append(f.Evidence.Network,
					NetworkRequest{Method: r.Method, URL: r.URL, Status: r.Status})
			}
		}
		out = append(out, f)
	}
	return out
}

// Per-scenario outcomes the agent writes. Shared with the report checker, which
// holds the agent to them.
//
// `failed` and `blocked` are both defects and are not merged: one says the
// behaviour is wrong, the other says the behaviour was never reached.
// `unjudgeable` is for truth that lives outside the running app. The last two are
// neither passes nor failures — they are the agent declining to judge, which is
// the honest answer and always better than a guess.
const (
	outcomePassed      = "passed"
	outcomeFailed      = "failed"
	outcomeBlocked     = "blocked"
	outcomeUnjudgeable = "unjudgeable"
)

// Whether the outcome held when the scenario was driven a second time. Shared
// with the report checker, which holds the agent to the same three words.
//
// `unattempted` has no constant because nothing branches on it: it is every value
// that is neither of these two, including a report written before the field
// existed, and all of them render the same — nothing. The only reader of a repair
// issue cannot drive the scenario itself, so "we did not check" is a disclaimer it
// can do nothing with.
const (
	reproducedYes = "yes"
	reproducedNo  = "no"
)

// Verdict derives a run's validation verdict from the committed report,
// returning one of the delivery.ValidationVerdict* values. Applied in order:
//
//  1. no usable report (absent, unparseable, or carrying no scenarios) → unreported
//  2. any scenario failed                                              → failed
//  3. no scenario passed                                               → inconclusive
//  4. any scenario was never judged                                    → partial
//  5. otherwise every scenario passed                                  → passed
//
// Order carries the meaning. A real assertion failure wins outright (2), because
// it is the one thing the report says about the *software* rather than about the
// run. Rule 5 then requires FULL coverage for `passed`: a project must not read
// "passed" over scenarios nobody could drive — `partial` exists to say that
// honestly instead.
//
// Rules 1 and 3 look similar and are not. `inconclusive` means we read the
// evidence and it records that nothing ran; `unreported` means there was nothing
// to read. Only the second is fatal, because the read is pinned to the validation
// cycle's own merge commit — so an absent report is a fact about this run, not a
// propagation artifact.
func (r Report) Verdict() string {
	// No scenarios is not a vacuous pass: "nothing failed" over an empty set would
	// otherwise report success for a run that judged nothing — and a report
	// whose every scenario lies outside the version's scope judged nothing it
	// was asked.
	if len(r.doc.Scenarios) == 0 {
		return delivery.ValidationVerdictUnreported
	}

	passed, uncovered := false, false
	for _, s := range r.doc.Scenarios {
		switch s.Outcome {
		case outcomeFailed:
			return delivery.ValidationVerdictFailed
		case outcomePassed:
			passed = true
		case outcomeBlocked, outcomeUnjudgeable:
			uncovered = true
		default:
			// An unrecognised outcome is evidence we cannot interpret; treat it as a
			// gap rather than silently counting it towards full coverage.
			uncovered = true
		}
	}

	switch {
	case !passed:
		return delivery.ValidationVerdictInconclusive
	case uncovered:
		return delivery.ValidationVerdictPartial
	default:
		return delivery.ValidationVerdictPassed
	}
}
