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
	// ErrNoAcceptanceCriteria means the version has no oracle to validate against.
	// Refused rather than run: a run with nothing to validate concludes `skipped`,
	// and because the newest run owns the version's verdict that would replace a
	// real answer with "not validated".
	ErrNoAcceptanceCriteria = errors.New("this version has no acceptance criteria to validate against")
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
	// Origin is RunOriginSpecBuild for the plan path and
	// RunOriginIncidentAdoption for everything the event plane starts.
	Origin string
	// RunID is the admitted run row this request supervises, when the caller
	// already admitted one (the plan path admits the row itself, so that the
	// spec-run mutex is armed before the slow planning turn begins). Empty means
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
