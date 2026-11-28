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
	"errors"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// resolvedEnv is a write target unlike testWriteTarget, so a test can tell a
// resolved environment from a leftover constant.
const resolvedEnv = "development"

func webWithSignIn() map[string]string {
	return map[string]string{
		spec.DesignRootFile:          rootDesignMd(),
		"components/web/design.json": webappWithPR("web", []prDep{{"user-auth", "idp-app"}}),
	}
}

// The web app's platform-resource outputs are read from, and its callback
// patched onto, the binding in the project's write target.
func Test_FilesForComponent_readsTheWriteTargetsBinding(t *testing.T) {
	t.Parallel()
	binding := "proj-user-auth-" + resolvedEnv
	rc := rcBindings(map[string]map[string]string{binding: authOutputs()}, nil)
	svc := svcWithCatalog(ocResolving(map[string]string{"web": "http://web.local/"}), rc, storeWith(webWithSignIn()),
		&fakeCatalog{markers: authMarkers("idp-app")})
	svc.SetWriteTargets(staticWriteTarget{env: resolvedEnv})

	files, ready, err := svc.FilesForComponent(context.Background(), "acme", "proj", "web")
	if err != nil || !ready || len(files) != 1 {
		t.Fatalf("FilesForComponent: files=%v ready=%v err=%v", files, ready, err)
	}
	if !strings.Contains(files[0].Value, `USER_AUTH_CLIENT_ID: "web-cid"`) {
		t.Fatalf("outputs of %s missing from env-config.js:\n%s", binding, files[0].Value)
	}
	for _, call := range rc.GetBindingCalls() {
		if call.Name != binding {
			t.Errorf("read binding %q; want only %q", call.Name, binding)
		}
	}
	for _, call := range rc.PatchBindingEnvironmentConfigsCalls() {
		if call.BindingName != binding {
			t.Errorf("patched binding %q; want %q", call.BindingName, binding)
		}
	}
}

// A web app whose write target cannot be resolved composes no file (the deploy
// leaves the binding's files untouched), and reads no binding.
func Test_FilesForComponent_noWriteTargetComposesNothing(t *testing.T) {
	t.Parallel()
	rc := rcOutputs(authOutputs(), nil)
	svc := svcWithCatalog(ocResolving(nil), rc, storeWith(webWithSignIn()), &fakeCatalog{markers: authMarkers("idp-app")})
	cause := &openchoreo.ErrNoWriteTarget{Org: "acme", Project: "proj", Cause: openchoreo.ErrPipelineRefMissing}
	svc.SetWriteTargets(staticWriteTarget{err: cause})

	files, ready, err := svc.FilesForComponent(context.Background(), "acme", "proj", "web")
	if !errors.Is(err, cause) || ready || files != nil {
		t.Fatalf("want (nil, false, ErrNoWriteTarget); got files=%v ready=%v err=%v", files, ready, err)
	}
	if n := len(rc.GetBindingCalls()); n != 0 {
		t.Fatalf("no binding may be read without a write target; got %d reads", n)
	}
}

// An agent's callback registration is best-effort: no write target skips it.
func Test_FilesForComponent_agentWithNoWriteTargetPatchesNothing(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		spec.DesignRootFile:            rootDesignMd(),
		"components/agent/design.json": buildComponentJSON("agent", "ai-agent", nil, []prDep{{"user-auth", "idp-app"}}),
	}
	rc := rcOutputs(authOutputs(), nil)
	svc := svcWithCatalog(ocResolving(nil), rc, storeWith(files), &fakeCatalog{markers: authMarkers("idp-app")})
	svc.SetTryItCallbackURL(tryIt)
	svc.SetWriteTargets(staticWriteTarget{err: errors.New("openchoreo: 503")})

	got, ready, err := svc.FilesForComponent(context.Background(), "acme", "proj", "agent")
	if err != nil || !ready || len(got) != 0 {
		t.Fatalf("an agent composes no file: files=%v ready=%v err=%v", got, ready, err)
	}
	if n := len(rc.PatchBindingEnvironmentConfigsCalls()); n != 0 {
		t.Fatalf("want no callback patch without a write target; got %d", n)
	}
}
