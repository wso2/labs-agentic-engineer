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
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/gen/mcpsock"
	"github.com/wso2/aep/ae-studio-tools/internal/mcp"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// The MCP socket is served contract-first from
// packages/contracts/sockets/ae-studio/mcp: the generated strict server in
// internal/gen/mcpsock, behind a request validator over the same contract.
// ae-design-agent is its only caller (the socket's emptyDir is mounted into
// that container and this one only, and the socket is 0660), so there is no
// token gate. It is the in-pod agent's only door out of the pod: POST /mcp
// (the JSON-RPC tool surface, mcp.Server), POST /room-token (the collab room
// token), and GET /projects/{p} and GET /skills (the snapshots a turn reads,
// mcp_sock_projects.go). POST /turn-usage is served by Task 3.7 and answers
// 404 until then.

const (
	// mcpSocketBodyBytes caps a request body: aep-api's own cap on the
	// /internal/v1/mcp body a forwarded call becomes (1 MiB).
	mcpSocketBodyBytes int64 = 1 << 20
	// mcpSocketRequestBudget bounds one MCP socket request: a forwarded call
	// is one aep-api call (15 s, platform.aepAPITimeout, its 401 retry
	// included), a remote-git call one GitHub read (15 s, mcp.remoteGitTimeout)
	// and a room token one token-endpoint call (10 s).
	mcpSocketRequestBudget = 20 * time.Second
)

// RoomTokens mints the token the in-pod agent joins its collab room with: the
// ae-studio-<org> client's (platform.ClientCredentials), the only client
// ae-collab's local listener accepts.
type RoomTokens interface {
	TokenWithExpiry(ctx context.Context) (string, time.Time, error)
}

// MCPSocketDeps is what the MCP socket's routes need.
type MCPSocketDeps struct {
	MCP        mcp.Server
	RoomTokens RoomTokens
	// Snapshots resolves a project or the org's skills repository and writes
	// the snapshots the agent reads.
	Snapshots files.Reader
}

// mcpSocketHandler is validator → generated server. A path or method the
// contract does not declare is 404 at the validator; a request that does not
// match its operation is 400 invalid_request.
func mcpSocketHandler(d MCPSocketDeps) http.Handler {
	strict := mcpsock.NewStrictHandlerWithOptions(mcpSocketServer{mcp: d.MCP, rooms: d.RoomTokens, snapshots: d.Snapshots}, nil, mcpsock.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  writeMCPSocketRequestError,
		ResponseErrorHandlerFunc: writeMCPSocketResponseError,
	})
	mux := http.NewServeMux()
	mcpsock.HandlerWithOptions(strict, mcpsock.StdHTTPServerOptions{
		BaseRouter:       mux,
		ErrorHandlerFunc: writeMCPSocketRequestError,
	})
	mux.Handle("/", http.HandlerFunc(notFound))
	return requestValidator(mustRouter("mcp socket", mcpsock.GetSpec).FindRoute, mcpSocketInvalidCode, mux)
}

// mcpSocketInvalidCode is the problem code of a request the contract refuses.
const mcpSocketInvalidCode = "invalid_request"

// mcpSocketServer implements the MCP socket operations.
type mcpSocketServer struct {
	mcp       mcp.Server
	rooms     RoomTokens
	snapshots files.Reader
}

var _ mcpsock.StrictServerInterface = mcpSocketServer{}

