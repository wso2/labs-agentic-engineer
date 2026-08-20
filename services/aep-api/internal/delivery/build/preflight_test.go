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

package build

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// ----- fakes -----------------------------------------------------------------

type fakeDesign struct {
	comps []spec.DesignComponent
	err   error
}

func (f fakeDesign) ReadDesignComponents(context.Context, string, string) ([]spec.DesignComponent, error) {
	return f.comps, f.err
}

// fakeStatus reports every dependency as NOT ready (nothing provisioned or
// in-flight) — the "everything still needs the drawer" baseline the
// filtering-rule tests build on.
type fakeStatus struct {
	err error
}

func (f fakeStatus) Ready(context.Context, string, string, string) (bool, error) {
	return false, f.err
}

// kindsByDep groups the emitted items' kinds by the dependency name they were
// raised for, so a test can assert "stripe produced exactly {external-config}"
// without caring about item order.
func kindsByDep(items []PreflightItem) map[string][]string {
	out := make(map[string][]string, len(items))
	for _, it := range items {
		out[it.Dependency] = append(out[it.Dependency], it.Kind)
	}
	return out
}

// ----- tests -------------------------------------------------------------------

// An external-config item carries its schema for later collection, but it is
// not a design-resolution blocker.
func TestPreflight_ExternalConfigSchemaDoesNotRequireResolution(t *testing.T) {
	svc := NewPreflightService(PreflightDeps{
		Design: fakeDesign{comps: []spec.DesignComponent{{
			Name: "orders",
			Dependencies: []spec.Dependency{{
				Kind:   spec.DependencyKindExternal,
				Name:   "stripe",
				Config: []spec.ConfigKey{{Key: "STRIPE_KEY", Secret: true}},
			}},
		}}},
		Status: fakeStatus{},
	})

	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.True(t, pf.NeedsInput)
	require.False(t, pf.NeedsResolution)
	require.Equal(t, "external-config", pf.Items[0].Kind)
}

// An unresolved external dependency remains a design-resolution blocker.
func TestPreflight_UnresolvedExternalRequiresResolution(t *testing.T) {
	svc := NewPreflightService(PreflightDeps{
		Design: fakeDesign{comps: []spec.DesignComponent{{
			Name: "orders",
			Dependencies: []spec.Dependency{{
				Kind:   spec.DependencyKindExternal,
				Name:   "stripe",
				Status: spec.DependencyStatusUnresolved,
				Reason: spec.DependencyReasonNeedsInput,
			}},
		}}},
		Status: fakeStatus{},
	})

	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.True(t, pf.NeedsResolution)
	require.Equal(t, "external-unresolved", pf.Items[0].Kind)
}

func TestPreflight_ItemsPerKind(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe",
				Config: []spec.ConfigKey{{Key: "STRIPE_KEY", Secret: true}, {Key: "STRIPE_ORG", Secret: false}}},
			{Kind: spec.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg", Parameters: map[string]any{"instances": 1}},
			{Kind: spec.DependencyKindOrgService, Name: "billing", Status: spec.DependencyStatusUnresolved},
			{Kind: spec.DependencyKindOrgService, Name: "audit", Status: spec.DependencyStatusResolved},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.True(t, pf.NeedsInput)
	kinds := kindsByDep(pf.Items)
	require.Equal(t, []string{"external-config"}, kinds["stripe"])
	require.Equal(t, []string{"platform-resource"}, kinds["orders-db"])
	require.Equal(t, []string{"org-service"}, kinds["billing"])
	_, present := kinds["audit"]
	require.False(t, present)
}

// The external-config item's Config carries key/secret/description VIEWS only —
// never values (there are none to leak on a Dependency, but the shape must stay
// value-free so a future value-bearing field never rides along). The optional
// per-key description threads through so the drawer can render it as a hint.
func TestPreflight_ExternalConfigItem_CarriesKeySecretDescriptionViewsOnly(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe",
				Config: []spec.ConfigKey{{Key: "STRIPE_KEY", Secret: true, Description: "Your Stripe secret API key"}, {Key: "STRIPE_ORG", Secret: false, DefaultValue: "acme"}}},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.Len(t, pf.Items, 1)
	item := pf.Items[0]
	require.Equal(t, "external-config", item.Kind)
	require.Equal(t, []ConfigKeyView{{Key: "STRIPE_KEY", Secret: true, Description: "Your Stripe secret API key"}, {Key: "STRIPE_ORG", Secret: false, DefaultValue: "acme"}}, item.Config)
}

