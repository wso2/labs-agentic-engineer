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
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/contracts"
)

// AgentUsageLedgerRepository is the write-authority over the agent-usage ledger
// — the record of what an org's agent work cost, kept past the dispatch rows it
// was captured from and past the project itself.
//
// There is no Delete verb, and that absence is the point: the ledger is the one
// delivery table the project-delete cascade does not purge. Retire closes a
// lifetime; nothing removes what was spent.
type AgentUsageLedgerRepository interface {
	// RecordCycleUsage copies a run cycle's freshly-stamped usage into the ledger.
	//
	// It reads the figures from the cycle row rather than taking them as
	// arguments, so the ledger cannot disagree with the row it mirrors: the
	// caller stamps the row, then calls this, and one statement carries the
	// tokens, the write-time USD, the cycle's phase and its milestone across.
	//
	// Idempotent on (source, cycle id): re-capture re-derives the same figures
	// from the same log and updates the entry in place. A cycle with no project
	// records nothing — there is no card for it to land on.
	RecordCycleUsage(ctx context.Context, cycleID string) error

	// RecordExecutionUsage is the same for the older per-issue funnel's rows.
	RecordExecutionUsage(ctx context.Context, executionID string) error

	// RetireByProject closes the project's LIVE lifetime: every entry not already
	// retired is stamped with one shared instant, so the generation is a single
	// value the rollup can group on.
	//
	// It is what a project delete calls INSTEAD of purging spend. Runs and cycles
	// go; what they cost stays, attributed to the incarnation that spent it, so a
	// project recreated under the same name starts at zero.
	//
	// Idempotent: a second call finds nothing live and stamps nothing, which is
	// also what makes a re-run of a half-finished delete safe.
	RetireByProject(ctx context.Context, orgID, projectID string) error

	// SumUsageByProjectPhase rolls the ledger up per project LIFETIME and SDLC
	// phase, keyed by contracts.UsageScope so a live project and a deleted
	// incarnation of the same slug stay apart. Each map is keyed by scope; CostUsd
	// sums the frozen per-row stamps and is nil when no contributing entry carried
	// one.
	SumUsageByProjectPhase(ctx context.Context, orgID string) (build, validation map[contracts.UsageScope]contracts.StampedUsage, err error)
}

type agentUsageLedgerRepository struct{ db *gorm.DB }

// NewAgentUsageLedgerRepository wires the gorm-backed ledger.
func NewAgentUsageLedgerRepository(db *gorm.DB) AgentUsageLedgerRepository {
	return &agentUsageLedgerRepository{db: db}
}

// ledgerUpsertTail is the conflict clause both writers share. retired_at is
// deliberately absent: a late capture for a project that has already been
// deleted must land on the entry without reopening its lifetime.
const ledgerUpsertTail = `
	ON CONFLICT (source, source_id) DO UPDATE SET
	  phase                 = EXCLUDED.phase,
	  milestone_number      = EXCLUDED.milestone_number,
	  tag                   = EXCLUDED.tag,
	  model_id              = EXCLUDED.model_id,
	  model_host            = EXCLUDED.model_host,
	  input_tokens          = EXCLUDED.input_tokens,
	  output_tokens         = EXCLUDED.output_tokens,
	  cache_read_tokens     = EXCLUDED.cache_read_tokens,
	  cache_creation_tokens = EXCLUDED.cache_creation_tokens,
	  cost_usd              = EXCLUDED.cost_usd,
	  captured_at           = EXCLUDED.captured_at`

func (r *agentUsageLedgerRepository) RecordCycleUsage(ctx context.Context, cycleID string) error {
	// INSERT … SELECT rather than a read-then-write: the ledger is a copy of the
	// row that was just stamped, so taking it from the row in the same statement
	// is what makes the two impossible to desync.
	//
	// The milestone join is LEFT: a cycle whose run row has already gone still
	// records its spend, just without the version label.
	//
	// COALESCE(NULLIF(tag,''), milestone_title, '') mirrors MilestoneRun.SpecTag —
	// the tag is empty on pre-phase and early incident rows, whose title IS the
	// version.
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO agent_usage_ledger (
		  org_id, project_id, source, source_id, phase, milestone_number, tag,
		  model_id, model_host, input_tokens, output_tokens, cache_read_tokens,
		  cache_creation_tokens, cost_usd, captured_at)
		SELECT c.org_id, c.project_id, ?, c.id::text, `+UsagePhaseCaseSQL("c.kind")+`,
		       COALESCE(m.milestone_number, 0),
		       COALESCE(NULLIF(m.tag, ''), m.milestone_title, ''),
		       c.model_id, c.model_host, c.input_tokens, c.output_tokens, c.cache_read_tokens,
		       c.cache_creation_tokens, c.cost_usd, now()
		FROM run_cycles c
		LEFT JOIN milestone_runs m ON m.id::text = c.run_id
		WHERE c.id::text = ? AND c.org_id <> '' AND c.project_id <> ''`+ledgerUpsertTail,
		UsageLedgerSourceRunCycle, cycleID).Error
}

func (r *agentUsageLedgerRepository) RecordExecutionUsage(ctx context.Context, executionID string) error {
	// Executions belong to no milestone, so the version columns stay zero/empty —
	// the ledger says "this project, this phase" and does not invent a version.
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO agent_usage_ledger (
		  org_id, project_id, source, source_id, phase, milestone_number, tag,
		  model_id, model_host, input_tokens, output_tokens, cache_read_tokens,
		  cache_creation_tokens, cost_usd, captured_at)
		SELECT e.org_id, e.project_id, ?, e.id::text, `+UsagePhaseCaseSQL("e.kind")+`,
		       0, '',
		       e.model_id, e.model_host, e.input_tokens, e.output_tokens, e.cache_read_tokens,
		       e.cache_creation_tokens, e.cost_usd, now()
		FROM executions e
		WHERE e.id::text = ? AND e.org_id <> '' AND e.project_id <> ''`+ledgerUpsertTail,
		UsageLedgerSourceExecution, executionID).Error
}

