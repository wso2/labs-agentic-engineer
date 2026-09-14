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

// COMPONENT tier — the `codingAgent` section of GET/PATCH /config, over the same
// real handler chain + real services + real Postgres as
// config_component_test.go (whose harness this reuses).
//
// The section is the odd one out: no secret, no external probe, and a projection
// that DOES echo what was written. Two consequences are only observable on the
// wire, which is why they are pinned here rather than in the service's unit
// tests — the section is always present even for an org that never touched it,
// and a runtime the platform cannot run is refused with a reason rather than
// quietly replaced by the one it can.
package organization_test

import (
	"strings"
	"testing"
)

func codingAgentSection(t *testing.T, body []byte) map[string]any {
	t.Helper()
	m := decodeCfg(t, body)
	v, present := m["codingAgent"]
	if !present {
		t.Fatal("codingAgent must always be present on the wire; absent is indistinguishable from an old server")
	}
	section, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("codingAgent must be an object, never null — every org has an effective runtime and model; got %v", v)
	}
	return section
}

// A fresh org is not "unconfigured": it is on the platform defaults, and the
// section says so with a null author. Every other section here reads null when
// nothing is set; this one cannot, because there is no such thing as an org with
// no runtime.
func TestConfigComponent_L1_FreshOrgReadsThePlatformDefaults(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	resp := c.h.AsOrg("acme").Get(configPath)
	if resp.Code != 200 {
		t.Fatalf("get: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	section := codingAgentSection(t, resp.Body.Bytes())
	if section["runtime"] != "claude-code" || section["model"] != "claude-sonnet-5" {
		t.Fatalf("a fresh org must read the platform defaults, got %v", section)
	}
	if section["updatedBy"] != nil || section["updatedAt"] != nil {
		t.Fatalf("nobody has chosen yet, so the section must claim no author: %v", section)
	}
}

// The model is the field an org changes; the runtime is the one it rarely
// touches. Sending only the model must not reset the runtime, which is why this
// is the one section whose fields are individually optional.
func TestConfigComponent_L2_SettingOnlyTheModelKeepsTheRuntime(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	resp := c.h.AsOrg("acme").Patch(configPath, `{"codingAgent":{"model":"claude-haiku-4-5"}}`)
	if resp.Code != 200 {
		t.Fatalf("patch: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	// The PATCH response is the post-write projection — equal to a following GET.
	section := codingAgentSection(t, resp.Body.Bytes())
	if section["model"] != "claude-haiku-4-5" {
		t.Fatalf("model = %v", section["model"])
	}
	if section["runtime"] != "claude-code" {
		t.Fatalf("runtime = %v, want the runtime the org already had", section["runtime"])
	}
	// Somebody chose it now, and the section says who — the compensating control
	// for this endpoint's coarse RBAC.
	if section["updatedBy"] == nil || section["updatedAt"] == nil {
		t.Fatalf("a chosen setting must carry its author and time: %v", section)
	}

	again := codingAgentSection(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	if again["model"] != "claude-haiku-4-5" {
		t.Fatalf("the choice did not survive the write: %v", again)
	}
}

// `opencode` IS in the contract's enum, so the request validator lets it
// through; refusing it is this platform's own statement about what it ships. It
// must be refused rather than substituted — running the one runtime we do have
// would bill an org for a runtime it did not choose and never tell it.
func TestConfigComponent_L3_OpenCodeIsRefusedWithAReason(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	resp := c.h.AsOrg("acme").Patch(configPath, `{"codingAgent":{"runtime":"opencode"}}`)
	if resp.Code != 400 {
		t.Fatalf("opencode: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	if !strings.Contains(body, "body.codingAgent") {
		t.Fatalf("the error must point at the codingAgent section: %s", body)
	}
	// The reason, not just a rejection: a client that is told "invalid" learns
	// nothing about why the choice it was offered does not work.
	for _, want := range []string{"opencode", "claude-code"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the reason does not name %q: %s", want, body)
		}
	}

	// Nothing was written: the org is still on the defaults, with no author.
	section := codingAgentSection(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	if section["updatedBy"] != nil {
		t.Fatalf("a refused patch still wrote something: %v", section)
	}
}

// A model outside the enum never reaches the service — the request validator
// rejects it at the edge, which is exactly what the contract's enum is for. The
// row exists so the two layers cannot both be assumed to be doing the job.
func TestConfigComponent_L4_AnUnpricedModelIsRejectedAtTheEdge(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	resp := c.h.AsOrg("acme").Patch(configPath, `{"codingAgent":{"model":"claude-opus-5"}}`)
	if resp.Code != 400 {
		t.Fatalf("unpriced model: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	section := codingAgentSection(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	if section["updatedBy"] != nil {
		t.Fatalf("a rejected model still wrote something: %v", section)
	}
}

// null RESETS to the platform defaults, and resetting is not the same as never
// having chosen: the author goes back to null, so the console can say which of
// the two the org is in.
func TestConfigComponent_L5_NullResetsToTheDefaultsAndClearsTheAuthor(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, `{"codingAgent":{"model":"claude-haiku-4-5"}}`); r.Code != 200 {
		t.Fatalf("set: %d %s", r.Code, r.Body.String())
	}

	resp := c.h.AsOrg("acme").Patch(configPath, `{"codingAgent":null}`)
	if resp.Code != 200 {
		t.Fatalf("reset: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	section := codingAgentSection(t, resp.Body.Bytes())
	if section["runtime"] != "claude-code" || section["model"] != "claude-sonnet-5" {
		t.Fatalf("reset did not restore the platform defaults: %v", section)
	}
	if section["updatedBy"] != nil || section["updatedAt"] != nil {
		t.Fatalf("after a reset the section must claim no author again: %v", section)
	}
}

// Sections are independent, and this one has no probe — so a patch that carries
// it alongside a section that DOES probe must not be able to half-apply.
func TestConfigComponent_L6_ARejectedSiblingSectionLeavesTheSettingAlone(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	// A coding-agent key with no default key is refused (K2). The codingAgent
	// section in the same request is perfectly valid, and must still not land.
	resp := c.h.AsOrg("acme").Patch(configPath,
		`{"codingAgent":{"model":"claude-haiku-4-5"},"codingLlm":{"kind":"anthropic","apiKey":"`+goodCodingKey+`"}}`)
	if resp.Code != 400 {
		t.Fatalf("want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	section := codingAgentSection(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	if section["model"] != "claude-sonnet-5" || section["updatedBy"] != nil {
		t.Fatalf("the codingAgent section was applied despite the patch failing: %v", section)
	}
}

// The org is derived from the verified JWT and nothing else, so one org's choice
// is invisible to another. Cheap to state, and the kind of thing a settings
// table gets wrong exactly once.
func TestConfigComponent_L7_TheSettingIsPerOrganization(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, `{"codingAgent":{"model":"claude-haiku-4-5"}}`); r.Code != 200 {
		t.Fatalf("acme set: %d %s", r.Code, r.Body.String())
	}

	other := codingAgentSection(t, c.h.AsOrg("globex").Get(configPath).Body.Bytes())
	if other["model"] != "claude-sonnet-5" || other["updatedBy"] != nil {
		t.Fatalf("globex sees acme's setting: %v", other)
	}
}
