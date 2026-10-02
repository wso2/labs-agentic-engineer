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

// model_key_rename.go — moves every org's model connection key off its
// Anthropic-era storage names, by copy-then-switch.
//
// The key lives in two places: its encrypted bytes in org_secrets (read by the
// spec agents, task planning and Agent Manager) and a mirror in SM-API (the
// vault path a coding run's ExternalSecret mounts, named by the row's
// secret_ref_* columns). Both carried Anthropic's name — `anthropic/key` and
// entity `anthropic` — and move to `model/key` and entity `model-connection`.
//
// The move is split by what each half needs. The bytes are copied at boot by
// migrate's phase20_model_key_rename, before anything is served, because every
// reader looks under `model/key`. The mirror needs the
// SM-API client, which exists only once the app is assembled, so this
// reconciler moves it: a pass at boot, then one every modelKeyRenameInterval.
// Per org still holding `anthropic/key`, step 1 runs on every pass and step 2
// only on the periodic ones:
//
//  1. One transaction under the card's lock: copy the bytes again if the old
//     key is newer (a replica of the previous release saved mid-rollout) and
//     the org still has a connection (a disconnected key is not brought
//     back); if the row still names the Anthropic-era reference, write the
//     key as a new default-key reference and switch the row's secret-ref
//     columns onto it. The write is inside the lock so a card save cannot
//     land between reading the key and switching to its copy.
//  2. After commit, once the row names the new copy (or none, or is gone) and
//     no cycle of the org is open: delete the old SM-API copy, then the old
//     bytes, best-effort, like forgetKey. The `anthropic/key` row goes last and
//     is the marker that the org still has work left. A row naming a reference
//     neither copy has keeps both old copies.
//
// Every step is idempotent and each crash point leaves the org readable: until
// the switch commits, the row names the old copy, which still exists; after
// it, the new one; the old copies go only after that. A re-run finishes what
// an interrupted one left, and does nothing once no `anthropic/key` remains.
//
// Nothing is deleted at boot, neither by phase20 nor by the boot pass: a
// replica of the previous release still draining reads `anthropic/key` for
// chat turns, so the old copies are retired from the first periodic pass on,
// one interval after the new release starts.
//
// The open-cycle gate keeps a run in flight on the old copy safe. Its pod reads
// the key from a Secret that the run's ExternalSecret synced from the vault
// path; ESO keeps that Secret when the remote key disappears (deletionPolicy
// Retain, the default the platform's ExternalSecrets leave in place), and a
// started container already has its env. What the gate covers is a run whose
// ExternalSecret had not synced yet when the copy went: the old copies wait
// until the org has no open cycle, and every cycle opened after the switch
// mounts the new copy.
package organization

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// The connection key's Anthropic-era storage names, read only by the rename
// (a disconnect deletes the bytes under both names).
const (
	legacyModelKeyStoreKey     = "anthropic/key"
	legacyModelKeySecretEntity = "anthropic"
)

// The SecretReference names the two mirrors have in every org: SM-API names an
// org-scoped reference by its entity alone.
var (
	legacyModelKeyRefName = secretmanagersvc.SecretLocation{EntityName: legacyModelKeySecretEntity}.SecretRefName()
	modelKeyRefName       = secretmanagersvc.SecretLocation{EntityName: modelKeySecretEntity}.SecretRefName()
)

// modelKeyEntityOf is the SM-API entity a connection key's reference name
// belongs to: the Anthropic-era entity for a row the rename has not moved yet,
// the connection's entity otherwise.
func modelKeyEntityOf(secretRefName string) string {
	if secretRefName == legacyModelKeyRefName {
		return legacyModelKeySecretEntity
	}
	return modelKeySecretEntity
}

// modelKeyRenameInterval is how often the rename looks for work after its boot
// pass: it catches a key saved by a replica of the previous release during a
// rolling deploy, and retires old copies once the previous release has had an
// interval to drain and the org's open cycles have ended.
const modelKeyRenameInterval = 10 * time.Minute

// OpenCycles answers whether an org has an agent run that may still be
// starting on the credential it was dispatched with. Satisfied by
// delivery.RunCycleRepository.
type OpenCycles interface {
	HasOpenCycle(ctx context.Context, ocOrgID string) (bool, error)
}

