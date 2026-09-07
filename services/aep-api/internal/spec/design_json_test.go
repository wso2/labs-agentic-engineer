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

import (
	"reflect"
	"strings"
	"testing"
)

// fullComponentDesignJSON is a canonical `components/checkout/design.json`
// exercising all four dependency kinds and the platform-owned exposesAPI
// block. It is authored to be byte-identical to marshalComponentDesignJSON's
// output so the round-trip assertion is exact.
const fullComponentDesignJSON = `{
  "name": "checkout",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "checkout",
  "entrypoint": "deployment/service",
  "exposure": "intranet",
  "description": "Owns order checkout. Does NOT handle payments capture.",
  "endpoint": {
    "name": "http"
  },
  "dependencies": [
    {
      "kind": "component",
      "name": "cart"
    },
    {
      "kind": "org-service",
      "name": "user-profile",
      "description": "cross-project profile lookup"
    },
    {
      "kind": "external",
      "name": "openweather",
      "description": "weather",
      "style": "rest-api",
      "specPath": "dependencies/openweather.openapi.yaml",
      "config": [
        {
          "key": "OPENWEATHER_API_KEY",
          "secret": true,
          "description": "Your OpenWeather API key"
        },
        {
          "key": "OPENWEATHER_REGION",
          "defaultValue": "us-east-1"
        }
      ],
      "wiring": {
        "ref": "shop-openweather",
        "envBindings": {
          "OPENWEATHER_API_KEY": "OPENWEATHER_API_KEY",
          "OPENWEATHER_REGION": "OPENWEATHER_REGION"
        }
      }
    },
    {
      "kind": "platform-resource",
      "name": "orders-db",
      "resourceType": "postgres",
      "parameters": {
        "size": "small"
      },
      "wiring": {
        "ref": "shop-orders-db",
        "envBindings": {
          "host": "ORDERS_DB_HOST",
          "port": "ORDERS_DB_PORT"
        }
      }
    }
  ],
  "exposesAPI": {
    "auth": "service-required",
    "orgPublished": true
  },
  "componentAgentInstructions": "prefer stdlib net/http"
}
`

func TestParseComponentDesignJSON_AllKinds(t *testing.T) {
	comp, err := parseComponentDesignJSON("checkout", fullComponentDesignJSON)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if comp.Name != "checkout" || comp.ComponentType != "service" || comp.Version != "0.1.0" {
		t.Fatalf("base fields drifted: %+v", comp)
	}
	if comp.Language != "Go" || comp.Buildpack != "docker" || comp.AppPath != "checkout" ||
		comp.Entrypoint != "deployment/service" || comp.Exposure != "intranet" {
		t.Fatalf("scalar fields drifted: %+v", comp)
	}
	if comp.Description != "Owns order checkout. Does NOT handle payments capture." {
		t.Fatalf("description drifted: %q", comp.Description)
	}
	if comp.ComponentAgentInstructions != "prefer stdlib net/http" {
		t.Fatalf("componentAgentInstructions drifted: %q", comp.ComponentAgentInstructions)
	}
	if comp.ExposesAPI == nil || comp.ExposesAPI.Auth != "service-required" || !comp.ExposesAPI.OrgPublished {
		t.Fatalf("exposesAPI drifted: %+v", comp.ExposesAPI)
	}
	if comp.Endpoint == nil || comp.Endpoint.Name != "http" || comp.EndpointName() != "http" {
		t.Fatalf("endpoint drifted: %+v (EndpointName=%q)", comp.Endpoint, comp.EndpointName())
	}

	if len(comp.Dependencies) != 4 {
		t.Fatalf("want 4 dependencies, got %d: %+v", len(comp.Dependencies), comp.Dependencies)
	}
	got := comp.Dependencies
	if got[0].Kind != DependencyKindComponent || got[0].Name != "cart" {
		t.Fatalf("dep[0] drifted: %+v", got[0])
	}
	if got[1].Kind != DependencyKindOrgService || got[1].Name != "user-profile" ||
		got[1].Description != "cross-project profile lookup" {
		t.Fatalf("dep[1] drifted: %+v", got[1])
	}
	if got[2].Kind != DependencyKindExternal || got[2].Name != "openweather" ||
		got[2].Style != DependencyStyleRestAPI || got[2].SpecPath != "dependencies/openweather.openapi.yaml" {
		t.Fatalf("dep[2] drifted: %+v", got[2])
	}
	// The codec is a pure decode: it never computes Status/Reason (no
	// org/registry context) — that is the shared resolver's job at read time.
	if got[2].Status != "" || got[2].Reason != "" {
		t.Fatalf("dep[2] must have no computed status (pure decode): %+v", got[2])
	}
	if len(got[2].Config) != 2 || got[2].Config[0].Key != "OPENWEATHER_API_KEY" || !got[2].Config[0].Secret ||
		got[2].Config[0].Description != "Your OpenWeather API key" || got[2].Config[0].DefaultValue != "" {
		t.Fatalf("dep[2] config drifted: %+v", got[2].Config)
	}
	if got[2].Config[1].Key != "OPENWEATHER_REGION" || got[2].Config[1].Secret ||
		got[2].Config[1].DefaultValue != "us-east-1" {
		t.Fatalf("dep[2] config[1] defaultValue drifted: %+v", got[2].Config)
	}
	if got[3].Kind != DependencyKindPlatformResource || got[3].Name != "orders-db" ||
		got[3].ResourceType != "postgres" || got[3].Parameters["size"] != "small" {
		t.Fatalf("dep[3] drifted: %+v", got[3])
	}
}

