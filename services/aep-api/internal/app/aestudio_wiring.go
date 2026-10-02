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
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/organization/aestudio"
)

// aeStudioConvergeOC builds the OpenChoreo clients an AE Studio converge
// reads and writes through (convergeOCConfig).
func aeStudioConvergeOC(base openchoreo.Config) aestudio.OC {
	cfg := convergeOCConfig(base)
	return aestudio.OC{
		Projects:   openchoreo.NewProjectClient(cfg),
		Cells:      openchoreo.NewProjectCellClient(cfg),
		Targets:    openchoreo.NewWriteTargets(cfg),
		Resources:  openchoreo.NewResourceClient(cfg),
		SecretRefs: openchoreo.NewSecretReferenceClient(cfg),
	}
}

// convergeOCConfig is the OpenChoreo client config of an AE Studio
// converge. Where the install authenticates aep-api's own calls with an M2M
// token and impersonates the target org (an AuthProvider and an
// ImpersonateOrgResolver are both set), the converge always does that:
// whatever the request strategy would pick, the request's user JWT is never
// passed through, so any member's poll converges with one identity and the
// caller's JWT only ever authorizes reading the status. The strategy is
// dropped, and a nil strategy is AuthModeServiceM2M (openchoreo.Config).
// Anywhere else (local: one admin identity, no resolver) the config is the
// request's own, unchanged.
func convergeOCConfig(base openchoreo.Config) openchoreo.Config {
	if base.AuthProvider == nil || base.ImpersonateOrgResolver == nil {
		return base
	}
	base.RequestAuthStrategy = nil
	return base
}
