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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/gen/mcpsock"
	"github.com/wso2/aep/ae-studio-tools/internal/mcp"
	"github.com/wso2/aep/ae-studio-tools/internal/usage"
)

// fakeUpstream stands in for aep-api's /internal/v1/mcp. It serves whatever
// tools it is given (a 12th included) and counts tools/call per name.
type fakeUpstream struct {
	mu    sync.Mutex
	tools []string
	calls map[string]int
	lists int
	err   error
}

func (u *fakeUpstream) Call(_ context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err != nil {
		return nil, u.err
	}
	switch method {
	case "tools/list":
		u.lists++
		tools := make([]map[string]any, 0, len(u.tools))
		for _, n := range u.tools {
			tools = append(tools, map[string]any{"name": n, "inputSchema": map[string]any{"type": "object"}})
		}
		return json.Marshal(map[string]any{"tools": tools})
	case "tools/call":
		var call struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(params, &call); err != nil {
			return nil, err
		}
		if u.calls == nil {
			u.calls = map[string]int{}
		}
		u.calls[call.Name]++
		return json.Marshal(map[string]any{"content": []map[string]any{{"type": "text", "text": "from aep-api: " + call.Name}}})
	}
	return nil, &mcp.RPCError{Code: -32601, Message: "method not found"}
}

func (u *fakeUpstream) count(name string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls[name]
}

// aepAPITools are the ten allowed tools aep-api serves for the pod.
func aepAPITools() []string {
	return []string{
		"list_external_resources", "get_external_resource_schema", "list_org_endpoints",
		"list_org_component_endpoints", "list_platform_resource_types", "list_groups",
		"list_guardrail_policies", "validate_openapi_spec", "fetch_openapi_spec", "slice_openapi_spec",
	}
}

// fakeGitHubAPI is GitHub's REST API for the remote-git tools: the Contents API
// over the files it is given, and a request count per owner.
type fakeGitHubAPI struct {
	*httptest.Server
	mu    sync.Mutex
	files map[string]string // "owner/repo/path" → content
	calls map[string]int    // lower-cased owner → requests
	auth  []string
}

func newFakeGitHubAPI(t *testing.T) *fakeGitHubAPI {
	g := &fakeGitHubAPI{files: map[string]string{}, calls: map[string]int{}}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /repos/{owner}/{repo}/contents/{path}
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/repos/"), "/", 4)
		g.mu.Lock()
		if len(parts) > 0 {
			g.calls[strings.ToLower(parts[0])]++
		}
		g.auth = append(g.auth, r.Header.Get("Authorization"))
		content, ok := "", false
		if len(parts) == 4 && parts[2] == "contents" {
			content, ok = g.files[parts[0]+"/"+parts[1]+"/"+parts[3]]
		}
		g.mu.Unlock()
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		_, _ = fmt.Fprintf(w, `{"type":"file","sha":"blob-1","content":%q,"encoding":"base64"}`,
			base64.StdEncoding.EncodeToString([]byte(content)))
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *fakeGitHubAPI) serveFile(owner, repo, path, content string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.files[owner+"/"+repo+"/"+path] = content
}

func (g *fakeGitHubAPI) callsFor(owner string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls[strings.ToLower(owner)]
}

// mcpHarness serves MCPSocketRoutes on a real Unix socket bound by
// ListenSocket, over a fake GitHub and a fake aep-api, capturing the logs.
type mcpHarness struct {
	t      *testing.T
	github *fakeGitHubAPI
	client *http.Client
	logs   *syncBuffer
	usage  *fakeUsage
	nextID int
}

// fakeUsage is the usage outbox: it keeps what the socket hands in.
type fakeUsage struct {
	mu      sync.Mutex
	records []usage.TurnRecord
}

func (f *fakeUsage) Enqueue(r usage.TurnRecord) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, r)
}

func (f *fakeUsage) got() []usage.TurnRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]usage.TurnRecord(nil), f.records...)
}

type mcpHarnessOpt func(*mcpHarnessConfig)

type mcpHarnessConfig struct {
	owner     string
	snapshots files.Reader
}

func withOwner(o string) mcpHarnessOpt { return func(c *mcpHarnessConfig) { c.owner = o } }

func withSnapshots(r files.Reader) mcpHarnessOpt {
	return func(c *mcpHarnessConfig) { c.snapshots = r }
}