func TestMarshalComponentDesignJSON_RoundTripByteIdentical(t *testing.T) {
	comp, err := parseComponentDesignJSON("checkout", fullComponentDesignJSON)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := marshalComponentDesignJSON("checkout", comp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != fullComponentDesignJSON {
		t.Fatalf("round-trip not byte-identical:\n--- got ---\n%s\n--- want ---\n%s", out, fullComponentDesignJSON)
	}
	// A trailing newline keeps git diffs clean.
	if !strings.HasSuffix(string(out), "\n") {
		t.Fatalf("output must end with a trailing newline")
	}
}

// TestComponentDesignJSON_Endpoint covers the design.json `endpoint` block:
// a declared non-default name round-trips verbatim and drives EndpointName(),
// while an omitted block leaves Endpoint nil and EndpointName() falls back to
// the platform default "http" (never emitting an `endpoint` key on write).
func TestComponentDesignJSON_Endpoint(t *testing.T) {
	t.Run("custom name round-trips and drives EndpointName", func(t *testing.T) {
		raw := `{
  "name": "gateway",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "gateway",
  "entrypoint": "deployment/service",
  "exposure": "internet",
  "description": "Edge gateway exposing a non-default endpoint name.",
  "endpoint": {
    "name": "api"
  },
  "dependencies": []
}
`
		comp, err := parseComponentDesignJSON("gateway", raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if comp.Endpoint == nil || comp.Endpoint.Name != "api" {
			t.Fatalf("endpoint drifted: %+v", comp.Endpoint)
		}
		if comp.EndpointName() != "api" {
			t.Fatalf("EndpointName() = %q, want %q", comp.EndpointName(), "api")
		}
		out, err := marshalComponentDesignJSON("gateway", comp)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(out) != raw {
			t.Fatalf("round-trip not byte-identical:\n--- got ---\n%s\n--- want ---\n%s", out, raw)
		}
	})

	t.Run("omitted endpoint defaults to http and emits no key", func(t *testing.T) {
		raw := `{
  "name": "worker",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "worker",
  "entrypoint": "deployment/service",
  "exposure": "intranet",
  "description": "The component declares no explicit network name; the default applies.",
  "dependencies": []
}
`
		comp, err := parseComponentDesignJSON("worker", raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if comp.Endpoint != nil {
			t.Fatalf("endpoint should be nil when omitted, got %+v", comp.Endpoint)
		}
		if comp.EndpointName() != DefaultEndpointName {
			t.Fatalf("EndpointName() = %q, want default %q", comp.EndpointName(), DefaultEndpointName)
		}
		out, err := marshalComponentDesignJSON("worker", comp)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if strings.Contains(string(out), `"endpoint"`) {
			t.Fatalf("omitted endpoint key must not be written back:\n%s", out)
		}
		if string(out) != raw {
			t.Fatalf("round-trip not byte-identical:\n--- got ---\n%s\n--- want ---\n%s", out, raw)
		}
	})
}

func TestMarshalComponentDesignJSON_NeverEmitsStatusReason(t *testing.T) {
	// A dependency whose Status/Reason were computed (by the shared resolver, at
	// read time) must never leak into the written file.
	comp := DesignComponent{
		Name:          "checkout",
		ComponentType: "service",
		Dependencies: []Dependency{
			{
				Kind:   DependencyKindExternal,
				Name:   "openweather",
				Style:  DependencyStyleRestAPI,
				Status: "unresolved", // computed — must be dropped
				Reason: "needs-spec", // computed — must be dropped
			},
		},
	}
	out, err := marshalComponentDesignJSON("checkout", comp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "status") || strings.Contains(string(out), "reason") {
		t.Fatalf("written design.json must not contain status/reason:\n%s", out)
	}
}

func TestParseComponentDesignJSON_UnknownTopLevelKeyRejected(t *testing.T) {
	raw := `{"name":"checkout","type":"service","dependencies":[],"connections":[]}`
	if _, err := parseComponentDesignJSON("checkout", raw); err == nil {
		t.Fatalf("expected unknown top-level key (connections) to be rejected")
	}
}

// TestParseComponentDesignJSON_RetiredCallerIdentityKeyRejected documents the
// deletion's actual on-disk consequence: this decoder calls
// DisallowUnknownFields, so a design.json still carrying the retired
// caller-identity field (e.g. one written before the thunder-app dependency
// replaced it) is now REJECTED as an unknown top-level key — not silently
// tolerated.
func TestParseComponentDesignJSON_RetiredCallerIdentityKeyRejected(t *testing.T) {
	raw := `{"name":"checkout","type":"service","dependencies":[],"callerIdentity":{"mode":"end-user"}}`
	_, err := parseComponentDesignJSON("checkout", raw)
	if err == nil {
		t.Fatalf("expected the retired caller-identity key to be rejected as an unknown top-level key")
	}
	if !strings.Contains(err.Error(), "callerIdentity") {
		t.Fatalf("expected error to name the unknown key, got: %v", err)
	}
}

func TestParseComponentDesignJSON_StatusReasonInDependencyRejected(t *testing.T) {
	for _, key := range []string{"status", "reason"} {
		raw := `{"name":"checkout","type":"service","dependencies":[{"kind":"external","name":"x","` + key + `":"v"}]}`
		if _, err := parseComponentDesignJSON("checkout", raw); err == nil {
			t.Fatalf("expected %q inside a dependency entry to be rejected as an unknown key", key)
		}
	}
}

func TestParseComponentDesignJSON_UnknownDependencyKeyRejected(t *testing.T) {
	raw := `{"name":"checkout","type":"service","dependencies":[{"kind":"external","name":"x","bogus":1}]}`
	if _, err := parseComponentDesignJSON("checkout", raw); err == nil {
		t.Fatalf("expected unknown dependency key to be rejected")
	}
}

func TestParseComponentDesignJSON_NameMustEqualDir(t *testing.T) {
	raw := `{"name":"other","type":"service","dependencies":[]}`
	if _, err := parseComponentDesignJSON("checkout", raw); err == nil {
		t.Fatalf("expected name!=dir to be rejected")
	}
}

func TestParseComponentDesignJSON_DependencyMissingKindRejected(t *testing.T) {
	// Two well-formed entries then a kindless one at index 2 — the error must
	// name the index and the missing key, self-correction style, so a writing
	// agent can fix it in one round trip.
	raw := `{"name":"checkout","type":"service","dependencies":[` +
		`{"kind":"component","name":"cart"},` +
		`{"kind":"org-service","name":"user-profile"},` +
		`{"name":"orphan"}` +
		`]}`
	_, err := parseComponentDesignJSON("checkout", raw)
	if err == nil {
		t.Fatalf("expected dependency missing kind to be rejected as a schema error")
	}
	msg := err.Error()
	for _, want := range []string{"components/checkout/design.json", "dependencies[2]", `"kind"`, "component | org-service | external | platform-resource"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message missing %q for self-correction: %v", want, err)
		}
	}
}

func TestParseComponentDesignJSON_DependencyMissingNameRejected(t *testing.T) {
	raw := `{"name":"checkout","type":"service","dependencies":[{"kind":"component"}]}`
	_, err := parseComponentDesignJSON("checkout", raw)
	if err == nil {
		t.Fatalf("expected dependency missing name to be rejected as a schema error")
	}
	msg := err.Error()
	for _, want := range []string{"components/checkout/design.json", "dependencies[0]", `"name"`} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message missing %q for self-correction: %v", want, err)
		}
	}
}

