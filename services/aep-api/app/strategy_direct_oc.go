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

package app

import (
	"context"
	"log/slog"

	"github.com/wso2/aep/aep-api/ocauth"
)

// DirectOCStrategy is the OSS / direct-OC default RequestAuthStrategy: it
// forwards the inbound console user's JWT to OpenChoreo as-is so OC applies
// that user's own RBAC, and falls back to the BFF's M2M service identity when
// the call was explicitly marked ocauth.WithServiceIdentity (background
// watchers, MCP tool calls, validation's runner-context lookups — see those
// call sites for why each one cannot use the caller's token).
//
// A call with neither a token nor that marker also gets M2M, which is the one
// uncomfortable edge here: absence of credentials resolves to the BFF's own
// full-privilege identity rather than to a refusal. It is how the
// webhook/dispatch paths that never carried a user token work today, and
// tenantGate 401s console traffic long before this runs, so nothing reaches it
// by losing a token mid-request. But "unauthenticated therefore privileged" is
// the wrong shape for a default, and a future path that drops the token on the
// floor would silently escalate rather than fail.
//
// Closing it means proving every tokenless caller carries the marker, and a
// miss surfaces as a 401 in a background job rather than a build failure. So
// the fallback is instrumented first: each occurrence logs at WARN with the
// call site's context, and once the warning stops appearing in practice this
// returns AuthModeNone — the transport then sends no bearer and OC refuses,
// loudly and in the right direction.
type DirectOCStrategy struct{}

// Decide implements ocauth.RequestAuthStrategy.
func (DirectOCStrategy) Decide(ctx context.Context) ocauth.AuthMode {
	if ocauth.IsServiceIdentity(ctx) {
		return ocauth.AuthModeServiceM2M
	}
	if ocauth.GetAuthToken(ctx) == "" {
		// Deliberately WARN, not DEBUG: this is the fail-open branch, and it
		// should be rare enough to read every occurrence. A path that turns up
		// here either wants ocauth.WithServiceIdentity or should not be
		// reaching OpenChoreo without a caller.
		slog.WarnContext(ctx, "openchoreo: no user token and no service-identity marker — "+
			"falling back to the BFF's M2M identity; mark this call site with ocauth.WithServiceIdentity if that is intended")
		return ocauth.AuthModeServiceM2M
	}
	return ocauth.AuthModeUserJWT
}

var _ ocauth.RequestAuthStrategy = DirectOCStrategy{}
