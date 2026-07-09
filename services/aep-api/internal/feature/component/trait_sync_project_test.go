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
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts"
	"github.com/wso2/aep/aep-api/internal/feature/artifacts/artifactstest"
	"github.com/wso2/aep/aep-api/models"
)

// UNIT tier for TraitSyncService.SyncProjectAPITraits + siblingSPAOrigins —
// the project-wide sibling-CORS re-emit that runs on every dispatch cascade
// (dispatch_cascade_hook.go). The two seams are doubled at the process
// boundary: the openchoreo.ComponentClient (generated moq — ListDeployments
// to read a web-app's external URL, UpdateComponentTraits* to emit) and the
// artifacts store (the REAL artifacts.NewArtifactStore decorator over a fake
// service serving a design working-tree, so the frontmatter → DesignComponent
// parse is the real one, not a stub). No idp is wired (SetIDPService not
// called) so the publisher-provisioning branch is skipped.

// traitStoreWith wraps the REAL artifacts.NewArtifactStore over a fake
// artifact service serving the given design working-tree map. ReadDesign's
// frontmatter parse therefore runs for real.
func traitStoreWith(files map[string]string) *artifacts.ArtifactStore {
	return artifacts.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
}

// traitReadDesign parses a design working-tree map through the real store.
func traitReadDesign(t *testing.T, files map[string]string) *artifacts.DesignFile {
	t.Helper()
	d, err := traitStoreWith(files).ReadDesign(context.Background(), "acme", "proj")
	if err != nil {
		t.Fatalf("ReadDesign: %v", err)
	}
	if d == nil {
		t.Fatalf("ReadDesign returned nil design for fixture")
	}
	return d
}

// ocDeployments builds a ComponentClient moq whose ListDeployments returns the
// mapped external URL for a component (keyed by k8s component name), an empty
// deployment list for any unmapped component, and canned success for the two
// trait writes. UpdateComponentTraits* start as success no-ops; a case can
// override them to inject failure.
func ocDeployments(urlsByComponent map[string]string) *mocks.ComponentClientMock {
	return &mocks.ComponentClientMock{
		ListDeploymentsFunc: func(_ context.Context, _, _, componentName string) (*models.DeploymentList, error) {
			if u, ok := urlsByComponent[componentName]; ok && u != "" {
				return &models.DeploymentList{Items: []models.Deployment{{EndpointURL: u}}}, nil
			}
			return &models.DeploymentList{}, nil
		},
		UpdateComponentTraitsFunc: func(context.Context, string, string, string, []models.ComponentTrait) error {
			return nil
		},
		UpdateComponentTraitEnvironmentConfigsFunc: func(context.Context, string, string, string, map[string]map[string]interface{}) error {
			return nil
		},
	}
}

// design fixtures -------------------------------------------------------------

func traitRootMd() string { return "---\nsourceSpec: v1\n---\n\nOverview.\n" }

func endUserServiceMd(name string) string {
	return traitServiceJSON(name, "end-user-required")
}

func serviceToServiceMd(name string) string {
	return traitServiceJSON(name, "service-required")
}

func plainServiceMd(name string) string {
	return traitServiceJSON(name, "")
}

// webAppMd renders a web-application component design.json (canonical type:
// models.ComponentTypeWebApplication — OpenChoreo's own term).
func webAppMd(name string) string {
	return "{\n  \"name\": \"" + name + "\",\n  \"type\": \"web-application\",\n  \"description\": \"SPA.\",\n  \"dependencies\": []\n}\n"
}

// traitServiceJSON renders a service component design.json with an optional
// exposesAPI.auth policy (empty auth ⇒ no exposesAPI block).
func traitServiceJSON(name, auth string) string {
	var b strings.Builder
	b.WriteString("{\n  \"name\": \"" + name + "\",\n  \"type\": \"service\",\n  \"description\": \"API.\",\n  \"dependencies\": []")
	if auth != "" {
		b.WriteString(",\n  \"exposesAPI\": {\n    \"auth\": \"" + auth + "\"\n  }")
	}
	b.WriteString("\n}\n")
	return b.String()
}

