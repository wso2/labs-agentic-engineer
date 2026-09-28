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
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/spec"
)

const tryIt = "http://tryit.aep.localhost:8095/callback"

// The fixed callback joins the set beside the web apps' own, deduped and sorted,
// so a project with a SPA registers both and the patch stays byte-stable.
func Test_consumerCallbackSet_carriesTheFixedCallback(t *testing.T) {
	t.Parallel()
	design := readDesign(t, map[string]string{
		spec.DesignRootFile:          rootDesignMd(),
		"components/web/design.json": webappWithPR("web", []prDep{{"user-auth", "thunder-app"}}),
	})
	originOf := func(string) string { return "http://web.local" }

	got := consumerCallbackSet(design, "user-auth", "/callback", originOf, tryIt, tryIt, "  ")
	want := []string{tryIt, "http://web.local/callback"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("callbacks = %v; want %v (fixed first by sort, deduped, blank dropped)", got, want)
	}
	if again := consumerCallbackSet(design, "user-auth", "/callback", originOf); len(again) != 1 || again[0] != want[1] {
		t.Errorf("without a fixed callback the set must be unchanged: %v", again)
	}
}

// An agent-only project has no web app, so nothing registered a redirect URI
// on its sign-in resource and no client outside a component could sign in. Its
// deploy now registers the platform tester's callback — and only that, since
// there is no SPA origin to add — while computing no file for the agent.
func Test_FilesForComponent_agentRegistersTheFixedCallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	files := map[string]string{
		spec.DesignRootFile:            rootDesignMd(),
		"components/agent/design.json": buildComponentJSON("agent", "ai-agent", nil, []prDep{{"user-auth", "thunder-app"}}),
	}
	rc := rcOutputs(authOutputs(), nil)
	svc := svcWithCatalog(ocResolving(nil), rc, storeWith(files), &fakeCatalog{markers: authMarkers("thunder-app")})
	svc.SetTryItCallbackURL(tryIt)

	got, ready, err := svc.FilesForComponent(ctx, "acme", "proj", "agent")
	if err != nil || !ready || len(got) != 0 {
		t.Fatalf("an agent composes no file: files=%v ready=%v err=%v", got, ready, err)
	}
	calls := rc.PatchBindingEnvironmentConfigsCalls()
	if len(calls) != 1 {
		t.Fatalf("want exactly 1 callback patch; got %d", len(calls))
	}
	if calls[0].BindingName != "proj-user-auth-default" {
		t.Errorf("patched binding = %q; want proj-user-auth-default", calls[0].BindingName)
	}
	if got := calls[0].Configs["redirectUris"]; got != tryIt {
		t.Errorf("redirectUris = %q; want only the fixed callback %q", got, tryIt)
	}
}

// Not configured means not registered: the agent path is a no-op, exactly the
// behaviour before the callback existed, and no patch reaches the binding.
func Test_FilesForComponent_agentWithoutAFixedCallbackPatchesNothing(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:            rootDesignMd(),
		"components/agent/design.json": buildComponentJSON("agent", "ai-agent", nil, []prDep{{"user-auth", "thunder-app"}}),
	}
	rc := rcOutputs(authOutputs(), nil)
	svc := svcWithCatalog(ocResolving(nil), rc, storeWith(files), &fakeCatalog{markers: authMarkers("thunder-app")})

	if _, ready, err := svc.FilesForComponent(context.Background(), "acme", "proj", "agent"); err != nil || !ready {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	if n := len(rc.PatchBindingEnvironmentConfigsCalls()); n != 0 {
		t.Errorf("no fixed callback configured, yet %d patch(es) were made", n)
	}
}

// A component with no sign-in dependency has nothing to register, whatever is
// configured — the patch is driven by the dependency's marker, not by the type.
func Test_FilesForComponent_agentWithoutSignInDepPatchesNothing(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:            rootDesignMd(),
		"components/agent/design.json": buildComponentJSON("agent", "ai-agent", nil, []prDep{{"orders-db", "postgres-cnpg"}}),
	}
	rc := rcOutputs(authOutputs(), nil)
	svc := svcWithCatalog(ocResolving(nil), rc, storeWith(files), &fakeCatalog{markers: authMarkers("thunder-app")})
	svc.SetTryItCallbackURL(tryIt)

	if _, _, err := svc.FilesForComponent(context.Background(), "acme", "proj", "agent"); err != nil {
		t.Fatal(err)
	}
	if n := len(rc.PatchBindingEnvironmentConfigsCalls()); n != 0 {
		t.Errorf("a database dependency carries no consumer-URL marker, yet %d patch(es) were made", n)
	}
}
