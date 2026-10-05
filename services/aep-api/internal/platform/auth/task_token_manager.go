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
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TaskTokenManager issues RS256-signed BFF identity JWTs (MCP discovery,
// outbound S2S) and publishes the public key at /auth/external/jwks.json.
//
// The signing key is loaded once at boot from the BFF_TASK_SIGNING_KEY env var.
// Verifiers fetch the public key via JWKS; rotation works by updating the env
// var and restarting the BFF — verifiers pick up the new kid automatically via
// JWKS kid-miss-refresh. The coding-agent Job authenticates with Thunder
// publisher CC, not these tokens.
type TaskTokenManager struct {
	keyID      string
	algorithm  string
	privateKey *rsa.PrivateKey
	jwks       JWKSResponse

	issuer string
	ttl    time.Duration
}

// TaskTokenConfig configures the manager.
type TaskTokenConfig struct {
	// PrivateKey is the PEM-encoded RSA private key (PKCS#1 or PKCS#8).
	// Passed as the BFF_TASK_SIGNING_KEY env var. Required.
	PrivateKey string
	// Issuer is the iss claim value (e.g., "aep-bff").
	Issuer string
	// Audience is required at construction (boot env still supplies it).
	// Each IssueServiceToken call sets aud explicitly.
	Audience string
	// TTL is the default lifetime when IssueServiceToken is passed a
	// non-positive ttl. Spec caps at 24h.
	TTL time.Duration
}

// TaskClaims is the custom claim set on BFF-signed identity JWTs.
type TaskClaims struct {
	jwt.RegisteredClaims
	TaskID    string `json:"taskId"`
	OcOrgID   string `json:"ocOrgId"`
	ProjectID string `json:"projectId,omitempty"`
}

// NewTaskTokenManager parses the signing key and returns a ready manager.
// Returns an error if the key is missing, malformed, or not RSA.
func NewTaskTokenManager(cfg TaskTokenConfig) (*TaskTokenManager, error) {
	if cfg.PrivateKey == "" {
		return nil, fmt.Errorf("BFF_TASK_SIGNING_KEY not configured")
	}
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("issuer not configured")
	}
	if cfg.Audience == "" {
		return nil, fmt.Errorf("audience not configured")
	}
	if cfg.TTL <= 0 {
		return nil, fmt.Errorf("TTL must be positive")
	}

	block, _ := pem.Decode([]byte(cfg.PrivateKey))
	if block == nil {
		return nil, fmt.Errorf("decode PEM block from BFF_TASK_SIGNING_KEY")
	}

	priv, err := parseRSAPrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	pub := &priv.PublicKey

	kid, err := deriveKeyID(pub)
	if err != nil {
		return nil, fmt.Errorf("derive kid: %w", err)
	}

	return &TaskTokenManager{
		keyID:      kid,
		algorithm:  "RS256",
		privateKey: priv,
		jwks: JWKSResponse{
			Keys: []JWK{{
				Kty: "RSA",
				Alg: "RS256",
				Use: "sig",
				Kid: kid,
				N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		},
		issuer: cfg.Issuer,
		ttl:    cfg.TTL,
	}, nil
}

// AudienceMCP is the aud claim on BFF-signed tokens that authenticate a caller
// to the BFF's internal MCP discovery surface. Only the local playground mint
// issues it, and POST /internal/v1/mcp no longer accepts it (publisher client
// token only, auth.PublisherMCPGate).
const AudienceMCP = "aep-api-mcp"

// mcpTokenTTL bounds an MCP identity token's validity. Like the agents-service
// token it only needs to be live when the request lands; minutes is ample and
// caps replay.
const mcpTokenTTL = 5 * time.Minute

// IssueMCPToken mints a short-lived BFF-signed identity JWT (aud AudienceMCP)
// carrying orgID in the ocOrgId claim, for a caller that will drive the BFF's
// MCP discovery surface. It is a thin wrapper over IssueServiceToken pinning the
// MCP audience + TTL; the org still travels in a verified claim, never a header.
func (m *TaskTokenManager) IssueMCPToken(orgID string) (string, error) {
	return m.IssueServiceToken(AudienceMCP, orgID, mcpTokenTTL)
}

// IssueServiceToken mints a short-lived BFF-signed JWT that authenticates an
// outbound BFF→service call (e.g. BFF→agents-service, design-agent MCP) and
// carries the acting org in the ocOrgId claim. Verifiers use the same
// /auth/external/jwks.json keyset, so org always travels in a signed claim,
// never a trusted header.
//
// audience names the target service (the verifier pins it as its aud check).
// ocOrgID is the gated active org; it MAY be empty for org-less service calls
// (e.g. the stateless dsl/render transform), which the target authenticates by
// signature+aud without requiring org. ttl is short (minutes); a non-positive
// ttl falls back to the manager's configured task TTL.
func (m *TaskTokenManager) IssueServiceToken(audience, ocOrgID string, ttl time.Duration) (string, error) {
	if audience == "" {
		return "", fmt.Errorf("audience is required")
	}
	if ttl <= 0 {
		ttl = m.ttl
	}
	now := time.Now()
	claims := TaskClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Audience:  jwt.ClaimStrings{audience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		OcOrgID: ocOrgID,
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = m.keyID

	signed, err := tok.SignedString(m.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign service token: %w", err)
	}
	return signed, nil
}

// JWKSResponse is the JSON shape served at /auth/external/jwks.json.
type JWKSResponse struct {
	Keys []JWK `json:"keys"`
}

// JWK is a single public key entry in JWK form.
type JWK struct {
	Kty string `json:"kty"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// JWKS returns the public key set (one entry — the active signing key).
func (m *TaskTokenManager) JWKS() JWKSResponse { return m.jwks }

// KeyID returns the kid of the active signing key.
func (m *TaskTokenManager) KeyID() string { return m.keyID }

// parseRSAPrivateKey attempts PKCS#1 first, then PKCS#8.
func parseRSAPrivateKey(der []byte) (*rsa.PrivateKey, error) {
	if priv, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return priv, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("not PKCS#1 or PKCS#8 RSA: %w", err)
	}
	priv, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return priv, nil
}

// deriveKeyID returns a stable kid derived from the public key's DER bytes.
// Truncated SHA-256 keeps the kid short while remaining unique enough that
// a key rotation produces a new kid (so verifiers know to refresh JWKS).
func deriveKeyID(pub *rsa.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:16]), nil
}
