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
	"github.com/wso2/aep/aep-api/internal/identity"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/platform/validate"
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

// ProjectAudience is the token audience a project's managed APIs accept: the
// project's resource-server identifier, derived rather than read, so the
// deployment projection needs no directory round trip and stays pure.
//
// identity.ResourceServerIdentifier is the ONE authority for the string; this
// wrapper only decides when to ask for it. It PANICS on a handle that is not a
// slug — deliberately, since the result is a token audience nobody can correct
// afterwards — so a handle that has not been through the platform's boundary
// check yields no audience instead. An empty audience leaves the gateway's
// `aud` check off; it never widens one.
func ProjectAudience(orgHandle, projectHandle string) string {
	orgHandle = strings.TrimSpace(orgHandle)
	projectHandle = strings.TrimSpace(projectHandle)
	if validate.Slug(orgHandle) != nil || validate.Slug(projectHandle) != nil {
		return ""
	}
	return identity.ResourceServerIdentifier(orgHandle, projectHandle)
}

// APIConfigurationDesired is everything the `api-configuration` trait's two
// halves are rendered from. A struct rather than a parameter list because the
// halves land on different objects at different times (see DesiredDeployment)
// and a caller that gets one fact right and another wrong should read as a
// named field, not as the fourth positional bool.
type APIConfigurationDesired struct {
	// ComponentName is the k8s-shaped component name.
	ComponentName string
	// EndpointName is the design.json-declared workload endpoint the trait
	// binds to; empty defaults to spec.DefaultEndpointName.
	EndpointName string
	// Enabled is spec.ResolveAPISecurityEnabled — whether the managed API is
	// fronted by the gateway at all.
	Enabled bool
	// Issuers pins which IDP's tokens this API trusts; empty trusts any
	// cluster-configured keymanager.
	Issuers []string
	// Audience is the project's resource-server identifier — the `aud` every
	// token minted for this project carries. Empty leaves the audience
	// unchecked.
	Audience string
	// Operations is the projected operation table. Empty leaves the trait's own
	// default (six methods on `/*`) in place: one API-level jwt-auth and no
	// per-operation scope.
	Operations []Operation
}

// DesiredAPIConfigurationTrait returns the BFF-internal desired state for the
// `api-configuration` trait. When `Enabled` is true, the trait is attached +
// jwtAuth is enabled in the per-env config with `issuers` pinned to the supplied
// list (empty ⇒ accept any cluster-configured keymanager). When `Enabled` is
// false, the function returns nil + a tombstone entry to strip any
// previously-set config.
//
// CORS omits `allowedOrigins` so the trait schema default `["*"]` applies.
//
// `configs` is keyed by trait instance name; the value is the parameters
// block that lands at `ReleaseBinding.spec.traitEnvironmentConfigs[<inst>]`.
// The shape (cors / jwtAuth) matches the trait's environmentConfigSchema.
//
// The split between the two halves is the design's, not an accident of the
// code: the OPERATION names the scope it needs (a property of the component's
// contract — one value for every environment, so a trait PARAMETER), while the
// issuers and the audience name whose tokens count (a property of the
// environment, so a traitEnvironmentConfig). The trait template composes them.
//
// `EndpointName` is the design.json-declared workload endpoint name the trait
// binds to (it must match a key in the component's workload.yaml
// `spec.endpoints`). An empty value defaults to spec.DefaultEndpointName
// ("http"). This is the SINGLE point that decides the bound endpoint name: the
// value must match a key in the component's workload.yaml, or deploy rendering
// fails with `workload.endpoints[…]: no such key`.
func DesiredAPIConfigurationTrait(in APIConfigurationDesired) (traits []openchoreo.ComponentTrait, configs map[string]map[string]interface{}) {
	endpointName := strings.TrimSpace(in.EndpointName)
	if endpointName == "" {
		endpointName = spec.DefaultEndpointName
	}
	inst := APIConfigurationInstanceName(in.ComponentName, endpointName)
	if !in.Enabled {
		// Clear both: empty traits + empty config marks the instance for
		// removal in the OC client's merge logic.
		return nil, map[string]map[string]interface{}{
			inst: nil,
		}
	}
	parameters := map[string]interface{}{
		"endpointName": endpointName,
	}
	// The key is omitted, never emitted empty: an empty list is a RestApi that
	// declares no operation at all — every path 404s — while an absent one
	// takes the trait's `/*` default.
	if len(in.Operations) > 0 {
		parameters["operations"] = renderOperations(in.Operations)
	}
	traits = []openchoreo.ComponentTrait{{
		InstanceName: inst,
		Kind:         "ClusterTrait",
		Name:         "api-configuration",
		Parameters:   parameters,
	}}
	issuersIface := make([]interface{}, 0, len(in.Issuers))
	for _, iss := range in.Issuers {
		issuersIface = append(issuersIface, iss)
	}
	audience := make([]interface{}, 0, 1)
	if strings.TrimSpace(in.Audience) != "" {
		audience = append(audience, strings.TrimSpace(in.Audience))
	}
	cors := map[string]interface{}{
		"enabled": true,
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
				"issuers": issuersIface,
				// The project's resource-server identifier. It is the DEFAULT
				// for every operation on this API; the trait falls back to it
				// whenever an operation names no `audiences` of its own, which
				// is every operation the projection renders — one component
				// belongs to one project, so there is one audience to pin and
				// per-operation pinning would be the same string repeated on
				// every row. The per-operation override exists for a component
				// that must mix audiences; nothing projects one today.
				"audience": audience,
			},
		},
	}
	return traits, configs
}

