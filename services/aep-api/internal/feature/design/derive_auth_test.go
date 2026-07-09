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

package design

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/feature/dependencies/resources"
	"github.com/wso2/aep/aep-api/models"
)

// --- deriveEndUserAuth (pure function) ---------------------------------------

// thunderDep builds a platform-resource dependency of the SAMPLE resourceType
// "thunder-app". The name is arbitrary sample data — derivation keys on the CRT
// role MARKER (authRole), never on the name (see
// TestDeriveEndUserAuth_UnlabeledTypeUntouchedEvenIfNamedThunderApp).
func thunderDep(name string) models.Dependency {
	return models.Dependency{Kind: models.DependencyKindPlatformResource, Name: name, ResourceType: "thunder-app"}
}

// authRole returns a marker map flagging resourceType as carrying the
// end-user-auth role — the labeled sample type the derivation stamps on.
func authRole(resourceType string) map[string]resources.TypeMarkers {
	return map[string]resources.TypeMarkers{resourceType: {EndUserAuth: true}}
}

// fakeMarkerCatalog is the resourceMarkerCatalog port double for SaveAndProceed
// integration tests: it records whether it was consulted and serves a canned
// marker map (or an error to exercise the fail-closed save gate).
type fakeMarkerCatalog struct {
	markers map[string]resources.TypeMarkers
	err     error
	calls   int
}

func (f *fakeMarkerCatalog) MarkersByName(context.Context) (map[string]resources.TypeMarkers, error) {
	f.calls++
	return f.markers, f.err
}

