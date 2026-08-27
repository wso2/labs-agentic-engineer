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

	"github.com/wso2/aep/aep-api/internal/delivery/validation"
	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// The internal service-to-service surface (/internal/v1), served CONTRACT-FIRST
// from packages/contracts/api/internal/v1 (generated strict server in
// internal/igen). It is NOT wrapped by the user-JWT middleware: every
// operation passes runnerAuthGate, which verifies the caller's publisher-cc
// bearer against the execution named in the path (the INT-6
// fence) and binds the verified org into the context. The spec is non-public —
// never gateway-advertised.
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
	// path execution id. nil fails closed: every internal op answers 503.
	RunnerAuth *auth.RunnerAuthorizer
	// ValidationContext backs the validation-context runner callback; a nil
	// provider answers 503 for that op. A test user's login is NOT served here —
	// it is published on the roles gate ticket, which is where the validation
	// agent reads it (ADR-0022).
	ValidationContext validation.ContextProvider
}

// internalServer implements igen.StrictServerInterface.
type internalServer struct {
	deps InternalDeps
}

var _ igen.StrictServerInterface = (*internalServer)(nil)

// newInternalV1Handler assembles the internal edge: runner-auth gate → strict
// wrapper (envelope error writers) → generated router.
func newInternalV1Handler(deps InternalDeps) http.Handler {
	strict := igen.NewStrictHandlerWithOptions(
		&internalServer{deps: deps},
		[]igen.StrictMiddlewareFunc{runnerAuthGate(deps.RunnerAuth)},
		igen.StrictHTTPServerOptions{
			RequestErrorHandlerFunc:  writeRequestError,
			ResponseErrorHandlerFunc: writeResponseError,
		},
	)
	mux := http.NewServeMux()
	igen.HandlerWithOptions(strict, igen.StdHTTPServerOptions{
		BaseURL:          internalV1,
		BaseRouter:       mux,
		ErrorHandlerFunc: writeRequestError,
	})
	return mux
}

// runnerAuthGate is the internal surface's deny-by-default gate: every
// operation must present a bearer the authorizer accepts for the CYCLE id named
// in the request, and the verified org is bound into the context. There are
// deliberately NO carve-outs here. An operation whose request shape the gate does
// not know is denied outright — adding an internal op means teaching this gate
// where its cycle id lives first.
//
// The refresh operation still spells its parameter `executionId` on the wire; the
// value is the dispatched cycle id, the same naming debt AEP_TASK_ID carries.
func runnerAuthGate(authorizer *auth.RunnerAuthorizer) igen.StrictMiddlewareFunc {
	return func(f igen.StrictHandlerFunc, operationID string) igen.StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			if authorizer == nil {
				return nil, errServiceUnavailable("runner auth not configured")
			}
			var cycleID string
			switch req := request.(type) {
			case igen.RunnerRefreshCredentialsRequestObject:
				cycleID = req.ExecutionID
			case igen.RunnerValidationContextRequestObject:
				cycleID = req.CycleID
			default:
				return nil, errUnauthorized("unauthenticated internal operation: " + operationID)
			}
			caller, err := authorizer.Authorize(ctx, r.Header.Get("Authorization"), cycleID)
			if err != nil {
				return nil, mapRunnerAuthError(err)
			}
			return f(tenant.WithBoundOrg(ctx, string(caller.Org)), w, r, request)
		}
	}
}

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
	return igen.ValidationContextResponse{Endpoints: eps, CriteriaPath: r.CriteriaPath}
}
