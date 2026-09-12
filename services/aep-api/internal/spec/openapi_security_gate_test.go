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

package spec

// openapi_security_gate_test.go — the platform half of the openapi.yaml security
// gate's proof, ported case for case from
// packages/agent-stream/test/openapi-security-gate.test.ts.
//
// It reads the AGENT'S OWN FIXTURES over the module boundary rather than
// vendoring them, for the reason the message table is vendored and diffed: the
// design's promise is that a document one gate accepts the other accepts, and a
// copied fixture is a promise that decays silently. The positive case is the
// Expense API spec hand-written for the P6 spike and served live behind the API
// Platform gateway with per-operation scope policies; every negative is one
// mutation of it, so a rule that starts rejecting real specs shows up as the
// positive failing rather than as a live 401.

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
)

// The agent package's fixtures and published message table. spec → internal →
// aep-api → services → repo root.
const (
	agentSpecFixture     = "../../../../packages/agent-stream/test/fixtures/openapi/expense-api.yaml"
	agentDesignFixture   = "../../../../packages/agent-stream/test/fixtures/openapi/expense-api.design.json"
	agentCatalogFixture  = "../../../../packages/agent-stream/test/fixtures/security/expense-tracker.json"
	agentOpenapiMessages = "../../../../packages/agent-stream/src/openapi-security-messages.json"
)

const (
	gateSpecPath     = "specs/design/components/expense-api/openapi.yaml"
	gateDesignPath   = "specs/design/components/expense-api/design.json"
	gateCatalogPath  = "specs/design/security.json"
	gateComponentDir = "components/expense-api/"
)

// specFileSource is a bundle addressed by REPO-RELATIVE path, the vocabulary
// securityspec.FileSource reads in.
type specFileSource map[string]string

func (s specFileSource) Read(path string) (string, bool) {
	content, ok := s[path]
	return content, ok
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read agent fixture %s — layout drift?: %v", path, err)
	}
	return string(content)
}

// p6Spec is the positive document every negative below mutates.
func p6Spec(t *testing.T) string { return readFixture(t, agentSpecFixture) }

// protectedDesign carries the sign-in dependency AND the exposesAPI.auth stamp
// derive_auth.go writes from it — which is the signal this gate reads.
func protectedDesign(t *testing.T) string { return readFixture(t, agentDesignFixture) }

// unprotectedDesign is the same component with no sign-in: every operation is
// public.
const unprotectedDesign = `{"name":"expense-api","type":"service","version":"0.1.0","dependencies":[]}`

func catalog(t *testing.T) string { return readFixture(t, agentCatalogFixture) }

// catalogWithForeignResource is the catalog plus a `notifications` resource
// another component owns.
func catalogWithForeignResource(t *testing.T) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(catalog(t)), &doc); err != nil {
		t.Fatalf("agent catalog fixture does not parse: %v", err)
	}
	permissions, ok := doc["permissions"].([]any)
	if !ok {
		t.Fatalf("agent catalog fixture has no permissions list")
	}
	doc["permissions"] = append(permissions, map[string]any{
		"resource":  "notifications",
		"component": "notify-api",
		"actions": []any{map[string]any{
			"handle": "send", "ownership": "any", "description": "Send a notification",
		}},
	})
	encoded, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("re-encode catalog: %v", err)
	}
	return string(encoded)
}

// gate runs the security gate over spec with the given siblings in the bundle.
func gate(t *testing.T, spec string, files map[string]string) string {
	t.Helper()
	return checkOpenapiSecurity(gateSpecPath, spec, specFileSource(files))
}

// fullBundle is the default: the component is protected and the catalog is present.
func fullBundle(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{gateDesignPath: protectedDesign(t), gateCatalogPath: catalog(t)}
}

// mutate makes ONE literal substitution that must match, so a fixture reword
// fails loudly instead of silently disarming the case.
func mutate(t *testing.T, spec, from, to string) string {
	t.Helper()
	if !strings.Contains(spec, from) {
		t.Fatalf("fixture no longer contains %q", from)
	}
	return strings.Replace(spec, from, to, 1)
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("message does not carry %q:\n%s", want, got)
	}
}

