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
	"context"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/config"
)

func ocClientConfig(cfg config.Config, seam Seam) openchoreo.Config {
	return openchoreo.Config{
		BaseURL:                  cfg.PlatformAPI.BaseURL,
		HostHeader:               cfg.PlatformAPI.HostHeader,
		AuthProvider:             seam.AuthProvider,
		RequestAuthStrategy:      seam.RequestAuthStrategy,
		ImpersonateOrgResolver:   seam.ImpersonateOrgResolver,
		PreferPlainHTTPEndpoints: !cfg.PlatformAPI.DataPlaneGatewayTLS,
	}
}

func ResolveWriteTarget(ctx context.Context, cfg config.Config, seam Seam) (string, error) {
	return openchoreo.ResolvePlatformWriteTarget(ctx, ocClientConfig(cfg, seam))
}
