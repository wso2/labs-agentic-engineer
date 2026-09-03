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

import "errors"

// Why a run was refused before it started. They live at the ROOT because the
// refusal is decided in the event plane and rendered by the run read surface,
// and those two sub-packages may not import each other (`slice ⊥ sibling`) —
// the same reason ErrTemporalUnavailable lives here.
//
// All three are written for a human: the console returns them verbatim.
var (
	// ErrRunAlreadyLive means a run is already working that milestone. Adoption
	// treats this as a no-op — the live run picks the issue up at its next
	// boundary — but a revalidation has nothing to hand off, since a live run's
	// verdict may be hours away or may never come.
	ErrRunAlreadyLive = errors.New("a run is already working this version — cancel it or wait for it to settle")
	// ErrMilestoneHasOpenWork means the version still has work in its working set.
	// The loop would dispatch a coding cycle for it before validating, which is a
	// build resumed rather than a version re-judged.
	ErrMilestoneHasOpenWork = errors.New("this version still has open work — the run would build it, not just re-check it")
	// ErrNoValidationCriteria means the version has no oracle to validate against.
	// Refused rather than run: a run with nothing to validate concludes `skipped`,
	// and because the newest run owns the version's verdict that would replace a
	// real answer with "not validated".
	ErrNoValidationCriteria = errors.New("this version has no validation criteria to validate against")
	// ErrRunNotStarted means the supervisor reported success but no run row exists
	// behind it — a degraded boot (no agent dispatcher, no workflow engine) or a
	// lost admission race. The paths that re-offer on a timer treat those as
	// nothing to do; a caller waiting on an answer has to be told.
	ErrRunNotStarted = errors.New("the run could not be started — the platform is not ready to work this version")
)

// StartRunRequest asks the run supervisor for a run over one milestone.
//
// It lives at the domain ROOT because two sub-packages ask for the same thing
// and may not import each other (`slice ⊥ sibling`): the plan path in `build`
// starts the spec run it just planned, and the event plane starts an incident
// run on adoption or from the reconcile sweep. Each declares its own narrow
// `RunStarter` port over THIS type, so one composition-root adapter satisfies
// both without either package naming the other.
type StartRunRequest struct {
	OrgID     string
	ProjectID string
	// MilestoneNumber is the platform key of the milestone to work. Titles are
	// renamable on GitHub; the number never changes.
	MilestoneNumber int
	// MilestoneTitle is the milestone's GitHub title at creation, carried for
	// display and for the runner's `gh issue list --milestone "<title>"`
	// discovery call. It is the milestone's name, not the version — the run row's
	// SpecTag answers that.
	MilestoneTitle string
	// Kind is what the run will DO: RunKindDev for the plan path, RunKindTask for
	// everything the event plane starts by detection, RunKindValidation for a
	// human asking a shipped version's criteria again. Every predicate the
	// supervisor and the loop apply reads this, including the build mutex.
	Kind string
	// Origin is where the request came from: RunOriginSpecBuild for the plan
	// path, RunOriginIncidentAdoption for the event plane, RunOriginRevalidate
	// for the human ask. It is recorded, never branched on.
	Origin string
	// RunID is the admitted run row this request supervises, when the caller
	// already admitted one (the plan path admits the row itself, so that the
	// build mutex is armed before the slow planning turn begins). Empty means
	// "admit one yourself" — the adoption and sweep paths, where admission and
	// supervision must happen together or a row exists that nobody drives.
	RunID string

	// Tag and ProvisionInputs ask the run to FILL its milestone before working
	// it: mint the version's dependency gates, then plan its Tasks.
	//
	// Only the build click sets them, and that is the whole contract. A run the
	// sweep or an adoption re-offers is past planning — re-offering must resume a
	// run, never re-derive a version — so those callers leave both empty and the
	// workflow skips its planning phase. They ride the REQUEST rather than the run
	// row for exactly that reason: the row cannot tell "start me" from "fill me".
	Tag             string
	ProvisionInputs []ProvisionInput

	// Rebuild says the version's milestone is ALREADY FILLED, so the run mints its
	// gates and skips the planning TURN.
	//
	// Only the build click sets it, and only when BOTH halves hold: the click
	// resolved to the same tag (the spec did not change) AND the milestone
	// actually holds planned work. The second half is not redundant, and the
	// clicks that taught us are the ones where a run died in its planning phase —
	// `plan-failed`, or a cancel that landed before the planning turn. The spec is
	// unchanged there too, but the milestone holds only its gates, and a run that
	// skips planning over it reads the empty working set as "planning produced
	// nothing to work" and settles the version SUCCEEDED having built none of it.
	// build.reopenIncrement answers the second half off the milestone's own
	// issues.
	//
	// Re-planning a genuinely filled milestone is the opposite failure and is why
	// the flag exists at all: plan dedupe is the title slug against the
	// milestone's issues in any state, so the turn would mint nothing and the run
	// would settle an unbuilt version as delivered just the same.
	//
	// It rides the request beside Tag for the same reason: the row cannot tell
	// "fill me" from "resume me", let alone "I refilled it for you".
	Rebuild bool

	// CycleCeiling and ValidationAttempts pin this run's budgets, overriding the
	// platform defaults. Zero on both means "use the default", which is what every
	// caller but the revalidate trigger passes — and what the sweep and adoption
	// paths must keep passing, since neither has a reason to narrow a run.
	//
	// They ride the REQUEST rather than being read from config at the workflow,
	// because the supervisor counts budgets deterministically: a value read
	// mid-run could differ on replay.
	CycleCeiling       int
	ValidationAttempts int
}
