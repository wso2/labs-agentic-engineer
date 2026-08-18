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

const webApplicationComponentTypeName = "web-application"
const webApplicationComponentTypeRef = "deployment/web-application"

// Same CEL as the local org web-application type (helm + setup-aep.sh): a
// dedicated hostname and path / so OpenChoreo fills
// ReleaseBinding status.endpoints[].externalURLs.
const webApplicationHTTPRouteForEach = `${workload.endpoints.transformList(name, ep, ("external" in ep.visibility && ep.type in ["HTTP", "REST", "GraphQL", "Websocket"]) ? [name] : []).flatten()}`

const webApplicationHTTPRouteHostnames = `${[gateway.ingress.external.?http, gateway.ingress.external.?https]
              .filter(g, g.hasValue()).map(g, g.value().host).distinct()
              .map(h, oc_dns_label(endpoint, metadata.componentName, metadata.environmentName, metadata.componentNamespace) + "." + h)}`

func componentTypeResourceID(r any) string {
	m, ok := r.(map[string]any)
	if !ok {
		return ""
	}
	id, _ := m["id"].(string)
	return id
}

func webApplicationExternalHTTPRoute() map[string]any {
	return map[string]any{
		"id":      "httproute-external",
		"forEach": webApplicationHTTPRouteForEach,
		"var":     "endpoint",
		"template": map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]any{
				"name":      `${oc_generate_name(metadata.componentName, endpoint)}`,
				"namespace": `${metadata.namespace}`,
				"labels":    `${oc_merge(metadata.labels, {"openchoreo.dev/endpoint-name": endpoint, "openchoreo.dev/endpoint-visibility": "external"})}`,
			},
			"spec": map[string]any{
				"parentRefs": []any{
					map[string]any{
						"name":      `${gateway.ingress.external.name}`,
						"namespace": `${gateway.ingress.external.namespace}`,
					},
				},
				"hostnames": webApplicationHTTPRouteHostnames,
				"rules": []any{
					map[string]any{
						"matches": []any{
							map[string]any{
								"path": map[string]any{
									"type":  "PathPrefix",
									"value": "/",
								},
							},
						},
						"backendRefs": []any{
							map[string]any{
								"name": `${metadata.componentName}`,
								"port": `${workload.endpoints[endpoint].port}`,
							},
						},
					},
				},
			},
		},
	}
}

// patchWebApplicationHTTPRouteResources replaces id:httproute (the PAS
// catch-all) with httproute-external. Everything else, including an already
// hosted httproute-external, is left alone.
func patchWebApplicationHTTPRouteResources(in []any) ([]any, bool) {
	out := make([]any, 0, len(in))
	changed := false
	for _, r := range in {
		if componentTypeResourceID(r) == "httproute" {
			out = append(out, webApplicationExternalHTTPRoute())
			changed = true
			continue
		}
		out = append(out, r)
	}
	return out, changed
}

func patchWebApplicationComponentTypeDoc(doc map[string]any) (bool, error) {
	if doc == nil {
		return false, fmt.Errorf("web-application componenttype: empty document")
	}
	spec, _ := doc["spec"].(map[string]any)
	if spec == nil {
		return false, nil
	}
	raw, ok := spec["resources"]
	if !ok {
		return false, nil
	}
	resources, ok := raw.([]any)
	if !ok {
		return false, fmt.Errorf("web-application componenttype: spec.resources is %T, want array", raw)
	}
	patched, changed := patchWebApplicationHTTPRouteResources(resources)
	if !changed {
		return false, nil
	}
	spec["resources"] = patched
	return true, nil
}

// ensureWebApplicationHTTPRouteHostnames replaces a catch-all httproute on
// the org namespaced web-application ComponentType. 404 is a no-op: local
// installs often have only the already-hosted cluster type.
func (c *componentClient) ensureWebApplicationHTTPRouteHostnames(ctx context.Context, orgName string) error {
	return retryStaleWrite(ctx, "componenttype/"+webApplicationComponentTypeName+" httproute", func(ctx context.Context) error {
		getResp, err := c.oc.GetComponentTypeWithResponse(ctx, orgName, ocgen.ComponentTypeNameParam(webApplicationComponentTypeName))
		if err != nil {
			return fmt.Errorf("get web-application componenttype: %w", err)
		}
		if getResp.StatusCode() == http.StatusNotFound {
			return nil
		}
		if getResp.StatusCode() != http.StatusOK {
			return fmt.Errorf("get web-application componenttype: %w", handleErrorResponse(getResp.StatusCode(), ErrorResponses{
				JSON401: getResp.JSON401,
				JSON403: getResp.JSON403,
				JSON404: getResp.JSON404,
				JSON500: getResp.JSON500,
			}))
		}

		var doc map[string]any
		if err := json.Unmarshal(getResp.Body, &doc); err != nil {
			return fmt.Errorf("decode web-application componenttype: %w", err)
		}
		changed, err := patchWebApplicationComponentTypeDoc(doc)
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			return fmt.Errorf("marshal web-application componenttype: %w", err)
		}
		updResp, err := c.oc.UpdateComponentTypeWithBodyWithResponse(ctx, orgName, ocgen.ComponentTypeNameParam(webApplicationComponentTypeName), "application/json", bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("update web-application componenttype: %w", err)
		}
		if updResp.StatusCode() != http.StatusOK && updResp.StatusCode() != http.StatusCreated {
			return fmt.Errorf("update web-application componenttype: %w", handleErrorResponse(updResp.StatusCode(), ErrorResponses{
				JSON400: updResp.JSON400,
				JSON401: updResp.JSON401,
				JSON403: updResp.JSON403,
				JSON404: updResp.JSON404,
				JSON409: updResp.JSON409,
				JSON500: updResp.JSON500,
			}))
		}
		return nil
	})
}
