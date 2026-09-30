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

// DBTEST tier (skips under -short; `make test-db` runs it): the org's SRE
// model connection — OrgSreModelConnectionRepository's plain read and
// AgentsCardTx's upsert/delete — over a pristine per-test Postgres
// (dbtest.New): absent is nil-not-error, an upsert is readable back with the
// fixed OpenAI-compatible/Bearer shape, a second upsert replaces the row
// rather than erroring, and delete is idempotent. On top of it,
// SreModelConnectionService's Set → Projection round trip over the real
// store: the key is stored and previewed, never projected.
//
// External test package: an in-package dbtest file would be an import cycle
// (dbtest imports migrate, which imports organization), same as
// anthropic_dbtest_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// sreModelConnDBAESKey is the 32-byte AES-256 key for the real DBStore.
const sreModelConnDBAESKey = "0123456789abcdef0123456789abcdef"

func TestOrgSreModelConnectionRepository_GetByOrgAbsentReturnsNilNotError(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	repo := organization.NewOrgSreModelConnectionRepository(db)

	row, err := repo.GetByOrg(context.Background(), "acme")
	if err != nil {
		t.Fatalf("get on empty table: %v", err)
	}
	if row != nil {
		t.Fatalf("get on empty table: want nil row, got %+v", row)
	}
}