// (a) service + thunder-app dep + nil ExposesAPI → ExposesAPI created with
// Auth end-user-required.
func TestDeriveEndUserAuth_StampsNilExposesAPI(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{{
		Name:          "orders-api",
		ComponentType: "service",
		Dependencies:  []models.Dependency{thunderDep("user-auth")},
	}}

	if err := deriveEndUserAuth(comps, authRole("thunder-app")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comps[0].ExposesAPI == nil || comps[0].ExposesAPI.Auth != authEndUserRequired {
		t.Fatalf("ExposesAPI = %+v, want Auth=%q", comps[0].ExposesAPI, authEndUserRequired)
	}
}

// (b) service + dep + existing end-user-required → unchanged, no error (and
// sibling ExposesAPI fields survive untouched).
func TestDeriveEndUserAuth_ExistingEndUserRequiredUnchanged(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{{
		Name:          "orders-api",
		ComponentType: "service",
		Dependencies:  []models.Dependency{thunderDep("user-auth")},
		ExposesAPI:    &models.ExposesAPI{Auth: authEndUserRequired, Managed: true, UserContext: "X-User-Id"},
	}}

	if err := deriveEndUserAuth(comps, authRole("thunder-app")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := comps[0].ExposesAPI
	if got.Auth != authEndUserRequired || !got.Managed || got.UserContext != "X-User-Id" {
		t.Fatalf("ExposesAPI mutated unexpectedly: %+v", got)
	}
}

// (c) service + dep + service-required → error naming both the dependency and
// the conflicting value; nothing is mutated.
func TestDeriveEndUserAuth_ServiceRequiredConflictErrors(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{{
		Name:          "orders-api",
		ComponentType: "service",
		Dependencies:  []models.Dependency{thunderDep("user-auth")},
		ExposesAPI:    &models.ExposesAPI{Auth: authServiceRequired},
	}}

	err := deriveEndUserAuth(comps, authRole("thunder-app"))
	if err == nil {
		t.Fatal("want a conflict error, got nil")
	}
	if !strings.Contains(err.Error(), "user-auth") {
		t.Fatalf("error must name the dependency %q: %v", "user-auth", err)
	}
	if !strings.Contains(err.Error(), authServiceRequired) {
		t.Fatalf("error must name the conflicting value %q: %v", authServiceRequired, err)
	}
	if comps[0].ExposesAPI.Auth != authServiceRequired {
		t.Fatalf("ExposesAPI must be left unchanged on conflict, got %+v", comps[0].ExposesAPI)
	}
}

// (d) web-app + dep → no ExposesAPI mutation (SPAs aren't gateway-exposed APIs).
func TestDeriveEndUserAuth_WebAppUntouched(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{{
		Name:          "storefront-web",
		ComponentType: "web-application",
		Dependencies:  []models.Dependency{thunderDep("user-auth")},
	}}

	if err := deriveEndUserAuth(comps, authRole("thunder-app")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comps[0].ExposesAPI != nil {
		t.Fatalf("web-app ExposesAPI must stay nil, got %+v", comps[0].ExposesAPI)
	}
}

// (e) service without the dependency → untouched.
func TestDeriveEndUserAuth_DepLessServiceUntouched(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{{
		Name:          "orders-api",
		ComponentType: "service",
		Dependencies:  []models.Dependency{{Kind: models.DependencyKindComponent, Name: "sibling"}},
	}}

	if err := deriveEndUserAuth(comps, authRole("thunder-app")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comps[0].ExposesAPI != nil {
		t.Fatalf("dep-less service ExposesAPI must stay nil, got %+v", comps[0].ExposesAPI)
	}
}

// Extra: a platform-resource dependency of a DIFFERENT resourceType (e.g.
// postgres-cnpg) must not trigger derivation.
func TestDeriveEndUserAuth_OtherResourceTypeUntouched(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{{
		Name:          "orders-api",
		ComponentType: "service",
		Dependencies: []models.Dependency{
			{Kind: models.DependencyKindPlatformResource, Name: "orders-db", ResourceType: "postgres-cnpg"},
		},
	}}

	if err := deriveEndUserAuth(comps, authRole("thunder-app")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comps[0].ExposesAPI != nil {
		t.Fatalf("non-thunder-app resource ExposesAPI must stay nil, got %+v", comps[0].ExposesAPI)
	}
}

// The NAME must mean nothing now: a platform-resource dep whose resourceType
// happens to be "thunder-app" but which carries NO end-user-auth role marker
// (empty catalog) is left completely untouched. This is the crux of the
// generalization — derivation keys on the CRT marker, never on the name.
func TestDeriveEndUserAuth_UnlabeledTypeUntouchedEvenIfNamedThunderApp(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{{
		Name:          "orders-api",
		ComponentType: "service",
		Dependencies:  []models.Dependency{thunderDep("user-auth")},
	}}

	// Empty marker map: "thunder-app" carries no role — nothing to derive.
	if err := deriveEndUserAuth(comps, map[string]resources.TypeMarkers{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comps[0].ExposesAPI != nil {
		t.Fatalf("unlabeled type must stay untouched regardless of its name, got %+v", comps[0].ExposesAPI)
	}
}

// A labeled type with a name OTHER than "thunder-app" stamps just the same —
// the marker, not the name, is the signal.
func TestDeriveEndUserAuth_StampsAnyLabeledType(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{{
		Name:          "orders-api",
		ComponentType: "service",
		Dependencies: []models.Dependency{
			{Kind: models.DependencyKindPlatformResource, Name: "user-auth", ResourceType: "custom-oidc"},
		},
	}}

	if err := deriveEndUserAuth(comps, authRole("custom-oidc")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comps[0].ExposesAPI == nil || comps[0].ExposesAPI.Auth != authEndUserRequired {
		t.Fatalf("labeled type of any name must stamp: ExposesAPI = %+v", comps[0].ExposesAPI)
	}
}

// Multiple components: only the qualifying service is stamped, everything
// else (a sibling service without the dep, and a web-app WITH the dep) is
// left alone in the same pass.
func TestDeriveEndUserAuth_MixedComponentsOnlyQualifyingServiceStamped(t *testing.T) {
	t.Parallel()
	comps := []models.DesignComponent{
		{Name: "orders-api", ComponentType: "service", Dependencies: []models.Dependency{thunderDep("user-auth")}},
		{Name: "billing-api", ComponentType: "service"},
		{Name: "storefront-web", ComponentType: "web-application", Dependencies: []models.Dependency{thunderDep("user-auth")}},
	}

	if err := deriveEndUserAuth(comps, authRole("thunder-app")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if comps[0].ExposesAPI == nil || comps[0].ExposesAPI.Auth != authEndUserRequired {
		t.Fatalf("orders-api: ExposesAPI = %+v, want Auth=%q", comps[0].ExposesAPI, authEndUserRequired)
	}
	if comps[1].ExposesAPI != nil {
		t.Fatalf("billing-api (no dep): ExposesAPI must stay nil, got %+v", comps[1].ExposesAPI)
	}
	if comps[2].ExposesAPI != nil {
		t.Fatalf("storefront-web: ExposesAPI must stay nil, got %+v", comps[2].ExposesAPI)
	}
}

// --- wiring: SaveAndProceed persists the derivation before the tag-cut ------

// designFilesWithDepsAndAuth is designFilesWithDeps (proceed_gate_test.go)
// plus an authored exposesAPI block, for conflict-path tests.
func designFilesWithDepsAndAuth(depsJSON, exposesAPIJSON string) map[string]string {
	files := designFilesWithDeps(depsJSON)
	marker := `"dependencies": ` + depsJSON
	key := "components/consumer/design.json"
	files[key] = strings.Replace(files[key], marker, marker+",\n  \"exposesAPI\": "+exposesAPIJSON, 1)
	return files
}

func TestSaveAndProceed_DerivesEndUserAuthAndPersistsBeforeTag(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	fake := happySave(designFilesWithDeps(deps))
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	svc.resourceCatalog = &fakeMarkerCatalog{markers: authRole("thunder-app")}

	got, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if got.Status != "approved" {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	if fc.commits != 1 {
		t.Fatalf("want exactly one derive-persist commit before the tag-cut, got %d", fc.commits)
	}
	if len(fc.writes) != 1 {
		t.Fatalf("want a single-file commit (the stamped component's design.json), got %d writes", len(fc.writes))
	}
	w := fc.writes[0]
	if !strings.HasSuffix(w.Path, "components/consumer/design.json") {
		t.Fatalf("commit path = %q, want the consumer component's design.json", w.Path)
	}
	if w.BaseSHA != "sha-design" {
		t.Fatalf("commit must CAS on the read sha, got %q", w.BaseSHA)
	}
	if !strings.Contains(w.Content, `"auth": "end-user-required"`) {
		t.Fatalf("committed design.json missing the derived auth:\n%s", w.Content)
	}
	// The response itself must also reflect the derived value (the re-read
	// after the derive-persist commit picks it up from the fake's HEAD map —
	// here the fake's ListDesignFilesFunc still serves the ORIGINAL map, so
	// the re-read naturally returns the un-derived content again; what matters
	// for this assertion is that the commit above landed with the right
	// content BEFORE SaveDesign (the tag-cut) was invoked at all.
	_ = got
}

// Regression: a component whose ExposesAPI was ALREADY non-nil (for some
// unrelated reason, here Managed) but had no Auth set yet must still have its
// derived Auth committed. This guards against a before/after diff that
// aliases the same pointer deriveEndUserAuth mutates in place — such a diff
// would never see the change and silently skip the commit.
func TestSaveAndProceed_DerivesAuthOnAlreadyNonNilExposesAPI(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	files := designFilesWithDepsAndAuth(deps, `{"managed": true}`)
	fake := happySave(files)
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	svc.resourceCatalog = &fakeMarkerCatalog{markers: authRole("thunder-app")}

	if _, err := svc.SaveAndProceed(context.Background(), "acme", "web", ""); err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if fc.commits != 1 {
		t.Fatalf("want the derive-persist commit to land even when ExposesAPI was already non-nil, got %d commits", fc.commits)
	}
	if len(fc.writes) != 1 || !strings.Contains(fc.writes[0].Content, `"auth": "end-user-required"`) {
		t.Fatalf("committed design.json missing the derived auth: %+v", fc.writes)
	}
	if !strings.Contains(fc.writes[0].Content, `"managed": true`) {
		t.Fatalf("committed design.json lost the pre-existing managed flag: %s", fc.writes[0].Content)
	}
}

func TestSaveAndProceed_EndUserAuthConflictBlocksTagCut(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	files := designFilesWithDepsAndAuth(deps, `{"auth": "service-required"}`)
	// readsFor's SaveDesignFunc fails the test if the tag-cut is reached — the
	// conflict must block before that, exactly like the unresolved-dependency
	// proceed-gate.
	fake := readsFor(t, files)
	svc := newService(fake)
	svc.fileCommitter = &fakeCommitter{}
	svc.resourceCatalog = &fakeMarkerCatalog{markers: authRole("thunder-app")}

	_, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if !errors.Is(err, ErrEndUserAuthConflict) {
		t.Fatalf("want ErrEndUserAuthConflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "user-auth") || !strings.Contains(err.Error(), authServiceRequired) {
		t.Fatalf("error must name the dependency and the conflicting value: %v", err)
	}
}

func TestSaveAndProceed_NoPlatformResourceDepNoDerivationCommit(t *testing.T) {
	t.Parallel()
	fake := happySave(validDesignFiles())
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc

	if _, err := svc.SaveAndProceed(context.Background(), "acme", "web", ""); err != nil {
		t.Fatalf("SaveAndProceed: unexpected error: %v", err)
	}
	if fc.commits != 0 {
		t.Fatalf("no platform-resource dependency in the design — want zero derive-persist commits, got %d", fc.commits)
	}
}

// Fail-closed save gate: when the design declares a platform-resource
// dependency but the CRT catalog is unreachable, the save must fail with the
// retryable ErrResourceCatalogUnavailable and commit NOTHING (the derivation
// can't be evaluated, so a silent skip would risk leaving an auth-required API
// exposed). readsFor's SaveDesignFunc fails the test if the tag-cut is reached.
func TestSaveAndProceed_CatalogDownWithPlatformResourceDepFailsClosed(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	fake := readsFor(t, designFilesWithDeps(deps))
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	cat := &fakeMarkerCatalog{err: errors.New("OC unreachable")}
	svc.resourceCatalog = cat

	_, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if !errors.Is(err, ErrResourceCatalogUnavailable) {
		t.Fatalf("want ErrResourceCatalogUnavailable, got %v", err)
	}
	if cat.calls != 1 {
		t.Fatalf("want exactly one catalog lookup, got %d", cat.calls)
	}
	if fc.commits != 0 {
		t.Fatalf("fail-closed save must commit nothing, got %d commits", fc.commits)
	}
}

// A nil catalog is treated as unreachable for the same reason: a design with a
// platform-resource dependency cannot proceed without the marker map.
func TestSaveAndProceed_NilCatalogWithPlatformResourceDepFailsClosed(t *testing.T) {
	t.Parallel()
	deps := `[{"kind":"platform-resource","name":"user-auth","resourceType":"thunder-app"}]`
	fake := readsFor(t, designFilesWithDeps(deps))
	svc := newService(fake)
	svc.fileCommitter = &fakeCommitter{}
	// No resourceCatalog wired.

	_, err := svc.SaveAndProceed(context.Background(), "acme", "web", "")
	if !errors.Is(err, ErrResourceCatalogUnavailable) {
		t.Fatalf("nil catalog + platform-resource dep: want ErrResourceCatalogUnavailable, got %v", err)
	}
}

// Auth-free save (no platform-resource dependency) must NEVER touch the catalog
// — even a catalog wired to error is not consulted, and the save succeeds.
func TestSaveAndProceed_NoPlatformResourceDepSkipsCatalog(t *testing.T) {
	t.Parallel()
	fake := happySave(validDesignFiles())
	svc := newService(fake)
	fc := &fakeCommitter{}
	svc.fileCommitter = fc
	cat := &fakeMarkerCatalog{err: errors.New("must not be called")}
	svc.resourceCatalog = cat

	if _, err := svc.SaveAndProceed(context.Background(), "acme", "web", ""); err != nil {
		t.Fatalf("auth-free save: unexpected error: %v", err)
	}
	if cat.calls != 0 {
		t.Fatalf("auth-free save must not consult the catalog, got %d calls", cat.calls)
	}
	if fc.commits != 0 {
		t.Fatalf("no derivation commit expected, got %d", fc.commits)
	}
}
