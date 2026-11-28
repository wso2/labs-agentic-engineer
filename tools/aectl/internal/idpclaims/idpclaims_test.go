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

package idpclaims

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const repoRoot = "../../../.."

func none(string) (string, Source) { return "", "" }

// A shipped preset holds exactly the claim values of the shared sample-token
// fixture of the same name: the file aep-api, the AE Studio pod and the
// console are tested against. So what aectl installs for an IdP is what its
// readers are proven to read.
func TestPresets_MatchTheSharedFixtures(t *testing.T) {
	for _, name := range Presets() {
		if name == NoPreset {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(repoRoot, "packages/platform-idp-auth/test/fixtures/idp-claims", name+".json"))
		if err != nil {
			t.Fatalf("preset %s has no shared fixture: %v", name, err)
		}
		var fx struct {
			Claims struct {
				Org        struct{ Handle, ID, Name string }
				CallerType struct{ Claim, Machine, User string }
			}
		}
		if err := json.Unmarshal(raw, &fx); err != nil {
			t.Fatal(err)
		}
		want := Claims{
			OrgHandle: fx.Claims.Org.Handle, OrgID: fx.Claims.Org.ID, OrgName: fx.Claims.Org.Name,
			CallerType: fx.Claims.CallerType.Claim, Machine: fx.Claims.CallerType.Machine, User: fx.Claims.CallerType.User,
		}
		got, err := Resolve(name, none)
		if err != nil {
			t.Fatalf("preset %s: %v", name, err)
		}
		if got.Claims() != want {
			t.Errorf("preset %s = %+v, fixture %+v", name, got.Claims(), want)
		}
	}
	if got := strings.Join(Presets(), ","); got != "asgardeo,thunder,none" {
		t.Errorf("Presets() = %s, want asgardeo,thunder,none", got)
	}
}

// The chart's own defaults are the default preset's values, so a chart
// installed without aectl reads ThunderID's tokens the same way.
func TestDefaultPreset_IsTheChartsDefault(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot, "deployments/helm-charts/platform/values.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var values struct {
		IDP struct {
			Claims struct {
				Org        struct{ Handle, ID, Name string }
				CallerType struct{ Claim, Machine, User string } `yaml:"callerType"`
			}
		} `yaml:"idp"`
	}
	if err := yaml.Unmarshal(raw, &values); err != nil {
		t.Fatal(err)
	}
	c := values.IDP.Claims
	chart := Claims{OrgHandle: c.Org.Handle, OrgID: c.Org.ID, OrgName: c.Org.Name, CallerType: c.CallerType.Claim, Machine: c.CallerType.Machine, User: c.CallerType.User}
	got, err := Resolve("", none)
	if err != nil {
		t.Fatal(err)
	}
	if got.Preset != DefaultPreset || got.Claims() != chart {
		t.Fatalf("default preset %q = %+v, chart defaults %+v", got.Preset, got.Claims(), chart)
	}
}

// A value comes from the preset unless a higher layer sets it, and each
// value says which layer it came from. An empty higher value sets nothing.
func TestResolve_LayersOverThePreset(t *testing.T) {
	got, err := Resolve(" Asgardeo ", func(key string) (string, Source) {
		switch key {
		case "org.handle":
			return " tenant_handle ", SourceConfig
		case "caller_type.machine":
			return "SERVICE", SourceConfig
		case "org.id":
			return "  ", SourceConfig
		}
		return "", ""
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Claims{OrgHandle: "tenant_handle", OrgID: "org_id", OrgName: "org_name", CallerType: "aut", Machine: "SERVICE", User: "APPLICATION_USER"}
	if got.Preset != "asgardeo" || got.Claims() != want {
		t.Fatalf("preset %q claims %+v, want asgardeo %+v", got.Preset, got.Claims(), want)
	}
	sources := map[string]Source{}
	for _, v := range got.Values {
		sources[v.Key] = v.Source
	}
	for key, src := range map[string]Source{
		"org.handle": SourceConfig, "caller_type.machine": SourceConfig,
		"org.id": SourcePreset("asgardeo"), "caller_type.user": SourcePreset("asgardeo"),
	} {
		if sources[key] != src {
			t.Errorf("%s came from %q, want %q", key, sources[key], src)
		}
	}
}

// With no preset every required value has to be set: a custom IdP's config
// that leaves one out is an error naming it, never another IdP's name.
func TestResolve_NoPresetRequiresEveryValue(t *testing.T) {
	_, err := Resolve(NoPreset, func(key string) (string, Source) {
		if key == "org.handle" || key == "caller_type.claim" {
			return "x", SourceConfig
		}
		return "", ""
	})
	if err == nil {
		t.Fatal("an incomplete block passed")
	}
	for _, key := range []string{"platform.idp.claims.org.id", "platform.idp.claims.caller_type.machine"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("err %v does not name %s", err, key)
		}
	}
	for _, key := range []string{"platform.idp.claims.org.handle", "platform.idp.claims.org.name", "platform.idp.claims.caller_type.user"} {
		if strings.Contains(err.Error(), key) {
			t.Errorf("err %v names %s, which is set or optional", err, key)
		}
	}

	full, err := Resolve(NoPreset, func(key string) (string, Source) {
		if key == "org.name" || key == "caller_type.user" {
			return "", ""
		}
		return "c_" + key, SourceConfig
	})
	if err != nil {
		t.Fatalf("a complete block with the optional values left out: %v", err)
	}
	if c := full.Claims(); c.OrgName != "" || c.User != "" || c.OrgHandle != "c_org.handle" {
		t.Fatalf("claims %+v", c)
	}
}

func TestResolve_UnknownPresetNamesTheShippedOnes(t *testing.T) {
	_, err := Resolve("okta", none)
	if err == nil || !strings.Contains(err.Error(), "okta") || !strings.Contains(err.Error(), "thunder") {
		t.Fatalf("err = %v, want one naming the unknown preset and the shipped ones", err)
	}
}

// The operator is shown every value with its layer, and what an unset user
// value means.
func TestDescribe(t *testing.T) {
	thunder, err := Resolve("", none)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Join(thunder.Describe(), "\n")
	for _, want := range []string{"platform.idp.claims.org.handle", "ouHandle", "from preset thunder", "platform.idp.claims.caller_type.user", "(none)", "not set", `any grant_type other than "client_credentials" is read as a user's`} {
		if !strings.Contains(lines, want) {
			t.Errorf("description lacks %q:\n%s", want, lines)
		}
	}
	asgardeo, err := Resolve("asgardeo", none)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(asgardeo.Describe(), "\n"), "is read as a user's") {
		t.Error("the unset-user note shows with a user value set")
	}
}
