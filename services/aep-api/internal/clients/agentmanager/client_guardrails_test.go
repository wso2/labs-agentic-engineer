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

package agentmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// The response shapes below are the ones a live amp-api (v1.0.0-rc2) returned
// on 2026-09-30; Agent Manager publishes no OpenAPI, so these pin the contract.

func TestListPoliciesReadsTheGatewayCatalog(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && r.URL.Path == "/orgs/default/llm-providers/policies":
			gotQuery = r.URL.Query().Get("providerId")
			_ = json.NewEncoder(w).Encode(map[string]any{"count": 1, "list": []any{map[string]any{
				"name": "pii-masking-regex", "version": "v1.0.4", "displayName": "PII Masking",
				"description": "Masks PII.", "parameters": map[string]any{"type": "object"},
			}}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	got, err := c.ListPolicies(context.Background(), "default", "prov-uuid")
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if gotQuery != "prov-uuid" {
		t.Errorf("providerId = %q, want prov-uuid", gotQuery)
	}
	if len(got) != 1 || got[0].Name != "pii-masking-regex" || got[0].Version != "v1.0.4" ||
		got[0].DisplayName != "PII Masking" || string(got[0].Parameters) != `{"type":"object"}` {
		t.Fatalf("catalog = %+v", got)
	}
}

const liveBinding = `{
 "agentId": "receipt-agent-4bc79ccf",
 "envMappings": {"development": {"environmentName": "development", "configuration": {
   "policies": [{"name": "regex-guardrail", "version": "v1.0.1",
     "paths": [{"path": "/*", "methods": ["*"], "params": {"request": {"regex": "x"}}}]}],
   "providerName": "aep-default-anthropic", "providerPolicies": [],
   "providerUuid": "66f533aa", "proxyUuid": "49cd", "status": "active",
   "url": "http://ai-gateway.amp.localhost:8084/aep-receip"}}},
 "environmentVariables": [{"key": "url", "name": "MODEL_ENDPOINT"}, {"key": "apikey", "name": "MODEL_API_KEY"}],
 "name": "aep-receipt-agent", "type": "llm", "uuid": "cfg-1"
}`

func TestReadBindingReturnsWhatAPolicyWriteMustCarryBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet && r.URL.Path == "/orgs/default/projects/shop/agents/receipt-agent-4bc79ccf/model-configs/cfg-1":
			_, _ = w.Write([]byte(liveBinding))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	b, err := c.ReadBinding(context.Background(), BindingRef{
		Org: "default", Project: "shop", Agent: "receipt-agent-4bc79ccf", ConfigID: "cfg-1", Environment: "development",
	})
	if err != nil {
		t.Fatalf("ReadBinding: %v", err)
	}
	if b.Name != "aep-receipt-agent" || b.ProviderHandle != "aep-default-anthropic" {
		t.Errorf("binding = %+v", b)
	}
	if len(b.Policies) != 1 || b.Policies[0].Name != "regex-guardrail" || b.Policies[0].Version != "v1.0.1" ||
		b.Policies[0].Paths[0].Path != "/*" {
		t.Errorf("policies = %+v", b.Policies)
	}
	if len(b.EnvironmentVariables) != 2 {
		t.Errorf("environmentVariables = %+v, want both", b.EnvironmentVariables)
	}
}

// The write carries everything the read returned that the update replaces:
// the name, the provider and BOTH environment variables, verbatim. Dropping
// the variables would leave Agent Manager unable to say which env vars the
// agent reads its model access from.
func TestWriteBindingPoliciesCarriesTheBindingBackUnchanged(t *testing.T) {
	var method, path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		default:
			method, path = r.Method, r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(liveBinding))
		}
	}))
	defer srv.Close()

	read := Binding{
		Name: "aep-receipt-agent", ProviderHandle: "aep-default-anthropic",
		EnvironmentVariables: []map[string]any{{"key": "url", "name": "MODEL_ENDPOINT"}, {"key": "apikey", "name": "MODEL_API_KEY"}},
	}
	policies := []BindingPolicy{{Name: "pii-masking-regex", Version: "v1.0.4",
		Paths: []BindingPolicyPath{{Path: "/*", Methods: []string{"*"}, Params: map[string]any{"email": true}}}}}

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	ref := BindingRef{Org: "default", Project: "shop", Agent: "receipt-agent-4bc79ccf", ConfigID: "cfg-1", Environment: "development"}
	if err := c.WriteBindingPolicies(context.Background(), ref, read, policies); err != nil {
		t.Fatalf("WriteBindingPolicies: %v", err)
	}
	if method != http.MethodPut || path != "/orgs/default/projects/shop/agents/receipt-agent-4bc79ccf/model-configs/cfg-1" {
		t.Fatalf("request = %s %s", method, path)
	}
	want := map[string]any{
		"name": "aep-receipt-agent",
		"envMappings": map[string]any{"development": map[string]any{
			"providerName": "aep-default-anthropic",
			"configuration": map[string]any{"policies": []any{map[string]any{
				"name": "pii-masking-regex", "version": "v1.0.4",
				"paths": []any{map[string]any{"path": "/*", "methods": []any{"*"}, "params": map[string]any{"email": true}}},
			}}},
		}},
		"environmentVariables": []any{
			map[string]any{"key": "url", "name": "MODEL_ENDPOINT"},
			map[string]any{"key": "apikey", "name": "MODEL_API_KEY"},
		},
	}
	if !reflect.DeepEqual(body, want) {
		got, _ := json.MarshalIndent(body, "", " ")
		t.Fatalf("PUT body =\n%s", got)
	}
}

