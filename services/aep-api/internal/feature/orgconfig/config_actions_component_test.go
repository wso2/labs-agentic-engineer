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
package orgconfig_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/api"
	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/models"
)

// fakeThunder is the component tier's Thunder admin stub — only Regenerate is
// exercised by the client-secret rotation route; the rest panic if reached.
type fakeThunder struct {
	regenSecret string
	regenCalls  []string
}

var _ thundersvc.Client = (*fakeThunder)(nil)

func (f *fakeThunder) RegenerateClientSecret(_ context.Context, org string) (string, error) {
	f.regenCalls = append(f.regenCalls, org)
	return f.regenSecret, nil
}
func (f *fakeThunder) EnsurePublisherApp(context.Context, string, string) (string, string, bool, error) {
	panic("fakeThunder: EnsurePublisherApp unexpected")
}
func (f *fakeThunder) DeletePublisherApp(context.Context, string) (bool, error) {
	panic("fakeThunder: DeletePublisherApp unexpected")
}
func (f *fakeThunder) OUExists(context.Context, string) (bool, error) {
	panic("fakeThunder: OUExists unexpected")
}

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

// --- IDP client-secret rotation ---------------------------------------------

func TestConfigComponent_RotateClientSecret_Happy(t *testing.T) {
	t.Parallel()
	th := &fakeThunder{regenSecret: "rotated-secret-123"}
	c := newConfigHarnessOpts(t, th, "")
	// Seed a profile that already has a publisher client, so Regenerate has
	// something to rotate.
	now := time.Now().UTC()
	if err := c.db.Create(&models.OrganizationIDPProfile{
		OrgID: "acme", Kind: "platform", Issuer: platformIss, JWKSURL: platformJWKS,
		PublisherClientID: "pub-x", PublisherClientSecret: "old", CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	resp := c.h.AsOrg("acme").Post(configPath+"/idp/client-secret", "")
	if resp.Code != 200 {
		t.Fatalf("rotate: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeCfg(t, resp.Body.Bytes()); m["clientSecret"] != "rotated-secret-123" {
		t.Fatalf("rotate body drifted: %v", m)
	}
	if len(th.regenCalls) != 1 || th.regenCalls[0] != "acme" {
		t.Fatalf("rotate must call Thunder once for the token org: %v", th.regenCalls)
	}
}

func TestConfigComponent_RotateClientSecret_503WhenThunderUnset(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t) // nil Thunder
	resp := c.h.AsOrg("acme").Post(configPath+"/idp/client-secret", "")
	if resp.Code != 503 {
		t.Fatalf("rotate (no thunder): want 503, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// --- discovery --------------------------------------------------------------

func TestConfigComponent_Discovery_Happy(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	var issuerURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"issuer":"` + issuerURL + `","jwks_uri":"` + issuerURL + `/oauth2/jwks"}`))
	}))
	t.Cleanup(srv.Close)
	issuerURL = srv.URL

	resp := c.h.AsOrg("acme").Get(configPath + "/idp/discovery?issuer=" + issuerURL)
	if resp.Code != 200 {
		t.Fatalf("discovery: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	m := decodeCfg(t, resp.Body.Bytes())
	if m["issuer"] != issuerURL || m["jwksUrl"] != issuerURL+"/oauth2/jwks" {
		t.Fatalf("discovery body drifted: %v", m)
	}
}

func TestConfigComponent_Discovery_MissingIssuer400(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	resp := c.h.AsOrg("acme").Get(configPath + "/idp/discovery")
	if resp.Code != 400 {
		t.Fatalf("discovery (no issuer): want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestConfigComponent_Discovery_Upstream502(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	resp := c.h.AsOrg("acme").Get(configPath + "/idp/discovery?issuer=" + srv.URL)
	if resp.Code != 502 {
		t.Fatalf("discovery (upstream 404): want 502, got %d body=%s", resp.Code, resp.Body.String())
	}
}

// --- H1. Legacy routes retired ----------------------------------------------

func TestConfigComponent_H1_LegacyRoutesRetired(t *testing.T) {
	t.Parallel()
	// Spec-level: no /org/* path survives (Decision 3).
	spec, err := api.GenerateOpenAPIYAML()
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if strings.Contains(string(spec), "/org/") {
		t.Fatalf("generated spec still carries /org/* routes")
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
