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

// api_operations_test.go — the operation table and its rendering.
//
// Three of these tests are worth more than they look:
//
//   - the GOLDEN renders, because the reviewable artifact of this projection is
//     yaml a human compares against the contract it came from;
//   - the KEY test, because OpenChoreo prunes an undeclared parameter key
//     SILENTLY: a renamed key does not fail the deployment, it produces an
//     operation served without the policy it was supposed to carry. The trait
//     yaml is the schema, so the test reads the trait yaml rather than a copy
//     of it;
//   - the REFUSALS, because one operation the RestApi CRD rejects leaves every
//     path on that API 404 — policy-free siblings included — which is
//     indistinguishable from "never deployed".

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The three design use cases' contracts, and the trait whose schema the render
// must satisfy. internal/projects → internal → aep-api → services → repo root.
const (
	fixtureRoot = "../platform/securityspec/testdata"
	traitPath   = "../../../../deployments/manifests/api-platform/api-configuration-trait.yaml"
)

// useCases are the fixtures every cross-cutting assertion runs over: one spec
// per generated API in the three design use cases.
var useCases = []struct{ name, path string }{
	{"expense-tracker", "expense-tracker/expense-api.openapi.yaml"},
	{"clinic", "clinic/appointments-api.openapi.yaml"},
	{"vendor-orders", "vendor/orders-api.openapi.yaml"},
	{"vendor-payments", "vendor/payments-api.openapi.yaml"},
}

func fixtureSpec(t *testing.T, rel string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(fixtureRoot, rel))
	if err != nil {
		t.Fatalf("read fixture %s — layout drift?: %v", rel, err)
	}
	return body
}

func mustOperations(t *testing.T, body []byte) []Operation {
	t.Helper()
	ops, err := OperationsFromSpec(body)
	if err != nil {
		t.Fatalf("OperationsFromSpec: %v", err)
	}
	return ops
}

// -------------------------------------------------------------------------
// The table
// -------------------------------------------------------------------------

