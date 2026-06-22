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

package component

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// --- Inputs / Outputs ------------------------------------------------------

type getConfigInput struct {
	humakit.OrgScopedInput
	ProjectName   string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ComponentName string `path:"componentName" doc:"Component name (DNS-label slug)"`
}

type updateConfigInput struct {
	humakit.OrgScopedInput
	ProjectName   string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ComponentName string `path:"componentName" doc:"Component name (DNS-label slug)"`
	Body          updateConfigBody
}

// updateConfigBody mirrors the legacy controller's anonymous request body
// (`{ envVars: [...] }`).
type updateConfigBody struct {
	EnvVars models.EnvVarSlice `json:"envVars"`
}

type configOutput struct{ Body *models.ComponentConfig }

// RegisterConfig registers the config feature's HTTP operations on the Huma
// API. Code-first replacement for registerConfigRoutes (api/config_routes.go):
// same paths, methods, auth posture, success status codes, and error mapping.
func RegisterConfig(api huma.API, svc ConfigService) {
	const prefix = "/api/v1/organizations/{orgHandle}/projects/{projectName}/components/{componentName}/configs"

	huma.Register(api, huma.Operation{
		OperationID: "get-component-config",
		Method:      http.MethodGet,
		Path:        prefix,
		Summary:     "Get component config",
		Tags:        []string{"Config"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *getConfigInput) (*configOutput, error) {
		if err := requireComponentSlugs(in.ProjectName, in.ComponentName); err != nil {
			return nil, err
		}
		config, err := svc.GetConfig(ctx, in.OrgHandle, in.ProjectName, in.ComponentName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to get config")
		}
		// Legacy returned 200 with a nil body when no config exists; a nil
		// *ComponentConfig marshals to JSON null, preserving that behaviour.
		return &configOutput{Body: config}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-component-config",
		Method:      http.MethodPut,
		Path:        prefix,
		Summary:     "Update component config",
		Tags:        []string{"Config"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *updateConfigInput) (*configOutput, error) {
		if err := requireComponentSlugs(in.ProjectName, in.ComponentName); err != nil {
			return nil, err
		}
		config, err := svc.UpdateConfig(ctx, in.OrgHandle, in.ProjectName, in.ComponentName, in.Body.EnvVars)
		if err != nil {
			// Legacy mapped any update error to 400 with the error string
			// (validation: empty/duplicate keys, or repo upsert failure).
			return nil, huma.Error400BadRequest(err.Error())
		}
		return &configOutput{Body: config}, nil
	})
}
