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
	"net/url"

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
// user-JWT middleware. Authenticate, then parse: every request is body-capped
// (capInternalBody, which finds its route once); every generated operation then
// passes internalGate, which verifies the caller's credential for the op's
// route group (a runner's publisher-cc bearer against the cycle named in the
// path, the INT-6 fence; the SRE handoff bearer for sre/; an org's publisher
// client token for ae-studio/) and binds the verified org into the context;
// only an authenticated request is validated against the embedded internal
// spec (internalValidator). The raw MCP routes carry their
// own verifier. The spec is non-public, never gateway-advertised, but the path
// is reachable through the console's /aep-api-service/ route, so nothing on it
// parses a body for an anonymous caller.
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
	// PublisherTokens verifies an org's publisher client token for the
	// ae-studio/ ops (the AE Studio tools pod). nil fails closed: every
	// ae-studio/ op answers 401.
	PublisherTokens *auth.PublisherTokenVerifier
	// AEStudioRepositories backs get-ae-studio-project-repository; nil
	// answers 503.
	AEStudioRepositories ProjectRepositoryLookup
	// DependencyCompleter backs complete-ae-studio-dependencies
	// (spec.CompleteDependencies over the org registry and the guarded URL
	// fetch); nil answers 503.
	DependencyCompleter DependencyCompleter
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
//	body cap (capInternalBody)    finds the route once; 1 MiB default, per-op overrides
//	→ internalGate                authenticates the matched op's caller, binds the org
//	→ internalValidator           kin-openapi against the embedded internal spec
//	→ inner mux                   raw MCP routes + generated router
//	→ requireInternalGate         backstop: a generated op the gate did not clear is 401
//	→ strict wrapper              envelope error writers
//
// The inner mux registers full paths, so a path no row names 404s.
func newInternalV1Handler(deps InternalDeps) http.Handler {
	strict := igen.NewStrictHandlerWithOptions(
		&internalServer{deps: deps},
		[]igen.StrictMiddlewareFunc{requireInternalGate},
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
	return capInternalBody(internalRouter(), internalBodyCaps, internalGate(deps, internalValidator(mux)))
}

// internalDefaultBodyBytes caps every /internal/v1 request body (03 §4);
// internalBodyCaps lists the operations allowed more. Phase 4 adds
// ingest-webhook-event at 25 MiB (GitHub's payload maximum).
const internalDefaultBodyBytes int64 = 1 << 20

var internalBodyCaps = map[string]int64{}

// internalRouteMatch is the embedded-spec operation a request matched, found
// once by capInternalBody and read from the context by internalGate and
// internalValidator. A route miss (unknown path, wrong method, or a raw MCP
// route the embedded spec lacks) stores nothing. pathParams are kin's values,
// still URL-escaped (kin matches the escaped path); the handlers are served
// the ServeMux's decoded PathValue, so the gate unescapes before fencing.
type internalRouteMatch struct {
	route      *routers.Route
	pathParams map[string]string
}

type internalRouteKey struct{}

func internalRouteFrom(ctx context.Context) (internalRouteMatch, bool) {
	m, ok := ctx.Value(internalRouteKey{}).(internalRouteMatch)
	return m, ok
}

// capInternalBody bounds every internal request body before anything reads it.
// It is the one route lookup per request: a matched operation is stored in the
// context for the gate and the validator. The limit is the matched operation's
// entry in caps, else internalDefaultBodyBytes. A declared Content-Length over the limit is
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
		if route, pathParams, err := findInternalRoute(router, r); err == nil && route.Operation != nil {
			if c, ok := caps[route.Operation.OperationID]; ok {
				limit = c
			}
			r = r.WithContext(context.WithValue(r.Context(), internalRouteKey{},
				internalRouteMatch{route: route, pathParams: pathParams}))
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

// findInternalRoute matches r against the embedded spec. A HEAD the spec does
// not declare matches the path's GET operation: the inner ServeMux serves HEAD
// through a GET pattern, so the gate must authenticate (and the validator
// validate) it as that GET, exactly as /api/v1 does, rather than let it reach
// the strict backstop unauthenticated.
func findInternalRoute(router routers.Router, r *http.Request) (*routers.Route, map[string]string, error) {
	route, pathParams, err := router.FindRoute(r)
	if err == nil || r.Method != http.MethodHead {
		return route, pathParams, err
	}
	get := r.WithContext(r.Context())
	get.Method = http.MethodGet
	return router.FindRoute(get)
}

// internalCredential is the credential a route group's operations require.
type internalCredential int

const (
	// runnerCredential: a coding runner's publisher-cc bearer, fenced to the
	// cycle the path names (INT-6).
	runnerCredential internalCredential = iota + 1
	// sreHandoffCredential: aep-mcp-server's static SRE handoff bearer.
	sreHandoffCredential
	// aeStudioCredential: the org's publisher client token, presented by its
	// AE Studio tools pod; no cycle fence, the org is the token's ouHandle.
	aeStudioCredential
)

// internalOpGate is one operation's gate entry.
type internalOpGate struct {
	credential internalCredential
	// cycleParam names the path parameter carrying the cycle id a runner op
	// is fenced to.
	cycleParam string
}

// internalOpGates is the gate table, keyed by embedded-spec operation id.
// TestInternalGate_CoversEverySpecOperation pins it to the spec both ways.
var internalOpGates = map[string]internalOpGate{
	"runner-refresh-credentials":       {credential: runnerCredential, cycleParam: "executionId"},
	"runner-validation-context":        {credential: runnerCredential, cycleParam: "cycleId"},
	"sre-list-issues":                  {credential: sreHandoffCredential},
	"sre-create-issue":                 {credential: sreHandoffCredential},
	"sre-create-rca-report":            {credential: sreHandoffCredential},
	"get-ae-studio-project-repository": {credential: aeStudioCredential},
	"complete-ae-studio-dependencies":  {credential: aeStudioCredential},
}

// internalGate is /internal/v1's deny-by-default gate (internalOpGates), one
// credential per route group (path prefix under /internal/v1). It runs after the body cap and
// before the validator, so an unauthenticated caller gets 401 and never a
// schema-detail 400 or a body parse:
//
//	executions/, validation/   coding runner   publisher token, cycle fence (cycle id in the path)
//	sre/                       SRE handoff     SRE handoff bearer, binds its one org + the incident context
//	ae-studio/                 AE Studio pod   publisher client token, binds its ouHandle org (no cycle)
//	mcp, mcp/playground-token  runner, agent   route miss here: passed through to their own verifier
//	any other embedded op      -               denied (401)
//
// A route miss passes through untouched: the inner mux answers 404 or 405, or
// serves a raw MCP route that verifies its own caller. Each generated operation
// must present the credential of its route group, and the verified org is bound
// into the context. A credential opens its own group only: a publisher token
// never clears sre/, the SRE bearer never clears a runner op or ae-studio/,
// and no other token (a user JWT, the AE-only client, an ae-studio-<org>
// client) clears ae-studio/. There are deliberately NO carve-outs: an operation absent from internalOpGates is
// denied outright, so adding an internal op means teaching this gate its
// credential first. The cycle fence checks the decoded path value, the one
// the handler is served. requireInternalGate denies any generated op that reaches
// the strict wrapper without this gate's verdict.
//
// The refresh operation still spells its parameter `executionId` on the wire; the
// value is the dispatched cycle id, the same naming debt AEP_TASK_ID carries.
func internalGate(deps InternalDeps, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := internalRouteFrom(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		ctx, err := authenticateInternal(r.Context(), deps, r.Header.Get("Authorization"), m)
		if err != nil {
			writeResponseError(w, r, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, internalGateKey{}, true)))
	})
}

