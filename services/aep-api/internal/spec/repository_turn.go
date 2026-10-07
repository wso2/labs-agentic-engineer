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

// The two statuses a CONSUMER outside this package branches on: the status
// poll folds the newest turn row into spec.agent (#562). Exported as aliases
// of the internal constants so the strings stay authored once — the store is
// still the only writer.
const (
	TurnStatusRunning = turnStatusRunning
	TurnStatusFailed  = turnStatusFailed
)

// Failure reasons (AgentTurn.Reason and the terminal event's `reason`).
const (
	turnReasonStreamDied     = "stream-died"
	turnReasonFoldParity     = "fold-parity"
	turnReasonBaseMoved      = "base-moved"
	turnReasonDispatchFailed = "dispatch-failed"
	turnReasonInternal       = "internal"
	// turnReasonAgentError: the agents service ended the turn with a coded
	// error frame — a failure it could name (AgentTurn.Code says which) — in
	// place of the manifest. Distinct from stream-died: the stream ended
	// cleanly, it just vouched for nothing.
	turnReasonAgentError = "agent-error"
)

// The error codes a coded agents error frame can carry (TurnErrorPart in
// packages/agent-stream), stored on the failed turn (AgentTurn.Code) so a
// reader can say why it failed. Mirrors the contract's TurnStatus.code enum.
const (
	TurnErrorProviderLimit   = "provider_limit"
	TurnErrorOutputTruncated = "output_truncated"
)

// knownTurnErrorCode reports whether code is one aep-api stores. An unknown
// code (a newer agents image) degrades to the uncoded path rather than
// writing a value the contract cannot represent.
func knownTurnErrorCode(code string) bool {
	return code == TurnErrorProviderLimit || code == TurnErrorOutputTruncated
}

// ErrTurnActive is returned by TryStart when another turn holds the D18
// one-active-turn guard (per project and chat view); the accompanying row is
// the active turn.
var ErrTurnActive = errors.New("a turn is already running for this project")