func TestAgentsCardTx_UpsertSreModelConnection_RoundTrip(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	store, err := secrets.NewDBStore(db, []byte(sreModelConnDBAESKey))
	if err != nil {
		t.Fatalf("real DBStore: %v", err)
	}
	card := organization.NewAgentsCardRepository(db, store)
	repo := organization.NewOrgSreModelConnectionRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	row := &organization.OrgSreModelConnection{
		OcOrgID:     "acme",
		BaseURL:     "https://gw.example.com/v1",
		Host:        "gw.example.com",
		Model:       "gpt-4o-mini",
		ConnectedAt: now,
		UpdatedAt:   now,
		UpdatedBy:   "user@acme.test",
	}
	if err := card.Tx(ctx, func(tx organization.AgentsCardTx) error {
		return tx.UpsertSreModelConnection(row)
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repo.GetByOrg(ctx, "acme")
	if err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if got == nil || got.BaseURL != row.BaseURL || got.Host != row.Host || got.Model != row.Model {
		t.Fatalf("got = %+v, want the upserted row", got)
	}
	wantConn := modelconn.Connection{
		Format:     modelconn.FormatOpenAICompatible,
		BaseURL:    "https://gw.example.com/v1",
		Host:       "gw.example.com",
		Model:      "gpt-4o-mini",
		AuthScheme: modelconn.AuthBearer,
	}
	if conn := got.Connection(); conn != wantConn {
		t.Fatalf("Connection() = %+v, want %+v", conn, wantConn)
	}

	// A second upsert replaces the row rather than erroring or duplicating it.
	later := now.Add(time.Hour)
	replacement := &organization.OrgSreModelConnection{
		OcOrgID:     "acme",
		BaseURL:     "https://gw2.example.com/v1",
		Host:        "gw2.example.com",
		Model:       "gpt-4o",
		ConnectedAt: later,
		UpdatedAt:   later,
		UpdatedBy:   "other@acme.test",
	}
	if err := card.Tx(ctx, func(tx organization.AgentsCardTx) error {
		return tx.UpsertSreModelConnection(replacement)
	}); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got, err = repo.GetByOrg(ctx, "acme")
	if err != nil {
		t.Fatalf("get after second upsert: %v", err)
	}
	if got == nil || got.Host != "gw2.example.com" || got.Model != "gpt-4o" {
		t.Fatalf("second upsert must replace the row, got %+v", got)
	}

	var count int64
	if err := db.Table("org_sre_model_connections").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("table must hold exactly one row for the org, got %d", count)
	}

	if err := card.Tx(ctx, func(tx organization.AgentsCardTx) error {
		return tx.DeleteSreModelConnection("acme")
	}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err = repo.GetByOrg(ctx, "acme")
	if err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("get after delete: want nil, got %+v", got)
	}
	// Idempotent.
	if err := card.Tx(ctx, func(tx organization.AgentsCardTx) error {
		return tx.DeleteSreModelConnection("acme")
	}); err != nil {
		t.Fatalf("second delete must be a no-op, got %v", err)
	}
}

func TestAgentsCardTx_UpsertSreModelConnection_OrgIsolation(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	store, err := secrets.NewDBStore(db, []byte(sreModelConnDBAESKey))
	if err != nil {
		t.Fatalf("real DBStore: %v", err)
	}
	card := organization.NewAgentsCardRepository(db, store)
	repo := organization.NewOrgSreModelConnectionRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	for _, org := range []string{"acme", "globex"} {
		row := &organization.OrgSreModelConnection{
			OcOrgID: org, BaseURL: "https://gw.example.com/v1", Host: "gw.example.com",
			Model: "gpt-4o-mini", ConnectedAt: now, UpdatedAt: now, UpdatedBy: "user@" + org,
		}
		if err := card.Tx(ctx, func(tx organization.AgentsCardTx) error {
			return tx.UpsertSreModelConnection(row)
		}); err != nil {
			t.Fatalf("upsert %s: %v", org, err)
		}
	}

	if err := card.Tx(ctx, func(tx organization.AgentsCardTx) error {
		return tx.DeleteSreModelConnection("acme")
	}); err != nil {
		t.Fatalf("delete acme: %v", err)
	}

	if got, err := repo.GetByOrg(ctx, "acme"); err != nil || got != nil {
		t.Fatalf("acme after its own delete: row=%+v err=%v, want nil,nil", got, err)
	}
	got, err := repo.GetByOrg(ctx, "globex")
	if err != nil || got == nil {
		t.Fatalf("globex must be untouched by acme's delete: row=%+v err=%v", got, err)
	}
}

// orgWithoutConnection is a ConnectionReader for an org with no model
// connection: EffectiveSRE then has only the SRE model connection to go on.
type orgWithoutConnection struct{}

func (orgWithoutConnection) Effective(context.Context, string) (modelconn.Connection, string, bool, error) {
	return modelconn.Connection{}, "", false, nil
}

func (orgWithoutConnection) KeyRef(context.Context, string) (modelconn.Connection, organization.SecretRefTriplet, error) {
	return modelconn.Connection{}, organization.SecretRefTriplet{}, &organization.NotFoundError{What: "org_model_connections"}
}

func TestSreModelConnectionService_SetThenProjection(t *testing.T) {
	t.Parallel()
	db := dbtest.New(t)
	store, err := secrets.NewDBStore(db, []byte(sreModelConnDBAESKey))
	if err != nil {
		t.Fatalf("real DBStore: %v", err)
	}
	endpoint := newModelEndpoint(t, http.StatusOK)
	svc := organization.NewSreModelConnectionService(organization.NewOrgSreModelConnectionRepository(db), store,
		organization.NewAgentsCardRepository(db, store), orgWithoutConnection{}).WithProbeClient(endpoint.client())
	ctx := context.Background()
	key, baseURL, model := "sre-db-key-0123456789abcdef", "https://gw.example.com/v1", "glm-5.3"

	if err := svc.Set(ctx, "acme", "user@acme.test", orgconfig.SreLlmWrite{BaseURL: &baseURL, APIKey: &key, Model: &model}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	proj, err := svc.Projection(ctx, "acme")
	if err != nil {
		t.Fatalf("Projection: %v", err)
	}
	if proj == nil || proj.BaseURL != baseURL || proj.Host != "gw.example.com" || proj.Model != model || proj.UpdatedBy != "user@acme.test" {
		t.Fatalf("Projection = %+v, want the saved connection", proj)
	}
	if proj.KeyPreview != "sre-…cdef" {
		t.Errorf("keyPreview = %q, want %q", proj.KeyPreview, "sre-…cdef")
	}
	raw, err := json.Marshal(proj)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), key) {
		t.Fatalf("the projection carries the key: %s", raw)
	}

	stored, err := store.Get(ctx, "acme", "sre-model/key")
	if err != nil || string(stored) != key {
		t.Fatalf("stored key: err=%v match=%v, want the saved key", err, string(stored) == key)
	}
	eff, err := svc.EffectiveSRE(ctx, "acme")
	if err != nil {
		t.Fatalf("EffectiveSRE: %v", err)
	}
	if eff.Source != organization.SRESourceOverride || eff.Conn.Host != "gw.example.com" || eff.Key != key {
		t.Errorf("EffectiveSRE = {%s %s}, want the override with its key", eff.Source, eff.Conn.Host)
	}

	if err := svc.Clear(ctx, "acme", "user@acme.test"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if proj, err := svc.Projection(ctx, "acme"); err != nil || proj != nil {
		t.Fatalf("Projection after Clear = %+v, %v; want nil, nil", proj, err)
	}
	if _, err := store.Get(ctx, "acme", "sre-model/key"); err == nil {
		t.Fatal("the key outlived Clear")
	}
}
