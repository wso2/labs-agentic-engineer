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

// DBTEST tier (skips under -short; `make test-db` runs it): the REAL
// AnthropicCredentialService over a pristine per-test Postgres (dbtest.New)
// with the REAL AES-256-GCM secrets.NewDBStore — the SQL-shaped behavior
// under pin: Connect's advisory-lock + ON CONFLICT upsert, the org_secrets
// write, Status/fetchRow org scoping, Disconnect's delete + best-effort GC,
// EffectiveKey resolution and the Connect/Disconnect cascade. The service
// writes nothing into any cluster: the runner reads the org key through an
// ExternalSecret rendered against the SM-API-mirrored path.
//
// External test package: anthropic_service_test.go (unit tier, package
// organization) imports dbtest, which imports migrate, which imports
// organization — an in-package dbtest file would be an import cycle.

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// anthropicDBAESKey is the 32-byte AES-256 key for the real DBStore.
const anthropicDBAESKey = "0123456789abcdef0123456789abcdef"

// anthropicDBKey2 is a second well-formed key for the replace/isolation pins.
const anthropicDBKey2 = "sk-ant-api03-ZyXwVuTsRqPoNmLkJiHgFe-second-9876"

// anthropicDBService wires the real service: per-test Postgres + the real
// encrypted DBStore + a fake Anthropic API answering apiStatus.
func anthropicDBService(t *testing.T, apiStatus int) (*organization.AnthropicCredentialService, secrets.CredentialStore) {
	t.Helper()
	db := dbtest.New(t)
	store, err := secrets.NewDBStore(db, []byte(anthropicDBAESKey))
	if err != nil {
		t.Fatalf("real DBStore: %v", err)
	}
	base, _ := anthropicFakeAPI(t, apiStatus)
	return organization.NewAnthropicCredentialService(organization.NewOrgAnthropicRepository(db), store).WithAnthropicAPIBase(base), store
}

// anthropicMustConnect connects key for org or fails the test.
func anthropicMustConnect(t *testing.T, svc *organization.AnthropicCredentialService, org, key string) *organization.AnthropicProjection {
	t.Helper()
	proj, err := svc.Connect(context.Background(), org, organization.AnthropicRoleDefault, organization.AnthropicConnectRequest{APIKey: key})
	if err != nil {
		t.Fatalf("connect %s: %v", org, err)
	}
	return proj
}

func TestAnthropicConnect_HappyPath_DB(t *testing.T) {
	t.Parallel()
	svc, store := anthropicDBService(t, http.StatusOK)
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Second)

	// The key arrives padded — Connect must trim before shape-check + store.
	proj, err := svc.Connect(ctx, "acme", organization.AnthropicRoleDefault, organization.AnthropicConnectRequest{APIKey: "  " + anthropicUnitKey + "\n"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if proj.OcOrgID != "acme" || proj.Status != "active" {
		t.Fatalf("projection identity: %+v", proj)
	}
	if proj.KeyPrefix != anthropicUnitKey[:15] || proj.KeyLast4 != anthropicUnitKey[len(anthropicUnitKey)-4:] {
		t.Fatalf("preview: got (%q,%q)", proj.KeyPrefix, proj.KeyLast4)
	}
	if proj.ConnectedAt.Before(start) || proj.LastValidatedAt == nil || proj.ValidationError != nil {
		t.Fatalf("timestamps/validation drifted: %+v", proj)
	}

	// The TRIMMED key round-trips through the real AES-GCM org_secrets store.
	got, err := store.Get(ctx, "acme", "anthropic/key")
	if err != nil {
		t.Fatalf("store get: %v", err)
	}
	if string(got) != anthropicUnitKey {
		t.Fatalf("stored key: got %q, want the trimmed key", string(got))
	}

	// Status serves the persisted row with the same projection field values.
	st, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.KeyPrefix != proj.KeyPrefix || st.KeyLast4 != proj.KeyLast4 || st.Status != "active" {
		t.Fatalf("status round-trip drifted: %+v", st)
	}
}

func TestAnthropicConnect_RejectedKeyLeavesNoTrace_DB(t *testing.T) {
	t.Parallel()
	svc, store := anthropicDBService(t, http.StatusUnauthorized)
	ctx := context.Background()

	_, err := svc.Connect(ctx, "acme", organization.AnthropicRoleDefault, organization.AnthropicConnectRequest{APIKey: anthropicUnitKey})
	if got := anthropicValidationCode(t, err); got != "anthropic_key_invalid" {
		t.Fatalf("code: got %q, want anthropic_key_invalid", got)
	}

	// No metadata row…
	var nfe *organization.NotFoundError
	if _, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault); !errors.As(err, &nfe) {
		t.Fatalf("status after rejected connect: want NotFoundError, got %v", err)
	}
	// …and no secret bytes.
	if _, err := store.Get(ctx, "acme", "anthropic/key"); !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("store after rejected connect: want ErrSecretNotFound, got %v", err)
	}
}

