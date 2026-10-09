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

// DBTEST tier — ModelConnectionService read back over the rows the AI agents
// card writes (same real Postgres and org secret writer as
// anthropic_dbtest_test.go). What is pinned here:
//
//   - Connection — the connection without a key; "none" is ok=false, never an
//     error;
//   - KeyRef — the key's reference from its default-key row, NotFoundError
//     while nothing is connected, an error while the row is missing;
//   - ResolveCodingCredential — the subscription only on Claude Code, the
//     connection's key otherwise, from the coding-agent-key and default-key
//     rows, falling back to the key when the subscription's token was never
//     recorded;
//   - the connection itself, read from org_model_connections with the model
//     it was saved with.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// wantAnthropic asserts conn is the one connection every org has: the
// Anthropic format on Anthropic's API, authenticated with x-api-key, on model.
func wantAnthropic(t *testing.T, conn modelconn.Connection, model string) {
	t.Helper()
	want := modelconn.Connection{
		Format:     modelconn.FormatAnthropic,
		BaseURL:    "https://api.anthropic.com/v1",
		Host:       "api.anthropic.com",
		Model:      model,
		AuthScheme: modelconn.AuthXAPIKey,
		ImageInput: modelconn.Yes,
	}
	if conn != want {
		t.Fatalf("connection = %+v, want %+v", conn, want)
	}
}

// --- Connection -----------------------------------------------------------------

func TestModelConnectionConnection_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	// No row → ok=false, NOT an error.
	if _, ok, err := c.conns.Connection(ctx, "acme"); err != nil || ok {
		t.Fatalf("absent org: ok=%v err %v, want none", ok, err)
	}
	c.connect(t, "acme", anthropicUnitKey)
	conn, ok, err := c.conns.Connection(ctx, "acme")
	if err != nil || !ok {
		t.Fatalf("connected: ok=%v err %v", ok, err)
	}
	wantAnthropic(t, conn, modelconn.DefaultAnthropicModel)
}

// The model is part of the connection: the org's choice, not the default.
func TestModelConnectionConnection_CarriesTheChosenModel_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.connect(t, "acme", anthropicUnitKey)
	c.patch(t, "acme", llmPatch(orgconfig.LLMPatch{Model: "claude-haiku-4-5"}))

	conn, ok, err := c.conns.Connection(context.Background(), "acme")
	if err != nil || !ok {
		t.Fatalf("connection: ok=%v err %v", ok, err)
	}
	// A model-only edit is saved unprobed: Anthropic's API keeps its defaults.
	wantAnthropic(t, conn, "claude-haiku-4-5")
}

// --- KeyRef -------------------------------------------------------------------

func TestModelConnectionKeyRef_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	// No connection → NotFoundError, the "not connected yet" contract — a
	// consumer wiring model access must treat this as skip, not fail.
	var nf *organization.NotFoundError
	if _, _, err := c.conns.KeyRef(ctx, "acme"); !errors.As(err, &nf) {
		t.Fatalf("absent org: got %v, want *organization.NotFoundError", err)
	}

	// Connected → the default-key row's reference and its key, never a value.
	c.connect(t, "acme", anthropicUnitKey)
	conn, ref, err := c.conns.KeyRef(ctx, "acme")
	if err != nil {
		t.Fatalf("KeyRef after connect: %v", err)
	}
	if row := c.ref(t, "acme", organization.OrgSecretDefaultKey); ref.Name != row.Name || ref.Property != "api-key" {
		t.Fatalf("KeyRef = %+v, want the default-key row's reference %s with api-key", ref, row.Name)
	}
	wantAnthropic(t, conn, modelconn.DefaultAnthropicModel)

	// A connection row whose default-key row is gone has no key to mount: an
	// error, never a fallback.
	if err := c.db.Exec(`DELETE FROM org_secrets WHERE oc_org_id = 'acme' AND secret = 'default-key'`).Error; err != nil {
		t.Fatalf("drop the row: %v", err)
	}
	if _, _, err := c.conns.KeyRef(ctx, "acme"); err == nil || errors.As(err, &nf) || !strings.Contains(err.Error(), "default-key") {
		t.Fatalf("KeyRef without its row = %v, want an error naming default-key", err)
	}
}

// --- ResolveCodingCredential ----------------------------------------------------

