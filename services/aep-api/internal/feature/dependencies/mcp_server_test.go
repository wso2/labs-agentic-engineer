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

package dependencies

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/models"
)

// ---- fake ports -------------------------------------------------------------

type fakeResourceReader struct {
	items   []models.ExternalResource
	listErr error
	getErr  error
	// lastOrg records the org the handler passed down, proving it flows from
	// the request context (the verified claim) into the port call.
	lastOrg string
}

func (f *fakeResourceReader) List(_ context.Context, orgID string) ([]models.ExternalResource, error) {
	f.lastOrg = orgID
	return f.items, f.listErr
}

func (f *fakeResourceReader) Get(_ context.Context, orgID, name string) (*models.ExternalResource, error) {
	f.lastOrg = orgID
	if f.getErr != nil {
		return nil, f.getErr
	}
	for i := range f.items {
		if f.items[i].Name == name {
			return &f.items[i], nil
		}
	}
	return nil, nil
}

type fakeEndpointLister struct {
	items   []openchoreo.WorkloadEndpointInfo
	err     error
	lastOrg string
	// lastCtxServiceIdentity records whether the handler marked the tool-call
	// context as service identity before hitting the port. Without the marker
	// the OC transport treats the request's MCP bearer as a forwardable user
	// JWT and OC 401s every catalog read (caught live in E2E S3).
	lastCtxServiceIdentity bool
}

func (f *fakeEndpointLister) List(ctx context.Context, orgHandle string) ([]openchoreo.WorkloadEndpointInfo, error) {
	f.lastOrg = orgHandle
	f.lastCtxServiceIdentity = auth.IsServiceIdentity(ctx)
	return f.items, f.err
}

type fakeTypeLister struct {
	items []resources.PlatformResourceType
	err   error
}

func (f *fakeTypeLister) List(context.Context) ([]resources.PlatformResourceType, error) {
	return f.items, f.err
}

// ---- helpers ----------------------------------------------------------------

// postRPC sends body to the handler with the org already bound on the context
// (as the auth middleware would) and returns the recorder.
func postRPC(t *testing.T, h http.Handler, org, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", strings.NewReader(body))
	if org != "" {
		req = req.WithContext(auth.WithMCPOrg(req.Context(), org))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// decodeRPC decodes a 200 JSON-RPC response.
func decodeRPC(t *testing.T, w *httptest.ResponseRecorder) jsonrpcResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	var resp jsonrpcResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp
}

// toolText extracts content[0].text from a successful tools/call result and
// asserts the isError flag matches wantErr.
func toolText(t *testing.T, resp jsonrpcResponse, wantErr bool) string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", resp.Error)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result is not an object: %T", resp.Result)
	}
	isErr, _ := result["isError"].(bool)
	if isErr != wantErr {
		t.Fatalf("isError = %v, want %v (result %+v)", isErr, wantErr, result)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("expected non-empty content array, got %+v", result["content"])
	}
	block, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not an object: %T", content[0])
	}
	text, ok := block["text"].(string)
	if !ok {
		t.Fatalf("content[0].text is not a string: %T", block["text"])
	}
	return text
}

func callBody(tool, args string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args)
}

func sampleHandler() (http.Handler, *fakeResourceReader, *fakeEndpointLister) {
	er := &fakeResourceReader{items: []models.ExternalResource{{
		Name:        "salesforce",
		Description: "CRM",
		ConfigKeys: models.ConfigKeySlice{
			{Key: "SALESFORCE_URL", Secret: false},
			{Key: "SALESFORCE_TOKEN", Secret: true},
		},
	}}}
	ep := &fakeEndpointLister{items: []openchoreo.WorkloadEndpointInfo{
		{Project: "billing", Component: "invoice-api", Name: "rest", Type: "HTTP", Visibility: []string{"namespace"}},
		{Project: "crm", Component: "leads-api", Name: "grpc", Type: "gRPC"}, // not published cross-project
	}}
	rt := &fakeTypeLister{items: []resources.PlatformResourceType{
		{Name: "postgres", Description: "A dedicated PostgreSQL database cluster.", Outputs: []string{"host", "port"}},
	}}
	return NewMCPHandler(er, ep, rt), er, ep
}

