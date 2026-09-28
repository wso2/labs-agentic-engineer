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
	"regexp"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// sreHandoffIncidentID is the opaque incident identity bound onto every
// request this gate authenticates (sourcecontrol.WithIncidentContext). It is
// intentionally a constant, not a per-alert value: CreateIssue only uses it
// (alongside org/project/componentName) to compute the dedupe hash, and the
// SRE-handoff design deliberately dedupes by component alone, not by a
// per-alert signature (docs/design/draft/2026-09-17-sre-agent-extensions-handoff.md
// §3) — there is no per-request signal this transport could bind that would
// mean anything finer. Without SOME incident context bound here,
// CreateIssue's own anti-spoofing guard (ErrIncidentContextRequired) rejects
// every SRE-filed componentName/actionStatuses outright, trusted bearer or not.
const sreHandoffIncidentID = "sre-handoff"

// sreHandoffRoute matches exactly the two operations the SRE-handoff
// credential may authenticate — CreateIssue and ListIssues — enumerated here
// in ONE place, the same "grow it and you're making a security decision"
// posture as tenantGateCarveOuts. Nothing else accepts this credential type:
// a bearer that verifies against SREHandoffVerifier but is presented to any
// other path falls through to the normal Thunder JWT verifier (and 401s,
// since it is never a valid JWT).
var sreHandoffRoute = regexp.MustCompile(`^/api/v1/projects/[^/]+/issues$`)

func isSREHandoffRoute(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return false
	}
	return sreHandoffRoute.MatchString(r.URL.Path)
}

// sreHandoffOrJWT returns the outermost /api/ middleware: on the exact
// SRE-handoff route with a bearer that verifies against verifier, it stamps
// synthetic Claims (see auth.SREHandoffVerifier) and calls next directly,
// skipping Thunder JWT verification for that one request. Every other
// request — wrong path, right path with no/invalid handoff bearer, verifier
// disabled — is unaffected and falls through to jwtMW unchanged, so this
// changes behaviour for no route/credential combination other than the one
// it names.
//
// tenantGate (unchanged) then binds the org from the stamped Claims exactly
// as it would from a verified Thunder JWT — see auth.WithClaims.
func sreHandoffOrJWT(verifier *auth.SREHandoffVerifier, jwtMW func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		jwtNext := jwtMW(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if verifier != nil && isSREHandoffRoute(r) {
				if claims, ok := verifier.Verify(r.Header.Get("Authorization")); ok {
					ctx := auth.WithClaims(r.Context(), claims)
					ctx = sourcecontrol.WithIncidentContext(ctx, sreHandoffIncidentID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}
			jwtNext.ServeHTTP(w, r)
		})
	}
}