func TestParseComponentDesignJSON_DependencyUnknownKindRejected(t *testing.T) {
	raw := `{"name":"checkout","type":"service","dependencies":[{"kind":"sidecar","name":"cart"}]}`
	_, err := parseComponentDesignJSON("checkout", raw)
	if err == nil {
		t.Fatalf("expected unknown dependency kind %q to be rejected as a schema error", "sidecar")
	}
	msg := err.Error()
	for _, want := range []string{"components/checkout/design.json", "dependencies[0]", `"sidecar"`, "component | org-service | external | platform-resource"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message missing %q for self-correction: %v", want, err)
		}
	}
}

func TestParseComponentDesignJSON_ExposureInvalidRejected(t *testing.T) {
	raw := `{"name":"checkout","type":"service","exposure":"public","dependencies":[]}`
	_, err := parseComponentDesignJSON("checkout", raw)
	if err == nil {
		t.Fatalf("expected invalid exposure %q to be rejected as a schema error", "public")
	}
	msg := err.Error()
	for _, want := range []string{"components/checkout/design.json", "exposure", `"public"`, "internet", "intranet"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error message missing %q for self-correction: %v", want, err)
		}
	}
}

func TestParseComponentDesignJSON_ExposureAbsentOrEmptyAccepted(t *testing.T) {
	raw := `{"name":"checkout","type":"service","dependencies":[]}`
	comp, err := parseComponentDesignJSON("checkout", raw)
	if err != nil {
		t.Fatalf("expected absent exposure to be accepted: %v", err)
	}
	if comp.Exposure != "" {
		t.Fatalf("want empty exposure, got %q", comp.Exposure)
	}
}

