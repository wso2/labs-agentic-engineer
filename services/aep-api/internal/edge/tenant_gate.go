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
	"log/slog"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// tenantGateCarveOuts enumerates — in exactly ONE place — the public
// operations that run WITHOUT the org claim requirement. Keys are the strict
// interface method names (the generated middleware's operationID), 1:1 with
// the contract's operationIDs via the name normalizer.
//
// Everything else is org-scoped BY DEFAULT: the gate denies a claimless
// request (ENFORCE) before any handler runs. This deny-by-default posture
// replaced the opt-in humakit.OrgScopedInput embedding — forgetting to gate a
// new operation is no longer representable; extending THIS list is the only
// way to un-gate one, and the arch guard pins the list against the contract.
//
//   - ListOrganizations: the pre-org-selection carve-out (§6.6f). It carries
//     no org context — the console calls it to render the org switcher before
//     an org claim exists — and its service scopes itself. Still user-JWT
//     authenticated at the outer middleware.
var tenantGateCarveOuts = map[string]struct{}{
	"ListOrganizations": {},
}

// tenantGate is the deny-by-default tenant gate, applied to every strict
// operation. The active org is derived SOLELY from the verified JWT claims
// (there is no org path/query/body input anywhere on the public edge — a
// cross-org request is unrepresentable by construction, the IDOR fence). On
// success it binds the org into the context; handlers read it via
// tenant.BoundOrgFromContext and pass it to services as an explicit argument.
//
// Gate mode rides the request context (stamped by mountRoutes): ENFORCE
// denies claimless requests with 401; LOG passes them through with a canary
// warning and no bound org.
func tenantGate(f gen.StrictHandlerFunc, operationID string) gen.StrictHandlerFunc {
	if _, ok := tenantGateCarveOuts[operationID]; ok {
		return f
	}
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		tokenOrg := auth.ResolveOuHandle(auth.ClaimsFromContext(ctx))
		if tokenOrg == "" {
			mode := tenant.GateModeFromContext(ctx)
			slog.WarnContext(ctx, "tenant gate would-deny",
				"reason", "no-org-claim", "op", operationID, "mode", string(mode))
			if mode == tenant.GateModeEnforce {
				return nil, errUnauthorized("authentication required")
			}
			return f(ctx, w, r, request)
		}
		return f(tenant.WithBoundOrg(ctx, tokenOrg), w, r, request)
	}
}
