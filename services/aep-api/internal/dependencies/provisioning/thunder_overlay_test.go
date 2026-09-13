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
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/platform/securityspec"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// thunder-app is a TEST FIXTURE type name the fake catalog marks end-user-auth.
// Production overlay keys on the CRT marker, never this string.

type fakeMarkers struct {
	byName map[string]dependencies.TypeMarkers
	err    error
}

func (f *fakeMarkers) MarkersByName(context.Context) (map[string]dependencies.TypeMarkers, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byName, nil
}

func endUserAuthMarkers() *fakeMarkers {
	return &fakeMarkers{byName: map[string]dependencies.TypeMarkers{
		"thunder-app":   {EndUserAuth: true},
		"postgres-cnpg": {EndUserAuth: false},
	}}
}

type fakeSecurityJSON struct {
	raw     []byte
	err     error
	lastTag string
}

func (f *fakeSecurityJSON) ReadSecurityJSON(_ context.Context, _, _, tag string) ([]byte, error) {
	f.lastTag = tag
	return f.raw, f.err
}

// securityJSONV2 is a minimal v2 document whose catalog is the whole point: the
// client's `scopes` parameter is derived from it, and nothing in the file names
// the client at all — v2 removed the `thunder` block because neither parameter
// was ever a design decision.
func securityJSONV2(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"version": 2,
		"permissions": []any{map[string]any{
			"resource": "claims", "component": "api",
			"actions": []any{
				map[string]any{"handle": "read", "ownership": "own"},
				map[string]any{"handle": "approve", "ownership": "any"},
			},
		}},
		"groups": []any{},
		"roles": []any{map[string]any{
			"name": "Viewer", "description": "Reads own claims.", "stories": []int{1},
			"grants": []string{"claims:read"}, "assignTo": []string{"Viewers"},
		}},
		"screens":   []any{},
		"testUsers": []any{map[string]any{"username": "test-viewer", "roles": []string{"Viewer"}}},
	})
	if err != nil {
		t.Fatalf("marshal security.json fixture: %v", err)
	}
	if _, err := securityspec.Parse(raw); err != nil {
		t.Fatalf("fixture is not a valid security.json: %v", err)
	}
	return raw
}

// wantScopes is what the catalog above projects onto the CRT parameter: the
// OIDC scopes every access token carries, then every catalog handle.
const wantScopes = "openid profile email group ou claims:read claims:approve"

// fakeProjectNames is the display-name lookup. Its zero value answers "", which
// is the "project has no display name" case the overlay falls back from.
type fakeProjectNames struct {
	display string
	err     error
}

func (f fakeProjectNames) ProjectDisplayName(context.Context, string, string) (string, error) {
	return f.display, f.err
}

func designWithThunderApp(params map[string]any) []spec.DesignComponent {
	return []spec.DesignComponent{{
		Name: "web",
		Dependencies: []spec.Dependency{
			{Kind: spec.DependencyKindPlatformResource, Name: "idp", ResourceType: "thunder-app", Parameters: params},
		},
	}}
}

func thunderOverlayService(design []spec.DesignComponent, plat *fakePlatProv, security *fakeSecurityJSON) *Service {
	return thunderOverlayServiceNamed(design, plat, security, fakeProjectNames{display: "Expense Tracker"})
}

func thunderOverlayServiceNamed(design []spec.DesignComponent, plat *fakePlatProv, security *fakeSecurityJSON, names ProjectNamer) *Service {
	return NewService(Deps{
		Issues:       newFakeIssues(nil),
		Execs:        &fakeExecStore{},
		Design:       fakeDesign{comps: design},
		Repos:        fakeRepos{},
		PlatProv:     plat,
		Markers:      endUserAuthMarkers(),
		SecurityJSON: security,
		ProjectNames: names,
	})
}

