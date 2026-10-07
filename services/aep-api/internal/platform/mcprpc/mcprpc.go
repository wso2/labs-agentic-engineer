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

// Package mcprpc is the Model Context Protocol server plumbing aep-api's MCP
// surfaces share: JSON-RPC 2.0 over the Streamable-HTTP transport in its
// non-streaming single-response form. The client POSTs one JSON-RPC request
// and gets one application/json answer; there are no sessions and no SSE
// stream, so a surface mounts only POST and a GET is the router's 405, which
// the transport allows. Each surface supplies its own tools; this package
// owns the protocol around them.
package mcprpc

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
)

// ProtocolVersion is the MCP revision every surface answers initialize with.
const ProtocolVersion = "2024-11-05"

// JSON-RPC 2.0 error codes the plumbing and its tool callers use.
const (
	CodeParseError     = -32700
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
)

// Request is one JSON-RPC request or notification.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // absent for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool is one entry of a tools/list answer.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema"`
}

// ToolCaller answers one tools/call request. It writes exactly one answer:
// WriteToolText or WriteToolError for a tool's own outcome, or WriteError for
// a protocol-level failure such as params that do not decode.
type ToolCaller func(w http.ResponseWriter, r *http.Request, req Request)

// Server is one MCP surface: its identity, its tool list and its tool caller.
type Server struct {
	Name, Version string
	Tools         []Tool
	Call          ToolCaller
}

// Serve decodes one JSON-RPC message from r and answers it. A notification
// (no id) gets a 202 with no body; initialize, ping and tools/list are
// answered from s; tools/call goes to s.Call; any other method is
// method-not-found.
func (s Server) Serve(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, nil, CodeParseError, "parse error")
		return
	}
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	switch req.Method {
	case "initialize":
		WriteResult(w, req.ID, map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		})
	case "ping":
		WriteResult(w, req.ID, map[string]any{})
	case "tools/list":
		WriteResult(w, req.ID, map[string]any{"tools": s.Tools})
	case "tools/call":
		s.Call(w, r, req)
	default:
		WriteError(w, req.ID, CodeMethodNotFound, "method not found: "+req.Method)
	}
}

// WriteResult answers id with result.
func WriteResult(w http.ResponseWriter, id json.RawMessage, result any) {
	writeJSON(w, response{JSONRPC: "2.0", ID: id, Result: result})
}

// WriteError answers id with a JSON-RPC error.
func WriteError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	writeJSON(w, response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg}})
}

// WriteToolText answers a tools/call with a single text block.
func WriteToolText(w http.ResponseWriter, id json.RawMessage, text string) {
	WriteResult(w, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

// WriteToolError answers a tools/call with a text block flagged isError: the
// tool ran and failed, which the model reads, as opposed to a protocol error.
func WriteToolError(w http.ResponseWriter, id json.RawMessage, text string) {
	WriteResult(w, id, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	})
}

// MustJSON marshals v for a text block, or a JSON error object if it cannot.
func MustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("{\"error\":%q}", err.Error())
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("mcp: encode response", "error", err)
	}
}
