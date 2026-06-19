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

package api

import "net/http"

// Router wraps an http.ServeMux and applies the central tenant gate per route.
// It makes "this org-scoped route is gated" an allowlist-by-construction
// invariant (§6.1b): org-scoped routes register via OrgScoped (gated), and the
// only escape is the enumerated Public carve-out list. Because Go's ServeMux
// binds path wildcards only after the inner handler is selected, the gate MUST
// wrap the leaf handler (not the mux), which is exactly what OrgScoped does.
type Router struct {
	mux  *http.ServeMux
	gate func(http.Handler) http.Handler
}

// NewRouter returns a Router over mux that applies gate to every OrgScoped
// route.
func NewRouter(mux *http.ServeMux, gate func(http.Handler) http.Handler) *Router {
	return &Router{mux: mux, gate: gate}
}

// OrgScoped registers an org-scoped route whose handler runs only after the
// tenant gate has matched the path org to the caller's verified JWT org.
func (rt *Router) OrgScoped(pattern string, h http.HandlerFunc) {
	rt.mux.Handle(pattern, rt.gate(h))
}

// HandleOrgScoped is OrgScoped for handlers already expressed as http.Handler
// (e.g. SSE progress streamers).
func (rt *Router) HandleOrgScoped(pattern string, h http.Handler) {
	rt.mux.Handle(pattern, rt.gate(h))
}

// Public registers an enumerated carve-out route with NO org gate (listing
// scoped to the JWT's own org, discovery, dev-only reset, etc. — §6.6f).
func (rt *Router) Public(pattern string, h http.HandlerFunc) {
	rt.mux.HandleFunc(pattern, h)
}
