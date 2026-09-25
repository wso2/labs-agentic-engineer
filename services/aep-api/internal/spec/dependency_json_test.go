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
  "resource": {
    "name": "stripe",
    "description": "Payments.",
    "provider": "Stripe",
    "config": [{"key": "STRIPE_API_KEY", "secret": true}],
    "contract": {"type": "openapi", "path": "openapi.yaml", "origin": "provider"}
  },
  "provenance": {"sourceUrl": "https://stripe.com/openapi.json"}
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
	if stripe.Provider != "Stripe" || stripe.Contract != "openapi.yaml" || stripe.ContractType != DependencyContractTypeOpenAPI {
		t.Fatalf("stripe not hydrated: %+v", stripe)
	}
	// Style is computed from the contract type, never read from the file.
	if stripe.Style != DependencyStyleRestAPI || stripe.ContractOrigin != DependencyContractOriginProvider || stripe.Source != DependencySourceProject {
		t.Fatalf("computed fields drifted: style %q origin %q source %q", stripe.Style, stripe.ContractOrigin, stripe.Source)
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

// A copy of a registered resource hydrates its ref, the organization's
// instructions and the copied keys; Source reads org.
func TestAssembleDesign_HydratesARegistryCopy(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{
  "name": "stripe",
  "resource": {
    "ref": "stripe",
    "name": "stripe",
    "description": "Payments.",
    "provider": "Stripe",
    "config": [{"key": "STRIPE_API_KEY", "secret": true}],
    "consumptionInstructions": "One client per component; never log the key.",
    "contract": {"type": "openapi", "path": "openapi.yaml", "origin": "registry"}
  },
  "provenance": {"registry": "stripe/openapi.yaml", "sha256": "` + strings.Repeat("a", 64) + `", "readOn": "2026-09-17T10:00:00Z"}
}
`
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	dep := d.Components[0].Dependencies[0]
	if dep.ResourceRef != "stripe" || dep.Source != DependencySourceOrg || dep.ConsumptionInstructions == "" || dep.ContractOrigin != DependencyContractOriginRegistry {
		t.Fatalf("copy not hydrated: %+v", dep)
	}
	ApplyDependencyStatus(&dep, RegistryHit{Registered: true, DocumentSHA256: strings.Repeat("a", 64)}, OrgServiceHit{})
	if dep.Status != DependencyStatusResolved || strings.Join(dep.Flags, ",") != "registered" {
		t.Fatalf("status %s flags %v, want resolved/registered", dep.Status, dep.Flags)
	}
	// A ref must be the dependency's own name.
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"ref":"payments","name":"stripe"}}`
	if _, err := AssembleDesign(files); err == nil {
		t.Fatalf("a ref that is not the dependency name must be refused")
	}
}

func TestAssembleDesign_ContractCountsOnlyWhenTheFileIsThere(t *testing.T) {
	files := directoryDesignFiles()
	delete(files, "dependencies/stripe/openapi.yaml")
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	dep := d.Components[0].Dependencies[0]
	if dep.Contract != "" {
		t.Fatalf("a contract path pointing at nothing must not count, got %q", dep.Contract)
	}
	// The type is still known (the definition names it), so style still reads.
	if dep.Style != DependencyStyleRestAPI {
		t.Fatalf("style must still compute from the declared type, got %q", dep.Style)
	}
}

// A definition with no provider is unchosen on every surface alike, even with
// a document beside it: nothing is derived at read time, so the file the view
// and the agent read agrees with the read model, and the user chooses the
// service.
func TestAssembleDesign_NoProviderIsUnchosenEvenWithADocument(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"name":"stripe","contract":{"type":"openapi","path":"openapi.yaml"}}}`
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	dep := d.Components[0].Dependencies[0]
	if dep.Provider != "" || dep.Contract != "openapi.yaml" {
		t.Fatalf("provider must stay as the file has it, contract still counted: %+v", dep)
	}
	if status, reason := ComputeDependencyStatus(dep, RegistryHit{}, OrgServiceHit{}); status != DependencyStatusUnresolved || reason != DependencyReasonNeedsInput {
		t.Fatalf("status = %s/%s, want unresolved/needs-input", status, reason)
	}
}

// The contract's origin is on the contract object; the dependency reads
// resolved and flagged derived, no record needed.
func TestAssembleDesign_DerivedOrigin(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"name":"stripe","provider":"Stripe","contract":{"type":"openapi","path":"openapi.yaml","origin":"derived"}}}`
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	dep := d.Components[0].Dependencies[0]
	if !dep.ContractDerived || dep.ContractAssumed {
		t.Fatalf("origin = derived %v assumed %v", dep.ContractDerived, dep.ContractAssumed)
	}
	ApplyDependencyStatus(&dep, RegistryHit{}, OrgServiceHit{})
	if dep.Status != DependencyStatusResolved || strings.Join(dep.Flags, ",") != "derived" {
		t.Fatalf("status %s flags %v, want resolved/derived", dep.Status, dep.Flags)
	}
}

