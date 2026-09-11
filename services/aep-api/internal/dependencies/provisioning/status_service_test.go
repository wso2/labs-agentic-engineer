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
				bindings.byName[ocname.ExternalResourceBindingName("proj", "stripe", "default")] = tc.binding
			}
			svc := NewService(Deps{Design: design, Bindings: bindings})
			got, err := svc.ConfigurationReadiness(context.Background(), "acme", "proj", "default")
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
		ocname.ExternalResourceBindingName("proj", "metrics", "default"): bindingConfig(t, map[string]string{}),
	}}
	svc := NewService(Deps{Design: design, Bindings: bindings})

	status, err := svc.Status(context.Background(), "acme", "proj", "metrics", "default")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	readiness, err := svc.ConfigurationReadiness(context.Background(), "acme", "proj", "default")
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

// ---- the deploy gate (ADR-0023) --------------------------------------------

// TestDeploymentReadiness_SeparatesTheTwoBlockers is the property the run
// depends on: it PARKS on an unconfigured external and POLLS a still-
// provisioning platform resource, so the two must never arrive merged.
func TestDeploymentReadiness_SeparatesTheTwoBlockers(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "API_KEY", Secret: true}}},
		{Kind: spec.DependencyKindExternal, Name: "twilio", Config: []spec.ConfigKey{{Key: "SID"}}},
		{Kind: spec.DependencyKindPlatformResource, Name: "orders-db"},
		{Kind: spec.DependencyKindPlatformResource, Name: "cache"},
	}}}}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		// stripe holds its secret-store path; twilio's plain key is still empty.
		ocname.ExternalResourceBindingName("proj", "stripe", "default"):    bindingConfig(t, map[string]string{openchoreo.SecretStorePathField: "sm://stripe"}),
		ocname.ExternalResourceBindingName("proj", "twilio", "default"):    bindingConfig(t, map[string]string{"SID": ""}),
		ocname.ExternalResourceBindingName("proj", "orders-db", "default"): readyBinding("host"),
		// cache has no binding at all — not provisioned yet.
	}}
	svc := NewService(Deps{Design: design, Bindings: bindings})

	got, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("DeploymentReadiness: %v", err)
	}
	if !reflect.DeepEqual(got.Unconfigured, []string{"twilio"}) {
		t.Errorf("Unconfigured = %v, want [twilio] — stripe holds its secret path", got.Unconfigured)
	}
	if !reflect.DeepEqual(got.Provisioning, []string{"cache"}) {
		t.Errorf("Provisioning = %v, want [cache] — orders-db is Ready", got.Provisioning)
	}
}

// TestDeploymentReadiness_AnExistingBindingThatIsNotReadyStillBlocks. The gate
// reports a platform resource under Provisioning when the binding is absent OR
// present-but-not-Ready, and the two ends of that are already covered above
// (`cache` has none, `orders-db` is Ready). This is the state in between — and
// it is the one the run actually polls THROUGH, because a resource the platform
// is standing up has a binding from the moment it is authored and only reaches
// Ready minutes later. A gate that read an unready binding as ready would
// deploy against a database that is not yet serving.
func TestDeploymentReadiness_AnExistingBindingThatIsNotReadyStillBlocks(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindPlatformResource, Name: "orders-db"},
	}}}}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		// Authored, so it exists — but its Ready condition is still False.
		ocname.ExternalResourceBindingName("proj", "orders-db", "default"): {
			Status: &openchoreo.ResourceReleaseBindingStatus{
				Conditions: []openchoreo.OCCondition{{Type: "Ready", Status: "False"}},
			},
		},
	}}
	svc := NewService(Deps{Design: design, Bindings: bindings})

	got, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("DeploymentReadiness: %v", err)
	}
	if !reflect.DeepEqual(got.Provisioning, []string{"orders-db"}) {
		t.Errorf("Provisioning = %v, want [orders-db] — the binding exists but is not Ready", got.Provisioning)
	}
	if len(got.Unconfigured) != 0 {
		t.Errorf("Unconfigured = %v, want none — a platform resource is never a person's to supply", got.Unconfigured)
	}
}

// TestDeploymentReadiness_OpensWhenEverythingIsConfigured: both lists empty is
// the only answer that deploys, so the fully-configured project has to produce
// exactly that and not, say, an empty-string entry.
func TestDeploymentReadiness_OpensWhenEverythingIsConfigured(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "BASE_URL"}}},
		{Kind: spec.DependencyKindPlatformResource, Name: "orders-db"},
	}}}}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "stripe", "default"):    bindingConfig(t, map[string]string{"BASE_URL": "https://api"}),
		ocname.ExternalResourceBindingName("proj", "orders-db", "default"): readyBinding("host"),
	}}
	svc := NewService(Deps{Design: design, Bindings: bindings})

	got, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("DeploymentReadiness: %v", err)
	}
	if len(got.Unconfigured) != 0 || len(got.Provisioning) != 0 {
		t.Fatalf("readiness = %+v, want both empty", got)
	}
}

