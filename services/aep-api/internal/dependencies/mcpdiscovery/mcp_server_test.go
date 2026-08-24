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

package mcpdiscovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// ---- fake ports -------------------------------------------------------------

// externalCatalogFixture wires the real dependencies.ExternalResourceCatalog
// over a ResourceClientMock returning canned org-namespaced ResourceTypes —
// the OC-RT fixture that replaces the pre-Task-3 DB-row fake (external
// resources are now sourced from provisioned ResourceTypes via
// openchoreo.ExternalDefinitionFromRT, never the external_resources table).
type externalCatalogFixture struct {
	*dependencies.ExternalResourceCatalog
	// lastOrg records the namespace the handler passed down (via List/Get →
	// ListResourceTypes), proving it flows from the request context (the
	// verified claim) into the port call.
	lastOrg string
}

// newExternalCatalogFixture builds a fixture whose ListResourceTypes returns
// rts verbatim (or listErr, when set, to exercise both tools' error path).
func newExternalCatalogFixture(listErr error, rts ...openchoreo.ResourceType) *externalCatalogFixture {
	f := &externalCatalogFixture{}
	rc := &mocks.ResourceClientMock{
		ListResourceTypesFunc: func(_ context.Context, namespace string) ([]openchoreo.ResourceType, error) {
			f.lastOrg = namespace
			return rts, listErr
		},
	}
	f.ExternalResourceCatalog = dependencies.NewExternalResourceCatalog(rc)
	return f
}

// mustBuildExternalRT builds an external resource's ResourceType fixture via
// the SAME production builder (openchoreo.BuildExternalResourceType) real
// provisioning uses. Mirrors the sibling helper in mcp_surface_test.go: fails
// the test loudly on a build error instead of silently dereferencing a
// possibly-nil *ResourceType (these fixed, well-formed inputs — non-empty
// name, at least one non-empty key — should never trigger one, but a nil-deref
// panic on a future regression would be a far worse failure mode than a clear
// t.Fatalf).
func mustBuildExternalRT(t *testing.T, name, description string, keys ...openchoreo.ExternalResourceConfigKey) openchoreo.ResourceType {
	t.Helper()
	rt, err := openchoreo.BuildExternalResourceType(name, description, keys)
	if err != nil {
		t.Fatalf("build external RT fixture %q: %v", name, err)
	}
	return *rt
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

	// resolved/resolvedErr back ListResolved (the A3 list_org_component_endpoints
	// tool). lastResolvedOrg/lastResolvedCtxServiceIdentity mirror the List
	// fields above for the resolved path.
	resolved                       []dependencies.OrgComponentEndpoint
	resolvedErr                    error
	lastResolvedOrg                string
	lastResolvedCtxServiceIdentity bool
}

func (f *fakeEndpointLister) List(ctx context.Context, orgHandle string) ([]openchoreo.WorkloadEndpointInfo, error) {
	f.lastOrg = orgHandle
	f.lastCtxServiceIdentity = auth.IsServiceIdentity(ctx)
	return f.items, f.err
}

func (f *fakeEndpointLister) ListResolved(ctx context.Context, orgHandle string) ([]dependencies.OrgComponentEndpoint, error) {
	f.lastResolvedOrg = orgHandle
	f.lastResolvedCtxServiceIdentity = auth.IsServiceIdentity(ctx)
	return f.resolved, f.resolvedErr
}

type fakeTypeLister struct {
	items []dependencies.PlatformResourceType
	err   error
}