// --- SyncProjectAPITraits -----------------------------------------------------

func TestSyncProjectAPITraits_ReEmitsEnabledServicesOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// api: protected end-user service (re-emitted, sibling-CORS active).
	// s2s: protected service-to-service (re-emitted, but no SPA origins).
	// worker: unprotected service (skipped — ResolveAPISecurityEnabled=false).
	// web: web-app (skipped — not ComponentType "service").
	files := map[string]string{
		artifacts.DesignRootFile:        traitRootMd(),
		"components/api/design.json":    endUserServiceMd("api"),
		"components/s2s/design.json":    serviceToServiceMd("s2s"),
		"components/worker/design.json": plainServiceMd("worker"),
		"components/web/design.json":    webAppMd("web"),
	}
	oc := ocDeployments(map[string]string{"web": "http://web.local/app/"})
	svc := NewTraitSyncService(oc, traitStoreWith(files))

	if err := svc.SyncProjectAPITraits(ctx, "acme", "proj"); err != nil {
		t.Fatalf("SyncProjectAPITraits: %v", err)
	}

	// Exactly the two enabled service components get their traits re-emitted.
	emitted := map[string]bool{}
	for _, c := range oc.UpdateComponentTraitsCalls() {
		emitted[c.ComponentName] = true
	}
	if !emitted["api"] || !emitted["s2s"] {
		t.Errorf("both enabled services must be re-emitted; got %v", emitted)
	}
	if emitted["worker"] {
		t.Errorf("unprotected service must not be re-emitted")
	}
	if emitted["web"] {
		t.Errorf("web-app must not be re-emitted (only ComponentType=service)")
	}
	if n := len(oc.UpdateComponentTraitsCalls()); n != 2 {
		t.Errorf("want 2 trait emits (api, s2s); got %d", n)
	}

	// Only the end-user API pulls sibling SPA origins: ListDeployments is
	// called for the web-app while reconciling "api", never for the s2s API
	// (service-required has no browser caller).
	sawWebLookup := false
	for _, c := range oc.ListDeploymentsCalls() {
		if c.ComponentName == "web" {
			sawWebLookup = true
		}
	}
	if !sawWebLookup {
		t.Errorf("end-user API reconcile must list the web-app sibling's deployments")
	}

	// The env-config emitted for "api" carries the sibling origin in CORS with
	// credentials on; "s2s" falls back to wildcard CORS (no allowedOrigins).
	apiCORS := traitCORSFor(t, oc, "api")
	if got, _ := apiCORS["allowCredentials"].(bool); !got {
		t.Errorf("end-user API CORS should set allowCredentials=true; got %+v", apiCORS)
	}
	origins, _ := apiCORS["allowedOrigins"].([]interface{})
	if len(origins) != 1 || origins[0] != "http://web.local" {
		t.Errorf("end-user API CORS allowedOrigins = %v; want [http://web.local] (path trimmed)", apiCORS["allowedOrigins"])
	}
	s2sCORS := traitCORSFor(t, oc, "s2s")
	if _, ok := s2sCORS["allowedOrigins"]; ok {
		t.Errorf("service-to-service API must not advertise SPA origins; got %+v", s2sCORS)
	}
}

// traitCORSFor digs the `cors` block out of the last env-config emitted for a
// component via UpdateComponentTraitEnvironmentConfigs.
func traitCORSFor(t *testing.T, oc *mocks.ComponentClientMock, component string) map[string]interface{} {
	t.Helper()
	inst := APIConfigurationInstanceName(component)
	for _, c := range oc.UpdateComponentTraitEnvironmentConfigsCalls() {
		if c.ComponentName != component {
			continue
		}
		params, ok := c.Configs[inst]
		if !ok || params == nil {
			t.Fatalf("no env config for %q under instance %q; got %+v", component, inst, c.Configs)
		}
		cors, _ := params["cors"].(map[string]interface{})
		if cors == nil {
			t.Fatalf("no cors block for %q; got %+v", component, params)
		}
		return cors
	}
	t.Fatalf("UpdateComponentTraitEnvironmentConfigs never called for %q", component)
	return nil
}

