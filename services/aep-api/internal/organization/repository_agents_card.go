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

package organization

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AgentsCardRepository is the unit of work behind the AI agents card: one
// save of the card (the `llm` and `agents` sections of PATCH /config) is one
// transaction over every row it touches — the model connection row, the
// Claude subscription row and the agent-settings row. A failure anywhere rolls
// every one of them back, so the card is never half-saved. The keys are not
// among them: they live only in vault (the org secrets' default-key and
// coding-agent-key), written by the save before this transaction runs.
type AgentsCardRepository interface {
	// Lock takes the card's per-org locks and holds them until unlock: a
	// save holds them from its first read through its vault writes, its
	// transaction and its copies, so two saves of one org's card never
	// interleave. Only the wait is bounded by ctx. unlock is safe to call
	// once, from a defer.
	Lock(ctx context.Context, ocOrgID string) (unlock func(), err error)
	// Tx begins the transaction, runs fn, and commits when fn returns nil or
	// rolls back when it returns an error. The caller holds Lock.
	Tx(ctx context.Context, fn func(tx AgentsCardTx) error) error
}

// AgentsCardTx is the transaction-scoped surface Tx hands its closure.
type AgentsCardTx interface {
	// GetCredential reads one role's row, or nil when absent.
	GetCredential(ocOrgID string, role AnthropicRole) (*OrgAnthropicCredential, error)
	// UpsertCredential INSERTs the row or, on (oc_org_id, role) conflict,
	// UPDATEs the metadata columns — preserving the ORIGINAL connected_at on a
	// replace — and scans the persisted connected_at back into the row.
	UpsertCredential(row *OrgAnthropicCredential) error
	// DeleteCredential removes one role's row. Idempotent.
	DeleteCredential(ocOrgID string, role AnthropicRole) error

	// GetConnection reads the org's model connection, or nil when absent.
	GetConnection(ocOrgID string) (*OrgModelConnection, error)
	// UpsertConnection writes the whole row, creating it or replacing its
	// columns.
	UpsertConnection(row *OrgModelConnection) error
	// DeleteConnection removes the row. Idempotent.
	DeleteConnection(ocOrgID string) error

	// GetSettings reads the org's agent setting, or nil when absent.
	GetSettings(ocOrgID string) (*OrgAgentSettings, error)
	// UpsertSettings writes the whole row, creating it or replacing every column.
	UpsertSettings(row *OrgAgentSettings) error
	// DeleteSettings removes the row, putting the org back on the platform
	// defaults. Idempotent.
	DeleteSettings(ocOrgID string) error

	// SetKeyDisconnectedAt records when the org's connection was disconnected
	// (organizations.llm_disconnected_at); nil clears it. A missing
	// organizations row is not an error: there is nothing to record against.
	SetKeyDisconnectedAt(ocOrgID string, at *time.Time) error
}

// cardLockPrefixes are the card's per-org advisory lock names, taken in this
// order. `org_anthropic:` is the name an earlier release takes; holding it
// too keeps a replica of that release, saving during a rolling deploy,
// serialized with this one. Both are pg_advisory_xact_lock(hashtext(name)),
// the earlier release's spelling, so the two contend on the same locks.
var cardLockPrefixes = []string{"org_anthropic:", "org_model:"}

type agentsCardRepository struct {
	db *gorm.DB
}

// NewAgentsCardRepository constructs the gorm-backed unit of work.
func NewAgentsCardRepository(db *gorm.DB) AgentsCardRepository {
	return &agentsCardRepository{db: db}
}

// Lock holds the card's advisory locks in a transaction on one pooled
// connection; releasing rolls it back, which drops the locks (as does the
// server if the connection dies), and returns the connection to the pool.
// The transaction ignores ctx cancellation once the locks are held, so a
// cancelled request cannot drop them while its save is still running (the
// org secret lock's shape, repository_org_secret.go).
func (r *agentsCardRepository) Lock(ctx context.Context, ocOrgID string) (func(), error) {
	sqlDB, err := r.db.DB()
	if err != nil {
		return nil, fmt.Errorf("agents card: lock: %w", err)
	}
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("agents card: lock: %w", err)
	}
	tx, err := conn.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("agents card: lock: %w", err)
	}
	release := func() {
		_ = tx.Rollback()
		_ = conn.Close()
	}
	for _, prefix := range cardLockPrefixes {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, prefix+ocOrgID); err != nil {
			release()
			return nil, fmt.Errorf("agents card: lock: %w", err)
		}
	}
	return release, nil
}