// TestDeploymentReadiness_RegisteredExternalIsNotTheProjectsToConfigure is the
// judgement call this method makes, and the one that would deadlock a run if it
// went the other way.
//
// A Registered External holds its values on the ORG record: the project's own
// values endpoint refuses one (SaveValues answers 409 "values live on the org
// record"), and build authoring fills the project binding from the org value
// plane. So an empty project binding for such a name is not evidence that
// anybody in this project has work to do — and naming it would park the run,
// unbounded, on a blocker nobody looking at that project can clear. Build
// preflight already suppresses the external-config item for exactly this reason.
func TestDeploymentReadiness_RegisteredExternalIsNotTheProjectsToConfigure(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindExternal, Name: "openweathermap", Config: []spec.ConfigKey{{Key: "API_KEY", Secret: true}}},
		{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "API_KEY", Secret: true}}},
	}}}}
	// Neither project binding holds a value: the difference is purely WHERE the
	// values live.
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "openweathermap", "default"): bindingConfig(t, map[string]string{}),
		ocname.ExternalResourceBindingName("proj", "stripe", "default"):         bindingConfig(t, map[string]string{}),
	}}
	plane := NewMemoryValuePlane()
	plane.PutEnvCells("acme", "openweathermap", []EnvCell{
		{Environment: "default", Key: "API_KEY", Status: "configured"},
	})
	svc := NewService(Deps{Design: design, Bindings: bindings, CatalogValuePlane: plane})

	got, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("DeploymentReadiness: %v", err)
	}
	if !reflect.DeepEqual(got.Unconfigured, []string{"stripe"}) {
		t.Fatalf("Unconfigured = %v, want only [stripe] — an org-held name is not this project's to supply", got.Unconfigured)
	}
}

// TestDeploymentReadiness_DefaultsTheEnvironment: the run passes no environment
// so the readiness service applies its own default rather than the run package
// pinning an environment name it does not own.
func TestDeploymentReadiness_DefaultsTheEnvironment(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "BASE_URL"}}},
	}}}}
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "stripe", defaultEnv): bindingConfig(t, map[string]string{"BASE_URL": "https://api"}),
	}}
	svc := NewService(Deps{Design: design, Bindings: bindings})

	got, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "")
	if err != nil {
		t.Fatalf("DeploymentReadiness: %v", err)
	}
	if len(got.Unconfigured) != 0 {
		t.Fatalf("Unconfigured = %v, want empty — an empty env must resolve to %q", got.Unconfigured, defaultEnv)
	}
}

// TestDeploymentReadiness_NamesAreOrdered: the run writes this list onto the run
// row and the console renders it verbatim, so an order that shuffled between
// polls would rewrite the row on every pass for no change in fact.
func TestDeploymentReadiness_NamesAreOrdered(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindExternal, Name: "zulip", Config: []spec.ConfigKey{{Key: "URL"}}},
		{Kind: spec.DependencyKindExternal, Name: "algolia", Config: []spec.ConfigKey{{Key: "URL"}}},
		{Kind: spec.DependencyKindExternal, Name: "mailgun", Config: []spec.ConfigKey{{Key: "URL"}}},
	}}}}
	svc := NewService(Deps{Design: design, Bindings: &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{}}})

	got, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("DeploymentReadiness: %v", err)
	}
	if !reflect.DeepEqual(got.Unconfigured, []string{"algolia", "mailgun", "zulip"}) {
		t.Fatalf("Unconfigured = %v, want name order", got.Unconfigured)
	}
}

// TestDeploymentReadiness_AnExternalWithNoDeclaredKeysNeverBlocks. A dependency
// whose union schema is empty has nothing to configure: the console renders no
// row for it — a row exists to collect values — so naming it would park the run,
// unbounded, on a blocker with no surface that could ever clear it. Same
// deadlock shape as a Registered External, from the other direction.
func TestDeploymentReadiness_AnExternalWithNoDeclaredKeysNeverBlocks(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindExternal, Name: "metrics"}, // declares no config keys at all
		{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "BASE_URL"}}},
	}}}}
	// No binding for `metrics` at all: the binding-absent path is the one that
	// reported it not-provisioned regardless of how many keys it declared.
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "stripe", "default"): bindingConfig(t, map[string]string{"BASE_URL": "https://api"}),
	}}
	svc := NewService(Deps{Design: design, Bindings: bindings})

	got, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("DeploymentReadiness: %v", err)
	}
	if len(got.Unconfigured) != 0 {
		t.Fatalf("Unconfigured = %v, want empty — nothing is declared, so nothing can be supplied", got.Unconfigured)
	}
}

