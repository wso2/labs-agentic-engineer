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

package app

import (
	"fmt"

	"github.com/wso2/aep/aep-api/internal/clients/oauth"
	"github.com/wso2/aep/aep-api/internal/clients/secretmanagersvc/providers/openbao"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/ocauth"
	"github.com/wso2/aep/aep-api/secretsprovider"
)

// NewM2MAuthProvider wraps oauth.NewTokenProvider as a public ocauth.AuthProvider
// so overlay modules need not import internal/clients/oauth.
func NewM2MAuthProvider(tokenURL, clientID, clientSecret, hostHeader string) ocauth.AuthProvider {
	return oauth.NewTokenProvider(tokenURL, clientID, clientSecret, hostHeader)
}

// NewOSSOptions loads and validates config, then returns Options for the OSS
// entry: M2M AuthProvider when service-auth env is set, a request-auth
// strategy selected by OCForwardUserJWT (UserJWTStrategy when true,
// DirectOCStrategy — the direct-OC default — when false), a nil impersonation
// resolver, and an OpenBao-direct SecretsProvider when OPENBAO_ADDR is
// configured (nil = delivery off).
func NewOSSOptions() (Options, error) {
	cfg, err := config.Load()
	if err != nil {
		return Options{}, fmt.Errorf("load config: %w", err)
	}

	var authProvider ocauth.AuthProvider
	if cfg.ServiceAuth.TokenURL != "" && cfg.ServiceAuth.ClientID != "" {
		authProvider = NewM2MAuthProvider(
			cfg.ServiceAuth.TokenURL,
			cfg.ServiceAuth.ClientID,
			cfg.ServiceAuth.ClientSecret,
			cfg.ServiceAuth.HostHeader,
		)
	}

	var requestAuthStrategy ocauth.RequestAuthStrategy = DirectOCStrategy{}
	if cfg.OCForwardUserJWT {
		requestAuthStrategy = UserJWTStrategy{}
	}

	var secretsProvider secretsprovider.Provider
	if cfg.OpenBaoAddr != "" {
		p, err := openbao.NewProvider(&secretsprovider.OpenBaoConfig{
			Server: cfg.OpenBaoAddr,
			Path:   "secret",
			Auth:   &secretsprovider.OpenBaoAuth{Token: cfg.OpenBaoToken},
		})
		if err != nil {
			return Options{}, fmt.Errorf("openbao secrets provider: %w", err)
		}
		secretsProvider = p
	}

	return Options{
		AuthProvider:           authProvider,
		RequestAuthStrategy:    requestAuthStrategy,
		ImpersonateOrgResolver: nil,
		SecretsProvider:        secretsProvider,
	}, nil
}
