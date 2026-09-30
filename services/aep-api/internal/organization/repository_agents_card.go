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
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// AgentsCardRepository is the unit of work behind the AI agents card: one
// save of the card (the `llm` and `agents` sections of PATCH /config) is one
// transaction over every row it touches — the model connection row, the
// Claude subscription row, the agent-settings row, and the encrypted secret
// bytes in org_secrets, whose store joins the same transaction. A failure anywhere rolls every one of them
// back, so the card is never half-saved.
//
// Tx begins the transaction, runs fn, and commits when fn returns nil or rolls
// back when it returns an error. fn takes the card's per-org advisory locks
// first (lockCard over AgentsCardTx.AdvisoryLock), so two saves of one org's
// card, and the copies that follow them, serialize.
type AgentsCardRepository interface {
	Tx(ctx context.Context, fn func(tx AgentsCardTx) error) error
}

// AgentsCardTx is the transaction-scoped surface Tx hands its closure. Every
// read goes through the transaction, so the state a save is judged against is
// the state it writes over.
type AgentsCardTx interface {
	// AdvisoryLock runs pg_advisory_xact_lock(hashtext(key)).
	AdvisoryLock(key string) error

	// GetCredential reads one role's row, or nil when absent.
	GetCredential(ocOrgID string, role AnthropicRole) (*OrgAnthropicCredential, error)
	// UpsertCredential INSERTs the row or, on (oc_org_id, role) conflict,
	// UPDATEs the metadata columns — preserving the ORIGINAL connected_at on a
	// replace, and clearing the SM-API secret_ref_* triplet the mirror stamps
	// after commit — and scans the persisted connected_at back into the row.
	UpsertCredential(row *OrgAnthropicCredential) error
	// DeleteCredential removes one role's row. Idempotent.
	DeleteCredential(ocOrgID string, role AnthropicRole) error
	// StampCredentialSecretRef records where one role's SM-API copy lives.
	StampCredentialSecretRef(ocOrgID string, role AnthropicRole, ref SecretRefTriplet) error

	// GetConnection reads the org's model connection, or nil when absent.
	GetConnection(ocOrgID string) (*OrgModelConnection, error)
	// UpsertConnection writes the whole row, creating it or replacing its
	// columns. keyWritten says the save stored a new key: only then is the
	// SM-API triplet cleared (the mirror re-stamps it after commit); a save
	// that kept the key leaves the triplet it already has.
	UpsertConnection(row *OrgModelConnection, keyWritten bool) error
	// DeleteConnection removes the row. Idempotent.
	DeleteConnection(ocOrgID string) error
	// StampConnectionSecretRef records where the connection key's SM-API copy
	// lives.
	StampConnectionSecretRef(ocOrgID string, ref SecretRefTriplet) error

	// GetSreModelConnection reads the org's SRE model connection, or nil when
	// absent.
	GetSreModelConnection(ocOrgID string) (*OrgSreModelConnection, error)
	// UpsertSreModelConnection writes the whole org_sre_model_connections row,
	// creating it or replacing its columns.
	UpsertSreModelConnection(row *OrgSreModelConnection) error
	// DeleteSreModelConnection removes the row. Idempotent.
	DeleteSreModelConnection(ocOrgID string) error

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

	// Secrets is the credential store bound to this transaction.
	Secrets() secrets.CredentialStore
}

type agentsCardRepository struct {
	db    *gorm.DB
	store secrets.TxCredentialStore
}

// NewAgentsCardRepository constructs the gorm-backed unit of work. store must be
// the same encrypted store the credential readers use, so what the card writes
// is what they read.
func NewAgentsCardRepository(db *gorm.DB, store secrets.TxCredentialStore) AgentsCardRepository {
	return &agentsCardRepository{db: db, store: store}
}

func (r *agentsCardRepository) Tx(ctx context.Context, fn func(tx AgentsCardTx) error) error {
	tx := r.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() { _ = tx.Rollback() }() // no-op once committed
	if err := fn(&agentsCardTx{tx: tx, secrets: r.store.WithDB(tx)}); err != nil {
		return err
	}
	return tx.Commit().Error
}

type agentsCardTx struct {
	tx      *gorm.DB
	secrets secrets.CredentialStore
}

