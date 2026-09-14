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

package agentfold

import (
	"fmt"
	"strings"
	"testing"
)

// designWithEndpoint builds a minimal valid component design.json for dir "svc",
// splicing in the given `endpoint` fragment (or none when empty).
func designWithEndpoint(endpointFragment string) string {
	ep := ""
	if endpointFragment != "" {
		ep = `,"endpoint":` + endpointFragment
	}
	return fmt.Sprintf(`{"name":"svc","type":"service","version":"0.1.0",`+
		`"language":"Go","buildpack":"docker","appPath":"svc",`+
		`"entrypoint":"deployment/service","exposure":"internet",`+
		`"description":"x","dependencies":[]%s}`, ep)
}

// TestDesignGate_Endpoint locks fold-parity for the design.json `endpoint`
// block against the zod endpointSchema (component-design-schema.ts): a
// zod-accepted write must fold here, and a zod-rejected shape must reject here.
func TestDesignGate_Endpoint(t *testing.T) {
	cases := []struct {
		name     string
		fragment string
		wantOK   bool
	}{
		{"absent — still valid", "", true},
		{"valid name", `{"name":"http"}`, true},
		{"valid non-default name", `{"name":"api"}`, true},
		{"empty name rejected", `{"name":""}`, false},
		{"unknown key rejected", `{"name":"http","port":8080}`, false},
		{"not an object rejected", `"http"`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validateComponentDesign(designWithEndpoint(c.fragment), "svc")
			if c.wantOK && p != nil {
				t.Fatalf("want accepted, got rejected: %s", p.message)
			}
			if !c.wantOK && p == nil {
				t.Fatalf("want rejected, got accepted")
			}
		})
	}
}

// TestDesignGate_TypeAliasRejected locks the canonical-kind rule in parity with
// the zod gate: the wrong web-application spellings reject; the canonical value
// and other kinds accept.
func TestDesignGate_TypeAliasRejected(t *testing.T) {
	tmpl := func(typ string) string {
		return fmt.Sprintf(`{"name":"svc","type":%q,"version":"0.1.0","language":"Go",`+
			`"buildpack":"docker","appPath":"svc","entrypoint":"deployment/service",`+
			`"exposure":"internet","description":"x","dependencies":[]}`, typ)
	}
	cases := []struct {
		typ    string
		wantOK bool
	}{
		{"service", true},
		{"web-application", true},
		{"worker", true},
		{"webapp", false},
		{"web-app", false},
		{"WebApp", false},
		{"webApplication", false},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			p := validateComponentDesign(tmpl(c.typ), "svc")
			if c.wantOK && p != nil {
				t.Fatalf("type %q: want accepted, got %s", c.typ, p.message)
			}
			if !c.wantOK && p == nil {
				t.Fatalf("type %q: want rejected (canonical is web-application), got accepted", c.typ)
			}
		})
	}
}

// designWithParams renders a schema-valid component design.json whose single
// platform-resource dependency carries the given raw `parameters` JSON literal.
func designWithParams(paramsJSON string) string {
	return fmt.Sprintf(`{
  "name": "orders-db-owner",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "svc",
  "entrypoint": "cmd/main",
  "exposure": "internet",
  "description": "owns orders data",
  "dependencies": [
    {
      "kind": "platform-resource",
      "name": "orders-db",
      "resourceType": "postgres-cnpg",
      "parameters": %s
    }
  ]
}`, paramsJSON)
}

// TestValidateComponentDesign_Parameters locks the parameters value rule in
// parity with the zod gate (component-design-schema.ts:
// z.record(z.string(), z.union([z.string(), z.number(), z.boolean()]))): mixed
// scalar values are accepted, non-scalar values reject. This is the write-gate
// half of the fold-parity fix for the number-vs-string divergence.
func TestValidateComponentDesign_Parameters(t *testing.T) {
	const dir = "orders-db-owner"

	accept := []struct {
		name   string
		params string
	}{
		// The postgres-cnpg shape: instances is an integer, storage/version strings.
		{"mixed scalars (number + strings)", `{ "instances": 1, "storage": "5Gi", "version": "16" }`},
		{"boolean value", `{ "highAvailability": true }`},
		{"empty object", `{}`},
	}
	for _, tc := range accept {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			if p := validateComponentDesign(designWithParams(tc.params), dir); p != nil {
				t.Fatalf("expected accept, got reject: %s", p.message)
			}
		})
	}

	reject := []struct {
		name   string
		params string
	}{
		{"object value", `{ "instances": { "nested": 1 } }`},
		{"array value", `{ "zones": ["a", "b"] }`},
		{"null value", `{ "instances": null }`},
		{"parameters not an object", `"not-an-object"`},
	}
	for _, tc := range reject {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			p := validateComponentDesign(designWithParams(tc.params), dir)
			if p == nil {
				t.Fatalf("expected reject for params %s, got accept", tc.params)
			}
			if p.code != ErrSchemaViolation {
				t.Fatalf("code = %q, want %q", p.code, ErrSchemaViolation)
			}
		})
	}
}

