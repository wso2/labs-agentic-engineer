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
	"strings"
	"testing"
)

func TestBuildExternalConnectionResourceType_PlainAndSecret(t *testing.T) {
	// OpenWeather shape: a plain base URL + a secret API key.
	rt, err := BuildExternalConnectionResourceType("openweather", []ConnectionConfigKey{
		{Key: "OPENWEATHER_BASE_URL", Secret: false},
		{Key: "OPENWEATHER_API_KEY", Secret: true},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if rt.Metadata.Name != "openweather" || rt.Kind != kindResourceType {
		t.Fatalf("bad metadata: %+v", rt.Metadata)
	}
	if rt.Spec.RetainPolicy != retainPolicyDelete {
		t.Errorf("retainPolicy = %q, want Delete", rt.Spec.RetainPolicy)
	}

	// environmentConfigs schema carries the plain key + the secret store path.
	props, _ := rt.Spec.EnvironmentConfigs.OpenAPIV3Schema["properties"].(map[string]any)
	if _, ok := props["OPENWEATHER_BASE_URL"]; !ok {
		t.Errorf("environmentConfigs missing plain key: %v", props)
	}
	if _, ok := props[SecretStorePathField]; !ok {
		t.Errorf("environmentConfigs missing %s: %v", SecretStorePathField, props)
	}

	// resources: a ConfigMap + an ExternalSecret, both with readyWhen.
	byID := map[string]ResourceTypeManifest{}
	for _, r := range rt.Spec.Resources {
		byID[r.ID] = r
	}
	if len(byID) != 2 {
		t.Fatalf("want 2 resources (config+secret), got %d", len(byID))
	}
	cm := byID[connConfigMapID]
	if cm.ReadyWhen == "" {
		t.Error("ConfigMap missing readyWhen")
	}
	es := byID[connSecretID]
	if !strings.Contains(es.ReadyWhen, "Ready") {
		t.Errorf("ExternalSecret readyWhen should gate on Ready: %q", es.ReadyWhen)
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

func TestBuildExternalConnectionResourceType_AllPlain_NoExternalSecret(t *testing.T) {
	rt, err := BuildExternalConnectionResourceType("plainsvc", []ConnectionConfigKey{
		{Key: "BASE_URL", Secret: false},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Only the ConfigMap (MinItems=1 satisfied); no ExternalSecret.
	if len(rt.Spec.Resources) != 1 || rt.Spec.Resources[0].ID != connConfigMapID {
		t.Fatalf("want only a ConfigMap resource, got %+v", rt.Spec.Resources)
	}
}

func TestBuildExternalConnectionResourceType_Errors(t *testing.T) {
	if _, err := BuildExternalConnectionResourceType("", []ConnectionConfigKey{{Key: "X"}}); err == nil {
		t.Error("want error on empty name")
	}
	if _, err := BuildExternalConnectionResourceType("c", nil); err == nil {
		t.Error("want error on no keys")
	}
}
