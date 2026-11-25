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
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
	"github.com/wso2/aep/aep-api/ocauth"
)

// NewM2MAuthProvider wraps oauth.NewTokenProvider as a public ocauth.AuthProvider
// so overlay modules need not import internal/clients/oauth.
func NewM2MAuthProvider(tokenURL, clientID, clientSecret, hostHeader string) ocauth.AuthProvider {
	return oauth.NewTokenProvider(tokenURL, clientID, clientSecret, hostHeader)
}

// NewOSSOptions loads and validates config, then returns Options for the OSS
// direct-OC entry: M2M AuthProvider when service-auth env is set,
// DirectOCStrategy, a nil impersonation resolver, secrets stored through
// OpenChoreo's Secret API (on SECRET_TARGET_PLANE_*), and an OpenBao session
// for the environment Thunder binding reader when OPENBAO_ADDR is configured,
// logged in by Kubernetes auth (OPENBAO_AUTH_*; no static token).
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

	var openBaoAuth secrets.VaultAuth
	if cfg.OpenBaoAddr != "" {
		// One session for the process, logged in lazily by the binding
		// reader Assemble builds.
		openBaoAuth = secrets.NewKubernetesAuth(cfg.OpenBaoAuth.Role, cfg.OpenBaoAuth.Mount, cfg.OpenBaoAuth.TokenPath)
	}

	return Options{
		AuthProvider:           authProvider,
		RequestAuthStrategy:    DirectOCStrategy{},
		ImpersonateOrgResolver: nil,
		openChoreoSecrets:      true,
		openBaoAuth:            openBaoAuth,
	}, nil
}