// ---- protocol ----------------------------------------------------------------

func TestMCP_Initialize(t *testing.T) {
	h, _, _ := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] != mcpProtocolVersion {
		t.Errorf("protocolVersion = %v, want %q", result["protocolVersion"], mcpProtocolVersion)
	}
	if _, ok := result["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("capabilities missing tools: %+v", result["capabilities"])
	}
}

func TestMCP_Ping(t *testing.T) {
	h, _, _ := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":7,"method":"ping"}`))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if string(resp.ID) != "7" {
		t.Errorf("response id = %s, want 7", resp.ID)
	}
}

func TestMCP_ToolsList_RenamedTools(t *testing.T) {
	h, _, _ := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	var result struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	var names []string
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		if strings.Contains(strings.ToLower(tool.Name), "connection") ||
			strings.Contains(strings.ToLower(tool.Description), "connection") {
			t.Errorf("tool %q leaks banned 'connection' terminology", tool.Name)
		}
	}
	want := []string{
		"list_external_resources",
		"get_external_resource_schema",
		"list_org_endpoints",
		"list_platform_resource_types",
	}
	if len(names) != len(want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("tools[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}

func TestMCP_Notification_202NoBody(t *testing.T) {
	h, _, _ := sampleHandler()
	w := postRPC(t, h, "org-1", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", w.Body.String())
	}
}

func TestMCP_ParseError(t *testing.T) {
	h, _, _ := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{not json`))
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("error = %+v, want code -32700", resp.Error)
	}
}

func TestMCP_MethodNotFound(t *testing.T) {
	h, _, _ := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`))
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("error = %+v, want code -32601", resp.Error)
	}
}

func TestMCP_UnknownTool(t *testing.T) {
	h, _, _ := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_connections", `{}`)))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("error = %+v, want code -32602 (source tool names must be gone)", resp.Error)
	}
}

// ---- guards ------------------------------------------------------------------

func TestMCP_NilResourceReader_503(t *testing.T) {
	h := NewMCPHandler(nil, &fakeEndpointLister{}, &fakeTypeLister{})
	w := postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestMCP_NoOrgOnContext_401(t *testing.T) {
	h, _, _ := sampleHandler()
	w := postRPC(t, h, "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (unwrapped mount must fail closed)", w.Code)
	}
}

// ---- tools/call ----------------------------------------------------------------

func TestMCP_ListExternalResources(t *testing.T) {
	h, er, _ := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_external_resources", `{}`)))
	text := toolText(t, resp, false)

	var payload struct {
		ExternalResources []externalResourceView `json:"externalResources"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.ExternalResources) != 1 {
		t.Fatalf("externalResources = %+v, want 1 entry", payload.ExternalResources)
	}
	got := payload.ExternalResources[0]
	if got.Name != "salesforce" || got.Description != "CRM" || len(got.ConfigKeys) != 2 {
		t.Errorf("unexpected view: %+v", got)
	}
	if !got.ConfigKeys[1].Secret {
		t.Errorf("SALESFORCE_TOKEN must be marked secret: %+v", got.ConfigKeys)
	}
	if er.lastOrg != "org-1" {
		t.Errorf("port called with org %q, want org-1 (context org must flow down)", er.lastOrg)
	}
}

func TestMCP_ListExternalResources_PortError(t *testing.T) {
	er := &fakeResourceReader{listErr: fmt.Errorf("db down")}
	h := NewMCPHandler(er, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_external_resources", `{}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "db down") {
		t.Errorf("tool error text = %q, want it to carry the port error", text)
	}
}

