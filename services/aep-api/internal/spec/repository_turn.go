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

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

// The agent_turns store: the finished-turn ledger. Every turn an
// org's AE Studio pod runs lands here once, whole and already finished,
// through record-turn-usage. The AgentTurn gorm lives here, in the spec
// domain's own repository, as single write-authority.

// Turn statuses (AgentTurn.Status). A ledger row is always finished.
const (
	turnStatusCompleted = "completed"
	turnStatusFailed    = "failed"
)

// TurnStatusFailed is the status a CONSUMER outside this package branches on:
// the status poll folds the newest turn row into spec.agent (#562). Exported
// as an alias of the internal constant so the string stays authored once.
const TurnStatusFailed = turnStatusFailed

// AE Studio turn kinds (AgentTurn.Kind, TurnRecord.Kind): what started the
// turn — a user in the browser, the project kickoff, or a task plan.
const (
	TurnKindBrowser = "browser"
	TurnKindKickoff = "kickoff"
	TurnKindPlan    = "plan"
)

// TurnRecord is one finished turn as an org's AE Studio pod reports it
// (record-turn-usage, the contract's AEStudioTurnRecord), already resolved to
// the org's tenancy by the caller.
type TurnRecord struct {
	TurnID string
	// Project is "" for a marketplace turn, which belongs to no project (C7).
	Project        string
	ConversationID string
	Kind           string // TurnKindBrowser | TurnKindKickoff | TurnKindPlan
	Flow           string
	Status         string // completed | failed
	Reason         string
	Code           string
	BaseRef        string
	SkillsRef      string
	StartedAt      time.Time
	FinishedAt     time.Time
	// AuthorID and AuthorName credit the user who sent the turn; both "" for
	// a turn nobody sent (a kickoff with no credit, a marketplace turn).
	AuthorID   string
	AuthorName string
	// ModelHost and Usage.Model key the rate the turn is priced at.
	ModelHost     string
	Usage         contracts.TokenUsage
	ContextTokens *int64
}

// TurnRepository is the finished-turn ledger. Lookups miss with (nil, nil),
// matching the house convention.
type TurnRepository interface {
	// RecordFinished stores org's finished turns, each exactly once: a record
	// whose turn id org already stored is skipped, never rewritten, so a
	// resent batch changes nothing. The ledger's identity is (org, turn id),
	// so another org's row with the same id never stands in for this org's
	// record. cost_usd is stamped here from the rates in force. The
	// caller has checked each record's project belongs to org.
	RecordFinished(ctx context.Context, org string, recs []TurnRecord) error

	// NewestCompletedFlow returns the project's most recent COMPLETED turn of
	// one flow ("design", "start", …), or (nil, nil) when it has run none.
	//
	// The status read's staleness check (#575) asks for the newest successful
	// design run so it can read the requirements as that run saw them. Scoped
	// to completed because a failed turn never reconciled anything: treating
	// one as the baseline would clear a staleness warning on the strength of
	// work that did not land.
	NewestCompletedFlow(ctx context.Context, orgID, projectID, flow string) (*AgentTurn, error)

	// CompletedFlows returns up to `limit` of the project's COMPLETED turns of
	// one flow, newest first. The build gate reads the design runs this way to
	// find, per feature, the run that last designed it (E1).
	CompletedFlows(ctx context.Context, orgID, projectID, flow string, limit int) ([]AgentTurn, error)

	// Newest returns the project's most recent turn row across every
	// conversation, or (nil, nil) when nothing has ever run.
	//
	// Two callers, both needing "has this project ever had an agent work on
	// it, and how did the last attempt end" (#562): the kickoff's idempotence
	// guard, and the status poll's spec.agent field.
	Newest(ctx context.Context, orgID, projectID string) (*AgentTurn, error)

	// SumUsageByProject rolls up captured spec/design turn usage per project
	// across an org (#291), keyed by project id — one half of the Settings →
	// Usage read (delivery supplies the coding-execution half). CostUsd sums
	// the frozen per-row stamps; nil when no row in a project is stamped.
	SumUsageByProject(ctx context.Context, orgID string) (map[string]contracts.StampedUsage, error)
}

