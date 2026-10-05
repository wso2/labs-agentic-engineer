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
)

// route is one row of aep-api's mount table: a ServeMux pattern, who calls
// it, and the gate that admits that caller. A nil handler leaves the row
// unmounted (feature unconfigured or dev tier off), which answers 404.
type route struct {
	pattern string
	caller  string
	gate    string
	handler http.Handler
}

// routes is the whole request boundary of aep-api: one row per route group.
// Where each group lives:
//
//	/healthz /readyz                            health.go
//	/api/                                       public.go (gate: tenant_gate.go)
//	/internal/v1/...                            internal.go
//	/_dev/v1                                    dev.go
//
// Credential verification lives in internal/platform/auth.
func routes(p AppParams) []route {
	internalDeps := p.InternalDeps
	internalDeps.MCP = mcpRoutes(p)
	return []route{
		{"GET /healthz", "kubelet", "none", healthz()},
		{"GET /readyz", "kubelet", "none", readyz()},
		{"/api/", "console", "user JWT, orgensure, tenant gate", publicChain(p)},
		// One mount: the inner mux registers full paths (raw MCP routes and the
		// generated ops), so a path it does not name 404s.
		{internalV1 + "/", "coding runner (runs/, MCP), AE Studio tools pod (ae-studio/, MCP), aep-mcp-server (SRE handoff)", "internal gate table (internal.go)", newInternalV1Handler(internalDeps)},
		{"POST /_dev/v1/secret-ref-resync", "local tooling", "dev tier + LOCAL_OPENBAO_REPAIR, on no HTTPRoute", devResyncRoute(p)},
	}
}

// mountRoutes mounts every row that has a handler on one mux.
func mountRoutes(p AppParams) *http.ServeMux {
	mux := http.NewServeMux()
	for _, r := range routes(p) {
		if r.handler != nil {
			mux.Handle(r.pattern, r.handler)
		}
	}
	return mux
}