func (f *fakeTypeLister) List(context.Context) ([]dependencies.PlatformResourceType, error) {
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

// nonExternalRT is a namespaced ResourceType fixture that carries NO
// aep.wso2.com/external-name annotation — e.g. an RT authored by
// pre-self-describing code, or any other namespaced RT that isn't an
// external-resource one. openchoreo.ExternalDefinitionFromRT reports ok=false
// for it, and the catalog must silently skip it rather than surface a
// half-formed entry.
var nonExternalRT = openchoreo.ResourceType{
	APIVersion: "openchoreo.dev/v1alpha1",
	Kind:       "ResourceType",
	Metadata:   openchoreo.OCObjectMeta{Name: "some-other-rt"},
	Spec: openchoreo.ResourceTypeSpec{
		Resources: []openchoreo.ResourceTypeManifest{{ID: "x", Template: []byte(`{}`)}},
	},
}

func sampleHandler(t *testing.T) (http.Handler, *externalCatalogFixture, *fakeEndpointLister) {
	t.Helper()
	er := newExternalCatalogFixture(nil,
		mustBuildExternalRT(t, "salesforce", "CRM",
			openchoreo.ExternalResourceConfigKey{Key: "SALESFORCE_URL", Secret: false},
			openchoreo.ExternalResourceConfigKey{Key: "SALESFORCE_TOKEN", Secret: true},
		),
		nonExternalRT, // must be skipped — proves the external-name-annotation filter
	)
	ep := &fakeEndpointLister{
		items: []openchoreo.WorkloadEndpointInfo{
			{Project: "billing", Component: "invoice-api", Name: "rest", Type: "HTTP", Visibility: []string{"namespace"}},
			{Project: "crm", Component: "leads-api", Name: "grpc", Type: "gRPC"}, // not published cross-project
		},
		resolved: []dependencies.OrgComponentEndpoint{
			{
				Project: "billing", Component: "invoice-api", Endpoint: "rest", Type: "HTTP",
				Port: 8080, BasePath: "/api", NamespaceVisible: true,
				Owner: "wso2", Repo: "billing-svc", Subdir: "services/invoice-api", Branch: "main",
				Spec: dependencies.EndpointSpec{
					Availability:  "inline",
					InlineContent: "openapi: 3.0.0\ninfo:\n  title: invoice-api\n",
					Path:          "specs/design/components/invoice-api/openapi.yaml",
				},
			},
			{
				Project: "crm", Component: "leads-api", Endpoint: "grpc", Type: "gRPC",
				Spec: dependencies.EndpointSpec{Availability: "none"},
			},
		},
	}
	rt := &fakeTypeLister{items: []dependencies.PlatformResourceType{
		{Name: "postgres", Description: "A dedicated PostgreSQL database cluster.", Outputs: []string{"host", "port"}},
	}}
	return NewMCPHandler(er, ep, rt, &fakeRemoteGit{},
		spec.ValidateOpenAPI, spec.NormalizeOpenAPIYAML, spec.FetchSpecFromURL), er, ep
}

// fakeRemoteGit is a stub RemoteGitReader for the handler-dispatch tests. It
// records the org + owner the handler passed down (proving org flows from the
// verified context, not a tool param) and returns canned results or errors.
type fakeRemoteGit struct {
	file      *RemoteGitFile
	hits      []RemoteGitSearchHit
	err       error
	lastOrg   string
	lastOwner string
}

func (f *fakeRemoteGit) GetFileContents(_ context.Context, ocOrgID, owner, _, _, _ string) (*RemoteGitFile, error) {
	f.lastOrg, f.lastOwner = ocOrgID, owner
	if f.err != nil {
		return nil, f.err
	}
	return f.file, nil
}

func (f *fakeRemoteGit) SearchCode(_ context.Context, ocOrgID, owner, _, _ string) ([]RemoteGitSearchHit, error) {
	f.lastOrg, f.lastOwner = ocOrgID, owner
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

// ---- protocol ----------------------------------------------------------------

func TestMCP_Initialize(t *testing.T) {
	h, _, _ := sampleHandler(t)
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
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":7,"method":"ping"}`))
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if string(resp.ID) != "7" {
		t.Errorf("response id = %s, want 7", resp.ID)
	}
}

func TestMCP_ToolsList_RenamedTools(t *testing.T) {
	h, _, _ := sampleHandler(t)
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
		"list_org_component_endpoints",
		"list_platform_resource_types",
		"get_remote_git_file_contents",
		"search_remote_git_code",
		"validate_openapi_spec",
		"fetch_openapi_spec",
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
	h, _, _ := sampleHandler(t)
	w := postRPC(t, h, "org-1", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", w.Body.String())
	}
}

func TestMCP_ParseError(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{not json`))
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("error = %+v, want code -32700", resp.Error)
	}
}

func TestMCP_MethodNotFound(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`))
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("error = %+v, want code -32601", resp.Error)
	}
}

func TestMCP_UnknownTool(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_connections", `{}`)))
	if resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("error = %+v, want code -32602 (source tool names must be gone)", resp.Error)
	}
}

// ---- guards ------------------------------------------------------------------

func TestMCP_NilResourceReader_503(t *testing.T) {
	h := NewMCPHandler(nil, &fakeEndpointLister{}, &fakeTypeLister{}, &fakeRemoteGit{}, nil, nil, nil)
	w := postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
}

func TestMCP_NoOrgOnContext_401(t *testing.T) {
	h, _, _ := sampleHandler(t)
	w := postRPC(t, h, "", `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (unwrapped mount must fail closed)", w.Code)
	}
}

// ---- tools/call ----------------------------------------------------------------

// TestMCP_ListExternalResources also proves the RT-registry filter: the
// fixture (sampleHandler) carries TWO namespaced ResourceTypes — the
// self-describing "salesforce" external RT and nonExternalRT, which lacks the
// aep.wso2.com/external-name annotation — yet exactly one entry comes
// back, so a non-external RT sharing the namespace never leaks into the tool.
func TestMCP_ListExternalResources(t *testing.T) {
	h, er, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_external_resources", `{}`)))
	text := toolText(t, resp, false)

	var payload struct {
		ExternalResources []externalResourceView `json:"externalResources"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.ExternalResources) != 1 {
		t.Fatalf("externalResources = %+v, want 1 entry (nonExternalRT must be filtered out)", payload.ExternalResources)
	}
	got := payload.ExternalResources[0]
	if got.Name != "salesforce" || got.Description != "CRM" || len(got.ConfigKeys) != 2 {
		t.Errorf("unexpected view: %+v", got)
	}
	secretByKey := map[string]bool{}
	for _, k := range got.ConfigKeys {
		secretByKey[k.Key] = k.Secret
	}
	if !secretByKey["SALESFORCE_TOKEN"] {
		t.Errorf("SALESFORCE_TOKEN must be marked secret: %+v", got.ConfigKeys)
	}
	if secretByKey["SALESFORCE_URL"] {
		t.Errorf("SALESFORCE_URL must NOT be marked secret: %+v", got.ConfigKeys)
	}
	if er.lastOrg != "org-1" {
		t.Errorf("port called with org %q, want org-1 (context org must flow down)", er.lastOrg)
	}
}