// A file written before origin existed says it in the contract body; the
// markers are read only as that fallback.
func TestAssembleDesign_FileMarkersAreTheFallbackForNoOrigin(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"name":"stripe","provider":"Stripe","contract":{"type":"openapi","path":"openapi.yaml"}}}`
	files["dependencies/stripe/openapi.yaml"] = "openapi: 3.0.3\nx-aep-derived: true\ninfo: {title: Stripe, version: '1'}\npaths: {}\n"
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	if dep := d.Components[0].Dependencies[0]; !dep.ContractDerived {
		t.Fatalf("marker fallback not read: %+v", dep)
	}
	// An origin on the object wins over a contradicting marker.
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"name":"stripe","provider":"Stripe","contract":{"type":"openapi","path":"openapi.yaml","origin":"provider"}}}`
	d, _ = AssembleDesign(files)
	if dep := d.Components[0].Dependencies[0]; dep.ContractDerived {
		t.Fatalf("origin on the object must win over the file marker: %+v", dep)
	}
	if a, dv := contractMarkers("# x-aep-derived: true\ntype Query { a: String }"); a || !dv {
		t.Fatalf("graphql comment marker: assumed %v derived %v", a, dv)
	}
}

func TestAssembleDesign_AssumedOriginAndAcceptance(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"name":"stripe","provider":"Stripe","contract":{"type":"openapi","path":"openapi.yaml","origin":"assumed"}}}`
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	stripe := d.Components[0].Dependencies[0]
	if !stripe.ContractAssumed || stripe.Assumed != nil {
		t.Fatalf("an agent-written contract must read as assumed and unaccepted: %+v", stripe)
	}
	if status, reason := ComputeDependencyStatus(stripe, RegistryHit{}, OrgServiceHit{}); status != DependencyStatusUnresolved || reason != DependencyReasonNeedsAcceptance {
		t.Fatalf("status = %s/%s, want unresolved/needs-acceptance", status, reason)
	}
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"name":"stripe","provider":"Stripe","contract":{"type":"openapi","path":"openapi.yaml","origin":"assumed","accepted":{"by":"admin","at":"2026-09-17T10:00:00Z"}}}}`
	d, _ = AssembleDesign(files)
	stripe = d.Components[0].Dependencies[0]
	if stripe.Assumed == nil || stripe.Assumed.By != "admin" {
		t.Fatalf("acceptance not hydrated: %+v", stripe)
	}
	if status, _ := ComputeDependencyStatus(stripe, RegistryHit{}, OrgServiceHit{}); status != DependencyStatusResolved {
		t.Fatalf("an accepted assumption resolves, got %s", status)
	}
}