func TestAnthropicConnect_ReplaceUpserts_DB(t *testing.T) {
	t.Parallel()
	svc, store := anthropicDBService(t, http.StatusOK)
	ctx := context.Background()

	anthropicMustConnect(t, svc, "acme", anthropicUnitKey)
	st1, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault)
	if err != nil {
		t.Fatalf("status after first connect: %v", err)
	}

	time.Sleep(25 * time.Millisecond) // make the two connects' clocks distinguishable
	proj2 := anthropicMustConnect(t, svc, "acme", anthropicDBKey2)

	// One row, now carrying key2's preview; the stored bytes are key2.
	st2, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault)
	if err != nil {
		t.Fatalf("status after replace: %v", err)
	}
	if st2.KeyPrefix != anthropicDBKey2[:15] || st2.KeyLast4 != anthropicDBKey2[len(anthropicDBKey2)-4:] || st2.Status != "active" {
		t.Fatalf("replaced row: %+v", st2)
	}
	got, err := store.Get(ctx, "acme", "anthropic/key")
	if err != nil || string(got) != anthropicDBKey2 {
		t.Fatalf("stored key after replace: got %q err %v, want key2", string(got), err)
	}

	// The upsert refreshes last_validated_at but PRESERVES connected_at —
	// the row remembers the original connection time across replaces.
	if !st2.ConnectedAt.Equal(st1.ConnectedAt) {
		t.Fatalf("connectedAt must survive a replace: first %v, after %v", st1.ConnectedAt, st2.ConnectedAt)
	}
	if st1.LastValidatedAt == nil || st2.LastValidatedAt == nil || !st2.LastValidatedAt.After(*st1.LastValidatedAt) {
		t.Fatalf("lastValidatedAt must be refreshed by a replace: first %v, after %v", st1.LastValidatedAt, st2.LastValidatedAt)
	}
	// The replacing Connect now RETURNS the persisted connected_at (the
	// original connection time the upsert preserves), not an in-memory now —
	// so the returned projection agrees with the stored row.
	if !proj2.ConnectedAt.Equal(st1.ConnectedAt) {
		t.Fatalf("replacing Connect must return the preserved connected_at: got %v, want %v (== original)", proj2.ConnectedAt, st1.ConnectedAt)
	}
}

func TestAnthropicStatus_AbsentOrgIsNotFound_DB(t *testing.T) {
	t.Parallel()
	svc, _ := anthropicDBService(t, http.StatusOK)

	_, err := svc.Status(context.Background(), "acme", organization.AnthropicRoleDefault)
	var nfe *organization.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("want *organization.NotFoundError, got %T: %v", err, err)
	}
	// Role-qualified: with two possible rows per org, "which one is missing"
	// is the part that makes the error actionable.
	if nfe.What != "org_anthropic_credentials.acme.default" {
		t.Fatalf("NotFoundError.What: got %q", nfe.What)
	}
}

func TestAnthropicDisconnect_RemovesRowAndBytes_Idempotent_DB(t *testing.T) {
	t.Parallel()
	svc, store := anthropicDBService(t, http.StatusOK)
	ctx := context.Background()
	anthropicMustConnect(t, svc, "acme", anthropicUnitKey)

	if err := svc.Disconnect(ctx, "acme", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	var nfe *organization.NotFoundError
	if _, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault); !errors.As(err, &nfe) {
		t.Fatalf("status after disconnect: want NotFoundError, got %v", err)
	}
	if _, err := store.Get(ctx, "acme", "anthropic/key"); !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("secret bytes must be GC'd on disconnect, got %v", err)
	}

	// Disconnecting an org with no row is a clean no-op (the edge serves the
	// same success body).
	if err := svc.Disconnect(ctx, "acme", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("second disconnect must be idempotent, got %v", err)
	}
}