// CallMcp answers one JSON-RPC request. A notification (no id) is accepted
// with 202 and goes nowhere; a refusal or an error aep-api answered is a
// JSON-RPC error on 200; aep-api unavailable is 502 aep_api_unavailable.
func (s mcpSocketServer) CallMcp(ctx context.Context, req mcpsock.CallMcpRequestObject) (mcpsock.CallMcpResponseObject, error) {
	if req.Body.ID == nil {
		return mcpsock.CallMcp202Response{}, nil
	}
	params, err := json.Marshal(req.Body.Params)
	if err != nil {
		return nil, err
	}
	result, err := s.mcp.Call(ctx, req.Body.Method, params)
	var rpcErr *mcp.RPCError
	switch {
	case errors.As(err, &rpcErr):
		return jsonRPCReply{ID: req.Body.ID, Error: &jsonRPCError{Code: rpcErr.Code, Message: rpcErr.Message}}, nil
	case err != nil:
		slog.WarnContext(ctx, "mcp.upstream_failed", "method", req.Body.Method, "error", err)
		return mcpsock.CallMcp502ApplicationProblemPlusJSONResponse(
			mcpSocketProblem(http.StatusBadGateway, "aep_api_unavailable", "aep-api could not answer the call")), nil
	}
	return jsonRPCReply{ID: req.Body.ID, Result: result}, nil
}

// MintRoomToken answers a token for the agent's collab room join. The token
// is never logged; a failure names the token endpoint's status only.
func (s mcpSocketServer) MintRoomToken(ctx context.Context, _ mcpsock.MintRoomTokenRequestObject) (mcpsock.MintRoomTokenResponseObject, error) {
	tok, exp, err := s.rooms.TokenWithExpiry(ctx)
	if err == nil && exp.IsZero() {
		err = errors.New("token endpoint named no lifetime")
	}
	if err != nil {
		slog.WarnContext(ctx, "room_token.mint_failed", "error", err)
		return mcpsock.MintRoomToken502ApplicationProblemPlusJSONResponse{
			ProblemApplicationProblemPlusJSONResponse: mcpsock.ProblemApplicationProblemPlusJSONResponse(
				mcpSocketProblem(http.StatusBadGateway, "idp_unavailable", "the room token could not be minted")),
		}, nil
	}
	return mcpsock.MintRoomToken200JSONResponse{Token: tok, ExpiresAt: exp}, nil
}

// PostTurnUsage is served by Task 3.7; 404 until then.
func (s mcpSocketServer) PostTurnUsage(context.Context, mcpsock.PostTurnUsageRequestObject) (mcpsock.PostTurnUsageResponseObject, error) {
	return notServedYetResponse{}, nil
}

// notServedYetResponse is the 404 of an operation the contract declares
// without one, until its task serves it.
type notServedYetResponse struct{}

func (notServedYetResponse) VisitPostTurnUsageResponse(w http.ResponseWriter) error {
	return writeNotFound(w)
}

func writeNotFound(w http.ResponseWriter) error {
	notFound(w, nil)
	return nil
}

// jsonRPCReply is a JSON-RPC 2.0 response carrying the request's id and
// either the result as answered (raw, so aep-api's result passes through
// byte for byte) or an error. The generated JSONRPCResponse cannot be used:
// its error member is a struct and would always be sent.
type jsonRPCReply struct {
	ID     any
	Result json.RawMessage
	Error  *jsonRPCError
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (r jsonRPCReply) VisitCallMcpResponse(w http.ResponseWriter) error {
	body := struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *jsonRPCError   `json:"error,omitempty"`
	}{"2.0", r.ID, r.Result, r.Error}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(body)
}

// mcpSocketProblem is the body problem.Write sends, as the generated type.
func mcpSocketProblem(status int, code, detail string) mcpsock.Problem {
	return mcpsock.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail, Code: code}
}

// writeMCPSocketRequestError answers a request the generated binder could not
// parse.
func writeMCPSocketRequestError(w http.ResponseWriter, _ *http.Request, _ error) {
	problem.Write(w, http.StatusBadRequest, mcpSocketInvalidCode, "the request does not match the contract")
}

// writeMCPSocketResponseError answers a handler error no typed response
// covers: a response that failed to encode.
func writeMCPSocketResponseError(w http.ResponseWriter, r *http.Request, _ error) {
	slog.Error("mcp_socket.handler_failed", "path", r.URL.Path)
	problem.Write(w, http.StatusInternalServerError, "internal_error", "the request could not be completed")
}
