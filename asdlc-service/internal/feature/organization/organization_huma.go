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

package organization

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// --- Inputs / Outputs ------------------------------------------------------
//
// list-organizations is an enumerated gate carve-out (§6.6f): it carries no
// {orgHandle} path var and is scoped to the caller's own org inside the
// service, so its input does NOT embed humakit.OrgScopedInput (no per-route
// tenant gate). It still requires a user JWT.

type listOrganizationsInput struct{}

type organizationListOutput struct{ Body *models.OrganizationList }

// RegisterOrganization registers the unscoped /api/v1/organizations feature on
// the Huma API. It is the code-first replacement for registerOrganizationRoutes
// (api/organization_routes.go): same path, same auth posture, with the spec
// generated from the typed input/output.
//
// The BFF is read-only over OC namespaces — there is no create/update/delete
// operation here (tenant onboarding is the platform's job), so this mirrors the
// single GET the legacy controller served.
func RegisterOrganization(api huma.API, svc OrganizationService) {
	huma.Register(api, huma.Operation{
		OperationID: "list-organizations",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations",
		Summary:     "List organizations",
		Tags:        []string{"Organizations"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, _ *listOrganizationsInput) (*organizationListOutput, error) {
		list, err := svc.List(ctx)
		if err != nil {
			return nil, mapOrganizationError(err)
		}
		return &organizationListOutput{Body: list}, nil
	})
}

// mapOrganizationError mirrors the legacy controller's error→status mapping:
// ErrUnauthorized surfaces as 401, everything else as a generic 500.
func mapOrganizationError(err error) error {
	if errors.Is(err, ErrUnauthorized) {
		return huma.Error401Unauthorized("invalid or expired token")
	}
	return huma.Error500InternalServerError("failed to list organizations")
}
