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
	"fmt"
	"log/slog"
	"math"
	"net/http"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// The /internal/v1 route group is served contract-first from
// packages/contracts/api/ae-studio-tools/internal/v1: the generated strict
// server in internal/gen, behind a request validator over the same contract
// (embedded in the binary). routes.go puts the body cap and the M2M gate in
// front of it, so nothing here runs for an unauthenticated caller.

const (
	internalV1 = "/internal/v1"
	// internalBodyBytes caps every /internal/v1 request body (ticket 04 §10).
	internalBodyBytes int64 = 1 << 20
)

// internalServer implements gen.StrictServerInterface.
type internalServer struct {
	gh github.Identity
}

var _ gen.StrictServerInterface = internalServer{}

// internalHandler is the gated part of the group: validator → mux holding the
// generated routes. A path or method the contract does not declare is 404.
func internalHandler(gh github.Identity) http.Handler {
	strict := gen.NewStrictHandlerWithOptions(internalServer{gh: gh}, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  writeRequestError,
		ResponseErrorHandlerFunc: writeResponseError,
	})
	mux := http.NewServeMux()
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:          internalV1,
		BaseRouter:       mux,
		ErrorHandlerFunc: writeRequestError,
	})
	mux.Handle(internalV1+"/", http.HandlerFunc(notFound))
	return requestValidator(mustInternalRouter(), mux)
}

// mustInternalRouter builds the kin router over the embedded contract. A
// decode failure is a build defect, not a runtime condition, so it panics.
func mustInternalRouter() routers.Router {
	doc, err := gen.GetSpec()
	if err != nil {
		panic(fmt.Sprintf("embedded internal contract failed to load: %v", err))
	}
	router, err := legacyrouter.NewRouter(doc)
	if err != nil {
		panic(fmt.Sprintf("internal contract router: %v", err))
	}
	return router
}

// requestValidator validates every request that matches a contract operation
// before it reaches the generated handler; a route miss falls through to next,
// whose catch-all answers 404. Security is not checked here: the gate in front
// already did.
func requestValidator(router routers.Router, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, pathParams, err := router.FindRoute(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		input := &openapi3filter.RequestValidationInput{
			Request:    r,
			PathParams: pathParams,
			Route:      route,
			Options:    &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		}
		if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeBodyTooLarge(w)
				return
			}
			problem.Write(w, http.StatusBadRequest, "validation_failed", "the request does not match the contract")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// capBody bounds a request body before anything reads it: a declared
// Content-Length over limit is 413 here, and a body of unknown length is
// wrapped in http.MaxBytesReader so a read past limit fails.
func capBody(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func writeBodyTooLarge(w http.ResponseWriter) {
	problem.Write(w, http.StatusRequestEntityTooLarge, "payload_too_large", "the request body exceeds the size limit")
}

// writeRequestError answers a request the generated binder could not parse.
func writeRequestError(w http.ResponseWriter, _ *http.Request, _ error) {
	problem.Write(w, http.StatusBadRequest, "validation_failed", "the request does not match the contract")
}

// writeResponseError answers a handler error no typed response covers.
func writeResponseError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("internal.handler_failed", "path", r.URL.Path, "error", err)
	problem.Write(w, http.StatusInternalServerError, "internal_error", "the request could not be completed")
}

// GetGithubIdentity answers the gitpat's GitHub user. A rate limit is 429 with
// Retry-After; any other GitHub failure is 502 naming GitHub's status.
func (s internalServer) GetGithubIdentity(ctx context.Context, _ gen.GetGithubIdentityRequestObject) (gen.GetGithubIdentityResponseObject, error) {
	login, id, err := s.gh.Whoami(ctx)
	if err == nil {
		return gen.GetGithubIdentity200JSONResponse{Login: login, ID: id}, nil
	}
	var rl *github.ErrRateLimited
	if errors.As(err, &rl) {
		return gen.GetGithubIdentity429ApplicationProblemPlusJSONResponse{
			RateLimitedApplicationProblemPlusJSONResponse: gen.RateLimitedApplicationProblemPlusJSONResponse{
				Body:    newProblem(http.StatusTooManyRequests, "github_rate_limited", "GitHub rate-limited the gitpat"),
				Headers: gen.RateLimitedResponseHeaders{RetryAfter: int(math.Ceil(rl.RetryAfter.Seconds()))},
			},
		}, nil
	}
	detail := "GitHub could not be reached"
	var se *github.StatusError
	if errors.As(err, &se) {
		detail = fmt.Sprintf("GitHub answered %d", se.Status)
	}
	slog.Warn("github.identity_failed", "error", err)
	return gen.GetGithubIdentity502ApplicationProblemPlusJSONResponse(
		newProblem(http.StatusBadGateway, "github_error", detail)), nil
}

// newProblem is the body problem.Write sends, as the generated type.
func newProblem(status int, code, detail string) gen.Problem {
	return gen.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail, Code: code}
}
