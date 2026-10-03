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
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/wso2/aep/ae-studio-tools/internal/auth"
	"github.com/wso2/aep/ae-studio-tools/internal/config"
	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// Deps is what the public listener's routes need. main.go builds it from a
// config.Load result: the gates panic on empty security parameters.
type Deps struct {
	Cfg      config.Config
	Verifier *auth.Verifier
	GitHub   github.Identity
	// Webhook serves POST /webhooks/github (WebhookHandler); it must not be nil.
	Webhook http.Handler
	// Files serves the /v1 read-only Files operations.
	Files files.Reader
	// References stores the reference documents aep-api uploads on
	// /internal/v1 (the git engine).
	References ReferenceStore
}

// Routes is the public listener's mount table (ticket 04 §1): one row per
// route group, each listener → gate → handler. Every gate runs before route
// matching inside its group, so an unknown path is 401/403 before it is 404.
// The health probes are on the health listener only. A path not listed here
// is 404; nothing redirects. The Files and MCP sockets are separate
// listeners (FilesSocketRoutes, MCPSocketRoutes).
func Routes(d Deps) http.Handler {
	// /v1: browser, Platform IdP user JWT of the pod's org. Gate → validator
	// → generated server; the read-only Files operations.
	v1 := auth.UserGate(d.Verifier, d.Cfg.UserAudiences, d.Cfg.OrgID, d.Cfg.OrgHandle)
	m2mGate := auth.M2MGate(d.Verifier, d.Cfg.M2MClientID, d.Cfg.OrgID)
	// /internal/v1: aep-api, AE-only M2M + X-Impersonate-Org. Per-op body
	// cap → gate → validator → generated server; every request is
	// access-logged.
	internal := func(next http.Handler) http.Handler {
		return accessLog(capOpBody(internalRouteFinder, internalBodyCaps, internalBodyBytes, m2mGate(next)))
	}
	internalSrv := internalServer{gh: d.GitHub, refs: d.References, githubOwner: d.Cfg.GitHubOwner}
	nf := http.HandlerFunc(notFound)

	mux := http.NewServeMux()
	mux.Handle("/v1/", v1(v1Handler(d.Files)))
	mux.Handle("/internal/v1/", internal(internalHandler(internalRouteFinder, internalSrv)))
	// The bare group roots are exact entries so the mux does not redirect
	// them to the subtree; each is gated, then 404.
	mux.Handle("/v1", v1(nf))
	mux.Handle("/internal/v1", internal(nf))
	// /webhooks/github: GitHub, HMAC signature.
	mux.Handle("POST /webhooks/github", d.Webhook)
	mux.Handle("/", nf)
	return uncleanPathNotFound(mux, []pathGroup{
		{prefix: internalV1 + "/", notFound: internal(nf)},
		{prefix: "/v1/", notFound: v1(nf)},
	})
}

// FilesSocketRoutes is the Files socket's mount table (04 §7): request
// budget → body cap → validator → generated server (files_sock.go). No token
// gate: the mount is the gate. Reads go through a.Reader, the apply through
// a. A path ServeMux would redirect is 404, as on the public listener.
func FilesSocketRoutes(a files.Applier) http.Handler {
	return withBudget(filesSocketRequestBudget, capBody(filesSocketBodyBytes, uncleanPathNotFound(filesSocketHandler(a), nil)))
}

// MCPSocketRoutes is the MCP socket's mount table (04 §7): request budget →
// body cap → validator → generated server (mcp_sock.go). No token gate: the
// mount is the gate. A path ServeMux would redirect is 404.
func MCPSocketRoutes(d MCPSocketDeps) http.Handler {
	return withBudget(mcpSocketRequestBudget, capBody(mcpSocketBodyBytes, uncleanPathNotFound(mcpSocketHandler(d), nil)))
}

// withBudget runs next under a deadline d from now (or the request's own,
// when sooner).
func withBudget(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// pathGroup is a route group's raw path prefix and its gated 404.
type pathGroup struct {
	prefix   string
	notFound http.Handler
}

// uncleanPathNotFound answers a path with dot segments, a double slash or the
// like 404 instead of letting ServeMux redirect it to its cleaned form ahead
// of every gate. A path whose raw prefix names a group is gated by that group
// first, so an unauthenticated caller still sees 401/403. Clean paths go to
// next.
func uncleanPathNotFound(next http.Handler, groups []pathGroup) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.EscapedPath()
		if r.Method == http.MethodConnect || isCleanPath(p) {
			next.ServeHTTP(w, r)
			return
		}
		for _, g := range groups {
			if strings.HasPrefix(p, g.prefix) {
				g.notFound.ServeHTTP(w, r)
				return
			}
		}
		notFound(w, r)
	})
}

// isCleanPath reports whether ServeMux would serve p as is: the same rule as
// its cleanPath (path.Clean, keeping one trailing slash). ServeMux does not
// clean CONNECT paths, so the caller lets those through.
func isCleanPath(p string) bool {
	if p == "" || p[0] != '/' {
		return false
	}
	c := path.Clean(p)
	if strings.HasSuffix(p, "/") && c != "/" {
		c += "/"
	}
	return c == p
}

func notFound(w http.ResponseWriter, _ *http.Request) {
	problem.Write(w, http.StatusNotFound, "not_found", "no such route")
}