// -------------------------------------------------------------------------
// The positive
// -------------------------------------------------------------------------

func TestOpenapiSecurity_P6SpecPasses(t *testing.T) {
	if problem := gate(t, p6Spec(t), fullBundle(t)); problem != "" {
		t.Fatalf("the P6 expense-api spec must pass: %s", problem)
	}
}

func TestOpenapiSecurity_CatalogAbsentNarrowsRatherThanBlocks(t *testing.T) {
	// The design lineup writes security.json before the per-component
	// artifacts, but a re-emitted spec must not be refused for a file that is
	// not there yet.
	files := map[string]string{gateDesignPath: protectedDesign(t)}
	if problem := gate(t, p6Spec(t), files); problem != "" {
		t.Fatalf("with no catalog the spec must still pass: %s", problem)
	}
}

func TestOpenapiSecurity_CatalogAbsentKeepsStructuralRules(t *testing.T) {
	files := map[string]string{gateDesignPath: protectedDesign(t)}
	stale := mutate(t, p6Spec(t), "- oauth2: [claims:approve]", "- oauth2: [claims:archive]")
	if problem := gate(t, stale, files); problem != "" {
		t.Fatalf("a stale handle is unknowable with no catalog: %s", problem)
	}
	twoScopes := mutate(t, p6Spec(t), "- oauth2: [claims:submit]", "- oauth2: [claims:submit, claims:read]")
	assertContains(t, gate(t, twoScopes, files), "more than one scope")
}

func TestOpenapiSecurity_DesignAbsentIsNoVerdict(t *testing.T) {
	// Also the back-compatibility of every structural case: a bundle holding
	// only the spec gets exactly the gate it had before.
	noScheme := mutate(t, p6Spec(t), "security:\n  - oauth2: []\n", "")
	if problem := gate(t, noScheme, map[string]string{gateCatalogPath: catalog(t)}); problem != "" {
		t.Fatalf("design.json absent means the premise is unknowable: %s", problem)
	}
	if problem := gate(t, noScheme, map[string]string{}); problem != "" {
		t.Fatalf("an empty bundle yields no security verdict: %s", problem)
	}
}

func TestOpenapiSecurity_NonComponentPathIsNotJudged(t *testing.T) {
	// A dependency's spec is somebody else's API: the platform relays it, it
	// does not hold it to the conventions of a spec it generated.
	dependency := "specs/design/dependencies/billing/openapi.yaml"
	if problem := checkOpenapiSecurity(dependency, p6Spec(t), specFileSource(fullBundle(t))); problem != "" {
		t.Fatalf("a dependency spec must not be judged: %s", problem)
	}
}

// -------------------------------------------------------------------------
// The scheme and the document default
// -------------------------------------------------------------------------

func TestOpenapiSecurity_MissingOAuth2Scheme(t *testing.T) {
	noScheme := mutate(t, p6Spec(t),
		"  securitySchemes:\n    oauth2:\n      type: oauth2",
		"  securitySchemes: {}\n  _removed:\n    oauth2:\n      type: oauth2")
	problem := gate(t, noScheme, fullBundle(t))
	assertContains(t, problem, "components.securitySchemes.oauth2 is absent")
	assertContains(t, problem, "one that does not declares none")
}

func TestOpenapiSecurity_SchemeWrongType(t *testing.T) {
	wrong := mutate(t, p6Spec(t), "    oauth2:\n      type: oauth2", "    oauth2:\n      type: http")
	assertContains(t, gate(t, wrong, fullBundle(t)), "declared as type: http")
}

func TestOpenapiSecurity_MissingDocumentSecurity(t *testing.T) {
	noDefault := mutate(t, p6Spec(t), "\nsecurity:\n  - oauth2: []\n", "\n")
	problem := gate(t, noDefault, fullBundle(t))
	assertContains(t, problem, "no document-level default")
	// The literal brace pair the model must write survives the slot formatter.
	assertContains(t, problem, "security: [{oauth2: []}]")
}

// -------------------------------------------------------------------------
// A component with no sign-in declares nothing
// -------------------------------------------------------------------------

