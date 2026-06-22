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

// organthropic_huma.go — code-first OpenAPI registration for the per-org
// Anthropic key surface. It is the Huma replacement for
// registerOrgAnthropicRoutes (api/org_anthropic_routes.go): same org-scoped
// paths, methods and auth posture, with the spec generated from typed
// inputs/outputs.
//
// The legacy OrgAnthropicController's HTTP methods are thin wrappers over an
// injected AnthropicCredentialService, so the register function takes that same
// service and replicates each handler body against the same service methods.
//
// See docs/design/anthropic-key-dual-token.md.

package orgcreds

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
)

// --- Inputs / Outputs ------------------------------------------------------
// Org-scoped inputs embed humakit.OrgScopedInput, which declares {orgHandle}
// and applies the tenant gate (the IDOR fence) by construction.

type orgAnthropicConnectInput struct {
	humakit.OrgScopedInput
	Body struct {
		APIKey string `json:"apiKey" doc:"Anthropic API key (sk-ant-...)"`
	}
}

type orgAnthropicStatusInput struct {
	humakit.OrgScopedInput
}

type orgAnthropicProjectionOutput struct {
	Body *AnthropicProjection
}

// orgAnthropicStatusMapOutput carries either the full AnthropicProjection
// (connected) or a minimal "not connected" object. Both serialize as the same
// JSON object, so a map-bodied output covers both shapes exactly as the legacy
// handler did.
type orgAnthropicStatusMapOutput struct {
	Body map[string]any
}

type orgAnthropicDisconnectOutput struct {
	Body map[string]string
}

// RegisterOrgAnthropic registers the per-org Anthropic connect / status /
// disconnect operations on the Huma API. It mirrors registerOrgAnthropicRoutes.
//
// Dependency mirrors NewOrgAnthropicController's param:
//   - anthropicSvc *AnthropicCredentialService
func RegisterOrgAnthropic(api huma.API, anthropicSvc *AnthropicCredentialService) {
	huma.Register(api, huma.Operation{
		OperationID: "connect-anthropic",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/anthropic",
		Summary:     "Connect or replace the org's Anthropic API key",
		Tags:        []string{"Anthropic Integration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgAnthropicConnectInput) (*orgAnthropicProjectionOutput, error) {
		proj, err := anthropicSvc.Connect(ctx, in.OrgHandle, AnthropicConnectRequest{
			APIKey: in.Body.APIKey,
		})
		if err != nil {
			return nil, mapCredentialError(err)
		}
		slog.InfoContext(ctx, "anthropic.connected", "ocOrgId", in.OrgHandle, "keyPrefix", proj.KeyPrefix)
		return &orgAnthropicProjectionOutput{Body: proj}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-anthropic-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/anthropic",
		Summary:     "Get the org's Anthropic key status (projection, no key)",
		Tags:        []string{"Anthropic Integration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgAnthropicStatusInput) (*orgAnthropicStatusMapOutput, error) {
		proj, err := anthropicSvc.Status(ctx, in.OrgHandle)
		if err != nil {
			var nfe *NotFoundError
			if errors.As(err, &nfe) {
				// Not connected — minimal payload so the console can show the
				// "Configure" panel without surfacing an error.
				return &orgAnthropicStatusMapOutput{Body: map[string]any{
					"ocOrgId": in.OrgHandle,
					"status":  "not_connected",
				}}, nil
			}
			return nil, mapCredentialError(err)
		}
		body, err := structToMap(proj)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal error")
		}
		return &orgAnthropicStatusMapOutput{Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "disconnect-anthropic",
		Method:      http.MethodDelete,
		Path:        "/api/v1/organizations/{orgHandle}/anthropic",
		Summary:     "Disconnect the org's Anthropic key",
		Tags:        []string{"Anthropic Integration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgAnthropicStatusInput) (*orgAnthropicDisconnectOutput, error) {
		if err := anthropicSvc.Disconnect(ctx, in.OrgHandle); err != nil {
			return nil, mapCredentialError(err)
		}
		slog.InfoContext(ctx, "anthropic.disconnected", "ocOrgId", in.OrgHandle)
		return &orgAnthropicDisconnectOutput{Body: map[string]string{"status": "disconnected"}}, nil
	})
}
