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

package agentsvc

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// defaultAudience is the aud claim the agents service verifies
	// (AGENT_JWT_AUDIENCE). Keep in sync with components/dataplane/ae-system-project/ae-studio/ae-design-agent config.
	defaultAudience = "agents-service"
	// tokenTTL bounds replay: the token only needs to be valid when the request
	// reaches the service; once the stream is accepted expiry is irrelevant.
	tokenTTL = 5 * time.Minute
	// nbfSkew backdates the nbf claim to absorb minor multi-node clock skew.
	// Without it, a verifier whose clock trails ours by a fraction of a second
	// rejects an otherwise-valid token as not-yet-valid, 401ing S2S turn
	// dispatch.
	nbfSkew = 30 * time.Second
)

// tokenSigner mints the outbound M2M bearer. The interface is the seam that
// lets an RS256/JWKS signer replace HS256 without touching the client: the
// service already accepts either (AGENT_JWT_SECRET or AGENT_JWT_JWKS_URL).
type tokenSigner interface {
	sign() (string, error)
}

// hs256Signer signs a short-lived HS256 JWT with a shared secret — the local
// M2M scheme (AGENT_JWT_SECRET on the service side).
type hs256Signer struct {
	secret   []byte
	audience string
	issuer   string
}

func newHS256Signer(secret, audience, issuer string) *hs256Signer {
	return &hs256Signer{secret: []byte(secret), audience: audience, issuer: issuer}
}

func (s *hs256Signer) sign() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{s.audience},
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now.Add(-nbfSkew)),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
	}
	if s.issuer != "" {
		claims.Issuer = s.issuer
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}
