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
	"fmt"
	"time"
)

// The agent-usage LEDGER: what an org's agent work cost, kept for good.
//
// It exists because spend outlived the rows that used to carry it. Delivery's
// dispatch records — run_cycles and executions — are working state, and the
// project-delete cascade purges them so a recreated same-named project starts
// with a clean timeline. Usage was stamped on those same rows, so deleting a
// project also erased the record of what it had cost, which the Settings → Usage
// page is built to keep showing (its cards carry a `deleted` flag for exactly
// this case).
//
// The ledger is therefore APPEND-ONLY and deliberately outside every purge. It
// is written at capture, beside the stamp on the dispatch row, so the record is
// complete whatever happens to the project afterwards — and in particular it does
// not depend on the delete path running, which is best-effort by contract.

// UsageLedgerSource names which dispatch history an entry came from. Both write
// here, so the rollup has ONE source of truth and cannot double-count.
const (
	// UsageLedgerSourceRunCycle is the milestone loop's cycle — where agent spend
	// lives after the issue-driven flip.
	UsageLedgerSourceRunCycle = "run_cycle"
	// UsageLedgerSourceExecution is the older per-issue funnel's row. The only
	// kind still minted runs no model, so in practice it contributes nothing
	// today; it is carried anyway, because a populated table the rollup ignored
	// would under-report spend rather than fail visibly.
	UsageLedgerSourceExecution = "execution"
)

// AgentUsageLedgerEntry is one dispatch's captured spend, frozen.
//
// Identity is (Source, SourceID) — the cycle or execution the capture came from.
// That is what makes re-capture idempotent: the runner's terminal log is read more
// than once, re-deriving the same figures, so a repeat write updates the entry in
// place instead of adding a second one.
//
// Everything else is a SNAPSHOT taken at capture, not a join. The dispatch row,
// its run, its milestone and eventually its project may all be gone by the time
// anybody reads this; an entry that had to join back to them would answer nothing
// for exactly the projects this table exists to remember.
type AgentUsageLedgerEntry struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`

	OrgID     string `gorm:"index;not null"`
	ProjectID string `gorm:"index;not null"`

	// Source + SourceID are the idempotency key (unique together).
	Source   string `gorm:"not null"`
	SourceID string `gorm:"not null"`

	// Phase is the SDLC phase this spend belongs to, decided at WRITE time by
	// UsagePhaseCaseSQL. It used to be a CASE in the rollup query, once per source
	// and drifting between them; storing it means the classification is made once,
	// from one spelling, at the moment the facts are in hand.
	Phase string `gorm:"not null"`

	// MilestoneNumber and Tag place the spend in the project's delivery history —
	// which version cost this. Both are zero/empty for an execution-sourced entry,
	// which belongs to no milestone.
	MilestoneNumber int    `gorm:"not null;default:0"`
	Tag             string `gorm:"not null;default:''"`

	ModelID             string   `gorm:"type:text;not null;default:''"`
	InputTokens         int64    `gorm:"not null;default:0"`
	OutputTokens        int64    `gorm:"not null;default:0"`
	CacheReadTokens     int64    `gorm:"not null;default:0"`
	CacheCreationTokens int64    `gorm:"not null;default:0"`
	CostUsd             *float64 `gorm:"column:cost_usd"`
	// ModelHost is copied from the source row with the rest of the capture, so
	// the ledger names the host its CostUsd was priced on.
	// Nullable on purpose (see RunPhase18ModelHost).
	ModelHost string `gorm:"type:text"`

	CapturedAt time.Time `gorm:"not null;default:now()"`

	// RetiredAt is the LIFETIME BOUNDARY, and the only column ever rewritten
	// after the entry is captured.
	//
	// It is stamped when the project this entry belongs to is deleted, which is
	// what keeps two lifetimes of the same project NAME apart: a slug is only a
	// slug, and a project deleted and recreated under the same name would
	// otherwise inherit its predecessor's bill. Live spend is the entries with a
	// NULL RetiredAt; each delete closes off exactly the entries that were live at
	// the time, so a slug deleted twice leaves two distinguishable generations
	// (one per stamp) rather than one merged heap.
	//
	// Nil is not "unknown" — it is a positive claim that this spend belongs to the
	// incarnation that is live now.
	RetiredAt *time.Time
}

// TableName pins the table name so a struct rename cannot silently move the
// table.
func (AgentUsageLedgerEntry) TableName() string { return "agent_usage_ledger" }

// UsagePhaseCaseSQL classifies a dispatch into the SDLC phase its spend is
// reported under, as the SQL fragment every ledger writer stamps with: a
// VALIDATION cycle is the validation phase, and every other kind — coding, fix,
// conflict, and the older funnel's build/provision rows, all of them agent work
// driving the increment toward green — is the build phase.
//
// It is a fragment rather than a Go predicate because the write is an INSERT …
// SELECT: the classification is applied to whatever rows the statement selects,
// so a Go twin could only be used by reading each row back first.
//
// This is the ONE spelling. It used to be written once per rollup query, and the
// two copies had already drifted — the executions side tested for a kind that
// table never had. Every caller now stamps through here, and the STORED phase is
// what the rollup groups on, so a future change is one edit and cannot half-land.
//
// kindColumn is a caller-supplied column reference (`c.kind`), never user input;
// the values interpolated beside it are this package's own constants.
func UsagePhaseCaseSQL(kindColumn string) string {
	return fmt.Sprintf("CASE WHEN %s = '%s' THEN '%s' ELSE '%s' END",
		kindColumn, CycleKindValidation, UsagePhaseValidation, UsagePhaseBuild)
}
