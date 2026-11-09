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

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/platform/auth/dialect"
	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// PublisherTokenVerifier validates the platform IdP's client_credentials
// access tokens whose audience identifies a per-org publisher OAuth app
// ("aep-publisher-{orgHandle}"). Mirrors agent-manager's
// PublisherClientAuthMiddleware at
// agent-manager-service/middleware/jwtassertion/auth.go:106 — same
// regex shape, same cross-org defense (the org handle embedded in the
// audience MUST match the org the dialect reads from the token).
//
// Used by the runner-callback handler (validation context) and the internal
// MCP gate to accept per-org publisher cc tokens, and over the ae-studio- prefix by
// StudioClientVerifier (the same checks, another per-org client).
type PublisherTokenVerifier struct {
	jwks           *jwtassertion.JWKSCache
	expectedIssuer string
	audiencePrefix string
	subjectPattern *regexp.Regexp
	dialect        dialect.Dialect
}

// PublisherClaims is the projection of a verified publisher cc token.
// OrgHandle is the canonical OC org handle: the one embedded in the
// audience, cross-checked against the org the dialect reads.
type PublisherClaims struct {
	jwt.RegisteredClaims
	dialect.Raw
	OrgHandle string `json:"-"`
}

// NewPublisherTokenVerifier returns a verifier. audiencePrefix is the
// shared prefix on every per-org publisher client (e.g. "aep-publisher-").
// jwks must be a populated cache pointed at the platform IDP's JWKS
// endpoint; expectedIssuer is the iss claim the IdP stamps (e.g.
// "platform-idp"); d reads the IdP's org claims. Returns nil when any
// input is empty — composition root uses this to disable cc verification
// cleanly in dev.
func NewPublisherTokenVerifier(jwks *jwtassertion.JWKSCache, expectedIssuer, audiencePrefix string, d dialect.Dialect) *PublisherTokenVerifier {
	if jwks == nil || strings.TrimSpace(expectedIssuer) == "" || strings.TrimSpace(audiencePrefix) == "" {
		return nil
	}
	escaped := regexp.QuoteMeta(audiencePrefix)
	pat := regexp.MustCompile("^" + escaped + `[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
	return &PublisherTokenVerifier{
		jwks:           jwks,
		expectedIssuer: expectedIssuer,
		audiencePrefix: audiencePrefix,
		subjectPattern: pat,
		dialect:        d,
	}
}

// Verify parses + validates the publisher cc token. Returns canonical
// claims on success; rejects with a descriptive error otherwise. Caller
// is responsible for the in-handler scope check (e.g. validating that
// the runner-presented taskID belongs to claims.OrgHandle).
func (v *PublisherTokenVerifier) Verify(tokenString string) (*PublisherClaims, error) {
	if v == nil {
		return nil, fmt.Errorf("publisher verifier not configured")
	}
	if tokenString == "" {
		return nil, fmt.Errorf("empty token")
	}
	tok, err := jwt.ParseWithClaims(tokenString, &PublisherClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("kid missing from token header")
		}
		return v.jwks.PublicKeyForKid(kid)
	})
	if err != nil {
		return nil, fmt.Errorf("parse publisher token: %w", err)
	}
	if !tok.Valid {
		return nil, fmt.Errorf("publisher token not valid")
	}
	claims, ok := tok.Claims.(*PublisherClaims)
	if !ok {
		return nil, fmt.Errorf("claims not PublisherClaims")
	}
	if claims.Issuer != v.expectedIssuer {
		return nil, fmt.Errorf("unexpected issuer %q", claims.Issuer)
	}

	// Identify the publisher app via the audience claim: every platform IdP
	// AE supports stamps a machine token's client id as aud (the
	// agent-manager pattern at
	// agent-manager/agent-manager-service/middleware/jwtassertion/auth.go:93).
	// Subject is not the client id on Thunder (an entity UUID), so it is
	// never read.
	var publisherAud string
	for _, a := range claims.Audience {
		if v.subjectPattern.MatchString(a) {
			publisherAud = a
			break
		}
	}
	if publisherAud == "" {
		return nil, fmt.Errorf("no publisher audience present (expected %s*)", v.audiencePrefix)
	}
	orgHandle := strings.TrimPrefix(publisherAud, v.audiencePrefix)

	// Cross-org defense: the org handle embedded in the audience MUST
	// match the org the dialect reads from the token. Defends against an
	// attacker who steals org A's publisher cc token and tries to act on
	// org B's data by hand-rolling a token with a mismatched org (which
	// would fail signature verification, but layered defense).
	//
	// A machine token must declare its org handle outright: aep-api
	// registers every publisher and studio client itself and stamps the
	// handle on it, so the user path's fallback to a name or id for legacy
	// tokens would only widen what is accepted here.
	org := v.dialect.Org(claims.Raw)
	if !org.HandleDeclared {
		return nil, fmt.Errorf("org handle claim missing")
	}
	if org.Handle != orgHandle {
		return nil, fmt.Errorf("org %q does not match audience org %q", org.Handle, orgHandle)
	}
	claims.OrgHandle = orgHandle
	return claims, nil
}
