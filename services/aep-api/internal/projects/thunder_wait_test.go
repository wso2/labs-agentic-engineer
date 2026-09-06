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
	"context"
	"errors"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// UNIT tier for the deploy-verdict thunder wait: DeploymentState is not
// Ready for a web-app that declares a consumer-URL CRT until the
// ThunderApplication CR carries the SPA callback. Seam is
// DeploymentService.DeploymentState; OC status, origin, patch, catalog, and
// Thunder CR are fakes. Design parse is the real ArtifactStore.

const (
	thunderWaitOrg     = "acme"
	thunderWaitProject = "proj"
	thunderWaitWeb     = "web"
	thunderWaitAPI     = "api"
	thunderWaitDep     = "idp"
	// Origin fixture with a trailing slash — the wait must trim before joining /callback.
	thunderWaitOrigin   = "http://web.local/"
	thunderWaitCallback = "http://web.local/callback"
	thunderWaitPending  = "https://pending.invalid/callback"
	// Known-good OC names: ExternalResourceName("proj","idp") /
	// ExternalResourceBindingName("proj","idp","default").
	// thunderWaitCRName is the Resource name on openchoreo.dev/resource, not
	// the rendered object name r-<resource>-default-<hash8>.
	thunderWaitCRName      = "proj-idp"
	thunderWaitBindingName = "proj-idp-default"
)

// fakeThunderReader is a hand double of ThunderApplicationReader.
type fakeThunderReader struct {
	view     *ThunderApplicationView
	err      error
	resource string
	env      string
	gets     int
}

func (f *fakeThunderReader) FindByResource(_ context.Context, resourceName, environment string) (*ThunderApplicationView, error) {
	f.gets++
	f.resource = resourceName
	f.env = environment
	return f.view, f.err
}

// fakeMarkerCatalog is a hand double of resourceMarkerCatalog. thunder-app
// carries EnvConfig; postgres-cnpg does not — the wait keys on the marker,
// never the type name.
type fakeMarkerCatalog struct {
	byName map[string]ConsumerURLMarker
	err    error
}

func (f *fakeMarkerCatalog) MarkersByName(context.Context) (map[string]ConsumerURLMarker, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byName, nil
}

func thunderWaitCatalog() *fakeMarkerCatalog {
	return &fakeMarkerCatalog{byName: map[string]ConsumerURLMarker{
		"thunder-app":   {EnvConfig: "redirectUris", Path: "/callback"},
		"postgres-cnpg": {},
	}}
}

func webAppWithPlatformResource(name, depName, resourceType string) string {
	return "{\n  \"name\": \"" + name + "\",\n  \"type\": \"web-application\",\n  \"description\": \"SPA.\",\n  \"" + "depend" + "encies\": [\n    {\"kind\": \"platform-resource\", \"name\": \"" + depName + "\", \"resourceType\": \"" + resourceType + "\"}\n  ]\n}\n"
}

func serviceWithPlatformResource(name, depName, resourceType string) string {
	return "{\n  \"name\": \"" + name + "\",\n  \"type\": \"service\",\n  \"description\": \"API.\",\n  \"" + "depend" + "encies\": [\n    {\"kind\": \"platform-resource\", \"name\": \"" + depName + "\", \"resourceType\": \"" + resourceType + "\"}\n  ]\n}\n"
}

func ocReadySummary() *openchoreo.ReleaseBindingSummary {
	return &openchoreo.ReleaseBindingSummary{ReadyStatus: "True"}
}

type thunderWaitOpts struct {
	component  string
	designBody string
	origin     string
	summary    *openchoreo.ReleaseBindingSummary
	cr         *ThunderApplicationView
	patchErr   error
	thunderErr error
}

type thunderWaitHarness struct {
	svc     *DeploymentService
	oc      *mocks.ComponentClientMock
	rc      *mocks.ResourceClientMock
	thunder *fakeThunderReader
}

