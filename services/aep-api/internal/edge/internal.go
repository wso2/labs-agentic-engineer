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
	"errors"
	"net/http"

	"github.com/getkin/kin-openapi/routers"

	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/dependencies/mcpdiscovery"
	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/ops"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// The internal service-to-service route group (/internal/v1), served CONTRACT-FIRST
// from packages/contracts/api/internal/v1 (generated strict server in
// internal/igen), mounted once at /internal/v1/. It is NOT wrapped by the
// user-JWT middleware. Every request is body-capped (capInternalBody) and
// validated against the embedded internal spec (requestValidator); every
// generated operation then passes internalGate, which verifies the caller's
// credential for the op's route group (a runner's publisher-cc bearer against
// the cycle named in the path, the INT-6 fence; the SRE handoff bearer for
// sre/) and binds the verified org into the context. The raw MCP routes carry their
// own verifier. The spec is non-public, never gateway-advertised.
//
// RUNNER LOCKSTEP: the credentials-refresh response body is projected from the
// organization domain's RefreshResponse onto igen.RefreshResponse (toIgenRefresh)
// — the schema pins the wire shape, so the bytes cannot drift from what the
// runner expects (igen stays a leaf; it cannot import the domain — §7).

// InternalDeps carries the services + authorizer the internal S2S operations
// need. main.go (internal/app) fills it with real instances.
type InternalDeps struct {
	CredsRefresh organization.CredentialsRefreshService
	// RunnerAuth verifies runner publisher-cc bearers against the
	// path execution id. nil fails closed: every runner op answers 503.
	RunnerAuth *auth.RunnerAuthorizer
	// SREHandoff verifies aep-mcp-server's static SRE handoff bearer for the
	// sre/ ops. It is the same instance that switches auto-RCA on
	// (internal/app). nil fails closed: every sre/ op answers 401.
	SREHandoff *auth.SREHandoffVerifier
	// Issues backs sre-list-issues and sre-create-issue (the same issue
	// service as the console's ops); RcaReports backs sre-create-rca-report.
	// A nil one answers 503 for its ops.
	Issues     sourcecontrol.IssueService
	RcaReports ops.Repository
	// ValidationContext backs the validation-context runner callback; a nil
	// provider answers 503 for that op. A test user's login is NOT served here —
	// it is published on the roles gate ticket, which is where the validation
	// agent reads it (ADR-0022).
	ValidationContext validation.ContextProvider
	// MCP serves POST /internal/v1/mcp (call-mcp-tool) and PlaygroundToken
	// POST /internal/v1/mcp/playground-token. Each is already wrapped in its
	// own verifier; nil leaves the route unmounted. The playground mint is
	// local-dev only and goes with token minting (phase 5).
	MCP             http.Handler
	PlaygroundToken http.Handler
}

// internalServer implements igen.StrictServerInterface.
type internalServer struct {
	deps InternalDeps
}

var _ igen.StrictServerInterface = (*internalServer)(nil)

// newInternalV1Handler assembles the internal edge, outermost first:
//
//	body cap (capInternalBody)        1 MiB default, per-op overrides
//	→ request validator               kin-openapi against the embedded internal spec
//	→ inner mux                       raw MCP routes + generated router
//	→ internalGate → strict wrapper   generated ops only (envelope error writers)
//
// The inner mux registers full paths, so a path no row names 404s.
func newInternalV1Handler(deps InternalDeps) http.Handler {
	strict := igen.NewStrictHandlerWithOptions(
		&internalServer{deps: deps},
		[]igen.StrictMiddlewareFunc{internalGate(deps)},
		igen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  writeRequestError,
			ResponseErrorHandlerFunc: writeResponseError,
		},
	)
	mux := http.NewServeMux()
	if deps.MCP != nil {
		mux.Handle("POST "+internalV1+"/mcp", deps.MCP)
	}
	if deps.PlaygroundToken != nil {
		mux.Handle("POST "+internalV1+"/mcp/playground-token", deps.PlaygroundToken)
	}
	igen.HandlerWithOptions(strict, igen.StdHTTPServerOptions{
		BaseURL:          internalV1,
		BaseRouter:       mux,
		ErrorHandlerFunc: writeRequestError,
	})
	return capInternalBody(internalRouter(), internalBodyCaps, requestValidator(internalRouter(), mux))
}

// internalDefaultBodyBytes caps every /internal/v1 request body (03 §4);
// internalBodyCaps lists the operations allowed more. Phase 4 adds
// ingest-webhook-event at 25 MiB (GitHub's payload maximum).
const internalDefaultBodyBytes int64 = 1 << 20

