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

package agentgovernance

// CONTRACT tier — the provider a connection becomes, as the bytes Agent Manager
// receives: ProviderInputFor through the real agentmanager client to a test
// server. The unit tests in governor_test.go prove the input; these prove the
// wire.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/agentmanager"
	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
)

// firstPartyProviderWireBefore is the provider body an org on Anthropic's own
// API already has in Agent Manager (the `anthropic` template's auth read from
// Agent Manager, then ProviderInputFor), with a fake key. POST and PUT send
// the same bytes.
const firstPartyProviderWireBefore = `{"accessControl":{"exceptions":[],"mode":"allow_all"},` +
	`"context":"/aep-default-anthropic","gateways":["gw-1"],"id":"aep-default-anthropic",` +
	`"name":"AEP default Anthropic",` +
	`"security":{"apiKey":{"enabled":true,"in":"header","key":"X-API-Key"},"enabled":true},` +
	`"template":"anthropic",` +
	`"upstream":{"main":{"auth":{"header":"x-api-key","type":"api-key","value":"sk-ant-api03-FAKE-golden-key"},` +
	`"url":"https://api.anthropic.com"}},"version":"v1.0"}`

// providerWire drives the real client to a test server and returns the POST
// that creates the provider and the PUT that re-asserts it.
func providerWire(t *testing.T, conn modelconn.Connection, key string) (post, put string) {
	t.Helper()
	exists := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/llm-providers"):
			providers := []map[string]any{}
			if exists {
				providers = append(providers, map[string]any{"uuid": "prov-uuid", "id": "aep-default-anthropic"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"providers": providers})
		case r.Method == http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			post, exists = string(b), true
			_ = json.NewEncoder(w).Encode(map[string]any{"uuid": "prov-uuid", "id": "aep-default-anthropic"})
		case r.Method == http.MethodPut:
			b, _ := io.ReadAll(r.Body)
			put = string(b)
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Errorf("unexpected %s %s — the provider needs no template read", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	in, err := ProviderInputFor("default", conn, key, "gw-1")
	if err != nil {
		t.Fatalf("ProviderInputFor: %v", err)
	}
	c := agentmanager.New(agentmanager.Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	if _, err := c.EnsureProvider(context.Background(), in); err != nil {
		t.Fatalf("EnsureProvider (create): %v", err)
	}
	in.ReassertCredential = true
	if _, err := c.EnsureProvider(context.Background(), in); err != nil {
		t.Fatalf("EnsureProvider (update): %v", err)
	}
	return post, put
}

// An org on Anthropic's own API keeps its existing provider byte for byte,
// except the display name.
func TestProviderWireForAnthropicsOwnAPIIsUnchanged(t *testing.T) {
	want := strings.Replace(firstPartyProviderWireBefore,
		`"name":"AEP default Anthropic"`, `"name":"AEP default model connection"`, 1)
	post, put := providerWire(t, firstPartyConn(), "sk-ant-api03-FAKE-golden-key")
	if post != want {
		t.Errorf("POST changed:\n got %s\nwant %s", post, want)
	}
	if put != want {
		t.Errorf("PUT changed:\n got %s\nwant %s", put, want)
	}
}

// An OpenAI-compatible connection rides the `openai` template with its own
// origin, and its key goes upstream as `Authorization: Bearer <key>`, prefix
// included: Agent Manager's API does not apply the template's valuePrefix.
func TestProviderWireForAnOpenAICompatibleConnection(t *testing.T) {
	post, put := providerWire(t, ollamaOpenAIConn(), "fake-ollama-key")
	if post != put {
		t.Errorf("POST and PUT differ:\n%s\n%s", post, put)
	}
	var body struct {
		Template string `json:"template"`
		Upstream struct {
			Main struct {
				URL  string `json:"url"`
				Auth struct {
					Type, Header, Value string
				} `json:"auth"`
			} `json:"main"`
		} `json:"upstream"`
	}
	if err := json.Unmarshal([]byte(put), &body); err != nil {
		t.Fatalf("decode %s: %v", put, err)
	}
	if body.Template != "openai" {
		t.Errorf("template = %q, want openai", body.Template)
	}
	if body.Upstream.Main.URL != "https://ollama.com" {
		t.Errorf("upstream = %q, want the origin with no path", body.Upstream.Main.URL)
	}
	auth := body.Upstream.Main.Auth
	if auth.Type != "api-key" || auth.Header != "Authorization" || auth.Value != "Bearer fake-ollama-key" {
		t.Errorf("upstream auth = %+v, want api-key / Authorization / Bearer <key>", auth)
	}
}