// TestOperationsFromSpec_ExpenseTracker is the shape the whole feature is read
// off: a public operation, a signed-in one, six scoped ones, and one
// synthesised OPTIONS per distinct path carrying its first sibling's
// requirement.
func TestOperationsFromSpec_ExpenseTracker(t *testing.T) {
	got := mustOperations(t, fixtureSpec(t, "expense-tracker/expense-api.openapi.yaml"))
	want := []Operation{
		{Method: "GET", Path: "/health", Requirement: Requirement{Kind: RequirementPublic}},
		{Method: "GET", Path: "/me", Requirement: Requirement{Kind: RequirementSignedIn}},
		{Method: "GET", Path: "/claims", Requirement: scopeOf("claims:read")},
		{Method: "POST", Path: "/claims", Requirement: scopeOf("claims:submit")},
		{Method: "POST", Path: "/claims/{claimId}/approve", Requirement: scopeOf("claims:approve")},
		{Method: "POST", Path: "/claims/{claimId}/reject", Requirement: scopeOf("claims:reject")},
		{Method: "GET", Path: "/reports", Requirement: scopeOf("reports:read")},
		{Method: "GET", Path: "/reports/export", Requirement: scopeOf("reports:export")},
		// Synthesised, appended after the declared rows, one per distinct path
		// in first-appearance order.
		{Method: "OPTIONS", Path: "/health", Requirement: Requirement{Kind: RequirementPublic}},
		{Method: "OPTIONS", Path: "/me", Requirement: Requirement{Kind: RequirementSignedIn}},
		{Method: "OPTIONS", Path: "/claims", Requirement: scopeOf("claims:read")},
		{Method: "OPTIONS", Path: "/claims/{claimId}/approve", Requirement: scopeOf("claims:approve")},
		{Method: "OPTIONS", Path: "/claims/{claimId}/reject", Requirement: scopeOf("claims:reject")},
		{Method: "OPTIONS", Path: "/reports", Requirement: scopeOf("reports:read")},
		{Method: "OPTIONS", Path: "/reports/export", Requirement: scopeOf("reports:export")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation table drift:\ngot  %+v\nwant %+v", got, want)
	}
}

func scopeOf(handle string) Requirement {
	return Requirement{Kind: RequirementScope, Scope: handle}
}

// TestOperationsFromSpec_EveryPathHasOptions — a CORS preflight to an
// undeclared method+path is answered 404 BEFORE any policy runs, so a missing
// OPTIONS row kills every cross-origin call to that path with no trace in the
// gateway's policy logs.
func TestOperationsFromSpec_EveryPathHasOptions(t *testing.T) {
	for _, useCase := range useCases {
		t.Run(useCase.name, func(t *testing.T) {
			ops := mustOperations(t, fixtureSpec(t, useCase.path))
			declared := map[string]bool{}
			preflight := map[string]bool{}
			for _, op := range ops {
				declared[op.Path] = true
				if op.Method == "OPTIONS" {
					preflight[op.Path] = true
				}
			}
			for path := range declared {
				if !preflight[path] {
					t.Errorf("path %s has no OPTIONS row", path)
				}
			}
			if len(declared) == 0 {
				t.Fatal("fixture produced no operations")
			}
		})
	}
}

// TestOperationsFromSpec_OptionsIsNotDuplicated — a contract that does declare
// OPTIONS keeps its own row and gets no synthesised twin.
func TestOperationsFromSpec_OptionsIsNotDuplicated(t *testing.T) {
	body := mutateSpec(t, "expense-tracker/expense-api.openapi.yaml",
		"  /me:\n    get:\n      operationId: me\n",
		"  /me:\n    options:\n      operationId: mePreflight\n      security: []\n"+
			"      responses:\n        \"204\": { description: ok }\n    get:\n      operationId: me\n")
	ops := mustOperations(t, body)
	var rows []Operation
	for _, op := range ops {
		if op.Path == "/me" && op.Method == "OPTIONS" {
			rows = append(rows, op)
		}
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly one OPTIONS /me row, got %d: %+v", len(rows), rows)
	}
	if rows[0].Requirement.Kind != RequirementPublic {
		t.Fatalf("the document's own OPTIONS row was overwritten: %+v", rows[0])
	}
}

// TestOperationsFromSpec_NeverEmitsAnOIDCScope — `scopes.anyOf: [openid]` admits
// every signed-in account in the org while looking guarded, so the class is
// refused at the document and never reachable by rendering.
func TestOperationsFromSpec_NeverEmitsAnOIDCScope(t *testing.T) {
	for _, useCase := range useCases {
		for _, op := range mustOperations(t, fixtureSpec(t, useCase.path)) {
			for _, reserved := range securityspec.OIDCScopes {
				if op.Requirement.Scope == reserved {
					t.Fatalf("%s: %s %s requires the OIDC scope %q",
						useCase.name, op.Method, op.Path, reserved)
				}
			}
		}
	}
}

// -------------------------------------------------------------------------
// Refusals
// -------------------------------------------------------------------------

// mutateSpec makes ONE literal substitution in a fixture that must match, so a
// fixture reword fails loudly instead of silently disarming the case.
func mutateSpec(t *testing.T, rel, from, to string) []byte {
	t.Helper()
	body := string(fixtureSpec(t, rel))
	if !strings.Contains(body, from) {
		t.Fatalf("fixture %s no longer contains %q", rel, from)
	}
	return []byte(strings.Replace(body, from, to, 1))
}

// TestOperationsFromSpec_RefusesWhatTheGateRefuses — the projection's refusal
// set is the gate's, minus the two rules that need security.json. It refuses
// rather than rendering a table it cannot vouch for.
func TestOperationsFromSpec_RefusesWhatTheGateRefuses(t *testing.T) {
	const expenseSpec = "expense-tracker/expense-api.openapi.yaml"
	cases := []struct {
		name, from, to, want string
	}{
		{
			name: "two scopes in one requirement",
			from: "security: [{ oauth2: [claims:read] }]",
			to:   "security: [{ oauth2: [claims:read, claims:submit] }]",
			want: "more than one scope",
		},
		{
			name: "two requirement objects",
			from: "security: [{ oauth2: [claims:read] }]",
			to:   "security: [{ oauth2: [claims:read] }, { oauth2: [claims:submit] }]",
			want: "more than one security requirement object",
		},
		{
			name: "an OIDC scope as a permission",
			from: "security: [{ oauth2: [claims:read] }]",
			to:   "security: [{ oauth2: [openid] }]",
			want: "is an OIDC scope",
		},
		{
			name: "an OIDC scope advertised in the flows",
			from: "            reports:export: Download CSV",
			to:   "            reports:export: Download CSV\n            profile: Who am I",
			want: "is an OIDC scope",
		},
		{
			name: "a scheme the document does not declare",
			from: "security: [{ oauth2: [claims:read] }]",
			to:   "security: [{ basicAuth: [] }]",
			want: "which this document does not declare",
		},
		{
			name: "security that is not a list",
			from: "security: [{ oauth2: [claims:read] }]",
			to:   "security: { oauth2: [claims:read] }",
			want: "is not a list",
		},
		{
			name: "no oauth2 scheme",
			from: "    oauth2:\n      type: oauth2",
			to:   "    oauth2x:\n      type: oauth2",
			want: "components.securitySchemes.oauth2 is absent",
		},
		{
			name: "a scheme that is not oauth2",
			from: "      type: oauth2\n",
			to:   "      type: http\n",
			want: "is type http, want oauth2",
		},
		{
			name: "no document-level default",
			from: "\nsecurity:\n  - oauth2: []\n",
			to:   "\n",
			want: "declares no `security: [{oauth2: []}]` default",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OperationsFromSpec(mutateSpec(t, expenseSpec, tc.from, tc.to))
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal does not carry %q:\n%s", tc.want, err)
			}
		})
	}
}

