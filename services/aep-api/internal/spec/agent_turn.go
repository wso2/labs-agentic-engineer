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

// AgentTurn is one committed-truth generation turn (design D16/D17): the
// durable record behind
// the 202-then-attach turn API. The row is the one-active-turn-per-project
// guard (D18) — a partial unique index ux_agent_turns_active on
// (org_id, project_id) WHERE status = 'running' (created by the agent_turns
// migration; AutoMigrate cannot express a partial index) — and the
// crash-safety anchor: the running replica heartbeats the row, and a stale
// heartbeat lets the sweep fail it and release the guard.
//
// The in-memory part buffer is deliberately NOT durable (D17) — only the
// terminal outcome lands here.
type AgentTurn struct {
	ID        string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrgID     string `gorm:"index;not null" json:"-"`
	ProjectID string `gorm:"index;not null" json:"projectId"`

	// ConversationID is the FE-chosen conversation uuid (NOT the namespaced
	// agents-service id — that is reconstructed from org/project/useCase when
	// dispatching). It keys the D20 filesChangedExternally / divergence-note
	// derivation: "the last terminal turn of this conversation".
	ConversationID string `gorm:"index;not null" json:"conversationId"`
	UseCase        string `gorm:"not null" json:"useCase"`

	// Flow is the `/<skill>` token this turn ran ("design", "start", …); "" for
	// plain chat. Recorded (#575) because the status read has to find "the
	// newest successful DESIGN run" to answer whether the requirements have
	// moved since — and a turn is otherwise indistinguishable from any other
	// once it has finished. NOT a conversation-identity dimension (see
	// useCaseGeneral): every turn of a project shares one thread whatever its
	// flow, and this column only ever narrows a lookup.
	Flow string `gorm:"type:text;index" json:"-"`

	// BaseRef is the main-tip commit SHA the turn ran against (its snapshot
	// ref); SkillsRef is the _skills head the skill catalog was read at.
	BaseRef   string `gorm:"not null" json:"baseRef"`
	SkillsRef string `gorm:"type:text" json:"skillsRef,omitempty"`

	// Status is running | completed | failed (canonical values live in
	// feature/genai; stored as plain strings per the model convention).
	Status string `gorm:"not null;index" json:"status"`

	// CommitSHA is the landed commit for a completed turn ("" for a
	// no-changes completion). Reason is the failure class for a failed turn:
	// stream-died | fold-parity | base-moved | dispatch-failed | internal.
	// Paths is a JSON array of the conflicting paths for base-moved.
	// Message carries a human-readable failure detail.
	CommitSHA string `gorm:"type:text" json:"commitSha,omitempty"`
	Reason    string `gorm:"type:text" json:"reason,omitempty"`
	Paths     string `gorm:"type:text" json:"-"`
	NoChanges bool   `json:"noChanges,omitempty"`
	Message   string `gorm:"type:text" json:"message,omitempty"`

	// SpecTag is the D19 lineage stamp for design turns: the latest approved
	// requirements tag (vN) at gate time. BaseRef covers the baseSha half.
	SpecTag string `gorm:"type:text" json:"specTag,omitempty"`

	// The turn's DISPLAY record (#562): the transcript line for the message
	// that started it, and who sent it. The same facts the journal carries to
	// the agents service — held here as well because that store persists a
	// turn's transcript only when the turn ENDS, so between dispatch and
	// landing there is nowhere else to read them from. That window is the
	// whole of a kickoff, and the browser that lands on a freshly created
	// project never sent the turn, so it has no local copy either: without
	// these it renders the agent narrating under a blank space.
	//
	// AuthorID is EMAIL-anchored to match the console's live author identity —
	// that equality is what lets a rendered row read as "you" rather than a
	// teammate. Both empty for an unattributable turn (an M2M token) and for
	// every row written before this existed.
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

	// HeartbeatAt is bumped by the running replica (~15s); the sweep fails
	// rows whose heartbeat went stale (~60s) and releases the D18 guard.
	HeartbeatAt time.Time `gorm:"index" json:"-"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName pins the table name so a struct rename cannot silently move the
// table (and the partial-index migration keeps targeting it).
func (AgentTurn) TableName() string { return "agent_turns" }
