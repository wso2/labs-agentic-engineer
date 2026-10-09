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

// NewHandlerForTest assembles the REAL production handler graph for the
// in-process component tier. It is the exported seam the componenttest
// harness calls: the same NewHandler /
// mountRoutes assembly production uses, with exactly two substitutions —
//
//   - inboundAuth REPLACES the JWKS-backed jwt.Middleware on the /api/ edge
//     (a claims-injector, so the real tenant gate runs in ENFORCE with no
//     Thunder/JWKS — §8.2). Pass nil to keep the production verifier.
//   - the raw-handler controller (webhook) is left nil;
//     mountRoutes already nil-guards it, so the handler degrades cleanly to
//     discovery + the /api/ chain + the global middleware stack.
//
// deps carries the feature services under test (real services + mocked
// clients). This lives in a non-_test.go file on purpose so packages outside
// api (the componenttest harness) can call it — it adds a seam, no
// production behavior.
func NewHandlerForTest(deps Deps, inboundAuth func(http.Handler) http.Handler) http.Handler {
	return NewHandler(AppParams{
		Deps:        deps,
		InboundAuth: inboundAuth,
		// Config left zero: TenantGateMode "" → tenant.ParseGateMode defaults to
		// ENFORCE, which is exactly what the component tier wants. Controllers,
		// OrganizationService, ThunderJWKS all nil (nil-guarded downstream).
	})
}
