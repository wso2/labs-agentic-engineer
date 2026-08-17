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
	"encoding/json"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The desired-state projection is pure, so the whole matrix is a table rather
// than a cluster round trip. This is where the deploy design's rules are
// pinned — everything downstream just writes what comes out of here.

// designComponent parses a component design.json fixture through the real
// codec, so these cases exercise the same parse the deploy path does.
func designComponent(t *testing.T, body string) spec.DesignComponent {
	t.Helper()
	var c spec.DesignComponent
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	return c
}

func svcJSON(name, auth, caller string) string {
	out := `{"name":"` + name + `","type":"service","description":"d","dependencies":[]`
	if auth != "" {
		out += `,"exposesAPI":{"auth":"` + auth + `"`
		if caller != "" {
			out += `,"callerKind":"` + caller + `"`
		}
		out += `}`
	}
	return out + `}`
}

func TestDesiredDeploymentFor(t *testing.T) {
	t.Parallel()

	const inst = "api-http"

	cases := []struct {
		name string
		body string
		// wantAPITrait: the api-configuration instance is attached.
		wantAPITrait bool
		// wantJWT: the binding carries a jwtAuth config for it.
		wantJWT bool
	}{
		{
			name: "unprotected service gets no api-configuration trait",
			body: svcJSON("api", "", ""),
		},
		{
			name:         "end-user-required uses trait-default wildcard CORS",
			body:         svcJSON("api", "end-user-required", ""),
			wantAPITrait: true,
			wantJWT:      true,
		},
		{
			name:         "service-required is protected but takes no SPA origins",
			body:         svcJSON("api", "service-required", ""),
			wantAPITrait: true,
			wantJWT:      true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DesiredDeploymentFor(DeploymentInputs{
				Component:     designComponent(t, tc.body),
				ComponentName: "api",
				Environment:   openchoreo.DevEnvironmentName,
				ReleaseName:   "rel-1",
			})

			hasTrait := false
			for _, tr := range got.Traits {
				if tr.InstanceName == inst && tr.Name == "api-configuration" {
					hasTrait = true
				}
			}
			if hasTrait != tc.wantAPITrait {
				t.Errorf("api-configuration attached = %v, want %v (traits: %+v)", hasTrait, tc.wantAPITrait, got.Traits)
			}

			cfg, hasCfg := got.Binding.TraitEnvironmentConfigs[inst]
			if hasCfg != tc.wantJWT {
				t.Fatalf("trait env config present = %v, want %v (%+v)", hasCfg, tc.wantJWT, got.Binding.TraitEnvironmentConfigs)
			}
			if !tc.wantJWT {
				return
			}
			if _, ok := cfg["jwtAuth"]; !ok {
				t.Errorf("protected component carries no jwtAuth: %+v", cfg)
			}
			cors, _ := cfg["cors"].(map[string]interface{})
			if _, ok := cors["allowedOrigins"]; ok {
				t.Errorf("allowedOrigins must be omitted (trait default wildcard); got %+v", cors)
			}
		})
	}
}

// The binding always pins and always activates. A composer that left either
// implicit would produce an object OpenChoreo cannot promote.
func TestDesiredDeploymentFor_AlwaysPinsAndActivates(t *testing.T) {
	t.Parallel()
	got := DesiredDeploymentFor(DeploymentInputs{
		Component:     designComponent(t, svcJSON("api", "", "")),
		ComponentName: "api",
		Environment:   openchoreo.DevEnvironmentName,
		ReleaseName:   "rel-7",
	})
	if got.Binding.ReleaseName != "rel-7" {
		t.Errorf("releaseName = %q, want the pin", got.Binding.ReleaseName)
	}
	if got.Binding.State != openchoreo.ReleaseBindingStateActive {
		t.Errorf("state = %q, want Active", got.Binding.State)
	}
	if got.Binding.ComponentName != "api" || got.Binding.Environment != openchoreo.DevEnvironmentName {
		t.Errorf("binding identity wrong: %+v", got.Binding)
	}
}

// Turning a trait OFF needs no tombstone under authoritative-replace: the
// instance is simply absent from the next write. The map must therefore be
// non-nil even when empty, or "no traits" would read as "do not manage traits"
// and a disabled trait's config would survive forever.
func TestDesiredDeploymentFor_EmptyTraitConfigsAreAuthoritativeNotUnmanaged(t *testing.T) {
	t.Parallel()
	got := DesiredDeploymentFor(DeploymentInputs{
		Component:     designComponent(t, svcJSON("api", "", "")),
		ComponentName: "api",
		Environment:   openchoreo.DevEnvironmentName,
		ReleaseName:   "rel-1",
	})
	if got.Binding.TraitEnvironmentConfigs == nil {
		t.Fatal("a component with no traits must produce an EMPTY map, not a nil one")
	}
	if len(got.Binding.TraitEnvironmentConfigs) != 0 {
		t.Errorf("want no live trait configs, got %+v", got.Binding.TraitEnvironmentConfigs)
	}
}

// Env vars and files pass through untouched, including the nil-means-unmanaged
// distinction the binding writer depends on.
func TestDesiredDeploymentFor_CarriesWorkloadOverrides(t *testing.T) {
	t.Parallel()
	in := DeploymentInputs{
		Component:     designComponent(t, svcJSON("api", "", "")),
		ComponentName: "api",
		Environment:   openchoreo.DevEnvironmentName,
		ReleaseName:   "rel-1",
		EnvVars:       []openchoreo.WorkflowEnvVarRef{{Key: "K", Value: "V"}},
		Files:         []openchoreo.WorkflowFileVar{{Key: "env-config.js", MountPath: "/x", Value: "y"}},
	}
	got := DesiredDeploymentFor(in)
	if len(got.Binding.Env) != 1 || got.Binding.Env[0].Key != "K" {
		t.Errorf("env not carried: %+v", got.Binding.Env)
	}
	if len(got.Binding.Files) != 1 || got.Binding.Files[0].Key != "env-config.js" {
		t.Errorf("files not carried: %+v", got.Binding.Files)
	}

	in.EnvVars, in.Files = nil, nil
	got = DesiredDeploymentFor(in)
	if got.Binding.Env != nil || got.Binding.Files != nil {
		t.Errorf("nil inputs must stay nil (unmanaged), got env=%+v files=%+v", got.Binding.Env, got.Binding.Files)
	}
}
