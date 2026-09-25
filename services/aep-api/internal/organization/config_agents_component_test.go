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

// COMPONENT tier — the AI agents card on GET/PATCH /config: the `llm` and
// `agents` sections, over the same real handler chain + real services + real
// Postgres as config_component_test.go (whose harness this reuses).
//
// What these rows pin, on the wire:
//   - `agents` is never null, and reads the platform defaults until someone
//     chooses;
//   - one Save is one unit of work: a failure anywhere writes nothing;
//   - the one rule — a subscription needs Claude Code and an API key — and the
//     deletions that follow from it (OpenCode, disconnect, reset);
//   - the refusals, each on its section with its own code;
//   - only a runtime the installation can run is selectable, and an org
//     already on one it lost still reads and saves.
package organization_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/orgconfig"
)

const goodToken = "sk-ant-oat01-CONFIGsubscriptionTokenABCDEFGH"

func subscribe(token string) string {
	return `{"agents":{"subscription":{"kind":"claude","token":"` + token + `"}}}`
}

// agentsOf reads the agents section of a /config body.
func agentsOf(t *testing.T, body []byte) map[string]any {
	t.Helper()
	m := decodeCfg(t, body)
	a, ok := m["agents"].(map[string]any)
	if !ok {
		t.Fatalf("agents must always be an object: %v", m["agents"])
	}
	return a
}

