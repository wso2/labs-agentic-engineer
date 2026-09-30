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

// COMPONENT tier — the SRE model connection (`sreLlm`) on GET/PATCH /config,
// over the same real handler chain, real services, real Postgres and fake
// model endpoint as config_llm_component_test.go. Also that both connections'
// OnChange follow a committed save, since either can decide which connection
// the SRE agent runs on.
package organization_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

const (
	sreComponentKey  = "sre-component-key-0123456789"
	sreComponentKey2 = "sre-component-key-second-9876"
)

func sreSet(baseURL, key, model string) string {
	return `{"sreLlm":{"baseURL":"` + baseURL + `","apiKey":"` + key + `","model":"` + model + `"}}`
}

func sreLlmOf(t *testing.T, body []byte) map[string]any {
	t.Helper()
	sre, ok := decodeCfg(t, body)["sreLlm"].(map[string]any)
	if !ok {
		t.Fatalf("sreLlm is not an object: %s", body)
	}
	return sre
}

// changes records the orgs an OnChange callback was called with.
type changes struct {
	mu   sync.Mutex
	orgs []string
}

func (c *changes) record(org string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.orgs = append(c.orgs, org)
}

func (c *changes) seen() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.orgs...)
}

func TestConfigSreModel_FreshOrgHasNeither(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	cfg := decodeCfg(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	for _, key := range []string{"sreLlm", "sreAgent"} {
		if v, ok := cfg[key]; !ok || v != nil {
			t.Errorf("%s = %v (present=%v), want null", key, v, ok)
		}
	}
}

func TestConfigSreModel_SaveProbesWithBearerAndProjectsThePreviewOnly(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	var sreChanges changes
	c.sre.OnChange(sreChanges.record)

	resp := c.h.AsOrg("acme").Patch(configPath, sreSet("https://gw.example.com/v1/", sreComponentKey, "glm-5.3"))
	if resp.Code != http.StatusOK {
		t.Fatalf("save: %d %s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), sreComponentKey) {
		t.Fatalf("the response echoes the key: %s", resp.Body.String())
	}
	sre := sreLlmOf(t, resp.Body.Bytes())
	if sre["baseURL"] != "https://gw.example.com/v1" || sre["host"] != "gw.example.com" || sre["model"] != "glm-5.3" ||
		sre["keyPreview"] != "sre-…6789" || sre["updatedBy"] == "" {
		t.Fatalf("sreLlm = %v", sre)
	}
	var bearer bool
	for _, r := range c.model.seen() {
		if r.Host == "gw.example.com" && r.Authorization == "Bearer "+sreComponentKey && r.APIKey == "" {
			bearer = true
		}
	}
	if !bearer {
		t.Fatalf("no Bearer probe of gw.example.com: %+v", c.model.seen())
	}
	if got := sreChanges.seen(); len(got) != 1 || got[0] != "acme" {
		t.Errorf("OnChange calls = %v, want [acme]", got)
	}

	again := sreLlmOf(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())
	if again["host"] != "gw.example.com" || again["keyPreview"] != sre["keyPreview"] {
		t.Errorf("GET sreLlm = %v, want what the save returned", again)
	}
}

func TestConfigSreModel_HostChangeNeedsTheKey(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, sreSet("https://gw.example.com/v1", sreComponentKey, "glm-5.3")); r.Code != http.StatusOK {
		t.Fatalf("save: %d %s", r.Code, r.Body.String())
	}

	r := c.h.AsOrg("acme").Patch(configPath, `{"sreLlm":{"baseURL":"https://other.example.com/v1"}}`)
	refused(t, r.Code, r.Body.String(), "sreLlm", "llm_key_required_for_new_host")

	r = c.h.AsOrg("acme").Patch(configPath, `{"sreLlm":{"baseURL":"https://other.example.com/v1","apiKey":"`+sreComponentKey2+`"}}`)
	if r.Code != http.StatusOK {
		t.Fatalf("move with a key: %d %s", r.Code, r.Body.String())
	}
	if sre := sreLlmOf(t, r.Body.Bytes()); sre["host"] != "other.example.com" || sre["model"] != "glm-5.3" {
		t.Errorf("sreLlm = %v, want other.example.com with the stored model", sre)
	}
}

func TestConfigSreModel_ARejectedKeyWritesNothing(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.model.setStatus(http.StatusUnauthorized)

	r := c.h.AsOrg("acme").Patch(configPath, sreSet("https://gw.example.com/v1", sreComponentKey, "glm-5.3"))
	refused(t, r.Code, r.Body.String(), "sreLlm", "llm_key_rejected")
	if n := c.count(t, `SELECT count(*) FROM org_sre_model_connections`); n != 0 {
		t.Errorf("rows = %d, want 0", n)
	}
	if n := c.count(t, `SELECT count(*) FROM org_secrets WHERE key = 'sre-model/key'`); n != 0 {
		t.Errorf("key rows = %d, want 0", n)
	}
}

// A refused sreLlm leaves the rest of the patch unwritten too.
func TestConfigSreModel_ARefusalAbortsTheWholePatch(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	body := `{"llm":{"kind":"openai-compatible","baseURL":"https://ollama.com/v1","apiKey":"` + ollamaKey + `","model":"gpt-oss:20b"},
		"agents":{"runtime":"opencode"},"sreLlm":{"baseURL":"http://gw.example.com/v1","apiKey":"` + sreComponentKey + `","model":"glm-5.3"}}`
	r := c.h.AsOrg("acme").Patch(configPath, body)
	refused(t, r.Code, r.Body.String(), "sreLlm", "llm_base_url_invalid")
	if n := c.count(t, `SELECT count(*) FROM org_model_connections`); n != 0 {
		t.Errorf("the llm section was written despite the sreLlm refusal: %d rows", n)
	}
}

func TestConfigSreModel_TheOldShapeIsRefusedAtTheEdge(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	r := c.h.AsOrg("acme").Patch(configPath, `{"sreLlm":{"provider":"openai","model":"gpt-4o-mini","apiKey":"`+sreComponentKey+`"}}`)
	if r.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for the retired provider field, got %d %s", r.Code, r.Body.String())
	}
}

