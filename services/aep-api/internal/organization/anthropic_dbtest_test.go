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

// DBTEST tier (skips under -short; `make test-db` runs it): the AI agents card
// writing the model connection and the Claude subscription, over a pristine
// per-test Postgres (dbtest.New) with the REAL AES-256-GCM secrets.NewDBStore
// and the REAL prober against a fake endpoint — the SQL-shaped behavior under
// pin: the connection row and its org_secrets bytes, org scoping, the
// disconnect's delete and org isolation. What ModelConnectionService reads
// back from these rows is pinned in model_connection_dbtest_test.go.
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

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/modelcost"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
	"github.com/wso2/aep/aep-api/internal/platform/patch"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// anthropicDBAESKey is the 32-byte AES-256 key for the real DBStore.
const anthropicDBAESKey = "0123456789abcdef0123456789abcdef"

// anthropicDBKey2 is a second well-formed key for the replace/isolation pins.
const anthropicDBKey2 = "sk-ant-api03-ZyXwVuTsRqPoNmLkJiHgFe-second-9876"

// cardDB is the real services, the card that writes them and the /config
// orchestrator over one per-test Postgres.
type cardDB struct {
	db       *gorm.DB
	svc      *organization.AnthropicCredentialService
	conns    *organization.ModelConnectionService
	settings *organization.AgentSettingsService
	config   *organization.Service
	store    secrets.CredentialStore
	repo     organization.OrgAnthropicRepository
	connRepo organization.OrgModelConnectionRepository
	endpoint *modelEndpoint
}

// newCardDB wires cardDB with a fake model endpoint and a fake Anthropic
// subscription probe, both answering apiStatus.
func newCardDB(t *testing.T, apiStatus int) *cardDB {
	t.Helper()
	db := dbtest.New(t)
	store, err := secrets.NewDBStore(db, []byte(anthropicDBAESKey))
	if err != nil {
		t.Fatalf("real DBStore: %v", err)
	}
	subsBase, _ := anthropicFakeAPI(t, apiStatus)
	endpoint := newModelEndpoint(t, apiStatus)
	repo := organization.NewOrgAnthropicRepository(db)
	connRepo := organization.NewOrgModelConnectionRepository(db)
	svc := organization.NewAnthropicCredentialService(repo, store).WithAnthropicAPIBase(subsBase)
	conns := organization.NewModelConnectionService(connRepo, repo, store, sonnetRates()).WithProbeClient(endpoint.client())
	settings := organization.NewAgentSettingsService(organization.NewOrgAgentSettingsRepository(db),
		organization.NewOrganizationRepository(db), svc, conns, organization.NewAgentsCardRepository(db, store), orgconfig.AgentRuntimes)
	config := organization.NewService(nil, nil, nil, organization.PlatformIDPConfig{}).
		WithAgentSettings(settings)
	return &cardDB{db: db, svc: svc, conns: conns, settings: settings, config: config, store: store, repo: repo, connRepo: connRepo, endpoint: endpoint}
}

// sonnetRates prices (api.anthropic.com, claude-sonnet-5) only.
func sonnetRates() *modelcost.Stamper {
	return modelcost.NewStamper([]modelcost.ModelRate{{Host: "api.anthropic.com", ModelID: "claude-sonnet-5", InputPerMTok: 2}})
}

func llmPatch(w orgconfig.LLMPatch) orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{LLM: patch.Field[orgconfig.LLMPatch]{Sent: true, Value: w}}
}

func keyPatch(key string) orgconfig.ConfigPatch {
	return llmPatch(orgconfig.LLMPatch{Kind: "anthropic", APIKey: key})
}

func disconnectPatch() orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{LLM: patch.Field[orgconfig.LLMPatch]{Sent: true, Null: true}}
}

func subscriptionPatch(token string) orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{Agents: patch.Field[orgconfig.AgentsWrite]{Sent: true, Value: orgconfig.AgentsWrite{
		Subscription: patch.Field[orgconfig.SubscriptionWrite]{Sent: true, Value: orgconfig.SubscriptionWrite{Kind: "claude", Token: token}},
	}}}
}

func (c *cardDB) patch(t *testing.T, org string, p orgconfig.ConfigPatch) *orgconfig.ConfigProjection {
	t.Helper()
	out, err := c.config.Patch(context.Background(), org, "ada", p)
	if err != nil {
		t.Fatalf("patch %s: %v", org, err)
	}
	return out
}

