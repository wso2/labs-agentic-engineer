/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Package auth verifies the API gateway's signed assertion and puts the caller
// it names on the request context.
//
// Copied VERBATIM from the `go` skill — there is no line to edit. It imports
// only the standard library, so it also adds nothing to go.mod.
//
// # What this is for, and what it is not
//
// The gateway in front of this service has already done the authorization. It
// validated the caller's token against the environment's identity provider and
// checked the scope every operation declares in openapi.yaml; a request that
// failed either never reached this process. This file does NOT repeat that
// work, and a service that re-implements the operation → scope table is keeping
// a second copy of the contract that nothing keeps in sync.
//
// What a gateway cannot do for you is prove to your own code that a request
// came through it. Any pod that can open a socket to this service can send
// whatever headers it likes. So the gateway signs a JWT of the caller it
// authenticated — the assertion — and this file verifies it against ONE public
// certificate the platform published for this environment. That signature is
// the whole trust anchor: no identity-provider JWKS, no token introspection,
// no network round trip.
//
// So: the gateway decides WHETHER a request may happen. This file decides WHO
// the request is from, in a way a forged header cannot fake.
//
// # The three environment variables
//
// The platform sets all three on the container when the environment's gateway
// publishes a keypair (see the `api-configuration` trait). They are not
// optional and not guessable:
//
//	GATEWAY_ASSERTION_CERTIFICATE  PEM X.509 certificate carrying the public key
//	GATEWAY_ASSERTION_ISSUER       the `iss` every assertion carries
//	GATEWAY_ASSERTION_HEADER       the header it arrives in
//
// NewVerifierFromEnv fails when any is missing, and main must treat that as
// fatal. A service that starts without them cannot tell a real caller from a
// forged one, and starting anyway is the failure mode this design exists to
// remove.
package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Caller is the authenticated caller, as the gateway's assertion names them.
type Caller struct {
	// UserID is the assertion's `sub`. For an end user it is their directory
	// id; for a service-to-service caller it is the client id.
	UserID string
	// Scopes are the whole handles the caller holds. Compared with ==, never
	// with strings.Contains: `claims:read` is a prefix of `claims:read-all`,
	// and the gateway compares them as whole strings too.
	Scopes []string
	// OrgHandle is the assertion's `ouHandle` — the organization unit the
	// caller signed in to. Empty when the identity provider sent none.
	OrgHandle string
}

// HasScope reports whether the caller holds this exact handle.
func (c Caller) HasScope(handle string) bool {
	for _, s := range c.Scopes {
		if s == handle {
			return true
		}
	}
	return false
}

type callerCtxKey struct{}

// CallerFrom returns the verified caller, if the request carried an assertion.
//
// Absent is NOT an error here: a `security: []` operation is served by the
// gateway with no assertion at all, and its handler must read no identity.
// A handler that needs one calls RequireCaller instead.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerCtxKey{}).(Caller)
	return c, ok
}

// ErrNoCaller is what RequireCaller returns for a request that carried no
// assertion.
var ErrNoCaller = errors.New("auth: no verified caller on this request")

// RequireCaller is CallerFrom for a handler that cannot serve an anonymous
// request. Answer 401 on the error; never fall back to a caller-supplied id.
func RequireCaller(ctx context.Context) (Caller, error) {
	c, ok := CallerFrom(ctx)
	if !ok {
		return Caller{}, ErrNoCaller
	}
	return c, nil
}

// HasScope is the package-level form of Caller.HasScope. It never decides which
// rows a handler returns — that is the operation's PATH (`/me/…` the caller's,
// anything else every row; ADR-0031) — and it is never an operation's only
// check, because whether the operation may be called at all was settled at the
// gateway, from the contract. It exists for a second fact about the caller a
// handler needs for something other than authorization.
func HasScope(ctx context.Context, handle string) bool {
	c, ok := CallerFrom(ctx)
	return ok && c.HasScope(handle)
}

// Verifier holds the one public key this service trusts.
type Verifier struct {
	key    *rsa.PublicKey
	issuer string
	header string
	// leeway absorbs clock skew between the gateway and this pod on the `exp`
	// check. Small on purpose: the assertion is minted per request and lives
	// 15 minutes, so nothing legitimate needs more.
	leeway time.Duration
}

// NewVerifierFromEnv builds the verifier from the three variables the platform
// sets. Every failure is fatal to the process — see the package comment.
func NewVerifierFromEnv() (*Verifier, error) {
	certPEM := strings.TrimSpace(os.Getenv("GATEWAY_ASSERTION_CERTIFICATE"))
	issuer := strings.TrimSpace(os.Getenv("GATEWAY_ASSERTION_ISSUER"))
	header := strings.TrimSpace(os.Getenv("GATEWAY_ASSERTION_HEADER"))
	switch {
	case certPEM == "":
		return nil, errors.New("auth: GATEWAY_ASSERTION_CERTIFICATE is not set")
	case issuer == "":
		return nil, errors.New("auth: GATEWAY_ASSERTION_ISSUER is not set")
	case header == "":
		return nil, errors.New("auth: GATEWAY_ASSERTION_HEADER is not set")
	}
	block, _ := pem.Decode([]byte(certPEM))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("auth: GATEWAY_ASSERTION_CERTIFICATE is not a PEM certificate")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("auth: parse GATEWAY_ASSERTION_CERTIFICATE: %w", err)
	}
	key, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("auth: GATEWAY_ASSERTION_CERTIFICATE carries a %T, want an RSA key", cert.PublicKey)
	}
	// The certificate's own validity window is deliberately NOT checked. It is
	// self-signed and pinned by the platform that put it here, so it proves
	// nothing on its own and expires on a schedule that has nothing to do with
	// this request. What must be fresh is the ASSERTION, and its `exp` is
	// checked on every one.
	return &Verifier{key: key, issuer: issuer, header: header, leeway: 60 * time.Second}, nil
}

