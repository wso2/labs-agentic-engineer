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

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
)

// ProtocolVersion is the MCP protocol version initialize answers, as aep-api's
// server does.
const ProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 error codes the server answers.
const (
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

// RPCError is a JSON-RPC error answered to the caller: a refusal of the pod's
// (unknown method, a tool outside AllowedTools, bad params) or one aep-api
// answered, relayed as is.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string { return fmt.Sprintf("json-rpc error %d: %s", e.Code, e.Message) }

// ErrUpstreamUnavailable means aep-api's MCP endpoint could not answer: it was
// unreachable, refused the pod's token, or answered something that is not a
// JSON-RPC response.
var ErrUpstreamUnavailable = errors.New("aep-api MCP is unavailable")

// Upstream calls aep-api's MCP endpoint: one JSON-RPC request, answering its
// result, an *RPCError for a JSON-RPC error, or ErrUpstreamUnavailable.
type Upstream interface {
	Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}

// Server answers the MCP socket's JSON-RPC methods: initialize, tools/list
// (the pinned descriptors, never forwarded) and tools/call (allow-list first,
// then the pod's remote-git tools or aep-api). Any other method is
// method-not-found.
type Server struct {
	Remote   RemoteGit
	Upstream Upstream
	// Log receives mcp.tools_call; nil is slog.Default().
	Log *slog.Logger
}

// Call answers one JSON-RPC request's result. A refusal or an error aep-api
// answered is an *RPCError; aep-api being unavailable is
// ErrUpstreamUnavailable.
func (s Server) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "initialize":
		return json.Marshal(map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "ae-studio-tools", "version": "1.0.0"},
		})
	case "tools/list":
		return json.Marshal(map[string]any{"tools": Tools()})
	case "tools/call":
		return s.callTool(ctx, params)
	default:
		return nil, &RPCError{Code: codeMethodNotFound, Message: "method not found: " + method}
	}
}

// callTool checks the name against AllowedTools before anything else, then
// runs a remote-git tool in the pod or forwards the call to aep-api. Only the
// name and the arguments are forwarded.
func (s Server) callTool(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil || call.Name == "" {
		return nil, &RPCError{Code: codeInvalidParams, Message: "invalid params: tools/call needs a tool name"}
	}
	if !allowed(call.Name) {
		return nil, &RPCError{Code: codeInvalidParams, Message: "tool not allowed: " + call.Name}
	}
	if len(call.Arguments) == 0 || string(call.Arguments) == "null" {
		call.Arguments = json.RawMessage(`{}`)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(call.Arguments, &object); err != nil {
		return nil, &RPCError{Code: codeInvalidParams, Message: "invalid params: arguments must be an object"}
	}
	if call.Name == toolGetFileContents || call.Name == toolSearchCode {
		var args remoteGitArgs
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return nil, &RPCError{Code: codeInvalidParams, Message: "invalid params: " + call.Name + " takes string arguments"}
		}
		s.logCall(ctx, call.Name, args, "pod")
		return s.Remote.callTool(ctx, call.Name, args), nil
	}
	s.logCall(ctx, call.Name, remoteGitArgs{}, "aep-api")
	// A string and a JSON object already decoded: the encoding cannot fail.
	forward, _ := json.Marshal(struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}{call.Name, call.Arguments})
	return s.Upstream.Call(ctx, "tools/call", forward)
}

// logCall logs mcp.tools_call {tool, repo?, upstream}: the tool, where it
// ran, and for a remote-git tool the owner/repo it addressed. Never the
// arguments' content or a token.
func (s Server) logCall(ctx context.Context, tool string, a remoteGitArgs, upstream string) {
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	attrs := []any{"tool", tool}
	if a.Owner != "" && a.Repo != "" {
		attrs = append(attrs, "repo", a.Owner+"/"+a.Repo)
	}
	log.InfoContext(ctx, "mcp.tools_call", append(attrs, "upstream", upstream)...)
}

// toolText is a successful tools/call result: one text block of v's JSON.
func toolText(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return toolError("encode tool result: " + err.Error())
	}
	return toolResult(string(b), false)
}

// toolError is a tools/call result flagged isError: a tool-level failure the
// model reads, not a protocol error.
func toolError(text string) json.RawMessage { return toolResult(text, true) }

func toolResult(text string, isError bool) json.RawMessage {
	r := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
	if isError {
		r["isError"] = true
	}
	b, _ := json.Marshal(r) // strings and bools only: cannot fail
	return b
}
