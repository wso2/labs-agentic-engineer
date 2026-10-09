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
	"fmt"
	"log/slog"
	"net/http"

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
	mux.HandleFunc(v1ReadFile.pattern("projectName"), siw.ReadFile)
	mux.Handle(v1Prefix+"/", http.HandlerFunc(notFound))
	return requestValidator(v1ReadFile.routes(mustRouter("v1", v1gen.GetSpec)), "path_invalid", mux)
}

// v1ReadFile is read-file's address, whose {path} is a trailing wildcard.
var v1ReadFile = trailingPath{scope: v1Prefix + "/projects/", vars: 1, literal: filesSegment}

// writeV1RequestError answers a request the generated binder could not parse.
// Every /v1 input is a project, a path or a ref.
func writeV1RequestError(w http.ResponseWriter, _ *http.Request, _ error) {
	problem.Write(w, http.StatusBadRequest, "path_invalid", "the request does not match the contract")
}

// writeV1ResponseError answers a handler error no typed response covers. The
// handlers return none, so this is a response that failed to encode; its
// error is logged by class only, as on the Files socket, since a Files error
// can carry git text.
func writeV1ResponseError(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("v1.handler_failed", "path", r.URL.Path, "class", fmt.Sprintf("%T", err))
	problem.Write(w, http.StatusInternalServerError, "internal_error", "the request could not be completed")
}