// authenticateInternal verifies authHeader for the matched operation's route
// group and returns ctx with the verified org (and, for sre/, the claims and
// incident context) bound.
func authenticateInternal(ctx context.Context, deps InternalDeps, authHeader string, m internalRouteMatch) (context.Context, error) {
	op := m.route.Operation.OperationID
	gate, ok := internalOpGates[op]
	switch {
	case ok && gate.credential == sreHandoffCredential:
		claims, ok := deps.SREHandoff.Verify(authHeader) // nil verifier: false, fails closed
		if !ok {
			return nil, errUnauthorized("SRE handoff bearer required")
		}
		ctx = auth.WithClaims(ctx, claims)
		ctx = sourcecontrol.WithIncidentContext(ctx, sreHandoffIncidentID)
		return tenant.WithBoundOrg(ctx, claims.OuHandle), nil
	case ok && gate.credential == aeStudioCredential:
		return authenticateAEStudio(ctx, deps.PublisherTokens, authHeader)
	case ok && gate.credential == runnerCredential:
		if deps.RunnerAuth == nil {
			return nil, errServiceUnavailable("runner auth not configured")
		}
		cycleID, err := url.PathUnescape(m.pathParams[gate.cycleParam])
		if err != nil {
			return nil, errUnauthorized("malformed cycle id in path")
		}
		caller, err := deps.RunnerAuth.Authorize(ctx, authHeader, cycleID)
		if err != nil {
			return nil, mapRunnerAuthError(err)
		}
		return tenant.WithBoundOrg(ctx, string(caller.Org)), nil
	default:
		return nil, errUnauthorized("unauthenticated internal operation: " + op)
	}
}

// internalGateKey marks a request internalGate authenticated.
type internalGateKey struct{}

// requireInternalGate is the strict-wrapper backstop: a generated operation
// runs only if internalGate cleared its request. The gate keys on kin's route
// match; a path the generated router serves but kin's router misses would skip
// the gate, and is denied here instead of served unauthenticated.
func requireInternalGate(f igen.StrictHandlerFunc, operationID string) igen.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		if cleared, _ := ctx.Value(internalGateKey{}).(bool); !cleared {
			return nil, errUnauthorized("unauthenticated internal operation: " + operationID)
		}
		return f(ctx, w, r, request)
	}
}

// internalValidator validates an authenticated request against the embedded
// internal spec, using the route capInternalBody found. A route miss falls
// through: the raw MCP routes are absent from the embedded spec (call-mcp-tool
// is excluded from generation), so their bodies are capped, not validated.
func internalValidator(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, ok := internalRouteFrom(r.Context())
		if !ok || validateRequest(w, r, m.route, m.pathParams) {
			next.ServeHTTP(w, r)
		}
	})
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
