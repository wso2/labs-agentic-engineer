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

package edge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// Component test for the mounted MCP discovery route group: the real outer mux
// (NewHandler → mountRoutes), the real gate (auth.MCPGate) over a real
// PublisherTokenVerifier and StudioClientVerifier backed by a test JWKS, and
// the real MCP handler over a fake external-resource port. Proves the caller
// flow with an org's aep-publisher-<org> client token (the coding runner's;
// initialize → tools/list → tools/call) and with its recorded ae-studio-<org>
// client token (the AE Studio tools pod's), and the negatives: no token, a
// token minted by aep-api itself (aud aep-api-mcp), an ae-studio token for an
// org it is not recorded for, and an org planted in the request instead of
// the verified one.

const mcpTestIssuer, mcpTestKid = "platform-idp", "mcp-idp-kid"

// mcpTestReader is dependencies.ExternalResourceReader's real implementation
// (resources.ExternalResourceCatalog) wired over a ResourceClientMock, so the
// component test fixtures OC ResourceTypes — not external_resources rows —
// while still recording the org (namespace) each call was scoped to.
type mcpTestReader struct {
	*dependencies.ExternalResourceCatalog
	lastOrg string
}

// newMCPTestReader returns a reader whose backing ListResourceTypes serves
// rts verbatim for whatever namespace it's called with.
func newMCPTestReader(rts ...openchoreo.ResourceType) *mcpTestReader {
	f := &mcpTestReader{}
	rc := &mocks.ResourceClientMock{
		ListResourceTypesFunc: func(_ context.Context, namespace string) ([]openchoreo.ResourceType, error) {
			f.lastOrg = namespace
			return rts, nil
		},
	}
	f.ExternalResourceCatalog = dependencies.NewExternalResourceCatalog(rc)
	return f
}

// mcpIdP is a test platform IdP: a JWKS server over one RSA key, the
// publisher and ae-studio client verifiers aep-api builds over it (every org
// but orgWithoutStudioClient has ae-studio-<org> recorded), and a signer for
// any claim set.
type mcpIdP struct {
	priv     *rsa.PrivateKey
	verifier *auth.PublisherTokenVerifier
	studio   *auth.StudioClientVerifier
}

