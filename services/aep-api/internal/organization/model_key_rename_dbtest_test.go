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

package organization_test

// DBTEST tier — the model connection key's rename (model_key_rename.go) over a
// real Postgres + AES-GCM store and a fake SM-API. What is pinned here:
//
//   - every org's key is readable under `model/key` with identical bytes, and
//     its row names the new SM-API entity; coding dispatch then mounts it;
//   - the boot pass moves and switches but deletes nothing; the previous
//     copies go from a periodic pass, after the switch, once the org has no
//     open cycle;
//   - a pass interrupted between copy and switch, or between switch and
//     delete, leaves the org readable and converges on the next pass;
//   - a second pass is a no-op;
//   - a key saved under the old name by a replica of the previous release is
//     copied again, a newer `model/key` never overwritten.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

const (
	renameLegacyRef = "anthropic-secrets"
	renameNewRef    = "model-connection-secrets"
)

// openCycles is the rename's view of delivery: which orgs have a cycle open.
type openCycles map[string]bool

func (o openCycles) HasOpenCycle(_ context.Context, ocOrgID string) (bool, error) {
	return o[ocOrgID], nil
}

type renameDB struct {
	db     *gorm.DB
	store  secrets.TxCredentialStore
	sm     *fakeSMClient
	cycles openCycles
	rename *organization.ModelKeyRename
	conns  *organization.ModelConnectionService
	ouIDs  map[string]string
}

func newRenameDB(t *testing.T) *renameDB {
	t.Helper()
	db := dbtest.New(t)
	store, err := secrets.NewDBStore(db, []byte(anthropicDBAESKey))
	if err != nil {
		t.Fatalf("real DBStore: %v", err)
	}
	sm := &fakeSMClient{createRef: renameNewRef}
	connRepo := organization.NewOrgModelConnectionRepository(db)
	anthropicRepo := organization.NewOrgAnthropicRepository(db)
	writer := organization.NewSecretRefWriter(sm, organization.NewOrgCredentialRepository(db, nil), anthropicRepo,
		organization.NewIDPRepository(db, nil), connRepo).
		WithOrgSecretWriter(organization.NewOrgSecretWriter(sm, organization.NewOrgSecretRepository(db), organization.NewOrgSecretLock(db), fixedClock))
	cycles := openCycles{}
	return &renameDB{
		db: db, store: store, sm: sm, cycles: cycles,
		rename: organization.NewModelKeyRename(organization.NewModelKeyRenameRepository(db, store),
			organization.NewOrganizationRepository(db), writer, cycles),
		conns: organization.NewModelConnectionService(connRepo, anthropicRepo, store, sonnetRates()),
		ouIDs: map[string]string{},
	}
}

// seedLegacyOrg is an org as the previous release left it: a connection whose
// key's bytes sit under `anthropic/key` and whose row names the `anthropic`
// SM-API copy. withOU=false leaves thunder_org_uuid NULL.
func (r *renameDB) seedLegacyOrg(t *testing.T, org, key string, withOU bool) {
	t.Helper()
	var ou *uuid.UUID
	if withOU {
		id := uuid.New()
		ou = &id
		r.ouIDs[org] = id.String()
	}
	if err := r.db.Create(&organization.Organization{UUID: uuid.New(), Name: org, ThunderOrgUUID: ou}).Error; err != nil {
		t.Fatalf("seed organization %s: %v", org, err)
	}
	seedModelConnectionRow(t, r.db, org)
	if err := r.db.Exec(`UPDATE org_model_connections
		   SET secret_ref_name = ?, secret_ref_kv_path = ?, secret_ref_property = 'api-key' WHERE oc_org_id = ?`,
		renameLegacyRef, "user-app-secrets/wc-"+org+"/"+renameLegacyRef, org).Error; err != nil {
		t.Fatalf("seed legacy triplet %s: %v", org, err)
	}
	if err := r.store.Put(context.Background(), org, "anthropic/key", []byte(key)); err != nil {
		t.Fatalf("seed legacy bytes %s: %v", org, err)
	}
}

func (r *renameDB) pass(t *testing.T) {
	t.Helper()
	if err := r.rename.Pass(context.Background()); err != nil {
		t.Fatalf("pass: %v", err)
	}
}

