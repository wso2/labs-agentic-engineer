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

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// The /internal/v1 route group is served contract-first from
// packages/contracts/api/ae-studio-tools/internal/v1: the generated strict
// server in internal/gen, behind a request validator over the same contract
// (embedded in the binary). routes.go puts the per-op body cap and the M2M
// gate in front of it, so nothing here runs for an unauthenticated caller.

const (
	internalV1 = "/internal/v1"
	// internalBodyBytes caps every /internal/v1 request body (ticket 04 §10)
	// unless internalBodyCaps names the operation.
	internalBodyBytes int64 = 1 << 20
	// referencesBodyBytes caps a references upload: 10 files × 5 MiB plus
	// the multipart framing, with room to spare (09 §1). The handler checks
	// the parts themselves.
	referencesBodyBytes int64 = 80 << 20
	// turnBodyBytes caps a turn start at what the agent's Turn socket takes
	// (ae-design-agent src/edge/turn-socket.ts MAX_BODY_BYTES): a re-plan's
	// taskContext carries the open Tasks' bodies, and a cap below the agent's
	// would refuse here a turn the agent accepts (R1-M3).
	turnBodyBytes int64 = 4 << 20
	// commitBodyBytes caps a create-commit: its writes travel base64 in one
	// JSON body (05 §3, report gap G5).
	commitBodyBytes int64 = 16 << 20
)

// internalBodyCaps lists the operations allowed more than internalBodyBytes,
// by operationId.
var internalBodyCaps = map[string]int64{
	"put-repo-references": referencesBodyBytes,
	"start-repo-turn":     turnBodyBytes,
	"create-commit":       commitBodyBytes,
}

// internalReadFile is read-file's address, whose {path} is a trailing
// wildcard.
var internalReadFile = trailingPath{scope: internalV1 + "/repos/", vars: 2, literal: "files"}

// internalRouteFinder matches a request to an /internal/v1 contract
// operation (a nested read-file path included): the cap table's lookup and
// the validator's.
var internalRouteFinder = internalReadFile.routes(mustRouter("internal", gen.GetSpec))

// gitHubOps names github.Handler for embedding beside repo.Handler (two
// embedded fields cannot share the name Handler).
type gitHubOps = github.Handler

// internalServer implements gen.StrictServerInterface. The git content ops
// are repo.Handler's; the issue, milestone and pull request ops
// github.Handler's.
type internalServer struct {
	repo.Handler
	gitHubOps
	gh github.Identity
	// refs is the reference store; githubOwner the org's connected GitHub
	// account (AE_GITHUB_OWNER), the only owner whose repos it stores for.
	refs        ReferenceStore
	githubOwner string
	// projects resolves a turn's project to its repository; turns relays
	// the turn to the agent (internal_turns.go).
	projects projects.Resolver
	turns    TurnRelay
}

var _ gen.StrictServerInterface = internalServer{}

// internalHandler is the gated part of the group: validator → owner guard →
// mux holding the generated routes and the read-file {path...} catch-all,
// with start-repo-turn on its raw route ahead of them (internal_turns.go). A
// path or method the contract does not declare is 404 at the validator; the
// mux's catch-all keeps any miss behind it a problem body.
func internalHandler(find routeFinder, s internalServer) http.Handler {
	strict := gen.NewStrictHandlerWithOptions(s, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  writeRequestError,
		ResponseErrorHandlerFunc: writeResponseError,
	})
	mux := http.NewServeMux()
	gen.HandlerWithOptions(strict, gen.StdHTTPServerOptions{
		BaseURL:          internalV1,
		BaseRouter:       mux,
		ErrorHandlerFunc: writeRequestError,
	})
	siw := &gen.ServerInterfaceWrapper{Handler: strict, ErrorHandlerFunc: writeRequestError}
	mux.HandleFunc(internalReadFile.pattern("owner", "repo"), siw.ReadFile)
	mux.Handle(internalV1+"/", http.HandlerFunc(notFound))
	routes := http.NewServeMux()
	routes.HandleFunc(startRepoTurnPattern, s.startRepoTurn)
	routes.Handle("/", mux)
	return requestValidator(find, "validation_failed", ownerGuard(s.githubOwner, routes))
}

// capOpBody bounds a request body before anything reads it, at the matched
// operation's entry in caps, else at def (a route miss gets def too). It
// runs ahead of the gate, as capBody does, so an unauthenticated caller
// cannot make the pod read more than the operation allows.
func capOpBody(find routeFinder, caps map[string]int64, def int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := def
		if route, _, err := find(r); err == nil && route.Operation != nil {
			if c, ok := caps[route.Operation.OperationID]; ok {
				limit = c
			}
		}
		capBody(limit, next).ServeHTTP(w, r)
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
// Retry-After; any other GitHub failure is 502 github_error with GitHub's
// status in githubStatus when it answered one.
func (s internalServer) GetGithubIdentity(ctx context.Context, _ gen.GetGithubIdentityRequestObject) (gen.GetGithubIdentityResponseObject, error) {
	login, id, err := s.gh.Whoami(ctx)
	if err == nil {
		return gen.GetGithubIdentity200JSONResponse{Login: login, ID: id}, nil
	}
	if wait, limited := github.RateLimited(err); limited {
		return gen.GetGithubIdentity429ApplicationProblemPlusJSONResponse{
			RateLimitedApplicationProblemPlusJSONResponse: gen.RateLimitedApplicationProblemPlusJSONResponse{
				Body:    newProblem(http.StatusTooManyRequests, "github_rate_limited", "GitHub rate-limited the gitpat"),
				Headers: gen.RateLimitedResponseHeaders{RetryAfter: int(math.Ceil(wait.Seconds()))},
			},
		}, nil
	}
	p := newProblem(http.StatusBadGateway, "github_error", "GitHub could not be reached")
	var se *github.HTTPStatusError
	if errors.As(err, &se) {
		p.Detail = fmt.Sprintf("GitHub answered %d", se.StatusCode)
		p.GithubStatus = se.StatusCode
	}
	slog.Warn("github.identity_failed", "error", err)
	return gen.GetGithubIdentity502ApplicationProblemPlusJSONResponse(p), nil
}

// newProblem is the body problem.Write sends, as the generated type.
func newProblem(status int, code, detail string) gen.Problem {
	return gen.Problem{Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail, Code: code}
}
