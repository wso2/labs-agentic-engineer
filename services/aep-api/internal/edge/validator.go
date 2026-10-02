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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/igen"
)

// mustRouter decodes a contract that oapi-codegen baked into the binary
// (embedded-spec), the same committed contract the handlers generate from, and
// builds its kin router. It panics on failure: a decode error is a build
// defect, not a runtime condition.
func mustRouter(load func() (*openapi3.T, error), name string) routers.Router {
	doc, err := load()
	if err != nil {
		panic(fmt.Sprintf("embedded %s contract failed to load: %v", name, err))
	}
	router, err := legacyrouter.NewRouter(doc)
	if err != nil {
		panic(fmt.Sprintf("%s contract router: %v", name, err))
	}
	return router
}

// publicRouter and internalRouter memoize the parsed contract + kin router per
// spec: both are read-only after construction (kin's schema-pattern cache is
// its own package-level sync.Map), and every handler construction (production
// once, but one per componenttest harness) would otherwise re-parse the spec
// and rebuild the route tree.
var (
	publicRouter   = sync.OnceValue(func() routers.Router { return mustRouter(gen.GetSpec, "public") })
	internalRouter = sync.OnceValue(func() routers.Router { return mustRouter(igen.GetSpec, "internal") })
)

// requestValidator validates every request router matches against the public
// contract BEFORE it reaches the generated handler chain: schema-invalid input
// never reaches a handler and is answered with the flat envelope (400
// validation_failed + field details). A route miss falls through to next
// untouched: the generated mux owns 404s. The internal edge validates
// through validateRequest with the route capInternalBody already found
// (internalValidator).
func requestValidator(router routers.Router, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, pathParams, err := router.FindRoute(r)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		if validateRequest(w, r, route, pathParams) {
			next.ServeHTTP(w, r)
		}
	})
}

// validateRequest checks r against its matched contract operation and reports
// whether it passed; on failure it has already written the 400 (or 413) envelope.
// Security requirements are NOT checked here (AuthenticationFunc is a no-op):
// authN/authZ belong to the route group (public: the JWKS middleware and tenant
// gate, which run before the validator; internal: internalGate, also before it).
func validateRequest(w http.ResponseWriter, r *http.Request, route *routers.Route, pathParams map[string]string) bool {
	input := &openapi3filter.RequestValidationInput{
		Request:    r,
		PathParams: pathParams,
		Route:      route,
		Options: &openapi3filter.Options{
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			// Multipart bodies (skill import): kin would io.ReadAll the
			// whole upload and schema-decode every part — including the
			// binary tarball — only to check the file field is present,
			// which the handler's own 400 already enforces. The strict
			// wrapper re-parses the multipart anyway; skip the redundant
			// 2-3x in-memory copies.
			ExcludeRequestBody: hasMultipartBody(route),
		},
	}
	if err := openapi3filter.ValidateRequest(r.Context(), input); err != nil {
		writeValidationError(w, err)
		return false
	}
	return true
}

// writeBodyTooLarge answers 413 with the envelope every body cap shares.
func writeBodyTooLarge(w http.ResponseWriter) {
	writeErrorEnvelope(w, http.StatusRequestEntityTooLarge, "request_too_large",
		"request body exceeds the size limit", nil)
}

// hasMultipartBody reports whether the matched operation declares a
// multipart/form-data request body.
func hasMultipartBody(route *routers.Route) bool {
	if route == nil || route.Operation == nil || route.Operation.RequestBody == nil || route.Operation.RequestBody.Value == nil {
		return false
	}
	_, ok := route.Operation.RequestBody.Value.Content["multipart/form-data"]
	return ok
}

// writeValidationError maps a kin-openapi request-validation failure onto the
// envelope: 400 validation_failed with one details entry per schema violation.
func writeValidationError(w http.ResponseWriter, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		writeBodyTooLarge(w)
		return
	}
	var reqErr *openapi3filter.RequestError
	if !errors.As(err, &reqErr) {
		writeErrorEnvelope(w, http.StatusBadRequest, CodeValidationFailed, err.Error(), nil)
		return
	}

	loc := "body"
	if reqErr.Parameter != nil {
		loc = reqErr.Parameter.In + "." + reqErr.Parameter.Name
	}

	var schemaErr *openapi3.SchemaError
	if errors.As(reqErr.Err, &schemaErr) {
		field := loc
		if ptr := strings.Join(schemaErr.JSONPointer(), "."); ptr != "" {
			field = loc + "." + ptr
		}
		writeErrorEnvelope(w, http.StatusBadRequest, CodeValidationFailed, "request validation failed",
			[]gen.ErrorDetail{{Field: field, Message: schemaErr.Reason}})
		return
	}

	msg := reqErr.Reason
	if msg == "" {
		msg = reqErr.Error()
	}
	writeErrorEnvelope(w, http.StatusBadRequest, CodeValidationFailed, "request validation failed",
		[]gen.ErrorDetail{{Field: loc, Message: msg}})
}
