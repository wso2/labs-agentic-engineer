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

package runtimeconfig

import (
	"context"
	"testing"

	"github.com/wso2/aep/aep-api/internal/spec"
)

// UNIT tier for the consumer-URL registration when SEVERAL web apps declare ONE
// dependency — the shape the per-component write could not represent.
//
// `cell-design` models `user-auth` as a single east external that many
// components edge into, so two SPAs share one sign-in client and one
// `redirectUris`. Emitting env-config.js per web app used to write that single
// field with only the composing app's callback, so each pass replaced the last
// and only one SPA was ever registered.

const (
	sharedDepName  = "user-auth"
	sharedGuestURL = "http://guest.local/"
	sharedAdminURL = "http://admin.local/"
	sharedGuestCB  = "http://guest.local/callback"
	sharedAdminCB  = "http://admin.local/callback"
	// sharedBothCB is the joined value in the sort order the write must produce.
	sharedBothCB = sharedAdminCB + "," + sharedGuestCB
)

func twoWebAppsSharingAuth() map[string]string {
	return map[string]string{
		spec.DesignRootFile:            rootDesignMd(),
		"components/guest/design.json": webappWithPR("guest", []prDep{{sharedDepName, "thunder-app"}}),
		"components/admin/design.json": webappWithPR("admin", []prDep{{sharedDepName, "thunder-app"}}),
	}
}

func TestEmit_SharedAuthDependencyRegistersEveryWebAppsCallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	design := readDesign(t, twoWebAppsSharingAuth())
	oc := ocResolving(map[string]string{"guest": sharedGuestURL, "admin": sharedAdminURL})
	rc := rcOutputs(authOutputs(), nil)
	svc := svcWithCatalog(oc, rc, nil, &fakeCatalog{markers: authMarkers("thunder-app")})

	// Composing ONE web app's env-config.js still registers the whole project's
	// set — the field belongs to the dependency, not to the app being composed.
	if _, ready := svc.buildEnvValues(ctx, "acme", "proj", design, componentNamed(t, design, "guest")); !ready {
		t.Fatal("want ready=true for the guest web app")
	}

	calls := rc.PatchBindingEnvironmentConfigsCalls()
	if len(calls) != 1 {
		t.Fatalf("want 1 consumer-URL patch, got %d", len(calls))
	}
	if got := calls[0].Configs["redirectUris"]; got != sharedBothCB {
		t.Fatalf("patched redirectUris = %q, want both apps' callbacks sorted (%q)", got, sharedBothCB)
	}
}

// Composing the SIBLING must write the SAME value. This is what stops the two
// emissions overwriting each other, and — because the binding client skips an
// unchanged value — what stops the cascade churning the CR.
func TestEmit_SharedAuthDependencyWritesTheSameValueForEveryWebApp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	design := readDesign(t, twoWebAppsSharingAuth())
	oc := ocResolving(map[string]string{"guest": sharedGuestURL, "admin": sharedAdminURL})
	rc := rcOutputs(authOutputs(), nil)
	svc := svcWithCatalog(oc, rc, nil, &fakeCatalog{markers: authMarkers("thunder-app")})

	for _, name := range []string{"guest", "admin", "guest", "admin"} {
		if _, ready := svc.buildEnvValues(ctx, "acme", "proj", design, componentNamed(t, design, name)); !ready {
			t.Fatalf("want ready=true for %s", name)
		}
	}

	calls := rc.PatchBindingEnvironmentConfigsCalls()
	if len(calls) < 2 {
		t.Fatalf("want a patch per emission, got %d", len(calls))
	}
	for i, c := range calls {
		if got := c.Configs["redirectUris"]; got != sharedBothCB {
			t.Fatalf("emission %d patched %q, want %q — the value must not depend on which app is composing",
				i, got, sharedBothCB)
		}
	}
}

// A web app whose public URL has not resolved contributes nothing, and must not
// take its sibling's registration down with it.
func TestEmit_SharedAuthDependencySkipsUnresolvedWebApps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	design := readDesign(t, twoWebAppsSharingAuth())
	oc := ocResolving(map[string]string{"admin": sharedAdminURL})
	rc := rcOutputs(authOutputs(), nil)
	svc := svcWithCatalog(oc, rc, nil, &fakeCatalog{markers: authMarkers("thunder-app")})

	if _, ready := svc.buildEnvValues(ctx, "acme", "proj", design, componentNamed(t, design, "admin")); !ready {
		t.Fatal("want ready=true for the admin web app")
	}

	calls := rc.PatchBindingEnvironmentConfigsCalls()
	if len(calls) != 1 {
		t.Fatalf("want 1 consumer-URL patch, got %d", len(calls))
	}
	if got := calls[0].Configs["redirectUris"]; got != sharedAdminCB {
		t.Fatalf("patched redirectUris = %q, want only the resolved %q", got, sharedAdminCB)
	}
}