func TestSyncProjectAPITraits_PerComponentErrorDoesNotAbort(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files := map[string]string{
		artifacts.DesignRootFile:       traitRootMd(),
		"components/api-a/design.json": serviceToServiceMd("api-a"),
		"components/api-b/design.json": serviceToServiceMd("api-b"),
	}
	oc := ocDeployments(map[string]string{})
	oc.UpdateComponentTraitsFunc = func(_ context.Context, _, _, componentName string, _ []models.ComponentTrait) error {
		if componentName == "api-a" {
			return errors.New("oc: transient PATCH failure")
		}
		return nil
	}
	svc := NewTraitSyncService(oc, traitStoreWith(files))

	// api-a's SyncComponentTraits errors; the loop logs and continues, so
	// api-b is still attempted and the project-level call returns nil.
	if err := svc.SyncProjectAPITraits(ctx, "acme", "proj"); err != nil {
		t.Fatalf("per-component failure must be best-effort; got %v", err)
	}
	if n := len(oc.UpdateComponentTraitsCalls()); n != 2 {
		t.Errorf("both enabled services must be attempted even after one errors; got %d", n)
	}
}

func TestSyncProjectAPITraits_NoDesignIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("empty tree", func(t *testing.T) {
		t.Parallel()
		oc := ocDeployments(map[string]string{})
		svc := NewTraitSyncService(oc, traitStoreWith(map[string]string{}))
		if err := svc.SyncProjectAPITraits(ctx, "acme", "proj"); err != nil {
			t.Fatalf("empty design must be a no-op; got %v", err)
		}
		if n := len(oc.UpdateComponentTraitsCalls()); n != 0 {
			t.Errorf("no emits expected for empty design; got %d", n)
		}
	})

	t.Run("design read not-found is swallowed", func(t *testing.T) {
		t.Parallel()
		store := artifacts.NewArtifactStore(&artifactstest.FakeArtifactService{
			ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
				return nil, artifacts.ErrArtifactNotFound
			},
		})
		svc := NewTraitSyncService(&mocks.ComponentClientMock{}, store)
		if err := svc.SyncProjectAPITraits(ctx, "acme", "proj"); err != nil {
			t.Fatalf("ErrArtifactNotFound must be swallowed; got %v", err)
		}
	})
}

func TestSyncProjectAPITraits_RejectsEmptyArgs(t *testing.T) {
	t.Parallel()
	svc := NewTraitSyncService(&mocks.ComponentClientMock{}, traitStoreWith(map[string]string{}))
	if err := svc.SyncProjectAPITraits(context.Background(), "", "proj"); err == nil {
		t.Errorf("empty orgID should error")
	}
	if err := svc.SyncProjectAPITraits(context.Background(), "acme", ""); err == nil {
		t.Errorf("empty projectID should error")
	}
}

func TestSyncProjectAPITraits_NilReceiverIsNoOp(t *testing.T) {
	t.Parallel()
	var svc *TraitSyncService
	if err := svc.SyncProjectAPITraits(context.Background(), "acme", "proj"); err != nil {
		t.Errorf("nil receiver should be a no-op; got %v", err)
	}
}

// --- siblingSPAOrigins --------------------------------------------------------