// Middleware verifies the assertion, if the request carries one, and puts the
// caller it names on the context.
//
// Three outcomes, and the middle one is the point:
//
//   - no assertion       → continue with no caller. The gateway serves a
//     `security: []` operation without one, and its handler reads no identity.
//   - assertion present but not verifiable → 401, immediately. A forged or
//     tampered assertion is never downgraded to "anonymous": that would make
//     forging one strictly better for an attacker than sending none.
//   - assertion verified → continue with the caller on the context.
//
// Register it with chi's r.Use, or wrap the handler directly. Unlike an
// authorization middleware it needs nothing from the generated server, so
// there is no ordering trap here.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(r.Header.Get(v.header))
		if raw == "" {
			next.ServeHTTP(w, r)
			return
		}
		caller, err := v.Verify(raw, time.Now())
		if err != nil {
			http.Error(w, "invalid gateway assertion", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerCtxKey{}, caller)))
	})
}

// assertionClaims is the subset of the assertion this service reads. The
// gateway mints more (aud, client_id, grant_type, auth_type, ouId); anything
// not named here is ignored rather than trusted.
type assertionClaims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Scope     string `json:"scope"`
	OrgHandle string `json:"ouHandle"`
	ExpiresAt int64  `json:"exp"`
	NotBefore int64  `json:"nbf"`
}

// Verify checks one assertion and returns the caller it names. Exported so a
// test can drive it without an HTTP round trip.
//
// The order matters: the SIGNATURE is checked before any claim is read, so no
// decision is ever made on a value an attacker chose.
func (v *Verifier) Verify(token string, now time.Time) (Caller, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Caller{}, errors.New("auth: assertion is not a three-part JWT")
	}
	var header struct {
		Alg string `json:"alg"`
	}
	headerJSON, err := decodeSegment(parts[0])
	if err != nil {
		return Caller{}, fmt.Errorf("auth: assertion header: %w", err)
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return Caller{}, fmt.Errorf("auth: assertion header: %w", err)
	}
	// Pinned, not read as a preference. Accepting whatever `alg` the token
	// names is how a signed token is turned into an unsigned one ("alg": "none")
	// or into an HMAC verified with the public key as its secret.
	if header.Alg != "RS256" {
		return Caller{}, fmt.Errorf("auth: assertion alg is %q, want RS256", header.Alg)
	}
	signature, err := decodeSegment(parts[2])
	if err != nil {
		return Caller{}, fmt.Errorf("auth: assertion signature: %w", err)
	}
	signed := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(v.key, crypto.SHA256, signed[:], signature); err != nil {
		return Caller{}, fmt.Errorf("auth: assertion signature does not verify: %w", err)
	}

	payload, err := decodeSegment(parts[1])
	if err != nil {
		return Caller{}, fmt.Errorf("auth: assertion payload: %w", err)
	}
	var claims assertionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Caller{}, fmt.Errorf("auth: assertion payload: %w", err)
	}
	// The issuer names the GATEWAY, not the identity provider. Pinning it is
	// what stops an assertion minted by some other gateway — another
	// environment's, say — being accepted here just because its key was
	// published to this container once.
	if claims.Issuer != v.issuer {
		return Caller{}, fmt.Errorf("auth: assertion iss is %q, want %q", claims.Issuer, v.issuer)
	}
	if claims.ExpiresAt == 0 {
		return Caller{}, errors.New("auth: assertion has no exp")
	}
	if now.After(time.Unix(claims.ExpiresAt, 0).Add(v.leeway)) {
		return Caller{}, errors.New("auth: assertion has expired")
	}
	if claims.NotBefore != 0 && now.Add(v.leeway).Before(time.Unix(claims.NotBefore, 0)) {
		return Caller{}, errors.New("auth: assertion is not valid yet")
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return Caller{}, errors.New("auth: assertion has no sub")
	}
	return Caller{
		UserID: claims.Subject,
		// Space-separated, as OAuth 2.0 spells a scope list. strings.Fields
		// rather than strings.Split(" ") so a double space is not a scope.
		Scopes:    strings.Fields(claims.Scope),
		OrgHandle: claims.OrgHandle,
	}, nil
}

// decodeSegment decodes one base64url JWT segment. Raw (unpadded) encoding is
// what a JWT uses; the padded decoder rejects it.
func decodeSegment(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