func (t *agentsCardTx) AdvisoryLock(key string) error {
	return t.tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, key).Error
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
	// The secret_ref_* triplet is cleared on a replace. The SM-API path is fixed
	// per (org, role), so a triplet left in place after a failed mirror would
	// still resolve — to the vault copy of the PREVIOUS credential, which
	// dispatch would mount without a word. Cleared, the row carries no triplet
	// until mirrorKey re-stamps it, and dispatch fails closed naming why.
	return t.tx.Raw(`
		INSERT INTO org_anthropic_credentials
		    (oc_org_id, role, credential_kind, key_prefix, key_last4, status, connected_at, last_validated_at, validation_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT (oc_org_id, role) DO UPDATE
		  SET credential_kind    = EXCLUDED.credential_kind,
		      key_prefix         = EXCLUDED.key_prefix,
		      key_last4          = EXCLUDED.key_last4,
		      status             = EXCLUDED.status,
		      last_validated_at  = EXCLUDED.last_validated_at,
		      validation_error   = NULL,
		      secret_ref_name     = NULL,
		      secret_ref_kv_path  = NULL,
		      secret_ref_property = NULL
		RETURNING connected_at`,
		row.OcOrgID, row.Role, row.CredentialKind, row.KeyPrefix, row.KeyLast4, row.Status, row.ConnectedAt, row.LastValidatedAt,
	).Scan(&row.ConnectedAt).Error
}

func (t *agentsCardTx) DeleteCredential(ocOrgID string, role AnthropicRole) error {
	return t.tx.Exec(`DELETE FROM org_anthropic_credentials WHERE oc_org_id = ? AND role = ?`, ocOrgID, role).Error
}

func (t *agentsCardTx) StampCredentialSecretRef(ocOrgID string, role AnthropicRole, ref SecretRefTriplet) error {
	return t.tx.Model(&OrgAnthropicCredential{}).
		Where("oc_org_id = ? AND role = ?", ocOrgID, role).
		Updates(stampSecretRefTriplet(ref.Name, ref.KVPath, ref.Property)).Error
}

func (t *agentsCardTx) GetConnection(ocOrgID string) (*OrgModelConnection, error) {
	return getModelConnection(t.tx, ocOrgID)
}

// connectionColumns are the columns a save rewrites; connected_at is among
// them because the writer decides it (kept on the same host, reset on another).
var connectionColumns = []string{
	"format", "base_url", "host", "model", "auth_scheme", "context_window", "output_limit",
	"image_input", "key_preview", "connected_at", "updated_at", "updated_by",
}

func (t *agentsCardTx) UpsertConnection(row *OrgModelConnection, keyWritten bool) error {
	cols := connectionColumns
	if keyWritten {
		// The SM-API path is fixed per org, so a triplet left in place after a
		// failed mirror would still resolve — to the vault copy of the PREVIOUS
		// key, which dispatch would mount without a word. Cleared, the row
		// carries no triplet until mirrorKey re-stamps it.
		row.SecretRefName, row.SecretRefKVPath, row.SecretRefProperty = nil, nil, nil
		cols = append(append([]string{}, cols...), "secret_ref_name", "secret_ref_kv_path", "secret_ref_property")
	}
	return t.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "oc_org_id"}},
		DoUpdates: clause.AssignmentColumns(cols),
	}).Create(row).Error
}

func (t *agentsCardTx) DeleteConnection(ocOrgID string) error {
	return t.tx.Where("oc_org_id = ?", ocOrgID).Delete(&OrgModelConnection{}).Error
}

func (t *agentsCardTx) StampConnectionSecretRef(ocOrgID string, ref SecretRefTriplet) error {
	return t.tx.Model(&OrgModelConnection{}).
		Where("oc_org_id = ?", ocOrgID).
		Updates(stampSecretRefTriplet(ref.Name, ref.KVPath, ref.Property)).Error
}

// sreModelConnectionColumns are the columns a save rewrites; connected_at is
// among them because the writer decides it (kept on the same host, reset on
// another), same as connectionColumns above.
var sreModelConnectionColumns = []string{"base_url", "host", "model", "connected_at", "updated_at", "updated_by"}

func (t *agentsCardTx) GetSreModelConnection(ocOrgID string) (*OrgSreModelConnection, error) {
	return getSreModelConnection(t.tx, ocOrgID)
}

func (t *agentsCardTx) UpsertSreModelConnection(row *OrgSreModelConnection) error {
	return t.tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "oc_org_id"}},
		DoUpdates: clause.AssignmentColumns(sreModelConnectionColumns),
	}).Create(row).Error
}

func (t *agentsCardTx) DeleteSreModelConnection(ocOrgID string) error {
	return t.tx.Where("oc_org_id = ?", ocOrgID).Delete(&OrgSreModelConnection{}).Error
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

func (t *agentsCardTx) Secrets() secrets.CredentialStore { return t.secrets }