// bareSpec is the smallest protected-shaped document, for the no-dependency cases.
const bareSpec = `openapi: 3.0.3
info:
  title: Expense API
  version: 1.0.0
paths:
  /claims:
    get:
      operationId: listClaims
      responses:
        "200":
          description: ok
`

func TestOpenapiSecurity_SchemeWithoutDependency(t *testing.T) {
	files := map[string]string{gateDesignPath: unprotectedDesign, gateCatalogPath: catalog(t)}
	assertContains(t, gate(t, p6Spec(t), files), "remove components.securitySchemes")
}

func TestOpenapiSecurity_DocumentSecurityWithoutDependency(t *testing.T) {
	withDefault := strings.Replace(bareSpec, "paths:", "security:\n  - oauth2: []\npaths:", 1)
	files := map[string]string{gateDesignPath: unprotectedDesign}
	assertContains(t, gate(t, withDefault, files), "remove the document-level `security` block")
}

func TestOpenapiSecurity_OperationSecurityWithoutDependency(t *testing.T) {
	withOp := strings.Replace(bareSpec,
		"      operationId: listClaims",
		"      operationId: listClaims\n      security: []", 1)
	files := map[string]string{gateDesignPath: unprotectedDesign}
	assertContains(t, gate(t, withOp, files), "GET /claims must declare no security")
}

// -------------------------------------------------------------------------
// One requirement object, one scope (decision B1)
// -------------------------------------------------------------------------

func TestOpenapiSecurity_OperationMultipleRequirements(t *testing.T) {
	anyOf := mutate(t, p6Spec(t),
		"        - oauth2: [claims:approve]",
		"        - oauth2: [claims:approve]\n        - oauth2: [claims:reject]")
	problem := gate(t, anyOf, fullBundle(t))
	assertContains(t, problem, "more than one security requirement object")
	assertContains(t, problem, "at most one requirement object with at most one scope")
}

func TestOpenapiSecurity_OperationMultipleScopes(t *testing.T) {
	two := mutate(t, p6Spec(t), "- oauth2: [claims:submit]", "- oauth2: [claims:submit, claims:read]")
	assertContains(t, gate(t, two, fullBundle(t)), "POST /claims names more than one scope")
}

// Two schemes in ONE requirement object mean "both", which the gateway cannot
// express — the same disagreement as two objects, and the same refusal.
func TestOpenapiSecurity_TwoSchemesInOneRequirement(t *testing.T) {
	both := mutate(t, p6Spec(t),
		"        - oauth2: [claims:approve]",
		"        - oauth2: [claims:approve]\n          bearerAuth: []")
	assertContains(t, gate(t, both, fullBundle(t)), "more than one security requirement object")
}

func TestOpenapiSecurity_OperationUnknownScheme(t *testing.T) {
	other := mutate(t, p6Spec(t), "- oauth2: [claims:approve]", "- bearerAuth: [claims:approve]")
	assertContains(t, gate(t, other, fullBundle(t)), "secured with the scheme `bearerAuth`")
}

func TestOpenapiSecurity_OperationSecurityNotAList(t *testing.T) {
	scalar := mutate(t, p6Spec(t),
		"      security:\n        - oauth2: [claims:approve]",
		"      security: oauth2")
	assertContains(t, gate(t, scalar, fullBundle(t)), "is not a list")
}

func TestOpenapiSecurity_ExplicitDocumentDefaultAccepted(t *testing.T) {
	// "at most one requirement object with at most one scope" — zero scopes
	// means exactly what inheriting means, and the gateway renders the same
	// policy.
	explicit := mutate(t, p6Spec(t), "- oauth2: [claims:approve]", "- oauth2: []")
	if problem := gate(t, explicit, fullBundle(t)); problem != "" {
		t.Fatalf("an operation spelling the document default out is legal: %s", problem)
	}
}

// -------------------------------------------------------------------------
// Every scope is a catalog handle this component owns
// -------------------------------------------------------------------------

