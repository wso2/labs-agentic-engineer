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

package spec

import "time"

// AgentTurn is one row of the finished-turn ledger: every turn an
// org's AE Studio pod ran lands here once, whole and already finished,
// through TurnRepository.RecordFinished (record-turn-usage). Nothing runs
// here and no row is ever updated after it is written.
//
// The identity is (org_id, id): the pod chooses the turn id (the kickoff's is
// deterministic, uuidv5 of org/project), so another org's row with the same
// id must never stand in for this org's (migrate's phase24 moved the primary
// key off id alone). OrgID leads so the key serves org-scoped reads.
//
// Older rows came from aep-api's in-process turn engine;
// phase24 gave them a kind and a start time and dropped the columns only that
// engine wrote.
type AgentTurn struct {
	OrgID     string `gorm:"primaryKey;index;not null" json:"-"`
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ProjectID string `gorm:"index;not null" json:"projectId"`

	// ConversationID is the conversation the turn ran in, as the pod reported
	// it.
	ConversationID string `gorm:"index;not null" json:"conversationId"`

	// Kind is what started the turn in AE Studio: browser | kickoff | plan
	// (TurnKind*). The ledger's rows carry it from record-turn-usage; phase24
	// derived it for the rows the in-process engine wrote.
	Kind string `gorm:"type:text;not null;default:'browser'" json:"-"`

	// Flow is the `/<skill>` token this turn ran ("design", "start", …); "" for
	// plain chat. Recorded (#575) because the status read has to find "the
	// newest successful DESIGN run" to answer whether the requirements have
	// moved since — and a turn is otherwise indistinguishable from any other
	// once it has finished. This column only ever narrows a lookup.
	Flow string `gorm:"type:text;index" json:"-"`

	// BaseRef is the main-tip commit SHA the turn ran against (its snapshot
	// ref); SkillsRef is the _skills head the skill catalog was read at.
	BaseRef   string `gorm:"not null" json:"baseRef"`
	SkillsRef string `gorm:"type:text" json:"skillsRef,omitempty"`

	// Status is completed | failed, as the pod reported it.
	Status string `gorm:"not null;index" json:"status"`

	// Reason is the failure class of a failed turn and Code names it when the
	// agent could (provider_limit, output_truncated), as the pod reported
	// them. Message and ResetAt hold what the in-process engine wrote on its
	// own failed turns; ledger rows leave them empty.
	Reason  string     `gorm:"type:text" json:"reason,omitempty"`
	Message string     `gorm:"type:text" json:"message,omitempty"`
	Code    string     `gorm:"type:text" json:"-"`
	ResetAt *time.Time `json:"-"`

	// Who sent the turn: the pod's credit (the verified caller's subject and
	// display name). Summary is the transcript line the in-process engine
	// stored for its turns; ledger rows leave it empty. All empty for a turn
	// nobody sent (a kickoff with no credit, a marketplace turn).
	Summary           string `gorm:"type:text" json:"-"`
	AuthorID          string `gorm:"type:text" json:"-"`
	AuthorDisplayName string `gorm:"type:text" json:"-"`

	// Token usage from the turn's terminal manifest (#249/#291). Tokens + model
	// are the stored truth; CostUsd is the USD stamped at capture from the
	// model_rates then in force (amended ADR-0011) — never repriced. All zero
	// (CostUsd null) for turns that predate capture or failed before the
	// manifest, or whose model had no rate row.
	InputTokens         int64    `gorm:"not null;default:0" json:"-"`
	OutputTokens        int64    `gorm:"not null;default:0" json:"-"`
	CacheReadTokens     int64    `gorm:"not null;default:0" json:"-"`
	CacheCreationTokens int64    `gorm:"not null;default:0" json:"-"`
	ModelID             string   `gorm:"type:text;not null;default:''" json:"-"`
	CostUsd             *float64 `gorm:"column:cost_usd" json:"-"`
	// ModelHost is the host of the model connection the turn ran on, the host
	// its usage is priced against: rates are keyed by (host, model).
	// Nullable on purpose: a row that predates the column reads NULL until
	// migrate's phase18 backfills it to api.anthropic.com, and NULL-only is what
	// keeps that backfill one-shot (see RunPhase18ModelHost).
	ModelHost string `gorm:"type:text" json:"-"`

	// ContextTokens is how much context the conversation held when the turn
	// ended (the last model step's prompt plus its output), as the pod
	// reported it. Nullable: NULL when the turn left no measure.
	ContextTokens *int64 `json:"-"`

	// StartedAt and FinishedAt are when the turn ran, as the AE Studio pod
	// that ran it reported (record-turn-usage). For rows the in-process
	// engine wrote, phase24 set StartedAt from created_at; FinishedAt is nil.
	StartedAt  time.Time  `json:"-"`
	FinishedAt *time.Time `json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName pins the table name so a struct rename cannot silently move the
// table (and the index migration keeps targeting it).
func (AgentTurn) TableName() string { return "agent_turns" }