// hasLegacyBytes reports whether the org still holds `anthropic/key`.
func (r *renameDB) hasLegacyBytes(t *testing.T, org string) bool {
	t.Helper()
	_, err := r.store.Get(context.Background(), org, "anthropic/key")
	if err != nil && !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("read legacy bytes %s: %v", org, err)
	}
	return err == nil
}

func (r *renameDB) wantKey(t *testing.T, org, want string) {
	t.Helper()
	got, err := r.store.Get(context.Background(), org, "model/key")
	if err != nil || string(got) != want {
		t.Fatalf("%s model/key = %q (%v), want %q", org, got, err, want)
	}
}

// wantDispatchRef asserts the reference a coding run launched now mounts.
func (r *renameDB) wantDispatchRef(t *testing.T, org, want string) {
	t.Helper()
	cred, err := r.conns.ResolveCodingCredential(context.Background(), org, orgconfig.AgentRuntimeOpenCode)
	if err != nil {
		t.Fatalf("%s ResolveCodingCredential: %v", org, err)
	}
	if cred.Ref.Name != want || !strings.HasSuffix(cred.Ref.KVPath, "/"+want) || cred.Ref.Property != "api-key" {
		t.Fatalf("%s dispatch mounts %+v, want reference %q", org, cred.Ref, want)
	}
}

// rowVersions is every row the rename could write, by xmin: equal before and
// after means nothing was rewritten.
func (r *renameDB) rowVersions(t *testing.T) map[string]string {
	t.Helper()
	var rows []struct{ K, V string }
	if err := r.db.Raw(`
		SELECT 'conn:' || oc_org_id AS k, xmin::text AS v FROM org_model_connections
		UNION ALL
		SELECT 'secret:' || oc_org_id || ':' || key, xmin::text FROM org_secrets`).Scan(&rows).Error; err != nil {
		t.Fatalf("row versions: %v", err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.K] = row.V
	}
	return out
}

func TestModelKeyRename_MovesEveryOrgAndASecondPassIsANoOp_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	keys := map[string]string{"acme": "sk-ant-api03-acme-key-bytes-0001", "globex": "sk-ant-api03-globex-key-bytes-0002"}
	for org, key := range keys {
		r.seedLegacyOrg(t, org, key, true)
	}
	r.wantDispatchRef(t, "acme", renameLegacyRef)

	r.pass(t)

	for org, key := range keys {
		r.wantKey(t, org, key)
		if _, got, ok, err := r.conns.Effective(context.Background(), org); err != nil || !ok || got != key {
			t.Fatalf("%s effective key = %q ok=%v (%v)", org, got, ok, err)
		}
		if r.hasLegacyBytes(t, org) {
			t.Fatalf("%s still holds anthropic/key", org)
		}
		r.wantDispatchRef(t, org, renameNewRef)
	}
	if len(r.sm.createCalls) != 2 {
		t.Fatalf("uploads = %d, want one per org", len(r.sm.createCalls))
	}
	for _, call := range r.sm.createCalls {
		org := call.loc.ControlPlaneNamespace
		if call.loc.EntityName != "default-key" || call.loc.OrgName != r.ouIDs[org] || call.data["api-key"] != keys[org] {
			t.Fatalf("upload %+v, want a default-key reference under the org's ouId with its key", call.loc)
		}
		if ref, err := organization.NewOrgSecretRepository(r.db).Get(context.Background(), org, organization.OrgSecretDefaultKey); err != nil || ref == nil || ref.Name != renameNewRef {
			t.Fatalf("%s default-key row = %+v (%v), want the new copy recorded", org, ref, err)
		}
	}
	if len(r.sm.deleteCalls) != 2 {
		t.Fatalf("deletes = %d, want the previous copy of each org", len(r.sm.deleteCalls))
	}
	for _, call := range r.sm.deleteCalls {
		if call.loc.EntityName != "anthropic" || call.secretRefName != renameLegacyRef {
			t.Fatalf("delete %+v %q, want the anthropic entity's copy", call.loc, call.secretRefName)
		}
	}

	before := r.rowVersions(t)
	r.pass(t)
	if after := r.rowVersions(t); len(after) != len(before) {
		t.Fatalf("a second pass changed the rows: %v -> %v", before, after)
	} else {
		for k, v := range before {
			if after[k] != v {
				t.Fatalf("a second pass rewrote %s", k)
			}
		}
	}
	if len(r.sm.createCalls) != 2 || len(r.sm.deleteCalls) != 2 {
		t.Fatalf("a second pass called SM-API: %d uploads, %d deletes", len(r.sm.createCalls), len(r.sm.deleteCalls))
	}
}