func newThunderWaitHarness(t *testing.T, opts thunderWaitOpts) *thunderWaitHarness {
	t.Helper()
	if opts.component == "" {
		opts.component = thunderWaitWeb
	}
	if opts.summary == nil {
		opts.summary = ocReadySummary()
	}
	oc := &mocks.ComponentClientMock{
		GetReleaseBindingStatusFunc: func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
			return opts.summary, nil
		},
		ListDeploymentsFunc: func(_ context.Context, _, _, componentName string) (*gen.DeploymentList, error) {
			if opts.origin != "" && componentName == opts.component {
				return &gen.DeploymentList{Items: []gen.Deployment{{EndpointURL: opts.origin}}}, nil
			}
			return &gen.DeploymentList{}, nil
		},
	}
	rc := &mocks.ResourceClientMock{
		PatchBindingEnvironmentConfigsFunc: func(context.Context, string, string, map[string]string) error {
			return opts.patchErr
		},
	}
	thunder := &fakeThunderReader{view: opts.cr, err: opts.thunderErr}
	svc := NewDeploymentService(oc, traitStoreWith(map[string]string{
		spec.DesignRootFile:                             traitRootMd(),
		"components/" + opts.component + "/design.json": opts.designBody,
	}))
	svc.SetResourceCatalog(thunderWaitCatalog())
	svc.SetResourceClient(rc)
	svc.SetThunderApplicationReader(thunder)
	return &thunderWaitHarness{svc: svc, oc: oc, rc: rc, thunder: thunder}
}

func (h *thunderWaitHarness) state(t *testing.T, component string) (ready, failed bool) {
	t.Helper()
	got, err := h.svc.DeploymentState(context.Background(), thunderWaitOrg, thunderWaitProject, []string{component})
	if err != nil {
		t.Fatalf("DeploymentState: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 component, got %d", len(got))
	}
	return got[0].Ready, got[0].Failed
}

func assertRedirectPatch(t *testing.T, rc *mocks.ResourceClientMock, wantCallback string, wantN int) {
	t.Helper()
	calls := rc.PatchBindingEnvironmentConfigsCalls()
	if len(calls) != wantN {
		t.Fatalf("want %d PatchBindingEnvironmentConfigs call(s), got %d", wantN, len(calls))
	}
	last := calls[wantN-1]
	if last.OrgID != thunderWaitOrg {
		t.Errorf("patch org = %q, want %q", last.OrgID, thunderWaitOrg)
	}
	if last.BindingName != thunderWaitBindingName {
		t.Errorf("patch binding = %q, want %q", last.BindingName, thunderWaitBindingName)
	}
	if got := last.Configs["redirectUris"]; got != wantCallback {
		t.Errorf("patch redirectUris = %q, want %q", got, wantCallback)
	}
	if len(last.Configs) != 1 {
		t.Errorf("patch configs = %+v, want only redirectUris", last.Configs)
	}
}

func assertNoThunderConsult(t *testing.T, h *thunderWaitHarness) {
	t.Helper()
	if n := len(h.rc.PatchBindingEnvironmentConfigsCalls()); n != 0 {
		t.Errorf("patch called %d time(s); wait must not consult Thunder", n)
	}
	if h.thunder.gets != 0 {
		t.Errorf("Thunder GET called %d time(s); wait must not consult Thunder", h.thunder.gets)
	}
	if n := len(h.oc.ListDeploymentsCalls()); n != 0 {
		t.Errorf("ListDeployments called %d time(s); wait must not resolve origin", n)
	}
}

func webAppThunderOpts(origin string, cr *ThunderApplicationView) thunderWaitOpts {
	return thunderWaitOpts{
		component:  thunderWaitWeb,
		designBody: webAppWithPlatformResource(thunderWaitWeb, thunderWaitDep, "thunder-app"),
		origin:     origin,
		cr:         cr,
	}
}

func matchingThunderCR() *ThunderApplicationView {
	return &ThunderApplicationView{
		RedirectURIs:       thunderWaitCallback,
		Ready:              true,
		Generation:         2,
		ObservedGeneration: 2,
	}
}

func placeholderThunderCR() *ThunderApplicationView {
	return &ThunderApplicationView{
		RedirectURIs:       thunderWaitPending,
		Ready:              true,
		Generation:         1,
		ObservedGeneration: 1,
	}
}

// OpenChoreo Ready is not deployed while Thunder still has the placeholder
// callback. The wait must patch origin+/callback and leave Ready false.
func TestDeploymentState_WebAppThunderPlaceholderIsNotReady(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, webAppThunderOpts(thunderWaitOrigin, placeholderThunderCR()))

	ready, failed := h.state(t, thunderWaitWeb)
	if ready {
		t.Fatal("Ready=true while Thunder CR still has the placeholder callback")
	}
	if failed {
		t.Fatal("placeholder mismatch must stay pending, not Failed")
	}
	assertRedirectPatch(t, h.rc, thunderWaitCallback, 1)
	if h.thunder.gets != 1 || h.thunder.resource != thunderWaitCRName || h.thunder.env != openchoreo.DevEnvironmentName {
		t.Errorf("Thunder FindByResource = %d %s/%s, want 1 %s/%s", h.thunder.gets, h.thunder.resource, h.thunder.env, thunderWaitCRName, openchoreo.DevEnvironmentName)
	}
}

