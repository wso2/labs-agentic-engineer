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
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wso2/aep/aep-api/internal/contracts"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
)

// The agent_turns store was extracted out of the genai turn engine (now internal/spec) during the
// spec-domain fold (P4): the ORM stays fenced to repositories/ while the turn
// VOCABULARY (the TurnTerminal shape, ErrTurnActive, the status/reason strings)
// re-exports back into the domain via type aliases, so the turn engine reads as
// one package. The AgentTurn gorm lives here, in the spec domain's own
// repository, as single write-authority.

// Turn statuses (AgentTurn.Status).
const (
	turnStatusRunning   = "running"
	turnStatusCompleted = "completed"
	turnStatusFailed    = "failed"
)

// Failure reasons (AgentTurn.Reason and the terminal event's `reason`).
const (
	turnReasonStreamDied     = "stream-died"
	turnReasonFoldParity     = "fold-parity"
	turnReasonBaseMoved      = "base-moved"
	turnReasonDispatchFailed = "dispatch-failed"
	turnReasonInternal       = "internal"
)

// ErrTurnActive is returned by TryStart when another turn holds the D18
// one-active-turn-per-project guard; the accompanying row is the active turn.
var ErrTurnActive = errors.New("a turn is already running for this project")

// TurnTerminal is the terminal state Finish stamps onto a running row.
type TurnTerminal struct {
	Status    string // turnStatusCompleted | turnStatusFailed
	CommitSHA string
	Reason    string
	Paths     []string
	NoChanges bool
	Message   string
	// Usage is the turn's token usage off the terminal manifest (#249); nil
	// when the stream carried none (failed turns, pre-capture agents).
	Usage *contracts.TokenUsage
	// SpecEdited is true when the turn authored real spec changes: a committed
	// turn whose fold produced a net change, or a room-scoped turn whose agent
	// edited the collab doc (issue #239 — the activity feed's agent-authorship
	// signal). It is independent of NoChanges: a room turn is always NoChanges
	// (git is untouched until the committer flushes the doc) yet still SpecEdited.
	SpecEdited bool
	// EditedPaths lists the collab-doc paths a room turn's manifest touched —
	// the paths whose later committer flush must be attributed to the agent,
	// not the flushing user (issue #239). In-process only (never persisted);
	// empty for a committed turn.
	EditedPaths []string
}

// TurnRepository is the agent_turns row store (design D17/D18): the durable
// turn record, the one-active guard, and the stale-heartbeat sweep. Lookups
// miss with (nil, nil), matching the house convention.
type TurnRepository interface {
	// TryStart INSERTs the running row; on conflict with the D18 partial
	// unique index it fetches and returns the active row alongside
	// ErrTurnActive. On success the passed row (ID populated) is returned.
	TryStart(ctx context.Context, t *AgentTurn) (*AgentTurn, error)

	// Heartbeat bumps heartbeat_at on a still-running row (no-op otherwise).
	Heartbeat(ctx context.Context, id string) error

	// Finish transitions a running row to its terminal state. Guarded on
	// status='running' so it never overwrites a swept/terminal row; returns
	// false when the row was not running anymore.
	Finish(ctx context.Context, id string, terminal TurnTerminal) (bool, error)

	// Get returns the turn only when it belongs to (orgID, projectID) — the
	// tenant fence for the status/stream endpoints. (nil, nil) on miss.
	Get(ctx context.Context, orgID, projectID, turnID string) (*AgentTurn, error)

	// GetActive returns the project's running turn, or (nil, nil).
	GetActive(ctx context.Context, orgID, projectID string) (*AgentTurn, error)

	// HasAny reports whether ANY turn row — running or terminal — exists for
	// the project: the kickoff's "a user already owns the interview" check
	// (#485). Firing an auto-/start after any first turn is the #432 bug
	// class server-side, so the kickoff stands down instead.
	HasAny(ctx context.Context, orgID, projectID string) (bool, error)

	// LastTerminal returns the most recent completed/failed turn of a
	// conversation — the D20 filesChangedExternally / divergence-note input.
	LastTerminal(ctx context.Context, orgID, projectID, conversationID string) (*AgentTurn, error)

	// SweepStale fails every running row whose heartbeat predates olderThan
	// (reason stream-died, message "replica crashed or hung") and returns the
	// swept rows so the caller can emit broker terminals.
	SweepStale(ctx context.Context, olderThan time.Time) ([]AgentTurn, error)

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

func (r *turnRepository) TryStart(ctx context.Context, t *AgentTurn) (*AgentTurn, error) {
	if t.Status == "" {
		t.Status = turnStatusRunning
	}
	if t.HeartbeatAt.IsZero() {
		t.HeartbeatAt = time.Now().UTC()
	}
	// Two attempts: a conflict whose active row finished between our INSERT
	// and the GetActive read retries the insert once instead of failing.
	for attempt := 0; attempt < 2; attempt++ {
		res := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(t)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected > 0 {
			return t, nil
		}
		active, err := r.GetActive(ctx, t.OrgID, t.ProjectID)
		if err != nil {
			return nil, err
		}
		if active != nil {
			return active, ErrTurnActive
		}
	}
	return nil, errors.New("genai: turn start raced the guard twice — give up")
}

func (r *turnRepository) Heartbeat(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).
		Model(&AgentTurn{}).
		Where("id = ? AND status = ?", id, turnStatusRunning).
		Update("heartbeat_at", time.Now().UTC()).Error
}

