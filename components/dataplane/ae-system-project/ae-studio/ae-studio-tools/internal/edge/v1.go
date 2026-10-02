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
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/routers"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	v1gen "github.com/wso2/aep/ae-studio-tools/internal/gen/v1"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// The /v1 route group is served contract-first from
// packages/contracts/api/ae-studio-tools/v1: the generated strict server in
// internal/gen/v1, behind a request validator over the same contract. routes.go
// puts the user gate (a Platform IdP user JWT of the pod's org) in front of it,
// so nothing here runs for an unauthenticated or wrong-org caller.

const (
	v1Prefix = "/v1"
	// filesSegment is the literal segment after the project in every Files
	// address.
	filesSegment = "files"
)

// v1Handler is the gated part of the group: validator → mux holding the
// generated routes and the read-file catch-all. A path or method the contract
// does not declare is 404 at the validator, and there is no write operation, so
// a write is 404 too.
func v1Handler(reader files.Reader) http.Handler {
	strict := v1gen.NewStrictHandlerWithOptions(v1Server{files: reader}, nil, v1gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  writeV1RequestError,
		ResponseErrorHandlerFunc: writeV1ResponseError,
	})
	mux := http.NewServeMux()
	v1gen.HandlerWithOptions(strict, v1gen.StdHTTPServerOptions{
		BaseURL:          v1Prefix,
		BaseRouter:       mux,
		ErrorHandlerFunc: writeV1RequestError,
	})
	// read-file's {path} is a trailing wildcard (the contract says so): the
	// generated single-segment pattern cannot match a nested spec path, so the
	// same wrapped handler is also registered under the ServeMux catch-all.
	// One-segment paths keep hitting the generated pattern (more specific);
	// PathValue("path") serves both.
	siw := &v1gen.ServerInterfaceWrapper{Handler: strict, ErrorHandlerFunc: writeV1RequestError}
	mux.HandleFunc("GET "+v1Prefix+"/projects/{projectName}/files/{path...}", siw.ReadFile)
	mux.Handle(v1Prefix+"/", http.HandlerFunc(notFound))
	return requestValidator(v1Routes(mustRouter("v1", v1gen.GetSpec)), "path_invalid", mux)
}

// v1Routes is the /v1 route finder: the contract's own routes, plus a nested
// read-file address (see nestedReadFile), which the contract's one-segment
// {path} cannot match. A nested address is validated as the read-file
// operation with path set to the whole remainder.
func v1Routes(router routers.Router) routeFinder {
	return func(r *http.Request) (*routers.Route, map[string]string, error) {
		route, params, err := router.FindRoute(r)
		if err == nil {
			return route, params, nil
		}
		probe, filePath, ok := nestedReadFile(r)
		if !ok {
			return nil, nil, err
		}
		route, params, perr := router.FindRoute(probe)
		if perr != nil {
			return nil, nil, err
		}
		params["path"] = filePath
		return route, params, nil
	}
}

// nestedReadFile recognises GET /v1/projects/{p}/files/<a>/<b>[/...]: a
// read-file address whose path has more than one segment. It returns a probe
// request for the same project with a one-segment path (which the contract's
// router matches as read-file) and the decoded remainder. Anything else is
// not ok.
func nestedReadFile(r *http.Request) (probe *http.Request, filePath string, ok bool) {
	if r.Method != http.MethodGet {
		return nil, "", false
	}
	rest, found := strings.CutPrefix(r.URL.EscapedPath(), v1Prefix+"/projects/")
	if !found {
		return nil, "", false
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 || parts[0] == "" || parts[1] != filesSegment || !strings.Contains(parts[2], "/") {
		return nil, "", false
	}
	filePath, err := url.PathUnescape(parts[2])
	if err != nil {
		return nil, "", false
	}
	probeURL, err := url.Parse(v1Prefix + "/projects/" + parts[0] + "/" + filesSegment + "/_")
	if err != nil {
		return nil, "", false
	}
	probeURL.RawQuery = r.URL.RawQuery
	probe = r.Clone(r.Context())
	probe.URL = probeURL
	return probe, filePath, true
}

// writeV1RequestError answers a request the generated binder could not parse.
// Every /v1 input is a project, a path or a ref.
func writeV1RequestError(w http.ResponseWriter, _ *http.Request, _ error) {
	problem.Write(w, http.StatusBadRequest, "path_invalid", "the request does not match the contract")
}

// writeV1ResponseError answers a handler error no typed response covers.
func writeV1ResponseError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("v1.handler_failed", "path", r.URL.Path, "error", err)
	problem.Write(w, http.StatusInternalServerError, "internal_error", "the request could not be completed")
}
