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

package auth

// This file holds the runner-callback authorizer for the BFF's internal-S2S
// surface. RunnerAuthorizer is the internal analogue of the public edge's
// deny-by-default tenant gate: where that gate binds the org from a verified
// user JWT on /api, this binds it from a verified publisher-cc token on
// /internal — "a request cannot act on an org it does not own", checked
// before any handler. It lives beside PublisherTokenVerifier — one auth home.

import (
	"context"
	"log/slog"
	"strings"

	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// CycleOrgLookup returns the owning org handle of a run cycle. It is injected
// (from the composition root) so this package — the auth layer — never imports a
// feature package. Publisher-cc tokens are org-scoped and must confirm the
// path cycle belongs to the token's org.
//
// The CYCLE is the runner's identity: every agent pod is launched by the
// milestone supervisor, which carries the cycle id to the pod. A lookup that
// misses fails the request closed.
type CycleOrgLookup func(ctx context.Context, cycleID string) (orgHandle string, err error)

// RunnerAuthorizer verifies a runner-callback publisher-cc bearer and
// resolves the acting org. It is the inbound half of the S2S identity
// model — the analogue of the user-JWT verifier on the public edge —
// and the single home for the path↔org fence that used to live inline
// in the task controller.
type RunnerAuthorizer struct {
	publisher *PublisherTokenVerifier
	cycleOrg  CycleOrgLookup
}

// HTTPError is the neutral transport error the authorizer returns (401/403);
// the serving layer maps it onto its error envelope. Neutral on purpose: this
// package must not depend on any HTTP framework.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string { return e.Message }

// cycleUnavailable is the ONE answer the publisher-cc branch gives for a cycle
// the caller may not act on, whatever the reason. Kept a constant so a later
// edit cannot reintroduce a second, more specific message on one of the two
// arms — which is all it would take to make the pair distinguishable again.
const cycleUnavailable = "cycle not found"

// NewRunnerAuthorizer builds the authorizer. publisher may be nil — then every
// runner callback 401s (fail closed).
func NewRunnerAuthorizer(publisher *PublisherTokenVerifier, cycleOrg CycleOrgLookup) *RunnerAuthorizer {
	return &RunnerAuthorizer{publisher: publisher, cycleOrg: cycleOrg}
}

// Authorize verifies authHeader for a runner callback scoped to cycleID and
// returns the verified caller. Only a Thunder publisher-cc token is accepted
// (org-bound: the path cycle MUST belong to the token's org). Returns an
// *HTTPError on any failure; the caller (the internal surface's auth gate)
// maps it onto its envelope.
func (a *RunnerAuthorizer) Authorize(ctx context.Context, authHeader, cycleID string) (tenant.Caller, error) {
	const prefix = "Bearer "
	if len(authHeader) <= len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return tenant.Caller{}, &HTTPError{Status: 401, Message: "bearer token required"}
	}
	tok := authHeader[len(prefix):]

	if a.publisher == nil {
		slog.WarnContext(ctx, "runner callback: publisher verifier not configured", "cycle", cycleID)
		return tenant.Caller{}, &HTTPError{Status: 401, Message: "invalid bearer"}
	}
	claims, err := a.publisher.Verify(tok)
	if err != nil {
		slog.WarnContext(ctx, "runner callback: publisher bearer rejected", "cycle", cycleID, "error", err)
		return tenant.Caller{}, &HTTPError{Status: 401, Message: "invalid bearer"}
	}
	if a.cycleOrg == nil {
		slog.WarnContext(ctx, "runner callback: cycle lookup not configured", "cycle", cycleID)
		return tenant.Caller{}, &HTTPError{Status: 403, Message: cycleUnavailable}
	}

	// Org-bound: confirm the path cycle belongs to the token's org so an
	// org-A token cannot read/refresh an org-B cycle it merely names in the path.
	//
	// Both ways that check can fail answer with the SAME message. They are
	// different facts — "no such cycle" and "that cycle is another org's" — and
	// telling them apart is exactly what turns a valid org-A token into an oracle
	// for whether a given cycle id exists anywhere on the platform. To this caller
	// the two are identical anyway: neither is a cycle it may act on. The
	// distinction survives in the logs, where the operator can see it and the
	// prober cannot.
	cycleOrg, lerr := a.cycleOrg(ctx, cycleID)
	if lerr != nil || cycleOrg == "" {
		slog.WarnContext(ctx, "runner callback: cycle lookup failed",
			"cycle", cycleID, "error", lerr)
		return tenant.Caller{}, &HTTPError{Status: 403, Message: cycleUnavailable}
	}
	if cycleOrg != claims.OrgHandle {
		slog.WarnContext(ctx, "runner callback: publisher org mismatch",
			"cycle", cycleID, "cycleOrg", cycleOrg, "publisherOrg", claims.OrgHandle)
		return tenant.Caller{}, &HTTPError{Status: 403, Message: cycleUnavailable}
	}
	return tenant.Caller{
		Org:    tenant.OrgHandle(claims.OrgHandle),
		Source: tenant.SourcePublisherCC,
	}, nil
}
