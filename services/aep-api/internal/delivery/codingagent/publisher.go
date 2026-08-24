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

package codingagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/organization"
)

const (
	envPublisherClientID     = "PUBLISHER_CLIENT_ID"
	envPublisherClientSecret = "PUBLISHER_CLIENT_SECRET"
	envPublisherTokenURL     = "PUBLISHER_TOKEN_URL"
)

// PublisherCredentialResolver loads the org's Thunder publisher
// client_credentials SecretReference name (never the secret value).
type PublisherCredentialResolver interface {
	SecretRefName(ctx context.Context, orgID string) (secretRefName string, err error)
}

func PublisherTokenURLFromJWKS(jwksURL string) string {
	u := strings.TrimRight(strings.TrimSpace(jwksURL), "/")
	const suffix = "/oauth2/jwks"
	if !strings.HasSuffix(strings.ToLower(u), suffix) {
		return ""
	}
	return u[:len(u)-len(suffix)] + "/oauth2/token"
}

// WithPublisherCredentials wires the Thunder publisher SecretReference
// resolver and the already-derived token URL used on every dispatch.
func (e *CodingExecutor) WithPublisherCredentials(r PublisherCredentialResolver, tokenURL string) *CodingExecutor {
	e.publisher = r
	e.publisherTokenURL = tokenURL
	return e
}

type idpPublisherResolver struct {
	profiles organization.IDPRepository
}

func NewIDPPublisherResolver(profiles organization.IDPRepository) PublisherCredentialResolver {
	return &idpPublisherResolver{profiles: profiles}
}

func (r *idpPublisherResolver) SecretRefName(ctx context.Context, orgID string) (string, error) {
	if r == nil || r.profiles == nil {
		return "", fmt.Errorf("publisher resolver not wired")
	}
	row, err := r.profiles.GetProfileByOrgID(ctx, orgID)
	if err != nil {
		return "", fmt.Errorf("load publisher profile: %w", err)
	}
	if row == nil {
		return "", fmt.Errorf("publisher profile missing")
	}
	if !organization.HasPublisherSecretRef(row) {
		return "", nil
	}
	return strings.TrimSpace(*row.SecretRefName), nil
}

func (e *CodingExecutor) publisherSecretEnv(ctx context.Context, orgID string) ([]SecretEnvRef, string, error) {
	tokenURL := strings.TrimSpace(e.publisherTokenURL)
	if tokenURL == "" {
		return nil, "", fmt.Errorf("publisher credentials require PLATFORM_IDP_JWKS_URL ending in /oauth2/jwks (publisher token URL)")
	}
	if e.publisher == nil {
		return nil, "", fmt.Errorf("publisher credentials required")
	}
	refName, err := e.publisher.SecretRefName(ctx, orgID)
	if err != nil {
		return nil, "", fmt.Errorf("publisher credentials: %w", err)
	}
	refName = strings.TrimSpace(refName)
	if refName == "" {
		return nil, "", fmt.Errorf("%w: coding-agent publisher SecretReference is not stamped", delivery.ErrPublisherCredentialsMissing)
	}
	return []SecretEnvRef{
		{Key: envPublisherClientID, SecretName: refName, SecretKey: organization.PublisherSecretFieldClientID},
		{Key: envPublisherClientSecret, SecretName: refName, SecretKey: organization.PublisherSecretFieldClientSecret},
	}, tokenURL, nil
}