func TestMCP_ListExternalResources_PortError(t *testing.T) {
	er := newExternalCatalogFixture(fmt.Errorf("db down"))
	h := NewMCPHandler(er, nil, nil, nil, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_external_resources", `{}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "db down") {
		t.Errorf("tool error text = %q, want it to carry the port error", text)
	}
}

func TestMCP_GetExternalResourceSchema(t *testing.T) {
	h, _, _ := sampleHandler(t)

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

// TestMCP_ExternalResources_DedupesStaleSchema covers the core OC-RT-registry
// hazard this task fixes: ResourceTypes are effectively immutable (a changed
// key/secret schema mints a brand-new RT name — see ExternalResourceRTName)
// and are never deleted, so an external resource that has gone through a
// schema change carries TWO namespaced RTs sharing the SAME
// aep.wso2.com/external-name annotation — a stale one and a current
// one. Both list_external_resources and get_external_resource_schema must
// surface the logical name exactly ONCE, picking the RT with the NEWER
// metadata.creationTimestamp — never the stale schema, never both, and
// (proven by the reversed-order sub-case) never dependent on
// ListResourceTypes' return order.
func TestMCP_ExternalResources_DedupesStaleSchema(t *testing.T) {
	stale := mustBuildExternalRT(t, "stripe", "Payments",
		openchoreo.ExternalResourceConfigKey{Key: "STRIPE_KEY", Secret: true})
	stale.Metadata.CreationTimestamp = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	current := mustBuildExternalRT(t, "stripe", "Payments v2",
		openchoreo.ExternalResourceConfigKey{Key: "STRIPE_KEY", Secret: true},
		openchoreo.ExternalResourceConfigKey{Key: "STRIPE_WEBHOOK_SECRET", Secret: true})
	current.Metadata.CreationTimestamp = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	if stale.Metadata.Name == current.Metadata.Name {
		t.Fatalf("fixture bug: stale/current must hash to different RT names (different schemas), got %q for both", stale.Metadata.Name)
	}

	assertNewestWins := func(t *testing.T, rts ...openchoreo.ResourceType) {
		t.Helper()
		er := newExternalCatalogFixture(nil, rts...)
		h := NewMCPHandler(er, nil, nil, nil, nil, nil, nil)

		resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_external_resources", `{}`)))
		text := toolText(t, resp, false)
		var listPayload struct {
			ExternalResources []externalResourceView `json:"externalResources"`
		}
		if err := json.Unmarshal([]byte(text), &listPayload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if len(listPayload.ExternalResources) != 1 {
			t.Fatalf("externalResources = %+v, want exactly 1 entry (stale RT must be deduped away)", listPayload.ExternalResources)
		}
		got := listPayload.ExternalResources[0]
		if got.Name != "stripe" || got.Description != "Payments v2" || len(got.ConfigKeys) != 2 {
			t.Errorf("list entry = %+v, want the NEWER (2-key, %q) schema", got, "Payments v2")
		}

		getResp := decodeRPC(t, postRPC(t, h, "org-1", callBody("get_external_resource_schema", `{"name":"stripe"}`)))
		getText := toolText(t, getResp, false)
		var getPayload struct {
			Found            bool                 `json:"found"`
			ExternalResource externalResourceView `json:"externalResource"`
		}
		if err := json.Unmarshal([]byte(getText), &getPayload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if !getPayload.Found || getPayload.ExternalResource.Description != "Payments v2" || len(getPayload.ExternalResource.ConfigKeys) != 2 {
			t.Errorf("get_external_resource_schema = %+v, want the NEWER schema (must agree with list)", getPayload)
		}
	}

	t.Run("stale then current", func(t *testing.T) { assertNewestWins(t, stale, current) })
	t.Run("current then stale (order must not matter)", func(t *testing.T) { assertNewestWins(t, current, stale) })
}

// TestMCP_ExternalResources_TieBreakDeterministic covers the fallback when two
// same-named RTs carry an EQUAL creationTimestamp — including the "absent"
// case where neither fixture sets one (both zero value), e.g. a test/dev
// fixture authored without one. Selection must still be deterministic (the
// lexically GREATER RT metadata.name) rather than flip-flopping with
// ListResourceTypes' return order — otherwise list_external_resources and
// get_external_resource_schema could each pick a different schema on
// different calls.
func TestMCP_ExternalResources_TieBreakDeterministic(t *testing.T) {
	a := mustBuildExternalRT(t, "hubspot", "CRM A",
		openchoreo.ExternalResourceConfigKey{Key: "HUBSPOT_KEY", Secret: true})
	b := mustBuildExternalRT(t, "hubspot", "CRM B",
		openchoreo.ExternalResourceConfigKey{Key: "HUBSPOT_KEY", Secret: true},
		openchoreo.ExternalResourceConfigKey{Key: "HUBSPOT_PORTAL_ID", Secret: false})
	// Both left at the zero CreationTimestamp (the "absent" case). Different
	// schemas (1 vs 2 keys) hash to different RT names, so the deterministic
	// fallback (lexically greater metadata.name) has a real choice to make.
	if a.Metadata.Name == b.Metadata.Name {
		t.Fatalf("fixture bug: a/b must hash to different RT names, got %q for both", a.Metadata.Name)
	}

	wantDescription, wantKeyCount := "CRM A", 1
	if b.Metadata.Name > a.Metadata.Name {
		wantDescription, wantKeyCount = "CRM B", 2
	}

	for _, order := range [][2]openchoreo.ResourceType{{a, b}, {b, a}} {
		er := newExternalCatalogFixture(nil, order[0], order[1])
		h := NewMCPHandler(er, nil, nil, nil, nil, nil, nil)

		resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_external_resources", `{}`)))
		text := toolText(t, resp, false)
		var payload struct {
			ExternalResources []externalResourceView `json:"externalResources"`
		}
		if err := json.Unmarshal([]byte(text), &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if len(payload.ExternalResources) != 1 {
			t.Fatalf("externalResources = %+v, want exactly 1 entry", payload.ExternalResources)
		}
		got := payload.ExternalResources[0]
		if got.Description != wantDescription || len(got.ConfigKeys) != wantKeyCount {
			t.Errorf("list order %v: got %+v, want the deterministic winner (description=%q, %d config keys) regardless of order",
				order, got, wantDescription, wantKeyCount)
		}

		getResp := decodeRPC(t, postRPC(t, h, "org-1", callBody("get_external_resource_schema", `{"name":"hubspot"}`)))
		getText := toolText(t, getResp, false)
		var getPayload struct {
			Found            bool                 `json:"found"`
			ExternalResource externalResourceView `json:"externalResource"`
		}
		if err := json.Unmarshal([]byte(getText), &getPayload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if !getPayload.Found || getPayload.ExternalResource.Description != wantDescription || len(getPayload.ExternalResource.ConfigKeys) != wantKeyCount {
			t.Errorf("list order %v: get_external_resource_schema = %+v, want the same deterministic winner as list", order, getPayload)
		}
	}
}

func TestMCP_ListOrgEndpoints(t *testing.T) {
	h, _, ep := sampleHandler(t)
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
	er := newExternalCatalogFixture(nil)
	h := NewMCPHandler(er, nil, nil, nil, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_org_endpoints", `{}`)))
	text := toolText(t, resp, false)
	if text != `{"endpoints":[]}` {
		t.Errorf("payload = %q, want empty endpoints", text)
	}
}

func TestMCP_ListOrgComponentEndpoints(t *testing.T) {
	h, _, ep := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_org_component_endpoints", `{}`)))
	text := toolText(t, resp, false)

	var payload struct {
		Endpoints []orgComponentEndpointView `json:"endpoints"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Endpoints) != 2 {
		t.Fatalf("endpoints = %+v, want 2 entries", payload.Endpoints)
	}
	first := payload.Endpoints[0]
	if first.Project != "billing" || first.Component != "invoice-api" || first.Endpoint != "rest" ||
		first.Type != "HTTP" || first.Port != 8080 || first.BasePath != "/api" || !first.NamespaceVisible ||
		first.Owner != "wso2" || first.Repo != "billing-svc" || first.Subdir != "services/invoice-api" ||
		first.Branch != "main" {
		t.Errorf("unexpected first endpoint view: %+v", first)
	}
	if first.Spec.Availability != "inline" {
		t.Errorf("first.Spec.Availability = %q, want inline", first.Spec.Availability)
	}
	if first.Spec.InlineContent == "" {
		t.Errorf("first.Spec.InlineContent must be populated for the inline case")
	}
	second := payload.Endpoints[1]
	if second.Spec.Availability != "none" {
		t.Errorf("second.Spec.Availability = %q, want none", second.Spec.Availability)
	}
	if ep.lastResolvedOrg != "org-1" {
		t.Errorf("resolver called with org %q, want org-1", ep.lastResolvedOrg)
	}
	if !ep.lastResolvedCtxServiceIdentity {
		t.Errorf("tool-call context must be marked service identity — otherwise the OC transport forwards the MCP bearer as a user JWT and every catalog read 401s")
	}
}

func TestMCP_ListOrgComponentEndpoints_NilLister_Empty(t *testing.T) {
	er := newExternalCatalogFixture(nil)
	h := NewMCPHandler(er, nil, nil, nil, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_org_component_endpoints", `{}`)))
	text := toolText(t, resp, false)
	if text != `{"endpoints":[]}` {
		t.Errorf("payload = %q, want empty endpoints", text)
	}
}

func TestMCP_ListPlatformResourceTypes(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_platform_resource_types", `{}`)))
	text := toolText(t, resp, false)

	var payload struct {
		ResourceTypes []dependencies.PlatformResourceType `json:"resourceTypes"`
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
	er := newExternalCatalogFixture(nil)
	h := NewMCPHandler(er, nil, nil, nil, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("list_platform_resource_types", `{}`)))
	text := toolText(t, resp, false)
	if text != `{"resourceTypes":[]}` {
		t.Errorf("payload = %q, want empty resourceTypes", text)
	}
}

// ---- remote-git tools (endpoint spec discovery) --------------------------------

func TestMCP_GetRemoteGitFileContents(t *testing.T) {
	rg := &fakeRemoteGit{file: &RemoteGitFile{Content: "openapi: 3.0.0\n", SHA: "abc"}}
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, rg, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"owner":"acme","repo":"billing-svc","path":"specs/openapi.yaml","ref":"main"}`)))
	text := toolText(t, resp, false)

	var payload remoteGitFileView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Content != "openapi: 3.0.0\n" || payload.SHA != "abc" || payload.IsDirectory {
		t.Errorf("unexpected payload: %+v", payload)
	}
	// The org MUST be the verified context claim, never a tool arg.
	if rg.lastOrg != "org-1" {
		t.Errorf("reader saw org %q, want org-1 (from the verified claim)", rg.lastOrg)
	}
	if rg.lastOwner != "acme" {
		t.Errorf("reader saw owner %q, want acme", rg.lastOwner)
	}
}

func TestMCP_GetRemoteGitFileContents_Directory(t *testing.T) {
	rg := &fakeRemoteGit{file: &RemoteGitFile{IsDirectory: true, Entries: []RemoteGitEntry{
		{Path: "specs/openapi.yaml", Type: "file", SHA: "a"},
	}}}
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, rg, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"owner":"acme","repo":"billing-svc","path":"specs"}`)))
	text := toolText(t, resp, false)
	var payload remoteGitFileView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !payload.IsDirectory || len(payload.Entries) != 1 || payload.Entries[0].Path != "specs/openapi.yaml" {
		t.Errorf("unexpected directory payload: %+v", payload)
	}
}

// Binary content never rides a tool result as text. A real turn died on this:
// the model fetched an 868KB PDF through this tool, the raw bytes became ~1.5M
// junk tokens per model step, and the NUL bytes the read carried then killed the
// conversation's jsonb persist (Postgres rejects U+0000 anywhere in a jsonb
// document). The tool answers with the file's facts and a refusal instead — the
// model can still reason about the file existing.
func TestMCP_GetRemoteGitFileContents_BinaryIsRefusedAsFacts(t *testing.T) {
	pdf := "%PDF-1.4\n\x00\x00binary\xff\xfe"
	rg := &fakeRemoteGit{file: &RemoteGitFile{Content: pdf, SHA: "abc"}}
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, rg, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"owner":"acme","repo":"billing-svc","path":"specs/requirements/references/form.pdf"}`)))
	text := toolText(t, resp, false)

	var payload remoteGitFileView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Content != "" {
		t.Fatalf("binary content leaked into the tool result (%d bytes) — must be empty", len(payload.Content))
	}
	if payload.SHA != "abc" {
		t.Errorf("sha = %q, want abc (the facts still ride)", payload.SHA)
	}
	if payload.Note == "" || !strings.Contains(payload.Note, "binary") {
		t.Errorf("note = %q, want an explanation naming the file as binary", payload.Note)
	}
}