func TestAssembleDesign_SdkContractPicksTheComponentsLanguage(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"name":"stripe","provider":"Stripe","contract":{"type":"sdk","path":"sdk.json"}}}`
	files["dependencies/stripe/sdk.json"] = `{"packages":{"go":"go:github.com/stripe/stripe-go/v79","typescript":"npm:stripe@^14"}}`
	delete(files, "dependencies/stripe/openapi.yaml")
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	stripe := d.Components[0].Dependencies[0]
	if stripe.SDK != "sdk.json" || stripe.Package != "go:github.com/stripe/stripe-go/v79" || stripe.Style != DependencyStyleSDK {
		t.Fatalf("sdk hydration drifted: %+v", stripe)
	}
	if flags := ComputeDependencyFlags(stripe, RegistryHit{}); len(flags) != 1 || flags[0] != DependencyFlagSDKOnly {
		t.Fatalf("flags = %v, want [sdk-only]", flags)
	}
}

// A manifest that names packages for other languages only is not this
// component's contract: it must read needs-contract rather than resolved with
// nothing for the coding agent to install.
func TestAssembleDesign_SdkManifestWithoutTheComponentsLanguageIsNoContract(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","resource":{"name":"stripe","provider":"Stripe","contract":{"type":"sdk","path":"sdk.json"}}}`
	files["dependencies/stripe/sdk.json"] = `{"packages":{"python":"pypi:stripe"}}`
	delete(files, "dependencies/stripe/openapi.yaml")
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	stripe := d.Components[0].Dependencies[0] // the component is Go
	if stripe.SDK != "" || stripe.Package != "" {
		t.Fatalf("a manifest for another language is not this component's SDK: %+v", stripe)
	}
	status, _ := ComputeDependencyStatus(stripe, RegistryHit{}, OrgServiceHit{})
	if status != DependencyStatusUnresolved {
		t.Fatalf("status = %s, want unresolved (needs-contract)", status)
	}
}

