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
// per-test Postgres (dbtest.New) with the REAL org secret writer, repository
// and lock, a fake vault at the writer's port, and the REAL prober against a
// fake endpoint — the SQL-shaped behavior under pin: the connection row, the
// key's default-key reference (the only place the key goes), org scoping, the
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
)

// anthropicDBKey2 is a second well-formed key for the replace/isolation pins.
const anthropicDBKey2 = "sk-ant-api03-ZyXwVuTsRqPoNmLkJiHgFe-second-9876"

// cardDB is the real services, the card that writes them and the /config
// orchestrator over one per-test Postgres, with the keys written to a fake
// vault through the real org secret writer. ctx carries the user's ouId
// claim every vault write needs.
type cardDB struct {
	db        *gorm.DB
	svc       *organization.AnthropicCredentialService
	conns     *organization.ModelConnectionService
	settings  *organization.AgentSettingsService
	config    *organization.Service
	repo      organization.OrgAnthropicRepository
	connRepo  organization.OrgModelConnectionRepository
	refs      organization.OrgSecretRepository
	vault     *fakeVault
	consumers *pathConsumers
	endpoint  *modelEndpoint
	ctx       context.Context
}

// newCardDB wires cardDB with a fake model endpoint and a fake Anthropic
// subscription probe, both answering apiStatus. existing are vault
// references that exist before the test.
func newCardDB(t *testing.T, apiStatus int, existing ...string) *cardDB {
	t.Helper()
	db := dbtest.New(t)
	subsBase, _ := anthropicFakeAPI(t, apiStatus)
	endpoint := newModelEndpoint(t, apiStatus)
	repo := organization.NewOrgAnthropicRepository(db)
	connRepo := organization.NewOrgModelConnectionRepository(db)
	refs := organization.NewOrgSecretRepository(db)
	vault := newFakeVault(existing...)
	sm := mintingSM{fakeSMClient: &fakeSMClient{}, vault: vault}
	consumers := &pathConsumers{}
	writer := organization.NewSecretRefWriter(sm, organization.NewIDPRepository(db)).
		WithOrgSecretWriter(organization.NewOrgSecretWriter(sm, refs, organization.NewOrgSecretLock(db), fixedClock)).
		WithModelKeyConsumers(consumers)
	svc := organization.NewAnthropicCredentialService(repo).WithAnthropicAPIBase(subsBase).WithSecretRefWriter(writer)
	conns := organization.NewModelConnectionService(connRepo, repo, refs, sonnetRates()).WithProbeClient(endpoint.client()).
		WithSecretRefWriter(writer)
	settings := organization.NewAgentSettingsService(organization.NewOrgAgentSettingsRepository(db),
		organization.NewOrganizationRepository(db), svc, conns, organization.NewAgentsCardRepository(db), orgconfig.AgentRuntimes)
	config := organization.NewService(nil, nil, nil, organization.PlatformIDPConfig{}).
		WithOrgSecretRefs(refs).WithAgentSettings(settings)
	return &cardDB{db: db, svc: svc, conns: conns, settings: settings, config: config, repo: repo, connRepo: connRepo,
		refs: refs, vault: vault, consumers: consumers, endpoint: endpoint, ctx: claimsCtx(uuid.NewString())}
}

// ref is the org secret's reference row, nil when unset.
func (c *cardDB) ref(t *testing.T, org string, s organization.OrgSecret) *organization.OrgSecretRef {
	t.Helper()
	ref, err := c.refs.Get(context.Background(), org, s)
	if err != nil {
		t.Fatalf("org secret %s/%s: %v", org, s, err)
	}
	return ref
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
	out, err := c.config.Patch(c.ctx, org, "ada", p)
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
	start := time.Now().UTC().Add(-time.Second)

	// The key arrives padded — the card must trim before shape-check + store.
	out := c.patch(t, "acme", keyPatch("  "+anthropicUnitKey+"\n"))
	row := c.row(t, "acme")
	if row.Format != "anthropic" || row.BaseURL != "https://api.anthropic.com/v1" || row.Model != "claude-sonnet-5" ||
		row.AuthScheme != "x-api-key" || row.ContextWindow != nil || row.OutputLimit != nil || row.ImageInput != "yes" {
		t.Fatalf("row = %+v, want Anthropic's API with its defaults and NULL limits", row)
	}
	if row.KeyPreview != "" || row.UpdatedBy == nil || *row.UpdatedBy != "ada" || row.ConnectedAt.Before(start) {
		t.Fatalf("row = %+v, want no key character, the actor and a fresh connectedAt", row)
	}
	// The probe's findings ride the response.
	if out.LLMCheck == nil || out.LLMCheck.ModelListed != "yes" || !out.LLMCheck.Priced {
		t.Fatalf("llmCheck = %+v, want a listed, priced model", out.LLMCheck)
	}
	// The TRIMMED key went to the vault alone, under its default-key
	// reference; Postgres holds the reference's name and no value.
	ref := c.ref(t, "acme", organization.OrgSecretDefaultKey)
	if ref == nil || !c.vault.refs[ref.Name] || c.vault.lastData["api-key"] != anthropicUnitKey {
		t.Fatalf("default-key %+v, vault %v: want the trimmed key under the recorded reference", ref, c.vault.live())
	}
	var values int64
	c.db.Raw(`SELECT count(*) FROM org_secrets WHERE oc_org_id = 'acme' AND value IS NOT NULL`).Scan(&values)
	if values != 0 {
		t.Fatalf("org_secrets holds %d value rows for the org, want none", values)
	}
}