// A run in flight may still be starting on the previous copy: it survives
// until the org has no open cycle, while new dispatches already mount the new
// one.
func TestModelKeyRename_KeepsThePreviousCopyWhileACycleIsOpen_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	r.seedLegacyOrg(t, "acme", "sk-ant-api03-acme-key-bytes-0001", true)
	r.cycles["acme"] = true

	r.pass(t)
	r.wantDispatchRef(t, "acme", renameNewRef)
	if len(r.sm.deleteCalls) != 0 || !r.hasLegacyBytes(t, "acme") {
		t.Fatalf("previous copies deleted under an open cycle: %d SM-API deletes, legacy bytes %v",
			len(r.sm.deleteCalls), r.hasLegacyBytes(t, "acme"))
	}

	r.cycles["acme"] = false
	r.pass(t)
	if len(r.sm.deleteCalls) != 1 || r.hasLegacyBytes(t, "acme") {
		t.Fatalf("after the cycle ended: %d SM-API deletes, legacy bytes %v; want both copies gone",
			len(r.sm.deleteCalls), r.hasLegacyBytes(t, "acme"))
	}
	if len(r.sm.createCalls) != 1 {
		t.Fatalf("uploads = %d, want the one move", len(r.sm.createCalls))
	}
}

// Interrupted between copy and switch: the bytes are copied, the row stays on
// its previous copy (which still exists), and the next pass finishes.
func TestModelKeyRename_AFailedUploadConvergesOnTheNextPass_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	const key = "sk-ant-api03-acme-key-bytes-0001"
	r.seedLegacyOrg(t, "acme", key, true)
	r.sm.createErr = errors.New("sm-api: 503")

	r.pass(t)
	r.wantKey(t, "acme", key)
	r.wantDispatchRef(t, "acme", renameLegacyRef)
	if len(r.sm.deleteCalls) != 0 || !r.hasLegacyBytes(t, "acme") {
		t.Fatal("previous copies deleted while the row still names them")
	}

	r.sm.createErr = nil
	r.pass(t)
	r.wantKey(t, "acme", key)
	r.wantDispatchRef(t, "acme", renameNewRef)
	if len(r.sm.deleteCalls) != 1 || r.hasLegacyBytes(t, "acme") {
		t.Fatal("the next pass did not retire the previous copies")
	}
}

// Interrupted between switch and delete: the row is on the new copy, the old
// copies linger, and the next pass deletes them without moving again.
func TestModelKeyRename_AFailedDeleteConvergesOnTheNextPass_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	r.seedLegacyOrg(t, "acme", "sk-ant-api03-acme-key-bytes-0001", true)
	r.sm.deleteErr = errors.New("sm-api: 500")

	r.pass(t)
	r.wantDispatchRef(t, "acme", renameNewRef)
	if !r.hasLegacyBytes(t, "acme") {
		t.Fatal("legacy bytes deleted although the previous SM-API copy was not")
	}

	r.sm.deleteErr = nil
	r.pass(t)
	if r.hasLegacyBytes(t, "acme") {
		t.Fatal("the next pass left the legacy bytes")
	}
	if len(r.sm.createCalls) != 1 || len(r.sm.deleteCalls) != 2 {
		t.Fatalf("SM-API calls: %d uploads, %d deletes; want 1 upload, the failed delete and its retry",
			len(r.sm.createCalls), len(r.sm.deleteCalls))
	}
}

