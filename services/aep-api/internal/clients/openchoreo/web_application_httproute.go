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

// WebApplicationComponentTypeName is the namespaced ComponentType PAS
// bootstraps into each org. AEP components reference it as
// deployment/web-application; the CR's metadata.name is unprefixed.
const WebApplicationComponentTypeName = "web-application"

const webApplicationComponentTypeRef = "deployment/web-application"

// Same CEL as deployments/helm-charts/platform/templates/openchoreo-org-types/
// componenttype-web-application.yaml and setup-aep.sh. Dedicated hostname +
// path / so OpenChoreo fills ReleaseBinding status.endpoints[].externalURLs.
const webApplicationHTTPRouteForEach = `${workload.endpoints.transformList(name, ep, ("external" in ep.visibility && ep.type in ["HTTP", "REST", "GraphQL", "Websocket"]) ? [name] : []).flatten()}`

const webApplicationHTTPRouteHostnames = `${[gateway.ingress.external.?http, gateway.ingress.external.?https]
              .filter(g, g.hasValue()).map(g, g.value().host).distinct()
              .map(h, oc_dns_label(endpoint, metadata.componentName, metadata.environmentName, metadata.componentNamespace) + "." + h)}`

func isWebApplicationEntrypoint(componentType string) bool {
	return componentType == webApplicationComponentTypeRef || componentType == WebApplicationComponentTypeName
}

func resourceID(r any) string {
	m, ok := r.(map[string]any)
	if !ok {
		return ""
	}
	id, _ := m["id"].(string)
	return id
}

func resourceHasHostnames(r any) bool {
	m, ok := r.(map[string]any)
	if !ok {
		return false
	}
	tmpl, _ := m["template"].(map[string]any)
	spec, _ := tmpl["spec"].(map[string]any)
	switch h := spec["hostnames"].(type) {
	case string:
		return h != ""
	case []any:
		return len(h) > 0
	default:
		return false
	}
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

// patchWebApplicationHTTPRouteResources replaces a hostname-less HTTPRoute
// template with httproute-external. Returns a new slice; in is not mutated.
func patchWebApplicationHTTPRouteResources(in []any) ([]any, bool) {
	hosted := false
	for _, r := range in {
		if resourceID(r) == "httproute-external" && resourceHasHostnames(r) {
			hosted = true
			break
		}
	}

	out := make([]any, 0, len(in))
	changed := false
	replaced := false
	for _, r := range in {
		id := resourceID(r)
		switch {
		case hosted && id == "httproute":
			changed = true
			continue
		case !hosted && (id == "httproute" || id == "httproute-external"):
			out = append(out, webApplicationExternalHTTPRoute())
			replaced = true
			changed = true
		default:
			out = append(out, r)
		}
	}
	if hosted || replaced {
		return out, changed
	}
	inserted := false
	withInsert := make([]any, 0, len(out)+1)
	for _, r := range out {
		withInsert = append(withInsert, r)
		if resourceID(r) == "service" {
			withInsert = append(withInsert, webApplicationExternalHTTPRoute())
			inserted = true
		}
	}
	if !inserted {
		withInsert = append(withInsert, webApplicationExternalHTTPRoute())
	}
	return withInsert, true
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

// ensureWebApplicationHTTPRouteHostnames overlays httproute-external onto the
// org's namespaced web-application ComponentType when it still emits a
// catch-all HTTPRoute. 404 is a no-op: local cluster types already mint
// hostnames and there is nothing namespaced to patch.
func (c *componentClient) ensureWebApplicationHTTPRouteHostnames(ctx context.Context, orgName string) error {
	return retryStaleWrite(ctx, "componenttype/"+WebApplicationComponentTypeName+" httproute", func(ctx context.Context) error {
		getResp, err := c.oc.GetComponentTypeWithResponse(ctx, orgName, ocgen.ComponentTypeNameParam(WebApplicationComponentTypeName))
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
		updResp, err := c.oc.UpdateComponentTypeWithBodyWithResponse(ctx, orgName, ocgen.ComponentTypeNameParam(WebApplicationComponentTypeName), "application/json", bytes.NewReader(raw))
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