// connect saves an Anthropic key with the format's defaults and returns the
// stored row.
func (c *cardDB) connect(t *testing.T, org, key string) *organization.OrgModelConnection {
	t.Helper()
	c.patch(t, org, keyPatch(key))
	return c.row(t, org)
}

func (c *cardDB) row(t *testing.T, org string) *organization.OrgModelConnection {
	t.Helper()
	row, err := c.connRepo.GetByOrg(context.Background(), org)
	if err != nil || row == nil {
		t.Fatalf("connection row for %s: %+v (%v)", org, row, err)
	}
	return row
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

func TestModelConnectionConnect_HappyPath_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()
	start := time.Now().UTC().Add(-time.Second)

	// The key arrives padded — the card must trim before shape-check + store.
	out := c.patch(t, "acme", keyPatch("  "+anthropicUnitKey+"\n"))
	row := c.row(t, "acme")
	if row.Format != "anthropic" || row.BaseURL != "https://api.anthropic.com/v1" || row.Model != "claude-sonnet-5" ||
		row.AuthScheme != "x-api-key" || row.ContextWindow != nil || row.OutputLimit != nil || row.ImageInput != "yes" {
		t.Fatalf("row = %+v, want Anthropic's API with its defaults and NULL limits", row)
	}
	if row.KeyPreview != "sk-a…1234" || row.UpdatedBy == nil || *row.UpdatedBy != "ada" || row.ConnectedAt.Before(start) {
		t.Fatalf("row = %+v, want the preview, the actor and a fresh connectedAt", row)
	}
	// The probe's findings ride the response.
	if out.LLMCheck == nil || out.LLMCheck.ModelListed != "yes" || !out.LLMCheck.Priced {
		t.Fatalf("llmCheck = %+v, want a listed, priced model", out.LLMCheck)
	}
	// The TRIMMED key round-trips through the real AES-GCM org_secrets store.
	got, err := c.store.Get(ctx, "acme", "model/key")
	if err != nil || string(got) != anthropicUnitKey {
		t.Fatalf("stored key: got %q (%v), want the trimmed key", string(got), err)
	}
}

func TestModelConnectionConnect_RejectedKeyLeavesNoTrace_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusUnauthorized)
	ctx := context.Background()

	_, err := c.config.Patch(ctx, "acme", "ada", keyPatch(anthropicUnitKey))
	if got := cardErrCode(t, err); got != "llm_key_rejected" {
		t.Fatalf("code: got %q, want llm_key_rejected", got)
	}
	if row, err := c.connRepo.GetByOrg(ctx, "acme"); err != nil || row != nil {
		t.Fatalf("row after rejected connect: %+v (%v), want none", row, err)
	}
	if _, err := c.store.Get(ctx, "acme", "model/key"); !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("store after rejected connect: want ErrSecretNotFound, got %v", err)
	}
}

// A key rotation on the same host keeps connected_at; a host change resets it.
func TestModelConnectionConnect_ReplaceUpserts_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	first := c.connect(t, "acme", anthropicUnitKey)
	time.Sleep(25 * time.Millisecond) // make the two saves' clocks distinguishable
	second := c.connect(t, "acme", anthropicDBKey2)

	if second.KeyPreview != "sk-a…9876" || !second.ConnectedAt.Equal(first.ConnectedAt) || !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("rotation: first %+v, second %+v", first, second)
	}
	got, err := c.store.Get(ctx, "acme", "model/key")
	if err != nil || string(got) != anthropicDBKey2 {
		t.Fatalf("stored key after replace: got %q err %v, want key2", string(got), err)
	}

	c.patch(t, "acme", orgconfig.ConfigPatch{
		LLM:    patch.Field[orgconfig.LLMPatch]{Sent: true, Value: orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/v1", APIKey: "ollama-db-key-0123456789", Model: "gpt-oss:20b"}},
		Agents: patch.Field[orgconfig.AgentsWrite]{Sent: true, Value: orgconfig.AgentsWrite{Runtime: "opencode"}},
	})
	third := c.row(t, "acme")
	if third.Host != "ollama.com" || !third.ConnectedAt.After(first.ConnectedAt) || third.AuthScheme != "bearer" {
		t.Fatalf("host change: %+v, want a fresh connectedAt on ollama.com with Bearer", third)
	}
	// Ollama told the probe the model's context window and that it reads no images.
	if third.ContextWindow == nil || *third.ContextWindow != 131072 || third.ImageInput != "no" || third.OutputLimit == nil {
		t.Fatalf("host enrichment: %+v", third)
	}
}