func (r *turnRepository) Finish(ctx context.Context, id string, terminal TurnTerminal) (bool, error) {
	updates := map[string]any{
		"status":     terminal.Status,
		"commit_sha": terminal.CommitSHA,
		"reason":     terminal.Reason,
		"paths":      encodePaths(terminal.Paths),
		"no_changes": terminal.NoChanges,
		"message":    terminal.Message,
	}
	if u := terminal.Usage; u != nil {
		updates["input_tokens"] = u.InputTokens
		updates["output_tokens"] = u.OutputTokens
		updates["cache_read_tokens"] = u.CacheReadTokens
		updates["cache_creation_tokens"] = u.CacheCreationTokens
		updates["model_id"] = u.Model
		// Stamp USD at capture from the rates in force now (#291): the cost is
		// frozen on the row and never re-derived, so a later rate change can't
		// rewrite this turn's spend. Null when unpriceable (no rate / no model).
		if r.stamper != nil {
			updates["cost_usd"] = r.stamper.Cost(modelcost.Tokens{
				ModelID:             u.Model,
				InputTokens:         u.InputTokens,
				OutputTokens:        u.OutputTokens,
				CacheReadTokens:     u.CacheReadTokens,
				CacheCreationTokens: u.CacheCreationTokens,
			})
		}
	}
	res := r.db.WithContext(ctx).
		Model(&AgentTurn{}).
		Where("id = ? AND status = ?", id, turnStatusRunning).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *turnRepository) Get(ctx context.Context, orgID, projectID, turnID string) (*AgentTurn, error) {
	var t AgentTurn
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND id = ?", orgID, projectID, turnID).
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *turnRepository) GetActive(ctx context.Context, orgID, projectID string) (*AgentTurn, error) {
	var t AgentTurn
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND status = ?", orgID, projectID, turnStatusRunning).
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *turnRepository) HasAny(ctx context.Context, orgID, projectID string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&AgentTurn{}).
		Where("org_id = ? AND project_id = ?", orgID, projectID).
		Count(&n).Error
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *turnRepository) LastTerminal(ctx context.Context, orgID, projectID, conversationID string) (*AgentTurn, error) {
	var t AgentTurn
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND conversation_id = ? AND status IN ?",
			orgID, projectID, conversationID, []string{turnStatusCompleted, turnStatusFailed}).
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

func (r *turnRepository) SweepStale(ctx context.Context, olderThan time.Time) ([]AgentTurn, error) {
	// One guarded UPDATE ... RETURNING so concurrent sweeps (or a Finish
	// racing the sweep) each claim a row at most once.
	var swept []AgentTurn
	err := r.db.WithContext(ctx).Raw(`
		UPDATE agent_turns
		SET status = ?, reason = ?, message = ?, updated_at = now()
		WHERE status = ? AND heartbeat_at < ?
		RETURNING *`,
		turnStatusFailed, turnReasonStreamDied, "replica crashed or hung",
		turnStatusRunning, olderThan.UTC()).
		Scan(&swept).Error
	if err != nil {
		return nil, err
	}
	return swept, nil
}

// encodePaths stores the conflicting-path list as a JSON array ("" for none).
func encodePaths(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	b, err := json.Marshal(paths)
	if err != nil {
		return ""
	}
	return string(b)
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
			"COALESCE(MAX(model_id), '') AS max_model").
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
		out[row.ProjectID] = contracts.StampedUsage{Tokens: u, CostUsd: row.CostUsd}
	}
	return out
}

// decodePaths reads the JSON array back (nil for empty/invalid).
func decodePaths(raw string) []string {
	if raw == "" {
		return nil
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return nil
	}
	return paths
}
