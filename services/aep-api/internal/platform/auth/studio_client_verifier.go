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
	"context"
	"errors"
	"fmt"

	"github.com/wso2/aep/aep-api/internal/platform/auth/jwtassertion"
)

// studioClientPrefix names every org's AE Studio client: ae-studio-<org>
// (thundersvc.StudioAppName), registered in the org's Thunder OU, so its
// client_credentials token carries aud ae-studio-<org> and ouHandle <org>.
const studioClientPrefix = "ae-studio-"

// StudioClientLookup answers the AE Studio client id aep-api recorded for an
// org when it registered the client (organization_idp_profiles
// .studio_client_id); "" when the org has none.
type StudioClientLookup interface {
	StudioClientID(ctx context.Context, org string) (string, error)
}

// StudioClientVerifier verifies the client_credentials token of an org's AE
// Studio client (ae-studio-<org>), the credential the org's AE Studio tools
// pod presents on aep-api's ae-studio/ internal operations and on MCP
// (MCPGate). The token must be
// Thunder-signed with the expected issuer, name the client in its audience
// with a matching ouHandle (PublisherTokenVerifier's checks over the
// ae-studio- prefix), AND the client must be the one recorded for that org:
// the org is bound from that stored client id, and an org with none, or a
// client other than the recorded one, fails closed. An org's publisher token
// (the one its coding Jobs hold) never verifies here.
type StudioClientVerifier struct {
	tokens  *PublisherTokenVerifier
	clients StudioClientLookup
}

// NewStudioClientVerifier returns a verifier, or nil when any input is
// missing (the gate then refuses every ae-studio/ call).
func NewStudioClientVerifier(jwks *jwtassertion.JWKSCache, expectedIssuer string, clients StudioClientLookup) *StudioClientVerifier {
	tokens := NewPublisherTokenVerifier(jwks, expectedIssuer, studioClientPrefix)
	if tokens == nil || clients == nil {
		return nil
	}
	return &StudioClientVerifier{tokens: tokens, clients: clients}
}

// ErrStudioClientLookup is a token that could not be checked because the
// recorded client could not be read: not a verdict on the caller, so the gate
// answers it as unavailable rather than unauthorized.
var ErrStudioClientLookup = errors.New("ae-studio client lookup failed")

// errStudioClientNotRecorded is a verified token whose client is not the one
// recorded for its org.
var errStudioClientNotRecorded = errors.New("ae-studio client is not the one recorded for the org")

// Verify checks token and returns the org it binds.
func (v *StudioClientVerifier) Verify(ctx context.Context, token string) (string, error) {
	if v == nil {
		return "", fmt.Errorf("ae-studio client verifier not configured")
	}
	claims, err := v.tokens.Verify(token)
	if err != nil {
		return "", err
	}
	org := claims.OrgHandle
	recorded, err := v.clients.StudioClientID(ctx, org)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrStudioClientLookup, err)
	}
	if recorded == "" || recorded != studioClientPrefix+org {
		return "", errStudioClientNotRecorded
	}
	return org, nil
}