func TestModelConnectionDisconnect_RemovesRowAndBytes_Idempotent_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()
	if err := c.db.Exec(`INSERT INTO organizations (uuid, name) VALUES (gen_random_uuid(), 'acme')`).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	c.connect(t, "acme", anthropicUnitKey)
	// An org the rename has not finished still holds its Anthropic-era copy.
	if err := c.store.Put(ctx, "acme", "anthropic/key", []byte(anthropicUnitKey)); err != nil {
		t.Fatalf("seed anthropic/key: %v", err)
	}

	out := c.patch(t, "acme", disconnectPatch())
	if row, err := c.connRepo.GetByOrg(ctx, "acme"); err != nil || row != nil {
		t.Fatalf("row after disconnect: %+v (%v)", row, err)
	}
	for _, key := range []string{"model/key", "anthropic/key"} {
		if _, err := c.store.Get(ctx, "acme", key); !errors.Is(err, secrets.ErrSecretNotFound) {
			t.Fatalf("%s must go with the row, got %v", key, err)
		}
	}
	if out.LLM != nil || out.LLMDisconnectedAt == nil {
		t.Fatalf("projection after disconnect: llm=%+v disconnectedAt=%v", out.LLM, out.LLMDisconnectedAt)
	}
	// Disconnecting an org with no connection is a clean no-op.
	c.patch(t, "acme", disconnectPatch())
}

func TestModelConnectionOrgIsolation_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	c.connect(t, "acme", anthropicUnitKey)
	c.connect(t, "globex", anthropicDBKey2)

	if _, key, ok, err := c.conns.Effective(ctx, "acme"); err != nil || !ok || key != anthropicUnitKey {
		t.Fatalf("acme effective key: ok=%v err %v", ok, err)
	}
	if _, key, ok, err := c.conns.Effective(ctx, "globex"); err != nil || !ok || key != anthropicDBKey2 {
		t.Fatalf("globex effective key: ok=%v err %v", ok, err)
	}
	if _, _, ok, err := c.conns.Effective(ctx, "intruder"); err != nil || ok {
		t.Fatalf("intruder must have no connection, got ok=%v (%v)", ok, err)
	}
	// Disconnecting one org must not touch the other's row or bytes.
	c.patch(t, "acme", disconnectPatch())
	c.row(t, "globex")
	if got, err := c.store.Get(ctx, "globex", "model/key"); err != nil || string(got) != anthropicDBKey2 {
		t.Fatalf("globex bytes must survive acme's disconnect: %q err %v", string(got), err)
	}
}

// --- the key's SM-API copy ------------------------------------------------------