// TurnTerminal is the terminal state Finish stamps onto a running row.
type TurnTerminal struct {
	Status    string // turnStatusCompleted | turnStatusFailed
	CommitSHA string
	Reason    string
	Paths     []string
	NoChanges bool
	Message   string
	// Code names why a failed turn failed when the agents service could say
	// (TurnErrorProviderLimit / TurnErrorOutputTruncated); "" otherwise.
	// ResetAt is when the provider said its limit resets (provider_limit
	// only, and only when it said). Host is the model host the code is about —
	// carried to the terminal event only; the row already holds it as
	// model_host.
	Code    string
	ResetAt *time.Time
	Host    string
	// Usage is the turn's token usage off the terminal manifest (#249); nil
	// when the stream carried none (failed turns, pre-capture agents).
	Usage *contracts.TokenUsage
	// ContextTokens is the conversation's context size at the turn's end
	// (AgentTurn.ContextTokens); nil when the turn left no measure.
	ContextTokens *int64
	// Outcome is an Issues turn's last reply off its manifest
	// (AgentTurn.Outcome); "" when there was none.
	Outcome string
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
// turn record, the one-active guard (per project and use case — one per chat
// view), and the stale-heartbeat sweep. Lookups
// miss with (nil, nil), matching the house convention.
type TurnRepository interface {
	// TryStart INSERTs the running row; on conflict with the D18 partial
	// unique index (one running row per org, project and use case) it fetches
	// and returns the active row of t.UseCase alongside ErrTurnActive. On success the passed row (ID populated) is returned.
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

	// GetActive returns the project's running turn in one use case, or
	// (nil, nil).
	GetActive(ctx context.Context, orgID, projectID, useCase string) (*AgentTurn, error)

	// LastTerminal returns the most recent completed/failed turn of a
	// conversation — the D20 filesChangedExternally / divergence-note input.
	LastTerminal(ctx context.Context, orgID, projectID, conversationID string) (*AgentTurn, error)

	// LastContextTokens returns the context size the conversation's newest
	// MEASURED turn ended at (AgentTurn.ContextTokens), or nil when none of
	// its turns has one. An unmeasured later turn (a dispatch failure, a
	// severed stream) left the saved history as it was, so it is skipped
	// rather than read as an empty conversation.
	LastContextTokens(ctx context.Context, orgID, projectID, conversationID string) (*int64, error)

	// BranchOutcomes counts the project's COMPLETED turns of one use case
	// created after `since` that carry an outcome, and returns the newest
	// one's outcome ("" when there are none). The main chat's dispatch reads
	// the Issues chat this way to tell its agent what happened there since its
	// own previous turn (agentsvc.BranchNote).
	BranchOutcomes(ctx context.Context, orgID, projectID, useCase string, since time.Time) (turns int, latest string, err error)

	// NewestCompletedFlow returns the project's most recent COMPLETED turn of
	// one flow ("design", "start", …), or (nil, nil) when it has run none.
	//
	// The status read's staleness check (#575) asks for the newest successful
	// design run so it can read the requirements as that run saw them. Scoped
	// to completed because a failed or running turn never reconciled anything:
	// treating one as the baseline would clear a staleness warning on the
	// strength of work that did not land.
	NewestCompletedFlow(ctx context.Context, orgID, projectID, flow string) (*AgentTurn, error)

	// CompletedFlows returns up to `limit` of the project's COMPLETED turns of
	// one flow, newest first. The build gate reads the design runs this way to
	// find, per feature, the run that last designed it (E1).
	CompletedFlows(ctx context.Context, orgID, projectID, flow string, limit int) ([]AgentTurn, error)

	// Newest returns the project's most recent turn row in one use case,
	// running or terminal, across every conversation of it — or (nil, nil)
	// when nothing has ever run there.
	//
	// Two callers, both asking about the spec chat (UseCaseGeneral) "has this
	// project ever had an agent work on it, and what is it doing now" (#562):
	// the kickoff's idempotence guard, and the status poll's spec.agent field.
	// Use-case-scoped so an Issues turn is never mistaken for spec work; NOT
	// conversation-scoped BECAUSE rotation exists — a rotated thread would
	// otherwise make an interviewed project look untouched and re-fire the
	// kickoff into it.
	Newest(ctx context.Context, orgID, projectID, useCase string) (*AgentTurn, error)

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
		active, err := r.GetActive(ctx, t.OrgID, t.ProjectID, t.UseCase)
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
		"code":       terminal.Code,
		"reset_at":   terminal.ResetAt,
	}
	if terminal.ContextTokens != nil {
		updates["context_tokens"] = *terminal.ContextTokens
	}
	if terminal.Outcome != "" {
		updates["outcome"] = terminal.Outcome
	}
	if u := terminal.Usage; u != nil {
		updates["input_tokens"] = u.InputTokens
		updates["output_tokens"] = u.OutputTokens
		updates["cache_read_tokens"] = u.CacheReadTokens
		updates["cache_creation_tokens"] = u.CacheCreationTokens
		updates["model_id"] = u.Model
		// Stamp USD at capture from the rates in force now (#291): the cost is
		// frozen on the row and never re-derived, so a later rate change can't
		// rewrite this turn's spend. Priced on the host the row was admitted
		// with; null when unpriceable (no (host, model) rate, no host, no model).
		if r.stamper != nil {
			host, err := r.modelHost(ctx, id)
			if err != nil {
				return false, err
			}
			updates["cost_usd"] = r.stamper.Cost(modelcost.Tokens{
				Host:                host,
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

// modelHost reads the host TryStart wrote on the turn at admission. A missing
// row reads as no host (unpriced); Finish's guarded update then touches nothing.
func (r *turnRepository) modelHost(ctx context.Context, id string) (string, error) {
	var hosts []string
	if err := r.db.WithContext(ctx).
		Model(&AgentTurn{}).
		Where("id = ?", id).
		Pluck("COALESCE(model_host, '')", &hosts).Error; err != nil {
		return "", err
	}
	if len(hosts) == 0 {
		return "", nil
	}
	return hosts[0], nil
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

func (r *turnRepository) GetActive(ctx context.Context, orgID, projectID, useCase string) (*AgentTurn, error) {
	var t AgentTurn
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND use_case = ? AND status = ?",
			orgID, projectID, useCase, turnStatusRunning).
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
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

// LastContextTokens reads through the conversation_id index; a conversation
// holds tens of turns, not thousands, so the sort is over a handful of rows.
func (r *turnRepository) LastContextTokens(ctx context.Context, orgID, projectID, conversationID string) (*int64, error) {
	var tokens []int64
	err := r.db.WithContext(ctx).
		Model(&AgentTurn{}).
		Where("org_id = ? AND project_id = ? AND conversation_id = ? AND context_tokens IS NOT NULL",
			orgID, projectID, conversationID).
		Order("created_at DESC").
		Limit(1).
		Pluck("context_tokens", &tokens).Error
	if err != nil || len(tokens) == 0 {
		return nil, err
	}
	return &tokens[0], nil
}

// BranchOutcomes reads the project's turns through
// `ix_agent_turns_project_newest` (org_id, project_id, created_at DESC),
// filtering the use case as it walks; it runs once per main-chat dispatch and
// only over the window since that chat's previous turn.
func (r *turnRepository) BranchOutcomes(ctx context.Context, orgID, projectID, useCase string, since time.Time) (int, string, error) {
	var outcomes []string
	err := r.db.WithContext(ctx).
		Model(&AgentTurn{}).
		Where("org_id = ? AND project_id = ? AND use_case = ? AND status = ? AND created_at > ? AND outcome IS NOT NULL",
			orgID, projectID, useCase, turnStatusCompleted, since).
		Order("created_at DESC").
		Pluck("outcome", &outcomes).Error
	if err != nil || len(outcomes) == 0 {
		return 0, "", err
	}
	return len(outcomes), outcomes[0], nil
}

// Newest reads one row off `ix_agent_turns_project_newest`
// (org_id, project_id, created_at DESC — migrate/agent_turns.go), whose column
// order IS this query's, so it is a single index read rather than a sort of
// every turn the project has ever run. That matters: the status poll runs this
// every 5s per viewer while an agent works.
//
// The use case is filtered as the index is walked, so the read also steps over
// the other chat view's turns newer than the answer — the Issues chat's recent
// turns, bounded by that chat's own volume rather than the project's history.
//
// A RUNNING row is always the newest one its use case has: TryStart's partial
// unique admits at most one per use case, and no later row of that use case
// can be inserted while it holds the guard — so ordering by creation is enough
// to find it, with no status precedence.
func (r *turnRepository) Newest(ctx context.Context, orgID, projectID, useCase string) (*AgentTurn, error) {
	var t AgentTurn
	err := r.db.WithContext(ctx).
		Where("org_id = ? AND project_id = ? AND use_case = ?", orgID, projectID, useCase).
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
