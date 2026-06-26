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
	"fmt"
	"net/http"
	"strings"

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
func RegisterConnections(api huma.API, svc *ValueService, registry *Registry) {
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

	// ── Org-level connection catalog: list (with consumers) + guarded delete. ──
	huma.Register(api, huma.Operation{
		OperationID: "list-org-connections",
		Method:      http.MethodGet,
		Path:        "/api/v1/organizations/{orgHandle}/connections",
		Summary:     "List the org's registered external connections with their consuming components",
		Tags:        []string{"Connections"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *listConnInput) (*listConnOutput, error) {
		if registry == nil {
			return nil, huma.Error503ServiceUnavailable("connection registry is not configured")
		}
		conns, err := registry.List(ctx, in.OrgHandle)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to list connections", err)
		}
		out := &listConnOutput{}
		out.Body.Connections = make([]connectionDTO, 0, len(conns))
		for i := range conns {
			consumers, cerr := registry.Consumers(ctx, in.OrgHandle, conns[i].Name)
			if cerr != nil {
				return nil, huma.Error500InternalServerError("failed to resolve connection consumers", cerr)
			}
			keys := make([]configKeyDTO, 0, len(conns[i].ConfigSchema))
			for _, k := range conns[i].ConfigSchema {
				keys = append(keys, configKeyDTO{Key: k.Key, Secret: k.Secret})
			}
			out.Body.Connections = append(out.Body.Connections, connectionDTO{
				Name:        conns[i].Name,
				Description: conns[i].Description,
				ConfigKeys:  keys,
				Consumers:   consumers,
			})
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-org-connection",
		Method:        http.MethodDelete,
		Path:          "/api/v1/organizations/{orgHandle}/connections/{connectionName}",
		Summary:       "Delete a registered connection (only when no component uses it)",
		Tags:          []string{"Connections"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *deleteConnInput) (*struct{}, error) {
		if registry == nil {
			return nil, huma.Error503ServiceUnavailable("connection registry is not configured")
		}
		consumers, err := registry.Consumers(ctx, in.OrgHandle, in.ConnectionName)
		if err != nil {
			return nil, huma.Error500InternalServerError("failed to check connection usage", err)
		}
		if len(consumers) > 0 {
			used := make([]string, 0, len(consumers))
			for _, c := range consumers {
				used = append(used, c.ProjectID+"/"+c.ComponentName)
			}
			return nil, huma.Error409Conflict(
				"connection is in use by " + joinUpTo(used, 5) + " — remove those components first")
		}
		if err := registry.Delete(ctx, in.OrgHandle, in.ConnectionName); err != nil {
			if err == ErrConnectionNotFound {
				return nil, huma.Error404NotFound("connection not found")
			}
			return nil, huma.Error500InternalServerError("failed to delete connection", err)
		}
		return nil, nil
	})
}

type listConnInput struct {
	humakit.OrgScopedInput
}

type configKeyDTO struct {
	Key    string `json:"key"`
	Secret bool   `json:"secret"`
}

type connectionDTO struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	ConfigKeys  []configKeyDTO       `json:"configKeys"`
	Consumers   []ConnectionConsumer `json:"consumers"`
}

type listConnOutput struct {
	Body struct {
		Connections []connectionDTO `json:"connections"`
	}
}

type deleteConnInput struct {
	humakit.OrgScopedInput
	ConnectionName string `path:"connectionName" doc:"External connection name to delete"`
}

// joinUpTo joins up to n items with ", ", appending "and N more" past the cap.
func joinUpTo(items []string, n int) string {
	if len(items) <= n {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:n], ", ") + fmt.Sprintf(", and %d more", len(items)-n)
}