func Test_siblingSPAOrigins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("collects each web-app origin, trims path, dedups", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			artifacts.DesignRootFile:      traitRootMd(),
			"components/web1/design.json": webAppMd("web1"),
			"components/web2/design.json": webAppMd("web2"),
			"components/api/design.json":  endUserServiceMd("api"),
		}
		design := traitReadDesign(t, files)
		oc := ocDeployments(map[string]string{
			"web1": "http://web1.local/todo/",    // path trimmed → scheme+host
			"web2": "http://web2.local:8080/app", // port preserved
		})
		svc := NewTraitSyncService(oc, traitStoreWith(files))

		got, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design)
		if err != nil {
			t.Fatalf("siblingSPAOrigins: %v", err)
		}
		want := map[string]bool{"http://web1.local": true, "http://web2.local:8080": true}
		if len(got) != 2 {
			t.Fatalf("want 2 origins; got %v", got)
		}
		for _, o := range got {
			if !want[o] {
				t.Errorf("unexpected origin %q; want one of %v", o, want)
			}
		}
		// The service component is never consulted for origins — only web-apps.
		for _, c := range oc.ListDeploymentsCalls() {
			if c.ComponentName == "api" {
				t.Errorf("siblingSPAOrigins must not list the service component's deployments")
			}
		}
	})

	t.Run("web-app with no deployment yet contributes nothing", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			artifacts.DesignRootFile:     traitRootMd(),
			"components/web/design.json": webAppMd("web"),
		}
		design := traitReadDesign(t, files)
		oc := ocDeployments(map[string]string{}) // web → empty list
		svc := NewTraitSyncService(oc, traitStoreWith(files))
		got, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design)
		if err != nil {
			t.Fatalf("siblingSPAOrigins: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("undeployed SPA should contribute no origins; got %v", got)
		}
	})

	t.Run("duplicate deployment URLs dedup to one origin", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			artifacts.DesignRootFile:     traitRootMd(),
			"components/web/design.json": webAppMd("web"),
		}
		design := traitReadDesign(t, files)
		oc := &mocks.ComponentClientMock{
			ListDeploymentsFunc: func(context.Context, string, string, string) (*models.DeploymentList, error) {
				return &models.DeploymentList{Items: []models.Deployment{
					{EndpointURL: "http://web.local/a"},
					{EndpointURL: "http://web.local/b"}, // same origin
				}}, nil
			},
		}
		svc := NewTraitSyncService(oc, traitStoreWith(files))
		got, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design)
		if err != nil {
			t.Fatalf("siblingSPAOrigins: %v", err)
		}
		if len(got) != 1 || got[0] != "http://web.local" {
			t.Errorf("same-origin deployments must dedup; got %v", got)
		}
	})

	t.Run("transient ListDeployments error surfaces (no partial allowlist)", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			artifacts.DesignRootFile:     traitRootMd(),
			"components/web/design.json": webAppMd("web"),
		}
		design := traitReadDesign(t, files)
		oc := &mocks.ComponentClientMock{
			ListDeploymentsFunc: func(context.Context, string, string, string) (*models.DeploymentList, error) {
				return nil, errors.New("oc: transient")
			},
		}
		svc := NewTraitSyncService(oc, traitStoreWith(files))
		if _, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design); err == nil {
			t.Fatalf("a sibling lookup error must surface — a partial CORS allowlist would silently block the missing SPA")
		}
	})

	t.Run("no web-apps yields empty slice, nil error", func(t *testing.T) {
		t.Parallel()
		files := map[string]string{
			artifacts.DesignRootFile:     traitRootMd(),
			"components/api/design.json": endUserServiceMd("api"),
		}
		design := traitReadDesign(t, files)
		oc := &mocks.ComponentClientMock{} // ListDeployments must never be called
		svc := NewTraitSyncService(oc, traitStoreWith(files))
		got, err := svc.siblingSPAOrigins(ctx, "acme", "proj", design)
		if err != nil {
			t.Fatalf("siblingSPAOrigins: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("want empty origins with no web-apps; got %v", got)
		}
		if len(oc.ListDeploymentsCalls()) != 0 {
			t.Errorf("ListDeployments must not be called when there are no web-apps; got %d", len(oc.ListDeploymentsCalls()))
		}
	})
}
