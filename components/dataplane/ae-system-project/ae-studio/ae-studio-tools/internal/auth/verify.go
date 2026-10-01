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


// Package auth is ae-studio-tools' own JWT check (04 §9): exact iss, aud per
// token kind, and the org rule. It produces the two gates of 04 §1: UserGate
// for /v1/* (Platform IdP user JWT of the pod's org) and M2MGate for
// /internal/v1/* (the AE-only client_credentials client, impersonating the
// pod's org).
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// ErrUnauthenticated means the token is missing, malformed, unsigned by the
// IdP, expired, from another issuer, or not for any of the accepted audiences.
var ErrUnauthenticated = errors.New("unauthenticated")

const (
	grantClientCredentials = "client_credentials"
	impersonateOrgHeader   = "X-Impersonate-Org"
	bearerChallenge        = `Bearer realm="ae-studio-tools"`
	clockLeeway            = 5 * time.Second
)

// Claims is the part of a verified token the gates and handlers read.
type Claims struct {
	Sub, ClientID, GrantType, OuID, OuHandle string
	Audience                                 []string
}

// tokenClaims is the wire shape of a Thunder access token.
type tokenClaims struct {
	ClientID  string `json:"client_id"`
	GrantType string `json:"grant_type"`
	OuID      string `json:"ouId"`
	OuHandle  string `json:"ouHandle"`
	jwt.RegisteredClaims
}

// Verifier checks a token's signature against the IdP's JWKS, its exact
// issuer, its expiry and its audience.
type Verifier struct {
	jwks   *JWKSCache
	parser *jwt.Parser
}

// NewVerifier returns a verifier that accepts only RS256 tokens issued by
// exactly issuer, signed by a key in jwks, and carrying an exp claim.
func NewVerifier(issuer string, jwks *JWKSCache) *Verifier {
	return &Verifier{
		jwks: jwks,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
			jwt.WithIssuer(issuer),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(clockLeeway),
		),
	}
}

// Verify returns the claims of raw when it is valid and its aud contains at
// least one of audiences (exact match, no wildcard). Any failure wraps
// ErrUnauthenticated.
func (v *Verifier) Verify(raw string, audiences []string) (*Claims, error) {
	var tc tokenClaims
	if _, err := v.parser.ParseWithClaims(raw, &tc, v.keyFor); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	if !slices.ContainsFunc(tc.Audience, func(a string) bool { return slices.Contains(audiences, a) }) {
		return nil, fmt.Errorf("%w: audience not accepted", ErrUnauthenticated)
	}
	return &Claims{
		Sub: tc.Subject, ClientID: tc.ClientID, GrantType: tc.GrantType,
		OuID: tc.OuID, OuHandle: tc.OuHandle, Audience: tc.Audience,
	}, nil
}

func (v *Verifier) keyFor(tok *jwt.Token) (any, error) {
	kid, ok := tok.Header["kid"].(string)
	if !ok || kid == "" {
		return nil, errors.New("kid not found in token header")
	}
	return v.jwks.PublicKeyForKid(kid)
}

type claimsCtxKey struct{}

// UserGate admits a Platform IdP user JWT for one of audiences whose org claim
// is the pod's org (ouId and ouHandle both match). Any client_credentials token
// is refused with 401, whatever its audience. A user of another org gets 403.
// The verified *Claims ride on the request context.
func UserGate(v *Verifier, audiences []string, orgID, orgHandle string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := verifyRequest(v, r, audiences)
			if !ok || c.GrantType == grantClientCredentials {
				writeUnauthenticated(w)
				return
			}
			if c.OuID != orgID || c.OuHandle != orgHandle {
				writeOrgMismatch(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsCtxKey{}, c)))
		})
	}
}

// M2MGate admits only the pinned AE-only client: a client_credentials token
// whose aud and client_id are clientID and which carries no org claim (11 §3).
// Anything else is 401. The X-Impersonate-Org header must then name the pod's
// org, else 403.
func M2MGate(v *Verifier, clientID, orgID string) func(http.Handler) http.Handler {
	audiences := []string{clientID}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := verifyRequest(v, r, audiences)
			if !ok || c.ClientID != clientID || c.GrantType != grantClientCredentials || c.OuID != "" {
				writeUnauthenticated(w)
				return
			}
			if r.Header.Get(impersonateOrgHeader) != orgID {
				writeOrgMismatch(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsCtxKey{}, c)))
		})
	}
}

// verifyRequest reads the bearer token from the Authorization header and
// verifies it. ok is false for a missing, malformed or invalid token.
func verifyRequest(v *Verifier, r *http.Request, audiences []string) (*Claims, bool) {
	scheme, raw, found := strings.Cut(r.Header.Get("Authorization"), " ")
	raw = strings.TrimSpace(raw)
	if !found || !strings.EqualFold(scheme, "Bearer") || raw == "" {
		return nil, false
	}
	c, err := v.Verify(raw, audiences)
	if err != nil {
		return nil, false
	}
	return c, true
}

func writeUnauthenticated(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", bearerChallenge)
	problem.Write(w, http.StatusUnauthorized, "unauthenticated", "a valid bearer token for this surface is required")
}

func writeOrgMismatch(w http.ResponseWriter) {
	problem.Write(w, http.StatusForbidden, "org_mismatch", "the caller's organization is not this pod's organization")
}
