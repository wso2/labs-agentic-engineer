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
// card writes (same real Postgres + AES-GCM store as anthropic_dbtest_test.go).
// What is pinned here:
//
//   - Effective — the connection and its key for the spec agents; "none" is
//     ok=false, never an error; it stays on the API key when a subscription
//     exists;
//   - KeyRef — the key's vault triplet, NotFoundError while nothing is
//     connected;
//   - ResolveCodingCredential — the subscription only on Claude Code, the
//     connection's key otherwise, failing closed on a broken subscription;
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

// --- Effective ----------------------------------------------------------------

func TestModelConnectionEffective_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	// No row → ok=false, NOT an error (the turn surface maps it).
	if _, key, ok, err := c.conns.Effective(ctx, "acme"); err != nil || ok || key != "" {
		t.Fatalf("absent org: ok=%v key=%q err %v, want none", ok, key, err)
	}
	c.connect(t, "acme", anthropicUnitKey)
	conn, key, ok, err := c.conns.Effective(ctx, "acme")
	if err != nil || !ok || key != anthropicUnitKey {
		t.Fatalf("connected: ok=%v err %v, want the org key", ok, err)
	}
	wantAnthropic(t, conn, modelconn.DefaultAnthropicModel)

	// The row exists but the bytes vanished → degrades to none.
	if err := c.store.Delete(ctx, "acme", "model/key"); err != nil {
		t.Fatalf("store delete: %v", err)
	}
	if _, key, ok, err := c.conns.Effective(ctx, "acme"); err != nil || ok || key != "" {
		t.Fatalf("active row without bytes: ok=%v key=%q err %v, want none", ok, key, err)
	}
}

// The model is part of the connection: the org's choice, not the default.
func TestModelConnectionEffective_CarriesTheChosenModel_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()
	c.connect(t, "acme", anthropicUnitKey)
	c.patch(t, "acme", llmPatch(orgconfig.LLMPatch{Model: "claude-haiku-4-5"}))

	conn, _, ok, err := c.conns.Effective(ctx, "acme")
	if err != nil || !ok {
		t.Fatalf("effective: ok=%v err %v", ok, err)
	}
	wantAnthropic(t, conn, "claude-haiku-4-5")
}

func TestModelConnectionEffective_IsTheKeyNotTheSubscription_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.connect(t, "acme", anthropicUnitKey)
	c.patch(t, "acme", subscriptionPatch(anthropicDBOAuthToken))

	_, key, ok, err := c.conns.Effective(context.Background(), "acme")
	if err != nil || !ok || key != anthropicUnitKey {
		t.Fatalf("the spec agents must keep billing the API key, got ok=%v (%v)", ok, err)
	}
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

	// Connected and mirrored → the vault triplet, not the key's bytes. No
	// SecretRefWriter is wired here, so the mirror's columns are stamped the
	// way the ResolveCodingCredential tests stamp them.
	c.connect(t, "acme", anthropicUnitKey)
	stampConnectionTriplet(t, c.connRepo, "acme", "acme-anthropic", "user-app-secrets/wc-acme/acme-anthropic", "api-key")
	conn, triplet, err := c.conns.KeyRef(ctx, "acme")
	if err != nil {
		t.Fatalf("KeyRef after connect: %v", err)
	}
	if triplet.KVPath != "user-app-secrets/wc-acme/acme-anthropic" || triplet.Property != "api-key" {
		t.Fatalf("KeyRef must return the stamped triplet, got %+v", triplet)
	}
	wantAnthropic(t, conn, modelconn.DefaultAnthropicModel)
}

// --- ResolveCodingCredential ----------------------------------------------------

func TestResolveCodingCredential_NoSubscription_UsesTheKey_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	c.connect(t, "acme", anthropicUnitKey)
	stampConnectionTriplet(t, c.connRepo, "acme", "acme-anthropic", "user-app-secrets/wc-acme/acme-anthropic", "api-key")

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred.Ref.KVPath != "user-app-secrets/wc-acme/acme-anthropic" ||
		cred.Kind != organization.CodingCredentialConnectionKey {
		t.Fatalf("no subscription must resolve to the API key, got %+v", cred)
	}
	wantAnthropic(t, cred.Conn, modelconn.DefaultAnthropicModel)
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
	if cred.Ref.KVPath != "user-app-secrets/wc-acme/acme-anthropic-coding" ||
		cred.Kind != organization.CodingCredentialClaudeSubscription {
		t.Fatalf("a Claude Code run must mount the subscription, got %+v", cred)
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
	if cred.Ref.KVPath != "user-app-secrets/wc-acme/acme-anthropic" ||
		cred.Kind != organization.CodingCredentialConnectionKey {
		t.Fatalf("an OpenCode run must mount the API key, got %+v", cred)
	}

	// Not consulted means not consulted: a subscription broken enough to fail a
	// Claude Code run closed still leaves OpenCode on the API key.
	stampTriplet(t, c.repo, "acme", "", "", "")
	cred, err = c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeOpenCode)
	if err != nil || cred.Kind != organization.CodingCredentialConnectionKey {
		t.Fatalf("a broken subscription reached an OpenCode run: cred=%+v err=%v", cred, err)
	}
}

// An org that chose to bill its plan must never have a run quietly billed to
// API credits instead: a subscription whose mirror never landed is an ERROR.
func TestResolveCodingCredential_BrokenSubscriptionFailsClosed_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	keyAndSubscription(t, c)
	stampTriplet(t, c.repo, "acme", "acme-anthropic-coding", "", "")

	cred, err := c.conns.ResolveCodingCredential(context.Background(), "acme", orgconfig.AgentRuntimeClaudeCode)
	if err == nil {
		t.Fatalf("a broken subscription triplet must fail closed, got %+v", cred)
	}
	if !strings.Contains(err.Error(), "secret_ref_property") {
		t.Fatalf("the error must name what is missing, got: %v", err)
	}
}

func TestResolveCodingCredential_NoRowsAtAll_Errors_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	if _, err := c.conns.ResolveCodingCredential(context.Background(), "ghost", orgconfig.AgentRuntimeClaudeCode); err == nil {
		t.Fatal("an org with no connection at all must not resolve a secret ref")
	}
}
