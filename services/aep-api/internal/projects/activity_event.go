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

package projects

import "time"

// ActivityEvent is one row of the project activity feed (issue #239): an
// append-only, best-effort record of an SDLC milestone (type vocabulary:
// internal/contracts/activityvocab). The console renders the
// human sentence from these structured facts (type + actor + target), so no
// rendered message is stored, and no tone (the console maps type → tone). "You"
// is viewer-relative, so the actor is stored as a stable identity (email for a
// user) and the console decides "You" vs a name.
//
// DedupKey makes emission idempotent under Temporal activity retry and GitHub
// webhook redelivery: the (org_id, project_id, dedup_key) unique index turns a
// re-emit into a no-op (INSERT ... ON CONFLICT DO NOTHING). Both indexes are
// full (non-partial) composite indexes, so AutoMigrate creates them from these
// tags — no hand-written migration is needed.
type ActivityEvent struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	OrgID     string `gorm:"not null;uniqueIndex:ux_activity_events_dedup,priority:1;index:idx_activity_events_feed,priority:1" json:"-"`
	ProjectID string `gorm:"not null;uniqueIndex:ux_activity_events_dedup,priority:2;index:idx_activity_events_feed,priority:2" json:"-"`

	Type string `gorm:"not null" json:"type"` // spec_updated | spec_published | plan_derived | task_started | task_deployed | task_failed

	// Actor identity — reuses the chat ChatAuthor model (#130). For a user:
	// kind=user, id=email, name=display name (denormalized at emission — there
	// is no user directory and JWT identity is request-time only). For an agent:
	// kind=agent, id='build-agent'|'plan-agent', name='Build agent'|'Plan agent'.
	ActorKind string `gorm:"not null" json:"actorKind"`
	ActorID   string `gorm:"type:text" json:"actorId,omitempty"`
	ActorName string `gorm:"type:text;not null" json:"actorName"`

	// Target references — each type populates the subset it needs; the console
	// renders them into the line. Flat columns, not a JSON blob.
	Issue       int    `json:"issue,omitempty"`
	Title       string `gorm:"type:text" json:"title,omitempty"`
	Component   string `gorm:"type:text" json:"component,omitempty"`
	Environment string `gorm:"type:text" json:"environment,omitempty"`
	Tag         string `gorm:"type:text" json:"tag,omitempty"`
	Reason      string `gorm:"type:text" json:"reason,omitempty"`

	// DedupKey — deterministic per logical event (e.g. "exec:{id}:deployed").
	DedupKey string `gorm:"type:text;not null;uniqueIndex:ux_activity_events_dedup,priority:3" json:"-"`

	// OccurredAt is the real event time; the feed orders by it, newest first.
	OccurredAt time.Time `gorm:"not null;index:idx_activity_events_feed,priority:3,sort:desc" json:"occurredAt"`
	CreatedAt  time.Time `json:"-"`
}

// TableName pins the table name.
func (ActivityEvent) TableName() string { return "activity_events" }
