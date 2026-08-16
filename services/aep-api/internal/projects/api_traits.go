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
	"context"
	"strings"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The `api-configuration` trait's DESIRED STATE, as pure functions over design
// facts. Composed into a whole deployment by DesiredDeploymentFor
// (deployment_spec.go) and written by DeploymentService — this file decides
// what the trait should say, never when or whether to write it.

// OrgPublisher is the narrow per-org Thunder publisher-provisioning surface
// the deployment projection consumes from the idp feature. Declared consumer-side so
// the component feature does not import idp's concrete service — the idp
// service satisfies it structurally and is injected via SetIDPService at the
// composition root.
type OrgPublisher interface {
	GetProfile(ctx context.Context, orgID string) (*organization.OrganizationIDPProfile, error)
	EnsureOrgPublisher(ctx context.Context, orgID, actor string) (clientID, clientSecret string, created bool, err error)
}

// -- Pure helpers ------------------------------------------------------------

// APIConfigurationInstanceName returns the canonical trait instance name for
// the component's managed endpoint. Mirrors the POC manifests' naming
// (`<componentName>-<endpointName>`) so on-cluster resources are predictable.
// The trait template uses this as the prefix for the generated Backend and
// RestApi resources (`<instanceName>-api-gw-backend`, `<instanceName>`).
//
// `endpointName` is the design.json-declared workload endpoint name; an empty
// value defaults to spec.DefaultEndpointName ("http"), preserving the prior
// `<componentName>-http` naming for components that declare no endpoint.
func APIConfigurationInstanceName(componentName, endpointName string) string {
	componentName = strings.TrimSpace(componentName)
	if componentName == "" {
		componentName = "component"
	}
	endpointName = strings.TrimSpace(endpointName)
	if endpointName == "" {
		endpointName = spec.DefaultEndpointName
	}
	return componentName + "-" + endpointName
}

// DesiredAPIConfigurationTraitWithIssuers returns the BFF-internal
// desired state for the `api-configuration` trait. When `enabled` is
// true, the trait is attached + jwtAuth is enabled in the per-env
// config with `issuers` pinned to the supplied list (empty ⇒ accept
// any cluster-configured keymanager). When `enabled` is false, the
// function returns nil + a tombstone entry to strip any previously-set
// config.
//
// `allowedOrigins` lists the SPA hostnames the gateway should
// echo on CORS preflight. Empty/nil falls back to the trait schema's
// default of `["*"]` (wildcard). Either way `allowedHeaders` stays `["*"]`
// and `allowCredentials` stays false:
//
//   - Bearer auth does NOT need `allowCredentials`. That flag governs
//     cookies, TLS client certs and browser-managed HTTP auth; an
//     `Authorization` header set by JS is an ordinary header and only has to
//     appear in `Access-Control-Allow-Headers`. Every SPA this platform
//     generates keeps its token in localStorage and the gateway strips
//     `authorization` before the upstream, so no cookie ever rides the API
//     call.
//   - Credentials-off is what makes the header wildcard legal — the WSO2
//     platform forbids `*` combined with credentials and answers such a
//     preflight with NO CORS headers at all, which browsers report as a
//     blanket CORS failure.
//   - `["*"]` rather than a fixed list because the gateway REFLECTS the
//     requested header names back. A generated client sending anything the
//     list did not anticipate (the gateway-injected `X-User-*` identity
//     headers, `If-Match`, `X-Request-Id`) otherwise gets the same
//     all-or-nothing blackout: the browser blocks the request before it is
//     ever sent, so nothing reaches the access log to diagnose.
//
// Origins stay pinned to the sibling SPA list — this widens headers, never
// the origin, and CORS constrains browsers only (curl was never gated).
//
// `configs` is keyed by trait instance name; the value is the parameters
// block that lands at `ReleaseBinding.spec.traitEnvironmentConfigs[<inst>]`.
// The shape (cors / jwtAuth) matches the trait's environmentConfigSchema.
//
// `endpointName` is the design.json-declared workload endpoint name the trait
// binds to (it must match a key in the component's workload.yaml
// `spec.endpoints`). An empty value defaults to spec.DefaultEndpointName
// ("http"). This is the SINGLE point that decides the bound endpoint name — it
// is no longer hardcoded, so a component whose workload names its endpoint
// something other than "http" still renders (previously deploy rendering failed
// with `workload.endpoints["http"]: no such key`).
func DesiredAPIConfigurationTraitWithIssuers(componentName, endpointName string, enabled bool, issuers []string, allowedOrigins []string) (traits []openchoreo.ComponentTrait, configs map[string]map[string]interface{}) {
	endpointName = strings.TrimSpace(endpointName)
	if endpointName == "" {
		endpointName = spec.DefaultEndpointName
	}
	inst := APIConfigurationInstanceName(componentName, endpointName)
	if !enabled {
		// Clear both: empty traits + empty config marks the instance for
		// removal in the OC client's merge logic.
		return nil, map[string]map[string]interface{}{
			inst: nil,
		}
	}
	traits = []openchoreo.ComponentTrait{{
		InstanceName: inst,
		Kind:         "ClusterTrait",
		Name:         "api-configuration",
		Parameters: map[string]interface{}{
			"endpointName": endpointName,
		},
	}}
	issuersIface := make([]interface{}, 0, len(issuers))
	for _, iss := range issuers {
		issuersIface = append(issuersIface, iss)
	}
	cors := map[string]interface{}{
		"enabled": true,
	}
	if len(allowedOrigins) > 0 {
		originsIface := make([]interface{}, 0, len(allowedOrigins))
		for _, o := range allowedOrigins {
			originsIface = append(originsIface, o)
		}
		cors["allowedOrigins"] = originsIface
		cors["allowedMethods"] = []interface{}{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
		cors["allowedHeaders"] = []interface{}{"*"}
		cors["allowCredentials"] = false
	}
	configs = map[string]map[string]interface{}{
		inst: {
			"cors": cors,
			"jwtAuth": map[string]interface{}{
				"enabled": true,
				// jwt-auth v1 accepts `issuers` + `audience` arrays. Empty
				// issuers means "no per-RestApi filter; trust any cluster-
				// configured keymanager". BYO-IDP orgs populate this from
				// the org's IDP profile so each protected API only trusts
				// its org's IDP.
				"issuers":  issuersIface,
				"audience": []interface{}{},
			},
		},
	}
	return traits, configs
}