func TestOpenapiSecurity_ScopeNotInCatalog(t *testing.T) {
	stale := mutate(t, p6Spec(t), "- oauth2: [claims:approve]", "- oauth2: [claims:archive]")
	problem := gate(t, stale, fullBundle(t))
	assertContains(t, problem, "requires the scope `claims:archive`")
	assertContains(t, problem, "reference catalog handles; they never define them")
}

func TestOpenapiSecurity_ScopeNotOwned(t *testing.T) {
	foreign := mutate(t, p6Spec(t), "- oauth2: [claims:approve]", "- oauth2: [notifications:send]")
	files := map[string]string{
		gateDesignPath: protectedDesign(t), gateCatalogPath: catalogWithForeignResource(t),
	}
	assertContains(t, gate(t, foreign, files), "the catalog assigns to component `notify-api`")
}

func TestOpenapiSecurity_FlowScopeNotInCatalog(t *testing.T) {
	bogus := mutate(t, p6Spec(t),
		"            reports:read: Monthly totals",
		"            reports:read: Monthly totals\n            claims:archive: Archive a claim")
	assertContains(t, gate(t, bogus, fullBundle(t)), "advertises the scope `claims:archive` in flows")
}

func TestOpenapiSecurity_FlowScopeNotOwned(t *testing.T) {
	foreign := mutate(t, p6Spec(t),
		"            reports:read: Monthly totals",
		"            reports:read: Monthly totals\n            notifications:send: Send a notification")
	files := map[string]string{
		gateDesignPath: protectedDesign(t), gateCatalogPath: catalogWithForeignResource(t),
	}
	assertContains(t, gate(t, foreign, files),
		"`notifications:send` in flows, whose resource the catalog assigns to component `notify-api`")
}

// -------------------------------------------------------------------------
// The OIDC scopes ride every token — refused anywhere (Δ P2-live-2 §2c)
// -------------------------------------------------------------------------

func TestOpenapiSecurity_ReservedScopeOnOperation(t *testing.T) {
	wideOpen := mutate(t, p6Spec(t), "- oauth2: [claims:approve]", "- oauth2: [openid]")
	problem := gate(t, wideOpen, fullBundle(t))
	assertContains(t, problem, "`openid` is an OIDC scope, not a permission handle")
	assertContains(t, problem, "the security of POST /claims/{claimId}/approve")
}

func TestOpenapiSecurity_ReservedScopeInFlows(t *testing.T) {
	inFlows := mutate(t, p6Spec(t),
		"            reports:read: Monthly totals",
		"            reports:read: Monthly totals\n            group: Groups the user is in")
	problem := gate(t, inFlows, fullBundle(t))
	assertContains(t, problem, "`group` is an OIDC scope")
	assertContains(t, problem, "flows.authorizationCode.scopes")
}

func TestOpenapiSecurity_EveryReservedScopeRefused(t *testing.T) {
	for _, scope := range securityspec.OIDCScopes {
		spec := mutate(t, p6Spec(t), "- oauth2: [claims:approve]", "- oauth2: ["+scope+"]")
		assertContains(t, gate(t, spec, fullBundle(t)), "`"+scope+"` is an OIDC scope")
	}
}

// -------------------------------------------------------------------------
// The injected identity header (Δ P6 §7.2, P2-live-1 a)
// -------------------------------------------------------------------------

func TestOpenapiSecurity_IdentityHeaderRequired(t *testing.T) {
	required := mutate(t, p6Spec(t),
		"    UserId:\n      name: X-User-Id\n      in: header\n      required: false",
		"    UserId:\n      name: X-User-Id\n      in: header\n      required: true")
	problem := gate(t, required, fullBundle(t))
	assertContains(t, problem, "declares the header parameter X-User-Id as `required: true`")
	assertContains(t, problem, "answered 400 by the parameter binder instead of 401")
}

func TestOpenapiSecurity_IdentityHeaderAnywhere(t *testing.T) {
	inline := mutate(t, p6Spec(t),
		"  /reports/monthly:\n    parameters:\n      - $ref: '#/components/parameters/UserId'",
		"  /reports/monthly:\n    parameters:\n      - $ref: '#/components/parameters/UserId'\n"+
			"      - name: X-User-Scopes\n        in: header\n        required: true\n        schema: { type: string }")
	assertContains(t, gate(t, inline, fullBundle(t)),
		"GET /reports/monthly declares the header parameter X-User-Scopes")
}