// A replica of the previous release saved a key mid-rollout: it wrote the old
// name and stamped the old entity. The next pass copies the newer bytes and
// moves the mirror again.
func TestModelKeyRename_AKeySavedUnderTheOldNameIsCopiedAgain_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	r.seedLegacyOrg(t, "acme", "sk-ant-api03-acme-key-bytes-0001", true)
	r.pass(t)

	const rotated = "sk-ant-api03-acme-rotated-key-0002"
	if err := r.store.Put(context.Background(), "acme", "anthropic/key", []byte(rotated)); err != nil {
		t.Fatalf("old-release save: %v", err)
	}
	if err := r.db.Exec(`UPDATE org_model_connections SET secret_ref_name = ? WHERE oc_org_id = 'acme'`, renameLegacyRef).Error; err != nil {
		t.Fatalf("old-release stamp: %v", err)
	}

	r.pass(t)
	r.wantKey(t, "acme", rotated)
	r.wantDispatchRef(t, "acme", renameNewRef)
	if last := r.sm.createCalls[len(r.sm.createCalls)-1]; last.data["api-key"] != rotated {
		t.Fatal("the new copy was not rewritten with the rotated key")
	}
	if r.hasLegacyBytes(t, "acme") {
		t.Fatal("legacy bytes left after the second move")
	}
}

// A key saved under `model/key` after the old one is never overwritten by it;
// the stale old bytes are simply retired.
func TestModelKeyRename_NeverOverwritesANewerKey_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	r.seedLegacyOrg(t, "acme", "sk-ant-api03-acme-stale-key-0001", true)
	const saved = "sk-ant-api03-acme-saved-key-00002"
	if err := r.db.Exec(`UPDATE org_secrets SET updated_at = now() - interval '1 hour' WHERE oc_org_id = 'acme'`).Error; err != nil {
		t.Fatalf("age the legacy bytes: %v", err)
	}
	// A save on this release: new bytes, triplet cleared until its mirror stamps.
	if err := r.store.Put(context.Background(), "acme", "model/key", []byte(saved)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := r.db.Exec(`UPDATE org_model_connections SET secret_ref_name = NULL, secret_ref_kv_path = NULL WHERE oc_org_id = 'acme'`).Error; err != nil {
		t.Fatalf("clear triplet: %v", err)
	}

	r.pass(t)
	r.wantKey(t, "acme", saved)
	if len(r.sm.createCalls) != 0 {
		t.Fatal("the rename uploaded over a save's own mirror")
	}
	if r.hasLegacyBytes(t, "acme") {
		t.Fatal("stale legacy bytes kept")
	}
}

// Without the org's ouId SM-API cannot be reached: the bytes still move, the
// row and the previous copies stay put.
func TestModelKeyRename_WithoutAnOuIDOnlyTheBytesMove_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	const key = "sk-ant-api03-acme-key-bytes-0001"
	r.seedLegacyOrg(t, "acme", key, false)

	r.pass(t)
	r.wantKey(t, "acme", key)
	r.wantDispatchRef(t, "acme", renameLegacyRef)
	if len(r.sm.createCalls) != 0 || len(r.sm.deleteCalls) != 0 || !r.hasLegacyBytes(t, "acme") {
		t.Fatal("SM-API reached, or previous copies deleted, without an ouId")
	}
}

// An org holding `anthropic/key` with no connection row (a credential phase19
// carried no connection for) has its previous copies retired, and the key is
// never copied under `model/key`.
func TestModelKeyRename_RetiresADisconnectedOrgsPreviousCopies_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	r.seedLegacyOrg(t, "acme", "sk-ant-api03-acme-key-bytes-0001", true)
	if err := r.db.Exec(`DELETE FROM org_model_connections WHERE oc_org_id = 'acme'`).Error; err != nil {
		t.Fatalf("disconnect: %v", err)
	}

	r.pass(t)
	if len(r.sm.createCalls) != 0 || len(r.sm.deleteCalls) != 1 || r.hasLegacyBytes(t, "acme") {
		t.Fatalf("disconnected org: %d uploads, %d deletes, legacy bytes %v; want only the old copies deleted",
			len(r.sm.createCalls), len(r.sm.deleteCalls), r.hasLegacyBytes(t, "acme"))
	}
	if _, err := r.store.Get(context.Background(), "acme", "model/key"); !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("a disconnected key was copied back under model/key (%v)", err)
	}
}