// The copies follow the commit under the card's lock: each saved key and
// token is uploaded and its row stamped with where it lives, and a
// disconnect deletes the connection key's copy and the token's.
func TestAgentsCardSave_MirrorsAndStampsTheCopies_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	sm := &fakeSMClient{createRef: "model-connection-secrets"}
	writer := organization.NewSecretRefWriter(sm, organization.NewOrgCredentialRepository(c.db, nil), c.repo,
		organization.NewIDPRepository(c.db, nil), c.connRepo).
		WithOrgSecretWriter(organization.NewOrgSecretWriter(sm, organization.NewOrgSecretRepository(c.db), organization.NewOrgSecretLock(c.db), fixedClock))
	c.conns.WithSecretRefWriter(writer)
	c.svc.WithSecretRefWriter(writer)
	ctx := claimsCtx(uuid.NewString())
	patch := func(p orgconfig.ConfigPatch) {
		t.Helper()
		if _, err := c.config.Patch(ctx, "acme", "ada", p); err != nil {
			t.Fatalf("patch: %v", err)
		}
	}
	lastUpload := func() string {
		t.Helper()
		if len(sm.createCalls) == 0 {
			t.Fatal("nothing was uploaded")
		}
		return sm.createCalls[len(sm.createCalls)-1].data[secretmanagersvc.SecretKeyAPIKey]
	}

	patch(keyPatch(anthropicUnitKey))
	if got := lastUpload(); got != anthropicUnitKey {
		t.Fatalf("uploaded %q, want the saved key", got)
	}
	if row := c.row(t, "acme"); derefStr(row.SecretRefName) != "model-connection-secrets" || derefStr(row.SecretRefKVPath) == "" {
		t.Fatalf("connection triplet = %v %v, want the uploaded copy", row.SecretRefName, row.SecretRefKVPath)
	}

	patch(subscriptionPatch(anthropicDBOAuthToken))
	if got := lastUpload(); got != anthropicDBOAuthToken {
		t.Fatalf("uploaded %q, want the subscription token", got)
	}
	sub, err := c.repo.GetByOrg(context.Background(), "acme", organization.AnthropicRoleCoding)
	if err != nil || sub == nil || derefStr(sub.SecretRefName) == "" || derefStr(sub.SecretRefKVPath) == "" {
		t.Fatalf("subscription row = %+v (%v), want its triplet stamped", sub, err)
	}

	patch(disconnectPatch())
	if len(sm.deleteCalls) != 2 {
		t.Fatalf("SM-API deletes = %d, want the key's copy and the token's", len(sm.deleteCalls))
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// --- the key reaches the Agent Manager provider ------------------------------

type fakeModelProviderPublisher struct {
	published string
	conn      modelconn.Connection
	calls     int
	cleared   int
	err       error
}

func (f *fakeModelProviderPublisher) PublishOrgModelConnection(_ context.Context, _ string, conn modelconn.Connection, apiKey string) error {
	f.calls++
	f.published, f.conn = apiKey, conn
	return f.err
}

func (f *fakeModelProviderPublisher) ClearOrgModelKey(context.Context, string, modelconn.Connection) error {
	f.cleared++
	return f.err
}

// A rotated key must reach the provider that holds a COPY of it, and so must a
// switch to another host. Without this, every governed agent in the org keeps
// calling the old upstream with the old credential, and the failure surfaces
// there rather than in Settings.
func TestModelConnectionSave_PublishesTheConnectionToTheModelProvider_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	pub := &fakeModelProviderPublisher{}
	c.svc.WithModelProvider(pub)

	c.connect(t, "acme", anthropicDBKey2)
	if pub.calls != 1 || pub.published != anthropicDBKey2 || pub.conn.Format != modelconn.FormatAnthropic {
		t.Fatalf("publisher calls = %d with %q on %q, want 1 with the newly saved key", pub.calls, pub.published, pub.conn.Format)
	}
	// Moving to another host publishes that host's connection, in one write.
	c.patch(t, "acme", ollamaPatch())
	if pub.calls != 2 || pub.cleared != 0 {
		t.Fatalf("after the move: publishes=%d clears=%d, want 2 and 0", pub.calls, pub.cleared)
	}
	if pub.published != "ollama-db-key-0123456789" || pub.conn.Format != modelconn.FormatOpenAICompatible ||
		pub.conn.BaseURL != "https://ollama.com/v1" {
		t.Fatalf("published %q on %+v, want the Ollama key and connection", pub.published, pub.conn)
	}
}

