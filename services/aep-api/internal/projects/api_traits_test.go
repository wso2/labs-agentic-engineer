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

import (
	"reflect"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
)

func TestAPIConfigurationInstanceName(t *testing.T) {
	cases := []struct {
		in, ep, want string
	}{
		// Empty endpoint name ⇒ the default "http" (prior behavior preserved).
		{"poc-public", "", "poc-public-http"},
		{"user-api", "", "user-api-http"},
		{"  trimmed  ", "", "trimmed-http"},
		{"", "", "component-http"},
		// A design-declared endpoint name is honored verbatim.
		{"user-api", "api", "user-api-api"},
		{"gateway", "  grpc  ", "gateway-grpc"},
	}
	for _, c := range cases {
		if got := APIConfigurationInstanceName(c.in, c.ep); got != c.want {
			t.Errorf("APIConfigurationInstanceName(%q, %q) = %q, want %q", c.in, c.ep, got, c.want)
		}
	}
}

// TestDesiredAPIConfigurationTrait_Enabled — protected component shape.
// Trait carries `endpointName: http`; per-env configs enable cors + jwtAuth.
// This is the schema the AP gateway-runtime expects (see
// deployments/manifests/api-platform/api-configuration-trait.yaml).
func TestDesiredAPIConfigurationTrait_Enabled(t *testing.T) {
	traits, configs := desiredAPIConfigurationTraitForTest("svc", "", true)
	if len(traits) != 1 {
		t.Fatalf("want 1 trait, got %d", len(traits))
	}
	tr := traits[0]
	if tr.InstanceName != "svc-http" || tr.Kind != "ClusterTrait" || tr.Name != "api-configuration" {
		t.Fatalf("unexpected trait shape: %+v", tr)
	}
	if got, want := tr.Parameters["endpointName"], "http"; got != want {
		t.Errorf("endpointName = %v, want %v", got, want)
	}

	cfg, ok := configs["svc-http"]
	if !ok {
		t.Fatalf("missing config for svc-http; got keys %v", keysOfAny(configs))
	}
	jwt, ok := cfg["jwtAuth"].(map[string]interface{})
	if !ok {
		t.Fatalf("jwtAuth missing or wrong type: %#v", cfg["jwtAuth"])
	}
	if jwt["enabled"] != true {
		t.Errorf("jwtAuth.enabled = %v, want true", jwt["enabled"])
	}
	// `issuers` + `audience` default to empty arrays (no per-RestApi
	// filter; trust any cluster-configured keymanager).
	if iss, ok := jwt["issuers"].([]interface{}); !ok {
		t.Errorf("jwtAuth.issuers should be []interface{}, got %T", jwt["issuers"])
	} else if len(iss) != 0 {
		t.Errorf("jwtAuth.issuers should default empty, got %v", iss)
	}
	if aud, ok := jwt["audience"].([]interface{}); !ok {
		t.Errorf("jwtAuth.audience should be []interface{}, got %T", jwt["audience"])
	} else if len(aud) != 0 {
		t.Errorf("jwtAuth.audience should default empty, got %v", aud)
	}
	cors, ok := cfg["cors"].(map[string]interface{})
	if !ok {
		t.Fatalf("cors missing or wrong type: %#v", cfg["cors"])
	}
	if cors["enabled"] != true {
		t.Errorf("cors.enabled = %v, want true", cors["enabled"])
	}
}

// TestDesiredAPIConfigurationTrait_CustomEndpointName — a design-declared
// endpoint name (other than the default "http") must flow into BOTH the trait
// `endpointName` parameter and the trait instance name / config key, so the
// gateway binds to the workload's actual endpoint (no more hardcoded "http").
func TestDesiredAPIConfigurationTrait_CustomEndpointName(t *testing.T) {
	traits, configs := desiredAPIConfigurationTraitForTest("svc", "api", true)
	if len(traits) != 1 {
		t.Fatalf("want 1 trait, got %d", len(traits))
	}
	tr := traits[0]
	if tr.InstanceName != "svc-api" {
		t.Errorf("InstanceName = %q, want %q", tr.InstanceName, "svc-api")
	}
	if got, want := tr.Parameters["endpointName"], "api"; got != want {
		t.Errorf("endpointName = %v, want %v", got, want)
	}
	if _, ok := configs["svc-api"]; !ok {
		t.Fatalf("missing config for svc-api; got keys %v", keysOfAny(configs))
	}
}

// TestDesiredAPIConfigurationTrait_Disabled — public component shape.
// No trait + a tombstone entry in configs so the OC client's merge logic
// removes any previously-set trait instance from each RB.
func TestDesiredAPIConfigurationTrait_Disabled(t *testing.T) {
	traits, configs := desiredAPIConfigurationTraitForTest("svc", "", false)
	if traits != nil {
		t.Fatalf("want nil traits when disabled, got %+v", traits)
	}
	want := map[string]map[string]interface{}{"svc-http": nil}
	if !reflect.DeepEqual(configs, want) {
		t.Fatalf("configs = %#v, want %#v", configs, want)
	}
}

func keysOfAny[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Test_originFromEndpointURL — extracts scheme://authority from a
// ListDeployments-shaped URL with trailing path/query/fragment.
func Test_originFromEndpointURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://host.example:19080/", "http://host.example:19080"},
		{"http://host.example:19080", "http://host.example:19080"},
		{"http://host.example:19080/path/here", "http://host.example:19080"},
		{"https://host.example/abc?q=1", "https://host.example"},
		{"", ""},
		{"not-a-url", ""},
	}
	for _, c := range cases {
		got := originFromEndpointURL(c.in)
		if got != c.want {
			t.Errorf("originFromEndpointURL(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

// desiredAPIConfigurationTraitForTest is the no-issuers, no-origins shape these
// table tests exercise. A test helper rather than a production shim: the only
// production caller is DesiredDeploymentFor, which always has both to pass.
func desiredAPIConfigurationTraitForTest(componentName, endpointName string, enabled bool) ([]openchoreo.ComponentTrait, map[string]map[string]interface{}) {
	return DesiredAPIConfigurationTraitWithIssuers(componentName, endpointName, enabled, nil, nil)
}