func TestResolveCodingCredential_NoSubscription_UsesTheKey_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.connect(t, "acme", anthropicUnitKey)

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred.Ref.Name != c.ref(t, "acme", organization.OrgSecretDefaultKey).Name || cred.Ref.Property != "api-key" ||
		cred.Kind != organization.CodingCredentialConnectionKey {
		t.Fatalf("no subscription must resolve to the API key, got %+v", cred)
	}
	wantAnthropic(t, cred.Conn, modelconn.DefaultAnthropicModel)
}

// keyAndSubscription connects the key and a subscription.
func keyAndSubscription(t *testing.T, c *cardDB) {
	t.Helper()
	c.connect(t, "acme", anthropicUnitKey)
	c.patch(t, "acme", subscriptionPatch(anthropicDBOAuthToken))
}

// Dispatch reads the row, never the bytes, so the resolved kind is the only
// thing that can tell it which variable to mount. Getting it wrong means Claude
// Code ignores the token (ANTHROPIC_API_KEY outranks CLAUDE_CODE_OAUTH_TOKEN)
// and bills the API key in silence.
func TestResolveCodingCredential_SubscriptionOnClaudeCode_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	keyAndSubscription(t, c)

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred.Ref.Name != c.ref(t, "acme", organization.OrgSecretCodingAgentKey).Name ||
		cred.Kind != organization.CodingCredentialClaudeSubscription {
		t.Fatalf("a Claude Code run must mount the subscription, got %+v", cred)
	}
}

// dropRow deletes the org secret's reference row, as an org saved before the
// rows existed has none.
func dropRow(t *testing.T, c *cardDB, s organization.OrgSecret) {
	t.Helper()
	if err := c.db.Exec(`DELETE FROM org_secrets WHERE oc_org_id = 'acme' AND secret = ?`, string(s)).Error; err != nil {
		t.Fatalf("drop the %s row: %v", s, err)
	}
}

// OpenCode cannot present a token, so a subscription row — which the save rule
// never leaves beside OpenCode — is not even consulted.
func TestResolveCodingCredential_OpenCodeNeverGetsTheSubscription_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	keyAndSubscription(t, c)

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeOpenCode)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred.Ref.Name != c.ref(t, "acme", organization.OrgSecretDefaultKey).Name ||
		cred.Kind != organization.CodingCredentialConnectionKey {
		t.Fatalf("an OpenCode run must mount the API key, got %+v", cred)
	}

	// Not consulted means not consulted: a subscription without its token
	// leaves OpenCode on the API key too.
	dropRow(t, c, organization.OrgSecretCodingAgentKey)
	cred, err = c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeOpenCode)
	if err != nil || cred.Kind != organization.CodingCredentialConnectionKey {
		t.Fatalf("a broken subscription reached an OpenCode run: cred=%+v err=%v", cred, err)
	}
}

// A subscription whose token was never recorded (the credential row stays, its
// coding-agent-key reference row is missing: saved before the token lived in
// vault) cannot be mounted, so coding falls back to the connection's key
// rather than fail every run. Settings shows that subscription with a "save
// its token again" warning (tokenMissing).
func TestResolveCodingCredential_SubscriptionWithoutItsTokenFallsBackToTheKey_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	keyAndSubscription(t, c)
	dropRow(t, c, organization.OrgSecretCodingAgentKey)

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil {
		t.Fatalf("a subscription without its token must fall back to the key, got: %v", err)
	}
	if cred.Ref.Name != c.ref(t, "acme", organization.OrgSecretDefaultKey).Name || cred.Ref.Property != "api-key" ||
		cred.Kind != organization.CodingCredentialConnectionKey {
		t.Fatalf("a subscription without its token must resolve to the API key, got %+v", cred)
	}
	wantAnthropic(t, cred.Conn, modelconn.DefaultAnthropicModel)
}

// With no connection key recorded either there is nothing to bill: the
// resolver errors, as it does for an org with no subscription and no key.
func TestResolveCodingCredential_SubscriptionWithoutItsTokenAndNoKey_Errors_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	keyAndSubscription(t, c)
	dropRow(t, c, organization.OrgSecretCodingAgentKey)
	dropRow(t, c, organization.OrgSecretDefaultKey)

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err == nil {
		t.Fatalf("no recorded token and no recorded key must not resolve, got %+v", cred)
	}
	if !strings.Contains(err.Error(), "default-key") {
		t.Fatalf("the error must name the missing key reference, got: %v", err)
	}
}

func TestResolveCodingCredential_NoRowsAtAll_Errors_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	if _, err := c.conns.ResolveCodingCredential(context.Background(), "ghost", orgconfig.AgentRuntimeClaudeCode); err == nil {
		t.Fatal("an org with no connection at all must not resolve a secret ref")
	}
}
