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

// DBTEST tier — the Claude subscription beside the model connection, over the
// same real Postgres and org secret writer as anthropic_dbtest_test.go. What
// is pinned here:
//
//   - the connection and the subscription are independent rows with
//     independent vault references, so rotating the key cannot touch the
//     token;
//   - the schema itself refuses anything but a subscription token in
//     org_anthropic_credentials;
//   - a replaced credential's readers follow its new reference row at once,
//     and a save that keeps a credential keeps its reference.
//
// Which of the two a reader gets (ModelConnectionService) is pinned in
// model_connection_dbtest_test.go.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

// anthropicDBOAuthToken is shaped like a `claude setup-token` result. The probe
// is faked at the HTTP boundary, so only the SHAPE has to be realistic.
const anthropicDBOAuthToken = "sk-ant-oat01-SubscriptionTokenForCodingRuns-9f2c"

func TestSubscription_IndependentOfTheConnection_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()

	c.connect(t, "acme", anthropicUnitKey)
	c.patch(t, "acme", subscriptionPatch(anthropicDBOAuthToken))
	before, err := c.svc.Status(ctx, "acme", organization.AnthropicRoleCoding)
	if err != nil || before.CredentialKind != organization.AnthropicCredentialOAuth {
		t.Fatalf("subscription row: %+v (%v), want an oauth_token", before, err)
	}

	token := c.ref(t, "acme", organization.OrgSecretCodingAgentKey)
	key := c.ref(t, "acme", organization.OrgSecretDefaultKey)

	// Rotating the key must not disturb the subscription.
	c.connect(t, "acme", anthropicDBKey2)
	after, err := c.svc.Status(ctx, "acme", organization.AnthropicRoleCoding)
	if err != nil || !after.ConnectedAt.Equal(before.ConnectedAt) {
		t.Fatalf("key rotation mutated the subscription row: before %+v, after %+v (%v)", before, after, err)
	}
	if again := c.ref(t, "acme", organization.OrgSecretCodingAgentKey); again == nil || again.Name != token.Name || !c.vault.refs[token.Name] {
		t.Fatalf("key rotation moved the token's reference: %+v → %+v", token, again)
	}
	if rotated := c.ref(t, "acme", organization.OrgSecretDefaultKey); rotated == nil || rotated.Name == key.Name || c.vault.lastData["api-key"] != anthropicDBKey2 {
		t.Fatalf("key did not rotate: %+v → %+v", key, rotated)
	}
}

// The CHECKs are the last line: org_anthropic_credentials holds a subscription
// token and nothing else, whatever path wrote it.
func TestSubscription_SchemaRefusesAnythingElse_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	for _, tc := range []struct{ role, kind, constraint string }{
		{"coding", "api_key", "role_kind"},
		{"default", "api_key", "subscription_only"},
	} {
		err := c.db.Exec(`
			INSERT INTO org_anthropic_credentials (oc_org_id, role, credential_kind, key_prefix, key_last4, status)
			VALUES ('acme', ?, ?, 'sk-ant-x', 'wxyz', 'active')`, tc.role, tc.kind).Error
		if err == nil || !strings.Contains(err.Error(), tc.constraint) {
			t.Errorf("%s/%s: want the %s CHECK to refuse it, got %v", tc.role, tc.kind, tc.constraint, err)
		}
	}
}

// A replaced credential's readers follow its new reference row at once; a
// save that keeps a credential (a model-only edit) keeps its reference.
func TestReplace_ReadersFollowTheNewReference_DB(t *testing.T) {
	t.Parallel()
	c := newCardDB(t, http.StatusOK)
	ctx := context.Background()
	c.connect(t, "acme", anthropicUnitKey)
	c.patch(t, "acme", subscriptionPatch(anthropicDBOAuthToken))
	key := c.ref(t, "acme", organization.OrgSecretDefaultKey)
	token := c.ref(t, "acme", organization.OrgSecretCodingAgentKey)

	c.patch(t, "acme", llmPatch(orgconfig.LLMPatch{Model: "claude-haiku-4-5"}))
	if cred, err := c.conns.ResolveCodingCredential(ctx, "acme", orgconfig.AgentRuntimeClaudeCode); err != nil || cred.Ref.Name != token.Name || cred.Ref.Property != "api-key" {
		t.Fatalf("a model-only save must keep the subscription's reference: ref=%+v err=%v", cred.Ref, err)
	}
	if _, ref, err := c.conns.KeyRef(ctx, "acme"); err != nil || ref.Name != key.Name {
		t.Fatalf("a model-only save must keep the key's reference: ref=%+v err=%v", ref, err)
	}

	const token2 = "sk-ant-oat01-SubscriptionTokenForCodingRuns-second"
	c.patch(t, "acme", subscriptionPatch(token2))
	token2Ref := c.ref(t, "acme", organization.OrgSecretCodingAgentKey)
	if cred, err := c.conns.ResolveCodingCredential(ctx, "acme", orgconfig.AgentRuntimeClaudeCode); err != nil || cred.Ref.Name != token2Ref.Name || token2Ref.Name == token.Name {
		t.Fatalf("a replaced token must resolve to its new reference: ref=%+v err=%v", cred.Ref, err)
	}
	if _, ref, err := c.conns.KeyRef(ctx, "acme"); err != nil || ref.Name != key.Name {
		t.Fatalf("replacing the token must not touch the key's reference: ref=%+v err=%v", ref, err)
	}
}