// A NUL byte alone is enough to withhold the content, and it is the byte that
// actually killed the persist. U+0000 IS valid UTF-8, so the UTF-8 half of the
// check cannot catch this one — the fixture is otherwise perfectly ordinary
// text, and the refusal must still fire.
func TestMCP_GetRemoteGitFileContents_ValidUTF8WithNULIsRefused(t *testing.T) {
	withNUL := "openapi: 3.0.3" + string(rune(0)) + "\ninfo:\n  title: still valid utf-8\n"
	if !utf8.ValidString(withNUL) {
		t.Fatal("fixture is not valid UTF-8 — it would exercise the wrong branch")
	}
	rg := &fakeRemoteGit{file: &RemoteGitFile{Content: withNUL, SHA: "def"}}
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, rg, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"owner":"acme","repo":"billing-svc","path":"specs/openapi.yaml"}`)))
	text := toolText(t, resp, false)

	var payload remoteGitFileView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Content != "" {
		t.Fatalf("NUL-bearing content leaked into the tool result (%q) — must be empty", payload.Content)
	}
	if payload.SHA != "def" {
		t.Errorf("sha = %q, want def (the facts still ride)", payload.SHA)
	}
	if payload.Note == "" || !strings.Contains(payload.Note, "binary") {
		t.Errorf("note = %q, want an explanation naming the file as binary", payload.Note)
	}
}

// Oversized text is truncated with a note, not returned whole: a tool result
// is prompt input, and an unbounded file becomes an unbounded prompt.
func TestMCP_GetRemoteGitFileContents_OversizedTextIsTruncated(t *testing.T) {
	huge := strings.Repeat("line of an enormous but honest yaml file\n", 10000) // ~420KB
	rg := &fakeRemoteGit{file: &RemoteGitFile{Content: huge, SHA: "abc"}}
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, rg, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"owner":"acme","repo":"billing-svc","path":"specs/openapi.yaml"}`)))
	text := toolText(t, resp, false)

	var payload remoteGitFileView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Content) >= len(huge) {
		t.Fatalf("content not truncated: %d bytes returned", len(payload.Content))
	}
	if len(payload.Content) == 0 {
		t.Fatal("truncation must keep a leading slice, not drop the file")
	}
	if payload.Note == "" || !strings.Contains(payload.Note, "truncated") {
		t.Errorf("note = %q, want a truncation notice", payload.Note)
	}
}