// designWithWiring renders a schema-valid component design.json whose single
// platform-resource dependency carries the given raw `wiring` JSON literal.
func designWithWiring(wiringJSON string) string {
	return fmt.Sprintf(`{
  "name": "orders-db-owner",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "svc",
  "entrypoint": "cmd/main",
  "exposure": "internet",
  "description": "owns orders data",
  "dependencies": [
    {
      "kind": "platform-resource",
      "name": "orders-db",
      "resourceType": "postgres-cnpg",
      "wiring": %s
    }
  ]
}`, wiringJSON)
}

// TestValidateComponentDesign_Wiring locks the `wiring` rule in parity with the
// zod gate (component-design-schema.ts dependencyWiringSchema): the object FOLDS
// — unlike status/reason it is persisted in design.json, and the design agent
// reads-edits-writes the file, so a rejection rule would reject its own echo of
// a platform-stamped value. What still rejects is a MALFORMED one: half-stamped
// or wrongly-typed wiring renders an unusable workload.yaml resource entry, which
// is worse than an absent wiring the coding agent reports as a platform fault.
func TestValidateComponentDesign_Wiring(t *testing.T) {
	const dir = "orders-db-owner"

	accept := []struct {
		name   string
		wiring string
	}{
		{"platform-stamped shape", `{ "ref": "shop-orders-db", "envBindings": { "host": "ORDERS_DB_HOST", "port": "ORDERS_DB_PORT" } }`},
		{"no outputs bound yet", `{ "ref": "shop-orders-db", "envBindings": {} }`},
		// The endpoints[] variant: a sibling component's endpoint, scoped for OC.
		{"sibling endpoint variant", `{ "endpoint": { "component": "todo-api99-todo-api", "name": "http", "visibility": "project", "envBindings": { "address": "TODO_API_URL" } } }`},
	}
	for _, tc := range accept {
		t.Run("accept/"+tc.name, func(t *testing.T) {
			if p := validateComponentDesign(designWithWiring(tc.wiring), dir); p != nil {
				t.Fatalf("expected accept, got reject: %s", p.message)
			}
		})
	}

	reject := []struct {
		name   string
		wiring string
	}{
		{"not an object", `"shop-orders-db"`},
		{"ref missing", `{ "envBindings": { "host": "ORDERS_DB_HOST" } }`},
		{"ref empty", `{ "ref": "", "envBindings": {} }`},
		{"envBindings missing", `{ "ref": "shop-orders-db" }`},
		{"envBindings not an object", `{ "ref": "shop-orders-db", "envBindings": "ORDERS_DB_HOST" }`},
		{"env var not a string", `{ "ref": "shop-orders-db", "envBindings": { "port": 5432 } }`},
		{"unknown property", `{ "ref": "shop-orders-db", "envBindings": {}, "values": { "host": "db" } }`},
		// The endpoints[] variant is all-or-nothing too, and exclusive with the
		// resources[] one: a partial or mixed entry is silently ignored by
		// OpenChoreo, which is the failure mode this whole field exists to end.
		{"endpoint not an object", `{ "endpoint": "todo-api99-todo-api" }`},
		{"endpoint component missing", `{ "endpoint": { "name": "http", "visibility": "project", "envBindings": { "address": "TODO_API_URL" } } }`},
		{"endpoint component empty", `{ "endpoint": { "component": "", "name": "http", "visibility": "project", "envBindings": {} } }`},
		{"endpoint name missing", `{ "endpoint": { "component": "todo-api99-todo-api", "visibility": "project", "envBindings": {} } }`},
		{"endpoint visibility missing", `{ "endpoint": { "component": "todo-api99-todo-api", "name": "http", "envBindings": {} } }`},
		{"endpoint envBindings missing", `{ "endpoint": { "component": "todo-api99-todo-api", "name": "http", "visibility": "project" } }`},
		{"endpoint env var not a string", `{ "endpoint": { "component": "todo-api99-todo-api", "name": "http", "visibility": "project", "envBindings": { "address": 8080 } } }`},
		{"endpoint unknown property", `{ "endpoint": { "component": "todo-api99-todo-api", "name": "http", "visibility": "project", "envBindings": {}, "project": "todo-api99" } }`},
		{"both variants at once", `{ "ref": "shop-orders-db", "envBindings": {}, "endpoint": { "component": "todo-api99-todo-api", "name": "http", "visibility": "project", "envBindings": {} } }`},
	}
	for _, tc := range reject {
		t.Run("reject/"+tc.name, func(t *testing.T) {
			p := validateComponentDesign(designWithWiring(tc.wiring), dir)
			if p == nil {
				t.Fatalf("expected reject for wiring %s, got accept", tc.wiring)
			}
			if p.code != ErrSchemaViolation {
				t.Fatalf("code = %q, want %q", p.code, ErrSchemaViolation)
			}
		})
	}
}

