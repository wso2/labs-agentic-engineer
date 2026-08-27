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

package openchoreo

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestBuildExternalResourceType_PlainAndSecret(t *testing.T) {
	t.Parallel()

	keys := []ExternalResourceConfigKey{
		{Key: "OPENWEATHER_BASE_URL", Secret: false, Description: "API base URL", DefaultValue: "https://api.openweathermap.org"},
		{Key: "OPENWEATHER_API_KEY", Secret: true, Description: "API key"},
	}
	// OpenWeather shape: a plain base URL + a secret API key.
	rt, err := BuildExternalResourceType("openweather", "Weather data provider", keys, "", nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	wantName := ExternalResourceRTName("openweather", keys)
	if rt.Metadata.Name != wantName || rt.Kind != kindResourceType {
		t.Fatalf("bad metadata: %+v, want name %q", rt.Metadata, wantName)
	}
	if rt.Spec.RetainPolicy != retainPolicyDelete {
		t.Errorf("retainPolicy = %q, want Delete", rt.Spec.RetainPolicy)
	}

	// metadata carries the logical name + description annotations (the
	// name-hash + version live in metadata.name, not the annotation).
	if rt.Metadata.Annotations[externalNameAnnotation] != "openweather" {
		t.Errorf("external-name annotation = %q", rt.Metadata.Annotations[externalNameAnnotation])
	}
	if rt.Metadata.Annotations[externalDescriptionAnnotation] != "Weather data provider" {
		t.Errorf("description annotation = %q", rt.Metadata.Annotations[externalDescriptionAnnotation])
	}

	// environmentConfigs schema carries the plain key + the secret store path
	// (type-only — no description/default; that lives in spec.parameters).
	props, _ := rt.Spec.EnvironmentConfigs.OpenAPIV3Schema["properties"].(map[string]any)
	if _, ok := props["OPENWEATHER_BASE_URL"]; !ok {
		t.Errorf("environmentConfigs missing plain key: %v", props)
	}
	if _, ok := props[SecretStorePathField]; !ok {
		t.Errorf("environmentConfigs missing %s: %v", SecretStorePathField, props)
	}

	// spec.parameters carries EVERY key (plain + secret) with its description/default.
	params, _ := rt.Spec.Parameters.OpenAPIV3Schema["properties"].(map[string]any)
	baseURLProp, _ := params["OPENWEATHER_BASE_URL"].(map[string]any)
	if baseURLProp["description"] != "API base URL" || baseURLProp["default"] != "https://api.openweathermap.org" {
		t.Errorf("spec.parameters plain key wrong: %v", baseURLProp)
	}
	apiKeyProp, _ := params["OPENWEATHER_API_KEY"].(map[string]any)
	if apiKeyProp["description"] != "API key" {
		t.Errorf("spec.parameters secret key missing description: %v", apiKeyProp)
	}
	if _, hasDefault := apiKeyProp["default"]; hasDefault {
		t.Errorf("spec.parameters secret key should carry no default: %v", apiKeyProp)
	}

	// resources: a ConfigMap + an ExternalSecret, both with readyWhen.
	byID := map[string]ResourceTypeManifest{}
	for _, r := range rt.Spec.Resources {
		byID[r.ID] = r
	}
	if len(byID) != 2 {
		t.Fatalf("want 2 resources (config+secret), got %d", len(byID))
	}
	cm := byID[extResourceConfigMapID]
	if cm.ReadyWhen == "" {
		t.Error("ConfigMap missing readyWhen")
	}
	es := byID[extResourceSecretID]
	if es.ReadyWhen == "" {
		t.Error("ExternalSecret must set readyWhen (an ES-only Resource isn't Ready by default)")
	}
	// ExternalSecret template uses the store-backed pattern.
	var esManifest map[string]any
	if err := json.Unmarshal(es.Template, &esManifest); err != nil {
		t.Fatalf("es template not json: %v", err)
	}
	spec := esManifest["spec"].(map[string]any)
	store := spec["secretStoreRef"].(map[string]any)
	if store["name"] != "${dataplane.secretStore}" || store["kind"] != "ClusterSecretStore" {
		t.Errorf("secretStoreRef wrong: %v", store)
	}
	data := spec["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("want 1 secret data entry, got %d", len(data))
	}
	entry := data[0].(map[string]any)
	if entry["secretKey"] != "OPENWEATHER_API_KEY" {
		t.Errorf("secretKey wrong: %v", entry)
	}
	rref := entry["remoteRef"].(map[string]any)
	if !strings.Contains(rref["key"].(string), SecretStorePathField) || rref["property"] != "OPENWEATHER_API_KEY" {
		t.Errorf("remoteRef wrong: %v", rref)
	}

	// outputs: plain → configMapKeyRef, secret → secretKeyRef.
	out := map[string]ResourceTypeOutput{}
	for _, o := range rt.Spec.Outputs {
		out[o.Name] = o
	}
	if out["OPENWEATHER_BASE_URL"].ConfigMapKeyRef == nil {
		t.Error("plain output should be configMapKeyRef")
	}
	if out["OPENWEATHER_API_KEY"].SecretKeyRef == nil {
		t.Error("secret output should be secretKeyRef")
	}
}

func TestBuildExternalResourceType_AllPlain_NoExternalSecret(t *testing.T) {
	t.Parallel()

	rt, err := BuildExternalResourceType("plainsvc", "", []ExternalResourceConfigKey{
		{Key: "BASE_URL", Secret: false},
	}, "", nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Only the ConfigMap (MinItems=1 satisfied); no ExternalSecret.
	if len(rt.Spec.Resources) != 1 || rt.Spec.Resources[0].ID != extResourceConfigMapID {
		t.Fatalf("want only a ConfigMap resource, got %+v", rt.Spec.Resources)
	}
	// An empty description sets no annotation at all.
	if _, ok := rt.Metadata.Annotations[externalDescriptionAnnotation]; ok {
		t.Errorf("empty description should not set the annotation: %v", rt.Metadata.Annotations)
	}
}

func TestBuildExternalResourceType_Errors(t *testing.T) {
	t.Parallel()

	if _, err := BuildExternalResourceType("", "", []ExternalResourceConfigKey{{Key: "X"}}, "", nil); err == nil {
		t.Error("want error on empty name")
	}
	if _, err := BuildExternalResourceType("r", "", nil, "", nil); err == nil {
		t.Error("want error on no keys")
	}
	if _, err := BuildExternalResourceType("r", "", []ExternalResourceConfigKey{{Key: ""}}, "", nil); err == nil {
		t.Error("want error on empty config key")
	}
}

func TestExternalResourceRTName_PinsTemplateVersion(t *testing.T) {
	t.Parallel()

	keys := []ExternalResourceConfigKey{{Key: "A", Secret: false}}
	got := ExternalResourceRTName("salesforce", keys)
	wantSuffix := fmt.Sprintf("-t%d", ExternalResourceRTTemplateVersion)
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("ExternalResourceRTName = %q, want suffix %q", got, wantSuffix)
	}
	if !strings.HasPrefix(got, "salesforce-") {
		t.Fatalf("ExternalResourceRTName = %q, want prefix %q", got, "salesforce-")
	}
}

func TestExternalResourceRTName_Deterministic(t *testing.T) {
	t.Parallel()

	keys := []ExternalResourceConfigKey{
		{Key: "SF_TOKEN", Secret: true},
		{Key: "SF_REGION", Secret: false},
	}
	// Same schema (even reordered) → same name.
	reordered := []ExternalResourceConfigKey{keys[1], keys[0]}
	if got, want := ExternalResourceRTName("salesforce", keys), ExternalResourceRTName("salesforce", reordered); got != want {
		t.Fatalf("same schema (reordered) produced different names: %q vs %q", got, want)
	}

	// A description/default-only edit does NOT change the name.
	annotated := []ExternalResourceConfigKey{
		{Key: "SF_TOKEN", Secret: true, Description: "API token"},
		{Key: "SF_REGION", Secret: false, DefaultValue: "us-east-1", Description: "deployment region"},
	}
	if got, want := ExternalResourceRTName("salesforce", keys), ExternalResourceRTName("salesforce", annotated); got != want {
		t.Fatalf("description/default-only edit changed the name: %q vs %q", got, want)
	}

	// A changed key mints a different name.
	changedKey := []ExternalResourceConfigKey{
		{Key: "SF_TOKEN", Secret: true},
		{Key: "SF_REGION_V2", Secret: false},
	}
	if got, other := ExternalResourceRTName("salesforce", keys), ExternalResourceRTName("salesforce", changedKey); got == other {
		t.Fatalf("changed key produced the same name: %q", got)
	}

	// A changed secret flag mints a different name.
	changedSecret := []ExternalResourceConfigKey{
		{Key: "SF_TOKEN", Secret: true},
		{Key: "SF_REGION", Secret: true},
	}
	if got, other := ExternalResourceRTName("salesforce", keys), ExternalResourceRTName("salesforce", changedSecret); got == other {
		t.Fatalf("changed secret flag produced the same name: %q", got)
	}
}

func TestExternalDefinitionFromRT_RoundTrips(t *testing.T) {
	t.Parallel()

	keys := []ExternalResourceConfigKey{
		{Key: "SF_REGION", Secret: false, Description: "deployment region", DefaultValue: "us-east-1"},
		{Key: "SF_TOKEN", Secret: true, Description: "API token"},
	}
	rt, err := BuildExternalResourceType("salesforce", "Salesforce CRM", keys, "", nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	def, ok := ExternalDefinitionFromRT(rt)
	if !ok {
		t.Fatalf("ExternalDefinitionFromRT returned ok=false for a freshly authored RT")
	}
	if def.Name != "salesforce" {
		t.Errorf("Name = %q, want salesforce", def.Name)
	}
	if def.Description != "Salesforce CRM" {
		t.Errorf("Description = %q, want %q", def.Description, "Salesforce CRM")
	}
	if len(def.Config) != len(keys) {
		t.Fatalf("Config length = %d, want %d: %+v", len(def.Config), len(keys), def.Config)
	}
	// def.Config is sorted by key; keys is already sorted (SF_REGION < SF_TOKEN).
	for i, want := range keys {
		got := def.Config[i]
		if got.Key != want.Key || got.Secret != want.Secret || got.Description != want.Description || got.DefaultValue != want.DefaultValue {
			t.Errorf("Config[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestExternalDefinitionFromRT_ReadsConsumptionAnnotations(t *testing.T) {
	t.Parallel()

	keys := []ExternalResourceConfigKey{
		{Key: "SF_REGION", Secret: false},
		{Key: "SF_TOKEN", Secret: true},
	}
	docs := []ResourceDoc{
		{Type: "openapi", URL: "https://example.com/openapi.yaml"},
		{Type: "documentation", Path: "docs/README.md"},
	}
	rt, err := BuildExternalResourceType("salesforce", "Salesforce CRM", keys, "Call REST with the token.", docs)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rt.Metadata.Annotations[consumptionInstructionsAnnotation] != "Call REST with the token." {
		t.Fatalf("consumption-instructions annotation = %q", rt.Metadata.Annotations[consumptionInstructionsAnnotation])
	}
	var gotDocs []ResourceDoc
	if err := json.Unmarshal([]byte(rt.Metadata.Annotations[resourceDocsAnnotation]), &gotDocs); err != nil {
		t.Fatalf("resource-docs annotation: %v", err)
	}
	if !reflect.DeepEqual(gotDocs, docs) {
		t.Fatalf("resource-docs pointers = %+v, want %+v (type+url/path only, no spec bodies)", gotDocs, docs)
	}

	empty, err := BuildExternalResourceType("salesforce", "Salesforce CRM", keys, "", nil)
	if err != nil {
		t.Fatalf("build empty: %v", err)
	}
	if _, ok := empty.Metadata.Annotations[consumptionInstructionsAnnotation]; ok {
		t.Fatal("empty consumptionInstructions must omit the annotation")
	}
	if _, ok := empty.Metadata.Annotations[resourceDocsAnnotation]; ok {
		t.Fatal("empty resourceDocs must omit the annotation")
	}

	def, ok := ExternalDefinitionFromRT(rt)
	if !ok {
		t.Fatal("want reconstructable RT")
	}
	if def.ConsumptionInstructions != "Call REST with the token." {
		t.Errorf("ConsumptionInstructions = %q", def.ConsumptionInstructions)
	}
	if len(def.ResourceDocs) != 2 {
		t.Fatalf("ResourceDocs = %+v", def.ResourceDocs)
	}
	if def.ResourceDocs[0].Type != "openapi" || def.ResourceDocs[0].URL != "https://example.com/openapi.yaml" {
		t.Errorf("ResourceDocs[0] = %+v", def.ResourceDocs[0])
	}
	if def.ResourceDocs[1].Type != "documentation" || def.ResourceDocs[1].Path != "docs/README.md" {
		t.Errorf("ResourceDocs[1] = %+v", def.ResourceDocs[1])
	}
	want := ExternalResourceDefinition{
		Name:                    def.Name,
		Description:             def.Description,
		Config:                  def.Config,
		ConsumptionInstructions: def.ConsumptionInstructions,
		ResourceDocs:            def.ResourceDocs,
	}
	if !reflect.DeepEqual(def, want) {
		t.Errorf("ExternalDefinitionFromRT must not invent envCells, got %+v", def)
	}
}

func TestExternalDefinitionFromRT_IgnoresMalformedResourceDocs(t *testing.T) {
	t.Parallel()

	rt, err := BuildExternalResourceType("salesforce", "", []ExternalResourceConfigKey{{Key: "TOKEN", Secret: true}}, "", nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	rt.Metadata.Annotations[resourceDocsAnnotation] = `{not-json`

	def, ok := ExternalDefinitionFromRT(rt)
	if !ok {
		t.Fatal("malformed resource-docs must not fail reconstruction")
	}
	if len(def.ResourceDocs) != 0 {
		t.Errorf("malformed resource-docs must leave ResourceDocs empty, got %+v", def.ResourceDocs)
	}
}

func TestExternalDefinitionFromRT_DropsUnsupportedResourceDocTypes(t *testing.T) {
	t.Parallel()

	rt, err := BuildExternalResourceType("salesforce", "", []ExternalResourceConfigKey{{Key: "TOKEN", Secret: true}}, "", nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	rt.Metadata.Annotations[resourceDocsAnnotation] = `[{"type":"openapi","url":"https://example.com/openapi.yaml"},{"type":"swagger"},{"type":""}]`

	def, ok := ExternalDefinitionFromRT(rt)
	if !ok {
		t.Fatal("want reconstructable RT")
	}
	if len(def.ResourceDocs) != 1 || def.ResourceDocs[0].Type != "openapi" || def.ResourceDocs[0].URL != "https://example.com/openapi.yaml" {
		t.Fatalf("unsupported types must be dropped, got %+v", def.ResourceDocs)
	}
}

func TestExternalDefinitionFromRT_NotOkWhenNotSelfDescribing(t *testing.T) {
	t.Parallel()

	if _, ok := ExternalDefinitionFromRT(nil); ok {
		t.Error("nil RT should not be reconstructable")
	}
	if _, ok := ExternalDefinitionFromRT(&ResourceType{}); ok {
		t.Error("an RT with no external-name annotation should not be reconstructable")
	}
	// external-name present but no spec.parameters (e.g. an RT authored by
	// pre-self-describing code): still not reconstructable.
	rt := &ResourceType{Metadata: OCObjectMeta{Annotations: map[string]string{externalNameAnnotation: "legacy"}}}
	if _, ok := ExternalDefinitionFromRT(rt); ok {
		t.Error("an RT with no spec.parameters should not be reconstructable")
	}
}