// The truncation walks back to a rune boundary, and an ASCII fixture cannot
// prove that: every byte is a rune start, so the walk-back loop never runs.
// This one puts a 3-byte rune straddling the cut, so a naive slice at
// maxToolFileBytes would hand the model a half-rune — invalid UTF-8 riding a
// prompt, and a jsonb persist that Postgres may well refuse.
func TestMCP_GetRemoteGitFileContents_TruncationNeverSplitsARune(t *testing.T) {
	// Land one byte short of the cap, then straddle it with "…" (E2 80 A6).
	huge := strings.Repeat("a", maxToolFileBytes-1) + "…" + strings.Repeat("b", 1024)
	rg := &fakeRemoteGit{file: &RemoteGitFile{Content: huge, SHA: "abc"}}
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, rg, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"owner":"acme","repo":"billing-svc","path":"specs/openapi.yaml"}`)))
	text := toolText(t, resp, false)

	var payload remoteGitFileView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !utf8.ValidString(payload.Content) {
		t.Error("truncated content is not valid UTF-8 — a rune was split at the cut")
	}
	if len(payload.Content) > maxToolFileBytes {
		t.Errorf("content is %d bytes, over the %d cap", len(payload.Content), maxToolFileBytes)
	}
	// The straddling rune is dropped whole rather than half-kept.
	if strings.HasSuffix(payload.Content, "\ufffd") {
		t.Error("truncation kept a replacement char — the rune was split, not dropped")
	}
}

func TestMCP_GetRemoteGitFileContents_OwnerMismatch_ToolError(t *testing.T) {
	// The reader refuses a cross-org owner; the handler must surface it as a
	// tool-level error (isError=true), not data.
	rg := &fakeRemoteGit{err: ErrOwnerNotInOrg}
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, rg, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"owner":"evilcorp","repo":"secret","path":"x"}`)))
	text := toolText(t, resp, true) // wantErr = true
	if !strings.Contains(text, "owner") {
		t.Errorf("tool error = %q, want it to mention the owner refusal", text)
	}
}