func newMCPIdP(t *testing.T) *mcpIdP {
	t.Helper()
	priv := newTestRSAKey(t)
	jwksSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwtassertion.JWKS{Keys: []jwtassertion.JSONWebKey{{
			Kty: "RSA", Kid: mcpTestKid, Use: "sig", Alg: "RS256",
			N: base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
			E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwksSrv.Close)
	jwks := jwtassertion.NewJWKSCache(jwksSrv.URL)
	v := auth.NewPublisherTokenVerifier(jwks, mcpTestIssuer, "aep-publisher-")
	studio := auth.NewStudioClientVerifier(jwks, mcpTestIssuer, recordedStudioClients{})
	if v == nil || studio == nil {
		t.Fatal("a verifier is nil")
	}
	return &mcpIdP{priv: priv, verifier: v, studio: studio}
}

// clientToken signs a client_credentials token the IdP would issue to the
// client whose id is aud, for the org ouHandle.
func (p *mcpIdP) clientToken(t *testing.T, aud, ouHandle string) string {
	t.Helper()
	return signTestJWT(t, p.priv, mcpTestKid, jwt.MapClaims{
		"iss": mcpTestIssuer, "aud": aud, "ouHandle": ouHandle,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
}

// publisherToken is the org's aep-publisher-<org> client token (the coding
// runner's MCP credential).
func (p *mcpIdP) publisherToken(t *testing.T, org string) string {
	return p.clientToken(t, "aep-publisher-"+org, org)
}

// studioToken is the org's ae-studio-<org> client token (the AE Studio tools
// pod's MCP credential).
func (p *mcpIdP) studioToken(t *testing.T, org string) string {
	return p.clientToken(t, "ae-studio-"+org, org)
}

// signTestJWT signs claims with RS256 under kid.
func signTestJWT(t *testing.T, priv *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign test JWT: %v", err)
	}
	return signed
}

func newTestRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return priv
}

// mcpSurface is the full handler with the MCP route group mounted on the
// publisher and ae-studio client verifiers (no task-token manager anywhere).
type mcpSurface struct {
	srv    *httptest.Server
	idp    *mcpIdP
	reader *mcpTestReader
}

// newMCPSurface builds the surface; edit tweaks AppParams before NewHandler.
func newMCPSurface(t *testing.T, edit ...func(*AppParams)) *mcpSurface {
	t.Helper()
	salesforceRT, err := openchoreo.BuildExternalResourceType(openchoreo.ExternalResourceTypeSpec{Name: "salesforce", Description: "CRM",
		Keys: []openchoreo.ExternalResourceConfigKey{{Key: "SALESFORCE_TOKEN", Secret: true}}, Scope: openchoreo.ExternalResourceScopeOrg})
	if err != nil {
		t.Fatalf("build salesforce RT fixture: %v", err)
	}
	s := &mcpSurface{idp: newMCPIdP(t), reader: newMCPTestReader(*salesforceRT)}
	p := AppParams{
		Config:               config.Config{},
		Deps:                 Deps{PublisherTokens: s.idp.verifier},
		InternalDeps:         InternalDeps{StudioClients: s.idp.studio},
		MCPExternalResources: s.reader,
		// MCPOrgEndpoints / MCPResourceTypes deliberately nil — those tools
		// degrade to empty results; the round-trip below uses the resource tools.
	}
	for _, e := range edit {
		e(&p)
	}
	s.srv = httptest.NewServer(NewHandler(p))
	t.Cleanup(s.srv.Close)
	return s
}

// postMCP POSTs a JSON-RPC body to the mounted path with the given bearer.
func postMCP(t *testing.T, srv *httptest.Server, bearer, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/internal/v1/mcp", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST mcp: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// rpcResult decodes a 200 JSON-RPC envelope and returns its result object.
func rpcResult(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var envelope struct {
		Result map[string]any  `json:"result"`
		Error  *map[string]any `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %+v", *envelope.Error)
	}
	return envelope.Result
}

// toolNames returns the tool names of a tools/list response, in order.
func toolNames(t *testing.T, resp *http.Response) []string {
	t.Helper()
	tools, _ := rpcResult(t, resp)["tools"].([]any)
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		names = append(names, name)
	}
	return names
}

// TestMCPRoutes_FullRoundTrip drives the complete caller flow through the
// mounted mux with an org's publisher client token.
func TestMCPRoutes_FullRoundTrip(t *testing.T) {
	s := newMCPSurface(t)
	tok := s.idp.publisherToken(t, "org-round-trip")

	// initialize
	result := rpcResult(t, postMCP(t, s.srv, tok, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`))
	if result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("protocolVersion = %v, want 2024-11-05", result["protocolVersion"])
	}

	// notifications/initialized → 202 (per Streamable-HTTP)
	notif := postMCP(t, s.srv, tok, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if notif.StatusCode != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202", notif.StatusCode)
	}

	// tools/call — the fake reader must be queried with the org from the CLAIM.
	body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_external_resources","arguments":{}}}`
	result = rpcResult(t, postMCP(t, s.srv, tok, body))
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("tools/call returned no content: %+v", result)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"salesforce"`) || !strings.Contains(text, `"SALESFORCE_TOKEN"`) {
		t.Errorf("tool payload = %q, want the registered resource + key", text)
	}
	if s.reader.lastOrg != "org-round-trip" {
		t.Errorf("port org = %q, want org-round-trip (from the token claim)", s.reader.lastOrg)
	}
}

// The publisher token lists the nine tools aep-api still serves; the two
// remote-git tools moved to the runner and the AE Studio tools pod, which
// serve them in-process.
func TestMCP_PublisherTokenListsNineToolsWithoutRemoteGit(t *testing.T) {
	s := newMCPSurface(t)
	names := toolNames(t, postMCP(t, s.srv, s.idp.publisherToken(t, "acme"), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if len(names) != 9 || slices.Contains(names, "get_remote_git_file_contents") || slices.Contains(names, "search_remote_git_code") {
		t.Fatalf("tools/list = %v, want the 9 non-remote-git tools", names)
	}
}

// The mount needs one verifier, no task-token manager: with one verifier
// alone, its own token opens MCP and the other caller's token is refused.
func TestMCP_MountsOnEitherVerifierAlone(t *testing.T) {
	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	publisherOnly := newMCPSurface(t, func(p *AppParams) { p.InternalDeps.StudioClients = nil })
	if resp := postMCP(t, publisherOnly.srv, publisherOnly.idp.publisherToken(t, "acme"), initialize); resp.StatusCode != http.StatusOK {
		t.Fatalf("publisher verifier alone, publisher token: got %d, want 200", resp.StatusCode)
	}
	if resp := postMCP(t, publisherOnly.srv, publisherOnly.idp.studioToken(t, "acme"), initialize); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("publisher verifier alone, ae-studio token: got %d, want 401", resp.StatusCode)
	}
	studioOnly := newMCPSurface(t, func(p *AppParams) { p.Deps.PublisherTokens = nil })
	if resp := postMCP(t, studioOnly.srv, studioOnly.idp.studioToken(t, "acme"), initialize); resp.StatusCode != http.StatusOK {
		t.Fatalf("ae-studio verifier alone, ae-studio token: got %d, want 200", resp.StatusCode)
	}
	if resp := postMCP(t, studioOnly.srv, studioOnly.idp.publisherToken(t, "acme"), initialize); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ae-studio verifier alone, publisher token: got %d, want 401", resp.StatusCode)
	}
}

// A token aep-api used to mint for MCP (aud aep-api-mcp, ocOrgId claim) opens
// nothing: on its own key (no verifier trusts it) or even on the IdP's key.
func TestMCP_MintedAepApiMcpTokenIs401(t *testing.T) {
	s := newMCPSurface(t)
	minted := jwt.MapClaims{
		"iss": "aep-bff", "aud": "aep-api-mcp", "ocOrgId": "acme",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for name, tok := range map[string]string{
		"own key": signTestJWT(t, newTestRSAKey(t), "aep-bff-kid", minted),
		"idp key": signTestJWT(t, s.idp.priv, mcpTestKid, minted),
	} {
		resp := postMCP(t, s.srv, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("minted MCP token (%s): got %d, want 401", name, resp.StatusCode)
		}
	}
}

// The AE Studio tools pod calls MCP with its org's recorded ae-studio-<org>
// client token (Task 9.H18): the tool runs for the org that client is recorded
// for, whatever org the request names.
func TestMCP_StudioClientTokenServesItsRecordedOrg(t *testing.T) {
	s := newMCPSurface(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_external_resources","arguments":{"orgHandle":"attacker-org"}}}`
	req, err := http.NewRequest(http.MethodPost, s.srv.URL+"/internal/v1/mcp?orgHandle=attacker-org", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.idp.studioToken(t, "acme"))
	req.Header.Set("X-Impersonate-Org", "attacker-org")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST mcp: %v", err)
	}
	defer resp.Body.Close()
	rpcResult(t, resp)
	if s.reader.lastOrg != "acme" {
		t.Fatalf("port org = %q, want acme (the org the ae-studio client is recorded for)", s.reader.lastOrg)
	}
}

