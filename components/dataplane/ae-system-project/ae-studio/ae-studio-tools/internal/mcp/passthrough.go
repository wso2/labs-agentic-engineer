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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
)

// mcpCaller is the generated aep-api client's call-mcp-tool operation
// (POST /internal/v1/mcp, a raw JSON-RPC body).
type mcpCaller interface {
	CallMcpToolWithBodyWithResponse(ctx context.Context, contentType string, body io.Reader, reqEditors ...aepapi.RequestEditorFn) (*aepapi.CallMcpToolResponse, error)
}

// aepAPIUpstream forwards to aep-api's MCP endpoint over the generated client,
// which carries the org's ae-studio client token (platform.NewAEPAPI: one
// retry after a 401). aep-api binds the org that client is recorded for,
// never one the request names.
type aepAPIUpstream struct {
	client mcpCaller
}

// NewAEPAPIUpstream is the Upstream over the aep-api client.
func NewAEPAPIUpstream(c mcpCaller) Upstream { return aepAPIUpstream{client: c} }

// upstreamRequestID is the id of every forwarded request: one request per
// HTTP exchange, so the id only has to be present (aep-api answers a request
// without one as a notification).
const upstreamRequestID = 1

// Call sends one JSON-RPC request and answers aep-api's result. A JSON-RPC
// error is relayed as *RPCError; anything but a 200 JSON-RPC response is
// ErrUpstreamUnavailable naming the status, never the body.
func (u aepAPIUpstream) Call(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	body, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{"2.0", upstreamRequestID, method, params})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	resp, err := u.client.CallMcpToolWithBodyWithResponse(ctx, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("%w: aep-api answered %d", ErrUpstreamUnavailable, resp.StatusCode())
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body, &reply); err != nil {
		return nil, fmt.Errorf("%w: aep-api answered an unreadable body", ErrUpstreamUnavailable)
	}
	if reply.Error != nil {
		return nil, &RPCError{Code: reply.Error.Code, Message: reply.Error.Message}
	}
	if len(reply.Result) == 0 || string(reply.Result) == "null" {
		return nil, fmt.Errorf("%w: aep-api answered no result", ErrUpstreamUnavailable)
	}
	return reply.Result, nil
}