// TestExternalValueState_NoDeclaredKeysIsConfigured pins the answer at its
// source, for every caller rather than the gate alone: the state names whether
// every DECLARED key holds a value, which is vacuously true when nothing is
// declared — including when no binding exists, since no binding could hold a
// key that was never asked for.
func TestExternalValueState_NoDeclaredKeysIsConfigured(t *testing.T) {
	for _, tc := range []struct {
		name    string
		binding *openchoreo.ResourceReleaseBinding
	}{
		{name: "no binding"},
		{name: "empty binding", binding: bindingConfig(t, map[string]string{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, missing, err := externalValueState(tc.binding, nil)
			if err != nil {
				t.Fatalf("externalValueState: %v", err)
			}
			if state != ValueStateConfigured {
				t.Errorf("state = %q, want %q", state, ValueStateConfigured)
			}
			if len(missing) != 0 {
				t.Errorf("missing = %v, want none", missing)
			}
		})
	}
}

// TestConfigurationReadiness_ReportsOnlyWhatTheProjectCanSupply is the contract
// the builds page reads: this response enumerates the externals a project member
// can actually act on, and only those.
//
// A Registered External's values live on the ORG record — SaveValues answers 409
// "values live on the org record" — so a row for one offers a Configure button
// that cannot work, and counting it would report the project unconfigured while
// the deploy gate, which skips it, is not blocked at all. The two readiness
// reads have to agree, or the console contradicts the run.
func TestConfigurationReadiness_ReportsOnlyWhatTheProjectCanSupply(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{{Name: "api", Dependencies: []spec.Dependency{
		{Kind: spec.DependencyKindExternal, Name: "openweathermap", Config: []spec.ConfigKey{{Key: "API_KEY", Secret: true}}},
		{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "BASE_URL"}}},
	}}}}
	// Neither project binding holds a value: the difference is purely WHERE the
	// values live.
	bindings := &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{
		ocname.ExternalResourceBindingName("proj", "openweathermap", "default"): bindingConfig(t, map[string]string{}),
		ocname.ExternalResourceBindingName("proj", "stripe", "default"):         bindingConfig(t, map[string]string{"BASE_URL": "https://api"}),
	}}
	plane := NewMemoryValuePlane()
	plane.PutEnvCells("acme", "openweathermap", []EnvCell{
		{Environment: "default", Key: "API_KEY", Status: "configured"},
	})
	svc := NewService(Deps{Design: design, Bindings: bindings, CatalogValuePlane: plane})

	got, err := svc.ConfigurationReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("ConfigurationReadiness: %v", err)
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0].Name != "stripe" {
		t.Fatalf("dependencies = %+v, want only [stripe] — an org-held name is not this project's to supply", got.Dependencies)
	}
	// And the headline agrees with the gate: the only external this project can
	// supply is supplied.
	if !got.Configured {
		t.Fatalf("Configured = false, want true — the org-held name must not hold the project unconfigured")
	}

	gate, err := svc.DeploymentReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("DeploymentReadiness: %v", err)
	}
	if len(gate.Unconfigured) != 0 {
		t.Fatalf("gate Unconfigured = %v, want empty — the two readiness reads must agree", gate.Unconfigured)
	}
}

// TestConfigurationReadiness_EnumeratesEveryProjectSuppliableExternal. The
// console renders rows from this response and defaults nothing, so the list has
// to be derived from the DESIGN — every external declared there, whether or not
// a binding for it exists yet — rather than from the bindings that happen to
// have been authored.
func TestConfigurationReadiness_EnumeratesEveryProjectSuppliableExternal(t *testing.T) {
	design := fakeDesign{comps: []spec.DesignComponent{
		{Name: "api", Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe", Config: []spec.ConfigKey{{Key: "API_KEY", Secret: true}}},
			{Kind: spec.DependencyKindPlatformResource, Name: "orders-db"},
		}},
		{Name: "web", Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "metrics"}, // declares no keys
			{Kind: spec.DependencyKindExternal, Name: "twilio", Config: []spec.ConfigKey{{Key: "SID"}}},
		}},
	}}
	// Not one external binding exists yet — the state right after a design save.
	svc := NewService(Deps{Design: design, Bindings: &fakeBindings{byName: map[string]*openchoreo.ResourceReleaseBinding{}}})

	got, err := svc.ConfigurationReadiness(context.Background(), "acme", "proj", "default")
	if err != nil {
		t.Fatalf("ConfigurationReadiness: %v", err)
	}
	names := make([]string, 0, len(got.Dependencies))
	for _, dep := range got.Dependencies {
		names = append(names, dep.Name)
	}
	if !reflect.DeepEqual(names, []string{"metrics", "stripe", "twilio"}) {
		t.Fatalf("dependencies = %v, want every external the design declares, in name order — platform resources excluded", names)
	}
	for _, dep := range got.Dependencies {
		want := ValueStateNotProvisioned
		if dep.Name == "metrics" {
			want = ValueStateConfigured // nothing declared, so nothing outstanding
		}
		if dep.State != want {
			t.Errorf("%s state = %q, want %q", dep.Name, dep.State, want)
		}
	}
	if got.Configured {
		t.Fatal("Configured = true, want false — stripe and twilio are unset")
	}
}