func TestOpenapiSecurity_PublicOperationDeclaresIdentityHeader(t *testing.T) {
	leaky := mutate(t, p6Spec(t),
		"  /health:\n    get:",
		"  /health:\n    parameters:\n      - $ref: '#/components/parameters/UserId'\n    get:")
	problem := gate(t, leaky, fullBundle(t))
	assertContains(t, problem, "GET /health is public (`security: []`) and declares the header parameter X-User-Id")
	assertContains(t, problem, "does not strip inbound x-user-* headers")
}

// -------------------------------------------------------------------------
// Rule ORDER — the half a per-rule test cannot prove
// -------------------------------------------------------------------------

// A document with TWO defects must name the same first violation as the agent's
// gate, or the model fixing one refusal meets a different one from the other
// side and the two gates read as two rule sets.
func TestOpenapiSecurity_FirstViolationOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(string) string
		wantHas string
	}{
		{
			// The scheme is judged before any operation.
			name: "the scheme outranks a stale operation handle",
			mutate: func(spec string) string {
				spec = mutate(t, spec, "    oauth2:\n      type: oauth2", "    oauth2:\n      type: http")
				return mutate(t, spec, "- oauth2: [claims:approve]", "- oauth2: [claims:archive]")
			},
			wantHas: "declared as type: http",
		},
		{
			// The advertised flow scopes are judged before any operation.
			name: "a flow scope outranks a stale operation handle",
			mutate: func(spec string) string {
				spec = mutate(t, spec,
					"            reports:read: Monthly totals",
					"            reports:read: Monthly totals\n            claims:archive: Archive a claim")
				return mutate(t, spec, "- oauth2: [claims:approve]", "- oauth2: [claims:vanish]")
			},
			wantHas: "advertises the scope `claims:archive` in flows",
		},
		{
			// Operations are judged in DOCUMENT order: POST /claims precedes
			// POST /claims/{claimId}/approve.
			name: "the earlier operation wins",
			mutate: func(spec string) string {
				spec = mutate(t, spec, "- oauth2: [claims:submit]", "- oauth2: [claims:submit, claims:read]")
				return mutate(t, spec, "- oauth2: [claims:approve]", "- oauth2: [claims:archive]")
			},
			wantHas: "POST /claims names more than one scope",
		},
		{
			// Every security rule outranks the identity-header rules, which run
			// last on both sides.
			name: "an operation scope outranks a required identity header",
			mutate: func(spec string) string {
				spec = mutate(t, spec,
					"    UserId:\n      name: X-User-Id\n      in: header\n      required: false",
					"    UserId:\n      name: X-User-Id\n      in: header\n      required: true")
				return mutate(t, spec, "- oauth2: [claims:approve]", "- oauth2: [claims:archive]")
			},
			wantHas: "requires the scope `claims:archive`",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertContains(t, gate(t, tc.mutate(p6Spec(t)), fullBundle(t)), tc.wantHas)
		})
	}
}

// -------------------------------------------------------------------------
// The message vocabulary is shared with the agent
// -------------------------------------------------------------------------

// openapiGateKeys is every message key THIS gate can render. The table below is
// the Go side of the cross-language vocabulary; the agent's published JSON is
// the other, and a rename that lands in one and not the other is a rule the
// model meets in two wordings.
var openapiGateKeys = []string{
	securityspec.MsgMissingOAuth2Scheme,
	securityspec.MsgSchemeWrongType,
	securityspec.MsgMissingDocumentSecurity,
	securityspec.MsgSchemeWithoutDependency,
	securityspec.MsgDocumentSecurityWithoutDependency,
	securityspec.MsgSecurityWithoutDependency,
	securityspec.MsgOperationSecurityNotAList,
	securityspec.MsgOperationMultipleRequirements,
	securityspec.MsgOperationMultipleScopes,
	securityspec.MsgOperationUnknownScheme,
	securityspec.MsgScopeNotInCatalog,
	securityspec.MsgScopeNotOwned,
	securityspec.MsgFlowScopeNotInCatalog,
	securityspec.MsgFlowScopeNotOwned,
	securityspec.MsgReservedOIDCScope,
	securityspec.MsgIdentityHeaderRequired,
	securityspec.MsgPublicOperationDeclaresIdentityHeader,
}

