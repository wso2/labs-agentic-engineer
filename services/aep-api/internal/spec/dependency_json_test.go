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
	"strings"
	"testing"
)

// A design with one consumer of two external dependencies: one with its
// directory (definition + contract), one referenced with nothing on disk.
func directoryDesignFiles() map[string]string {
	return map[string]string{
		DesignRootFile: "component api service\n",
		"components/api/design.json": `{
  "name": "api",
  "type": "service",
  "language": "Go",
  "dependencies": [
    {"kind": "external", "name": "stripe", "description": "charges shipping"},
    {"kind": "external", "name": "crm"},
    {"kind": "component", "name": "web"}
  ]
}
`,
		"dependencies/stripe/dependency.json": `{
  "name": "stripe",
  "description": "Payments.",
  "provider": "Stripe",
  "style": "rest-api",
  "contract": "openapi.yaml",
  "provenance": {"sourceUrl": "https://stripe.com/openapi.json", "sliced": true},
  "config": [{"key": "STRIPE_API_KEY", "secret": true}]
}
`,
		"dependencies/stripe/openapi.yaml": "openapi: 3.0.3\ninfo: {title: Stripe, version: '1'}\npaths:\n  /charges:\n    post: {responses: {'201': {description: created}}}\n",
	}
}

func TestAssembleDesign_HydratesTheReferenceFromTheDirectory(t *testing.T) {
	d, err := AssembleDesign(directoryDesignFiles())
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	if len(d.Dependencies) != 1 || d.Dependencies[0].Name != "stripe" {
		t.Fatalf("definitions = %+v, want stripe only (crm has no file)", d.Dependencies)
	}
	byName := map[string]Dependency{}
	for _, dep := range d.Components[0].Dependencies {
		byName[dep.Name] = dep
	}
	stripe := byName["stripe"]
	if stripe.Provider != "Stripe" || stripe.Style != DependencyStyleRestAPI || stripe.Contract != "openapi.yaml" {
		t.Fatalf("stripe not hydrated: %+v", stripe)
	}
	if len(stripe.Config) != 1 || stripe.Config[0].Key != "STRIPE_API_KEY" || !stripe.Config[0].Secret {
		t.Fatalf("stripe config not hydrated: %+v", stripe.Config)
	}
	if stripe.Provenance == nil || stripe.Provenance.SourceURL != "https://stripe.com/openapi.json" {
		t.Fatalf("stripe provenance not hydrated: %+v", stripe.Provenance)
	}
	// The component's own line about the dependency survives; the
	// definition's description does not overwrite it.
	if stripe.Description != "charges shipping" {
		t.Fatalf("component description clobbered: %q", stripe.Description)
	}
	crm := byName["crm"]
	if crm.Provider != "" || crm.Style != "" || crm.Contract != "" {
		t.Fatalf("a reference with no file must hydrate to nothing: %+v", crm)
	}
	if len(d.LegacyCarriers) != 0 {
		t.Fatalf("no legacy carriers expected, got %v", d.LegacyCarriers)
	}
}

func TestAssembleDesign_ContractCountsOnlyWhenTheFileIsThere(t *testing.T) {
	files := directoryDesignFiles()
	delete(files, "dependencies/stripe/openapi.yaml")
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	if got := d.Components[0].Dependencies[0].Contract; got != "" {
		t.Fatalf("a contract name pointing at nothing must not count, got %q", got)
	}
}

// A definition with no provider is unchosen on every surface alike, even with
// a document beside it (one from before providers were named): nothing is
// derived at read time, so the file the view and the agent read agrees with
// the read model, and the user chooses the service.
func TestAssembleDesign_NoProviderIsUnchosenEvenWithADocument(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","style":"rest-api","contract":"openapi.yaml"}`
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	dep := d.Components[0].Dependencies[0]
	if dep.Provider != "" || dep.Contract != "openapi.yaml" {
		t.Fatalf("provider must stay as the file has it, contract still counted: %+v", dep)
	}
	if status, reason := ComputeDependencyStatus(dep, false, OrgServiceHit{}); status != DependencyStatusUnresolved || reason != DependencyReasonNeedsInput {
		t.Fatalf("status = %s/%s, want unresolved/needs-input", status, reason)
	}
}