// ModelKeyRename — see file doc. A background watcher.
type ModelKeyRename struct {
	repo   ModelKeyRenameRepository
	orgs   OrganizationRepository
	writer *SecretRefWriter
	cycles OpenCycles
}

// NewModelKeyRename wires the rename. writer may be disabled (no secrets
// provider), in which case there is no mirror to move; every other
// collaborator must be non-nil.
func NewModelKeyRename(repo ModelKeyRenameRepository, orgs OrganizationRepository, writer *SecretRefWriter, cycles OpenCycles) *ModelKeyRename {
	return &ModelKeyRename{repo: repo, orgs: orgs, writer: writer, cycles: cycles}
}

// Run makes the boot pass, then a periodic pass every modelKeyRenameInterval
// until ctx ends.
func (r *ModelKeyRename) Run(ctx context.Context) {
	ticker := time.NewTicker(modelKeyRenameInterval)
	defer ticker.Stop()
	if err := r.BootPass(ctx); err != nil {
		slog.WarnContext(ctx, "model key rename: boot pass failed", "error", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := r.Pass(ctx); err != nil {
			slog.WarnContext(ctx, "model key rename: pass failed", "error", err)
		}
	}
}

// BootPass moves and switches every org that still holds the key under its
// Anthropic-era name, and deletes nothing (see the file doc).
func (r *ModelKeyRename) BootPass(ctx context.Context) error {
	return r.pass(ctx, false)
}

// Pass is a periodic pass: BootPass's move and switch, then the old copies
// retired wherever they are no longer needed.
func (r *ModelKeyRename) Pass(ctx context.Context) error {
	return r.pass(ctx, true)
}

// pass runs over every org still holding `anthropic/key`. One org's failure is
// logged and left for the next pass; only failing to list the orgs is
// returned.
func (r *ModelKeyRename) pass(ctx context.Context, retire bool) error {
	orgs, err := r.repo.OrgsWithLegacyKey(ctx)
	if err != nil {
		return fmt.Errorf("model key rename: list orgs: %w", err)
	}
	for _, ocOrgID := range orgs {
		if err := r.moveOrg(ctx, ocOrgID, retire); err != nil {
			slog.WarnContext(ctx, "model key rename: org left on its previous copy until the next pass",
				"ocOrgId", ocOrgID, "error", err)
		}
	}
	return nil
}

// errLegacyRefKept: the row still names the old copy, so the old copies stay.
var errLegacyRefKept = errors.New("the row still names the Anthropic-era SM-API copy")

func (r *ModelKeyRename) moveOrg(ctx context.Context, ocOrgID string, retire bool) error {
	// SM-API derives the vault path from the org's ouId. Without one the
	// bytes still move; the mirror and the old copies wait.
	smCtx, claimsErr := ctx, error(nil)
	if r.writer.Enabled() {
		smCtx, claimsErr = r.orgClaims(ctx, ocOrgID)
	}
	// A row that could not be switched still commits the byte copy: the
	// bytes' readers must not wait on SM-API.
	var kept error
	if err := r.repo.Tx(ctx, func(tx ModelKeyRenameTx) (err error) {
		kept, err = r.copyAndSwitch(smCtx, tx, ocOrgID, claimsErr)
		return err
	}); err != nil {
		return err
	}
	if kept != nil {
		return kept
	}
	if !retire {
		return nil
	}
	if claimsErr != nil {
		return claimsErr
	}
	return r.retireLegacy(smCtx, ocOrgID)
}

// copyAndSwitch is step 1 of the file doc, inside the transaction. kept
// reports a row left on the old copy (the transaction still commits); err
// rolls it back. claimsErr is why ctx cannot reach SM-API, if it cannot.
func (r *ModelKeyRename) copyAndSwitch(ctx context.Context, tx ModelKeyRenameTx, ocOrgID string, claimsErr error) (kept, err error) {
	if err := tx.Lock(ocOrgID); err != nil {
		return nil, fmt.Errorf("lock: %w", err)
	}
	if err := tx.CopyLegacyKey(ocOrgID); err != nil {
		return nil, fmt.Errorf("copy bytes: %w", err)
	}
	row, err := tx.Connection(ocOrgID)
	if err != nil {
		return nil, fmt.Errorf("load connection: %w", err)
	}
	if row == nil {
		return nil, nil // disconnected: nothing to switch, the old copies go
	}
	switch name := derefOrEmpty(row.SecretRefName); name {
	case modelKeyRefName, "":
		// On the new copy, or on none (a save cleared it and its write stamps
		// the new name): nothing to switch.
		return nil, nil
	case legacyModelKeyRefName:
	default:
		// A save's default-key write names a fresh reference each time: the
		// row is on the new copy when the default-key row records the name.
		recorded, err := r.writer.recordedRef(ctx, ocOrgID, OrgSecretDefaultKey)
		if err != nil {
			return fmt.Errorf("%w: read the default-key row: %w", errLegacyRefKept, err), nil
		}
		if recorded == name {
			return nil, nil
		}
		// A name neither copy has: the old copies may be what it points at, so
		// they stay until someone looks.
		return fmt.Errorf("the row names an unrecognized SM-API reference %q; the previous copies are kept", name), nil
	}
	if !r.writer.Enabled() {
		return fmt.Errorf("%w, and no secrets provider is configured to move it", errLegacyRefKept), nil
	}
	if claimsErr != nil {
		return fmt.Errorf("%w: %w", errLegacyRefKept, claimsErr), nil
	}
	key, err := tx.Key(ctx, ocOrgID)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	// The new copy is a default-key write whose repoint switches the row onto
	// it. Its Retire is never run: the Anthropic-era copy goes in step 2, once
	// no cycle of the org is open.
	var switchErr error
	written, err := r.writer.WriteModelKey(ctx, ocOrgID, string(key), legacyModelKeyRefName, func(ref SecretRefTriplet) error {
		switchErr = tx.SwitchSecretRef(ocOrgID, legacyModelKeyRefName, ref)
		return switchErr
	})
	if switchErr != nil {
		return nil, fmt.Errorf("switch secret reference: %w", switchErr)
	}
	if err != nil {
		return fmt.Errorf("%w: upload the new copy: %w", errLegacyRefKept, err), nil
	}
	slog.InfoContext(ctx, "model key rename: connection key moved to its new SM-API copy",
		"ocOrgId", ocOrgID, "secretRefName", written.Name)
	return nil, nil
}

// retireLegacy is step 2 of the file doc, after commit.
func (r *ModelKeyRename) retireLegacy(ctx context.Context, ocOrgID string) error {
	open, err := r.cycles.HasOpenCycle(ctx, ocOrgID)
	if err != nil {
		return fmt.Errorf("check open cycles: %w", err)
	}
	if open {
		slog.InfoContext(ctx, "model key rename: previous copy kept while the org has an open cycle",
			"ocOrgId", ocOrgID)
		return nil
	}
	if r.writer.Enabled() {
		if err := r.writer.DeleteModelKey(ctx, ocOrgID, legacyModelKeyRefName); err != nil {
			return fmt.Errorf("delete the previous SM-API copy: %w", err)
		}
	}
	if err := r.repo.DeleteLegacyKey(ctx, ocOrgID); err != nil {
		return fmt.Errorf("delete the previous bytes: %w", err)
	}
	return nil
}

// orgClaims carries the org's Thunder ouId, which SM-API derives the vault
// path from, the way a signed-in user's token would (as the dev secret resync
// does). On error it returns ctx unchanged.
func (r *ModelKeyRename) orgClaims(ctx context.Context, ocOrgID string) (context.Context, error) {
	org, err := r.orgs.GetByName(ctx, ocOrgID)
	if err != nil {
		return ctx, fmt.Errorf("load organization: %w", err)
	}
	if org == nil || org.ThunderOrgUUID == nil {
		return ctx, errors.New("no thunder_org_uuid for the org — cannot derive its vault path")
	}
	return jwtassertion.ContextWithTokenClaims(ctx, &jwtassertion.TokenClaims{OuId: org.ThunderOrgUUID.String()}), nil
}
