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
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/clients/openchoreo/mocks"
	"github.com/wso2/aep/aep-api/internal/delivery"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// UNIT tier for the case the per-component wait could not represent: SEVERAL web
// apps declaring ONE consumer-URL dependency. cell-design models `user-auth` as a
// single external that many components edge into, so they share one Thunder
// client and one `redirectUris` field. Registering per component wrote that field
// once per web app, each call replacing the last, so only the final one in the
// loop was ever registered and every other stayed pending until the deploy budget
// expired.
//
// Seam is DeploymentService.DeploymentState, same as thunder_wait_test.go.

const (
	sharedAdmin       = "admin"
	sharedGuest       = "guest"
	sharedAdminOrigin = "http://admin.local/"
	sharedGuestOrigin = "http://guest.local/"
	sharedAdminCB     = "http://admin.local/callback"
	sharedGuestCB     = "http://guest.local/callback"
	// sharedBothCB is the joined value, in the sort order registration must
	// produce: "admin" sorts before "guest".
	sharedBothCB = sharedAdminCB + "," + sharedGuestCB
)

func plainWebApp(name string) string {
	return "{\n  \"name\": \"" + name + "\",\n  \"type\": \"web-application\",\n  \"description\": \"SPA.\"\n}\n"
}

type sharedDepHarness struct {
	svc     *DeploymentService
	oc      *mocks.ComponentClientMock
	rc      *mocks.ResourceClientMock
	thunder *fakeThunderReader
}

// newSharedDepHarness builds a project whose components are `designs`, with
// `origins` naming the external URL each one has resolved so far. A component
// absent from `origins` has not got a URL yet.
func newSharedDepHarness(t *testing.T, designs, origins map[string]string, cr *ThunderApplicationView) *sharedDepHarness {
	t.Helper()
	files := map[string]string{spec.DesignRootFile: traitRootMd()}
	for name, body := range designs {
		files["components/"+name+"/design.json"] = body
	}
	oc := &mocks.ComponentClientMock{
		GetReleaseBindingStatusFunc: func(context.Context, string, string, string, string) (*openchoreo.ReleaseBindingSummary, error) {
			return ocReadySummary(), nil
		},
		ListDeploymentsFunc: func(_ context.Context, _, _, componentName string) (*gen.DeploymentList, error) {
			if origin := origins[componentName]; origin != "" {
				return &gen.DeploymentList{Items: []gen.Deployment{{EndpointURL: origin}}}, nil
			}
			return &gen.DeploymentList{}, nil
		},
	}
	rc := &mocks.ResourceClientMock{
		PatchBindingEnvironmentConfigsFunc: func(context.Context, string, string, map[string]string) error {
			return nil
		},
	}
	thunder := &fakeThunderReader{view: cr}
	svc := NewDeploymentService(oc, traitStoreWith(files))
	svc.SetResourceCatalog(thunderWaitCatalog())
	svc.SetResourceClient(rc)
	svc.SetThunderApplicationReader(thunder)
	return &sharedDepHarness{svc: svc, oc: oc, rc: rc, thunder: thunder}
}

func (h *sharedDepHarness) state(t *testing.T, components ...string) map[string]delivery.ComponentDeploy {
	t.Helper()
	got, err := h.svc.DeploymentState(context.Background(), thunderWaitOrg, thunderWaitProject, components)
	if err != nil {
		t.Fatalf("DeploymentState: %v", err)
	}
	out := make(map[string]delivery.ComponentDeploy, len(got))
	for _, st := range got {
		out[st.Component] = st
	}
	return out
}

// registered returns the redirectUris value of the Nth (0-based) patch call.
func (h *sharedDepHarness) registered(t *testing.T, n int) string {
	t.Helper()
	calls := h.rc.PatchBindingEnvironmentConfigsCalls()
	if n >= len(calls) {
		t.Fatalf("want at least %d patch call(s), got %d", n+1, len(calls))
	}
	return calls[n].Configs["redirectUris"]
}

func twoWebAppsOneDep() map[string]string {
	return map[string]string{
		sharedAdmin: webAppWithPlatformResource(sharedAdmin, thunderWaitDep, "thunder-app"),
		sharedGuest: webAppWithPlatformResource(sharedGuest, thunderWaitDep, "thunder-app"),
	}
}

func bothOrigins() map[string]string {
	return map[string]string{sharedAdmin: sharedAdminOrigin, sharedGuest: sharedGuestOrigin}
}

func crWith(uris string) *ThunderApplicationView {
	return &ThunderApplicationView{RedirectURIs: uris, Ready: true, Generation: 3, ObservedGeneration: 3}
}

