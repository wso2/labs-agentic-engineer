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
// AnthropicCredentialService and the AI agents card that writes its rows, over
// a pristine per-test Postgres (dbtest.New) with the REAL AES-256-GCM
// secrets.NewDBStore — the SQL-shaped behavior under pin: the card's upsert and
// org_secrets write, Status org scoping, the disconnect's delete, EffectiveKey
// resolution and org isolation. Nothing here writes into any cluster.
//
// Writes go through organization.Service.Patch, the one path that writes a
// credential, so the tests pin what production does.
//
// External test package: dbtest imports migrate, which imports organization —
// an in-package dbtest file would be an import cycle.

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// anthropicDBAESKey is the 32-byte AES-256 key for the real DBStore.
const anthropicDBAESKey = "0123456789abcdef0123456789abcdef"

// anthropicDBKey2 is a second well-formed key for the replace/isolation pins.
const anthropicDBKey2 = "sk-ant-api03-ZyXwVuTsRqPoNmLkJiHgFe-second-9876"

// cardDB is the real credential service, the card that writes it and the
// /config orchestrator over one per-test Postgres.
type cardDB struct {
	db     *gorm.DB
	svc    *organization.AnthropicCredentialService
	config *organization.Service
	store  secrets.CredentialStore
	repo   organization.OrgAnthropicRepository
}

// newCardDB wires cardDB with a fake Anthropic API answering apiStatus.
func newCardDB(t *testing.T, apiStatus int) *cardDB {
	t.Helper()
	db := dbtest.New(t)
	store, err := secrets.NewDBStore(db, []byte(anthropicDBAESKey))
	if err != nil {
		t.Fatalf("real DBStore: %v", err)
	}
	base, _ := anthropicFakeAPI(t, apiStatus)
	repo := organization.NewOrgAnthropicRepository(db)
	svc := organization.NewAnthropicCredentialService(repo, store).WithAnthropicAPIBase(base)
	settings := organization.NewAgentSettingsService(organization.NewOrgAgentSettingsRepository(db),
		organization.NewOrganizationRepository(db), svc, organization.NewAgentsCardRepository(db, store))
	config := organization.NewService(svc, nil, nil, nil, nil, organization.PlatformIDPConfig{}, "", "").
		WithAgentSettings(settings)
	return &cardDB{db: db, svc: svc, config: config, store: store, repo: repo}
}

func llmPatch(key string) orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{LLM: patch.Field[orgconfig.LLMWrite]{Sent: true, Value: orgconfig.LLMWrite{Kind: "anthropic", APIKey: key}}}
}

func disconnectPatch() orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{LLM: patch.Field[orgconfig.LLMWrite]{Sent: true, Null: true}}
}

func subscriptionPatch(token string) orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{Agents: patch.Field[orgconfig.AgentsWrite]{Sent: true, Value: orgconfig.AgentsWrite{
		Subscription: patch.Field[orgconfig.SubscriptionWrite]{Sent: true, Value: orgconfig.SubscriptionWrite{Kind: "claude", Token: token}},
	}}}
}

func (c *cardDB) patch(t *testing.T, org string, p orgconfig.ConfigPatch) {
	t.Helper()
	if _, err := c.config.Patch(context.Background(), org, "ada", p); err != nil {
		t.Fatalf("patch %s: %v", org, err)
	}
}

func (c *cardDB) connect(t *testing.T, org, key string) *organization.AnthropicProjection {
	t.Helper()
	c.patch(t, org, llmPatch(key))
	st, err := c.svc.Status(context.Background(), org, organization.AnthropicRoleDefault)
	if err != nil {
		t.Fatalf("status after connect %s: %v", org, err)
	}
	return st
}

// cardErrCode unwraps the refusal slug of a /config patch error.
func cardErrCode(t *testing.T, err error) string {
	t.Helper()
	var se *organization.SectionError
	if !errors.As(err, &se) {
		t.Fatalf("want *organization.SectionError, got %#v", err)
	}
	return se.Code
}

func TestAnthropicConnect_HappyPath_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Second)

	// The key arrives padded — the card must trim before shape-check + store.
	proj := c.connect(t, "acme", "  "+anthropicUnitKey+"\n")
	if proj.OcOrgID != "acme" || proj.Status != "active" || proj.CredentialKind != organization.AnthropicCredentialAPIKey {
		t.Fatalf("projection identity: %+v", proj)
	}
	if proj.KeyPrefix != anthropicUnitKey[:15] || proj.KeyLast4 != anthropicUnitKey[len(anthropicUnitKey)-4:] {
		t.Fatalf("preview: got (%q,%q)", proj.KeyPrefix, proj.KeyLast4)
	}
	if proj.ConnectedAt.Before(start) || proj.LastValidatedAt == nil || proj.ValidationError != nil {
		t.Fatalf("timestamps/validation drifted: %+v", proj)
	}
	// The TRIMMED key round-trips through the real AES-GCM org_secrets store.
	got, err := c.store.Get(ctx, "acme", "anthropic/key")
	if err != nil || string(got) != anthropicUnitKey {
		t.Fatalf("stored key: got %q (%v), want the trimmed key", string(got), err)
	}
}