func (c *configHarness) count(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := c.db.Raw(sql, args...).Scan(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// cardRows counts every row the card writes for org: credential rows, the
// setting row, and secret bytes.
func (c *configHarness) cardRows(t *testing.T, org string) (creds, settings, secrets int64) {
	t.Helper()
	return c.count(t, `SELECT count(*) FROM org_anthropic_credentials WHERE oc_org_id = ?`, org),
		c.count(t, `SELECT count(*) FROM org_agent_settings WHERE oc_org_id = ?`, org),
		c.count(t, `SELECT count(*) FROM org_secrets WHERE oc_org_id = ? AND key LIKE 'anthropic/%'`, org)
}

// refused asserts a 400 on body.<section> carrying code.
func refused(t *testing.T, code int, body, section, wantCode string) {
	t.Helper()
	if code != 400 {
		t.Fatalf("want 400, got %d body=%s", code, body)
	}
	if !strings.Contains(body, `"body.`+section+`"`) {
		t.Fatalf("the refusal must point at body.%s: %s", section, body)
	}
	if !strings.Contains(body, `"code":"`+wantCode+`"`) {
		t.Fatalf("the refusal must carry code %s: %s", wantCode, body)
	}
}

func TestConfigAgents_FreshOrgReadsTheDefaults(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	a := agentsOf(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	if a["model"] != "claude-sonnet-5" || a["runtime"] != "claude-code" {
		t.Fatalf("defaults drifted: %v", a)
	}
	for _, k := range []string{"subscription", "updatedAt", "updatedBy"} {
		v, present := a[k]
		if !present || v != nil {
			t.Fatalf("%s must be present and null on a fresh org: %v", k, a)
		}
	}
	if got := fmt.Sprint(a["availableRuntimes"]); got != "[claude-code opencode]" {
		t.Fatalf("availableRuntimes = %s, want both runtimes on an installation with both images", got)
	}
}

// An installation deployed without the OpenCode runner image says so on GET and
// refuses a save choosing OpenCode, writing nothing: accepted, it would fail
// every coding dispatch after it.
func TestConfigAgents_AnInstallationWithoutOpenCodeRefusesIt(t *testing.T) {
	t.Parallel()
	c := newConfigHarnessRuntimes(t, []orgconfig.AgentRuntime{orgconfig.AgentRuntimeClaudeCode})

	a := agentsOf(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	if got := fmt.Sprint(a["availableRuntimes"]); got != "[claude-code]" {
		t.Fatalf("availableRuntimes = %s, want [claude-code]", got)
	}

	resp := c.h.AsOrg("acme").Patch(configPath, `{"agents":{"runtime":"opencode"}}`)
	refused(t, resp.Code, resp.Body.String(), "agents", "agents_runtime_unavailable")
	if _, settings, _ := c.cardRows(t, "acme"); settings != 0 {
		t.Fatalf("a refused runtime left a setting row")
	}
}

// An org that chose OpenCode before the installation lost its image: GET still
// answers, naming the runtime it chose (never substituted) beside the runtimes
// on offer, and the org can change its model or move to Claude Code.
func TestConfigAgents_AnOrgOnALostRuntimeStillReadsAndSaves(t *testing.T) {
	t.Parallel()
	c := newConfigHarnessRuntimes(t, []orgconfig.AgentRuntime{orgconfig.AgentRuntimeClaudeCode})
	if err := c.db.Exec(`INSERT INTO org_agent_settings (oc_org_id, runtime, model, updated_by, updated_at)
		VALUES ('acme', 'opencode', 'claude-sonnet-5', 'ada', now())`).Error; err != nil {
		t.Fatalf("seed an org on OpenCode: %v", err)
	}

	resp := c.h.AsOrg("acme").Get(configPath)
	if resp.Code != 200 {
		t.Fatalf("get: %d %s", resp.Code, resp.Body.String())
	}
	a := agentsOf(t, resp.Body.Bytes())
	if a["runtime"] != "opencode" || fmt.Sprint(a["availableRuntimes"]) != "[claude-code]" {
		t.Fatalf("agents = %v, want runtime opencode beside availableRuntimes [claude-code]", a)
	}

	resp = c.h.AsOrg("acme").Patch(configPath, `{"agents":{"model":"claude-haiku-4-5"}}`)
	if resp.Code != 200 {
		t.Fatalf("a model-only save was refused over the lost runtime: %d %s", resp.Code, resp.Body.String())
	}
	resp = c.h.AsOrg("acme").Patch(configPath, `{"agents":{"runtime":"claude-code"}}`)
	if resp.Code != 200 || agentsOf(t, resp.Body.Bytes())["runtime"] != "claude-code" {
		t.Fatalf("moving to Claude Code: %d %s", resp.Code, resp.Body.String())
	}
}

// Key, model, runtime and subscription in ONE save, then read back.
func TestConfigAgents_OneSaveWritesTheWholeCard(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},
		"agents":{"model":"claude-haiku-4-5","runtime":"claude-code","subscription":{"kind":"claude","token":"`+goodToken+`"}}}`)
	if resp.Code != 200 {
		t.Fatalf("save: %d %s", resp.Code, resp.Body.String())
	}
	a := agentsOf(t, resp.Body.Bytes())
	if a["model"] != "claude-haiku-4-5" || a["updatedBy"] == nil {
		t.Fatalf("agents: %v", a)
	}
	sub, ok := a["subscription"].(map[string]any)
	if !ok || sub["kind"] != "claude" || sub["keyLast4"] != goodToken[len(goodToken)-4:] {
		t.Fatalf("subscription must project masked: %v", a["subscription"])
	}
	for _, secret := range []string{goodAnthKey, goodToken} {
		if strings.Contains(resp.Body.String(), secret) {
			t.Fatalf("a write-only credential was echoed: %s", resp.Body.String())
		}
	}
	// A later model change keeps the token without it being sent again.
	resp = c.h.AsOrg("acme").Patch(configPath, `{"agents":{"model":"claude-sonnet-5"}}`)
	if resp.Code != 200 || agentsOf(t, resp.Body.Bytes())["subscription"] == nil {
		t.Fatalf("a model change dropped the subscription: %d %s", resp.Code, resp.Body.String())
	}
}

// The unit of work: a failure after the key and its bytes are written, inside
// the same save, rolls ALL of it back. The failure is injected in the database
// itself — a trigger that refuses the setting row — so the real transaction is
// what is under test.
func TestConfigAgents_AFailureMidSaveWritesNothing(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	for _, sql := range []string{
		`CREATE FUNCTION refuse_agent_settings() RETURNS trigger AS $$
		 BEGIN RAISE EXCEPTION 'injected failure'; END $$ LANGUAGE plpgsql`,
		`CREATE TRIGGER refuse_agent_settings BEFORE INSERT OR UPDATE ON org_agent_settings
		 FOR EACH ROW EXECUTE FUNCTION refuse_agent_settings()`,
	} {
		if err := c.db.Exec(sql).Error; err != nil {
			t.Fatalf("install failure: %v", err)
		}
	}

	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},
		"agents":{"model":"claude-haiku-4-5","subscription":{"kind":"claude","token":"`+goodToken+`"}}}`)
	if resp.Code != 500 {
		t.Fatalf("want the injected failure as a 500, got %d %s", resp.Code, resp.Body.String())
	}
	if creds, settings, secrets := c.cardRows(t, "acme"); creds+settings+secrets != 0 {
		t.Fatalf("a failed save left rows behind: credentials=%d settings=%d secrets=%d", creds, settings, secrets)
	}
}

// A key Anthropic rejects writes nothing, including the sections beside it.
func TestConfigAgents_ARejectedKeyWritesNothing(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.anth.rejectOnly(goodAnthKey)

	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},"agents":{"model":"claude-haiku-4-5"}}`)
	refused(t, resp.Code, resp.Body.String(), "llm", "anthropic_key_invalid")
	if creds, settings, secrets := c.cardRows(t, "acme"); creds+settings+secrets != 0 {
		t.Fatalf("a rejected key left rows behind: credentials=%d settings=%d secrets=%d", creds, settings, secrets)
	}
}

func TestConfigAgents_OpenCodeDeletesTheToken(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("key: %d %s", r.Code, r.Body.String())
	}
	if r := c.h.AsOrg("acme").Patch(configPath, subscribe(goodToken)); r.Code != 200 {
		t.Fatalf("subscribe: %d %s", r.Code, r.Body.String())
	}

	resp := c.h.AsOrg("acme").Patch(configPath, `{"agents":{"runtime":"opencode"}}`)
	if resp.Code != 200 {
		t.Fatalf("opencode: %d %s", resp.Code, resp.Body.String())
	}
	a := agentsOf(t, resp.Body.Bytes())
	if a["runtime"] != "opencode" || a["subscription"] != nil {
		t.Fatalf("OpenCode must leave no subscription: %v", a)
	}
	if n := c.count(t, `SELECT count(*) FROM org_secrets WHERE oc_org_id = 'acme' AND key = 'anthropic/coding-key'`); n != 0 {
		t.Fatalf("the token's bytes survived the switch to OpenCode")
	}
}

func TestConfigAgents_DisconnectCascadesToTheToken(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},"agents":{"subscription":{"kind":"claude","token":"`+goodToken+`"}}}`); r.Code != 200 {
		t.Fatalf("setup: %d %s", r.Code, r.Body.String())
	}

	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":null}`)
	if resp.Code != 200 {
		t.Fatalf("disconnect: %d %s", resp.Code, resp.Body.String())
	}
	m := decodeCfg(t, resp.Body.Bytes())
	if m["llm"] != nil || agentsOf(t, resp.Body.Bytes())["subscription"] != nil {
		t.Fatalf("the key and the subscription must both be gone: %v", m)
	}
	if creds, _, secrets := c.cardRows(t, "acme"); creds+secrets != 0 {
		t.Fatalf("disconnect left credentials=%d secrets=%d", creds, secrets)
	}
}