func TestMCP_GetRemoteGitFileContents_MissingArgs_ToolError(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"repo":"billing-svc","path":"x"}`))) // no owner
	toolText(t, resp, true)
}

func TestMCP_GetRemoteGitFileContents_NilReader_ToolError(t *testing.T) {
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, nil, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("get_remote_git_file_contents", `{"owner":"acme","repo":"r","path":"x"}`)))
	toolText(t, resp, true)
}

func TestMCP_SearchRemoteGitCode(t *testing.T) {
	rg := &fakeRemoteGit{hits: []RemoteGitSearchHit{
		{Path: "specs/openapi.yaml", SHA: "a"},
		{Path: "api/openapi.yaml", SHA: "b"},
	}}
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, rg, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("search_remote_git_code", `{"owner":"acme","repo":"billing-svc","query":"openapi"}`)))
	text := toolText(t, resp, false)
	var payload struct {
		Items []remoteGitSearchHitView `json:"items"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(payload.Items) != 2 || payload.Items[0].Path != "specs/openapi.yaml" {
		t.Errorf("unexpected payload: %+v", payload)
	}
	if rg.lastOrg != "org-1" {
		t.Errorf("reader saw org %q, want org-1", rg.lastOrg)
	}
}

func TestMCP_SearchRemoteGitCode_MissingQuery_ToolError(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1",
		callBody("search_remote_git_code", `{"owner":"acme","repo":"billing-svc"}`)))
	toolText(t, resp, true)
}

// The two remote-git tools must be advertised by tools/list.
func TestMCP_ToolsList_IncludesRemoteGitTools(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	result := resp.Result.(map[string]any)
	tools, _ := result["tools"].([]any)
	names := map[string]bool{}
	for _, tr := range tools {
		if m, ok := tr.(map[string]any); ok {
			names[m["name"].(string)] = true
		}
	}
	for _, want := range []string{"get_remote_git_file_contents", "search_remote_git_code"} {
		if !names[want] {
			t.Errorf("tools/list missing %q (got %v)", want, names)
		}
	}
}