// A row stamped with a reference neither copy has is left alone: its target
// may be the old copy, so nothing is deleted.
func TestModelKeyRename_KeepsThePreviousCopiesUnderAnUnrecognizedReference_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	const key = "sk-ant-api03-acme-key-bytes-0001"
	r.seedLegacyOrg(t, "acme", key, true)
	if err := r.db.Exec(`UPDATE org_model_connections SET secret_ref_name = 'acme-custom-ref' WHERE oc_org_id = 'acme'`).Error; err != nil {
		t.Fatalf("stamp an unrecognized reference: %v", err)
	}

	r.pass(t)
	r.wantKey(t, "acme", key)
	if len(r.sm.createCalls) != 0 || len(r.sm.deleteCalls) != 0 || !r.hasLegacyBytes(t, "acme") {
		t.Fatalf("unrecognized reference: %d uploads, %d deletes, legacy bytes %v; want nothing touched",
			len(r.sm.createCalls), len(r.sm.deleteCalls), r.hasLegacyBytes(t, "acme"))
	}
}

// A card save that wrote the key's default-key reference before the rename
// ran leaves the row on a fresh name that row records: that is the new
// copy, so nothing is uploaded and the Anthropic-era copies are retired.
func TestModelKeyRename_ARecordedDefaultKeyReferenceIsTheNewCopy_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	const key = "sk-ant-api03-acme-key-bytes-0001"
	r.seedLegacyOrg(t, "acme", key, true)
	const minted = "acme-default-key-0000aaaa"
	if err := r.db.Exec(`UPDATE org_model_connections SET secret_ref_name = ?, secret_ref_kv_path = replace(secret_ref_kv_path, ?, ?) WHERE oc_org_id = 'acme'`, minted, renameLegacyRef, minted).Error; err != nil {
		t.Fatalf("stamp the save's reference: %v", err)
	}
	if err := organization.NewOrgSecretRepository(r.db).Upsert(context.Background(), "acme",
		organization.OrgSecretRef{Secret: organization.OrgSecretDefaultKey, Name: minted}, ""); err != nil {
		t.Fatalf("record the save's reference: %v", err)
	}

	r.pass(t)
	if len(r.sm.createCalls) != 0 || r.hasLegacyBytes(t, "acme") || len(r.sm.deleteCalls) != 1 ||
		r.sm.deleteCalls[0].secretRefName != renameLegacyRef {
		t.Fatalf("recorded reference: %d uploads, deletes %+v, legacy bytes %v; want the old copies retired",
			len(r.sm.createCalls), r.sm.deleteCalls, r.hasLegacyBytes(t, "acme"))
	}
	r.wantDispatchRef(t, "acme", minted)
}

// The boot pass moves and switches but deletes nothing, so a replica of the
// previous release still draining can read `anthropic/key`; the next periodic
// pass retires the old copies.
func TestModelKeyRename_TheBootPassDeletesNothing_DB(t *testing.T) {
	t.Parallel()
	r := newRenameDB(t)
	const key = "sk-ant-api03-acme-key-bytes-0001"
	r.seedLegacyOrg(t, "acme", key, true)

	if err := r.rename.BootPass(context.Background()); err != nil {
		t.Fatalf("boot pass: %v", err)
	}
	r.wantKey(t, "acme", key)
	r.wantDispatchRef(t, "acme", renameNewRef)
	if len(r.sm.createCalls) != 1 || len(r.sm.deleteCalls) != 0 || !r.hasLegacyBytes(t, "acme") {
		t.Fatalf("boot pass: %d uploads, %d deletes, legacy bytes %v; want the move and no delete",
			len(r.sm.createCalls), len(r.sm.deleteCalls), r.hasLegacyBytes(t, "acme"))
	}

	r.pass(t)
	if len(r.sm.createCalls) != 1 || len(r.sm.deleteCalls) != 1 || r.hasLegacyBytes(t, "acme") {
		t.Fatalf("periodic pass: %d uploads, %d deletes, legacy bytes %v; want the old copies retired, no second move",
			len(r.sm.createCalls), len(r.sm.deleteCalls), r.hasLegacyBytes(t, "acme"))
	}
}
