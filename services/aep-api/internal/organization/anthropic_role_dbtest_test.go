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

// DBTEST tier — the coding-agent key as an OVERRIDE on the org's default key
// (ADR-0016), over the same real Postgres + real AES-GCM store as
// anthropic_dbtest_test.go. What is pinned here is everything the composite
// (oc_org_id, role) primary key made possible, and everything it must NOT have
// broken:
//
//   - the two roles are independent rows with independent secret bytes, so
//     rotating one cannot touch the other;
//   - a coding row can only exist under an active default (the 422), and
//     cannot outlive it (the disconnect cascade);
//   - the reuse fallback and its fail-closed edge, which is the whole reason
//     the feature is worth anything: an org that asked for isolation must never
//     silently get the default key instead;
//   - EffectiveKey — the design agent's reader — stays default-only.
//
// The phase13 migration itself is pinned in migrate/; here the schema arrives
// via the same migrate.RunAll production path (dbtest.New), so every assertion
// below is also evidence the re-key landed.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// anthropicDBCodingKey is a third well-formed key, distinct from
// anthropicUnitKey (default) and anthropicDBKey2 (the rotation pin), so a test
// that mixes up two roles fails on the VALUE rather than passing by accident.
const anthropicDBCodingKey = "sk-ant-api03-CodingAgentOnlyKey-abcdefghij-4242"

// anthropicRoleService wires the real service plus the repository, which the
// secret-ref tests need in order to stamp triplets directly: dbtest runs with
// no SM-API provider, so nothing else would ever populate them.
func anthropicRoleService(t *testing.T, apiStatus int) (*organization.AnthropicCredentialService, secrets.CredentialStore, organization.OrgAnthropicRepository) {
	t.Helper()
	db := dbtest.New(t)
	store, err := secrets.NewDBStore(db, []byte(anthropicDBAESKey))
	if err != nil {
		t.Fatalf("real DBStore: %v", err)
	}
	base, _ := anthropicFakeAPI(t, apiStatus)
	repo := organization.NewOrgAnthropicRepository(db)
	svc := organization.NewAnthropicCredentialService(repo, store).WithAnthropicAPIBase(base)
	return svc, store, repo
}

func connectRole(t *testing.T, svc *organization.AnthropicCredentialService, org string, role organization.AnthropicRole, key string) *organization.AnthropicProjection {
	t.Helper()
	proj, err := svc.Connect(context.Background(), org, role, organization.AnthropicConnectRequest{APIKey: key})
	if err != nil {
		t.Fatalf("connect %s/%s: %v", org, role, err)
	}
	return proj
}

// stampTriplet fakes what the SM-API mirror would have written, so the
// resolution tests can exercise ResolveCodingSecretRef against real rows.
func stampTriplet(t *testing.T, repo organization.OrgAnthropicRepository, org string, role organization.AnthropicRole, name, kvPath, property string) {
	t.Helper()
	cols := map[string]any{
		"secret_ref_name":     name,
		"secret_ref_kv_path":  kvPath,
		"secret_ref_property": property,
	}
	if err := repo.UpdateColumns(context.Background(), org, role, cols); err != nil {
		t.Fatalf("stamp triplet %s/%s: %v", org, role, err)
	}
}

// --- the two roles are independent rows ---------------------------------------

func TestAnthropicRoles_CoexistIndependently_DB(t *testing.T) {
	t.Parallel()
	svc, store, _ := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	coding := connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBCodingKey)

	if coding.KeyLast4 != anthropicDBCodingKey[len(anthropicDBCodingKey)-4:] {
		t.Fatalf("coding projection previews the wrong key: %+v", coding)
	}

	// Two rows, two sets of bytes, under two distinct org_secrets keys.
	gotDefault, err := store.Get(ctx, "acme", "anthropic/key")
	if err != nil {
		t.Fatalf("store get default: %v", err)
	}
	gotCoding, err := store.Get(ctx, "acme", "anthropic/coding-key")
	if err != nil {
		t.Fatalf("store get coding: %v", err)
	}
	if string(gotDefault) != anthropicUnitKey {
		t.Fatalf("default bytes: got %q", string(gotDefault))
	}
	if string(gotCoding) != anthropicDBCodingKey {
		t.Fatalf("coding bytes: got %q", string(gotCoding))
	}

	// Both rows are readable, and each previews its OWN key.
	stDefault, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault)
	if err != nil {
		t.Fatalf("status default: %v", err)
	}
	if stDefault.KeyLast4 == coding.KeyLast4 {
		t.Fatal("default and coding rows preview the same key — the roles collapsed into one row")
	}
}