type turnRepository struct {
	db      *gorm.DB
	stamper *modelcost.Stamper
}

// NewTurnRepository builds the agent_turns store. stamper prices captured turn
// usage at write time (#291); nil disables stamping (tests, or a boot with no
// rates) and cost_usd stays null.
func NewTurnRepository(db *gorm.DB, stamper *modelcost.Stamper) TurnRepository {
	return &turnRepository{db: db, stamper: stamper}
}

func (r *turnRepository) RecordFinished(ctx context.Context, org string, recs []TurnRecord) error {
	if len(recs) == 0 {
		return nil
	}
	rows := make([]AgentTurn, 0, len(recs))
	for _, rec := range recs {
		rows = append(rows, r.ledgerRow(org, rec))
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "org_id"}, {Name: "id"}}, DoNothing: true}).
		Create(&rows).Error
}

// ledgerRow is rec as org's agent_turns row, its cost stamped at the rate in
// force now and frozen there (#291): null when unpriceable (no stamper, no
// (host, model) rate). created_at is the turn's start, as it was for a row the
// in-process engine admitted: Newest and NewestCompletedFlow order by it, so
// a batch's order or a late delivery never makes an older turn the newest.
// It is clamped to now: the start is the pod's clock, and a pod running ahead
// would otherwise pin its row as Newest (the kickoff guard, spec.agent) until
// real time caught up. started_at keeps the pod's own value.
func (r *turnRepository) ledgerRow(org string, rec TurnRecord) AgentTurn {
	finished := rec.FinishedAt
	created := rec.StartedAt
	if now := time.Now().UTC(); created.After(now) {
		created = now
	}
	row := AgentTurn{
		ID:                  rec.TurnID,
		OrgID:               org,
		ProjectID:           rec.Project,
		ConversationID:      rec.ConversationID,
		Kind:                rec.Kind,
		Flow:                rec.Flow,
		BaseRef:             rec.BaseRef,
		SkillsRef:           rec.SkillsRef,
		Status:              rec.Status,
		Reason:              rec.Reason,
		Code:                rec.Code,
		AuthorID:            rec.AuthorID,
		AuthorDisplayName:   rec.AuthorName,
		InputTokens:         rec.Usage.InputTokens,
		OutputTokens:        rec.Usage.OutputTokens,
		CacheReadTokens:     rec.Usage.CacheReadTokens,
		CacheCreationTokens: rec.Usage.CacheCreationTokens,
		ModelID:             rec.Usage.Model,
		ModelHost:           rec.ModelHost,
		ContextTokens:       rec.ContextTokens,
		StartedAt:           rec.StartedAt,
		FinishedAt:          &finished,
		CreatedAt:           created,
	}
	if r.stamper != nil {
		row.CostUsd = r.stamper.Cost(modelcost.Tokens{
			Host:                rec.ModelHost,
			ModelID:             rec.Usage.Model,
			InputTokens:         rec.Usage.InputTokens,
			OutputTokens:        rec.Usage.OutputTokens,
			CacheReadTokens:     rec.Usage.CacheReadTokens,
			CacheCreationTokens: rec.Usage.CacheCreationTokens,
		})
	}
	return row
}