func (r *agentsCardRepository) Tx(ctx context.Context, fn func(tx AgentsCardTx) error) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed
	if err := fn(&agentsCardTx{tx: tx}); err != nil {
		return err
	}
	return tx.Commit().Error
}

type agentsCardTx struct {
	tx *gorm.DB
}

func (t *agentsCardTx) GetCredential(ocOrgID string, role AnthropicRole) (*OrgAnthropicCredential, error) {
	var row OrgAnthropicCredential
	err := t.tx.Where("oc_org_id = ? AND role = ?", ocOrgID, role).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (t *agentsCardTx) UpsertCredential(row *OrgAnthropicCredential) error {
	// The UPDATE deliberately omits connected_at so a replace preserves the
	// ORIGINAL connection time; RETURNING reads the persisted value back so the
	// projection matches the stored row.
	//
	// credential_kind is written on BOTH the insert and the update. Omitting it
	// would leave the column at its 'api_key' default, and dispatch would then
	// mount a subscription token as ANTHROPIC_API_KEY — a name Claude Code ranks
	// higher, so the run would bill the wrong credential with no error to show.
	//
	// key_prefix and key_last4 are written empty: no character of the token
	// is kept (the columns are NOT NULL until they are dropped).
	return t.tx.Raw(`
		INSERT INTO org_anthropic_credentials
		    (oc_org_id, role, credential_kind, key_prefix, key_last4, status, connected_at, last_validated_at, validation_error)
		VALUES (?, ?, ?, '', '', ?, ?, ?, NULL)
		ON CONFLICT (oc_org_id, role) DO UPDATE
		  SET credential_kind    = EXCLUDED.credential_kind,
		      key_prefix         = '',
		      key_last4          = '',
		      status             = EXCLUDED.status,
		      last_validated_at  = EXCLUDED.last_validated_at,
		      validation_error   = NULL
		RETURNING connected_at`,
		row.OcOrgID, row.Role, row.CredentialKind, row.Status, row.ConnectedAt, row.LastValidatedAt,
	).Scan(&row.ConnectedAt).Error
}

func (t *agentsCardTx) DeleteCredential(ocOrgID string, role AnthropicRole) error {
	return t.tx.Exec(`DELETE FROM org_anthropic_credentials WHERE oc_org_id = ? AND role = ?`, ocOrgID, role).Error
}

func (t *agentsCardTx) GetConnection(ocOrgID string) (*OrgModelConnection, error) {
	return getModelConnection(t.tx, ocOrgID)
}

// connectionColumns are the columns a save rewrites; connected_at is among
// them because the writer decides it (kept on the same host, reset on another).
var connectionColumns = []string{
	"format", "base_url", "host", "model", "auth_scheme", "context_window", "output_limit",
	"image_input", "connected_at", "updated_at", "updated_by",
}

func (t *agentsCardTx) UpsertConnection(row *OrgModelConnection) error {
	return t.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "oc_org_id"}},
		DoUpdates: clause.AssignmentColumns(connectionColumns),
	}).Create(row).Error
}

func (t *agentsCardTx) DeleteConnection(ocOrgID string) error {
	return t.tx.Where("oc_org_id = ?", ocOrgID).Delete(&OrgModelConnection{}).Error
}

func (t *agentsCardTx) GetSettings(ocOrgID string) (*OrgAgentSettings, error) {
	return getAgentSettings(t.tx, ocOrgID)
}

func (t *agentsCardTx) UpsertSettings(row *OrgAgentSettings) error {
	return t.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "oc_org_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"runtime", "updated_by", "updated_at"}),
	}).Create(row).Error
}

func (t *agentsCardTx) DeleteSettings(ocOrgID string) error {
	return t.tx.Where("oc_org_id = ?", ocOrgID).Delete(&OrgAgentSettings{}).Error
}

func (t *agentsCardTx) SetKeyDisconnectedAt(ocOrgID string, at *time.Time) error {
	return t.tx.Exec(`UPDATE organizations SET llm_disconnected_at = ? WHERE name = ?`, at, ocOrgID).Error
}