// Both web apps' callbacks go on in ONE write, and both components then read
// Ready. This is the regression: the per-component write produced two patches,
// the second overwriting the first, and left whichever app lost pending forever.
func TestDeploymentState_TwoWebAppsShareOneDependency_OneWriteCarriesBoth(t *testing.T) {
	t.Parallel()
	h := newSharedDepHarness(t, twoWebAppsOneDep(), bothOrigins(), crWith(sharedBothCB))

	states := h.state(t, sharedAdmin, sharedGuest)

	if n := len(h.rc.PatchBindingEnvironmentConfigsCalls()); n != 1 {
		t.Fatalf("patch called %d time(s); a shared dependency takes ONE write per read", n)
	}
	if got := h.registered(t, 0); got != sharedBothCB {
		t.Fatalf("registered redirectUris = %q, want %q", got, sharedBothCB)
	}
	for _, name := range []string{sharedAdmin, sharedGuest} {
		if st := states[name]; !st.Ready || st.Failed {
			t.Errorf("%s ready/failed = %v/%v (reason %q); both apps are registered, so both are Ready",
				name, st.Ready, st.Failed, st.Reason)
		}
	}
}

// The registered value must be byte-identical across reads. It is what makes the
// binding client's unchanged-value skip fire, and Go randomises map iteration —
// so an unsorted join would differ between two otherwise identical reads and turn
// every deploy poll into a real write.
func TestDeploymentState_SharedDependencyValueIsStableAcrossReads(t *testing.T) {
	t.Parallel()
	h := newSharedDepHarness(t, twoWebAppsOneDep(), bothOrigins(), crWith(sharedBothCB))

	const reads = 8
	for i := 0; i < reads; i++ {
		h.state(t, sharedAdmin, sharedGuest)
	}
	first := h.registered(t, 0)
	for i := 1; i < reads; i++ {
		if got := h.registered(t, i); got != first {
			t.Fatalf("read %d registered %q, read 0 registered %q; the value must not depend on map order",
				i, got, first)
		}
	}
	if first != sharedBothCB {
		t.Fatalf("registered redirectUris = %q, want %q", first, sharedBothCB)
	}
}

// A web app whose external URL has not resolved contributes no callback — and is
// the one held. Writing a placeholder for it, or dropping the sibling that IS up,
// would both be worse than waiting.
func TestDeploymentState_SharedDependencyRegistersOnlyResolvedOrigins(t *testing.T) {
	t.Parallel()
	h := newSharedDepHarness(t, twoWebAppsOneDep(),
		map[string]string{sharedAdmin: sharedAdminOrigin}, crWith(sharedAdminCB))

	states := h.state(t, sharedAdmin, sharedGuest)

	if got := h.registered(t, 0); got != sharedAdminCB {
		t.Fatalf("registered redirectUris = %q, want only the resolved %q", got, sharedAdminCB)
	}
	if st := states[sharedAdmin]; !st.Ready {
		t.Errorf("%s ready = false (reason %q); its callback is registered", sharedAdmin, st.Reason)
	}
	st := states[sharedGuest]
	if st.Ready {
		t.Fatalf("%s ready = true; it has no external URL yet", sharedGuest)
	}
	if st.Reason == "" {
		t.Fatalf("%s held with no reason; the deploy budget files the pending set and has no other cause to give", sharedGuest)
	}
}

// One app registered, the other not: only the unregistered one is held, and its
// reason names the dependency and the callback it is waiting on.
func TestDeploymentState_SharedDependencyHoldsOnlyTheUnregisteredApp(t *testing.T) {
	t.Parallel()
	h := newSharedDepHarness(t, twoWebAppsOneDep(), bothOrigins(), crWith(sharedAdminCB))

	states := h.state(t, sharedAdmin, sharedGuest)

	if st := states[sharedAdmin]; !st.Ready {
		t.Errorf("%s ready = false (reason %q); the CR carries its callback", sharedAdmin, st.Reason)
	}
	st := states[sharedGuest]
	if st.Ready {
		t.Fatalf("%s ready = true; the CR does not carry its callback", sharedGuest)
	}
	if !contains(st.Reason, thunderWaitDep) || !contains(st.Reason, sharedGuestCB) {
		t.Fatalf("%s reason = %q; it must name the dependency %q and the callback %q",
			sharedGuest, st.Reason, thunderWaitDep, sharedGuestCB)
	}
}

// A web app that declares no consumer-URL dependency is neither registered nor
// waited on — it is not part of this dependency's set.
func TestDeploymentState_WebAppWithoutTheDependencyContributesNothing(t *testing.T) {
	t.Parallel()
	designs := map[string]string{
		sharedAdmin: webAppWithPlatformResource(sharedAdmin, thunderWaitDep, "thunder-app"),
		sharedGuest: plainWebApp(sharedGuest),
	}
	h := newSharedDepHarness(t, designs, bothOrigins(), crWith(sharedAdminCB))

	states := h.state(t, sharedAdmin, sharedGuest)

	if got := h.registered(t, 0); got != sharedAdminCB {
		t.Fatalf("registered redirectUris = %q, want only %q", got, sharedAdminCB)
	}
	if st := states[sharedGuest]; !st.Ready || st.Reason != "" {
		t.Errorf("%s ready/reason = %v/%q; it declares no consumer-URL dependency", sharedGuest, st.Ready, st.Reason)
	}
}

func contains(haystack, needle string) bool {
	return haystack != "" && needle != "" && len(haystack) >= len(needle) &&
		indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