// Rotating the default key must not disturb an org's coding key. If the
// composite PK were wrong (or Upsert still conflicted on oc_org_id alone) this
// is where it shows: the rotation would overwrite the coding row.
func TestAnthropicRoles_RotateDefaultLeavesCodingUntouched_DB(t *testing.T) {
	t.Parallel()
	svc, store, _ := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	before := connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBCodingKey)

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicDBKey2)

	after, err := svc.Status(ctx, "acme", organization.AnthropicRoleCoding)
	if err != nil {
		t.Fatalf("coding status after default rotation: %v", err)
	}
	if after.KeyLast4 != before.KeyLast4 || !after.ConnectedAt.Equal(before.ConnectedAt) {
		t.Fatalf("default rotation mutated the coding row: before %+v, after %+v", before, after)
	}
	bytes, err := store.Get(ctx, "acme", "anthropic/coding-key")
	if err != nil || string(bytes) != anthropicDBCodingKey {
		t.Fatalf("default rotation clobbered the coding bytes: %q (%v)", string(bytes), err)
	}
	// …and the default really did rotate.
	stDefault, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault)
	if err != nil {
		t.Fatalf("status default: %v", err)
	}
	if stDefault.KeyLast4 != anthropicDBKey2[len(anthropicDBKey2)-4:] {
		t.Fatalf("default did not rotate: %+v", stDefault)
	}
}

// --- the override invariant ---------------------------------------------------

func TestAnthropicRoles_CodingWithoutDefaultRejected_DB(t *testing.T) {
	t.Parallel()
	svc, store, _ := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	_, err := svc.Connect(ctx, "acme", organization.AnthropicRoleCoding,
		organization.AnthropicConnectRequest{APIKey: anthropicDBCodingKey})
	if !errors.Is(err, organization.ErrAnthropicDefaultKeyRequired) {
		t.Fatalf("coding connect with no default: want ErrAnthropicDefaultKeyRequired, got %v", err)
	}

	// The rejection must leave nothing behind — the key was already validated
	// upstream, so a leaked store.Put here would be invisible until dispatch.
	if _, err := store.Get(ctx, "acme", "anthropic/coding-key"); !errors.Is(err, secrets.ErrSecretNotFound) {
		t.Fatalf("rejected coding connect left bytes behind: %v", err)
	}
	var nfe *organization.NotFoundError
	if _, err := svc.Status(ctx, "acme", organization.AnthropicRoleCoding); !errors.As(err, &nfe) {
		t.Fatalf("rejected coding connect left a row behind: %v", err)
	}
}

// Disconnecting the default cascades: the coding key is an override and has
// nothing to override once the default is gone. Without this, an org would sit
// in llm=null + codingLlm=set, which the projection cannot describe.
func TestAnthropicRoles_DisconnectDefaultCascades_DB(t *testing.T) {
	t.Parallel()
	svc, store, _ := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBCodingKey)

	if err := svc.Disconnect(ctx, "acme", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("disconnect default: %v", err)
	}

	var nfe *organization.NotFoundError
	for _, role := range []organization.AnthropicRole{organization.AnthropicRoleDefault, organization.AnthropicRoleCoding} {
		if _, err := svc.Status(ctx, "acme", role); !errors.As(err, &nfe) {
			t.Fatalf("row for role %s survived the cascade: %v", role, err)
		}
	}
	for _, key := range []string{"anthropic/key", "anthropic/coding-key"} {
		if _, err := store.Get(ctx, "acme", key); !errors.Is(err, secrets.ErrSecretNotFound) {
			t.Fatalf("bytes at %s survived the cascade: %v", key, err)
		}
	}
}

// The reverse is NOT a cascade: flipping the console back to "reuse the key
// above" removes only the override.
func TestAnthropicRoles_DisconnectCodingKeepsDefault_DB(t *testing.T) {
	t.Parallel()
	svc, store, _ := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBCodingKey)

	if err := svc.Disconnect(ctx, "acme", organization.AnthropicRoleCoding); err != nil {
		t.Fatalf("disconnect coding: %v", err)
	}

	var nfe *organization.NotFoundError
	if _, err := svc.Status(ctx, "acme", organization.AnthropicRoleCoding); !errors.As(err, &nfe) {
		t.Fatalf("coding row survived its own disconnect: %v", err)
	}
	if _, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault); err != nil {
		t.Fatalf("default row must survive a coding disconnect: %v", err)
	}
	if got, err := store.Get(ctx, "acme", "anthropic/key"); err != nil || string(got) != anthropicUnitKey {
		t.Fatalf("default bytes must survive a coding disconnect: %q (%v)", string(got), err)
	}

	// Idempotent: an org already reusing can be told to reuse again.
	if err := svc.Disconnect(ctx, "acme", organization.AnthropicRoleCoding); err != nil {
		t.Fatalf("second coding disconnect must be a no-op: %v", err)
	}
}