func TestDeploymentState_WebAppThunderCRNotReadyIsPending(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, webAppThunderOpts(thunderWaitOrigin, &ThunderApplicationView{
		RedirectURIs:       thunderWaitCallback,
		Ready:              false,
		Generation:         2,
		ObservedGeneration: 2,
	}))
	ready, failed := h.state(t, thunderWaitWeb)
	if ready || failed {
		t.Fatalf("ready/failed = %v/%v; CR Ready=false must stay pending", ready, failed)
	}
	assertRedirectPatch(t, h.rc, thunderWaitCallback, 1)
}

func TestDeploymentState_WebAppThunderStaleObservedGenerationIsPending(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, webAppThunderOpts(thunderWaitOrigin, &ThunderApplicationView{
		RedirectURIs:       thunderWaitCallback,
		Ready:              true,
		Generation:         3,
		ObservedGeneration: 2,
	}))
	ready, failed := h.state(t, thunderWaitWeb)
	if ready || failed {
		t.Fatalf("ready/failed = %v/%v; stale observedGeneration must stay pending", ready, failed)
	}
	assertRedirectPatch(t, h.rc, thunderWaitCallback, 1)
}

func TestDeploymentState_WebAppThunderMissingCRIsPending(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, webAppThunderOpts(thunderWaitOrigin, nil))
	ready, failed := h.state(t, thunderWaitWeb)
	if ready || failed {
		t.Fatalf("ready/failed = %v/%v; missing CR must stay pending", ready, failed)
	}
	assertRedirectPatch(t, h.rc, thunderWaitCallback, 1)
}

func TestDeploymentState_WebAppThunderEmptyOriginIsPending(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, webAppThunderOpts("", matchingThunderCR()))
	ready, failed := h.state(t, thunderWaitWeb)
	if ready || failed {
		t.Fatalf("ready/failed = %v/%v; empty origin must stay pending", ready, failed)
	}
	if n := len(h.rc.PatchBindingEnvironmentConfigsCalls()); n != 0 {
		t.Errorf("empty origin must not patch, got %d call(s)", n)
	}
	if h.thunder.gets != 0 {
		t.Errorf("empty origin must not GET the CR, got %d", h.thunder.gets)
	}
}

// Matching callback + CR ready at that generation is the wait's green.
func TestDeploymentState_WebAppThunderCallbackReady(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, webAppThunderOpts(thunderWaitOrigin, matchingThunderCR()))
	ready, failed := h.state(t, thunderWaitWeb)
	if !ready || failed {
		t.Fatalf("ready/failed = %v/%v; matching CR must be Ready", ready, failed)
	}
	assertRedirectPatch(t, h.rc, thunderWaitCallback, 1)
}

// Rebuild with the same origin re-patches (idempotent at the client) and stays
// Ready when the CR already matches.
func TestDeploymentState_WebAppThunderRebuildSameOriginRepatchesAndStaysReady(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, webAppThunderOpts(thunderWaitOrigin, matchingThunderCR()))
	for i := 0; i < 2; i++ {
		ready, failed := h.state(t, thunderWaitWeb)
		if !ready || failed {
			t.Fatalf("pass %d: ready/failed = %v/%v; same-origin rebuild must stay Ready", i+1, ready, failed)
		}
	}
	assertRedirectPatch(t, h.rc, thunderWaitCallback, 2)
}

// A new origin patches the new callback and stays pending until the CR matches it.
func TestDeploymentState_WebAppThunderNewOriginPendingUntilCRMatches(t *testing.T) {
	t.Parallel()
	const newOrigin = "http://web-v2.local/"
	const newCallback = "http://web-v2.local/callback"
	h := newThunderWaitHarness(t, webAppThunderOpts(newOrigin, matchingThunderCR())) // CR still has the old callback
	ready, failed := h.state(t, thunderWaitWeb)
	if ready || failed {
		t.Fatalf("ready/failed = %v/%v; new origin must stay pending until CR matches", ready, failed)
	}
	assertRedirectPatch(t, h.rc, newCallback, 1)

	h.thunder.view = &ThunderApplicationView{
		RedirectURIs:       newCallback,
		Ready:              true,
		Generation:         4,
		ObservedGeneration: 4,
	}
	ready, failed = h.state(t, thunderWaitWeb)
	if !ready || failed {
		t.Fatalf("after CR match: ready/failed = %v/%v; want Ready", ready, failed)
	}
	assertRedirectPatch(t, h.rc, newCallback, 2)
}