func (r *agentUsageLedgerRepository) RetireByProject(ctx context.Context, orgID, projectID string) error {
	// One instant for the whole generation, computed here rather than as a
	// per-row now(), so every entry closed by this delete carries the SAME stamp
	// and the rollup can group on it.
	return r.db.WithContext(ctx).
		Model(&AgentUsageLedgerEntry{}).
		Where("org_id = ? AND project_id = ? AND retired_at IS NULL", orgID, projectID).
		Update("retired_at", time.Now().UTC()).Error
}

// ledgerHasTokens is the "this entry actually spent something" predicate, shared
// by the model-agreement CASEs so the notion of a contributing row is spelled
// once.
const ledgerHasTokens = "input_tokens + output_tokens + cache_read_tokens + cache_creation_tokens > 0"

// ledgerPhaseUsageRow is the per-(project, lifetime, phase) aggregate scan shape.
type ledgerPhaseUsageRow struct {
	ProjectID           string
	Phase               string
	Retired             bool
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUsd             *float64
	Models              int64
	MaxModel            string
	Hosts               int64
	MaxHost             string
}

func (r *agentUsageLedgerRepository) SumUsageByProjectPhase(ctx context.Context, orgID string) (map[contracts.UsageScope]contracts.StampedUsage, map[contracts.UsageScope]contracts.StampedUsage, error) {
	var rows []ledgerPhaseUsageRow
	err := r.db.WithContext(ctx).
		Model(&AgentUsageLedgerEntry{}).
		Select("project_id, phase, (retired_at IS NOT NULL) AS retired, "+
			"COALESCE(SUM(input_tokens),0) AS input_tokens, "+
			"COALESCE(SUM(output_tokens),0) AS output_tokens, "+
			"COALESCE(SUM(cache_read_tokens),0) AS cache_read_tokens, "+
			"COALESCE(SUM(cache_creation_tokens),0) AS cache_creation_tokens, "+
			"SUM(cost_usd) AS cost_usd, "+ // NULL when no entry is stamped — the #291 semantic
			// The phase's model id survives only while every contributor THAT SPENT
			// TOKENS agrees on it — exactly contracts.TokenUsage.Add, which keeps the
			// model across a zero-token contributor and blanks it on a genuine
			// disagreement. COUNT(DISTINCT …) and MAX(…) both ignore NULL, so the
			// entries that spent nothing drop out instead of dragging a single-model
			// phase to "unknown".
			"COUNT(DISTINCT CASE WHEN "+ledgerHasTokens+" THEN model_id END) AS models, "+
			"COALESCE(MAX(CASE WHEN "+ledgerHasTokens+" THEN model_id END), '') AS max_model, "+
			// The host the phase was billed by, under the same agreement rule
			// (contracts.StampedUsage.Add): an unstamped row counts as the "" host.
			"COUNT(DISTINCT CASE WHEN "+ledgerHasTokens+" THEN COALESCE(model_host, '') END) AS hosts, "+
			"COALESCE(MAX(CASE WHEN "+ledgerHasTokens+" THEN COALESCE(model_host, '') END), '') AS max_host").
		Where("org_id = ? AND project_id <> ''", orgID).
		Group("project_id, phase, (retired_at IS NOT NULL)").
		// Only lifetimes with real token traffic — an entry that captured nothing
		// must not conjure a phase, or a card, out of nothing.
		Having("SUM(input_tokens) + SUM(output_tokens) + SUM(cache_read_tokens) + SUM(cache_creation_tokens) > 0").
		Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	build := make(map[contracts.UsageScope]contracts.StampedUsage)
	validation := make(map[contracts.UsageScope]contracts.StampedUsage)
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
		scope := contracts.UsageScope{ProjectID: row.ProjectID, Retired: row.Retired}
		stamped := contracts.StampedUsage{Tokens: u, CostUsd: row.CostUsd}
		if row.Hosts == 1 {
			stamped.Host = row.MaxHost
		}
		if row.Phase == UsagePhaseValidation {
			validation[scope] = stamped
		} else {
			build[scope] = stamped
		}
	}
	return build, validation, nil
}
