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
	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
	"github.com/wso2/aep/ae-studio-tools/internal/gen/mcpsock"
	"github.com/wso2/aep/ae-studio-tools/internal/mcp"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
	"github.com/wso2/aep/ae-studio-tools/internal/usage"
)

// The MCP socket is served contract-first from
// packages/contracts/sockets/ae-studio/mcp: the generated strict server in
// internal/gen/mcpsock, behind a request validator over the same contract.
// ae-design-agent is its only caller (the socket's emptyDir is mounted into
// that container and this one only, and the socket is 0660), so there is no
// token gate. It is the in-pod agent's only door out of the pod: POST /mcp
// (the JSON-RPC tool surface, mcp.Server), GET /projects/{p} and GET /skills (the snapshots a turn reads,
// mcp_sock_projects.go), and POST /turn-usage (a finished turn's record,
// handed to the usage outbox).

const (
	// mcpSocketBodyBytes caps a request body: aep-api's own cap on the
	// /internal/v1/mcp body a forwarded call becomes (1 MiB).
	mcpSocketBodyBytes int64 = 1 << 20
	// mcpSocketRequestBudget bounds one MCP socket request: a forwarded call
	// is one aep-api call (15 s, platform.aepAPITimeout, its 401 retry
	// included) and a remote-git call one GitHub read (15 s, mcp.remoteGitTimeout).
	mcpSocketRequestBudget = 20 * time.Second
)

// MCPSocketDeps is what the MCP socket's routes need.
type MCPSocketDeps struct {
	MCP mcp.Server
	// Snapshots resolves a project or the org's skills repository and writes
	// the snapshots the agent reads.
	Snapshots files.Reader
	// Usage is the outbox the turns' records are handed to (usage.Sender).
	Usage UsageOutbox
}

// UsageOutbox takes one finished turn's record for delivery to aep-api; it
// never blocks on delivery.
type UsageOutbox interface {
	Enqueue(usage.TurnRecord)
}

// mcpSocketHandler is validator → generated server. A path or method the
// contract does not declare is 404 at the validator; a request that does not
// match its operation is 400 invalid_request.
func mcpSocketHandler(d MCPSocketDeps) http.Handler {
	strict := mcpsock.NewStrictHandlerWithOptions(mcpSocketServer{mcp: d.MCP, snapshots: d.Snapshots, usage: d.Usage}, nil, mcpsock.StrictHTTPServerOptions{
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
	snapshots files.Reader
	usage     UsageOutbox
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

// PostTurnUsage hands one finished turn's record (validated against the
// contract) to the outbox and answers 202; delivery to aep-api is the
// outbox's (usage.Sender).
func (s mcpSocketServer) PostTurnUsage(_ context.Context, req mcpsock.PostTurnUsageRequestObject) (mcpsock.PostTurnUsageResponseObject, error) {
	s.usage.Enqueue(turnRecord(*req.Body))
	return mcpsock.PostTurnUsage202Response{}, nil
}

// turnRecord is the socket's TurnRecord as record-turn-usage takes it: the
// two contracts declare the same schema.
func turnRecord(r mcpsock.TurnRecord) usage.TurnRecord {
	out := usage.TurnRecord{
		TurnID:              r.TurnID,
		Project:             r.Project,
		ConversationID:      r.ConversationID,
		Kind:                aepapi.AEStudioTurnRecordKind(r.Kind),
		Flow:                r.Flow,
		Status:              aepapi.AEStudioTurnRecordStatus(r.Status),
		Reason:              aepapi.AEStudioTurnRecordReason(r.Reason),
		Code:                r.Code,
		BaseRef:             r.BaseRef,
		SkillsRef:           r.SkillsRef,
		StartedAt:           r.StartedAt,
		FinishedAt:          r.FinishedAt,
		Model:               r.Model,
		ModelHost:           r.ModelHost,
		InputTokens:         r.InputTokens,
		OutputTokens:        r.OutputTokens,
		CacheReadTokens:     r.CacheReadTokens,
		CacheCreationTokens: r.CacheCreationTokens,
		ContextTokens:       r.ContextTokens,
		DesignFeatures:      r.DesignFeatures,
	}
	if r.Author != nil {
		out.Author = &aepapi.AEStudioTurnRecordAuthor{ID: r.Author.ID, Name: r.Author.Name}
	}
	return out
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