// Service/API components never enter the wait, even when they declare thunder-app.
func TestDeploymentState_ServiceWithThunderAppSkipsWait(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, thunderWaitOpts{
		component:  thunderWaitAPI,
		designBody: serviceWithPlatformResource(thunderWaitAPI, thunderWaitDep, "thunder-app"),
		origin:     thunderWaitOrigin,
		cr:         placeholderThunderCR(),
	})
	ready, failed := h.state(t, thunderWaitAPI)
	if !ready || failed {
		t.Fatalf("ready/failed = %v/%v; service OC-Ready must be Ready without Thunder wait", ready, failed)
	}
	assertNoThunderConsult(t, h)
}

// A web-app whose only platform-resource has no ConsumerURLEnvConfig skips the wait.
func TestDeploymentState_WebAppPostgresOnlySkipsWait(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, thunderWaitOpts{
		component:  thunderWaitWeb,
		designBody: webAppWithPlatformResource(thunderWaitWeb, "orders-db", "postgres-cnpg"),
		origin:     thunderWaitOrigin,
		cr:         placeholderThunderCR(),
	})
	ready, failed := h.state(t, thunderWaitWeb)
	if !ready || failed {
		t.Fatalf("ready/failed = %v/%v; postgres-only web-app must be Ready without Thunder wait", ready, failed)
	}
	assertNoThunderConsult(t, h)
}

func TestDeploymentState_ThunderPatchErrorSurfaces(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, thunderWaitOpts{
		component:  thunderWaitWeb,
		designBody: webAppWithPlatformResource(thunderWaitWeb, thunderWaitDep, "thunder-app"),
		origin:     thunderWaitOrigin,
		cr:         matchingThunderCR(),
		patchErr:   errors.New("patch boom"),
	})
	_, err := h.svc.DeploymentState(context.Background(), thunderWaitOrg, thunderWaitProject, []string{thunderWaitWeb})
	if err == nil {
		t.Fatal("want patch error from DeploymentState, got nil")
	}
	if h.thunder.gets != 0 {
		t.Errorf("GET after a patch error: %d call(s)", h.thunder.gets)
	}
}

func TestDeploymentState_ThunderGetErrorSurfaces(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, thunderWaitOpts{
		component:  thunderWaitWeb,
		designBody: webAppWithPlatformResource(thunderWaitWeb, thunderWaitDep, "thunder-app"),
		origin:     thunderWaitOrigin,
		thunderErr: errors.New("get boom"),
	})
	_, err := h.svc.DeploymentState(context.Background(), thunderWaitOrg, thunderWaitProject, []string{thunderWaitWeb})
	if err == nil {
		t.Fatal("want CR GET error from DeploymentState, got nil")
	}
	assertRedirectPatch(t, h.rc, thunderWaitCallback, 1)
}

func TestDeploymentState_FailedOCDoesNotConsultThunder(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, thunderWaitOpts{
		component:  thunderWaitWeb,
		designBody: webAppWithPlatformResource(thunderWaitWeb, thunderWaitDep, "thunder-app"),
		origin:     thunderWaitOrigin,
		summary:    &openchoreo.ReleaseBindingSummary{ReadyStatus: "False", ReadyReason: "RenderingFailed"},
		cr:         matchingThunderCR(),
	})
	ready, failed := h.state(t, thunderWaitWeb)
	if ready || !failed {
		t.Fatalf("ready/failed = %v/%v; OC Failed must stay Failed", ready, failed)
	}
	assertNoThunderConsult(t, h)
}

func TestDeploymentState_UndeployDoesNotConsultThunder(t *testing.T) {
	t.Parallel()
	h := newThunderWaitHarness(t, thunderWaitOpts{
		component:  thunderWaitWeb,
		designBody: webAppWithPlatformResource(thunderWaitWeb, thunderWaitDep, "thunder-app"),
		origin:     thunderWaitOrigin,
		summary:    &openchoreo.ReleaseBindingSummary{Undeploy: true},
		cr:         placeholderThunderCR(),
	})
	ready, failed := h.state(t, thunderWaitWeb)
	if !ready || failed {
		t.Fatalf("ready/failed = %v/%v; Undeploy must stay that verdict", ready, failed)
	}
	assertNoThunderConsult(t, h)
}
