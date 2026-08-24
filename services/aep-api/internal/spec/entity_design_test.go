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
	"encoding/json"
	"reflect"
	"testing"
)

// mixedDeps is a component whose dependencies span every kind, used to prove
// the kind-filtering helpers select the right subset.
func mixedDeps() DesignComponent {
	return DesignComponent{
		Name: "checkout",
		Dependencies: []Dependency{
			{Kind: DependencyKindComponent, Name: "cart"},
			{Kind: DependencyKindOrgService, Name: "billing"},
			{Kind: DependencyKindExternal, Name: "stripe"},
			{Kind: DependencyKindComponent, Name: "inventory"},
			{Kind: DependencyKindPlatformResource, Name: "orders-db"},
			{Kind: DependencyKindOrgService, Name: "identity"},
		},
	}
}

func TestComponentDependsOn_FiltersByKind(t *testing.T) {
	got := mixedDeps().ComponentDependsOn()
	want := []string{"cart", "inventory"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComponentDependsOn() = %v, want %v", got, want)
	}
}

func TestOrgServiceDependsOn_FiltersByKind(t *testing.T) {
	got := mixedDeps().OrgServiceDependsOn()
	want := []string{"billing", "identity"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("OrgServiceDependsOn() = %v, want %v", got, want)
	}
}

func TestExternalDependencies_ReturnsExternalAndOrgService(t *testing.T) {
	got := mixedDeps().ExternalDependencies()
	want := []Dependency{
		{Kind: DependencyKindOrgService, Name: "billing"},
		{Kind: DependencyKindExternal, Name: "stripe"},
		{Kind: DependencyKindOrgService, Name: "identity"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ExternalDependencies() = %+v, want %+v", got, want)
	}
}

func TestHelpers_EmptyDependenciesReturnNonNil(t *testing.T) {
	c := DesignComponent{Name: "solo"}
	if got := c.ComponentDependsOn(); got == nil || len(got) != 0 {
		t.Fatalf("ComponentDependsOn() on empty = %v, want non-nil empty", got)
	}
	if got := c.OrgServiceDependsOn(); got == nil || len(got) != 0 {
		t.Fatalf("OrgServiceDependsOn() on empty = %v, want non-nil empty", got)
	}
	if got := c.ExternalDependencies(); got == nil || len(got) != 0 {
		t.Fatalf("ExternalDependencies() on empty = %v, want non-nil empty", got)
	}
}

// TestDependency_JSONRoundTrip proves each kind survives a marshal/unmarshal
// hop with its kind-specific fields intact.
func TestDependency_JSONRoundTrip(t *testing.T) {
	cases := map[string]Dependency{
		"component": {
			Kind: DependencyKindComponent,
			Name: "cart",
		},
		"org-service": {
			Kind:   DependencyKindOrgService,
			Name:   "billing",
			Status: "blocked",
			Reason: "access-required",
		},
		"external-rest": {
			Kind:     DependencyKindExternal,
			Name:     "stripe",
			Style:    DependencyStyleRestAPI,
			SpecPath: "dependencies/stripe.openapi.yaml",
			Config: []ConfigKey{
				{Key: "STRIPE_API_KEY", Secret: true, Description: "Your Stripe secret API key"},
				{Key: "STRIPE_REGION", Secret: false, DefaultValue: "us-east-1"},
			},
		},
		"external-ambiguous-candidates": {
			Kind: DependencyKindExternal,
			Name: "email-provider",
			Candidates: []DependencyCandidate{
				{Name: "sendgrid-rest", Style: DependencyStyleRestAPI, Description: "SendGrid v3 Web API"},
				{Name: "resend-sdk", Style: DependencyStyleSDK, Description: "Resend Node SDK",
					Package: "npm:resend@^4.0.0"},
			},
		},
		"platform-resource": {
			Kind:         DependencyKindPlatformResource,
			Name:         "orders-db",
			ResourceType: "postgres-cnpg",
			Parameters:   map[string]any{"size": "10Gi"},
		},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var out Dependency
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(in, out) {
				t.Fatalf("round-trip mismatch:\n in = %+v\nout = %+v", in, out)
			}
		})
	}
}