// TestOperationsFromSpec_RefusesWhatTheGatewayCannotBeTold — the two rules that
// are the projection's own: a method nobody has measured the CRD accepting, and
// a path template this file cannot vouch for. Both cost the WHOLE API, not the
// operation.
func TestOperationsFromSpec_RefusesWhatTheGatewayCannotBeTold(t *testing.T) {
	const expenseSpec = "expense-tracker/expense-api.openapi.yaml"
	cases := []struct {
		name, from, to, want string
	}{
		{
			name: "HEAD is not a measured method",
			from: "  /me:\n    get:",
			to:   "  /me:\n    head:",
			want: "measured only for GET, POST, PUT, PATCH, DELETE and OPTIONS",
		},
		{
			name: "TRACE is not a measured method",
			from: "  /me:\n    get:",
			to:   "  /me:\n    trace:",
			want: "measured only for GET, POST, PUT, PATCH, DELETE and OPTIONS",
		},
		{
			name: "a wildcard the contract never asked for",
			from: "  /reports/export:",
			to:   "  /reports/*:",
			want: "neither a literal nor a single `{param}` placeholder",
		},
		{
			name: "a relative path",
			from: "  /reports/export:",
			to:   "  reports/export:",
			want: "an OpenAPI path is absolute",
		},
		{
			name: "an empty segment",
			from: "  /reports/export:",
			to:   "  /reports//export:",
			want: "empty segment",
		},
		{
			name: "a query string in the template",
			from: "  /reports/export:",
			to:   "  /reports/export?format=csv:",
			want: "no query, fragment or whitespace",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OperationsFromSpec(mutateSpec(t, expenseSpec, tc.from, tc.to))
			if err == nil {
				t.Fatal("want a refusal, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal does not carry %q:\n%s", tc.want, err)
			}
		})
	}
}

// TestOperationsFromSpec_RefusesAnEmptyOrUnreadableSpec — the two inputs a
// component gets when its contract was never written: nothing to project, and
// the caller must leave the trait's `/*` default alone rather than render an
// empty operation table (which would 404 every path).
func TestOperationsFromSpec_RefusesAnEmptyOrUnreadableSpec(t *testing.T) {
	for _, body := range []string{"", "   ", "not: an: openapi document"} {
		if _, err := OperationsFromSpec([]byte(body)); err == nil {
			t.Fatalf("want a refusal for %q, got none", body)
		}
	}
	// A well-formed protected document with no paths at all.
	noPaths := mutateSpec(t, "expense-tracker/expense-api.openapi.yaml", "paths:\n  /health:", "unused:\n  /health:")
	if _, err := OperationsFromSpec(noPaths); err == nil {
		t.Fatal("want a refusal for a document with no operations, got none")
	}
}

// -------------------------------------------------------------------------
// The render
// -------------------------------------------------------------------------

// renderedOperations is the `operations` trait parameter for one fixture.
func renderedOperations(t *testing.T, rel string) []interface{} {
	t.Helper()
	traits, _ := DesiredAPIConfigurationTrait(APIConfigurationDesired{
		ComponentName: "api",
		Enabled:       true,
		Operations:    mustOperations(t, fixtureSpec(t, rel)),
	})
	if len(traits) != 1 {
		t.Fatalf("want 1 trait, got %d", len(traits))
	}
	ops, ok := traits[0].Parameters["operations"].([]interface{})
	if !ok {
		t.Fatalf("operations parameter missing or wrong type: %#v", traits[0].Parameters["operations"])
	}
	return ops
}