// Disconnecting leaves a trace the onboarding gate reads: the org HAD a key.
// Connecting a key again clears it. The trace lives on the organizations row,
// which production's orgensure creates on the first request; this harness runs
// orgensure without a service, so the row is seeded.
func TestConfigAgents_DisconnectIsRememberedUntilAKeyReturns(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if err := c.db.Create(&organization.Organization{UUID: uuid.New(), Name: "acme"}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if m := decodeCfg(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes()); m["llmDisconnectedAt"] != nil {
		t.Fatalf("an org that never had a key reads as disconnected: %v", m)
	}
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("key: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":null}`)
	if at, ok := decodeCfg(t, resp.Body.Bytes())["llmDisconnectedAt"].(string); !ok || at == "" {
		t.Fatalf("llmDisconnectedAt must be set after a disconnect: %s", resp.Body.String())
	}
	resp = c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey2))
	if m := decodeCfg(t, resp.Body.Bytes()); m["llmDisconnectedAt"] != nil {
		t.Fatalf("connecting a key must clear llmDisconnectedAt: %v", m)
	}
}

func TestConfigAgents_ResetDeletesTheSettingAndTheToken(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},
		"agents":{"model":"claude-haiku-4-5","subscription":{"kind":"claude","token":"`+goodToken+`"}}}`); r.Code != 200 {
		t.Fatalf("setup: %d %s", r.Code, r.Body.String())
	}

	resp := c.h.AsOrg("acme").Patch(configPath, `{"agents":null}`)
	if resp.Code != 200 {
		t.Fatalf("reset: %d %s", resp.Code, resp.Body.String())
	}
	m := decodeCfg(t, resp.Body.Bytes())
	a := agentsOf(t, resp.Body.Bytes())
	if a["model"] != "claude-sonnet-5" || a["updatedBy"] != nil || a["subscription"] != nil {
		t.Fatalf("reset must return to the defaults with no subscription: %v", a)
	}
	if m["llm"] == nil {
		t.Fatal("resetting the card disconnected the API key")
	}
}