// Newest reads one row off `ix_agent_turns_project_newest`
// (org_id, project_id, created_at DESC — migrate/agent_turns.go), whose column
// order IS this query's, so it is a single index read rather than a sort of
// every turn the project has ever run. That matters: the status poll runs this
// every 5s per viewer while an agent works.
func (r *turnRepository) Newest(ctx context.Context, orgID, projectID string) (*AgentTurn, error) {
	var t AgentTurn
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Order("created_at DESC").
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// NewestCompletedFlow reads one row off the (org_id, project_id) index,
// narrowed by the indexed `flow` column.
func (r *turnRepository) NewestCompletedFlow(ctx context.Context, orgID, projectID, flow string) (*AgentTurn, error) {
	var t AgentTurn
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND flow = ? AND status = ?",
			orgID, projectID, flow, turnStatusCompleted).
		Order("created_at DESC").
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// CompletedFlows reads the newest completed runs of one flow off the
// (org_id, project_id) index.
func (r *turnRepository) CompletedFlows(ctx context.Context, orgID, projectID, flow string, limit int) ([]AgentTurn, error) {
	var turns []AgentTurn
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND flow = ? AND status = ?",
			orgID, projectID, flow, turnStatusCompleted).
		Order("created_at DESC").
		Limit(limit).
		Find(&turns).Error
	return turns, err
}

func (r *turnRepository) SumUsageByProject(ctx context.Context, orgID string) (map[string]contracts.StampedUsage, error) {
	var rows []usageByProjectRow
	err := r.db.WithContext(ctx).
		Model(&AgentTurn{}).
		Select("project_id, "+
			"COALESCE(SUM(input_tokens),0) AS input_tokens, "+
			"COALESCE(SUM(output_tokens),0) AS output_tokens, "+
			"COALESCE(SUM(cache_read_tokens),0) AS cache_read_tokens, "+
			"COALESCE(SUM(cache_creation_tokens),0) AS cache_creation_tokens, "+
			"SUM(cost_usd) AS cost_usd, "+ // NULL when no row is stamped — exactly the #291 semantic
			// Count distinct model_id INCLUDING '' so any unknown-model row
			// makes the project's model degrade to '' (matching
			// contracts.TokenUsage.Add: a mix of known + unknown is '').
			"COUNT(DISTINCT model_id) AS models, "+
			"COALESCE(MAX(model_id), '') AS max_model, "+
			// The host survives only while every row that spent tokens agrees
			// on it (a NULL, pre-stamping host counts as '' and so disagrees),
			// matching contracts.StampedUsage.Add, where a zero-token
			// contributor has no say.
			"CASE WHEN COUNT(DISTINCT COALESCE(model_host, '')) FILTER (WHERE "+turnSpentTokens+") = 1 "+
			"THEN MIN(COALESCE(model_host, '')) FILTER (WHERE "+turnSpentTokens+") ELSE '' END AS host").
		Where("org_id = ?", orgID).
		Group("project_id").
		// Only projects with real token traffic — a failed turn that captured
		// nothing leaves a 0-token row that should not surface an empty card.
		Having("SUM(input_tokens) + SUM(output_tokens) + SUM(cache_read_tokens) + SUM(cache_creation_tokens) > 0").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return usageRowsToMap(rows), nil
}

// turnSpentTokens is the SQL predicate for a turn row that captured usage.
const turnSpentTokens = "input_tokens + output_tokens + cache_read_tokens + cache_creation_tokens > 0"

// usageByProjectRow is the per-project aggregate scan shape shared by the
// turn and execution roll-ups (#291).
type usageByProjectRow struct {
	ProjectID           string
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUsd             *float64
	Models              int64
	MaxModel            string
	Host                string
}

// usageRowsToMap folds the per-project scan rows into StampedUsage keyed by
// project id: model survives only when a project ran a single model.
func usageRowsToMap(rows []usageByProjectRow) map[string]contracts.StampedUsage {
	out := make(map[string]contracts.StampedUsage, len(rows))
	for _, row := range rows {
		u := contracts.TokenUsage{
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
		}
		if row.Models == 1 {
			u.Model = row.MaxModel
		}
		out[row.ProjectID] = contracts.StampedUsage{Tokens: u, CostUsd: row.CostUsd, Host: row.Host}
	}
	return out
}