func TestOpenapiSecurity_SharesEveryKeyWithTheAgent(t *testing.T) {
	published := map[string]string{}
	if err := json.Unmarshal([]byte(readFixture(t, agentOpenapiMessages)), &published); err != nil {
		t.Fatalf("agent message table does not parse: %v", err)
	}
	declared := map[string]bool{}
	for _, key := range openapiGateKeys {
		declared[key] = true
		text, ok := published[key]
		if !ok {
			t.Errorf("the Go gate renders %q, which the agent's table does not publish", key)
			continue
		}
		// …and the vendored copy the Go side renders from says the same thing.
		if rendered := securityspec.Msg(key); rendered != substituteNothing(text) {
			t.Errorf("message %q renders from a different template than the agent publishes:\n  agent: %s\n  go:    %s",
				key, text, rendered)
		}
	}
	for key := range published {
		if !declared[key] {
			t.Errorf("the agent publishes %q, which no Go rule renders", key)
		}
	}
}

// substituteNothing is what Msg does to a template given no parameters: nothing.
// Named so the comparison above reads as the identity it is rather than as a
// stray call.
func substituteNothing(template string) string { return template }

// -------------------------------------------------------------------------
// Wiring: the save gate and the build gate
// -------------------------------------------------------------------------

// securityDesignBundle is a whole design bundle (keys relative to specs/design/)
// whose expense-api component is protected and sound.
func securityDesignBundle(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"design.cell":                       "component expense-api service\n",
		gateComponentDir + "design.json":    protectedDesign(t),
		gateComponentDir + "openapi.yaml":   p6Spec(t),
		securityspec.BundleKey:              catalog(t),
		"components/other/openapi.yaml":     "openapi: 3.0.3\n",
		"components/other/design.json":      enriched("other", "service", "1"),
		"dependencies/billing/openapi.yaml": p6Spec(t),
	}
}

// -------------------------------------------------------------------------
// Resource safety
// -------------------------------------------------------------------------

// aliasBomb is the classic "billion laughs": ten nesting levels of nine aliases
// each, in half a kilobyte. Expanding it yields 9^10 values. The gate reads a
// spec through a *yaml.Node so it can judge the document in WRITTEN order, and
// that decode never resolves an alias — so the walker, not the YAML library,
// used to be the thing that expanded this, and a design save spent minutes of
// CPU on a file any user can author.
func aliasBomb() string {
	var b strings.Builder
	b.WriteString("openapi: 3.0.0\n")
	b.WriteString(`l0: &l0 ["lol","lol","lol","lol","lol","lol","lol","lol","lol"]` + "\n")
	prev := "l0"
	for i := 1; i <= 10; i++ {
		name := "l" + strconv.Itoa(i)
		b.WriteString(name + ": &" + name + " [")
		for j := 0; j < 9; j++ {
			if j > 0 {
				b.WriteString(",")
			}
			b.WriteString("*" + prev)
		}
		b.WriteString("]\n")
		prev = name
	}
	return b.String()
}