var internalBodyCaps = map[string]int64{}

// capInternalBody bounds every internal request body before anything reads it.
// The limit is the matched operation's entry in caps, else
// internalDefaultBodyBytes. A declared Content-Length over the limit is
// answered 413 here, before next runs: call-mcp-tool is absent from the
// embedded spec (excluded from generation), so the validator never reads an
// MCP body, and an over-limit one would otherwise reach mcpdiscovery truncated
// and come back as a JSON-RPC parse error. MCP bodies are therefore capped but
// not schema-validated. A body of unknown length (chunked) is bounded by
// http.MaxBytesReader instead: whoever reads past the limit gets a
// *http.MaxBytesError (the validator maps it to the same 413).
func capInternalBody(router routers.Router, caps map[string]int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := internalDefaultBodyBytes
		if route, _, err := router.FindRoute(r); err == nil && route.Operation != nil {
			if c, ok := caps[route.Operation.OperationID]; ok {
				limit = c
			}
		}
		if r.ContentLength > limit {
			writeBodyTooLarge(w)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// internalGate is /internal/v1's deny-by-default gate table, one entry per
// route group (path prefix under /internal/v1):
//
//	executions/, validation/   coding runner   publisher token, cycle fence (cycle id in the path)
//	sre/                       SRE handoff     SRE handoff bearer, binds its one org + the incident context
//	mcp                        runner, agent   its own verifier (raw route, not this middleware)
//	anything else              -               denied (401)
//
// Each generated operation must present the credential of its route group, and
// the verified org is bound into the context. A credential opens its own group
// only: a publisher token never clears sre/, the SRE bearer never clears a
// runner op. There are deliberately NO carve-outs: an operation whose request
// shape the gate does not know is denied outright, so adding an internal op
// means teaching this gate its credential first.
//
// The refresh operation still spells its parameter `executionId` on the wire; the
// value is the dispatched cycle id, the same naming debt AEP_TASK_ID carries.
func internalGate(deps InternalDeps) igen.StrictMiddlewareFunc {
	return func(f igen.StrictHandlerFunc, operationID string) igen.StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			var cycleID string
			switch req := request.(type) {
			case igen.RunnerRefreshCredentialsRequestObject:
				cycleID = req.ExecutionID
			case igen.RunnerValidationContextRequestObject:
				cycleID = req.CycleID
			case igen.SreListIssuesRequestObject, igen.SreCreateIssueRequestObject, igen.SreCreateRcaReportRequestObject:
				claims, ok := deps.SREHandoff.Verify(r.Header.Get("Authorization")) // nil verifier: false, fails closed
				if !ok {
					return nil, errUnauthorized("SRE handoff bearer required")
				}
				ctx = auth.WithClaims(ctx, claims)
				ctx = sourcecontrol.WithIncidentContext(ctx, sreHandoffIncidentID)
				return f(tenant.WithBoundOrg(ctx, claims.OuHandle), w, r, request)
			default:
				return nil, errUnauthorized("unauthenticated internal operation: " + operationID)
			}
			if deps.RunnerAuth == nil {
				return nil, errServiceUnavailable("runner auth not configured")
			}
			caller, err := deps.RunnerAuth.Authorize(ctx, r.Header.Get("Authorization"), cycleID)
			if err != nil {
				return nil, mapRunnerAuthError(err)
			}
			return f(tenant.WithBoundOrg(ctx, string(caller.Org)), w, r, request)
		}
	}
}

// sreHandoffIncidentID is the opaque incident identity the sre/ gate binds onto
// every request it authenticates (sourcecontrol.WithIncidentContext). It is
// intentionally a constant, not a per-alert value: CreateIssue only uses it
// (alongside org/project/componentName) to compute the dedupe hash, and the
// SRE-handoff design deliberately dedupes by component alone, not by a
// per-alert signature (docs/design/draft/2026-09-17-sre-agent-extensions-handoff.md
// §3) — there is no per-request signal this transport could bind that would
// mean anything finer. Without SOME incident context bound here,
// CreateIssue's own anti-spoofing guard (ErrIncidentContextRequired) rejects
// every SRE-filed componentName/actionStatuses outright, trusted bearer or not.
const sreHandoffIncidentID = "sre-handoff"

// mapRunnerAuthError translates the authorizer's neutral auth.HTTPError onto
// the envelope; anything unrecognized fails closed as a 401.
func mapRunnerAuthError(err error) error {
	var ae *auth.HTTPError
	if errors.As(err, &ae) {
		switch ae.Status {
		case http.StatusForbidden:
			return errForbidden(ae.Message)
		default:
			return errUnauthorized(ae.Message)
		}
	}
	return errUnauthorized("invalid bearer")
}