// syncBuffer is a log sink safe for the server's goroutines.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newMCPHarness(t *testing.T, up mcp.Upstream, opts ...mcpHarnessOpt) *mcpHarness {
	t.Helper()
	cfg := mcpHarnessConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	gh := newFakeGitHubAPI(t)
	logs := &syncBuffer{}
	sink := &fakeUsage{}
	server := mcp.Server{
		Remote:   mcp.RemoteGit{Owner: cfg.owner, Token: "gh-test-token", APIBase: gh.URL},
		Upstream: up,
		Log:      slog.New(slog.NewJSONHandler(logs, nil)),
	}
	path := filepath.Join(shortSocketDir(t), "mcp.sock")
	ln, err := ListenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler:           MCPSocketRoutes(MCPSocketDeps{MCP: server, Snapshots: cfg.snapshots, Usage: sink}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}}
	t.Cleanup(client.CloseIdleConnections)
	return &mcpHarness{t: t, github: gh, client: client, logs: logs, usage: sink}
}

// post sends body to path on the socket and answers the status and body.
func (h *mcpHarness) post(path, body string) (int, string) {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://mcp"+path, strings.NewReader(body))
	if err != nil {
		h.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := h.client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// call sends one JSON-RPC request and decodes the 200 reply, checking it
// echoes the request's id.
func (h *mcpHarness) call(method, params string) rpcReply {
	h.t.Helper()
	h.nextID++
	status, body := h.post("/mcp", fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, h.nextID, method, params))
	if status != http.StatusOK {
		h.t.Fatalf("%s: status %d: %s", method, status, body)
	}
	var r rpcReply
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		h.t.Fatalf("%s: reply %q: %v", method, body, err)
	}
	if r.JSONRPC != "2.0" || string(r.ID) != fmt.Sprint(h.nextID) {
		h.t.Fatalf("%s: envelope %q", method, body)
	}
	return r
}

type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// rpc answers a successful call's result; a JSON-RPC error or a tool result
// flagged isError fails the test.
func (h *mcpHarness) rpc(t *testing.T, method, params string) json.RawMessage {
	t.Helper()
	r := h.call(method, params)
	if r.Error != nil {
		t.Fatalf("%s: JSON-RPC error %d %q", method, r.Error.Code, r.Error.Message)
	}
	var tr toolResult
	if json.Unmarshal(r.Result, &tr) == nil && tr.IsError {
		t.Fatalf("%s: tool error %s", method, r.Result)
	}
	return r.Result
}

// rpcError answers the refusal a caller sees: a JSON-RPC error's message, or
// the text of a tool result flagged isError. A success fails the test.
func (h *mcpHarness) rpcError(t *testing.T, method, params string) string {
	t.Helper()
	r := h.call(method, params)
	if r.Error != nil {
		return r.Error.Message
	}
	var tr toolResult
	if err := json.Unmarshal(r.Result, &tr); err != nil || !tr.IsError || len(tr.Content) == 0 {
		t.Fatalf("%s: want a refusal, got %s", method, r.Result)
	}
	return tr.Content[0].Text
}

// logCount counts the log lines carrying every fragment.
func (h *mcpHarness) logCount(fragments ...string) int {
	n := 0
	for _, line := range strings.Split(h.logs.String(), "\n") {
		all := line != ""
		for _, f := range fragments {
			all = all && strings.Contains(line, f)
		}
		if all {
			n++
		}
	}
	return n
}

