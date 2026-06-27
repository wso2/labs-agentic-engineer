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

package component

import (
	"reflect"
	"testing"
)

func TestAPIConfigurationInstanceName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"poc-public", "poc-public-http"},
		{"user-api", "user-api-http"},
		{"  trimmed  ", "trimmed-http"},
		{"", "component-http"},
	}
	for _, c := range cases {
		if got := APIConfigurationInstanceName(c.in); got != c.want {
			t.Errorf("APIConfigurationInstanceName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDesiredAPIConfigurationTrait_Enabled — protected component shape.
// Trait carries `endpointName: http`; per-env configs enable jwtAuth ONLY
// (CORS has been removed — web-apps reach backends same-origin via the nginx
// `/api/*` proxy). This is the schema the AP gateway-runtime expects (see
// deployments/manifests/api-platform/api-configuration-trait.yaml).
func TestDesiredAPIConfigurationTrait_Enabled(t *testing.T) {
	traits, configs := DesiredAPIConfigurationTrait("svc", true)
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
	// CORS has been removed: the trait config must carry no `cors` key.
	if _, ok := cfg["cors"]; ok {
		t.Errorf("cors must not be present after CORS removal; got %#v", cfg["cors"])
	}
}

// TestDesiredAPIConfigurationTrait_Disabled — public component shape.
// No trait + a tombstone entry in configs so the OC client's merge logic
// removes any previously-set trait instance from each RB.
func TestDesiredAPIConfigurationTrait_Disabled(t *testing.T) {
	traits, configs := DesiredAPIConfigurationTrait("svc", false)
	if traits != nil {
		t.Fatalf("want nil traits when disabled, got %+v", traits)
	}
	want := map[string]map[string]interface{}{"svc-http": nil}
	if !reflect.DeepEqual(configs, want) {
		t.Fatalf("configs = %#v, want %#v", configs, want)
	}
}

// TestDesiredAPIConfigurationTrait_PublicNoAuthNoTrait — after CORS removal a
// PUBLIC, no-auth service (authEnabled=false) gets NO api-configuration trait:
// browsers reach it same-origin via the web-app's nginx `/api/*` proxy, so
// there is no cross-origin CORS to emit and no auth filter to attach. The
// config carries only the tombstone entry that strips any prior instance.
func TestDesiredAPIConfigurationTrait_PublicNoAuthNoTrait(t *testing.T) {
	traits, configs := DesiredAPIConfigurationTraitWithIssuers("svc", false, nil)
	if traits != nil {
		t.Fatalf("want nil traits for a public no-auth service, got %+v", traits)
	}
	want := map[string]map[string]interface{}{"svc-http": nil}
	if !reflect.DeepEqual(configs, want) {
		t.Fatalf("configs = %#v, want %#v (tombstone only)", configs, want)
	}
}

func keysOfAny[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