// The previous FLAT file shape still reads: it is lifted into the nested one
// in memory, and the next split writes the new shape.
func TestAssembleDesign_LiftsTheFlatFileShape(t *testing.T) {
	files := directoryDesignFiles()
	files["dependencies/stripe/dependency.json"] = `{
  "name": "stripe",
  "description": "Payments.",
  "source": "org",
  "provider": "Stripe",
  "style": "rest-api",
  "contract": "openapi.yaml",
  "provenance": {"sourceUrl": "https://stripe.com/openapi.json", "sha256": "` + strings.Repeat("b", 64) + `", "fetchedAt": "2026-09-08T10:00:00Z", "sliced": true},
  "config": [{"key": "STRIPE_API_KEY", "secret": true}],
  "assumed": {"by": "admin", "at": "2026-09-08T10:15:00Z", "note": "auth guessed"}
}
`
	d, err := AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign: %v", err)
	}
	def := d.Dependencies[0]
	if def.Resource.Ref != "stripe" || def.Resource.Provider != "Stripe" || def.Resource.Description != "Payments." {
		t.Fatalf("flat fields not lifted into the resource block: %+v", def.Resource)
	}
	if c := def.Resource.Contract; c == nil || c.Type != DependencyContractTypeOpenAPI || c.Path != "openapi.yaml" || c.Accepted == nil || c.Accepted.By != "admin" {
		t.Fatalf("style/contract/assumed not lifted onto the contract: %+v", def.Resource.Contract)
	}
	if def.Provenance == nil || def.Provenance.SourceURL != "https://stripe.com/openapi.json" || def.Provenance.ReadOn != "2026-09-08T10:00:00Z" {
		t.Fatalf("provenance not lifted (fetchedAt → readOn, sliced dropped): %+v", def.Provenance)
	}
	out, err := SplitDesign(d)
	if err != nil {
		t.Fatalf("SplitDesign: %v", err)
	}
	written := out["dependencies/stripe/dependency.json"]
	for _, want := range []string{`"resource": {`, `"ref": "stripe"`, `"contract": {`, `"accepted": {`, `"readOn": "2026-09-08T10:00:00Z"`} {
		if !strings.Contains(written, want) {
			t.Errorf("the next write must carry the nested shape, missing %s:\n%s", want, written)
		}
	}
	for _, gone := range []string{`"style"`, `"source"`, `"sliced"`, `"fetchedAt"`} {
		if strings.Contains(written, gone) {
			t.Errorf("the next write must drop the retired field %s:\n%s", gone, written)
		}
	}
	// A flat sdk file: the manifest is the contract.
	files["dependencies/stripe/dependency.json"] = `{"name":"stripe","provider":"Stripe","style":"sdk","sdk":"sdk.json"}`
	d, err = AssembleDesign(files)
	if err != nil {
		t.Fatalf("AssembleDesign sdk: %v", err)
	}
	if c := d.Dependencies[0].Resource.Contract; c == nil || c.Type != DependencyContractTypeSDK || c.Path != "sdk.json" {
		t.Fatalf("flat sdk not lifted: %+v", c)
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
	if def.Name != "stripe" || def.Resource.Provider != "" || def.Resource.Contract != nil {
		t.Fatalf("lifted definition drifted (no invented provider, no invented contract): %+v", def)
	}
	if def.Provenance == nil || def.Provenance.SourceURL != "https://stripe.com/openapi.json" {
		t.Fatalf("legacy specPath must become provenance: %+v", def.Provenance)
	}
	keys := []string{}
	for _, k := range def.Resource.Config {
		keys = append(keys, k.Key)
	}
	if strings.Join(keys, ",") != "STRIPE_API_KEY,STRIPE_WEBHOOK_SECRET" || !def.Resource.Config[0].Secret {
		t.Fatalf("config keys must union across the carriers, secret winning: %+v", def.Resource.Config)
	}
	if strings.Join(d.LegacyCarriers, ",") != "api,worker" {
		t.Fatalf("legacy carriers = %v", d.LegacyCarriers)
	}
	// Status reads off the lifted definition: the legacy shape named no
	// service and has no document to name one, so the user chooses next.
	if status, reason := ComputeDependencyStatus(d.Components[0].Dependencies[0], RegistryHit{}, OrgServiceHit{}); status != DependencyStatusUnresolved || reason != DependencyReasonNeedsInput {
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
	for _, want := range []string{`"name": "stripe"`, `"resource": {`, `"STRIPE_WEBHOOK_SECRET"`, `"sourceUrl": "https://stripe.com/openapi.json"`} {
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
	if def.Resource.Contract.Type != DependencyContractTypeOpenAPI || len(def.Resource.Config) != 1 || def.Resource.Config[0].Key != "STRIPE_API_KEY" {
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
  "resource": {
    "ref": "stripe",
    "name": "stripe",
    "description": "Payments.",
    "provider": "Stripe",
    "config": [
      {
        "key": "STRIPE_API_KEY",
        "secret": true
      }
    ],
    "contract": {
      "type": "openapi",
      "path": "openapi.yaml",
      "origin": "registry",
      "accepted": {
        "by": "admin",
        "at": "2026-09-08T10:15:00Z",
        "note": "auth guessed"
      }
    },
    "consumptionInstructions": "Never log the key."
  },
  "provenance": {
    "registry": "stripe/openapi.yaml",
    "sha256": "abc",
    "readOn": "2026-09-08T10:00:00Z"
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
	if _, err := parseDependencyDefinitionJSON("stripe", `{"name":"stripe","resource":{"name":"stripe"},"status":"resolved"}`); err == nil {
		t.Fatalf("read-time state must be rejected on read")
	}
	if _, err := parseDependencyDefinitionJSON("stripe", `{"name":"stripe","resource":{"name":"stripe","contract":{"type":"openapi","url":"https://x"}}}`); err == nil {
		t.Fatalf("a URL form of contract must be rejected: the coding agent follows no link")
	}
	if _, err := parseDependencyDefinitionJSON("payments", raw); err == nil {
		t.Fatalf("name must equal the directory")
	}
	if _, err := parseDependencyDefinitionJSON("stripe", `{"name":"stripe","resource":{"name":"payments"}}`); err == nil {
		t.Fatalf("resource.name must equal the dependency name")
	}
	// Anything after the document is a malformed file, including a stray
	// closing delimiter — which reads as "no more elements", not as trailing
	// content, to a decoder that only asks whether another element follows.
	for _, trailing := range []string{
		`{"name":"stripe","resource":{"name":"stripe"}}}`,
		`{"name":"stripe","resource":{"name":"stripe"}}]`,
		`{"name":"stripe","resource":{"name":"stripe"}} {"name":"other"}`,
		`{"name":"stripe","resource":{"name":"stripe"}} garbage`,
	} {
		if _, err := parseDependencyDefinitionJSON("stripe", trailing); err == nil {
			t.Fatalf("trailing content must be rejected: %s", trailing)
		}
	}
}