func sortedNames(list json.RawMessage) []string {
	var l struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(list, &l)
	names := make([]string, 0, len(l.Tools))
	for _, tl := range l.Tools {
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	return names
}

// Tools/list answers exactly the twelve names whatever aep-api
// serves; tools/call of any other name is refused in the pod; the remote-git
// tools run in the pod for AE_GITHUB_OWNER only (case-insensitive) and never
// call GitHub for another owner; the rest are forwarded to aep-api.
func TestMCP_AllowListPinnedAndEnforced(t *testing.T) {
	up := &fakeUpstream{tools: append(aepAPITools(), "drop_database")}
	s := newMCPHarness(t, up, withOwner("Acme-GH"))
	list := s.rpc(t, "tools/list", `{}`)
	want := []string{
		"fetch_openapi_spec", "get_external_resource_schema", "get_remote_git_file_contents", "list_external_resources",
		"list_groups", "list_guardrail_policies", "list_org_component_endpoints", "list_org_endpoints",
		"list_platform_resource_types", "search_remote_git_code", "slice_openapi_spec", "validate_openapi_spec",
	}
	if got := sortedNames(list); !reflect.DeepEqual(got, want) {
		t.Fatalf("tools/list = %v", got)
	}
	if up.lists != 0 {
		t.Fatalf("tools/list was forwarded to aep-api %d times", up.lists)
	}
	if msg := s.rpcError(t, "tools/call", `{"name":"drop_database","arguments":{}}`); !strings.Contains(msg, "not allowed") {
		t.Fatalf("drop_database refusal = %q", msg)
	}
	if n := up.count("drop_database"); n != 0 {
		t.Fatalf("drop_database reached aep-api %d times", n)
	}

	s.github.serveFile("acme-gh", "e2e-reference", "CONVENTIONS.md", "use gin")
	out := s.rpc(t, "tools/call", `{"name":"get_remote_git_file_contents","arguments":{"owner":"acme-gh","repo":"e2e-reference","path":"CONVENTIONS.md"}}`)
	if !strings.Contains(string(out), "use gin") {
		t.Fatalf("file read = %s", out)
	}
	if msg := s.rpcError(t, "tools/call", `{"name":"get_remote_git_file_contents","arguments":{"owner":"someone-else","repo":"x","path":"a"}}`); !strings.Contains(msg, "not in org") {
		t.Fatalf("other owner refusal = %q", msg)
	}
	if n := s.github.callsFor("someone-else"); n != 0 {
		t.Fatalf("GitHub called %d times for another owner", n)
	}
	if n := s.logCount(`"msg":"mcp.tools_call"`, `"tool":"get_remote_git_file_contents"`, `"repo":"acme-gh/e2e-reference"`, `"upstream":"pod"`); n != 1 {
		t.Fatalf("pod tools_call logs = %d\n%s", n, s.logs.String())
	}

	_ = s.rpc(t, "tools/call", `{"name":"list_groups","arguments":{}}`)
	if n := up.count("list_groups"); n != 1 {
		t.Fatalf("list_groups reached aep-api %d times", n)
	}
	if n := s.logCount(`"tool":"list_groups"`, `"upstream":"aep-api"`); n != 1 {
		t.Fatalf("aep-api tools_call logs = %d\n%s", n, s.logs.String())
	}
	_ = s.rpc(t, "tools/call", `{"name":"list_guardrail_policies","arguments":{}}`)
	if n := up.count("list_guardrail_policies"); n != 1 {
		t.Fatalf("list_guardrail_policies reached aep-api %d times", n)
	}
	// The log line is value-free: no arguments, no GitHub token.
	if logs := s.logs.String(); strings.Contains(logs, "CONVENTIONS.md") || strings.Contains(logs, "gh-test-token") {
		t.Fatalf("log carries values:\n%s", logs)
	}
}

// With no connected GitHub owner every remote-git call is refused per call,
// without a GitHub request; an empty owner argument is refused too.
func TestMCP_RemoteGitEmptyOwnerRefused(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	s.github.serveFile("acme-gh", "r", "a", "x")
	for _, args := range []string{
		`{"owner":"acme-gh","repo":"r","path":"a"}`,
		`{"owner":"","repo":"r","path":"a"}`,
	} {
		if msg := s.rpcError(t, "tools/call", `{"name":"get_remote_git_file_contents","arguments":`+args+`}`); !strings.Contains(msg, "not in org") {
			t.Fatalf("%s: refusal = %q", args, msg)
		}
	}
	if msg := s.rpcError(t, "tools/call", `{"name":"search_remote_git_code","arguments":{"owner":"acme-gh","repo":"r","query":"openapi"}}`); !strings.Contains(msg, "not in org") {
		t.Fatalf("search refusal = %q", msg)
	}
	o := newMCPHarness(t, &fakeUpstream{}, withOwner("acme-gh"))
	if msg := o.rpcError(t, "tools/call", `{"name":"search_remote_git_code","arguments":{"owner":"","repo":"r","query":"openapi"}}`); !strings.Contains(msg, "not in org") {
		t.Fatalf("empty owner argument: refusal = %q", msg)
	}
	if n := s.github.callsFor("acme-gh") + o.github.callsFor(""); n != 0 {
		t.Fatalf("GitHub called %d times", n)
	}
}

func TestMCP_InitializeAndUnknownMethod(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	var init struct {
		ProtocolVersion string         `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(s.rpc(t, "initialize", `{}`), &init); err != nil {
		t.Fatal(err)
	}
	if init.ProtocolVersion != "2024-11-05" || init.Capabilities["tools"] == nil {
		t.Fatalf("initialize = %+v", init)
	}
	r := s.call("resources/list", `{}`)
	if r.Error == nil || r.Error.Code != -32601 {
		t.Fatalf("resources/list = %+v", r)
	}
	r = s.call("tools/call", `{"arguments":{}}`)
	if r.Error == nil || r.Error.Code != -32602 {
		t.Fatalf("tools/call without a name = %+v", r)
	}
}

// A notification (no id) is accepted without a reply and reaches nothing.
func TestMCP_NotificationIsAccepted(t *testing.T) {
	up := &fakeUpstream{}
	s := newMCPHarness(t, up)
	status, body := s.post("/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if status != http.StatusAccepted || body != "" {
		t.Fatalf("notification = %d %q", status, body)
	}
	status, _ = s.post("/mcp", `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"list_groups","arguments":{}}}`)
	if status != http.StatusAccepted || up.count("list_groups") != 0 {
		t.Fatalf("tools/call notification = %d, forwarded %d", status, up.count("list_groups"))
	}
}

// aep-api unreachable is a 502 problem, not a tool result; a JSON-RPC error
// aep-api answers is relayed as one.
func TestMCP_UpstreamFailures(t *testing.T) {
	up := &fakeUpstream{err: fmt.Errorf("%w: aep-api answered 500", mcp.ErrUpstreamUnavailable)}
	s := newMCPHarness(t, up)
	status, body := s.post("/mcp", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_groups","arguments":{}}}`)
	if status != http.StatusBadGateway || !strings.Contains(body, `"code":"aep_api_unavailable"`) {
		t.Fatalf("upstream down = %d %s", status, body)
	}
	up.err = &mcp.RPCError{Code: -32602, Message: "invalid params"}
	r := s.call("tools/call", `{"name":"list_groups","arguments":{}}`)
	if r.Error == nil || r.Error.Code != -32602 || r.Error.Message != "invalid params" {
		t.Fatalf("relayed error = %+v", r)
	}
}