// The client's display name is the PROJECT's — what a person reads on the login
// screen — not anything security.json says. Nothing in the document names it.
func TestProvision_DisplayNameIsTheProjectDisplayName(t *testing.T) {
	plat := &fakePlatProv{}
	sec := &fakeSecurityJSON{raw: securityJSONV2(t)}
	svc := thunderOverlayService(designWithThunderApp(nil), plat, sec)
	if err := svc.Provision(context.Background(), "org", "proj", "idp", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if plat.calls != 1 {
		t.Fatalf("platform provisioner must be called once, got %d", plat.calls)
	}
	if sec.lastTag != "" {
		t.Fatalf("HTTP provision must read security.json at HEAD (empty tag), got %q", sec.lastTag)
	}
	if got := plat.params["displayName"]; got != "Expense Tracker" {
		t.Fatalf("displayName = %v, want %q", got, "Expense Tracker")
	}
	if _, copied := plat.params["type"]; copied {
		t.Fatalf("the client type is a constant, never a CRT param: %+v", plat.params)
	}
}

// A project that never set a display name falls back to its id — what the
// console shows for it anyway — rather than to anything invented.
func TestProvision_DisplayNameFallsBackToTheProjectID(t *testing.T) {
	for name, namer := range map[string]ProjectNamer{
		"no display name": fakeProjectNames{},
		"lookup failed":   fakeProjectNames{err: errors.New("openchoreo said no")},
		"unwired":         nil,
	} {
		t.Run(name, func(t *testing.T) {
			plat := &fakePlatProv{}
			svc := thunderOverlayServiceNamed(designWithThunderApp(nil), plat,
				&fakeSecurityJSON{raw: securityJSONV2(t)}, namer)
			if err := svc.Provision(context.Background(), "org", "proj", "idp", nil, nil); err != nil {
				t.Fatalf("Provision: %v", err)
			}
			if got := plat.params["displayName"]; got != "proj" {
				t.Fatalf("displayName = %v, want the project id", got)
			}
		})
	}
}

// One project, several web applications: the project name alone would not say
// WHICH app the login screen belongs to, so the owning component is suffixed.
func TestProvision_DisplayNameNamesTheWebAppWhenAProjectHasSeveral(t *testing.T) {
	design := []spec.DesignComponent{
		{
			Name: "web", ComponentType: "web-application",
			Dependencies: []spec.Dependency{
				{Kind: spec.DependencyKindPlatformResource, Name: "idp", ResourceType: "thunder-app"},
			},
		},
		{Name: "admin-web", ComponentType: "web-application"},
	}
	plat := &fakePlatProv{}
	svc := thunderOverlayService(design, plat, &fakeSecurityJSON{raw: securityJSONV2(t)})
	if err := svc.Provision(context.Background(), "org", "proj", "idp", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if got := plat.params["displayName"]; got != "Expense Tracker · web" {
		t.Fatalf("displayName = %v, want the project name suffixed with the web app", got)
	}
}

// The client's allowlist is derived from the catalog: the OIDC scopes plus every
// declared handle. It is a truthful record of what the client will ask for —
// ThunderID stores it and never enforces it.
func TestProvision_ScopesAreTheOIDCScopesPlusTheCatalog(t *testing.T) {
	plat := &fakePlatProv{}
	svc := thunderOverlayService(designWithThunderApp(nil), plat, &fakeSecurityJSON{raw: securityJSONV2(t)})
	if err := svc.Provision(context.Background(), "org", "proj", "idp", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if got := plat.params["scopes"]; got != wantScopes {
		t.Fatalf("scopes = %v, want %q", got, wantScopes)
	}
}

// Neither the design's authored parameters nor the request's can reach the
// provisioner: the catalog is the only source of the client's scope list.
func TestProvision_AuthoredScopesCannotWin(t *testing.T) {
	designScopes := map[string]any{"scopes": "openid profile email"}

	t.Run("design.json parameters", func(t *testing.T) {
		plat := &fakePlatProv{}
		svc := thunderOverlayService(designWithThunderApp(designScopes), plat,
			&fakeSecurityJSON{raw: securityJSONV2(t)})
		if err := svc.Provision(context.Background(), "org", "proj", "idp", nil, nil); err != nil {
			t.Fatalf("Provision: %v", err)
		}
		if got := plat.params["scopes"]; got != wantScopes {
			t.Fatalf("design.json parameters.scopes must not win; got %v", got)
		}
	})

	t.Run("request parameters", func(t *testing.T) {
		plat := &fakePlatProv{}
		svc := thunderOverlayService(designWithThunderApp(nil), plat,
			&fakeSecurityJSON{raw: securityJSONV2(t)})
		if err := svc.Provision(context.Background(), "org", "proj", "idp",
			map[string]any{"scopes": "openid profile email"}, nil); err != nil {
			t.Fatalf("Provision: %v", err)
		}
		if got := plat.params["scopes"]; got != wantScopes {
			t.Fatalf("request parameters.scopes must not win; got %v", got)
		}
	})
}

func TestProvision_NonEndUserAuthParamsUnchanged(t *testing.T) {
	plat := &fakePlatProv{}
	svc := thunderOverlayService(designWithDeps(), plat, &fakeSecurityJSON{raw: securityJSONV2(t)})
	if err := svc.Provision(context.Background(), "org", "proj", "orders-db", nil, nil); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if plat.params["size"] != "small" {
		t.Fatalf("existing params must still flow, got %+v", plat.params)
	}
	if _, got := plat.params["displayName"]; got {
		t.Fatalf("non-end-user-auth must not take displayName from security.json, got %+v", plat.params)
	}
	if _, got := plat.params["scopes"]; got {
		t.Fatalf("non-end-user-auth must not take scopes from security.json, got %+v", plat.params)
	}
}

func TestProvision_InvalidSecurityJSONFailsProvision(t *testing.T) {
	plat := &fakePlatProv{}
	svc := thunderOverlayService(designWithThunderApp(nil), plat, &fakeSecurityJSON{
		raw: []byte(`{"not": "a security document"}`),
	})
	if err := svc.Provision(context.Background(), "org", "proj", "idp", nil, nil); err == nil {
		t.Fatal("present-but-invalid security.json must fail provision")
	}
	if plat.calls != 0 {
		t.Fatalf("platform provisioner must not be called on parse error, got %d", plat.calls)
	}
}

func TestProvisionForBuild_ReadsSecurityJSONAtSpecTag(t *testing.T) {
	plat := &fakePlatProv{}
	sec := &fakeSecurityJSON{raw: securityJSONV2(t)}
	svc := thunderOverlayService(designWithThunderApp(nil), plat, sec)
	fails, err := svc.ProvisionForBuild(context.Background(), "org", "org", "proj", "v3", 0, []BuildProvisionInput{
		{Component: "web", Dependency: "idp", Kind: buildKindPlatformResrc},
	})
	if err != nil {
		t.Fatalf("ProvisionForBuild: %v", err)
	}
	if len(fails) != 0 {
		t.Fatalf("want no failures, got %+v", fails)
	}
	if sec.lastTag != "v3" {
		t.Fatalf("build path must read security.json at the spec tag, got %q", sec.lastTag)
	}
	if plat.params["displayName"] != "Expense Tracker" {
		t.Fatalf("displayName = %v, want Expense Tracker", plat.params["displayName"])
	}
}
