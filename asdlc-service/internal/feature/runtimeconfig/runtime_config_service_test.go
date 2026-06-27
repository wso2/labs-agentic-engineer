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

	"github.com/wso2/asdlc/asdlc-service/internal/feature/artifacts"
	"github.com/wso2/asdlc/asdlc-service/models"
)

// Test_renderEnvConfigJS — the file content must be deterministic + valid
// JS. Keys are sorted so identical inputs produce byte-identical files.
//
// window._env_ now carries only browser-direct config: THUNDER_* (the
// OIDC issuer the browser redirects to — can't be proxied) plus
// user-config / feature flags. Backend service URLs no longer appear
// here; the SPA reaches its backends through its own nginx reverse-proxy.
func Test_renderEnvConfigJS(t *testing.T) {
	values := map[string]interface{}{
		"THUNDER_URL":           "http://thunder.openchoreo.localhost:8080",
		"THUNDER_CLIENT_ID":     "todo-project",
		"SUPPORT_EMAIL":         "support@example.com",
		"FEATURE_NEW_DASHBOARD": false,
	}
	got := renderEnvConfigJS(values)

	for _, want := range []string{
		"window._env_ = {",
		`THUNDER_URL: "http://thunder.openchoreo.localhost:8080"`,
		`THUNDER_CLIENT_ID: "todo-project"`,
		`FEATURE_NEW_DASHBOARD: false`,
		`SUPPORT_EMAIL: "support@example.com"`,
		"};",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderEnvConfigJS output missing %q\ngot:\n%s", want, got)
		}
	}

	// Backend service URLs must NOT be emitted into window._env_ — the
	// SPA reaches backends via its same-origin nginx proxy now.
	for _, banned := range []string{"API_BASE_URL", "TODO_API_URL"} {
		if strings.Contains(got, banned) {
			t.Errorf("renderEnvConfigJS unexpectedly emitted backend URL key %q\ngot:\n%s", banned, got)
		}
	}

	// Verify keys are emitted in sorted order so the rendered JS is
	// byte-stable (so equality checks against the on-cluster file
	// don't flap).
	wantOrder := []string{"FEATURE_NEW_DASHBOARD", "SUPPORT_EMAIL", "THUNDER_CLIENT_ID", "THUNDER_URL"}
	prev := 0
	for _, k := range wantOrder {
		i := strings.Index(got, k+":")
		if i < 0 {
			t.Fatalf("key %s not present in output", k)
		}
		if i < prev {
			t.Errorf("key %s appears before prior key (got pos %d, want > %d)\noutput:\n%s", k, i, prev, got)
		}
		prev = i
	}
}

// Test_buildEnvValues_omitsBackendURLs — a web-app with `kind: component`
// service dependencies must NOT get `API_BASE_URL` / `<DEP>_URL` keys in
// its window._env_: those backends are now reached through the SPA's own
// nginx reverse-proxy (a dependencies.endpoints WorkloadConnection), not
// the browser config. And because no backend URL must be resolved, the
// emit is `ready` even with no component client wired and no OIDC.
func Test_buildEnvValues_omitsBackendURLs(t *testing.T) {
	webapp := &models.DesignComponent{
		Name:          "todo-ui",
		ComponentType: "web-app",
		Dependencies: []models.Dependency{
			{Kind: models.DependencyKindComponent, Name: "todo-api"},
		},
	}
	design := &artifacts.DesignFile{
		Components: []models.DesignComponent{
			*webapp,
			{Name: "todo-api", ComponentType: "service"},
		},
	}

	// No component client / thunder admin wired. With the backend-URL
	// loop removed, buildEnvValues must not touch OC and must return a
	// ready, backend-free map.
	s := &RuntimeConfigService{}
	out, ready := s.buildEnvValues(context.Background(), "org", "proj", webapp, design)

	if !ready {
		t.Fatalf("buildEnvValues ready = false; want true (no backend URL gating, no OIDC)")
	}
	for k := range out {
		if strings.HasSuffix(k, "_URL") || k == "API_BASE_URL" {
			t.Errorf("buildEnvValues emitted backend URL key %q; want none", k)
		}
	}
	if _, ok := out["TODO_API_URL"]; ok {
		t.Errorf("buildEnvValues emitted TODO_API_URL; backend URLs must not be in window._env_")
	}
}

// Test_upperSnakeKey — kebab-case + dash + camelCase normalise to safe
// JS identifier prefixes.
func Test_upperSnakeKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"todo-api", "TODO_API"},
		{"todo_api", "TODO_API"},
		{"TodoApi", "TODOAPI"},
		{"todo--api", "TODO_API"},
		{"--todo-api--", "TODO_API"},
		{"", ""},
		{"a", "A"},
		{"a-b-c", "A_B_C"},
		{"todo-api-v2", "TODO_API_V2"},
	}
	for _, c := range cases {
		got := upperSnakeKey(c.in)
		if got != c.want {
			t.Errorf("upperSnakeKey(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}
