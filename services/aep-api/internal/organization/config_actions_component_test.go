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

// COMPONENT tier: the /config action routes (connect-sessions, disconnect,
// client-secret rotation, discovery) plus the migration assertions (H1 — the
// legacy /org/* routes are retired, not aliased). The action routes are path
// relocations over the reused orgcreds/idp services; these rows re-point the
// coverage that used to live in the deleted orgcreds/idp component tests onto
// the new paths (docs/design/org-config-consolidation.md §5.H, §7 Phase 3).
package organization_test

import (
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/contracttest"
)

// --- connect-sessions (App-mode OAuth start) --------------------------------

func TestConfigComponent_ConnectSessions_503WhenAppUnset(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t) // appClientID empty
	resp := c.h.AsOrg("acme").Post(configPath+"/git-provider/connect-sessions", `{}`)
	if resp.Code != 503 {
		t.Fatalf("connect-sessions (no app): want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestConfigComponent_ConnectSessions_ReturnsAuthorizeURL(t *testing.T) {
	t.Parallel()
	c := newConfigHarnessOpts(t, nil, "gh-client-xyz")
	resp := c.h.AsOrg("acme").Post(configPath+"/git-provider/connect-sessions", `{}`)
	if resp.Code != 200 {
		t.Fatalf("connect-sessions: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	m := decodeCfg(t, resp.Body.Bytes())
	authorizeURL, _ := m["authorizeUrl"].(string)
	if !strings.Contains(authorizeURL, "gh-client-xyz") {
		t.Fatalf("authorizeUrl must carry the app client id: %q", authorizeURL)
	}
	// The redirect_uri still points at the UNCHANGED callback path (the callback
	// keeps its /org/... path — state-JWT authed on the outer mux).
	if !strings.Contains(authorizeURL, "org%2Fcredentials%2Fgithub%2Fconnect%2Fcallback") {
		t.Fatalf("authorizeUrl redirect_uri must point at the callback path: %q", authorizeURL)
	}
}

// --- disconnect -------------------------------------------------------------

func TestConfigComponent_Disconnect_ConnectedThenGone(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.gh.patHappy()
	if r := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":{"kind":"github","mode":"pat","pat":"ghp","githubLogin":"ada"}}`); r.Code != 200 {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Post(configPath+"/git-provider/disconnect", `{}`)
	if resp.Code != 200 {
		t.Fatalf("disconnect: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeCfg(t, resp.Body.Bytes()); m["status"] != "disconnected" {
		t.Fatalf("disconnect body drifted: %v", m)
	}
	// The credential row is retained (status flip, not delete — audit trail and
	// app re-adoption need it), but the config contract says null = not
	// connected (types.go, ADR-0009): a disconnected org must project
	// gitProvider as null so the console re-gates onboarding at the GitHub step.
	if m := decodeCfg(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes()); m["gitProvider"] != nil {
		t.Fatalf("disconnected gitProvider must project as null: %v", m["gitProvider"])
	}
}

func TestConfigComponent_Disconnect_NeverConnected(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	resp := c.h.AsOrg("acme").Post(configPath+"/git-provider/disconnect", `{}`)
	if resp.Code != 200 {
		t.Fatalf("disconnect (never): want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeCfg(t, resp.Body.Bytes()); m["status"] != "not_connected" {
		t.Fatalf("never-connected body drifted: %v", m)
	}
}

// --- H1. Legacy routes retired ----------------------------------------------

func TestConfigComponent_H1_LegacyRoutesRetired(t *testing.T) {
	t.Parallel()
	// Contract-level: no /org/* path survives (Decision 3). The served spec
	// IS the committed contract (embedded at build time).
	spec := contracttest.SourceYAML(t)
	if strings.Contains(string(spec), "/org/") {
		t.Fatalf("committed contract still carries /org/* routes")
	}

	// Runtime: the retired user-JWT routes 404 (not aliased).
	c := newConfigHarness(t)
	for _, p := range []string{
		"/api/v1/org/credentials/anthropic",
		"/api/v1/org/credentials/github",
		"/api/v1/org/idp",
		"/api/v1/org/skills",
	} {
		if resp := c.h.AsOrg("acme").Get(p); resp.Code != 404 {
			t.Fatalf("legacy route %s must 404, got %d", p, resp.Code)
		}
	}
}