// A platform-resource item carries its ResourceType + Parameters through —
// the drawer's provision call needs both.
func TestPreflight_PlatformResourceItem_CarriesResourceTypeAndParameters(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg", Parameters: map[string]any{"instances": 1}},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.Len(t, pf.Items, 1)
	item := pf.Items[0]
	require.Equal(t, "orders-db", item.Dependency)
	require.Equal(t, "postgres-cnpg", item.ResourceType)
	require.Equal(t, map[string]any{"instances": 1}, item.Parameters)
}

// A "blocked" or "ambiguous" org-service dependency also needs the drawer —
// only "resolved" is skipped.
func TestPreflight_OrgServiceBlockedAndAmbiguous_AlsoEmit(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindOrgService, Name: "payments", Status: spec.DependencyStatusBlocked},
			{Kind: spec.DependencyKindOrgService, Name: "shipping", Status: spec.DependencyStatusAmbiguous},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	kinds := kindsByDep(pf.Items)
	require.Equal(t, []string{"org-service"}, kinds["payments"])
	require.Equal(t, []string{"org-service"}, kinds["shipping"])
}

// A "component" kind dependency (sibling component) is never emitted — it is
// not provisioned via the drawer.
func TestPreflight_ComponentKindDependency_NeverEmits(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindComponent, Name: "catalog"},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.False(t, pf.NeedsInput)
	require.Empty(t, pf.Items)
}

// A resolved external dependency always exposes its config schema, even when
// the legacy provisioning status says Ready. The schema is a non-gating view
// used by Builds for later corrections; platform-resource readiness keeps its
// existing behavior and suppresses the automatic provisioning item.
func TestPreflight_ReadyResolvedExternal_EmitsSchemaWithoutGating(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe", Status: spec.DependencyStatusResolved,
				Config: []spec.ConfigKey{{Key: "STRIPE_KEY", Secret: true}}},
			{Kind: spec.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg"},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: readyStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.True(t, pf.NeedsInput)
	require.False(t, pf.NeedsResolution)
	require.Equal(t, []string{"external-config"}, kindsByDep(pf.Items)["stripe"])
	require.Equal(t, []ConfigKeyView{{Key: "STRIPE_KEY", Secret: true}}, pf.Items[0].Config)
	_, platformPresent := kindsByDep(pf.Items)["orders-db"]
	require.False(t, platformPresent)
}

// Non-service components (e.g. web-application) flow through the SAME
// itemsFor logic as service components (#252 Task 14 — lifting the
// ComponentType != service guard: Task 9 already surfaces a web-app
// dependency's status chips and the coding-agent wiring already emits
// consumed-spec instructions for it, so the drawer/gate must not silently skip
// it). An unresolved external dependency on a web-application raises the same
// blocker item a service component's would.
func TestPreflight_WebApplicationComponent_UnresolvedExternal_EmitsBlockerItem(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "web", ComponentType: spec.ComponentTypeWebApplication,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe",
				Status: spec.DependencyStatusUnresolved, Reason: spec.DependencyReasonNeedsInput},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.True(t, pf.NeedsInput)
	require.Len(t, pf.Items, 1)
	item := pf.Items[0]
	require.Equal(t, "web", item.Component)
	require.Equal(t, "stripe", item.Dependency)
	require.Equal(t, "external-unresolved", item.Kind)
}

