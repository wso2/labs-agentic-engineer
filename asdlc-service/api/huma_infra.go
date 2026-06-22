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

package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/feature/orgcreds"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/auth"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/ids"
)

// registerInfraHuma registers the two UNAUTHENTICATED infrastructure endpoints
// that live outside the /api/ tree:
//   - the Task-JWT JWKS document, fetched by verifiers before any auth, and
//   - the Anthropic effective-key resolver the agents-service calls (no user JWT).
//
// app.go routes their outer-mux paths into apiMux so these Huma operations serve
// them. effective-key takes {orgHandle} but is deliberately UNGATED (trusted
// S2S, slug-validated) — it is intentionally NOT a humakit.OrgScopedInput route,
// and lives in the api package (outside the feature arch-lock that forbids
// hand-rolled orgHandle bindings on user-facing routes).
func registerInfraHuma(api huma.API, taskTokens *auth.TaskTokenManager, anthropicSvc *orgcreds.AnthropicCredentialService) {
	type jwksOutput struct{ Body auth.JWKSResponse }
	huma.Register(api, huma.Operation{
		OperationID: "get-jwks",
		Method:      http.MethodGet,
		Path:        "/auth/external/jwks.json",
		Summary:     "Task-JWT public key set (JWKS) — unauthenticated discovery",
		Tags:        []string{"Discovery"},
	}, func(_ context.Context, _ *struct{}) (*jwksOutput, error) {
		if taskTokens == nil {
			return &jwksOutput{Body: auth.JWKSResponse{Keys: []auth.JWK{}}}, nil
		}
		return &jwksOutput{Body: taskTokens.JWKS()}, nil
	})

	// Registered unconditionally so the operation is always in the spec; the
	// handler nil-checks the service at request time (nil only in spec-gen).
	type effKeyInput struct {
		OrgHandle string `path:"orgHandle" doc:"OpenChoreo org handle"`
	}
	type effKeyOutput struct{ Body *orgcreds.EffectiveKeyResponse }
	huma.Register(api, huma.Operation{
		OperationID: "get-anthropic-effective-key",
		Method:      http.MethodGet,
		Path:        "/internal/credentials/orgs/{orgHandle}/anthropic/effective-key",
		Summary:     "Resolve the effective Anthropic key for an org (internal; agents-service only; unauthenticated)",
		Tags:        []string{"Internal"},
	}, func(ctx context.Context, in *effKeyInput) (*effKeyOutput, error) {
		if err := ids.Slug(in.OrgHandle); err != nil {
			return nil, huma.Error400BadRequest("orgHandle: " + err.Error())
		}
		if anthropicSvc == nil {
			return nil, huma.Error503ServiceUnavailable("anthropic credential service not configured")
		}
		out, err := anthropicSvc.EffectiveKey(ctx, in.OrgHandle)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to resolve effective key")
		}
		return &effKeyOutput{Body: out}, nil
	})
}
