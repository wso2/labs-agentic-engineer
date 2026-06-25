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

package connections

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/asdlc/asdlc-service/internal/platform/humakit"
)

type saveConnValuesInput struct {
	humakit.OrgScopedInput
	ProjectName    string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ConnectionName string `path:"connectionName" doc:"External connection name (e.g. openweather)"`
	Body           struct {
		// Environments maps an environment name (e.g. "development") to that
		// env's {key: value} map. Values are split into plain/secret by the
		// connection's registered schema — the caller never marks which is which.
		Environments map[string]map[string]string `json:"environments" doc:"Per-environment key→value map (development required)"`
	}
}

type saveConnValuesOutput struct {
	Body struct {
		Status string `json:"status"`
	}
}

// RegisterConnections registers the external-connection value-save operation.
// Saving values provisions the OC Resource model for the connection in the
// project and completes the gating config-collection task (the cascade then
// dispatches the dependent component tasks).
func RegisterConnections(api huma.API, svc *ValueService) {
	huma.Register(api, huma.Operation{
		OperationID: "save-connection-values",
		Method:      http.MethodPost,
		Path:        "/api/v1/organizations/{orgHandle}/projects/{projectName}/connections/{connectionName}/values",
		Summary:     "Save an external connection's per-environment values and provision it",
		Tags:        []string{"Connections"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *saveConnValuesInput) (*saveConnValuesOutput, error) {
		if svc == nil {
			return nil, huma.Error503ServiceUnavailable("connection provisioning is not configured")
		}
		if err := svc.SaveValues(ctx, in.OrgHandle, in.ProjectName, in.ConnectionName, in.Body.Environments); err != nil {
			return nil, huma.Error500InternalServerError("failed to save connection values", err)
		}
		out := &saveConnValuesOutput{}
		out.Body.Status = "provisioned"
		return out, nil
	})
}