// A contract written from the provider's own documentation says so in the
// file; the dependency reads resolved and flagged derived, no record needed.
func TestAssembleDesign_DerivedMarker(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/openapi.yaml"] = "openapi: 3.0.3\nx-aep-derived: true\ninfo: {title: Stripe, version: '1'}\npaths: {}\n"
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	dep := d.Components[0].Dependencies[0]
	if !dep.ContractDerived || dep.ContractAssumed {
		t.Fatalf("markers = derived %v assumed %v", dep.ContractDerived, dep.ContractAssumed)
	}
	ApplyDependencyStatus(&dep, false, OrgServiceHit{})
	if dep.Status != DependencyStatusResolved || strings.Join(dep.Flags, ",") != "derived" {
		t.Fatalf("status %s flags %v, want resolved/derived", dep.Status, dep.Flags)
	}
	if a, dv := contractMarkers("# x-aep-derived: true\ntype Query { a: String }"); a || !dv {
		t.Fatalf("graphql comment marker: assumed %v derived %v", a, dv)
	}
}

func TestAssembleDesign_AssumedMarkerAndAcceptance(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/openapi.yaml"] = "openapi: 3.0.3\nx-aep-assumed: true\ninfo: {title: Stripe, version: '1'}\npaths:\n  /charges:\n    post: {responses: {'201': {description: created}}}\n"
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	stripe := d.Components[0].Dependencies[0]
	if !stripe.ContractAssumed || stripe.Assumed != nil {
		t.Fatalf("an agent-written contract must read as assumed and unaccepted: %+v", stripe)
	}
	if status, reason := ComputeDependencyStatus(stripe, false, OrgServiceHit{}); status != DependencyStatusUnresolved || reason != DependencyReasonNeedsAcceptance {
		t.Fatalf("status = %s/%s, want unresolved/needs-acceptance", status, reason)
	}
	// A JSON contract carries the same marker; a GraphQL schema carries it as a comment.
	if !contractMarkedAssumed(`{"openapi":"3.0.3","x-aep-assumed":true,"paths":{}}`) {
		t.Fatalf("JSON marker not read")
	}
	if !contractMarkedAssumed("# x-aep-assumed: true\ntype Query { ping: String }\n") {
		t.Fatalf("GraphQL comment marker not read")
	}
	if contractMarkedAssumed("openapi: 3.0.3\ninfo: {title: x, version: '1'}\npaths: {}\n") {
		t.Fatalf("an unmarked document must not read as assumed")
	}
}

func TestAssembleDesign_SdkManifestPicksTheComponentsLanguage(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","provider":"Stripe","style":"sdk","sdk":"sdk.json"}`
	files["dependencies/stripe/sdk.json"] = `{"packages":{"go":"go:github.com/stripe/stripe-go/v79","typescript":"npm:stripe@^14"}}`
	delete(files, "dependencies/stripe/openapi.yaml")
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	stripe := d.Components[0].Dependencies[0]
	if stripe.SDK != "sdk.json" || stripe.Package != "go:github.com/stripe/stripe-go/v79" {
		t.Fatalf("sdk hydration drifted: %+v", stripe)
	}
	if flags := ComputeDependencyFlags(stripe, false); len(flags) != 1 || flags[0] != DependencyFlagSDKOnly {
		t.Fatalf("flags = %v, want [sdk-only]", flags)
	}
}