// --- the design agent's reader is unaffected ----------------------------------

func TestAnthropicRoles_EffectiveKeyIsDefaultOnly_DB(t *testing.T) {
	t.Parallel()
	svc, _, _ := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBCodingKey)

	res, err := svc.EffectiveKey(ctx, "acme")
	if err != nil {
		t.Fatalf("effective key: %v", err)
	}
	if res.Source != "org" || res.Key != anthropicUnitKey {
		t.Fatalf("the design agent must keep billing the DEFAULT key, got source=%q key=%q", res.Source, res.Key)
	}
}

// --- the reuse fallback, and its fail-closed edge -----------------------------

func TestResolveCodingSecretRef_NoCodingRow_FallsBackToDefault_DB(t *testing.T) {
	t.Parallel()
	svc, _, repo := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	stampTriplet(t, repo, "acme", organization.AnthropicRoleDefault,
		"acme-anthropic", "user-app-secrets/wc-acme/acme-anthropic", "api-key")

	ref, err := svc.ResolveCodingSecretRef(ctx, "acme")
	if err != nil {
		t.Fatalf("resolve with no coding row: %v", err)
	}
	if ref.KVPath != "user-app-secrets/wc-acme/acme-anthropic" || ref.Property != "api-key" {
		t.Fatalf("reuse must resolve to the DEFAULT triplet, got %+v", ref)
	}
}

func TestResolveCodingSecretRef_CodingRow_UsesCodingTriplet_DB(t *testing.T) {
	t.Parallel()
	svc, _, repo := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	stampTriplet(t, repo, "acme", organization.AnthropicRoleDefault,
		"acme-anthropic", "user-app-secrets/wc-acme/acme-anthropic", "api-key")
	connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBCodingKey)
	stampTriplet(t, repo, "acme", organization.AnthropicRoleCoding,
		"acme-anthropic-coding", "user-app-secrets/wc-acme/acme-anthropic-coding", "api-key")

	ref, err := svc.ResolveCodingSecretRef(ctx, "acme")
	if err != nil {
		t.Fatalf("resolve with a coding row: %v", err)
	}
	if ref.KVPath != "user-app-secrets/wc-acme/acme-anthropic-coding" {
		t.Fatalf("a configured coding key must win, got %+v", ref)
	}
}

// The point of the whole feature: an org that scoped a key to its coding agent
// must never have a run quietly billed to the default key instead. A coding row
// whose mirror never landed is an ERROR, not a fall-through.
func TestResolveCodingSecretRef_BrokenCodingTriplet_FailsClosed_DB(t *testing.T) {
	t.Parallel()
	svc, _, repo := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	stampTriplet(t, repo, "acme", organization.AnthropicRoleDefault,
		"acme-anthropic", "user-app-secrets/wc-acme/acme-anthropic", "api-key")
	// Coding row exists but its mirror never completed: name stamped, path not.
	connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBCodingKey)
	stampTriplet(t, repo, "acme", organization.AnthropicRoleCoding, "acme-anthropic-coding", "", "")

	ref, err := svc.ResolveCodingSecretRef(ctx, "acme")
	if err == nil {
		t.Fatalf("a broken coding triplet must fail closed, got %+v", ref)
	}
	if !strings.Contains(err.Error(), "secret_ref_kv_path") {
		t.Fatalf("the error must name what is missing, got: %v", err)
	}
	// Specifically: it must NOT have silently resolved to the default key.
	if strings.Contains(err.Error(), "user-app-secrets/wc-acme/acme-anthropic\"") {
		t.Fatalf("resolver leaked the default path into a coding failure: %v", err)
	}
}

func TestResolveCodingSecretRef_NoRowsAtAll_Errors_DB(t *testing.T) {
	t.Parallel()
	svc, _, _ := anthropicRoleService(t, http.StatusOK)

	if _, err := svc.ResolveCodingSecretRef(context.Background(), "ghost"); err == nil {
		t.Fatal("an org with no Anthropic key at all must not resolve a secret ref")
	}
}