// Every refusal of the one rule, and the wrong kind of credential in each field.
func TestConfigAgents_Refusals(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	// No key anywhere.
	r := c.h.AsOrg("acme").Patch(configPath, subscribe(goodToken))
	refused(t, r.Code, r.Body.String(), "agents", "agents_subscription_requires_api_key")

	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("key: %d %s", r.Code, r.Body.String())
	}
	// Disconnecting the key in the same save as a new token.
	r = c.h.AsOrg("acme").Patch(configPath, `{"llm":null,"agents":{"subscription":{"kind":"claude","token":"`+goodToken+`"}}}`)
	refused(t, r.Code, r.Body.String(), "agents", "agents_subscription_requires_api_key")
	// OpenCode and a new token in one save.
	r = c.h.AsOrg("acme").Patch(configPath, `{"agents":{"runtime":"opencode","subscription":{"kind":"claude","token":"`+goodToken+`"}}}`)
	refused(t, r.Code, r.Body.String(), "agents", "agents_subscription_requires_claude_code")
	// An API key pasted as the subscription.
	r = c.h.AsOrg("acme").Patch(configPath, subscribe(goodAnthKey2))
	refused(t, r.Code, r.Body.String(), "agents", "agents_subscription_token_required")
	// A subscription token pasted as the org's key.
	r = c.h.AsOrg("acme").Patch(configPath, llmConnect(goodToken))
	refused(t, r.Code, r.Body.String(), "llm", "anthropic_oauth_token_coding_only")

	// None of it was written: still the key alone, on the defaults.
	a := agentsOf(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	if a["runtime"] != "claude-code" || a["subscription"] != nil || a["updatedBy"] != nil {
		t.Fatalf("a refused save wrote something: %v", a)
	}
}

// A blank credential is refused on its section and writes nothing: it cannot
// stand in for the key the subscription needs, and it cannot answer 200 while
// storing nothing. Whitespace passes the contract's minLength, so the service
// is what refuses it.
func TestConfigAgents_BlankCredentialsAreRefused(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	// A blank key alone.
	r := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"   "}}`)
	refused(t, r.Code, r.Body.String(), "llm", "anthropic_key_missing")
	// A blank key beside a real subscription token.
	r = c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"   "},"agents":{"subscription":{"kind":"claude","token":"`+goodToken+`"}}}`)
	refused(t, r.Code, r.Body.String(), "llm", "anthropic_key_missing")
	if creds, settings, secrets := c.cardRows(t, "acme"); creds+settings+secrets != 0 {
		t.Fatalf("a blank key left rows behind: credentials=%d settings=%d secrets=%d", creds, settings, secrets)
	}

	// A blank token, with a key connected.
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("key: %d %s", r.Code, r.Body.String())
	}
	r = c.h.AsOrg("acme").Patch(configPath, subscribe("   "))
	refused(t, r.Code, r.Body.String(), "agents", "anthropic_key_missing")
	if a := agentsOf(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes()); a["subscription"] != nil {
		t.Fatalf("a blank token was stored: %v", a)
	}

	// An empty string never reaches the service: the contract's minLength refuses it.
	for _, body := range []string{`{"llm":{"kind":"anthropic","apiKey":""}}`, subscribe("")} {
		if r := c.h.AsOrg("acme").Patch(configPath, body); r.Code != 400 {
			t.Errorf("%s: want 400, got %d %s", body, r.Code, r.Body.String())
		}
	}
}

// The request validator refuses a value outside the contract's enums before
// any handler runs; the section never persists one.
func TestConfigAgents_ValuesOutsideTheEnumsAreRefused(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	for _, body := range []string{
		`{"agents":{"runtime":"cursor"}}`,
		`{"agents":{"model":"claude-opus-5"}}`,
		`{"agents":{"subscription":{"kind":"chatgpt","token":"x"}}}`,
	} {
		if r := c.h.AsOrg("acme").Patch(configPath, body); r.Code != 400 {
			t.Errorf("%s: want 400, got %d %s", body, r.Code, r.Body.String())
		}
	}
	if _, settings, _ := c.cardRows(t, "acme"); settings != 0 {
		t.Fatal("a refused value was persisted")
	}
}