// Removing the last guardrail writes an empty list, not a missing one: a
// missing `policies` would leave Agent Manager's copy in place.
func TestWriteBindingPoliciesSendsAnEmptyListNotNull(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(liveBinding))
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	ref := BindingRef{Org: "default", Project: "shop", Agent: "a", ConfigID: "cfg-1", Environment: "development"}
	if err := c.WriteBindingPolicies(context.Background(), ref, Binding{Name: "n", ProviderHandle: "p"}, nil); err != nil {
		t.Fatalf("WriteBindingPolicies: %v", err)
	}
	cfg := body["envMappings"].(map[string]any)["development"].(map[string]any)["configuration"].(map[string]any)
	if list, ok := cfg["policies"].([]any); !ok || len(list) != 0 {
		t.Fatalf("configuration.policies = %#v, want []", cfg["policies"])
	}
}

// A binding the operator also edits: another environment's mapping, an extra
// field on their own guardrail, and a resilience setting. AEP replaces only
// the policies of the environment it governs; everything else goes back
// exactly as it was read.
const operatorEditedBinding = `{
 "name": "aep-receipt-agent", "description": "receipt reader",
 "envMappings": {
  "development": {"configuration": {
    "policies": [{"name": "url-guardrail", "version": "v1.0.1", "enabled": true,
      "paths": [{"path": "/*", "methods": ["*"], "params": {"request": {"onlyDNS": true}}}]}],
    "resilience": {"timeout": 30},
    "providerName": "aep-default-anthropic", "status": "active", "url": "http://gw/aep-receip"}},
  "staging": {"configuration": {
    "policies": [{"name": "regex-guardrail", "version": "v1.0.1",
      "paths": [{"path": "/*", "methods": ["*"], "params": {"request": {"regex": "x"}}}]}],
    "providerName": "aep-default-anthropic"}}
 },
 "environmentVariables": [{"key": "url", "name": "MODEL_ENDPOINT"}, {"key": "apikey", "name": "MODEL_API_KEY"}],
 "uuid": "cfg-1"
}`

func TestWriteBindingPoliciesKeepsEverythingElseOnTheBinding(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/token"):
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(operatorEditedBinding))
		default:
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(operatorEditedBinding))
		}
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token"})
	ref := BindingRef{Org: "default", Project: "shop", Agent: "a", ConfigID: "cfg-1", Environment: "development"}
	b, err := c.ReadBinding(context.Background(), ref)
	if err != nil {
		t.Fatalf("ReadBinding: %v", err)
	}
	// Keep the operator's entry as read, and add AEP's.
	next := append(append([]BindingPolicy{}, b.Policies...), BindingPolicy{Name: "pii-masking-regex", Version: "v1.0.4",
		Paths: []BindingPolicyPath{{Path: "/*", Methods: []string{"*"}, Params: map[string]any{"email": true}}}})
	if err := c.WriteBindingPolicies(context.Background(), ref, b, next); err != nil {
		t.Fatalf("WriteBindingPolicies: %v", err)
	}

	if body["description"] != "receipt reader" {
		t.Errorf("description = %v, want it carried back", body["description"])
	}
	envs := body["envMappings"].(map[string]any)
	staging, ok := envs["staging"].(map[string]any)
	if !ok {
		t.Fatalf("the staging mapping was dropped: %+v", envs)
	}
	stagingPolicies := staging["configuration"].(map[string]any)["policies"].([]any)
	if len(stagingPolicies) != 1 || stagingPolicies[0].(map[string]any)["name"] != "regex-guardrail" {
		t.Errorf("staging policies = %+v, want them untouched", stagingPolicies)
	}
	dev := envs["development"].(map[string]any)["configuration"].(map[string]any)
	if dev["resilience"] == nil {
		t.Errorf("development resilience was dropped: %+v", dev)
	}
	devPolicies := dev["policies"].([]any)
	if len(devPolicies) != 2 {
		t.Fatalf("development policies = %+v, want the operator's and AEP's", devPolicies)
	}
	operators := devPolicies[0].(map[string]any)
	if operators["name"] != "url-guardrail" || operators["enabled"] != true {
		t.Errorf("the operator's entry lost a field: %+v", operators)
	}
	if devPolicies[1].(map[string]any)["name"] != "pii-masking-regex" {
		t.Errorf("AEP's entry = %+v", devPolicies[1])
	}
}