func TestConfigSreModel_NullClears(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	var sreChanges changes
	c.sre.OnChange(sreChanges.record)
	if r := c.h.AsOrg("acme").Patch(configPath, sreSet("https://gw.example.com/v1", sreComponentKey, "glm-5.3")); r.Code != http.StatusOK {
		t.Fatalf("save: %d %s", r.Code, r.Body.String())
	}

	r := c.h.AsOrg("acme").Patch(configPath, `{"sreLlm":null}`)
	if r.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", r.Code, r.Body.String())
	}
	if v := decodeCfg(t, r.Body.Bytes())["sreLlm"]; v != nil {
		t.Errorf("sreLlm after null = %v, want null", v)
	}
	if n := c.count(t, `SELECT count(*) FROM org_secrets WHERE key = 'sre-model/key'`); n != 0 {
		t.Errorf("key rows after null = %d, want 0", n)
	}
	if got := sreChanges.seen(); len(got) != 2 {
		t.Errorf("OnChange calls = %v, want one per committed change", got)
	}
}

// The org connection's saves and deletes run OnChange too: either can flip
// its SREAgent capability. A runtime-only save changes no connection.
func TestConfigSreModel_OrgConnectionChangesRunOnChange(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	var orgChanges changes
	c.conns.OnChange(orgChanges.record)

	if r := c.h.AsOrg("acme").Patch(configPath, onOllama(ollamaKey)); r.Code != http.StatusOK {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}
	if r := c.h.AsOrg("acme").Patch(configPath, `{"agents":{"runtime":"opencode"}}`); r.Code != http.StatusOK {
		t.Fatalf("runtime-only save: %d %s", r.Code, r.Body.String())
	}
	if r := c.h.AsOrg("acme").Patch(configPath, `{"llm":null}`); r.Code != http.StatusOK {
		t.Fatalf("disconnect: %d %s", r.Code, r.Body.String())
	}
	if got := orgChanges.seen(); len(got) != 2 || got[0] != "acme" || got[1] != "acme" {
		t.Errorf("OnChange calls = %v, want [acme acme] (connect, disconnect)", got)
	}
}