func TestMarshalComponentDesignJSON_NameMustEqualDir(t *testing.T) {
	comp := DesignComponent{Name: "other", ComponentType: "service"}
	if _, err := marshalComponentDesignJSON("checkout", comp); err == nil {
		t.Fatalf("expected component name %q != dir %q to be rejected", comp.Name, "checkout")
	}
}

// TestParseComponentDesignJSON_NeedsSpecNowUnknownFieldRejected documents the
// hard-break: `needsSpec` was dropped from the schema entirely (no read-path
// shim, no back-compat — dependency-management schema revision, derived-state
// model). DisallowUnknownFields now rejects it like any other retired key; the
// needs-spec resolution STATE is reborn from style/specPath via the
// shared resolver (a later task), computed at read time — never stored.
func TestParseComponentDesignJSON_NeedsSpecNowUnknownFieldRejected(t *testing.T) {
	raw := `{"name":"checkout","type":"service","dependencies":[{"kind":"external","name":"weather","needsSpec":true}]}`
	_, err := parseComponentDesignJSON("checkout", raw)
	if err == nil {
		t.Fatalf("expected the retired needsSpec key to be rejected as an unknown field")
	}
	if !strings.Contains(err.Error(), "needsSpec") {
		t.Fatalf("expected error to name the unknown field, got: %v", err)
	}
}