// TestDependency_StatusReasonOmittedWhenEmpty guards the read-time-only fields:
// they carry `omitempty` so an unresolved-at-write-time dependency serialises
// without them (they are recomputed on read by later tasks).
func TestDependency_StatusReasonOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(Dependency{Kind: DependencyKindComponent, Name: "cart"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	if want := `{"kind":"component","name":"cart"}`; got != want {
		t.Fatalf("marshal = %s, want %s", got, want)
	}
}

// TestComponentTypeConstants pins the component-type vocabulary to
// OpenChoreo's OWN terms (its ComponentType names minus the `deployment/`
// prefix: `deployment/service`, `deployment/web-application`). AEP uses these
// values end-to-end — agent contract, design.json, platform comparisons —
// with zero translation, so a drift here is a platform-wide vocabulary break.
func TestComponentTypeConstants(t *testing.T) {
	if ComponentTypeService != "service" {
		t.Errorf("ComponentTypeService = %q, want %q (OC: deployment/service)", ComponentTypeService, "service")
	}
	if ComponentTypeWebApplication != "web-application" {
		t.Errorf("ComponentTypeWebApplication = %q, want %q (OC: deployment/web-application)", ComponentTypeWebApplication, "web-application")
	}
}

// TestUnionExternalConfigKeys proves the request-time secret-classification
// source: config keys UNION across every component declaring an external
// name, secret wins on conflict, grouping is case-insensitive under the
// first-seen casing, and non-external kinds are ignored. A regression here is
// a secret-leak (a key secret in one component classified plain because
// another component declared it plain / was scanned first).
func TestUnionExternalConfigKeys(t *testing.T) {
	comps := []DesignComponent{
		{Name: "webhook-worker", Dependencies: []Dependency{
			// same external, api_key PLAIN here + a component-local secret
			{Kind: DependencyKindExternal, Name: "stripe", Config: []ConfigKey{
				{Key: "api_key"}, {Key: "webhook_secret", Secret: true},
			}},
			// non-external kinds must be ignored
			{Kind: DependencyKindPlatformResource, Name: "stripe"},
			{Kind: DependencyKindComponent, Name: "cart"},
		}},
		{Name: "checkout", Dependencies: []Dependency{
			// SAME external, api_key SECRET here + region PLAIN
			{Kind: DependencyKindExternal, Name: "stripe", Config: []ConfigKey{
				{Key: "api_key", Secret: true}, {Key: "region"},
			}},
			// a DIFFERENT external
			{Kind: DependencyKindExternal, Name: "sendgrid", Config: []ConfigKey{
				{Key: "SENDGRID_API_KEY", Secret: true},
			}},
		}},
		{Name: "reporter", Dependencies: []Dependency{
			// case-variant name must merge into the first-seen "stripe"
			{Kind: DependencyKindExternal, Name: "Stripe", Config: []ConfigKey{
				{Key: "extra_key"},
			}},
		}},
	}

	got := UnionExternalConfigKeys(comps)

	// case-insensitive grouping under the first-seen casing "stripe"
	if _, ok := got["Stripe"]; ok {
		t.Fatalf("case variant must fold into first-seen casing, got separate %q entry", "Stripe")
	}
	secretByKey := func(name string) map[string]bool {
		m := map[string]bool{}
		for _, k := range got[name] {
			if _, dup := m[k.Key]; dup {
				t.Fatalf("%s: key %q appears twice — union must dedupe", name, k.Key)
			}
			m[k.Key] = k.Secret
		}
		return m
	}
	stripe := secretByKey("stripe")
	// union of keys across all three declarations of stripe
	for _, want := range []string{"api_key", "webhook_secret", "region", "extra_key"} {
		if _, ok := stripe[want]; !ok {
			t.Errorf("stripe union missing key %q; got %v", want, stripe)
		}
	}
	// secret WINS: api_key is plain in webhook-worker but secret in checkout
	if !stripe["api_key"] {
		t.Error("api_key must be SECRET (secret wins across components), got plain — leak")
	}
	if !stripe["webhook_secret"] {
		t.Error("webhook_secret must stay secret")
	}
	if stripe["region"] || stripe["extra_key"] {
		t.Errorf("plain keys must stay plain; region=%v extra_key=%v", stripe["region"], stripe["extra_key"])
	}
	// the other external is independent
	if sg := secretByKey("sendgrid"); !sg["SENDGRID_API_KEY"] || len(sg) != 1 {
		t.Errorf("sendgrid union wrong: %v", sg)
	}
}

func TestUnionExternalConfigFor(t *testing.T) {
	comps := []DesignComponent{{Name: "c", Dependencies: []Dependency{
		{Kind: DependencyKindExternal, Name: "stripe", Config: []ConfigKey{{Key: "api_key", Secret: true}}},
	}}}
	// case-insensitive lookup
	if cfg, ok := UnionExternalConfigFor(comps, "STRIPE"); !ok || len(cfg) != 1 || cfg[0].Key != "api_key" || !cfg[0].Secret {
		t.Fatalf("case-insensitive lookup failed: %v", cfg)
	}
	// absent name → nil
	if cfg, ok := UnionExternalConfigFor(comps, "nope"); ok || cfg != nil {
		t.Fatalf("absent dependency must yield nil, got %v", cfg)
	}
}
