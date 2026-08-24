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

package provisioning

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/platform/ocname"
	"github.com/wso2/aep/aep-api/internal/spec"
)

func TestConfigurationReadiness_UsesDesignUnionSchema(t *testing.T) {
	keys := []spec.ConfigKey{
		{Key: "BASE_URL"},
		{Key: "REGION", DefaultValue: "us-east-1"},
		{Key: "API_KEY", Secret: true},
	}
	cases := []struct {
		name       string
		binding    *openchoreo.ResourceReleaseBinding
		designKeys []spec.ConfigKey
		wantState  ExternalDependencyValueState
		wantMiss   []string
	}{
		{name: "absent binding", designKeys: keys, wantState: ValueStateNotProvisioned, wantMiss: []string{"BASE_URL", "REGION", "API_KEY"}},
		{name: "missing key", designKeys: keys, binding: bindingConfig(t, map[string]string{"BASE_URL": "https://api", "REGION": "us-east-1"}), wantState: ValueStateUnset, wantMiss: []string{"API_KEY"}},
		{name: "empty key", designKeys: keys, binding: bindingConfig(t, map[string]string{"BASE_URL": "", "REGION": "us-east-1", openchoreo.SecretStorePathField: "sm://stripe"}), wantState: ValueStateUnset, wantMiss: []string{"BASE_URL"}},
		{name: "defaulted key", designKeys: keys, binding: bindingConfig(t, map[string]string{"BASE_URL": "https://api", "REGION": "us-east-1", openchoreo.SecretStorePathField: "sm://stripe"}), wantState: ValueStateConfigured, wantMiss: []string{}},
		{name: "fully configured", designKeys: keys, binding: bindingConfig(t, map[string]string{"BASE_URL": "https://api", "REGION": "eu-west-1", openchoreo.SecretStorePathField: "sm://stripe"}), wantState: ValueStateConfigured, wantMiss: []string{}},
		{name: "dropped binding key ignored", designKeys: []spec.ConfigKey{{Key: "BASE_URL"}}, binding: bindingConfig(t, map[string]string{"BASE_URL": "https://api", "DROPPED": ""}), wantState: ValueStateConfigured, wantMiss: []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{{Kind: spec.DependencyKindExternal, Name: "stripe", Config: tc.designKeys}}}}}
			bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{}}
			if tc.binding != nil {
				bindings.byName[ocname.ExternalResourceBindingName("proj", "stripe", "development")] = tc.binding
			}
			svc := NewService(Deps{Design: design, Bindings: bindings})
			got, err := svc.ConfigurationReadiness(context.Background(), "acme", "proj", "development")
			if err != nil {
				t.Fatalf("ConfigurationReadiness: %v", err)
			}
			if got.Configured != (tc.wantState == ValueStateConfigured) {
				t.Errorf("Configured = %v", got.Configured)
			}
			if len(got.Dependencies) != 1 {
				t.Fatalf("dependencies = %+v", got.Dependencies)
			}
			dep := got.Dependencies[0]
			if dep.State != tc.wantState || !reflect.DeepEqual(dep.MissingKeys, tc.wantMiss) {
				t.Errorf("readiness = %+v, want state=%q missing=%v", dep, tc.wantState, tc.wantMiss)
			}
		})
	}
}

func TestStatus_ZeroKeyExternalMatchesProjectReadiness(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{{Kind: spec.DependencyKindExternal, Name: "metrics"}}}}}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "metrics", "development"): bindingConfig(t, map[string]string{}),
	}}
	svc := NewService(Deps{Design: design, Bindings: bindings})

	status, err := svc.Status(context.Background(), "acme", "proj", "metrics", "development")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	readiness, err := svc.ConfigurationReadiness(context.Background(), "acme", "proj", "development")
	if err != nil {
		t.Fatalf("ConfigurationReadiness: %v", err)
	}
	if status.ValueState != ValueStateConfigured {
		t.Fatalf("status valueState = %q, want %q", status.ValueState, ValueStateConfigured)
	}
	if len(readiness.Dependencies) != 1 || readiness.Dependencies[0].State != status.ValueState {
		t.Fatalf("project readiness = %+v, status = %+v", readiness, status)
	}
}

func bindingConfig(t *testing.T, values map[string]string) *openchoreo.ResourceReleaseBinding {
	t.Helper()
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	return &openchoreo.ResourceReleaseBinding{Spec: openchoreo.ResourceReleaseBindingSpec{ResourceTypeEnvironmentConfigs: raw}}
}
