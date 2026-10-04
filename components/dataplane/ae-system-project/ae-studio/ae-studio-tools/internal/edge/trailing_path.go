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
	"net/http"
	"net/url"
	"strings"

	"github.com/getkin/kin-openapi/routers"
)

// trailingPath is a GET operation whose last path parameter is a trailing
// wildcard (a file path, which may contain slashes): the address is
// scope + <vars variable segments> + "/" + literal + "/" + <path...>. The
// contract can only declare the parameter as one segment, so a nested path
// misses the contract's router and ServeMux's generated pattern; the route
// finder (routes) and a {path...} catch-all (pattern) serve it instead.
type trailingPath struct {
	// scope is the escaped address up to the first variable segment, e.g.
	// "/v1/projects/".
	scope string
	// vars counts the variable segments between scope and literal.
	vars int
	// literal is the fixed segment before the wildcard, e.g. "files".
	literal string
}

// routes is router's FindRoute plus the nested form of tp: a nested address
// is validated as the operation with path set to the whole remainder.
func (tp trailingPath) routes(router routers.Router) routeFinder {
	return func(r *http.Request) (*routers.Route, map[string]string, error) {
		route, params, err := router.FindRoute(r)
		if err == nil {
			return route, params, nil
		}
		probe, value, ok := tp.nested(r)
		if !ok {
			return nil, nil, err
		}
		route, params, perr := router.FindRoute(probe)
		if perr != nil {
			return nil, nil, err
		}
		params["path"] = value
		return route, params, nil
	}
}

// nested recognises a GET address of tp whose path has more than one
// segment. It returns a probe request for the same variable segments with a
// one-segment path (which the contract's router matches as the operation)
// and the decoded remainder. Anything else is not ok.
func (tp trailingPath) nested(r *http.Request) (probe *http.Request, value string, ok bool) {
	if r.Method != http.MethodGet {
		return nil, "", false
	}
	rest, found := strings.CutPrefix(r.URL.EscapedPath(), tp.scope)
	if !found {
		return nil, "", false
	}
	parts := strings.SplitN(rest, "/", tp.vars+2)
	if len(parts) != tp.vars+2 || parts[tp.vars] != tp.literal || !strings.Contains(parts[tp.vars+1], "/") {
		return nil, "", false
	}
	for _, v := range parts[:tp.vars] {
		if v == "" {
			return nil, "", false
		}
	}
	value, err := url.PathUnescape(parts[tp.vars+1])
	if err != nil {
		return nil, "", false
	}
	probeURL, err := url.Parse(tp.scope + strings.Join(parts[:tp.vars], "/") + "/" + tp.literal + "/_")
	if err != nil {
		return nil, "", false
	}
	probeURL.RawQuery = r.URL.RawQuery
	probe = r.Clone(r.Context())
	probe.URL = probeURL
	return probe, value, true
}

// pattern is the ServeMux catch-all for tp's method and address, naming the
// variable segments as names and the wildcard {path...}.
func (tp trailingPath) pattern(names ...string) string {
	segs := make([]string, len(names))
	for i, n := range names {
		segs[i] = "{" + n + "}"
	}
	return http.MethodGet + " " + tp.scope + strings.Join(segs, "/") + "/" + tp.literal + "/{path...}"
}