func (s *internalServer) RunnerRefreshCredentials(ctx context.Context, request igen.RunnerRefreshCredentialsRequestObject) (igen.RunnerRefreshCredentialsResponseObject, error) {
	if s.deps.CredsRefresh == nil {
		return nil, errServiceUnavailable("credentials refresh not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	resp, err := s.deps.CredsRefresh.Refresh(ctx, request.ExecutionID, org)
	if err != nil {
		return nil, errInternal("failed to refresh credentials")
	}
	return igen.RunnerRefreshCredentials200JSONResponse(toIgenRefresh(*resp)), nil
}

// toIgenRefresh projects the org domain's RefreshResponse onto the S2S wire
// shape. igen must stay a leaf, so it cannot import the domain that owns the
// value type — hence a mapping here rather
// than the former x-go-type alias. The wire keys are byte-identical (the
// Identity sub-object marshals capitalized either way); only Go field ORDER
// differs between the two Identity structs, which forbids a whole-struct
// conversion, so the three fields are copied by name.
func toIgenRefresh(r organization.RefreshResponse) igen.RefreshResponse {
	return igen.RefreshResponse{
		Token:     r.Token,
		ExpiresAt: r.ExpiresAt,
		Identity: igen.Identity{
			Name:  r.Identity.Name,
			Email: r.Identity.Email,
			Login: r.Identity.Login,
		},
		TaskID: r.TaskID,
	}
}

func (s *internalServer) RunnerValidationContext(ctx context.Context, request igen.RunnerValidationContextRequestObject) (igen.RunnerValidationContextResponseObject, error) {
	if s.deps.ValidationContext == nil {
		return nil, errServiceUnavailable("validation context not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	resp, err := s.deps.ValidationContext.ValidationContext(ctx, request.CycleID, org)
	if err != nil {
		if errors.Is(err, validation.ErrCycleNotFound) {
			return nil, errNotFound("no validation cycle with this id")
		}
		return nil, errInternal("failed to resolve validation context")
	}
	return igen.RunnerValidationContext200JSONResponse(toIgenValidationContext(*resp)), nil
}

// toIgenValidationContext projects the validation service's own struct onto the
// S2S wire shape. igen must stay a leaf, so it cannot import the feature/domain
// that owns the value type (§7) — hence a mapping here rather than the former
// x-go-type alias. The wire keys are byte-identical; a nil endpoints slice stays
// nil (marshals `null`) exactly as the alias did, never silently becoming `[]`.
func toIgenValidationContext(r validation.ValidationContextResponse) igen.ValidationContextResponse {
	var eps []igen.ComponentEndpoint
	if r.Endpoints != nil {
		eps = make([]igen.ComponentEndpoint, len(r.Endpoints))
		for i, e := range r.Endpoints {
			eps[i] = igen.ComponentEndpoint{Component: e.Component, URL: e.URL}
		}
	}
	return igen.ValidationContextResponse{Endpoints: eps}
}

// mcpRoutes returns the internal MCP discovery handler (POST /internal/v1/mcp,
// raw JSON-RPC) and the local playground-token mint. The MCP server answers the
// design agent's queries for the org's external resources, endpoints and
// platform resource types, gated by auth.AgentsScopedVerifier (BFF-signed token
// aud aep-api-mcp, or a Thunder publisher token); the acting org comes from a
// verified claim, never the request. Without a token manager nothing could
// verify a caller, so both return nil and the paths 404 instead of 503-ing.
// routes() hands both to newInternalV1Handler via InternalDeps.
// The playground mint is local-dev only, mounted solely under
// PlaygroundTokenEnabled.
func mcpRoutes(p AppParams) (mcp, playground http.Handler) {
	if p.Deps.TaskTokens == nil {
		return nil, nil
	}
	verifier := auth.NewAgentsScopedVerifier(p.Deps.TaskTokens, p.Deps.PublisherTokens)
	mcp = verifier.Middleware(mcpdiscovery.NewMCPHandler(
		p.MCPExternalResources, p.MCPOrgEndpoints, p.MCPResourceTypes,
		p.MCPGroupCatalog, p.MCPRemoteGit,
		p.MCPSpecValidator, p.MCPSpecNormalizer, p.MCPSpecFetcher, p.MCPSpecSlicer))
	if p.Config.PlaygroundTokenEnabled {
		playground = mcpdiscovery.NewPlaygroundTokenHandler(p.Deps.TaskTokens)
	}
	return mcp, playground
}