// --- credential kind: API key vs Claude Code OAuth token ----------------------

// anthropicDBOAuthToken is shaped like a `claude setup-token` result. The probe
// is faked at the HTTP boundary, so only the SHAPE has to be realistic.
const anthropicDBOAuthToken = "sk-ant-oat01-SubscriptionTokenForCodingRuns-9f2c"

func TestAnthropicRoles_CodingAcceptsOAuthToken_DB(t *testing.T) {
	t.Parallel()
	svc, store, _ := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	proj := connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBOAuthToken)

	if proj.CredentialKind != organization.AnthropicCredentialOAuth {
		t.Fatalf("an sk-ant-oat token must be stored as an OAuth credential, got %q", proj.CredentialKind)
	}
	// The default row is unaffected and still an API key.
	def, err := svc.Status(ctx, "acme", organization.AnthropicRoleDefault)
	if err != nil {
		t.Fatalf("status default: %v", err)
	}
	if def.CredentialKind != organization.AnthropicCredentialAPIKey {
		t.Fatalf("the default key must stay an api_key, got %q", def.CredentialKind)
	}
	if got, err := store.Get(ctx, "acme", "anthropic/coding-key"); err != nil || string(got) != anthropicDBOAuthToken {
		t.Fatalf("token bytes: %q (%v)", string(got), err)
	}
}

// The design agent is an AI SDK model call and cannot present a bearer token, so
// an OAuth token as the ORG's key would leave every non-coding reader unable to
// authenticate. Rejected up front rather than discovered by a failing turn.
func TestAnthropicRoles_DefaultRejectsOAuthToken_DB(t *testing.T) {
	t.Parallel()
	svc, _, _ := anthropicRoleService(t, http.StatusOK)

	_, err := svc.Connect(context.Background(), "acme", organization.AnthropicRoleDefault,
		organization.AnthropicConnectRequest{APIKey: anthropicDBOAuthToken})
	if got := anthropicValidationCode(t, err); got != "anthropic_oauth_token_coding_only" {
		t.Fatalf("code: got %q, want anthropic_oauth_token_coding_only (err %v)", got, err)
	}
}

// The whole reason the kind is persisted: dispatch reads the row, never the
// bytes, so this is the only thing that can tell it which variable to mount.
// Getting it wrong means Claude Code ignores the token (ANTHROPIC_API_KEY
// outranks CLAUDE_CODE_OAUTH_TOKEN) and bills the default key in silence.
func TestResolveCodingSecretRef_OAuthTokenMountsClaudeCodeVar_DB(t *testing.T) {
	t.Parallel()
	svc, _, repo := anthropicRoleService(t, http.StatusOK)
	ctx := context.Background()

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	stampTriplet(t, repo, "acme", organization.AnthropicRoleDefault,
		"acme-anthropic", "user-app-secrets/wc-acme/acme-anthropic", "api-key")
	connectRole(t, svc, "acme", organization.AnthropicRoleCoding, anthropicDBOAuthToken)
	stampTriplet(t, repo, "acme", organization.AnthropicRoleCoding,
		"acme-anthropic-coding", "user-app-secrets/wc-acme/acme-anthropic-coding", "api-key")

	ref, err := svc.ResolveCodingSecretRef(ctx, "acme")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ref.EnvVar != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Fatalf("an OAuth token must be mounted as CLAUDE_CODE_OAUTH_TOKEN, got %q", ref.EnvVar)
	}
	if ref.KVPath != "user-app-secrets/wc-acme/acme-anthropic-coding" {
		t.Fatalf("wrong vault path: %+v", ref)
	}
}

// …and reuse still mounts the API-key variable, so adding OAuth support cannot
// have changed what an org that never configured a coding key receives.
func TestResolveCodingSecretRef_ReuseMountsApiKeyVar_DB(t *testing.T) {
	t.Parallel()
	svc, _, repo := anthropicRoleService(t, http.StatusOK)

	connectRole(t, svc, "acme", organization.AnthropicRoleDefault, anthropicUnitKey)
	stampTriplet(t, repo, "acme", organization.AnthropicRoleDefault,
		"acme-anthropic", "user-app-secrets/wc-acme/acme-anthropic", "api-key")

	ref, err := svc.ResolveCodingSecretRef(context.Background(), "acme")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ref.EnvVar != "ANTHROPIC_API_KEY" {
		t.Fatalf("reuse must mount ANTHROPIC_API_KEY, got %q", ref.EnvVar)
	}
}
