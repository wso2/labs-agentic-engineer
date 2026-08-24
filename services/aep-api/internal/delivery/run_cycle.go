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
	"time"

	"github.com/wso2/aep/aep-api/internal/contracts"
)

// RunCycle kinds (plain strings, matching the model convention). The kind names
// what the cycle was dispatched to do; recovery cycles are ordinary cycles, so
// a fix or conflict cycle is indistinguishable from normal work apart from this
// label and the budget it spends.
const (
	CycleKindCoding     = "coding"
	CycleKindConflict   = "conflict"
	CycleKindFix        = "fix"
	CycleKindValidation = "validation"
)

// Merge verdicts — why a cycle's pull request did not merge. There is
// deliberately no "merged" verdict: a merge is recorded by the merge SHA, so a
// second spelling of it could disagree with the first.
const (
	// CycleMergeDeclined is the auto-merge POLICY's no: the pull request claims
	// nothing that is agent work in this run's milestone, so it is not this
	// run's work and is left for a human.
	CycleMergeDeclined = "declined"
	// CycleMergeRefused is the HOST's no on a pull request that is still open,
	// which in this model means exactly one thing — it does not merge cleanly.
	// A conflict issue is minted and the next cycle rebases.
	CycleMergeRefused = "refused"
)

// IssueNumbers is a jsonb-serialized list of GitHub issue numbers. Named so the
// column's shape is declared once rather than spelled out at each field.
type IssueNumbers []int

