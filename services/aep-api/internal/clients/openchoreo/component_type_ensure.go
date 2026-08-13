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

package openchoreo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	ocgen "github.com/wso2/aep/aep-api/internal/clients/openchoreo/gen"
)

// EnsureComponentType get-or-creates the namespaced ComponentType.
// Idempotent: HTTP 409 → GET existing and return nil.
//
// body is the raw CR shape from CodingAgentComponentType() (map[string]any).
// Posted via CreateComponentTypeWithBody so we avoid a hand-written converter
// into gen.ComponentType — JSON round-trip through the gen client is enough.
func (c *componentClient) EnsureComponentType(ctx context.Context, orgName string, body map[string]any) error {
	name := componentTypeNameFromBody(body)
	if name == "" {
		return fmt.Errorf("ensure componenttype: body metadata.name is required")
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("ensure componenttype %q: marshal body: %w", name, err)
	}

	resp, err := c.oc.CreateComponentTypeWithBodyWithResponse(ctx, orgName, "application/json", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("ensure componenttype %q: %w", name, err)
	}

	switch {
	case resp.StatusCode() == http.StatusCreated || resp.StatusCode() == http.StatusOK:
		return nil
	case resp.StatusCode() == http.StatusConflict:
		getResp, gerr := c.oc.GetComponentTypeWithResponse(ctx, orgName, ocgen.ComponentTypeNameParam(name))
		if gerr != nil {
			return fmt.Errorf("ensure componenttype %q: conflict but refetch failed: %w", name, gerr)
		}
		if getResp.StatusCode() != http.StatusOK || getResp.JSON200 == nil {
			return fmt.Errorf("ensure componenttype %q: conflict but refetch failed: %w", name, handleErrorResponse(getResp.StatusCode(), ErrorResponses{
				JSON401: getResp.JSON401,
				JSON403: getResp.JSON403,
				JSON404: getResp.JSON404,
				JSON500: getResp.JSON500,
			}))
		}
		return nil
	default:
		return fmt.Errorf("ensure componenttype %q: %w", name, handleErrorResponse(resp.StatusCode(), ErrorResponses{
			JSON400: resp.JSON400,
			JSON401: resp.JSON401,
			JSON403: resp.JSON403,
			JSON409: resp.JSON409,
			JSON500: resp.JSON500,
		}))
	}
}

func componentTypeNameFromBody(body map[string]any) string {
	meta, _ := body["metadata"].(map[string]any)
	if meta == nil {
		return ""
	}
	name, _ := meta["name"].(string)
	return name
}