// An ae-studio token that is not the client recorded for its org gets the
// same 401 as any other refused token, and no tool runs.
func TestMCP_UnrecordedStudioClientTokenIs401(t *testing.T) {
	s := newMCPSurface(t)
	for name, tok := range map[string]string{
		"org with no client recorded":   s.idp.studioToken(t, orgWithoutStudioClient),
		"audience naming another org":   s.idp.clientToken(t, "ae-studio-acme", "evil"),
		"audience without an org claim": s.idp.clientToken(t, "ae-studio-acme", ""),
	} {
		resp := postMCP(t, s.srv, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_external_resources","arguments":{}}}`)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: got %d, want 401", name, resp.StatusCode)
		}
	}
	if s.reader.lastOrg != "" {
		t.Fatalf("a refused token reached the port (org %q)", s.reader.lastOrg)
	}
}

// TestMCPRoutes_NoToken401 proves the mount is behind the verifier: an
// unauthenticated POST never reaches the JSON-RPC handler.
func TestMCPRoutes_NoToken401(t *testing.T) {
	s := newMCPSurface(t)
	resp := postMCP(t, s.srv, "", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestMCPRoutes_OrgFromClaimNotRequest plants a different org in every
// request-controlled slot; the port must still be scoped by the claim org.
func TestMCPRoutes_OrgFromClaimNotRequest(t *testing.T) {
	s := newMCPSurface(t)
	tok := s.idp.publisherToken(t, "claim-org")

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_external_resources","arguments":{"orgHandle":"attacker-org"}}}`
	req, err := http.NewRequest(http.MethodPost,
		s.srv.URL+"/internal/v1/mcp?orgHandle=attacker-org", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("X-Oc-Org-Id", "attacker-org")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if s.reader.lastOrg != "claim-org" {
		t.Fatalf("port org = %q, want claim-org (the signed claim must win over every request-supplied org)", s.reader.lastOrg)
	}
}

// TestMCPRoutes_NoVerifier404 proves the conditional mount: without either
// verifier nothing can verify a caller, so the path is not mounted at all.
func TestMCPRoutes_NoVerifier404(t *testing.T) {
	handler := NewHandler(AppParams{Config: config.Config{}})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/internal/v1/mcp", "application/json",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize"}`)))
	if err != nil {
		t.Fatalf("POST mcp: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route unmounted without a verifier)", resp.StatusCode)
	}
}

// newMCPSpecToolServer mounts the MCP route group with the spec-tool ports
// wired to fakes, so a validate_openapi_spec call reaches a real result. The
// fake validator records how many bytes of document reached it.
func newMCPSpecToolServer(t *testing.T) (*httptest.Server, string, *int) {
	t.Helper()
	seen := new(int)
	s := newMCPSurface(t, func(p *AppParams) {
		p.MCPSpecValidator = func(raw []byte) (int, error) {
			*seen = len(raw)
			return 1, nil
		}
		p.MCPSpecNormalizer = func(content string) (string, error) { return "normalized", nil }
	})
	return s.srv, s.idp.publisherToken(t, "org-spec"), seen
}

// validateSpecCall is a validate_openapi_spec tools/call whose whole JSON-RPC
// body is exactly size bytes (the inline document is padding).
func validateSpecCall(size int) string {
	const head = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"validate_openapi_spec","arguments":{"content":"`
	const tail = `"}}}`
	return head + strings.Repeat("a", size-len(head)-len(tail)) + tail
}

// The design agent sends a whole OpenAPI document inline; one of 900 KiB
// passes the internal body cap and reaches the tool.
func TestMCPRoutes_LargeInlineSpecUnderCap(t *testing.T) {
	srv, tok, seen := newMCPSpecToolServer(t)
	result := rpcResult(t, postMCP(t, srv, tok, validateSpecCall(900<<10)))
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("tools/call returned no content: %+v", result)
	}
	if text, _ := content[0].(map[string]any)["text"].(string); !strings.Contains(text, `"valid":true`) {
		t.Errorf("tool payload = %q, want valid=true", text)
	}
	if *seen < 899<<10 {
		t.Errorf("validator saw %d bytes, want the whole inline document", *seen)
	}
}

// One byte over 1 MiB is refused at the edge with 413 (C2), never a
// JSON-RPC parse error from a truncated body.
func TestMCPRoutes_InlineSpecOverCap413(t *testing.T) {
	srv, tok, seen := newMCPSpecToolServer(t)
	resp := postMCP(t, srv, tok, validateSpecCall(1<<20+1))
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
	if *seen != 0 {
		t.Errorf("validator saw %d bytes, want the call refused before the tool", *seen)
	}
}