// A web-application component exercises the SAME per-kind branches as a
// service component (external-config, platform-resource, org-service
// unresolved-vs-resolved, and the never-emitted component kind) —
// mirroring TestPreflight_ItemsPerKind exactly, but on a web-application, to
// prove itemsFor's kind switch (not the caller's loop) is what decides
// emission, so lifting the ComponentType guard cannot mis-gate any kind.
func TestPreflight_WebApplicationComponent_AllDependencyKinds_BehaveSameAsService(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "web", ComponentType: spec.ComponentTypeWebApplication,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe",
				Config: []spec.ConfigKey{{Key: "STRIPE_KEY", Secret: true}, {Key: "STRIPE_ORG", Secret: false}}},
			{Kind: spec.DependencyKindPlatformResource, Name: "web-cache", ResourceType: "redis", Parameters: map[string]any{"size": "small"}},
			{Kind: spec.DependencyKindOrgService, Name: "billing", Status: spec.DependencyStatusUnresolved},
			{Kind: spec.DependencyKindOrgService, Name: "audit", Status: spec.DependencyStatusResolved},
			{Kind: spec.DependencyKindComponent, Name: "catalog"},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.True(t, pf.NeedsInput)
	kinds := kindsByDep(pf.Items)
	require.Equal(t, []string{"external-config"}, kinds["stripe"])
	require.Equal(t, []string{"platform-resource"}, kinds["web-cache"])
	require.Equal(t, []string{"org-service"}, kinds["billing"])
	_, auditPresent := kinds["audit"]
	require.False(t, auditPresent, "resolved org-service dependency must not surface")
	_, catalogPresent := kinds["catalog"]
	require.False(t, catalogPresent, "sibling component dependency must never surface")
}

// readyStatus reports every dependency as already ready — the "nothing left
// to ask" fake used by the skip-on-ready tests.
type readyStatus struct{}

func (readyStatus) Ready(context.Context, string, string, string) (bool, error) { return true, nil }

// ----- #252 Task 10: the restored external proceed gate -----------------------
//
// externalItems now reads the dependency's ALREADY-COMPUTED Status/Reason
// (spec.ComputeDependencyStatus's output, per dependencyBlocker) rather than
// re-deriving anything — exactly mirroring how orgServiceItems already reads
// d.Status above. These fixtures set Status/Reason directly, the same
// convention TestPreflight_ItemsPerKind and TestPreflight_OrgServiceBlockedAndAmbiguous_AlsoEmit
// already use for org-service.

// An ambiguous external dependency (2+ candidates) raises "external-ambiguous"
// — never the config item, and Ready is never consulted (nothing meaningful to
// collect until the dependency itself resolves).
func TestPreflight_ExternalAmbiguous_EmitsBlockerItem(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "salesforce", Status: spec.DependencyStatusAmbiguous},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.Len(t, pf.Items, 1)
	item := pf.Items[0]
	require.Equal(t, "external-ambiguous", item.Kind)
	require.Equal(t, "salesforce", item.Dependency)
	require.NotEmpty(t, item.Description)
}

// An unresolved external dependency with reason=needs-input raises
// "external-unresolved".
func TestPreflight_ExternalNeedsInput_EmitsUnresolvedBlockerItem(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "weather-api",
				Status: spec.DependencyStatusUnresolved, Reason: spec.DependencyReasonNeedsInput},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.Len(t, pf.Items, 1)
	item := pf.Items[0]
	require.Equal(t, "external-unresolved", item.Kind)
	require.Equal(t, "weather-api", item.Dependency)
	require.NotEmpty(t, item.Description)
}

// An unresolved external dependency with reason=needs-spec raises the
// pre-existing "external-spec" kind — reborn, not reinvented.
func TestPreflight_ExternalNeedsSpec_EmitsSpecBlockerItem(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "partner-api",
				Status: spec.DependencyStatusUnresolved, Reason: spec.DependencyReasonNeedsSpec},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.Len(t, pf.Items, 1)
	item := pf.Items[0]
	require.Equal(t, "external-spec", item.Kind)
	require.Equal(t, "partner-api", item.Dependency)
	require.NotEmpty(t, item.Description)
}

// A resolved external dependency (or one with no Status computed at all —
// the resolver-never-wired fail-open case) falls through to the unchanged
// external-config path: config collection is a provisioning-readiness
// concern, untouched by this task.
func TestPreflight_ExternalResolved_FallsThroughToConfigItem(t *testing.T) {
	comps := []spec.DesignComponent{{Name: "orders", ComponentType: spec.ComponentTypeService,
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindExternal, Name: "stripe", Status: spec.DependencyStatusResolved,
				Config: []spec.ConfigKey{{Key: "STRIPE_KEY", Secret: true}}},
		}}}
	svc := NewPreflightService(PreflightDeps{Design: fakeDesign{comps: comps}, Status: fakeStatus{}})
	pf, err := svc.Preflight(context.Background(), "acme", "shop")
	require.NoError(t, err)
	require.Len(t, pf.Items, 1)
	require.Equal(t, "external-config", pf.Items[0].Kind)
}