func TestModelConnectionConnect_RejectedKeyLeavesNoTrace_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusUnauthorized)
	ctx := context.Background()

	_, err := c.config.Patch(c.ctx, "acme", "ada", keyPatch(anthropicUnitKey))
	if got := cardErrCode(t, err); got != "llm_key_rejected" {
		t.Fatalf("code: got %q, want llm_key_rejected", got)
	}
	if row, err := c.connRepo.GetByOrg(ctx, "acme"); err != nil || row != nil {
		t.Fatalf("row after rejected connect: %+v (%v), want none", row, err)
	}
	if c.ref(t, "acme", organization.OrgSecretDefaultKey) != nil || c.vault.creates != 0 {
		t.Fatalf("a rejected key reached the vault (%d writes)", c.vault.creates)
	}
}

// A key rotation on the same host keeps connected_at; a host change resets it.
func TestModelConnectionConnect_ReplaceUpserts_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)

	first := c.connect(t, "acme", anthropicUnitKey)
	time.Sleep(25 * time.Millisecond) // make the two saves' clocks distinguishable
	second := c.connect(t, "acme", anthropicDBKey2)

	if !second.ConnectedAt.Equal(first.ConnectedAt) || !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("rotation: first %+v, second %+v", first, second)
	}
	if c.vault.lastData["api-key"] != anthropicDBKey2 {
		t.Fatal("the rotation's vault write holds key2")
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
	key := c.ref(t, "acme", organization.OrgSecretDefaultKey)

	out := c.patch(t, "acme", disconnectPatch())
	if row, err := c.connRepo.GetByOrg(ctx, "acme"); err != nil || row != nil {
		t.Fatalf("row after disconnect: %+v (%v)", row, err)
	}
	if c.ref(t, "acme", organization.OrgSecretDefaultKey) != nil || c.vault.refs[key.Name] {
		t.Fatalf("the default-key row and its reference must go with the connection (vault %v)", c.vault.live())
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

	_, acme, err := c.conns.KeyRef(ctx, "acme")
	if err != nil {
		t.Fatalf("acme key ref: %v", err)
	}
	_, globex, err := c.conns.KeyRef(ctx, "globex")
	if err != nil || globex.Name == acme.Name {
		t.Fatalf("globex key ref %+v (%v): each org its own reference", globex, err)
	}
	if _, ok, err := c.conns.Connection(ctx, "intruder"); err != nil || ok {
		t.Fatalf("intruder must have no connection, got ok=%v (%v)", ok, err)
	}
	// Disconnecting one org must not touch the other's row or reference.
	c.patch(t, "acme", disconnectPatch())
	c.row(t, "globex")
	if ref := c.ref(t, "globex", organization.OrgSecretDefaultKey); ref == nil || ref.Name != globex.Name || !c.vault.refs[globex.Name] {
		t.Fatalf("globex's reference must survive acme's disconnect: %+v", ref)
	}
}

// --- the keys in vault -------------------------------------------------------

// Each saved key and token is written to the vault from the request, as a new
// reference its org_secrets row records; no triplet is stamped on the rows,
// and a disconnect deletes the key's reference and the token's.
func TestAgentsCardSave_WritesTheRequestKeysToVault_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)

	c.patch(t, "acme", keyPatch(anthropicUnitKey))
	key := c.ref(t, "acme", organization.OrgSecretDefaultKey)
	if c.vault.lastData[secretmanagersvc.SecretKeyAPIKey] != anthropicUnitKey || key == nil || !c.vault.refs[key.Name] {
		t.Fatalf("default-key %+v, vault %v: want the saved key under a recorded reference", key, c.vault.live())
	}
	if row := c.row(t, "acme"); row.SecretRefName != nil || row.SecretRefKVPath != nil {
		t.Fatalf("connection triplet = %v %v, want none written", row.SecretRefName, row.SecretRefKVPath)
	}

	c.patch(t, "acme", subscriptionPatch(anthropicDBOAuthToken))
	token := c.ref(t, "acme", organization.OrgSecretCodingAgentKey)
	if c.vault.lastData[secretmanagersvc.SecretKeyAPIKey] != anthropicDBOAuthToken || token == nil || !c.vault.refs[token.Name] {
		t.Fatalf("coding-agent-key %+v: want the subscription token under a recorded reference", token)
	}
	sub, err := c.repo.GetByOrg(context.Background(), "acme", organization.AnthropicRoleCoding)
	if err != nil || sub == nil || sub.SecretRefName != nil || sub.KeyPrefix != "" || sub.KeyLast4 != "" {
		t.Fatalf("subscription row = %+v (%v), want no triplet and no token character", sub, err)
	}

	c.patch(t, "acme", disconnectPatch())
	if c.vault.refs[key.Name] || c.vault.refs[token.Name] ||
		c.ref(t, "acme", organization.OrgSecretDefaultKey) != nil || c.ref(t, "acme", organization.OrgSecretCodingAgentKey) != nil {
		t.Fatalf("a disconnect deletes the key's reference and the token's (vault %v)", c.vault.live())
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

// A save that moves the URL rewrites the provider, so it needs the key: the
// stored one is never read back. Without one it is refused and publishes
// nothing; with one it publishes the request's key on the new URL.
func TestModelConnectionSave_AURLChangeNeedsTheKeyAndPublishesIt_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	pub := &fakeModelProviderPublisher{}
	c.svc.WithModelProvider(pub)

	c.patch(t, "acme", ollamaPatch())
	_, err := c.config.Patch(c.ctx, "acme", "ada", llmPatch(orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/api/v1"}))
	if got := cardErrCode(t, err); got != "llm_key_required" || pub.calls != 1 {
		t.Fatalf("a keyless URL change: code %q, publishes %d; want llm_key_required and none", got, pub.calls)
	}
	c.patch(t, "acme", llmPatch(orgconfig.LLMPatch{Kind: "openai-compatible", BaseURL: "https://ollama.com/api/v1", APIKey: "ollama-db-key-rotated-0123"}))
	if pub.calls != 2 || pub.published != "ollama-db-key-rotated-0123" || pub.conn.BaseURL != "https://ollama.com/api/v1" {
		t.Fatalf("published %q on %q (%d calls), want the request's key on the new URL", pub.published, pub.conn.BaseURL, pub.calls)
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

	if _, err := c.config.Patch(c.ctx, "acme", "ada", disconnectPatch()); err != nil {
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

	_, err := c.config.Patch(c.ctx, "acme", "ada", keyPatch(anthropicDBKey2))
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

// --- a row means set ---------------------------------------------------------

// The subscription is projected only while the org's coding-agent-key
// reference row exists: a credential row with no reference (saved before the
// token lived in vault) has no usable token, so it reads as not set.
func TestAgentSettings_SubscriptionProjectedOnlyWithItsReferenceRow_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)

	c.patch(t, "acme", keyPatch(anthropicUnitKey))
	c.patch(t, "acme", subscriptionPatch(anthropicDBOAuthToken))
	got, err := c.settings.Effective(context.Background(), "acme")
	if err != nil || got.Subscription == nil {
		t.Fatalf("with its reference row: subscription = %+v (%v), want projected", got.Subscription, err)
	}

	// The legacy shape: the credential row stays, the reference row is gone.
	if err := c.db.Exec(`DELETE FROM org_secrets WHERE oc_org_id = 'acme' AND key = 'coding-agent-key'`).Error; err != nil {
		t.Fatal(err)
	}
	if c.ref(t, "acme", organization.OrgSecretCodingAgentKey) != nil {
		t.Fatal("test setup: the coding-agent-key row is still there")
	}
	if row, err := c.repo.GetByOrg(context.Background(), "acme", organization.AnthropicRoleCoding); err != nil || row == nil {
		t.Fatalf("test setup: the credential row must remain: %+v (%v)", row, err)
	}
	got, err = c.settings.Effective(context.Background(), "acme")
	if err != nil || got.Subscription != nil {
		t.Fatalf("without its reference row: subscription = %+v (%v), want null", got.Subscription, err)
	}
}
