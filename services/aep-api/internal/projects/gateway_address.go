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

package projects

import (
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Addressing a PROTECTED sibling API through the gateway.
//
// A component-kind dependency resolves, via OpenChoreo, to the provider's
// project Service — a direct pod-to-pod address with no gateway in front of it.
// That is correct for a trusted service-to-service caller and WRONG for a
// consumer that forwards untrusted traffic: a SPA's nginx proxying the browser's
// `/api` turns the project's trusted lane into an unauthenticated public one, and
// the backend then receives no injected identity at all (or, worse, whatever the
// browser chose to send).
//
// So for every protected sibling the platform ALSO publishes the gateway
// address, and the consumer picks the lane. The direct address stays: a genuine
// backend-to-backend caller may still want it.

// DefaultAPIGatewayHost is the in-cluster host:port of the API Platform gateway
// runtime — the hop that terminates authentication for a managed API.
//
// It MIRRORS the `api-configuration` ClusterTrait's Backend template, which
// points its static host at the same service. The two must move together: this
// is the address, that is the routing, and a consumer proxying here relies on
// both agreeing. Overridable via API_GATEWAY_HOST for a data plane that names its
// gateway differently.
//
// FULLY QUALIFIED on purpose, unlike the trait's `<service>.<namespace>` short
// form. The consumer here is nginx, and nginx's `resolver` queries the name
// verbatim — it does NOT apply the search domains in /etc/resolv.conf the way
// getaddrinfo does. The short name resolves for `wget` inside the very same pod
// and is NXDOMAIN for nginx, which surfaces as a 502 on every /api call with
// `could not be resolved (3: Host not found)` in the error log.
const DefaultAPIGatewayHost = "api-platform-default-gateway-gateway-runtime.openchoreo-data-plane.svc.cluster.local:8080"

// ProtectedSibling is one component-kind dependency whose provider sits behind
// the API gateway, resolved to everything needed to address it there.
type ProtectedSibling struct {
	// DepName is the LOGICAL dependency name from design.json. It keys the env
	// var, so it must be the name the coding agent codes against.
	DepName string
	// ComponentName is the SCOPED OC component name, taken from the dependency's
	// platform-recomputed endpoint wiring rather than re-derived here.
	ComponentName string
	// EndpointName is the provider's workload endpoint name ("http" by default).
	EndpointName string
}

// APIGatewayContextPath is the URL prefix the api-configuration trait gives a
// component's managed API.
//
// It MIRRORS the trait's `RestApi.spec.context` template:
//
//	/${metadata.environmentName}-${metadata.componentNamespace}-${metadata.componentName}-${parameters.endpointName}
//
// and must move with it. This prefix is how the gateway decides which API a
// request belongs to, so a consumer proxying to the gateway has to preserve it
// verbatim — drop it and every call 404s at the gateway instead of reaching the
// service.
func APIGatewayContextPath(environment, componentNamespace, scopedComponentName, endpointName string) string {
	if endpointName == "" {
		endpointName = spec.DefaultEndpointName
	}
	return "/" + environment + "-" + componentNamespace + "-" + scopedComponentName + "-" + endpointName
}

// ProtectedSiblingsOf returns the component-kind dependencies of comp whose
// provider component declares gateway auth.
//
// Resolution reads the dependency's `wiring.endpoint`, which the platform
// recomputes on every design save and which already carries the scoped component
// name and endpoint name. A dependency with no endpoint wiring yet is skipped
// rather than guessed at: the address would be wrong, and a wrong gateway prefix
// fails every call.
func ProtectedSiblingsOf(design *spec.DesignFile, comp spec.DesignComponent) []ProtectedSibling {
	if design == nil {
		return nil
	}
	var out []ProtectedSibling
	for _, dep := range comp.Dependencies {
		if dep.Kind != spec.DependencyKindComponent {
			continue
		}
		provider := findDesignComponent(design, dep.Name)
		if provider == nil || !spec.ResolveAPISecurityEnabled(*provider) {
			continue
		}
		wiring := dep.Wiring
		if wiring == nil || wiring.Endpoint == nil || wiring.Endpoint.Component == "" {
			continue
		}
		endpoint := wiring.Endpoint.Name
		if endpoint == "" {
			endpoint = provider.EndpointName()
		}
		out = append(out, ProtectedSibling{
			DepName:       dep.Name,
			ComponentName: wiring.Endpoint.Component,
			EndpointName:  endpoint,
		})
	}
	return out
}

// GatewayEnvVars projects one `<DEP>_GATEWAY_URL` pod env var per protected
// sibling. An empty gatewayHost or no siblings yields nothing, which is the
// correct answer for a component that consumes no protected API.
func GatewayEnvVars(gatewayHost, environment, componentNamespace string, siblings []ProtectedSibling) []openchoreo.WorkflowEnvVarRef {
	if gatewayHost == "" || environment == "" || componentNamespace == "" || len(siblings) == 0 {
		return nil
	}
	out := make([]openchoreo.WorkflowEnvVarRef, 0, len(siblings))
	for _, s := range siblings {
		if s.DepName == "" || s.ComponentName == "" {
			continue
		}
		out = append(out, openchoreo.WorkflowEnvVarRef{
			Key:   ocname.ServiceGatewayURLEnvName(s.DepName),
			Value: "http://" + gatewayHost + APIGatewayContextPath(environment, componentNamespace, s.ComponentName, s.EndpointName),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeEnvVars overlays platform-owned env vars onto the user's, platform
// winning on a key collision.
//
// It is only ever called with a NON-NIL user slice. Nil means "this write does
// not manage the binding's env" (the config read failed, or no reader is wired),
// and merging into that would replace the user's whole env with the platform's
// two variables — silently dropping configuration the platform never owned.
func mergeEnvVars(user, platform []openchoreo.WorkflowEnvVarRef) []openchoreo.WorkflowEnvVarRef {
	if len(platform) == 0 {
		return user
	}
	owned := make(map[string]struct{}, len(platform))
	for _, p := range platform {
		owned[p.Key] = struct{}{}
	}
	out := make([]openchoreo.WorkflowEnvVarRef, 0, len(user)+len(platform))
	for _, u := range user {
		if _, taken := owned[u.Key]; taken {
			continue
		}
		out = append(out, u)
	}
	return append(out, platform...)
}