// ---- spec tools (validate_openapi_spec, fetch_openapi_spec) -------------------

// specToolSampleSpec is a minimal valid OpenAPI 3.x document with 3 operations
// (mirrors artifacts' own sampleSpec fixture) used across the spec-tool tests.
const specToolSampleSpec = `openapi: 3.0.3
info: { title: Weather, version: "1.0" }
paths:
  /weather:
    get: { responses: { "200": { description: ok } } }
  /forecast:
    get: { responses: { "200": { description: ok } } }
    post: { responses: { "201": { description: created } } }
`

// toolSchema is the subset of an mcpTool.InputSchema this file's schema-shape
// assertions read.
type toolSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]map[string]any `json:"properties"`
	Required   []string                  `json:"required"`
}

// schemaOf decodes tool's InputSchema into toolSchema.
func schemaOf(t *testing.T, tool mcpTool) toolSchema {
	t.Helper()
	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshal inputSchema: %v", err)
	}
	var s toolSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("unmarshal inputSchema: %v", err)
	}
	return s
}

// TestMCP_ToolsList_SpecToolsSchema asserts validate_openapi_spec and
// fetch_openapi_spec are advertised with the exact schema shape the other
// tools use: an object with a single required string argument.
func TestMCP_ToolsList_SpecToolsSchema(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	raw, _ := json.Marshal(resp.Result)
	var result struct {
		Tools []mcpTool `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal tools: %v", err)
	}
	byName := map[string]mcpTool{}
	for _, tool := range result.Tools {
		byName[tool.Name] = tool
	}

	validate, ok := byName["validate_openapi_spec"]
	if !ok {
		t.Fatal("validate_openapi_spec missing from tools/list")
	}
	if s := schemaOf(t, validate); s.Type != "object" || s.Properties["content"] == nil ||
		len(s.Required) != 1 || s.Required[0] != "content" {
		t.Errorf("validate_openapi_spec schema = %+v, want object with required %q", s, "content")
	}

	fetch, ok := byName["fetch_openapi_spec"]
	if !ok {
		t.Fatal("fetch_openapi_spec missing from tools/list")
	}
	if s := schemaOf(t, fetch); s.Type != "object" || s.Properties["url"] == nil ||
		len(s.Required) != 1 || s.Required[0] != "url" {
		t.Errorf("fetch_openapi_spec schema = %+v, want object with required %q", s, "url")
	}
}

func TestMCP_ValidateOpenAPISpec_Good(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("validate_openapi_spec", fmt.Sprintf(`{"content":%q}`, specToolSampleSpec))))
	text := toolText(t, resp, false)

	var payload validateSpecView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if !payload.Valid {
		t.Fatalf("valid = false, want true (errors %v)", payload.Errors)
	}
	if payload.Operations != 3 {
		t.Errorf("operations = %d, want 3", payload.Operations)
	}
	if payload.NormalizedContent == "" {
		t.Errorf("normalizedContent is empty, want the canonical-form doc")
	}
	if len(payload.Errors) != 0 {
		t.Errorf("errors = %v, want empty", payload.Errors)
	}
}

// TestMCP_ValidateOpenAPISpec_Bad asserts an invalid document is reported IN
// the payload (valid=false, errors populated) — the tool call itself succeeds
// (isError=false); only a missing argument or unconfigured port is a tool
// error.
func TestMCP_ValidateOpenAPISpec_Bad(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("validate_openapi_spec", `{"content":"foo: bar"}`)))
	text := toolText(t, resp, false)

	var payload validateSpecView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Valid {
		t.Fatalf("valid = true, want false for a non-OpenAPI document")
	}
	if len(payload.Errors) == 0 {
		t.Fatalf("errors is empty, want at least one parse error")
	}
	if payload.NormalizedContent != "" {
		t.Errorf("normalizedContent = %q, want empty when invalid", payload.NormalizedContent)
	}
}

func TestMCP_ValidateOpenAPISpec_MissingContent_ToolError(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("validate_openapi_spec", `{}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "content") {
		t.Errorf("tool error text = %q, want it to name the missing argument", text)
	}
}

func TestMCP_ValidateOpenAPISpec_NilPort_ToolError(t *testing.T) {
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, nil, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("validate_openapi_spec", `{"content":"whatever"}`)))
	toolText(t, resp, true)
}

// handlerWithFetcher wires a stub SpecFetcher alongside the REAL
// spec.ValidateOpenAPI/NormalizeOpenAPIYAML, isolating fetch_openapi_spec's
// own logic (size cap, validate+normalize wiring) from the network. SSRF
// behavior itself is exercised separately, through the real
// spec.FetchSpecFromURL (see TestMCP_FetchOpenAPISpec_SSRFBlocked).
func handlerWithFetcher(fetch SpecFetcher) http.Handler {
	return NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, nil,
		spec.ValidateOpenAPI, spec.NormalizeOpenAPIYAML, fetch)
}