func TestAnthropicConnect_RejectedKeyLeavesNoTrace_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusUnauthorized)
	ctx := context.Background()

	_, err := c.config.Patch(ctx, "acme", "ada", llmPatch(anthropicUnitKey))
	if got := cardErrCode(t, err); got != "anthropic_key_invalid" {
		t.Fatalf("code: got %q, want anthropic_key_invalid", got)
	}
	var nfe *organization.NotFoundError
	if _, err := c.svc.Status(ctx, "acme", organization.AnthropicRoleDefault); !errors.As(err, &nfe) {
		t.Fatalf("status after rejected connect: want NotFoundError, got %v", err)
	}
	if _, err := c.store.Get(ctx, "acme", "anthropic/key"); !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("store after rejected connect: want ErrSecretNotFound, got %v", err)
	}
}

func TestAnthropicConnect_ReplaceUpserts_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	st1 := c.connect(t, "acme", anthropicUnitKey)
	time.Sleep(25 * time.Millisecond) // make the two saves' clocks distinguishable
	st2 := c.connect(t, "acme", anthropicDBKey2)

	if st2.KeyPrefix != anthropicDBKey2[:15] || st2.KeyLast4 != anthropicDBKey2[len(anthropicDBKey2)-4:] || st2.Status != "active" {
		t.Fatalf("replaced row: %+v", st2)
	}
	got, err := c.store.Get(ctx, "acme", "anthropic/key")
	if err != nil || string(got) != anthropicDBKey2 {
		t.Fatalf("stored key after replace: got %q err %v, want key2", string(got), err)
	}
	// The upsert refreshes last_validated_at but PRESERVES connected_at.
	if !st2.ConnectedAt.Equal(st1.ConnectedAt) {
		t.Fatalf("connectedAt must survive a replace: first %v, after %v", st1.ConnectedAt, st2.ConnectedAt)
	}
	if st1.LastValidatedAt == nil || st2.LastValidatedAt == nil || !st2.LastValidatedAt.After(*st1.LastValidatedAt) {
		t.Fatalf("lastValidatedAt must be refreshed by a replace: first %v, after %v", st1.LastValidatedAt, st2.LastValidatedAt)
	}
}

func TestAnthropicStatus_AbsentOrgIsNotFound_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)

	_, err := c.svc.Status(context.Background(), "acme", organization.AnthropicRoleDefault)
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
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()
	c.connect(t, "acme", anthropicUnitKey)

	c.patch(t, "acme", disconnectPatch())
	var nfe *organization.NotFoundError
	if _, err := c.svc.Status(ctx, "acme", organization.AnthropicRoleDefault); !errors.As(err, &nfe) {
		t.Fatalf("status after disconnect: want NotFoundError, got %v", err)
	}
	if _, err := c.store.Get(ctx, "acme", "anthropic/key"); !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("secret bytes must go with the row, got %v", err)
	}
	// Disconnecting an org with no key is a clean no-op.
	c.patch(t, "acme", disconnectPatch())
}

func TestAnthropicEffectiveKey_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	// No row → the "none" answer, NOT an error (the turn surface maps it).
	res, err := c.svc.EffectiveKey(ctx, "acme")
	if err != nil || res.Source != "none" || res.Key != "" {
		t.Fatalf("absent org: got %+v err %v, want source none", res, err)
	}
	c.connect(t, "acme", anthropicUnitKey)
	res, err = c.svc.EffectiveKey(ctx, "acme")
	if err != nil || res.Source != "org" || res.Key != anthropicUnitKey {
		t.Fatalf("connected: got %+v err %v, want the org key", res, err)
	}
	// Row says active but the bytes vanished → degrades to "none".
	if err := c.store.Delete(ctx, "acme", "anthropic/key"); err != nil {
		t.Fatalf("store delete: %v", err)
	}
	res, err = c.svc.EffectiveKey(ctx, "acme")
	if err != nil || res.Source != "none" || res.Key != "" {
		t.Fatalf("active row without bytes: got %+v err %v, want source none", res, err)
	}
}

func TestAnthropicDefaultKeyRef_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	svc := c.svc
	ctx := context.Background()

	// No org row → NotFoundError, same "not connected yet" contract as
	// fetchRow's other callers — a consumer wiring model access must treat
	// this as skip, not fail.
	if _, err := svc.DefaultKeyRef(ctx, "acme"); !isAnthropicNotFound(err) {
		t.Fatalf("absent org: got %v, want *organization.NotFoundError", err)
	}

	// Connected and mirrored → the vault triplet, not the key's bytes. No
	// SecretRefWriter is wired here, so the mirror's columns are stamped the
	// way the ResolveCodingSecretRef tests stamp them.
	c.connect(t, "acme", anthropicUnitKey)
	stampTriplet(t, c.repo, "acme", organization.AnthropicRoleDefault,
		"acme-anthropic", "user-app-secrets/wc-acme/acme-anthropic", "api-key")
	triplet, err := svc.DefaultKeyRef(ctx, "acme")
	if err != nil {
		t.Fatalf("DefaultKeyRef after connect: %v", err)
	}
	if triplet.KVPath != "user-app-secrets/wc-acme/acme-anthropic" || triplet.Property != "api-key" {
		t.Fatalf("DefaultKeyRef must return the stamped default triplet, got %+v", triplet)
	}
}