// TestRenderedOperations_Golden — the reviewable artifact. A change here is a
// change to what the gateway is told, so it is reviewed as yaml against
// deployments/manifests/api-platform/examples/restapi-expense-api.yaml rather
// than inferred from assertions.
func TestRenderedOperations_Golden(t *testing.T) {
	for _, useCase := range useCases {
		t.Run(useCase.name, func(t *testing.T) {
			encoded, err := yaml.Marshal(map[string]interface{}{
				"operations": renderedOperations(t, useCase.path),
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			golden := filepath.Join("testdata", "operations-"+useCase.name+".golden.yaml")
			if os.Getenv("UPDATE_GOLDEN") != "" {
				if err := os.WriteFile(golden, encoded, 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (re-run with UPDATE_GOLDEN=1 to create it): %v", err)
			}
			if string(encoded) != string(want) {
				t.Fatalf("rendered operations drift for %s:\n--- got ---\n%s\n--- want ---\n%s",
					useCase.name, encoded, want)
			}
		})
	}
}

// TestRenderedOperations_PolicyShape — the three requirement kinds, spelled the
// only way the trait composes them.
func TestRenderedOperations_PolicyShape(t *testing.T) {
	rows := renderedOperations(t, "expense-tracker/expense-api.openapi.yaml")
	byKey := map[string]map[string]interface{}{}
	for _, raw := range rows {
		row := raw.(map[string]interface{})
		byKey[row["method"].(string)+" "+row["path"].(string)] = row
	}

	// Public: `public: true` and NO policies key — a policies list would be
	// composed by the trait and make the operation demand a token.
	health := byKey["GET /health"]
	if health["public"] != true {
		t.Errorf("GET /health public = %v, want true", health["public"])
	}
	if _, ok := health["policies"]; ok {
		t.Errorf("GET /health carries policies: %#v", health["policies"])
	}

	// Signed-in: a jwt-auth policy with NO scopes — never `anyOf: [openid]`.
	me := onePolicy(t, byKey["GET /me"])
	if me["name"] != "jwt-auth" || me["version"] != "v1" {
		t.Errorf("GET /me policy = %#v, want jwt-auth v1", me)
	}
	if params, ok := me["params"]; ok {
		t.Errorf("GET /me carries params, want none (the trait's omission IS signed-in): %#v", params)
	}
	if _, ok := byKey["GET /me"]["public"]; ok {
		t.Error("GET /me carries a public flag")
	}

	// Scoped: exactly one handle, in anyOf.
	claims := onePolicy(t, byKey["GET /claims"])
	params, ok := claims["params"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET /claims params missing: %#v", claims)
	}
	scopes, ok := params["scopes"].(map[string]interface{})
	if !ok {
		t.Fatalf("GET /claims scopes missing: %#v", params)
	}
	if want := []interface{}{"claims:read"}; !reflect.DeepEqual(scopes["anyOf"], want) {
		t.Errorf("GET /claims anyOf = %#v, want %#v", scopes["anyOf"], want)
	}
	if _, ok := scopes["allOf"]; ok {
		t.Errorf("GET /claims emits allOf: %#v", scopes["allOf"])
	}
	// No per-operation audience: the environment's `jwtAuth.audience` is the
	// project's resource server, and repeating it on every row would be one
	// more copy of the same fact to keep in sync.
	if _, ok := params["audiences"]; ok {
		t.Errorf("GET /claims pins an audience per operation: %#v", params["audiences"])
	}
}

func onePolicy(t *testing.T, row map[string]interface{}) map[string]interface{} {
	t.Helper()
	policies, ok := row["policies"].([]interface{})
	if !ok || len(policies) != 1 {
		t.Fatalf("want exactly one policy, got %#v", row["policies"])
	}
	policy, ok := policies[0].(map[string]interface{})
	if !ok {
		t.Fatalf("policy is not a mapping: %#v", policies[0])
	}
	return policy
}

// TestRenderedOperations_KeysAreDeclaredByTheTrait — OpenChoreo PRUNES an
// undeclared parameter key against the trait's schema before the template runs,
// silently. A key this projection invents therefore does not fail the
// deployment: it vanishes, and the operation is served without the policy it
// was supposed to carry. So the rendered keys are asserted against the TRAIT
// YAML itself, not against a copy of it in this test.
func TestRenderedOperations_KeysAreDeclaredByTheTrait(t *testing.T) {
	schema := traitOperationsSchema(t)
	for _, useCase := range useCases {
		t.Run(useCase.name, func(t *testing.T) {
			for _, row := range renderedOperations(t, useCase.path) {
				assertKeysDeclared(t, "operations[]", row, schema)
			}
		})
	}
}

// traitOperationsSchema is the `operations` items schema from the ClusterTrait.
func traitOperationsSchema(t *testing.T) map[string]interface{} {
	t.Helper()
	body, err := os.ReadFile(traitPath)
	if err != nil {
		t.Fatalf("read the trait (layout drift?): %v", err)
	}
	var trait map[string]interface{}
	if err := yaml.Unmarshal(body, &trait); err != nil {
		t.Fatalf("the trait does not parse: %v", err)
	}
	node := trait
	for _, key := range []string{"spec", "parameters", "openAPIV3Schema", "properties", "operations", "items"} {
		next, ok := node[key].(map[string]interface{})
		if !ok {
			t.Fatalf("the trait has no spec.parameters…%s — the projection is rendering against a schema "+
				"that no longer exists", key)
		}
		node = next
	}
	return node
}

// assertKeysDeclared walks a rendered value against the trait's schema, failing
// on the first key the schema does not declare.
func assertKeysDeclared(t *testing.T, where string, value interface{}, schema map[string]interface{}) {
	t.Helper()
	switch typed := value.(type) {
	case map[string]interface{}:
		properties, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: rendered a mapping where the trait declares none (%v)", where, keysOf(typed))
		}
		for _, key := range keysOf(typed) {
			declared, ok := properties[key].(map[string]interface{})
			if !ok {
				t.Fatalf("%s.%s is not declared by the trait — OpenChoreo would PRUNE it silently "+
					"and serve the operation without it. Declared: %v", where, key, keysOf(properties))
			}
			assertKeysDeclared(t, where+"."+key, typed[key], declared)
		}
	case []interface{}:
		items, ok := schema["items"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: rendered a list where the trait declares none", where)
		}
		for i, entry := range typed {
			assertKeysDeclared(t, fmt.Sprintf("%s[%d]", where, i), entry, items)
		}
	}
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// TestRenderedOperations_PolicyVersionIsTheCRDSpelling — the RestApi CRD's
// `version` matches ^v\d+$, so `v1` is the only legal spelling even though the
// jwt-auth policy's own semantic version is 1.3.0. A rejected version leaves
// the whole API unserved.
func TestRenderedOperations_PolicyVersionIsTheCRDSpelling(t *testing.T) {
	for _, useCase := range useCases {
		for _, raw := range renderedOperations(t, useCase.path) {
			row := raw.(map[string]interface{})
			policies, ok := row["policies"].([]interface{})
			if !ok {
				continue
			}
			for _, entry := range policies {
				policy := entry.(map[string]interface{})
				if policy["version"] != "v1" {
					t.Fatalf("%s: %v %v policy version = %v, want v1",
						useCase.name, row["method"], row["path"], policy["version"])
				}
			}
		}
	}
}

// -------------------------------------------------------------------------
// Through the deployment projection
// -------------------------------------------------------------------------

const testAudience = "https://aep.wso2.com/orgs/acme/projects/expense-tracker"

// apiConfigurationParameters picks the api-configuration trait out of the
// desired shape — a service component also carries the auto-RCA alert-rule
// trait, and the two ride the same list.
func apiConfigurationParameters(t *testing.T, desired DesiredDeployment) map[string]interface{} {
	t.Helper()
	for _, trait := range desired.Traits {
		if trait.Name == "api-configuration" {
			return trait.Parameters
		}
	}
	t.Fatalf("no api-configuration trait in %+v", desired.Traits)
	return nil
}

func protectedComponent(t *testing.T) spec.DesignComponent {
	t.Helper()
	return spec.DesignComponent{
		Name:          "expense-api",
		ComponentType: "service",
		ExposesAPI:    &spec.ExposesAPI{Auth: "end-user-required", Managed: true},
		OpenAPISpec:   string(fixtureSpec(t, "expense-tracker/expense-api.openapi.yaml")),
	}
}

// TestDesiredDeploymentFor_ProjectsOperationsAndAudience — the whole wiring:
// the contract becomes the trait's operation table (the shape, one value for
// every environment) and the resource-server identifier becomes the
// environment's accepted audience.
func TestDesiredDeploymentFor_ProjectsOperationsAndAudience(t *testing.T) {
	desired := DesiredDeploymentFor(DeploymentInputs{
		Component:     protectedComponent(t),
		ComponentName: "expense-api",
		Environment:   "default",
		Audience:      testAudience,
	})
	if desired.APIOperationsProblem != "" {
		t.Fatalf("the fixture contract was refused: %s", desired.APIOperationsProblem)
	}
	parameters := apiConfigurationParameters(t, desired)
	ops, ok := parameters["operations"].([]interface{})
	if !ok || len(ops) != 15 {
		t.Fatalf("operations parameter = %#v, want 15 rows", parameters["operations"])
	}
	config := desired.Binding.TraitEnvironmentConfigs["expense-api-http"]
	jwt, ok := config["jwtAuth"].(map[string]interface{})
	if !ok {
		t.Fatalf("jwtAuth missing: %#v", config)
	}
	if want := []interface{}{testAudience}; !reflect.DeepEqual(jwt["audience"], want) {
		t.Fatalf("jwtAuth.audience = %#v, want %#v", jwt["audience"], want)
	}
}

// TestDesiredDeploymentFor_NoSignInKeepsTheDefault — a component with no
// sign-in dependency keeps the trait's `/*` × six methods default and pins no
// audience. This is the pre-scopes behaviour, unchanged, and it is what every
// `service-required` API still runs on.
func TestDesiredDeploymentFor_NoSignInKeepsTheDefault(t *testing.T) {
	component := protectedComponent(t)
	component.ExposesAPI = &spec.ExposesAPI{Auth: "service-required", Managed: true}

	desired := DesiredDeploymentFor(DeploymentInputs{
		Component:     component,
		ComponentName: "expense-api",
		Environment:   "default",
		Audience:      testAudience,
	})
	if ops, ok := apiConfigurationParameters(t, desired)["operations"]; ok {
		t.Fatalf("a component with no end-user sign-in got an operation table: %#v", ops)
	}
	if desired.APIOperationsProblem != "" {
		t.Fatalf("no projection was attempted, so there is no problem to report: %s", desired.APIOperationsProblem)
	}
	config := desired.Binding.TraitEnvironmentConfigs["expense-api-http"]
	jwt := config["jwtAuth"].(map[string]interface{})
	if aud := jwt["audience"].([]interface{}); len(aud) != 0 {
		t.Fatalf("jwtAuth.audience = %#v, want empty for a component with no end-user sign-in", aud)
	}
}

// TestDesiredDeploymentFor_UnreadableContractFallsBackLoudly — a protected
// component whose contract cannot be projected keeps the `/*` default (every
// operation needs a token, none needs a scope) and SAYS why. Silence here would
// present as a 401 on a health check with nothing in any log.
func TestDesiredDeploymentFor_UnreadableContractFallsBackLoudly(t *testing.T) {
	component := protectedComponent(t)
	component.OpenAPISpec = ""

	desired := DesiredDeploymentFor(DeploymentInputs{
		Component:     component,
		ComponentName: "expense-api",
		Environment:   "default",
		Audience:      testAudience,
	})
	if _, ok := apiConfigurationParameters(t, desired)["operations"]; ok {
		t.Fatal("an unreadable contract still rendered an operation table")
	}
	if desired.APIOperationsProblem == "" {
		t.Fatal("an unreadable contract was swallowed silently")
	}
	// The audience is a fact about the TOKEN, not about the rows: it is still
	// pinned, so a token minted for another resource server is still rejected.
	jwt := desired.Binding.TraitEnvironmentConfigs["expense-api-http"]["jwtAuth"].(map[string]interface{})
	if want := []interface{}{testAudience}; !reflect.DeepEqual(jwt["audience"], want) {
		t.Fatalf("jwtAuth.audience = %#v, want %#v", jwt["audience"], want)
	}
}

// TestProjectAudience — deterministic from the two handles, and no audience at
// all for a handle that never went through the platform's boundary check (the
// identifier is a token audience nobody can correct afterwards).
func TestProjectAudience(t *testing.T) {
	if got := ProjectAudience("acme", "expense-tracker"); got != testAudience {
		t.Fatalf("ProjectAudience = %q, want %q", got, testAudience)
	}
	for _, tc := range [][2]string{{"", "p"}, {"o", ""}, {"Acme Inc", "p"}, {"o", "a/b"}} {
		if got := ProjectAudience(tc[0], tc[1]); got != "" {
			t.Fatalf("ProjectAudience(%q, %q) = %q, want no audience", tc[0], tc[1], got)
		}
	}
}