// TestComponentDesignJSON_ExternalIntentFields_RoundTrip covers the four
// external-only intent fields (style, package, specPath, candidates) added
// alongside the needsSpec removal: an SDK-resolved dep (style+package), an
// ambiguous dep (2+ candidates), and a needs-input dep (no
// fields at all — the agent could not classify it without the user) all
// survive a parse → marshal round trip byte-identically.
func TestComponentDesignJSON_ExternalIntentFields_RoundTrip(t *testing.T) {
	raw := `{
  "name": "checkout",
  "type": "service",
  "dependencies": [
    {
      "kind": "external",
      "name": "stripe",
      "description": "Payments via the stripe Node SDK (secret-key auth).",
      "style": "sdk",
      "package": "npm:stripe@^14"
    },
    {
      "kind": "external",
      "name": "email-provider",
      "description": "Transactional email for signup + reset flows.",
      "candidates": [
        {
          "name": "sendgrid-rest",
          "style": "rest-api",
          "description": "SendGrid v3 Web API"
        },
        {
          "name": "resend-sdk",
          "style": "sdk",
          "description": "Resend Node SDK",
          "package": "npm:resend@^4.0.0"
        }
      ]
    },
    {
      "kind": "external",
      "name": "crm",
      "description": "Needs a CRM; user must name the system + how to authenticate."
    }
  ]
}
`
	comp, err := parseComponentDesignJSON("checkout", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(comp.Dependencies) != 3 {
		t.Fatalf("want 3 deps, got %d", len(comp.Dependencies))
	}

	stripe := comp.Dependencies[0]
	if stripe.Style != DependencyStyleSDK || stripe.Package != "npm:stripe@^14" {
		t.Fatalf("stripe style/package drifted: %+v", stripe)
	}
	email := comp.Dependencies[1]
	if len(email.Candidates) != 2 {
		t.Fatalf("want 2 candidates, got %d: %+v", len(email.Candidates), email.Candidates)
	}
	if email.Candidates[0].Name != "sendgrid-rest" || email.Candidates[0].Style != DependencyStyleRestAPI {
		t.Fatalf("candidate[0] drifted: %+v", email.Candidates[0])
	}
	if email.Candidates[1].Name != "resend-sdk" || email.Candidates[1].Style != DependencyStyleSDK ||
		email.Candidates[1].Package != "npm:resend@^4.0.0" {
		t.Fatalf("candidate[1] drifted: %+v", email.Candidates[1])
	}

	crm := comp.Dependencies[2]
	if crm.Style != "" || crm.Package != "" || len(crm.Candidates) != 0 {
		t.Fatalf("needs-input dep must carry none of the intent fields: %+v", crm)
	}

	out, err := marshalComponentDesignJSON("checkout", comp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != raw {
		t.Fatalf("round-trip not byte-identical:\n--- got ---\n%s\n--- want ---\n%s", out, raw)
	}
}

// TestParseComponentDesignJSON_CandidatesLenientOnDecode documents that the
// disk codec stays a lenient, pure decode (like every other kind-specific
// field): the candidates minItems:2 / kind="external"-only business rules are
// enforced upstream by the write-gates (zod superRefine, the Go fold
// validator) BEFORE a file is ever committed, not re-checked here on read.
func TestParseComponentDesignJSON_CandidatesLenientOnDecode(t *testing.T) {
	raw := `{"name":"checkout","type":"service","dependencies":[{"kind":"component","name":"cart","candidates":[{"name":"a","style":"rest-api"}]}]}`
	comp, err := parseComponentDesignJSON("checkout", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(comp.Dependencies[0].Candidates) != 1 || comp.Dependencies[0].Candidates[0].Name != "a" {
		t.Fatalf("candidates drifted: %+v", comp.Dependencies[0].Candidates)
	}
}

// TestParseComponentDesignJSON_TypePassesThroughVerbatim pins the codec's
// type handling: NO normalization, NO shims. The vocabulary is OpenChoreo's
// own terms (ComponentTypeService / ComponentTypeWebApplication) used
// end-to-end, so the codec maps `type` verbatim in both directions. Older
// spellings ("webapp", "web-app") also pass through untouched — they are
// simply NOT web applications; stored designs carrying them must be migrated
// (a one-line design.json edit).
func TestParseComponentDesignJSON_TypePassesThroughVerbatim(t *testing.T) {
	cases := []string{
		"web-application", // canonical (OC's deployment/web-application)
		"service",         // canonical (OC's deployment/service)
		"webapp",          // retired spelling: verbatim, not a web application
		"web-app",         // retired spelling: verbatim, not a web application
		"scheduled-task",  // unknown kind: verbatim
	}
	for _, diskType := range cases {
		t.Run(diskType, func(t *testing.T) {
			raw := `{"name":"checkout","type":"` + diskType + `","dependencies":[]}`
			comp, err := parseComponentDesignJSON("checkout", raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if comp.ComponentType != diskType {
				t.Fatalf("ComponentType = %q, want verbatim %q", comp.ComponentType, diskType)
			}
			out, err := marshalComponentDesignJSON("checkout", comp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(out), `"type": "`+diskType+`"`) {
				t.Fatalf("re-save must persist the type verbatim (%q):\n%s", diskType, out)
			}
		})
	}
}

func TestSplitAssembleDesign_ComponentRoundTrip(t *testing.T) {
	// End-to-end through the store split/assemble seam: a DesignFile with one
	// component survives Split → Assemble with the design.json codec.
	d := &DesignFile{
		Components: []DesignComponent{
			{
				Name:          "checkout",
				ComponentType: "service",
				Version:       "0.1.0",
				Language:      "Go",
				Description:   "Owns checkout.",
				Dependencies: []Dependency{
					{Kind: DependencyKindComponent, Name: "cart"},
				},
				OpenAPISpec: "openapi: 3.0.3\n",
			},
		},
	}
	files, err := SplitDesign(d)
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	if _, ok := files["components/checkout/design.json"]; !ok {
		t.Fatalf("expected components/checkout/design.json, got keys: %v", keysOf(files))
	}
	if _, ok := files["components/checkout/design.md"]; ok {
		t.Fatalf("component design.md must NOT be written any more")
	}
	if _, ok := files[DesignRootFile]; ok {
		t.Fatalf("SplitDesign must not render the root design.cell — it is authored, not rendered")
	}
	// The cell root is authored, never rendered by SplitDesign — seed it for
	// the assemble half of the trip.
	files[DesignRootFile] = "component checkout service\n"
	out, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	if len(out.Components) != 1 {
		t.Fatalf("want 1 component, got %d", len(out.Components))
	}
	got := out.Components[0]
	if got.Name != "checkout" || got.Version != "0.1.0" || got.Description != "Owns checkout." {
		t.Fatalf("round-trip drifted: %+v", got)
	}
	if got.OpenAPISpec != "openapi: 3.0.3\n" {
		t.Fatalf("openapi.yaml must round-trip as a separate file: %q", got.OpenAPISpec)
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0].Name != "cart" {
		t.Fatalf("dependency round-trip drifted: %+v", got.Dependencies)
	}
}

func TestComponentDesignJSON_SkillsPinnedRoundTrip(t *testing.T) {
	src := `{
  "name": "orders-api",
  "type": "service",
  "version": "0.1.0",
  "language": "Go",
  "buildpack": "docker",
  "appPath": "orders-api",
  "entrypoint": "cmd/main",
  "exposure": "internet",
  "description": "orders",
  "dependencies": [],
  "skillsPinned": ["go", "openapi-conventions"]
}`
	comp, err := parseComponentDesignJSON("orders-api", src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if want := []string{"go", "openapi-conventions"}; !reflect.DeepEqual(comp.SkillsPinned, want) {
		t.Fatalf("parsed skillsPinned = %v, want %v", comp.SkillsPinned, want)
	}
	out, err := marshalComponentDesignJSON("orders-api", comp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"skillsPinned"`) {
		t.Fatalf("marshalled design.json missing skillsPinned: %s", out)
	}
}

// keysOf returns the (unsorted) keys of a file map — a test helper for
// diagnostics when an expected path is missing.
func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The codec must carry the platform-stamped `wiring` in BOTH directions, and the
// byte-identical round-trip above only proves it because the fixture contains one.
// This test names the failure mode directly, because it shipped once: `wiring` was
// added to the model and to both write-gates but NOT to this codec, so every
// derivation was silently discarded on write — design.json came back from a build
// with `exposesAPI.auth` stamped and no wiring at all, and the coding agent was
// left with nothing to copy into workload.yaml.
func TestComponentDesignJSON_CarriesPlatformStampedWiring(t *testing.T) {
	comp, err := parseComponentDesignJSON("checkout", fullComponentDesignJSON)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	byName := map[string]Dependency{}
	for _, d := range comp.Dependencies {
		byName[d.Name] = d
	}

	// READ: a platform-resource's prefixed bindings reach the model.
	db := byName["orders-db"]
	if db.Wiring == nil {
		t.Fatal("read dropped wiring on the platform-resource dependency")
	}
	if db.Wiring.Ref != "shop-orders-db" || db.Wiring.EnvBindings["host"] != "ORDERS_DB_HOST" {
		t.Errorf("platform-resource wiring = %+v", db.Wiring)
	}
	// READ: an external's verbatim config-key bindings reach the model.
	ow := byName["openweather"]
	if ow.Wiring == nil || ow.Wiring.EnvBindings["OPENWEATHER_API_KEY"] != "OPENWEATHER_API_KEY" {
		t.Errorf("external wiring = %+v", ow.Wiring)
	}

	// WRITE: a wiring set on the model reaches the file.
	out, err := marshalComponentDesignJSON("checkout", comp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"wiring"`, `"ref": "shop-orders-db"`, `"host": "ORDERS_DB_HOST"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("write dropped %s:\n%s", want, out)
		}
	}
}

// The endpoints[] variant has to survive the same round trip, and its loss is the
// one that hides: a missing `ref` leaves the coding agent with nothing to write,
// but a missing `endpoint` leaves it free to invent a plausible sibling name — and
// the wrong one builds, deploys and serves while its ReleaseBinding never reaches
// Ready.
func TestComponentDesignJSON_CarriesTheSiblingEndpointWiring(t *testing.T) {
	comp := DesignComponent{
		Name: "todo-webapp", ComponentType: "web-application", Version: "0.1.0",
		Language: "TypeScript", Buildpack: "docker", AppPath: "todo-webapp",
		Dependencies: []Dependency{{
			Kind: DependencyKindComponent, Name: "todo-api",
			Wiring: &DependencyWiring{Endpoint: &EndpointWiring{
				Component:   "todo-api99-todo-api",
				Name:        "http",
				Visibility:  "project",
				EnvBindings: map[string]string{"address": "TODO_API_URL"},
			}},
		}},
	}

	out, err := marshalComponentDesignJSON("todo-webapp", comp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"endpoint"`, `"component": "todo-api99-todo-api"`, `"visibility": "project"`, `"address": "TODO_API_URL"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("write dropped %s:\n%s", want, out)
		}
	}
	// The variants are exclusive on the wire too: an empty `ref` alongside the
	// endpoint is a mixed object, which both write gates reject.
	if strings.Contains(string(out), `"ref"`) {
		t.Errorf("endpoint-variant wiring emitted an empty ref — the write gates reject the mix:\n%s", out)
	}

	back, err := parseComponentDesignJSON("todo-webapp", string(out))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	got := back.Dependencies[0].Wiring
	if got == nil || got.Endpoint == nil {
		t.Fatalf("read dropped the endpoint wiring: %+v", got)
	}
	if got.Endpoint.Component != "todo-api99-todo-api" || got.Endpoint.Name != "http" ||
		got.Endpoint.Visibility != "project" || got.Endpoint.EnvBindings["address"] != "TODO_API_URL" {
		t.Errorf("round-trip changed the endpoint wiring: %+v", got.Endpoint)
	}
}