func TestAnthropicEffectiveKey_DB(t *testing.T) {
	t.Parallel()
	svc, store := anthropicDBService(t, http.StatusOK)
	ctx := context.Background()

	// No org row → the "none" answer, NOT an error (agents-service maps it).
	res, err := svc.EffectiveKey(ctx, "acme")
	if err != nil || res.Source != "none" || res.Key != "" {
		t.Fatalf("absent org: got %+v err %v, want source none", res, err)
	}

	// Connected → source "org" + the decrypted key bytes.
	anthropicMustConnect(t, svc, "acme", anthropicUnitKey)
	res, err = svc.EffectiveKey(ctx, "acme")
	if err != nil || res.Source != "org" || res.Key != anthropicUnitKey {
		t.Fatalf("connected: got %+v err %v, want the org key", res, err)
	}

	// Row says active but the bytes vanished → degrades to "none" (logged
	// loudly in the service), still not an error.
	if err := store.Delete(ctx, "acme", "anthropic/key"); err != nil {
		t.Fatalf("store delete: %v", err)
	}
	res, err = svc.EffectiveKey(ctx, "acme")
	if err != nil || res.Source != "none" || res.Key != "" {
		t.Fatalf("active row without bytes: got %+v err %v, want source none", res, err)
	}
}

func TestAnthropicOrgIsolation_DB(t *testing.T) {
	t.Parallel()
	svc, store := anthropicDBService(t, http.StatusOK)
	ctx := context.Background()

	anthropicMustConnect(t, svc, "acme", anthropicUnitKey)
	anthropicMustConnect(t, svc, "globex", anthropicDBKey2)

	// Each org's row + secret resolve to its OWN key — fetchRow and the
	// org_secrets lookups are (oc_org_id)-scoped.
	stA, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault)
	if err != nil || stA.KeyLast4 != anthropicUnitKey[len(anthropicUnitKey)-4:] {
		t.Fatalf("acme status: %+v err %v", stA, err)
	}
	stB, err := svc.Status(ctx, "globex", organization.AnthropicRoleDefault)
	if err != nil || stB.KeyLast4 != anthropicDBKey2[len(anthropicDBKey2)-4:] {
		t.Fatalf("globex status: %+v err %v", stB, err)
	}
	resA, err := svc.EffectiveKey(ctx, "acme")
	if err != nil || resA.Key != anthropicUnitKey {
		t.Fatalf("acme effective key: %+v err %v", resA, err)
	}
	resB, err := svc.EffectiveKey(ctx, "globex")
	if err != nil || resB.Key != anthropicDBKey2 {
		t.Fatalf("globex effective key: %+v err %v", resB, err)
	}

	// A third org sharing the table sees nothing.
	var nfe *organization.NotFoundError
	if _, err := svc.Status(ctx, "intruder", organization.AnthropicRoleDefault); !errors.As(err, &nfe) {
		t.Fatalf("intruder must get NotFound, got %v", err)
	}

	// Disconnecting one org must not touch the other's row or bytes.
	if err := svc.Disconnect(ctx, "acme", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("disconnect acme: %v", err)
	}
	if _, err := svc.Status(ctx, "globex", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("globex must survive acme's disconnect: %v", err)
	}
	if got, err := store.Get(ctx, "globex", "anthropic/key"); err != nil || string(got) != anthropicDBKey2 {
		t.Fatalf("globex bytes must survive acme's disconnect: %q err %v", string(got), err)
	}
}

func TestAnthropicDisconnect_NoWorkflowPlaneSecret_DB(t *testing.T) {
	t.Parallel()
	svc, store := anthropicDBService(t, http.StatusOK)
	ctx := context.Background()

	if _, err := svc.Connect(ctx, "acme", organization.AnthropicRoleDefault, organization.AnthropicConnectRequest{APIKey: anthropicUnitKey}); err != nil {
		t.Fatalf("Connect = %v", err)
	}
	if err := svc.Disconnect(ctx, "acme", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("Disconnect = %v", err)
	}
	if _, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault); err == nil {
		t.Fatal("Status after Disconnect must report the row is gone")
	}
	if b, err := store.Get(ctx, "acme", "anthropic/key"); err == nil && len(b) > 0 {
		t.Fatal("Disconnect left the encrypted key bytes behind")
	}
	// Idempotent: a second Disconnect is a no-op, not an error.
	if err := svc.Disconnect(ctx, "acme", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("second Disconnect = %v, want nil", err)
	}
}

func TestAnthropicResyncSecretRef_NoopCases_DB(t *testing.T) {
	t.Parallel()
	svc, _ := anthropicDBService(t, http.StatusOK)
	ctx := context.Background()

	// No row → idempotent (false, nil).
	if wrote, err := svc.ResyncSecretRef(ctx, "acme"); wrote || err != nil {
		t.Fatalf("absent org: want (false,nil), got (%v,%v)", wrote, err)
	}

	// Active row but the secret-ref triplet was never populated (no writer
	// wired) → still (false, nil).
	anthropicMustConnect(t, svc, "acme", anthropicUnitKey)
	if wrote, err := svc.ResyncSecretRef(ctx, "acme"); wrote || err != nil {
		t.Fatalf("no triplet: want (false,nil), got (%v,%v)", wrote, err)
	}
}