func TestMCP_FetchOpenAPISpec_Good(t *testing.T) {
	var gotURL string
	h := handlerWithFetcher(func(_ context.Context, url string) ([]byte, error) {
		gotURL = url
		return []byte(specToolSampleSpec), nil
	})
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{"url":"https://example.com/openapi.yaml"}`)))
	text := toolText(t, resp, false)

	var payload fetchSpecView
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Operations != 3 {
		t.Errorf("operations = %d, want 3", payload.Operations)
	}
	if payload.Content == "" {
		t.Errorf("content is empty, want the normalized doc")
	}
	if payload.SourceURL != "https://example.com/openapi.yaml" {
		t.Errorf("sourceUrl = %q, want the requested url echoed back", payload.SourceURL)
	}
	if gotURL != "https://example.com/openapi.yaml" {
		t.Errorf("fetcher saw url %q, want the requested url", gotURL)
	}
}

// TestMCP_FetchOpenAPISpec_TooLarge_ToolError asserts the tool-level 256 KiB
// cap rejects an oversized fetch with the exact "too large" message BEFORE
// validation runs — a context-safety guard layered on top of (never a
// substitute for) FetchSpecFromURL's own 5 MiB SSRF-hardened cap.
func TestMCP_FetchOpenAPISpec_TooLarge_ToolError(t *testing.T) {
	oversized := make([]byte, maxToolSpecBytes+1)
	h := handlerWithFetcher(func(context.Context, string) ([]byte, error) { return oversized, nil })
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{"url":"https://example.com/openapi.yaml"}`)))
	text := toolText(t, resp, true)
	if text != "spec too large — ask the user for a trimmed spec" {
		t.Errorf("tool error text = %q, want the exact too-large message", text)
	}
}

func TestMCP_FetchOpenAPISpec_AtCap_OK(t *testing.T) {
	// Exactly at the cap must NOT be rejected — only strictly over it.
	atCap := []byte(specToolSampleSpec + strings.Repeat(" ", maxToolSpecBytes-len(specToolSampleSpec)))
	if len(atCap) != maxToolSpecBytes {
		t.Fatalf("test fixture len = %d, want exactly %d", len(atCap), maxToolSpecBytes)
	}
	h := handlerWithFetcher(func(context.Context, string) ([]byte, error) { return atCap, nil })
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{"url":"https://example.com/openapi.yaml"}`)))
	toolText(t, resp, false)
}

func TestMCP_FetchOpenAPISpec_FailedValidation_ToolError(t *testing.T) {
	h := handlerWithFetcher(func(context.Context, string) ([]byte, error) { return []byte("foo: bar"), nil })
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{"url":"https://example.com/openapi.yaml"}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "failed validation") {
		t.Errorf("tool error text = %q, want it to report validation failure", text)
	}
}

func TestMCP_FetchOpenAPISpec_FetchError_ToolError(t *testing.T) {
	h := handlerWithFetcher(func(context.Context, string) ([]byte, error) { return nil, fmt.Errorf("connection refused") })
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{"url":"https://example.com/openapi.yaml"}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "connection refused") {
		t.Errorf("tool error text = %q, want it to carry the fetch error", text)
	}
}

func TestMCP_FetchOpenAPISpec_MissingURL_ToolError(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "url") {
		t.Errorf("tool error text = %q, want it to name the missing argument", text)
	}
}

func TestMCP_FetchOpenAPISpec_NilPort_ToolError(t *testing.T) {
	h := NewMCPHandler(newExternalCatalogFixture(nil), nil, nil, nil, nil, nil, nil)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{"url":"https://example.com/openapi.yaml"}`)))
	toolText(t, resp, true)
}

// TestMCP_FetchOpenAPISpec_SSRFBlocked_RealGuardUnweakened proves the MCP tool
// wiring reuses spec.FetchSpecFromURL's SSRF hardening AS-IS: routed
// through the real function (sampleHandler wires it, not a stub), a loopback
// URL must still be refused. A regression that wraps/relaxes the guard would
// make this test pass a real fetch through instead of rejecting it.
func TestMCP_FetchOpenAPISpec_SSRFBlocked_RealGuardUnweakened(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{"url":"https://127.0.0.1/openapi.yaml"}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "non-public address") {
		t.Errorf("tool error text = %q, want the SSRF guard's refusal message (FetchSpecFromURL weakened?)", text)
	}
}

// TestMCP_FetchOpenAPISpec_RejectsNonHTTPS proves the https-only half of the
// SSRF guard also passes through unchanged.
func TestMCP_FetchOpenAPISpec_RejectsNonHTTPS(t *testing.T) {
	h, _, _ := sampleHandler(t)
	resp := decodeRPC(t, postRPC(t, h, "org-1", callBody("fetch_openapi_spec", `{"url":"http://example.com/openapi.yaml"}`)))
	text := toolText(t, resp, true)
	if !strings.Contains(text, "https") {
		t.Errorf("tool error text = %q, want it to require https", text)
	}
}
