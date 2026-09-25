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

package app

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	ocmocks "github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/dependencies/runtimeconfig"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/projects"
	"github.com/wso2/aep/aep-api/internal/spec"
	"github.com/wso2/aep/aep-api/internal/spec/artifactstest"
)

// A consumer-URL dependency's env-config key has TWO writers, and they live in
// different domains:
//
//	projects.DeploymentService    — the deploy-verdict read, every 15s poll
//	runtimeconfig.RuntimeConfigService — env-config.js composition + converge
//
// Neither can be dropped. The deploy read is the only writer that runs AFTER a
// web app's external URL resolves and WHILE the deploy wait is still open, so
// without it a first-ever deploy deadlocks: nothing re-composes until
// awaitDeployments goes green, and it cannot go green until the callback is
// registered. The composition write is the only one on the converge sweep, so
// without it drift is never re-asserted outside a deploy.
//
// They are safe ONLY because both are pure functions of the same design and
// therefore cannot disagree. This test is that guarantee, executable: same
// design, same resolved origins — the two must write the same binding the same
// bytes. If they ever drift, the field flaps between two values on every pass
// and the write storm and the clobber come back, with both sides looking correct
// read on their own.
//
// It lives here because the composition root is where the two are wired
// together; neither domain can see the other.

const (
	pinOrg        = "acme"
	pinProject    = "proj"
	pinDep        = "user-auth"
	pinType       = "thunder-app"
	pinEnvConfig  = "redirectUris"
	pinPath       = "/callback"
	pinBinding    = "proj-user-auth-default"
	pinGuest      = "guest-webapp"
	pinAdmin      = "hotel-admin-webapp"
	pinGuestOrig  = "https://guest.example/"
	pinAdminOrig  = "https://admin.example/"
	pinGuestCB    = "https://guest.example/callback"
	pinAdminCB    = "https://admin.example/callback"
	pinBothSorted = pinAdminCB + "," + pinGuestCB
)

func pinDesignFiles() map[string]string {
	webapp := func(name string) string {
		return `{"name":"` + name + `","type":"web-application","description":"SPA.",` +
			`"dependencies":[{"kind":"platform-resource","name":"` + pinDep + `","resourceType":"` + pinType + `"}]}`
	}
	return map[string]string{
		spec.DesignRootFile:                       "---\nsourceSpec: v1\n---\n\nOverview.\n",
		"components/" + pinGuest + "/design.json": webapp(pinGuest),
		"components/" + pinAdmin + "/design.json": webapp(pinAdmin),
	}
}

func pinStore() *spec.ArtifactStore {
	files := pinDesignFiles()
	return spec.NewArtifactStore(&artifactstest.FakeArtifactService{
		ListDesignFilesFunc: func(context.Context, string, string) (map[string]string, error) {
			return files, nil
		},
	})
}

// pinOriginsClient resolves both web apps' external URLs, as OpenChoreo would
// once their bindings are up.
func pinOriginsClient() *ocmocks.ComponentClientMock {
	origins := map[string]string{pinGuest: pinGuestOrig, pinAdmin: pinAdminOrig}
	return &ocmocks.ComponentClientMock{
		GetReleaseBindingStatusFunc: func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
			return &openchoreo.ReleaseBindingSummary{ReadyStatus: "True"}, nil
		},
		ListDeploymentsFunc: func(_ context.Context, _, _, componentName string) (*gen.DeploymentList, error) {
			if u := origins[componentName]; u != "" {
				return &gen.DeploymentList{Items: []gen.Deployment{{EndpointURL: u}}}, nil
			}
			return &gen.DeploymentList{}, nil
		},
	}
}

func pinResourceClient() *ocmocks.ResourceClientMock {
	return &ocmocks.ResourceClientMock{
		PatchBindingEnvironmentConfigsFunc: func(context.Context, string, string, map[string]string) error {
			return nil
		},
		GetBindingFunc: func(context.Context, string, string) (*openchoreo.ResourceReleaseBinding, error) {
			return &openchoreo.ResourceReleaseBinding{}, nil
		},
	}
}

// pinProjectsCatalog / pinRuntimeCatalog carry the SAME marker in each domain's
// own vocabulary — the split app.go's toConsumerURLMarker exists to bridge.
type pinProjectsCatalog struct{}