// designWithSkills builds a minimal valid component design.json for dir "svc",
// optionally injecting a `skillsPinned` value verbatim from fragment.
func designWithSkills(fragment string) string {
	sa := ""
	if fragment != "" {
		sa = `,"skillsPinned":` + fragment
	}
	return fmt.Sprintf(`{"name":"svc","type":"service","version":"0.1.0",`+
		`"language":"Go","buildpack":"docker","appPath":"svc",`+
		`"entrypoint":"deployment/service","exposure":"internet",`+
		`"description":"x","dependencies":[]%s}`, sa)
}

// designWithDependency builds a minimal valid component design.json for dir
// "svc" whose sole dependency is the given raw dependency-object JSON literal.
func designWithDependency(depFragment string) string {
	return fmt.Sprintf(`{"name":"svc","type":"service","version":"0.1.0",`+
		`"language":"Go","buildpack":"docker","appPath":"svc",`+
		`"entrypoint":"deployment/service","exposure":"internet",`+
		`"description":"x","dependencies":[%s]}`, depFragment)
}

// TestDesignGate_MovedDependencyFields locks the Go mirror of
// component-design-schema.ts's MOVED_DEPENDENCY_FIELDS: an external
// dependency's definition (style, package, specPath, candidates, config) lives
// in specs/design/dependencies/<name>/dependency.json now, so a component that
// still writes any of them is refused with a message naming that file; on any
// other kind the same keys are plain unknown properties.
func TestDesignGate_MovedDependencyFields(t *testing.T) {
	fields := []struct {
		field string
		value string // raw JSON literal for the field's value
	}{
		{"candidates", `[{"name":"sendgrid-rest","style":"rest-api"},{"name":"resend-sdk","style":"sdk"}]`},
		{"style", `"sdk"`},
		{"package", `"npm:stripe@^14"`},
		{"specPath", `"dependencies/stripe.openapi.yaml"`},
		{"config", `[{"key":"STRIPE_API_KEY","secret":true}]`},
	}
	for _, f := range fields {
		t.Run(f.field+"/rejected on kind=external, pointing at the dependency file", func(t *testing.T) {
			dep := fmt.Sprintf(`{"kind":"external","name":"stripe","%s":%s}`, f.field, f.value)
			p := validateComponentDesign(designWithDependency(dep), "svc")
			if p == nil {
				t.Fatalf("want rejected (moved field %q on kind=external), got accepted", f.field)
			}
			if p.code != ErrSchemaViolation {
				t.Fatalf("code = %q, want %q", p.code, ErrSchemaViolation)
			}
			if !strings.Contains(p.message, `"`+f.field+`"`) || !strings.Contains(p.message, "specs/design/dependencies/stripe/dependency.json") {
				t.Fatalf("message %q must name the field and the dependency file", p.message)
			}
		})
		t.Run(f.field+"/unknown property on kind=org-service", func(t *testing.T) {
			dep := fmt.Sprintf(`{"kind":"org-service","name":"identity","%s":%s}`, f.field, f.value)
			p := validateComponentDesign(designWithDependency(dep), "svc")
			if p == nil {
				t.Fatalf("want rejected (unknown field %q on kind=org-service), got accepted", f.field)
			}
			if !strings.Contains(p.message, f.field) {
				t.Fatalf("message %q does not mention field %q", p.message, f.field)
			}
		})
	}
	t.Run("a bare external reference is accepted", func(t *testing.T) {
		dep := `{"kind":"external","name":"stripe","description":"charges shipping"}`
		if p := validateComponentDesign(designWithDependency(dep), "svc"); p != nil {
			t.Fatalf("want accepted, got rejected: %s", p.message)
		}
	})
}