// A design from before the directory existed: the definition fields still sit
// on the component. Assembly lifts them into an in-memory definition, names
// the component as a legacy carrier, and a split writes the directory while
// the component becomes a bare reference.
func TestAssembleDesign_LiftsLegacyFieldsAndSplitWritesTheDirectory(t *testing.T) {
	files := map[string]string{
		DesignRootFile: "component api service\ncomponent worker service\n",
		"components/api/design.json": `{"name":"api","type":"service","dependencies":[
  {"kind":"external","name":"stripe","style":"rest-api","specPath":"https://stripe.com/openapi.json",
   "config":[{"key":"STRIPE_API_KEY","secret":true}]}]}`,
		"components/worker/design.json": `{"name":"worker","type":"service","dependencies":[
  {"kind":"external","name":"stripe","config":[{"key":"STRIPE_WEBHOOK_SECRET","secret":true},{"key":"STRIPE_API_KEY"}]}]}`,
	}
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	if len(d.Dependencies) != 1 {
		t.Fatalf("want one lifted definition, got %+v", d.Dependencies)
	}
	def := d.Dependencies[0]
	if def.Name != "stripe" || def.Style != DependencyStyleRestAPI || def.Provider != "" || def.Contract != "" {
		t.Fatalf("lifted definition drifted (no invented provider, no invented contract): %+v", def)
	}
	if def.Provenance == nil || def.Provenance.SourceURL != "https://stripe.com/openapi.json" {
		t.Fatalf("legacy specPath must become provenance: %+v", def.Provenance)
	}
	keys := []string{}
	for _, k := range def.Config {
		keys = append(keys, k.Key)
	}
	if strings.Join(keys, ",") != "STRIPE_API_KEY,STRIPE_WEBHOOK_SECRET" || !def.Config[0].Secret {
		t.Fatalf("config keys must union across the carriers, secret winning: %+v", def.Config)
	}
	if strings.Join(d.LegacyCarriers, ",") != "api,worker" {
		t.Fatalf("legacy carriers = %v", d.LegacyCarriers)
	}
	// Status reads off the lifted definition: the legacy shape named no
	// service and has no document to name one, so the user chooses next.
	if status, reason := ComputeDependencyStatus(d.Components[0].Dependencies[0], false, OrgServiceHit{}); status != DependencyStatusUnresolved || reason != DependencyReasonNeedsInput {
		t.Fatalf("status = %s/%s, want unresolved/needs-input", status, reason)
	}

	out, err := SplitDesign(d)
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	defFile, ok := out["dependencies/stripe/dependency.json"]
	if !ok {
		t.Fatalf("split must write the lifted definition; keys: %v", keysOf(out))
	}
	for _, want := range []string{`"name": "stripe"`, `"style": "rest-api"`, `"STRIPE_WEBHOOK_SECRET"`, `"sourceUrl": "https://stripe.com/openapi.json"`} {
		if !strings.Contains(defFile, want) {
			t.Errorf("dependency.json missing %s:\n%s", want, defFile)
		}
	}
	for _, comp := range []string{"api", "worker"} {
		body := out["components/"+comp+"/design.json"]
		if strings.Contains(body, `"style"`) || strings.Contains(body, `"specPath"`) || strings.Contains(body, `"config"`) {
			t.Errorf("%s must be a bare reference after the split:\n%s", comp, body)
		}
	}
}

// A definition on disk is the truth: legacy fields still on a component never
// union into it, and the component is only named as a carrier to be stripped.
func TestAssembleDesign_OnDiskDefinitionWinsOverLegacyCarry(t *testing.T) {
	files := directoryDesignFiles()
	files["components/api/design.json"] = `{"name":"api","type":"service","dependencies":[
  {"kind":"external","name":"stripe","style":"sdk","config":[{"key":"STALE_KEY"}]}]}`
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	def := d.Dependencies[0]
	if def.Style != DependencyStyleRestAPI || len(def.Config) != 1 || def.Config[0].Key != "STRIPE_API_KEY" {
		t.Fatalf("on-disk definition altered by legacy carry: %+v", def)
	}
	if strings.Join(d.LegacyCarriers, ",") != "api" {
		t.Fatalf("legacy carriers = %v", d.LegacyCarriers)
	}
	if got := d.Components[0].Dependencies[0]; got.Style != DependencyStyleRestAPI || got.Config[0].Key != "STRIPE_API_KEY" {
		t.Fatalf("the edge must hydrate from the file, not the stale carry: %+v", got)
	}
}

func TestDependencyDefinitionJSON_RoundTripAndStrictness(t *testing.T) {
	raw := `{
  "name": "stripe",
  "description": "Payments.",
  "source": "project",
  "provider": "Stripe",
  "style": "rest-api",
  "contract": "openapi.yaml",
  "provenance": {
    "sourceUrl": "https://stripe.com/openapi.json",
    "sha256": "abc",
    "fetchedAt": "2026-09-08T10:00:00Z",
    "sliced": true
  },
  "config": [
    {
      "key": "STRIPE_API_KEY",
      "secret": true
    }
  ],
  "assumed": {
    "by": "admin",
    "at": "2026-09-08T10:15:00Z",
    "note": "auth guessed"
  }
}
`
	def, err := parseDependencyDefinitionJSON("stripe", raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := marshalDependencyDefinitionJSON("stripe", def)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != raw {
		t.Fatalf("round-trip not byte-identical:\n--- got ---\n%s\n--- want ---\n%s", out, raw)
	}
	if _, err := parseDependencyDefinitionJSON("stripe", `{"name":"stripe","status":"resolved"}`); err == nil {
		t.Fatalf("read-time state must be rejected on read")
	}
	if _, err := parseDependencyDefinitionJSON("payments", raw); err == nil {
		t.Fatalf("name must equal the directory")
	}
}