func TestMCP_GetExternalResourceSchema(t *testing.T) {
	h, _, _ := sampleHandler()

	t.Run("found", func(t *testing.T) {
		resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("get_external_resource_schema", `{"name":"salesforce"}`)))
		text := toolText(t, resp, false)
		var payload struct {
			Found            bool                 `json:"found"`
			ExternalResource externalResourceView `json:"externalResource"`
		}
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if !payload.Found || payload.ExternalResource.Name != "salesforce" {
			t.Errorf("unexpected payload: %+v", payload)
		}
	})

	t.Run("not found", func(t *testing.T) {
		resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("get_external_resource_schema", `{"name":"stripe"}`)))
		text := toolText(t, resp, false)
		var payload struct {
			Found bool   `json:"found"`
			Name  string `json:"name"`
		}
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.Found || payload.Name != "stripe" {
			t.Errorf("unexpected payload: %+v", payload)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("get_external_resource_schema", `{}`)))
		text := toolText(t, resp, true)
		if !strings.Contains(text, "name") {
			t.Errorf("tool error text = %q, want it to name the missing argument", text)
		}
	})
}

func TestMCP_ListOrgEndpoints(t *testing.T) {
	h, _, ep := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_org_endpoints", `{}`)))
	text := toolText(t, resp, false)

	var payload struct {
		Endpoints []orgEndpointView `json:"endpoints"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Endpoints) != 2 {
		t.Fatalf("endpoints = %+v, want 2 entries", payload.Endpoints)
	}
	first := payload.Endpoints[0]
	if first.Name != "invoice-api" || first.Project != "billing" || first.Endpoint != "rest" ||
		first.Type != "HTTP" || !first.NamespaceVisible {
		t.Errorf("unexpected first endpoint view: %+v", first)
	}
	if payload.Endpoints[1].NamespaceVisible {
		t.Errorf("endpoint without namespace visibility must report namespaceVisible=false")
	}
	if ep.lastOrg != "org-1" {
		t.Errorf("lister called with org %q, want org-1", ep.lastOrg)
	}
	if !ep.lastCtxServiceIdentity {
		t.Errorf("tool-call context must be marked service identity — otherwise the OC transport forwards the MCP bearer as a user JWT and every catalog read 401s")
	}
}

func TestMCP_ListOrgEndpoints_NilLister_Empty(t *testing.T) {
	er := &fakeResourceReader{}
	h := NewMCPHandler(er, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_org_endpoints", `{}`)))
	text := toolText(t, resp, false)
	if text != `{"endpoints":[]}` {
		t.Errorf("payload = %q, want empty endpoints", text)
	}
}

func TestMCP_ListPlatformResourceTypes(t *testing.T) {
	h, _, _ := sampleHandler()
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_platform_resource_types", `{}`)))
	text := toolText(t, resp, false)

	var payload struct {
		ResourceTypes []resources.PlatformResourceType `json:"resourceTypes"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.ResourceTypes) != 1 || payload.ResourceTypes[0].Name != "postgres" {
		t.Errorf("unexpected payload: %+v", payload)
	}
	// The self-description flows through to the architect-facing payload —
	// assert on the raw JSON text so a `json:"-"` regression cannot pass.
	if !strings.Contains(text, `"description":"A dedicated PostgreSQL database cluster."`) {
		t.Errorf("payload missing serialized description: %s", text)
	}
	// Markers stay internal (json:"-") — no marker leakage into the payload.
	if strings.Contains(text, "Markers") || strings.Contains(text, "EndUserAuth") {
		t.Errorf("payload leaks internal Markers: %s", text)
	}
}

func TestMCP_ListPlatformResourceTypes_NilLister_Empty(t *testing.T) {
	er := &fakeResourceReader{}
	h := NewMCPHandler(er, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_platform_resource_types", `{}`)))
	text := toolText(t, resp, false)
	if text != `{"resourceTypes":[]}` {
		t.Errorf("payload = %q, want empty resourceTypes", text)
	}
}
