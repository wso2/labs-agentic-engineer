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

package models

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
		"external": {
			Kind:      DependencyKindExternal,
			Name:      "stripe",
			NeedsSpec: true,
			SpecPath:  "dependencies/stripe.openapi.yaml",
			SpecUrl:   "https://api.example.com/openapi.yaml",
			Config: []ConfigKey{
				{Key: "STRIPE_API_KEY", Secret: true, CredentialClass: "secret"},
			},
			Candidates: []DependencyCandidate{
				{Label: "Stripe", Description: "payments", URL: "https://stripe.com"},
			},
		},
		"platform-resource": {
			Kind:         DependencyKindPlatformResource,
			Name:         "orders-db",
			ResourceType: "postgres-cnpg",
			Parameters:   map[string]string{"size": "10Gi"},
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
