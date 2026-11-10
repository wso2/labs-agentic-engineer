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

// Package auth is ae-studio-tools' own JWT check: exact iss, aud per
// token kind, and the org rule. It produces the two gates: UserGate
// for /v1/* (Platform IdP user JWT of the pod's org) and M2MGate for
// /internal/v1/* (the AE-only machine client, impersonating the pod's org).
// The IdP's own claims (who the org is, whether a token is a machine's) are
// read through the platform IdP's token dialect (the dialect package).
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/ae-studio-tools/internal/auth/dialect"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

var (
	// ErrUnauthenticated means the token is missing, malformed, unsigned by
	// the IdP, expired, from another issuer, or not for any of the accepted
	// audiences.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrIdPUnavailable means the IdP's key set could not be fetched (the
	// endpoint unreachable, a non-200, a body that is not a key set), so no
	// verdict on the token was reached: the caller answers "retry", never
	// "denied". The twin of @aep/platform-idp-auth's IdpUnavailableError.
	ErrIdPUnavailable = errors.New("idp unavailable")
)

const (
	impersonateOrgHeader = "X-Impersonate-Org"
	bearerChallenge      = `Bearer realm="ae-studio-tools"`
	clockLeeway          = 5 * time.Second
	// idpRetryAfter is the Retry-After (seconds) on idp_unavailable.
	idpRetryAfter = "5"
)

// Claims is the part of a verified token the gates and handlers read. Org
// and Machine are the dialect's reading of the IdP's own claims; MachineKnown
// is false when the token says nothing about being a user's or a machine's,
// and every gate refuses such a token rather than guess.
type Claims struct {
	Sub, ClientID string
	Org           dialect.Org
	Machine       bool
	MachineKnown  bool
	Audience      []string
}

// tokenClaims is the wire shape of a platform IdP access token: the
// dialect's claims by their wire names beside the ones every IdP shares.
type tokenClaims struct {
	dialect.Raw
	ClientID string `json:"client_id"`
	jwt.RegisteredClaims
}

// Verifier checks a token's signature against the IdP's JWKS, its exact
// issuer, its expiry and its audience, and reads its org and token type
// through the IdP's dialect.
type Verifier struct {
	jwks    *JWKSCache
	parser  *jwt.Parser
	dialect dialect.Dialect
}

// NewVerifier returns a verifier that accepts only RS256 tokens issued by
// exactly issuer, signed by a key in jwks, and carrying an exp claim, and
// reads them by d. An empty issuer would switch jwt's issuer check off, so
// it panics, as does a nil jwks: both are wiring errors, never runtime input.
func NewVerifier(issuer string, jwks *JWKSCache, d dialect.Dialect) *Verifier {
	if issuer == "" || jwks == nil {
		panic("auth.NewVerifier: issuer and jwks are required")
	}
	return &Verifier{
		jwks:    jwks,
		dialect: d,
		parser: jwt.NewParser(
			jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
			jwt.WithIssuer(issuer),
			jwt.WithExpirationRequired(),
			jwt.WithLeeway(clockLeeway),
		),
	}
}