// A save that moves the URL and keeps the key (allowed on the stored host)
// still changes what the provider holds, so it publishes — with the stored
// key, since the save carried none.
func TestModelConnectionSave_AURLChangeKeepingTheKeyPublishesTheStoredKey_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	pub := &fakeModelProviderPublisher{}
	c.svc.WithModelProvider(pub)

	c.patch(t, "acme", ollamaPatch())
	c.patch(t, "acme", llmPatch(orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/api/v1"}))
	if pub.calls != 2 {
		t.Fatalf("publishes = %d, want the URL change published", pub.calls)
	}
	if pub.published != "ollama-db-key-0123456789" || pub.conn.BaseURL != "https://ollama.com/api/v1" {
		t.Fatalf("published %q on %q, want the stored key on the new URL", pub.published, pub.conn.BaseURL)
	}
}

// ollamaPatch moves the org to Ollama's OpenAI-compatible endpoint on OpenCode.
func ollamaPatch() orgconfig.ConfigPatch {
	return orgconfig.ConfigPatch{
		LLM:    patch.Field[orgconfig.LLMPatch]{Sent: true, Value: orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/v1", APIKey: "ollama-db-key-0123456789"}},
		Agents: patch.Field[orgconfig.AgentsWrite]{Sent: true, Value: orgconfig.AgentsWrite{Runtime: "opencode"}},
	}
}

// A disconnect leaves the org with no connection, so the provider's copy of the
// key is cleared on it, and only on it: a second disconnect has nothing to
// clear, and the Ollama connect that follows publishes.
func TestModelConnectionDisconnect_ClearsTheModelProviderOnce_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	pub := &fakeModelProviderPublisher{}
	c.svc.WithModelProvider(pub)

	c.connect(t, "acme", anthropicDBKey2)
	c.patch(t, "acme", disconnectPatch())
	if pub.calls != 1 || pub.cleared != 1 {
		t.Fatalf("after the disconnect: publishes=%d clears=%d, want 1 and 1", pub.calls, pub.cleared)
	}
	c.patch(t, "acme", disconnectPatch())
	c.patch(t, "acme", ollamaPatch())
	if pub.calls != 2 || pub.cleared != 1 {
		t.Fatalf("after a second disconnect and an Ollama connect: publishes=%d clears=%d, want 2 and 1 in total",
			pub.calls, pub.cleared)
	}
}

// Every format's key reaches the provider, so every format's disconnect clears
// it.
func TestModelConnectionDisconnect_FromAnotherHostClears_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	pub := &fakeModelProviderPublisher{}
	c.svc.WithModelProvider(pub)

	c.patch(t, "acme", ollamaPatch())
	c.patch(t, "acme", disconnectPatch())
	if pub.calls != 1 || pub.cleared != 1 {
		t.Fatalf("publishes=%d clears=%d, want 1 and 1", pub.calls, pub.cleared)
	}
}

// A failed clear must not fail the disconnect: the connection IS gone, and
// the failure is logged naming the copy left behind.
func TestModelConnectionDisconnect_SurvivesAClearFailure_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.connect(t, "acme", anthropicDBKey2)
	pub := &fakeModelProviderPublisher{err: errors.New("amp unreachable")}
	c.svc.WithModelProvider(pub)

	if _, err := c.config.Patch(context.Background(), "acme", "ada", disconnectPatch()); err != nil {
		t.Fatalf("the disconnect must succeed even when the provider clear fails: %v", err)
	}
	if pub.cleared != 1 {
		t.Fatalf("clears = %d, want one attempt", pub.cleared)
	}
	if row, err := c.connRepo.GetByOrg(context.Background(), "acme"); err != nil || row != nil {
		t.Fatalf("row after disconnect: %+v (%v)", row, err)
	}
}

// A publisher failure fails the save with agent_manager_not_updated (502), and
// the key stays stored: a governed deploy fails closed without the provider,
// so the user is told to save the key again rather than told "Saved".
func TestModelConnectionSave_APublisherFailureIsAgentManagerNotUpdated_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.svc.WithModelProvider(&fakeModelProviderPublisher{err: errors.New("amp unreachable")})

	_, err := c.config.Patch(context.Background(), "acme", "ada", keyPatch(anthropicDBKey2))
	var se *organization.SectionError
	if !errors.As(err, &se) || se.Status != http.StatusBadGateway || se.Code != "agent_manager_not_updated" || se.Section != "llm" {
		t.Fatalf("err = %v, want a 502 agent_manager_not_updated on llm", err)
	}
	if se.Message != "Key saved; Agent Manager was not updated. Save the key again." {
		t.Fatalf("message = %q", se.Message)
	}
	c.row(t, "acme")
}

// The subscription token belongs to the coding agent, which does not call
// through the gateway. Publishing it would overwrite the provider's credential
// with one no governed agent can use.
func TestModelConnectionSave_DoesNotPublishTheSubscription_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	pub := &fakeModelProviderPublisher{}
	c.svc.WithModelProvider(pub)

	c.connect(t, "acme", anthropicDBKey2)
	c.patch(t, "acme", subscriptionPatch(anthropicDBOAuthToken))
	if pub.calls != 1 {
		t.Errorf("publisher called %d time(s); the subscription must not publish", pub.calls)
	}
}
