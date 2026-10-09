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

package designspec

import (
	"encoding/json"
	"strings"
	"testing"
)

const depPath = "specs/design/dependencies/payment-provider/dependency.json"
const sdkPath = "specs/design/dependencies/payment-provider/sdk.json"

// dep builds a resolved REST dependency.json for dir "payment-provider" with
// overrides spliced in — top-level keys by name, resource-block keys with a
// "resource." prefix; a nil value deletes the key — the same fixture shape the
// zod gate's dependency-design-gate.test.ts uses, so the two tables read alike.
func dep(overrides map[string]any) string {
	res := map[string]any{
		"name":        "payment-provider",
		"description": "Charges the customer for shipping.",
		"provider":    "Stripe",
		"config":      []any{map[string]any{"key": "PAYMENT_API_KEY", "secret": true}},
		"contract":    map[string]any{"type": "openapi", "path": "openapi.yaml", "origin": "provider"},
	}
	m := map[string]any{"name": "payment-provider", "resource": res}
	for k, v := range overrides {
		target, key := m, k
		if strings.HasPrefix(k, "resource.") {
			target, key = res, strings.TrimPrefix(k, "resource.")
		}
		if v == nil {
			delete(target, key)
		} else {
			target[key] = v
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// contract is a contract object with overrides.
func contract(overrides map[string]any) map[string]any {
	c := map[string]any{"type": "openapi", "path": "openapi.yaml", "origin": "provider"}
	for k, v := range overrides {
		if v == nil {
			delete(c, k)
		} else {
			c[k] = v
		}
	}
	return c
}

// TestDependencyShape_Parity locks parity with checkDependencyDesign: every
// case here has its twin in packages/agent-stream/test/dependency-design-gate.test.ts.
func TestDependencyShape_Parity(t *testing.T) {
	two := []any{map[string]any{"name": "a", "style": "sdk"}, map[string]any{"name": "b"}}
	open := map[string]any{"resource.provider": nil, "resource.contract": nil, "resource.config": nil}
	withOpen := func(extra map[string]any) map[string]any {
		m := map[string]any{}
		for k, v := range open {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	cases := []struct {
		name    string
		content string
		wantOK  bool
		wantMsg string
	}{
		{"resolved rest dependency", dep(nil), true, ""},
		{"open suggestions, nothing chosen", dep(withOpen(map[string]any{"suggestions": two})), true, ""},
		{"a single suggestion is fine", dep(withOpen(map[string]any{"suggestions": two[:1]})), true, ""},
		{"the need alone", dep(open), true, ""},
		{"config before a provider", dep(map[string]any{"resource.provider": nil, "resource.contract": nil}), false, "derived from the chosen service"},
		{"registry stub: the agent asks by name", `{"name":"payment-provider","resource":{"ref":"payment-provider","name":"payment-provider"}}`, true, ""},
		{"a copy carries config without a project-chosen provider", `{"name":"payment-provider","resource":{"ref":"payment-provider","name":"payment-provider","config":[{"key":"K"}]}}`, true, ""},
		{"ref not the dependency name", `{"name":"payment-provider","resource":{"ref":"stripe","name":"payment-provider"}}`, false, `"resource.ref" must equal`},
		{"provider chosen, no contract yet", dep(map[string]any{"resource.contract": nil}), true, ""},
		{"name not the directory", dep(map[string]any{"name": "stripe", "resource.name": "stripe"}), false, `"payment-provider"`},
		{"resource.name not the dependency name", dep(map[string]any{"resource.name": "stripe"}), false, `"resource.name" must equal`},
		{"no resource block", `{"name":"payment-provider"}`, false, `"resource" is required`},
		{"read-time status rejected", dep(map[string]any{"status": "resolved"}), false, "unknown property status"},
		{"retired specPath rejected", dep(map[string]any{"specPath": "https://x"}), false, "unknown property specPath"},
		{"retired flat provider", dep(map[string]any{"provider": "Stripe"}), false, `"provider" is not a top-level field`},
		{"retired flat style", dep(map[string]any{"style": "rest-api"}), false, `"style" is not a top-level field`},
		{"retired flat source", dep(map[string]any{"source": "org"}), false, `"source" is not a top-level field`},
		{"retired candidates", dep(withOpen(map[string]any{"candidates": two})), false, `"candidates" is not a top-level field`},
		{"suggestions with a provider", dep(map[string]any{"suggestions": two}), false, "never coexist"},
		{"suggestions with a contract", dep(map[string]any{"resource.provider": nil, "resource.config": nil, "suggestions": two}), false, "stay unset"},
		{"contract path not fitting the type", dep(map[string]any{"resource.contract": contract(map[string]any{"path": "schema.graphql"})}), false, `for type "openapi"`},
		{"graphql contract", dep(map[string]any{"resource.contract": contract(map[string]any{"type": "graphql", "path": "schema.graphql"})}), true, ""},
		{"contract path as a path", dep(map[string]any{"resource.contract": contract(map[string]any{"path": "specs/x/openapi.yaml"})}), false, "not a path"},
		{"contract without a type", dep(map[string]any{"resource.contract": contract(map[string]any{"type": nil})}), false, "resource.contract.type"},
		{"contract with a url", dep(map[string]any{"resource.contract": map[string]any{"type": "openapi", "url": "https://x/openapi.yaml"}}), false, `no "url" form`},
		{"unknown contract type", dep(map[string]any{"resource.contract": contract(map[string]any{"type": "asyncapi", "path": "asyncapi.yaml"})}), false, "not an allowed value"},
		{"bad origin", dep(map[string]any{"resource.contract": contract(map[string]any{"origin": "guessed"})}), false, "origin"},
		{"sdk contract is the manifest", dep(map[string]any{"resource.contract": contract(map[string]any{"type": "sdk", "path": "sdk.json"})}), true, ""},
		{"sdk contract misnamed", dep(map[string]any{"resource.contract": contract(map[string]any{"type": "sdk", "path": "manifest.json"})}), false, `"sdk.json"`},
		{"secret key with a default", dep(map[string]any{"resource.config": []any{map[string]any{"key": "K", "secret": true, "defaultValue": "x"}}}), false, "secret"},
		{"bad sha256", dep(map[string]any{"provenance": map[string]any{"sha256": "nope"}}), false, "sha256"},
		{"good provenance", dep(map[string]any{"provenance": map[string]any{"sourceUrl": "https://x", "sha256": strings.Repeat("ab", 32), "readOn": "2026-09-17"}}), true, ""},
		{"registry provenance", dep(map[string]any{"provenance": map[string]any{"registry": "payment-provider/openapi.yaml", "sha256": strings.Repeat("ab", 32)}}), true, ""},
		{"registry provenance as a url", dep(map[string]any{"provenance": map[string]any{"registry": "https://x/openapi.yaml"}}), false, "never a URL"},
		{"retired provenance.sliced", dep(map[string]any{"provenance": map[string]any{"sliced": true}}), false, "sliced is retired"},
		{"retired provenance.fetchedAt", dep(map[string]any{"provenance": map[string]any{"fetchedAt": "x"}}), false, "fetchedAt is retired"},
		// Types the zod schema pins — the save gate refuses the same shapes.
		{"secret not a boolean", dep(map[string]any{"resource.config": []any{map[string]any{"key": "K", "secret": "yes"}}}), false, "secret: must be a boolean"},
		{"defaultValue not a string", dep(map[string]any{"resource.config": []any{map[string]any{"key": "K", "defaultValue": 5}}}), false, "defaultValue: must be a string"},
		{"suggestion description not a string", dep(withOpen(map[string]any{"suggestions": []any{map[string]any{"name": "a", "description": 1}}})), false, "description: must be a string"},
		{"suggestion carries a package", dep(withOpen(map[string]any{"suggestions": []any{map[string]any{"name": "a", "package": "npm:a"}}})), false, "unknown property package"},
		{"unknown resource property", dep(map[string]any{"resource.style": "rest-api"}), false, "resource: unknown property style"},
		// zod's optional() never admits null; the save gate refuses the same.
		{"null provenance", `{"name":"payment-provider","resource":{"name":"payment-provider"},"provenance":null}`, false, "must not be null"},
		{"null suggestions", `{"name":"payment-provider","resource":{"name":"payment-provider"},"suggestions":null}`, false, "must not be null"},
		{"null resource contract", `{"name":"payment-provider","resource":{"name":"payment-provider","provider":"Stripe","contract":null}}`, false, "must not be null"},
		{"absent accepted is fine", dep(map[string]any{"resource.contract": contract(map[string]any{"accepted": nil})}), true, ""},
		{"null accepted", `{"name":"payment-provider","resource":{"name":"payment-provider","provider":"Stripe","contract":{"type":"openapi","path":"openapi.yaml","accepted":null}}}`, false, "must not be null"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validateDependencyDesign(c.content, "payment-provider")
			if c.wantOK && p != nil {
				t.Fatalf("want accepted, got rejected: %s", p.message)
			}
			if !c.wantOK {
				if p == nil {
					t.Fatalf("want rejected, got accepted")
				}
				if !strings.Contains(p.message, c.wantMsg) {
					t.Fatalf("message %q does not contain %q", p.message, c.wantMsg)
				}
			}
		})
	}
}

func TestDependencyShape_SdkManifest(t *testing.T) {
	ok := `{"packages":{"typescript":"npm:stripe@^14","go":"go:github.com/stripe/stripe-go/v79"},"calls":["paymentIntents.create"]}`
	if p := validateSdkManifest(ok); p != nil {
		t.Fatalf("want accepted, got %s", p.message)
	}
	for name, c := range map[string]struct{ content, want string }{
		"empty packages":  {`{"packages":{}}`, "at least one"},
		"upper-case lang": {`{"packages":{"TypeScript":"npm:stripe"}}`, "lower-case"},
		"no ecosystem":    {`{"packages":{"go":"stripe-go"}}`, "ecosystem-prefixed"},
		"unknown key":     {`{"packages":{"go":"go:x"},"version":"1"}`, "unknown property version"},
		"docsUrl type":    {`{"packages":{"go":"go:x"},"docsUrl":3}`, "docsUrl: must be a string"},
		"assumed type":    {`{"packages":{"go":"go:x"},"assumed":"yes"}`, "assumed: must be a boolean"},
		"derived type":    {`{"packages":{"go":"go:x"},"derived":"yes"}`, "derived: must be a boolean"},
	} {
		t.Run(name, func(t *testing.T) {
			p := validateSdkManifest(c.content)
			if p == nil || !strings.Contains(p.message, c.want) {
				t.Fatalf("want rejection containing %q, got %v", c.want, p)
			}
		})
	}
	if ve := CheckDependencyFile(sdkPath, "{nope"); ve == nil || ve.Code != CodeInvalidJSON {
		t.Fatalf("invalid JSON must be CodeInvalidJSON, got %v", ve)
	}
	if ve := CheckDependencyFile(depPath, dep(nil)); ve != nil {
		t.Fatalf("a valid dependency.json must pass, got %v", ve)
	}
	if ve := CheckDependencyFile("specs/design/dependencies/payment-provider/openapi.yaml", "openapi: 3"); ve != nil {
		t.Fatalf("a contract file is not this gate's, got %v", ve)
	}
}

// At save the file is its own record: the platform's fields (the copy's
// instructions, the user's acceptance record) pass whoever wrote them, and a
// rejection names the file and carries the schema code.
func TestCheckDependencyFile_SaveView(t *testing.T) {
	accepted := map[string]any{"by": "admin", "at": "2026-09-08T10:15:00Z", "note": "auth guessed"}
	withPlatformFields := dep(map[string]any{
		"resource.ref": "payment-provider", "resource.consumptionInstructions": "Never log the key.",
		"resource.contract": contract(map[string]any{"origin": "assumed", "accepted": accepted}),
	})
	if ve := CheckDependencyFile(depPath, withPlatformFields); ve != nil {
		t.Fatalf("the platform's fields must pass at save, got %v", ve)
	}
	ve := CheckDependencyFile(depPath, dep(map[string]any{"suggestions": []any{map[string]any{"name": "a"}}}))
	if ve == nil || ve.Code != CodeSchemaViolation || !strings.HasPrefix(ve.Message, depPath+": ") || !strings.Contains(ve.Message, "never coexist") {
		t.Fatalf("want a schema violation naming the file, got %v", ve)
	}
}