func TestMCP_RequestShapeIsValidated(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	for _, body := range []string{`{"id":1,"method":"tools/list"}`, `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`, `not json`} {
		if status, _ := s.post("/mcp", body); status != http.StatusBadRequest {
			t.Fatalf("%q = %d", body, status)
		}
	}
	big := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"validate_openapi_spec","arguments":{"content":"` + strings.Repeat("a", 2<<20) + `"}}}`
	if status, _ := s.post("/mcp", big); status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d", status)
	}
}

// The agent joins its collab Room on ae-collab's Room socket (the
// mount is its identity), so this socket mints no room token: the ae-studio
// client's token never leaves this container. /room-token is not in the
// contract, so it is 404 like any undeclared path.
func TestMCPSocket_NoRoomToken(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	if status, body := s.post("/room-token", ""); status != http.StatusNotFound {
		t.Fatalf("POST /room-token = %d %s", status, body)
	}
	spec, err := mcpsock.GetSpec()
	if err != nil {
		t.Fatal(err)
	}
	if spec.Paths.Find("/room-token") != nil {
		t.Fatal("the MCP socket contract still declares /room-token")
	}
}

// An unknown path is 404.
func TestMCPSocket_UnknownPathIs404(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	for _, path := range []string{"/nope", "/projects/greeter/x"} {
		req, _ := http.NewRequest(http.MethodGet, "http://mcp"+path, nil)
		resp, err := s.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s = %d", path, resp.StatusCode)
		}
	}
}

const turnRecordJSON = `{"turnId":"5f0c6c1e-6c39-4f0e-9a51-7a1d0e5a6b10","project":"greeter","conversationId":"6f0c6c1e-6c39-4f0e-9a51-7a1d0e5a6b10",` +
	`"kind":"plan","flow":"design","status":"failed","reason":"shutdown","code":"shutdown","baseRef":"a1","skillsRef":"b2",` +
	`"startedAt":"2026-10-03T10:00:00Z","finishedAt":"2026-10-03T10:01:00Z","author":{"id":"u1","name":"Ada"},"model":"m","modelHost":"h",` +
	`"inputTokens":11,"outputTokens":12,"cacheReadTokens":13,"cacheCreationTokens":14,"contextTokens":15}`

// POST /turn-usage hands the one record to the outbox, every field carried
// over, and answers 202.
func TestTurnUsage_HandsTheRecordToTheOutbox(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	if c, body := s.post("/turn-usage", turnRecordJSON); c != http.StatusAccepted {
		t.Fatalf("POST /turn-usage = %d %s", c, body)
	}
	got := s.usage.got()
	if len(got) != 1 {
		t.Fatalf("outbox = %d records", len(got))
	}
	sent, err := json.Marshal(got[0])
	if err != nil {
		t.Fatal(err)
	}
	var want, have map[string]any
	if err := json.Unmarshal([]byte(turnRecordJSON), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(sent, &have); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, have) {
		t.Fatalf("record = %s\nwant     %s", sent, turnRecordJSON)
	}
}

// A marketplace record (no project) without an author is valid; neither key
// is invented on the way to aep-api.
func TestTurnUsage_OptionalFieldsStayAbsent(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	record := strings.NewReplacer(`"project":"greeter",`, "", `"author":{"id":"u1","name":"Ada"},`, "",
		`"reason":"shutdown","code":"shutdown",`, "", `,"contextTokens":15`, "").Replace(turnRecordJSON)
	if c, body := s.post("/turn-usage", record); c != http.StatusAccepted {
		t.Fatalf("POST /turn-usage = %d %s", c, body)
	}
	got := s.usage.got()
	if len(got) != 1 || got[0].Author != nil || got[0].Project != "" || got[0].Reason != "" {
		t.Fatalf("outbox = %+v", got)
	}
	sent, _ := json.Marshal(got[0])
	for _, key := range []string{`"author"`, `"project"`, `"reason"`, `"code"`, `"contextTokens"`, `"designFeatures"`} {
		if strings.Contains(string(sent), key) {
			t.Fatalf("%s invented: %s", key, sent)
		}
	}
}

// A design turn's features reach the record aep-api gets as the agent sent
// them, in order.
func TestTurnUsage_DesignFeaturesCarriedOver(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	record := strings.Replace(turnRecordJSON, `,"contextTokens":15}`, `,"contextTokens":15,"designFeatures":["F1","F2"]}`, 1)
	if c, body := s.post("/turn-usage", record); c != http.StatusAccepted {
		t.Fatalf("POST /turn-usage = %d %s", c, body)
	}
	got := s.usage.got()
	if len(got) != 1 || !reflect.DeepEqual(got[0].DesignFeatures, []string{"F1", "F2"}) {
		t.Fatalf("outbox = %+v", got)
	}
	sent, _ := json.Marshal(got[0])
	if !strings.Contains(string(sent), `"designFeatures":["F1","F2"]`) {
		t.Fatalf("record = %s", sent)
	}
}

// A record the contract refuses is 400 and never reaches the outbox.
func TestTurnUsage_InvalidRecordIs400(t *testing.T) {
	s := newMCPHarness(t, &fakeUpstream{})
	for name, body := range map[string]string{
		"no turnId":   strings.Replace(turnRecordJSON, `"turnId":"5f0c6c1e-6c39-4f0e-9a51-7a1d0e5a6b10",`, "", 1),
		"bad kind":    strings.Replace(turnRecordJSON, `"kind":"plan"`, `"kind":"chat"`, 1),
		"extra field": strings.Replace(turnRecordJSON, `"model":"m"`, `"model":"m","cost":1`, 1),
		"a batch":     "[" + turnRecordJSON + "]",
		// aep-api refuses a whole batch for one bad feature ID, so the
		// socket refuses the one record and the agent sees why.
		"a lower-case feature":   strings.Replace(turnRecordJSON, `"model":"m"`, `"model":"m","designFeatures":["f1"]`, 1),
		"a story, not a feature": strings.Replace(turnRecordJSON, `"model":"m"`, `"model":"m","designFeatures":["F1.2"]`, 1),
		"a feature over 16":      strings.Replace(turnRecordJSON, `"model":"m"`, `"model":"m","designFeatures":["F`+strings.Repeat("1", 16)+`"]`, 1),
		"201 features":           strings.Replace(turnRecordJSON, `"model":"m"`, `"model":"m","designFeatures":[`+strings.Repeat(`"F1",`, 200)+`"F1"]`, 1),
	} {
		if c, out := s.post("/turn-usage", body); c != http.StatusBadRequest || !strings.Contains(out, "invalid_request") {
			t.Fatalf("%s: POST /turn-usage = %d %s", name, c, out)
		}
	}
	if n := len(s.usage.got()); n != 0 {
		t.Fatalf("outbox = %d records", n)
	}
}