// jwtAuthPolicyName / jwtAuthPolicyVersion are the RestApi CRD's spelling, not
// the policy's own semantic version: the CRD's `version` matches `^v\d+$`, so
// `v1` is the only legal spelling even though the jwt-auth policy is 1.3.0.
const (
	jwtAuthPolicyName    = "jwt-auth"
	jwtAuthPolicyVersion = "v1"
)

// renderOperations renders the operation table into the trait's `operations`
// parameter shape.
//
// Every key below is declared in the trait's parameter schema, and that is a
// hard requirement rather than tidiness: OpenChoreo PRUNES undeclared keys
// against the schema before the template runs, silently, so a key this function
// invents does not fail — it vanishes, and the operation is served without the
// policy it was supposed to carry (api_operations_test.go asserts the rendered
// keys against the trait yaml for exactly this reason).
func renderOperations(ops []Operation) []interface{} {
	out := make([]interface{}, 0, len(ops))
	for _, op := range ops {
		row := map[string]interface{}{
			"method": op.Method,
			"path":   op.Path,
		}
		switch op.Requirement.Kind {
		case RequirementPublic:
			// `public` suppresses jwt-auth here AND — because per-operation
			// policies only ADD to API-level ones — the API-level jwt-auth for
			// the whole API. No `policies` key: the trait omits the rendered
			// list for a public operation anyway, and an empty list here would
			// read as "policies were considered and none applied".
			row["public"] = true
		case RequirementScope:
			row["policies"] = []interface{}{jwtAuthPolicy(map[string]interface{}{
				// Exactly one handle, and `anyOf` rather than `allOf`: the
				// gateway compares scopes as WHOLE STRINGS (`claims:read-all`
				// does not imply `claims:read`), and the contract fixes one
				// handle per operation. `allOf` is left to the trait's schema
				// default — emitting an empty list would be a second way to
				// say the same nothing.
				"scopes": map[string]interface{}{
					"anyOf": []interface{}{op.Requirement.Scope},
				},
			})}
		default:
			// Signed-in, no particular permission: a jwt-auth policy with NO
			// `scopes` param. NEVER `anyOf: [openid]` — OIDC scopes ride every
			// access token the IDP issues, so that spelling admits every
			// signed-in account in the org while looking guarded.
			row["policies"] = []interface{}{jwtAuthPolicy(nil)}
		}
		out = append(out, row)
	}
	return out
}

// jwtAuthPolicy is one per-operation policy entry. `params` carries only what
// the OPERATION knows; the trait composes the issuers, the audience and the
// fixed claimMappings around it from the environment's config.
func jwtAuthPolicy(params map[string]interface{}) map[string]interface{} {
	policy := map[string]interface{}{
		"name":    jwtAuthPolicyName,
		"version": jwtAuthPolicyVersion,
	}
	if len(params) > 0 {
		policy["params"] = params
	}
	return policy
}