// RunCycle is one dispatch within a MilestoneRun: the platform hands the
// coding agent a milestone reference, the agent opens one PR, and the PR
// squash-merges. A run is a sequence of these — one row per dispatch — and the
// run's loop position is always read from the LATEST row rather than from a
// stored phase enum, because fix and conflict cycles re-enter earlier phases.
//
// Attempts is the per-cycle re-dispatch counter (budget
// RunMaxRedispatchPerCycle): it lives here, not on the run row, precisely
// because the budget resets at every cycle boundary. It counts dispatches, so a
// freshly appended row starts at 0 and the first dispatch takes it to 1.
//
// Branch, the pull request (PRNumber and PRURL) and MergeSHA are LEARNED FROM
// WEBHOOKS, never from dispatch — the agent derives its own branch identity
// (crash resume reuses an unmerged `aep/m<milestone#>-*` branch), so the
// platform records what actually happened. They stay empty on a cycle whose
// agent died before opening a PR.
//
// OrgID and ProjectID are denormalized from the owning run so a tenant-fenced
// read needs no join; RunID is the real parent key.
type RunCycle struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID     string `gorm:"index;not null" json:"-"`
	ProjectID string `gorm:"index;not null" json:"projectId"`

	// RunID is the owning MilestoneRun.
	RunID string `gorm:"index;not null" json:"runId"`

	Kind string `gorm:"not null;index" json:"kind"` // coding | conflict | fix | validation

	// Attempts counts dispatches of THIS cycle (re-dispatch budget).
	Attempts int `gorm:"not null;default:0" json:"attempts"`

	// JobRef is the dispatched Kubernetes Job for the current attempt (empty
	// until the first dispatch, replaced on re-dispatch).
	JobRef string `gorm:"type:text" json:"jobRef,omitempty"`

	Branch   string `gorm:"type:text" json:"branch,omitempty"`
	PRNumber int    `gorm:"index" json:"prNumber,omitempty"`
	MergeSHA string `gorm:"type:text" json:"mergeSha,omitempty"`

	// PRURL is the pull request's page on the HOST, exactly as the webhook
	// reported it. It is stored rather than composed from the repo row and
	// PRNumber: composing would spread GitHub's URL grammar and the repo's
	// clone-URL spelling (`.git` suffixes and all) into every reader that wants a
	// link, and be wrong everywhere at once on the day either changes.
	PRURL string `gorm:"type:text" json:"prUrl,omitempty"`

	// PRDraft mirrors the pull request's draft flag. A draft is the agent saying
	// it is not finished, so the merge policy never runs on one — and a cycle
	// parked behind a draft is otherwise indistinguishable from one whose agent
	// never opened a pull request at all.
	PRDraft bool `gorm:"not null;default:false" json:"prDraft,omitempty"`

	// Resolves is the merge policy's MATCHED set: the milestone agent-work
	// issues this cycle's pull request claims, which is what the merge closes.
	//
	// It is recorded because it is the only durable answer to "what did this
	// cycle work". The issues themselves are closed by the merge, and the
	// boundary read the supervisor dispatches on returns COUNTS, so nothing else
	// in the system can attribute a closed issue to the cycle that closed it.
	Resolves IssueNumbers `gorm:"type:jsonb;serializer:json" json:"resolves,omitempty"`

	// ValidationVerdict is what THIS validation attempt concluded, derived from the
	// report at this cycle's own MergeSHA. Empty on every other kind, and until the
	// attempt settles.
	//
	// It lives here as well as on the run for the same reason the loop's position
	// is read from the latest cycle rather than a stored phase enum: the run's
	// single column can only hold the LATEST answer, and a validation run that
	// re-dispatched an agent which reported nothing has two cycles with two
	// answers. Without a per-cycle copy the first would exist only in Temporal
	// history and vanish with its retention, leaving a self-healed run
	// indistinguishable from one that reported first time.
	ValidationVerdict string `gorm:"type:text" json:"validationVerdict,omitempty"`
	// ValidationIssue is the validation issue this cycle was dispatched at. Same
	// reasoning: the issue is reused across attempts, but which attempt asked is a
	// per-cycle fact, and it keeps a settled run navigable to its criteria.
	ValidationIssue int `gorm:"not null;default:0" json:"validationIssue,omitempty"`
	// ValidationDigest fingerprints WHAT THIS ATTEMPT CONCLUDED — the criteria,
	// their outcomes and their failure messages, never the file bytes
	// (validation.ReportDigest). Empty on every other kind, and on an attempt
	// whose report was absent or unreadable.
	//
	// It is stored because the comparison it exists for SPANS RUNS: each attempt
	// is its own validation run, and a repeat that reached the same answer as the
	// last one proves the repair moved nothing, so the loop stops instead of
	// spending the remaining allowance on the same report. Workflow state cannot
	// carry that — the previous attempt's execution has ended — and Temporal
	// history is not queryable by milestone, so the ledger row is the only place
	// the next attempt can read it from.
	//
	// It is written by the SAME activity as ValidationVerdict, and must be: that
	// write is fenced write-once on an empty verdict, so a digest written
	// separately afterwards could never land.
	//
	// Off the wire (`json:"-"`): it is a loop control input, not something a
	// reader of the timeline can interpret.
	ValidationDigest string `gorm:"type:text" json:"-"`

	// MergeVerdict is why the pull request did NOT merge, when something decided
	// so: CycleMergeDeclined (the policy: not this run's work) or
	// CycleMergeRefused (the host: it does not merge cleanly). Empty on a cycle
	// whose merge was never decided against — including every cycle that merged,
	// since a merge is recorded by MergeSHA and each fresh decision overwrites
	// this field.
	MergeVerdict string `gorm:"type:text" json:"mergeVerdict,omitempty"`
	// MergeReason is the verdict's own words, for a reader. Never parsed.
	MergeReason string `gorm:"type:text" json:"mergeReason,omitempty"`

	// AgentReason is why the CYCLE'S AGENT stopped without landing a pull
	// request, as the pod-truth watcher classified it: `timed_out`,
	// `agent_failed[:<pod reason>]`, `startup_failed:<reason>: <message>` or
	// `job_not_found`. Empty on every cycle whose agent produced a pull request
	// — the ordinary path, where the outcome is the pull request's and this
	// column would only be a second, weaker opinion.
	AgentReason string `gorm:"type:text" json:"agentReason,omitempty"`

	// Token usage captured from the runner's terminal NDJSON result (#249/#291).
	// A cycle IS one agent run, so this is where delivery's agent spend lives:
	// after the issue-driven flip every token-burning dispatch is a cycle
	// (coding, fix, conflict, validation), and the only Execution rows left are
	// KindProvision ones, which stand up OpenChoreo resources and run no model.
	//
	// Tokens + model are the stored truth; CostUsd is the USD stamped at capture
	// from the model_rates then in force (amended ADR-0011) — never repriced. All
	// zero (CostUsd null) for cycles that predate capture, whose agent died
	// before its terminal message, or whose model had no rate row.
	//
	// Deliberately off the wire (`json:"-"`): #291 moved agent spend out of the
	// per-build and per-task surfaces and into Settings → Usage, and the run
	// spine does not re-litigate that.
	InputTokens         int64    `gorm:"not null;default:0" json:"-"`
	OutputTokens        int64    `gorm:"not null;default:0" json:"-"`
	CacheReadTokens     int64    `gorm:"not null;default:0" json:"-"`
	CacheCreationTokens int64    `gorm:"not null;default:0" json:"-"`
	ModelID             string   `gorm:"type:text;not null;default:''" json:"-"`
	CostUsd             *float64 `gorm:"column:cost_usd" json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// EndedAt stamps the cycle closed. A nil EndedAt is the "still open" guard
	// every mutator is fenced on.
	EndedAt *time.Time `json:"endedAt,omitempty"`
}

// TableName pins the table name so a struct rename cannot silently move the
// table.
func (RunCycle) TableName() string { return "run_cycles" }

// Usage returns the cycle's captured token usage. Mirrors Execution.Usage so
// both delivery capture surfaces hand the same shape to the rollup.
func (c RunCycle) Usage() contracts.TokenUsage {
	return contracts.TokenUsage{
		InputTokens:         c.InputTokens,
		OutputTokens:        c.OutputTokens,
		CacheReadTokens:     c.CacheReadTokens,
		CacheCreationTokens: c.CacheCreationTokens,
		Model:               c.ModelID,
	}
}

// The two delivery-owned SDLC phases in the Settings → Usage split (#291); the
// third (spec/design) is the spec domain's, keyed off agent turns.
//
// The mapping from cycle kind to phase is: a VALIDATION cycle is the validation
// phase, and every other kind — coding, fix, conflict, all of them agent work
// driving the build toward green — is the build phase. That classification is
// applied in SQL, by the CASE in RunCycleRepository.SumUsageByProjectPhase, so
// the aggregate is one query rather than a per-row round trip; it deliberately
// has no Go twin that could drift from it.
const (
	UsagePhaseBuild      = "build"
	UsagePhaseValidation = "validation"
)

// CyclePullRequest is the pull request identity a cycle learns from one
// pull_request delivery: what the agent opened, as the HOST describes it.
//
// The four facts travel together because they are one observation. They are also
// learned twice — the `opened` delivery and, when that one was missed, the
// `closed` backfill — and a partial write from either would leave the cycle
// describing a pull request that does not exist.
type CyclePullRequest struct {
	Branch string
	Number int
	// URL is the host's own page for the pull request (GitHub's `html_url`).
	URL string
	// Draft is the pull request's draft flag at the moment of the delivery. It is
	// part of the identity, not a separate fact: a pull request marked ready is
	// the SAME pull request.
	Draft bool
}
