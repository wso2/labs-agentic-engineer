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

// orggithub_huma.go — code-first OpenAPI registration for the per-org GitHub
// integration surface. It is the Huma replacement for registerOrgGitHubRoutes
// (api/org_github_routes.go): same org-scoped paths, methods and auth posture,
// with the spec generated from typed inputs/outputs.
//
// The legacy OrgGitHubController's HTTP methods are thin wrappers over an
// injected CredentialService / OrgDisconnectService / BearerService, so the
// register function takes those same dependencies (mirroring the params of
// NewOrgGitHubController) and replicates each handler body against the same
// service methods.
//
// SKIPPED: the App-mode connect callback (GET /api/v1/github/connect/callback).
// It is registered on the OUTER mux (registerConnectCallbackRoute), is NOT
// org-scoped (no {orgHandle}), and is authed via a signed connect-state JWT in
// the `state` query param rather than the console user JWT. The caller wires it
// separately.

package orgcreds

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/httpkit"
	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
)

// --- Inputs / Outputs ------------------------------------------------------
// Org-scoped inputs embed humakit.OrgScopedInput, which declares {orgHandle}
// and applies the tenant gate (the IDOR fence) by construction.

type orgGitHubStartConnectInput struct {
	humakit.OrgScopedInput
	Body struct {
		InstallationID int64 `json:"installationId,omitempty" doc:"Optional installation to pin (set when the user picks a candidate from the 2+ picker)"`
	}
}

type orgGitHubConnectPATInput struct {
	humakit.OrgScopedInput
	Body struct {
		PAT         string `json:"pat" doc:"GitHub personal access token"`
		GitHubLogin string `json:"githubLogin,omitempty" doc:"GitHub login the PAT belongs to"`
	}
}

type orgGitHubStatusInput struct {
	humakit.OrgScopedInput
}

type orgGitHubDisconnectInput struct {
	humakit.OrgScopedInput
	Uninstall bool `query:"uninstall" default:"true" doc:"App-mode only: when false, leave the install on GitHub for later re-adoption (defaults true)"`
}

type orgGitHubStartConnectOutput struct {
	Body map[string]string
}

// orgGitHubStatusMapOutput carries either the full Projection (connected) or a
// minimal "not connected" object. Both serialize as the same JSON object, so a
// map-bodied output covers both shapes exactly as the legacy handler did.
type orgGitHubStatusMapOutput struct {
	Body map[string]any
}

type orgGitHubProjectionOutput struct {
	Body *Projection
}

type orgGitHubDisconnectOutput struct {
	Body map[string]string
}

// RegisterOrgGitHub registers the per-org GitHub connect / status / disconnect
// operations on the Huma API. It mirrors registerOrgGitHubRoutes.
//
// Dependencies mirror NewOrgGitHubController's params:
//   - credentialSvc *CredentialService
//   - disconnectSv  *OrgDisconnectService
//   - bearerSvc     *BearerService
//   - appSlug, publicURL, appClientID string
//
// The same defaulting NewOrgGitHubController applied (appSlug → "asdlc-platform",
// publicURL → "http://localhost:8090") is applied here so behaviour is identical.
func RegisterOrgGitHub(
	api huma.API,
	credentialSvc *CredentialService,
	disconnectSv *OrgDisconnectService,
	bearerSvc *BearerService,
	appSlug, publicURL, appClientID string,
) {
	if appSlug == "" {
		appSlug = "asdlc-platform"
	}
	if publicURL == "" {
		publicURL = "http://localhost:8090"
	}

	huma.Register(api, huma.Operation{
		OperationID: "start-github-connect",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/github/connect/start",
		Summary:     "Start App-mode GitHub connect (OAuth-driven)",
		Tags:        []string{"GitHub Integration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgGitHubStartConnectInput) (*orgGitHubStartConnectOutput, error) {
		if appClientID == "" {
			return nil, huma.Error503ServiceUnavailable("github app oauth client not configured")
		}
		actor := httpkit.ActorFromContext(ctx)
		state, err := bearerSvc.IssueConnectState(in.OrgHandle, actor, in.Body.InstallationID, 15*time.Minute)
		if err != nil {
			slog.ErrorContext(ctx, "issue connect-state failed", "error", err, "org", in.OrgHandle)
			return nil, huma.Error500InternalServerError("could not start connect")
		}
		redirectURI := publicURL + connectCallbackPath
		authorizeURL := "https://github.com/login/oauth/authorize?client_id=" + url.QueryEscape(appClientID) +
			"&redirect_uri=" + url.QueryEscape(redirectURI) +
			"&state=" + url.QueryEscape(state)
		return &orgGitHubStartConnectOutput{Body: map[string]string{"authorizeUrl": authorizeURL}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "connect-github-pat",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/github/pat",
		Summary:     "Connect or replace a PAT-mode GitHub credential",
		Tags:        []string{"GitHub Integration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgGitHubConnectPATInput) (*orgGitHubProjectionOutput, error) {
		proj, err := credentialSvc.Connect(ctx, in.OrgHandle, ConnectRequest{
			Kind:        "user-pat",
			PAT:         in.Body.PAT,
			GitHubLogin: in.Body.GitHubLogin,
		})
		if err != nil {
			return nil, mapCredentialError(err)
		}
		return &orgGitHubProjectionOutput{Body: proj}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-github-status",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/github",
		Summary:     "Get GitHub connection status (projection, no token)",
		Tags:        []string{"GitHub Integration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgGitHubStatusInput) (*orgGitHubStatusMapOutput, error) {
		proj, err := credentialSvc.Status(ctx, in.OrgHandle)
		if err != nil {
			var nfe *NotFoundError
			if errors.As(err, &nfe) {
				// Not connected — minimal payload so the UI can render the
				// "Choose a connection method" panel without an error toast.
				return &orgGitHubStatusMapOutput{Body: map[string]any{
					"ocOrgId": in.OrgHandle,
					"status":  "not_connected",
					"kind":    "",
				}}, nil
			}
			return nil, mapCredentialError(err)
		}
		body, err := structToMap(proj)
		if err != nil {
			return nil, huma.Error500InternalServerError("internal error")
		}
		return &orgGitHubStatusMapOutput{Body: body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "disconnect-github",
		Method:      http.MethodDelete,
		Path:        "/api/v1/organizations/{orgHandle}/github",
		Summary:     "Disconnect GitHub (cascade) for the org",
		Tags:        []string{"GitHub Integration"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *orgGitHubDisconnectInput) (*orgGitHubDisconnectOutput, error) {
		if err := disconnectSv.Disconnect(ctx, in.OrgHandle, "manual.disconnect", in.Uninstall); err != nil {
			if errors.Is(err, ErrOrgNotFound) {
				return &orgGitHubDisconnectOutput{Body: map[string]string{"status": "not_connected"}}, nil
			}
			slog.ErrorContext(ctx, "disconnect failed", "error", err, "org", in.OrgHandle)
			return nil, huma.Error500InternalServerError("disconnect failed: " + err.Error())
		}
		return &orgGitHubDisconnectOutput{Body: map[string]string{"status": "disconnected"}}, nil
	})
}