func isAnthropicNotFound(err error) bool {
	var nf *organization.NotFoundError
	return errors.As(err, &nf)
}

func TestAnthropicOrgIsolation_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	c.connect(t, "acme", anthropicUnitKey)
	c.connect(t, "globex", anthropicDBKey2)

	resA, err := c.svc.EffectiveKey(ctx, "acme")
	if err != nil || resA.Key != anthropicUnitKey {
		t.Fatalf("acme effective key: %+v err %v", resA, err)
	}
	resB, err := c.svc.EffectiveKey(ctx, "globex")
	if err != nil || resB.Key != anthropicDBKey2 {
		t.Fatalf("globex effective key: %+v err %v", resB, err)
	}
	var nfe *organization.NotFoundError
	if _, err := c.svc.Status(ctx, "intruder", organization.AnthropicRoleDefault); !errors.As(err, &nfe) {
		t.Fatalf("intruder must get NotFound, got %v", err)
	}
	// Disconnecting one org must not touch the other's row or bytes.
	c.patch(t, "acme", disconnectPatch())
	if _, err := c.svc.Status(ctx, "globex", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("globex must survive acme's disconnect: %v", err)
	}
	if got, err := c.store.Get(ctx, "globex", "anthropic/key"); err != nil || string(got) != anthropicDBKey2 {
		t.Fatalf("globex bytes must survive acme's disconnect: %q err %v", string(got), err)
	}
}

func TestAnthropicResyncSecretRef_NoopCases_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	if wrote, err := c.svc.ResyncSecretRef(ctx, "acme"); wrote || err != nil {
		t.Fatalf("absent org: want (false,nil), got (%v,%v)", wrote, err)
	}
	// Active row but no triplet (no writer wired) → still (false, nil).
	c.connect(t, "acme", anthropicUnitKey)
	if wrote, err := c.svc.ResyncSecretRef(ctx, "acme"); wrote || err != nil {
		t.Fatalf("no triplet: want (false,nil), got (%v,%v)", wrote, err)
	}
}

// --- rotation reaches the Agent Manager provider ------------------------------

type fakeModelProviderPublisher struct {
	published string
	calls     int
	err       error
}

func (f *fakeModelProviderPublisher) PublishOrgModelKey(_ context.Context, _, apiKey string) error {
	f.calls++
	f.published = apiKey
	return f.err
}

// A rotated key must reach the provider that holds a COPY of it. Without this,
// every governed agent in the org keeps calling Anthropic with a revoked
// credential, and the failure surfaces at the upstream rather than in Settings.
func TestAnthropicSave_PublishesTheKeyToTheModelProvider_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	pub := &fakeModelProviderPublisher{}
	c.svc.WithModelProvider(pub)

	c.connect(t, "acme", anthropicDBKey2)

	if pub.calls != 1 {
		t.Fatalf("publisher calls = %d, want 1", pub.calls)
	}
	if pub.published != anthropicDBKey2 {
		t.Errorf("published %q, want the newly saved key", pub.published)
	}
}

// A publisher failure must not fail the user's Settings action: the key IS
// stored, and the next governed deploy re-asserts it on the provider.
func TestAnthropicSave_SurvivesAPublisherFailure_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.svc.WithModelProvider(&fakeModelProviderPublisher{err: errors.New("amp unreachable")})

	if _, err := c.config.Patch(context.Background(), "acme", "ada", llmPatch(anthropicDBKey2)); err != nil {
		t.Fatalf("the save must succeed even when the provider push fails: %v", err)
	}
	if _, err := c.svc.Status(context.Background(), "acme", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("the key must be stored despite the failed push: %v", err)
	}
}

// The coding role's subscription token belongs to the coding agent, which does
// not call through the gateway. Publishing it would overwrite the provider's
// credential with one no governed agent can use.
func TestAnthropicSave_DoesNotPublishTheSubscription_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	pub := &fakeModelProviderPublisher{}
	c.svc.WithModelProvider(pub)

	// A subscription is only meaningful alongside the org's own key.
	c.connect(t, "acme", anthropicDBKey2)
	if pub.calls != 1 {
		t.Fatalf("API key save published %d time(s), want 1", pub.calls)
	}

	c.patch(t, "acme", subscriptionPatch(anthropicDBOAuthToken))
	if pub.calls != 1 {
		t.Errorf("publisher called %d time(s); the subscription must not publish", pub.calls)
	}
}
