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

package idp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/clients/oidc"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/httpkit"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
)

// --- Inputs / Outputs ------------------------------------------------------
// Org-scoped inputs embed humakit.OrgScopedInput, which declares {orgHandle}
// and applies the tenant gate (the IDOR fence) by construction. The discover
// carve-out is Public (no org assignment) so it does NOT embed it.

type orgInput struct {
	humakit.OrgScopedInput
}

type updateProfileInput struct {
	humakit.OrgScopedInput
	Body struct {
		Kind    string `json:"kind" doc:"IDP kind: platform | asgardeo | custom"`
		Issuer  string `json:"issuer" doc:"OIDC issuer URL; empty leaves the existing value unchanged"`
		JWKSURL string `json:"jwksUrl" doc:"JWKS URL; empty leaves the existing value unchanged"`
	}
}

// discoverInput is the Public IDP-picker carve-out (§6.6f): only a User JWT is
// required, not an org assignment, so it does NOT embed OrgScopedInput.
type discoverInput struct {
	Issuer string `query:"issuer" doc:"OIDC issuer URL to fetch the discovery document for"`
}

// profileOutput carries either the full IDP profile or, when no profile exists
// yet, a "no IDP configured" map (kind=null) — mirroring the legacy
// controller's 200-even-when-absent contract. Body is typed as any so a single
// operation models both branches.
type profileOutput struct{ Body any }

type clientSecretOutput struct {
	Body struct {
		ClientSecret string `json:"clientSecret"`
	}
}

type discoverOutput struct {
	Body struct {
		Issuer  string `json:"issuer"`
		JWKSURL string `json:"jwksUrl"`
	}
}

// RegisterIDP registers the IDP feature's HTTP operations on the Huma API. It
// is the code-first replacement for registerIDPRoutes (api/idp_routes.go): same
// paths, same auth posture (org-scoped routes gated by OrgScopedInput; the
// discover helper is the Public User-JWT carve-out), with the spec generated
// from the typed inputs/outputs.
func RegisterIDP(api huma.API, svc IDPService) {
	huma.Register(api, huma.Operation{
		OperationID: "get-idp-profile",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/idp-profile",
		Summary:     "Get the organization's IDP profile",
		Tags:        []string{"IDP"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgInput) (*profileOutput, error) {
		profile, err := svc.GetProfile(ctx, in.OrgHandle)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to load IDP profile")
		}
		if profile == nil {
			return &profileOutput{Body: map[string]interface{}{
				"orgId":   in.OrgHandle,
				"kind":    nil,
				"message": "no IDP profile configured yet; first protected-component deploy provisions one",
			}}, nil
		}
		return &profileOutput{Body: profile}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-idp-profile",
		Method:      http.MethodPut,
		Path:        "/api/v1/organizations/{orgHandle}/idp-profile",
		Summary:     "Update the organization's IDP profile",
		Tags:        []string{"IDP"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *updateProfileInput) (*profileOutput, error) {
		actor := httpkit.ActorFromContext(ctx)
		updated, err := svc.UpdateProfile(ctx, in.OrgHandle, actor, UpdateProfileRequest{
			Kind:    in.Body.Kind,
			Issuer:  in.Body.Issuer,
			JWKSURL: in.Body.JWKSURL,
		})
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &profileOutput{Body: updated}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "regenerate-idp-client-secret",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/idp-profile/rotate",
		Summary:     "Rotate the publisher client secret",
		Tags:        []string{"IDP"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgInput) (*clientSecretOutput, error) {
		actor := httpkit.ActorFromContext(ctx)
		newSecret, err := svc.RegenerateClientSecret(ctx, in.OrgHandle, actor)
		if err != nil {
			if errors.Is(err, ErrIDPThunderUnavailable) {
				return nil, huma.Error503ServiceUnavailable("Thunder admin client not configured")
			}
			return nil, huma.Error500InternalServerError("failed to regenerate client secret")
		}
		out := &clientSecretOutput{}
		out.Body.ClientSecret = newSecret
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "discover-idp-issuer",
		Method:      http.MethodGet,
		Path:        "/api/v1/idp/discover",
		Summary:     "Discover an OIDC issuer's metadata",
		Tags:        []string{"IDP"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *discoverInput) (*discoverOutput, error) {
		if in.Issuer == "" {
			return nil, huma.Error400BadRequest("issuer query param required")
		}
		md, err := oidc.DiscoverFromIssuer(ctx, in.Issuer)
		if err != nil {
			return nil, huma.Error502BadGateway(err.Error())
		}
		out := &discoverOutput{}
		out.Body.Issuer = md.Issuer
		out.Body.JWKSURL = md.JWKSURI
		return out, nil
	})
}
