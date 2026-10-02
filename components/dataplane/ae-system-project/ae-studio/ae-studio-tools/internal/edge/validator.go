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

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"

	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// routeFinder matches a request to a contract operation, in kin's FindRoute
// shape: the route and its path parameters, or an error on a route miss.
type routeFinder func(*http.Request) (*routers.Route, map[string]string, error)

// mustRouter builds the kin router over a contract embedded in the binary. A
// decode failure is a build defect, not a runtime condition, so it panics.
func mustRouter(name string, spec func() (*openapi3.T, error)) routers.Router {
	doc, err := spec()
	if err != nil {
		panic(fmt.Sprintf("embedded %s contract failed to load: %v", name, err))
	}
	router, err := legacyrouter.NewRouter(doc)
	if err != nil {
		panic(fmt.Sprintf("%s contract router: %v", name, err))
	}
	return router
}

// requestValidator validates every request that matches a contract operation
// before it reaches the generated handler. The contract is the route table: a
// route miss (an unknown path, or a method the operation does not declare,
// such as HEAD on a GET op that ServeMux would otherwise serve) is 404 here and
// never reaches next. A request that does not match its operation is 400 with
// invalidCode, the group's problem code for a malformed request. Security is
// not checked here: the gate in front already did.
func requestValidator(find routeFinder, invalidCode string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, pathParams, err := find(r)
		if err != nil {
			notFound(w, r)
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
			problem.Write(w, http.StatusBadRequest, invalidCode, "the request does not match the contract")
			return
		}
		next.ServeHTTP(w, r)
	})
}
