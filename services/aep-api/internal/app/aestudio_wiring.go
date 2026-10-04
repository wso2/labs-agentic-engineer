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

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/organization/aestudio"
)

// aeStudioTools is aep-api's one adapter to each org's ae-studio-tools: the
// org's Target from its AE Studio status, the AE-only client's token
// (AE_STUDIO_INTERNAL_CLIENT_ID/SECRET at AE_STUDIO_IDP_TOKEN_URL). Boot
// never needs those; without them every call is ErrAEStudioMisconfigured
// (the ae-studio-tools degradation names them at boot).
func aeStudioTools(cfg config.AEStudioConfig, status organization.AEStudioStatusReader) *aestudiotools.Adapter {
	return aestudiotools.New(aestudiotools.Config{
		Endpoints: aeStudioEndpoints{status: status},
		Tokens:    aestudiotools.NewClientCredentials(cfg.IDP.TokenURL, cfg.InternalClientID, cfg.InternalClientSecret, nil),
	})
}

// aeStudioOC builds the OpenChoreo clients AE Studio reads and writes
// through, its status reads and its converge alike (aeStudioOCConfig).
func aeStudioOC(base openchoreo.Config) aestudio.OC {
	cfg := aeStudioOCConfig(base)
	return aestudio.OC{
		Projects:   openchoreo.NewProjectClient(cfg),
		Cells:      openchoreo.NewProjectCellClient(cfg),
		Targets:    openchoreo.NewWriteTargets(cfg),
		Resources:  openchoreo.NewResourceClient(cfg),
		SecretRefs: openchoreo.NewSecretReferenceClient(cfg),
	}
}

// aeStudioOCConfig is the OpenChoreo client config of AE Studio's reads and
// writes. Where the install authenticates aep-api's own calls with an M2M
// token and impersonates the target org (an AuthProvider and an
// ImpersonateOrgResolver are both set), AE Studio always does that:
// whatever the request strategy would pick, the request's user JWT is never
// passed through, so every member's poll reads and converges with one
// identity; the org-membership gate in front of GET /ae-studio is the only
// check on the caller. The strategy is dropped, and a nil strategy is
// AuthModeServiceM2M (openchoreo.Config). Anywhere else (local: one admin
// identity, no resolver) the config is the request's own, unchanged.
func aeStudioOCConfig(base openchoreo.Config) openchoreo.Config {
	if base.AuthProvider == nil || base.ImpersonateOrgResolver == nil {
		return base
	}
	base.RequestAuthStrategy = nil
	return base
}

// studioClientRecords answers the ae-studio-<org> client id recorded on the
// org's IDP profile (auth.StudioClientLookup): the binding the ae-studio/
// internal gate checks a tools pod's token against.
type studioClientRecords struct {
	profiles interface {
		GetProfileByOrgID(ctx context.Context, orgID string) (*organization.OrganizationIDPProfile, error)
	}
}

func (r studioClientRecords) StudioClientID(ctx context.Context, org string) (string, error) {
	p, err := r.profiles.GetProfileByOrgID(ctx, org)
	if err != nil || p == nil {
		return "", err
	}
	return p.StudioClientID, nil
}