// Verify returns the claims of raw when it is valid and its aud contains at
// least one of audiences (exact match, no wildcard). A key set that cannot be
// fetched wraps ErrIdPUnavailable; any other failure wraps ErrUnauthenticated.
func (v *Verifier) Verify(raw string, audiences []string) (*Claims, error) {
	var tc tokenClaims
	if _, err := v.parser.ParseWithClaims(raw, &tc, v.keyFor); err != nil {
		if errors.Is(err, ErrIdPUnavailable) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrUnauthenticated, err)
	}
	if !slices.ContainsFunc(tc.Audience, func(a string) bool { return slices.Contains(audiences, a) }) {
		return nil, fmt.Errorf("%w: audience not accepted", ErrUnauthenticated)
	}
	machine, merr := v.dialect.Machine(tc.Raw)
	return &Claims{
		Sub: tc.Subject, ClientID: tc.ClientID,
		Org: v.dialect.Org(tc.Raw), Machine: machine, MachineKnown: merr == nil,
		Audience: tc.Audience,
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

// UserGate admits a Platform IdP user JWT for one of audiences whose org is
// the pod's org: the dialect's org id and its declared handle both match. A
// machine token is refused with 401, whatever its audience, as is a token the
// dialect cannot place as a user's or a machine's, and one with no sub. A
// user of another org gets 403. An IdP whose keys cannot be fetched is 503
// idp_unavailable: the console retries instead of sending the user to sign in.
// The verified *Claims ride on the request context. It panics on a nil
// verifier, no audience, an empty audience entry or an empty org: each would
// widen the gate.
func UserGate(v *Verifier, audiences []string, orgID, orgHandle string) func(http.Handler) http.Handler {
	if v == nil || len(audiences) == 0 || slices.Contains(audiences, "") || orgID == "" || orgHandle == "" {
		panic("auth.UserGate: verifier, audiences, orgID and orgHandle are required")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := verifyRequest(v, r, audiences)
			if errors.Is(err, ErrIdPUnavailable) {
				writeIdPUnavailable(w, "user")
				return
			}
			if err != nil || !c.MachineKnown || c.Machine || c.Sub == "" {
				writeUnauthenticated(w)
				return
			}
			if c.Org.ID != orgID || !c.Org.HandleDeclared || c.Org.Handle != orgHandle {
				writeOrgMismatch(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsCtxKey{}, c)))
		})
	}
}

// M2MGate admits only the pinned AE-only client: a machine token (by the
// dialect) whose aud and client_id are clientID and which carries no org id.
// Anything else is 401, except an IdP whose keys cannot be fetched: 503
// idp_unavailable, so aep-api retries rather than reading a credential fault.
// The X-Impersonate-Org header must then name the pod's org, else 403. It panics on a nil verifier or an empty clientID or orgID:
// each would widen the gate.
func M2MGate(v *Verifier, clientID, orgID string) func(http.Handler) http.Handler {
	if v == nil || clientID == "" || orgID == "" {
		panic("auth.M2MGate: verifier, clientID and orgID are required")
	}
	audiences := []string{clientID}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := verifyRequest(v, r, audiences)
			if errors.Is(err, ErrIdPUnavailable) {
				writeIdPUnavailable(w, "m2m")
				return
			}
			if err != nil || c.ClientID != clientID || !c.MachineKnown || !c.Machine || c.Org.ID != "" {
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
// verifies it: ErrUnauthenticated for a missing, malformed or invalid token,
// ErrIdPUnavailable when the IdP's keys cannot be fetched.
func verifyRequest(v *Verifier, r *http.Request, audiences []string) (*Claims, error) {
	scheme, raw, found := strings.Cut(r.Header.Get("Authorization"), " ")
	raw = strings.TrimSpace(raw)
	if !found || !strings.EqualFold(scheme, "Bearer") || raw == "" {
		return nil, ErrUnauthenticated
	}
	return v.Verify(raw, audiences)
}

// writeIdPUnavailable answers a request whose token could not be checked. The
// log line names the gate only: never the token, the kid or the JWKS URL.
func writeIdPUnavailable(w http.ResponseWriter, gate string) {
	slog.Warn("auth.idp_unavailable", "gate", gate)
	w.Header().Set("Retry-After", idpRetryAfter)
	problem.Write(w, http.StatusServiceUnavailable, "idp_unavailable", "the identity provider cannot be reached")
}

func writeUnauthenticated(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", bearerChallenge)
	problem.Write(w, http.StatusUnauthorized, "unauthenticated", "a valid bearer token for this surface is required")
}

func writeOrgMismatch(w http.ResponseWriter) {
	problem.Write(w, http.StatusForbidden, "org_mismatch", "the caller's organization is not this pod's organization")
}
