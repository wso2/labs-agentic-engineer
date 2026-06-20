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

import (
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/middleware"
)

// ServiceRouter is the Service-JWT analogue of Router (api/router.go): it makes
// "this Service-JWT route is tenant-scoped" an allowlist-by-construction
// invariant for the gsMux surface (/api/v1/repos + /internal/credentials).
// Every route registers through one of three typed methods that classify it by
// the scope binding it requires:
//
//   - RepoScoped/HandleRepoScoped — repo+project routes; wrapped by repoScope
//     (middleware.RequireOrgScope) which validates {orgId}/{projectId}, 404s a
//     cross-org pair, and binds a Service-JWT Caller.
//   - OrgScoped — org-only routes (/internal/credentials/orgs/{orgHandle}/*);
//     wrapped by middleware.BindServiceOrgScope("orgHandle") which validates the
//     org slug (400) and binds a Service-JWT Caller, no DB row.
//   - Public — enumerated carve-outs with no path org var to scope on (org
//     travels in the body / installation id / repo full-name and is validated
//     downstream).
//
// Because Go's ServeMux binds path wildcards only after the leaf handler is
// selected, the scope middleware MUST wrap the leaf handler (not the mux) — the
// same constraint Router solves for the User-JWT plane.
type ServiceRouter struct {
	mux       *http.ServeMux
	repoScope func(http.Handler) http.Handler
}

// NewServiceRouter returns a ServiceRouter over mux. repoScope may be nil in
// dev/test setups where RequireOrgScope is disabled (no RepoRepository wired);
// RepoScoped/HandleRepoScoped then degrade to a no-op wrap, preserving the
// handler bindings.
func NewServiceRouter(mux *http.ServeMux, repoScope func(http.Handler) http.Handler) *ServiceRouter {
	return &ServiceRouter{mux: mux, repoScope: repoScope}
}

// repoWrap applies repoScope, falling back to a no-op when it is nil (dev/test).
func (rt *ServiceRouter) repoWrap(h http.Handler) http.Handler {
	if rt.repoScope == nil {
		return h
	}
	return rt.repoScope(h)
}

// RepoScoped registers a repo+project route gated by RequireOrgScope.
func (rt *ServiceRouter) RepoScoped(pattern string, h http.HandlerFunc) {
	rt.mux.Handle(pattern, rt.repoWrap(h))
}

// HandleRepoScoped is RepoScoped for handlers already expressed as http.Handler.
func (rt *ServiceRouter) HandleRepoScoped(pattern string, h http.Handler) {
	rt.mux.Handle(pattern, rt.repoWrap(h))
}

// OrgScoped registers an org-only route gated by BindServiceOrgScope("orgHandle").
func (rt *ServiceRouter) OrgScoped(pattern string, h http.HandlerFunc) {
	rt.mux.Handle(pattern, middleware.BindServiceOrgScope("orgHandle")(h))
}

// Public registers an enumerated carve-out route with NO path-org scope (org is
// in the body / installation id / repo full-name, validated downstream).
func (rt *ServiceRouter) Public(pattern string, h http.HandlerFunc) {
	rt.mux.HandleFunc(pattern, h)
}