// TestDesignGate_RetiredExternalFieldsRejected documents the hard-break: the
// specUrl (URL hint) and sources (provenance array) fields were removed from
// the dependency schema — the coding agent now researches contracts freely
// from the web, so neither is authored. Like status/reason and the retired
// needsSpec, they are no longer in the known-key set and reject as unknown keys
// (strictObject) even on kind="external". Parity with the zod gate, whose
// strictObject stopped listing them.
func TestDesignGate_RetiredExternalFieldsRejected(t *testing.T) {
	for _, f := range []struct{ field, value string }{
		{"specUrl", `"https://example.com/stripe.yaml"`},
		{"sources", `["https://stripe.com/docs/api"]`},
	} {
		t.Run(f.field+"/rejected on kind=external", func(t *testing.T) {
			dep := fmt.Sprintf(`{"kind":"external","name":"stripe","%s":%s}`, f.field, f.value)
			p := validateComponentDesign(designWithDependency(dep), "svc")
			if p == nil {
				t.Fatalf("want rejected (retired field %q), got accepted", f.field)
			}
			if p.code != ErrSchemaViolation {
				t.Fatalf("code = %q, want %q", p.code, ErrSchemaViolation)
			}
			if !strings.Contains(p.message, f.field) {
				t.Fatalf("message %q does not mention field %q", p.message, f.field)
			}
		})
	}
}

func TestDesignGate_SkillsPinned(t *testing.T) {
	cases := []struct {
		name     string
		fragment string
		wantOK   bool
	}{
		{"absent — valid", "", true},
		{"empty array — valid", `[]`, true},
		{"string array — valid", `["go","openapi-conventions"]`, true},
		{"not an array — rejected", `"go"`, false},
		{"non-string element — rejected", `["go",3]`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := validateComponentDesign(designWithSkills(c.fragment), "svc")
			if c.wantOK && p != nil {
				t.Fatalf("want accepted, got: %s", p.message)
			}
			if !c.wantOK && p == nil {
				t.Fatalf("want rejected, got accepted")
			}
		})
	}
}

// designWithSkillsKey builds a minimal valid component design.json for dir
// "svc", splicing in the given skills array fragment under the given key
// name (e.g. "skillsPinned" or the legacy "skillsPinned").
func designWithSkillsKey(key, fragment string) string {
	return fmt.Sprintf(`{"name":"svc","type":"service","version":"0.1.0",`+
		`"language":"Go","buildpack":"docker","appPath":"svc",`+
		`"entrypoint":"deployment/service","exposure":"internet",`+
		`"description":"x","dependencies":[],"%s":%s}`, key, fragment)
}

// TestDesignGate_SkillsPinned_And_LegacyAccepted locks the rename's
// compatibility rule: `skillsPinned` is the name the design agent now
// writes, but `skillsPinned` designs already committed in customer org
// repos must keep validating forever — the schema's root
// additionalProperties:false must accept BOTH keys. See
// packages/agent-stream/src/component-design-schema.ts.
func TestDesignGate_SkillsPinned_And_LegacyAccepted(t *testing.T) {
	for _, key := range []string{"skillsPinned", "skillsPinned"} {
		t.Run(key+"/valid", func(t *testing.T) {
			p := validateComponentDesign(designWithSkillsKey(key, `["go","openapi-conventions"]`), "svc")
			if p != nil {
				t.Fatalf("want accepted (%s), got: %s", key, p.message)
			}
		})
	}
}