// The gate must refuse to expand an alias bomb, and it must do so by having
// NOTHING to say: an unparseable document is the structural gate's business,
// and one defect produces one message.
func TestOpenapiSecurity_AliasBombIsNotExpanded(t *testing.T) {
	bomb := aliasBomb()
	if len(bomb) > 1024 {
		t.Fatalf("the bomb is meant to be tiny, got %d bytes", len(bomb))
	}
	bundle := fullBundle(t)

	type outcome struct {
		problem string
		gateErr error
	}
	done := make(chan outcome, 1)
	go func() {
		var got outcome
		got.problem = gate(t, bomb, bundle)
		// …and on the path a real save takes, where the bomb arrives as a
		// component spec in a whole design bundle.
		files := securityDesignBundle(t)
		files[gateComponentDir+"openapi.yaml"] = bomb
		got.gateErr = validateDesignBundle(files)
		done <- got
	}()

	select {
	case got := <-done:
		if got.problem != "" {
			t.Errorf("the security gate must have no verdict on an unparseable document, got: %s", got.problem)
		}
		if got.gateErr == nil {
			t.Error("the save gate must still refuse the document as unparseable OpenAPI")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the gate expanded the alias bomb — a 500-byte design save must not burn CPU")
	}
}

// The save gate refuses a component spec whose security block is wrong, with
// the same sentence and the same code as the agent's write gate.
func TestSaveGate_RefusesAnInvalidSecurityBlock(t *testing.T) {
	files := securityDesignBundle(t)
	files[gateComponentDir+"openapi.yaml"] = mutate(t, p6Spec(t),
		"- oauth2: [claims:approve]", "- oauth2: [claims:archive]")

	err := validateDesignBundle(files)
	if err == nil {
		t.Fatal("the save gate must refuse a stale handle")
	}
	verr, ok := err.(*DesignValidationError)
	if !ok {
		t.Fatalf("want *DesignValidationError, got %T: %v", err, err)
	}
	var found *FileValidationError
	for i := range verr.Files {
		if verr.Files[i].Code == codeInvalidOpenAPI {
			found = &verr.Files[i]
		}
	}
	if found == nil {
		t.Fatalf("want an INVALID_OPENAPI row, got %+v", verr.Files)
	}
	if found.Path != gateComponentDir+"openapi.yaml" {
		t.Errorf("row names %q, want the component spec", found.Path)
	}
	assertContains(t, found.Message, "requires the scope `claims:archive`")
}

func TestSaveGate_AcceptsASoundBundle(t *testing.T) {
	if err := validateDesignBundle(securityDesignBundle(t)); err != nil {
		t.Fatalf("a sound bundle must pass the save gate: %v", err)
	}
}

// A dependency's spec is relayed, not judged: the same document under
// specs/design/dependencies/ is left alone even when it would be refused as a
// component's own.
func TestSaveGate_LeavesDependencySpecsAlone(t *testing.T) {
	files := securityDesignBundle(t)
	files["dependencies/billing/openapi.yaml"] = mutate(t, p6Spec(t),
		"- oauth2: [claims:approve]", "- oauth2: [claims:archive]")
	if err := validateDesignBundle(files); err != nil {
		t.Fatalf("a dependency spec must not be judged: %v", err)
	}
}

// The build gate is the backstop: the agent's own write gate cannot see
// security.json in a later turn's bundle, so a stale handle can reach the tag
// with every save having passed.
func TestBuildGate_RefusesAnInvalidSecurityBlock(t *testing.T) {
	designFiles := completeDesignFiles()
	designFiles["design.cell"] = gateCell + "component expense-api service\n"
	designFiles[gateComponentDir+"design.json"] = protectedDesign(t)
	designFiles[gateComponentDir+"openapi.yaml"] = mutate(t, p6Spec(t),
		"- oauth2: [claims:approve]", "- oauth2: [claims:archive]")
	designFiles[securityspec.BundleKey] = catalog(t)

	var messages []string
	for _, e := range gateErrors(t, designFiles) {
		if e.Code == codeInvalidOpenAPI {
			messages = append(messages, e.Message)
		}
	}
	if len(messages) != 1 {
		t.Fatalf("want exactly one INVALID_OPENAPI row, got %v", messages)
	}
	assertContains(t, messages[0], "requires the scope `claims:archive`")
}

func TestBuildGate_SoundSecurityBlockPasses(t *testing.T) {
	designFiles := completeDesignFiles()
	designFiles["design.cell"] = gateCell + "component expense-api service\n"
	designFiles[gateComponentDir+"design.json"] = protectedDesign(t)
	designFiles[gateComponentDir+"openapi.yaml"] = p6Spec(t)
	designFiles[securityspec.BundleKey] = catalog(t)

	for _, e := range gateErrors(t, designFiles) {
		if e.Code == codeInvalidOpenAPI {
			t.Fatalf("a sound spec must pass the build gate: %s", e.Message)
		}
	}
}