func (pinProjectsCatalog) MarkersByName(context.Context) (map[string]projects.ConsumerURLMarker, error) {
	return map[string]projects.ConsumerURLMarker{pinType: {EnvConfig: pinEnvConfig, Path: pinPath}}, nil
}

type pinRuntimeCatalog struct{}

func (pinRuntimeCatalog) MarkersByName(context.Context) (map[string]dependencies.TypeMarkers, error) {
	return map[string]dependencies.TypeMarkers{
		pinType: {ConsumerURLEnvConfig: pinEnvConfig, ConsumerURLPath: pinPath},
	}, nil
}

// pinThunderReader keeps the deploy-verdict path wired. Its view is irrelevant
// to what gets WRITTEN — a nil reader would skip the whole pass.
type pinThunderReader struct{}

func (pinThunderReader) FindByResource(context.Context, string, string) (*projects.ThunderApplicationView, error) {
	return &projects.ThunderApplicationView{
		RedirectURIs: pinBothSorted, Ready: true, Generation: 1, ObservedGeneration: 1,
	}, nil
}

// writtenByDeployRead drives the deploy-verdict writer.
func writtenByDeployRead(t *testing.T) (binding, value string) {
	t.Helper()
	rc := pinResourceClient()
	svc := projects.NewDeploymentService(pinOriginsClient(), pinStore())
	svc.SetResourceCatalog(pinProjectsCatalog{})
	svc.SetResourceClient(rc)
	svc.SetThunderApplicationReader(pinThunderReader{})

	if _, err := svc.DeploymentState(context.Background(), pinOrg, pinProject, []string{pinGuest, pinAdmin}); err != nil {
		t.Fatalf("DeploymentState: %v", err)
	}
	calls := rc.PatchBindingEnvironmentConfigsCalls()
	if len(calls) == 0 {
		t.Fatal("the deploy read registered nothing")
	}
	last := calls[len(calls)-1]
	return last.BindingName, last.Configs[pinEnvConfig]
}

// writtenByComposition drives the env-config.js writer.
func writtenByComposition(t *testing.T) (binding, value string) {
	t.Helper()
	rc := pinResourceClient()
	svc := runtimeconfig.NewRuntimeConfigService(pinOriginsClient(), rc, pinStore())
	svc.SetResourceCatalog(pinRuntimeCatalog{})

	// Composition of EITHER web app registers the project's whole set; the
	// deferred `ready` (no resolved outputs in this fixture) does not affect the
	// registration, which happens before the outputs are read.
	if _, _, err := svc.FilesForComponent(context.Background(), pinOrg, pinProject, pinGuest); err != nil {
		t.Fatalf("FilesForComponent: %v", err)
	}
	calls := rc.PatchBindingEnvironmentConfigsCalls()
	if len(calls) == 0 {
		t.Fatal("composition registered nothing")
	}
	last := calls[len(calls)-1]
	return last.BindingName, last.Configs[pinEnvConfig]
}

func TestConsumerURLWriters_AgreeOnTheSameDesign(t *testing.T) {
	t.Parallel()
	deployBinding, deployValue := writtenByDeployRead(t)
	composeBinding, composeValue := writtenByComposition(t)

	if deployBinding != composeBinding {
		t.Fatalf("the two writers target different bindings: deploy read %q, composition %q",
			deployBinding, composeBinding)
	}
	if deployValue != composeValue {
		t.Fatalf("the two writers disagree on %s:\n  deploy read: %q\n  composition: %q\n"+
			"One field with two values flaps on every pass — the clobber this split was fixed to remove.",
			pinEnvConfig, deployValue, composeValue)
	}
}

// Pinning agreement alone would be satisfied by both being wrong in the same
// way, so pin the value itself: every declaring web app, sorted.
func TestConsumerURLWriters_WriteEveryWebAppSorted(t *testing.T) {
	t.Parallel()
	if _, got := writtenByDeployRead(t); got != pinBothSorted {
		t.Errorf("deploy read wrote %q, want %q", got, pinBothSorted)
	}
	if _, got := writtenByComposition(t); got != pinBothSorted {
		t.Errorf("composition wrote %q, want %q", got, pinBothSorted)
	}
}
