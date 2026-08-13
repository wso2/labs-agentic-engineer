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

// COMPONENT tier, Component+DB flavor: the REAL
// orgconfig.Service over the REAL Anthropic/GitHub/IDP services, all sharing ONE
// pristine dbtest Postgres + real AES-GCM store, with only the out-of-process
// probes (Anthropic /v1/messages, api.github.com) faked at the HTTP boundary —
// behind the REAL production handler chain (faked auth at the jwt.WithClaims
// seam → orgensure → Huma parsing/validation → tenant gate ENFORCE → the
// section-pointered error mapping in config_huma.go), driven in-process via
// componenttest. These rows ARE the contract in
// docs/design/org-config-consolidation.md §5 (groups B–H); they were written
// against the surface before the route layer, TDD-style.
//
// The DB rides along (real upserts ARE the services), so this file self-skips
// under -short like the dbtest tier.
//
// External test package: the harness imports api, which imports orgconfig — an
// in-package test file would be an import cycle.
package organization_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/wso2/aep/aep-api/internal/clients/thundersvc"
	"github.com/wso2/aep/aep-api/internal/edge"
	"github.com/wso2/aep/aep-api/internal/organization"
	"github.com/wso2/aep/aep-api/internal/organization/httpapi"
	"github.com/wso2/aep/aep-api/internal/platform/componenttest"
	"github.com/wso2/aep/aep-api/internal/platform/contracttest"
	"github.com/wso2/aep/aep-api/internal/platform/dbtest"
	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

const (
	configPath   = "/api/v1/config"
	configAESKey = "0123456789abcdef0123456789abcdef"
	configEnvSec = "platform-webhook-secret"
	goodAnthKey  = "sk-ant-api03-CONFIGtestKeyABCDEFGHIJKLmnop"
	goodAnthKey2 = "sk-ant-api03-SECONDkeyZYXWVUTSRQPonmlk9999"
	platformIss  = "http://platform.test/issuer"
	platformJWKS = "http://platform.test/jwks"
)

// --- fakes ------------------------------------------------------------------

// anthropicFake is the out-of-process Anthropic /v1/messages probe. Its status
// is switchable mid-test so the probe-fail paths (C3/D-symmetry/F3) can flip a
// previously-good key to rejected.
type anthropicFake struct {
	*httptest.Server
	mu        sync.Mutex
	status    int
	rejectKey string
}

func newAnthropicFake(t *testing.T) *anthropicFake {
	t.Helper()
	a := &anthropicFake{status: http.StatusOK}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		code, reject := a.status, a.rejectKey
		a.mu.Unlock()
		if reject != "" && presentedKey(r) == reject {
			code = http.StatusUnauthorized
		}
		w.WriteHeader(code)
		_, _ = io.WriteString(w, `{"id":"msg_fake"}`)
	}))
	t.Cleanup(a.Server.Close)
	return a
}

func (a *anthropicFake) setStatus(code int) {
	a.mu.Lock()
	a.status = code
	a.mu.Unlock()
}

// rejectOnly fails the probe for ONE key and keeps every other key good. A
// blanket setStatus cannot isolate a later section's probe: the earlier ones
// abort the patch first, so the test would pass while never reaching the path
// it names.
func (a *anthropicFake) rejectOnly(key string) {
	a.mu.Lock()
	a.rejectKey = key
	a.mu.Unlock()
}

// presentedKey reads whichever credential header the probe used — an API key
// goes out as x-api-key, a Claude Code OAuth token as a bearer.
func presentedKey(r *http.Request) string {
	if k := r.Header.Get("x-api-key"); k != "" {
		return k
	}
	return strings.TrimPrefix(r.Header.Get("authorization"), "Bearer ")
}

// cfgFakeGH is the external-package fake api.github.com (mirrors the orgcreds
// component test's fakeGH — the internal stub isn't exported).
type cfgFakeGH struct {
	*httptest.Server
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
}

func newCfgFakeGH(t *testing.T) *cfgFakeGH {
	t.Helper()
	g := &cfgFakeGH{routes: map[string]http.HandlerFunc{}}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		h := g.routes[r.Method+" "+r.URL.Path]
		g.mu.Unlock()
		if h == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(g.Server.Close)
	return g
}

func (g *cfgFakeGH) on(method, path string, code int, body string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.routes[method+" "+path] = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
		_, _ = io.WriteString(w, body)
	}
}

// patHappy registers the responses a valid PAT connect for login "ada" needs.
func (g *cfgFakeGH) patHappy() {
	g.on("GET", "/user", 200, `{"login":"ada","name":"Ada","email":"ada@x.io"}`)
	g.on("GET", "/orgs/ada/repos", 200, `[]`)
}

// --- harness ----------------------------------------------------------------

type configHarness struct {
	h    *componenttest.Harness
	db   *gorm.DB
	gh   *cfgFakeGH
	anth *anthropicFake
}

// newConfigHarness assembles the real orgconfig.Service over one shared dbtest
// Postgres with real Anthropic/GitHub/IDP services and faked external probes.
// Thunder is nil and the GitHub App client id empty (the state-changing action
// routes that need them are covered separately via newConfigHarnessOpts).
func newConfigHarness(t *testing.T) *configHarness {
	return newConfigHarnessOpts(t, nil, "")
}

// newConfigHarnessOpts is newConfigHarness with the two knobs the action-route
// tests need: a fake Thunder admin client (for IDP client-secret rotation) and
// a GitHub App client id (for the connect-sessions authorize URL).
func newConfigHarnessOpts(t *testing.T, thunder thundersvc.Client, appClientID string) *configHarness {
	t.Helper()
	db := dbtest.New(t) // self-skips under -short
	gh := newCfgFakeGH(t)
	anth := newAnthropicFake(t)

	store, err := secrets.NewDBStore(db, []byte(configAESKey))
	if err != nil {
		t.Fatalf("NewDBStore: %v", err)
	}
	minter, err := secrets.NewAppTokenMinter(nil)
	if err != nil {
		t.Fatalf("NewAppTokenMinter: %v", err)
	}

	anthropicSvc := organization.NewAnthropicCredentialService(organization.NewOrgAnthropicRepository(db), store).WithAnthropicAPIBase(anth.URL)
	credSvc := organization.NewCredentialService(organization.NewOrgCredentialRepository(db, nil), store, minter, configEnvSec, "", "", nil).WithGitHubAPIBase(gh.URL)
	disconnectSvc := organization.NewOrgDisconnectService(credSvc, nil)
	bearerSvc := organization.NewBearerService("state-key", time.Minute)
	idpSvc := organization.NewIDPService(organization.NewIDPRepository(db, nil), organization.NewOrganizationRepository(db), thunder, organization.PlatformIDPConfig{Issuer: platformIss, JWKSURL: platformJWKS})

	svc := organization.NewService(
		anthropicSvc, credSvc, disconnectSvc, bearerSvc, idpSvc,
		organization.PlatformIDPConfig{Issuer: platformIss, JWKSURL: platformJWKS},
		"http://localhost:8090", appClientID,
	)

	// The harness wires the DOMAIN, not a loose service: the edge embeds
	// organization's handlers, so this assembles the same graph production does.
	h := componenttest.New(t, componenttest.Options{Deps: edge.Deps{Organization: mustNewOrgHandlers(t, organization.Deps{Config: svc})}})
	return &configHarness{h: h, db: db, gh: gh, anth: anth}
}

// mustNewOrgHandlers assembles the real organization domain around the given
// ports — the same httpapi.New the composition root calls — so a component test
// drives the domain the edge embeds, not a loose service.
func mustNewOrgHandlers(t *testing.T, d organization.Deps) *httpapi.Handlers {
	t.Helper()
	h, err := httpapi.New(d)
	if err != nil {
		t.Fatalf("assemble organization: %v", err)
	}
	return h
}

// --- decode helpers ---------------------------------------------------------

func decodeCfg(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body is not a JSON object: %v\n%s", err, body)
	}
	return m
}

func ptr[T any](v T) *T { return &v }

func llmConnect(key string) string {
	return `{"llm":{"kind":"anthropic","apiKey":"` + key + `"}}`
}

// --- B. GET /config ---------------------------------------------------------

func TestConfigComponent_B1_FreshOrg(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	resp := c.h.AsOrg("acme").Get(configPath)
	if resp.Code != 200 {
		t.Fatalf("get: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	m := decodeCfg(t, resp.Body.Bytes())
	if m["llm"] != nil {
		t.Fatalf("fresh org llm must be null: %v", m["llm"])
	}
	if m["gitProvider"] != nil {
		t.Fatalf("fresh org gitProvider must be null: %v", m["gitProvider"])
	}
	idpSec, ok := m["idp"].(map[string]any)
	if !ok {
		t.Fatalf("idp section must always be present: %v", m["idp"])
	}
	if idpSec["kind"] != "platform" || idpSec["issuer"] != platformIss || idpSec["jwksUrl"] != platformJWKS {
		t.Fatalf("fresh idp must be the platform default: %v", idpSec)
	}
	// GET is side-effect free — no idp row was created on read.
	var count int64
	c.db.Model(&organization.OrganizationIDPProfile{}).Where("org_id = ?", "acme").Count(&count)
	if count != 0 {
		t.Fatalf("GET must not persist an idp row, found %d", count)
	}
}

func TestConfigComponent_B2_AllConnectedNoSecrets(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.gh.patHappy()

	// Connect llm + gitProvider through the real PATCH path, and seed a custom
	// idp with a stored secret.
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("llm connect: %d %s", r.Code, r.Body.String())
	}
	if r := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":{"kind":"github","mode":"pat","pat":"ghp_live","githubLogin":"ada"}}`); r.Code != 200 {
		t.Fatalf("gitProvider connect: %d %s", r.Code, r.Body.String())
	}
	seedCustomIDP(t, c.db, "acme")

	resp := c.h.AsOrg("acme").Get(configPath)
	if resp.Code != 200 {
		t.Fatalf("get: %d %s", resp.Code, resp.Body.String())
	}
	body := resp.Body.String()
	m := decodeCfg(t, resp.Body.Bytes())
	llm := m["llm"].(map[string]any)
	if llm["kind"] != "anthropic" || llm["status"] != "active" {
		t.Fatalf("llm projection drifted: %v", llm)
	}
	gp := m["gitProvider"].(map[string]any)
	if gp["kind"] != "github" || gp["mode"] != "pat" {
		t.Fatalf("gitProvider projection drifted: %v", gp)
	}
	idpSec := m["idp"].(map[string]any)
	if idpSec["kind"] != "custom" || idpSec["hasClientSecret"] != true {
		t.Fatalf("idp projection drifted: %v", idpSec)
	}
	// No FULL secret material anywhere in the body. (The keyPrefix/keyLast4
	// display fragments are intentional and safe — only the full apiKey/pat/
	// clientSecret must never appear.)
	for _, secret := range []string{goodAnthKey, "ghp_live", "the-stored-secret"} {
		if strings.Contains(body, secret) {
			t.Fatalf("GET /config leaks secret material %q: %s", secret, body)
		}
	}
}

func TestConfigComponent_B3_LLMProbeFailedStatus(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	// Seed an llm row in a non-active state carrying a validation error.
	if err := c.db.Create(&organization.OrgAnthropicCredential{
		OcOrgID:         "acme",
		KeyPrefix:       "sk-ant-api03-XX",
		KeyLast4:        "9999",
		Status:          "invalid",
		ConnectedAt:     time.Now().UTC(),
		ValidationError: ptr("Anthropic rejected the key (401 Unauthorized)"),
	}).Error; err != nil {
		t.Fatalf("seed llm row: %v", err)
	}

	resp := c.h.AsOrg("acme").Get(configPath)
	m := decodeCfg(t, resp.Body.Bytes())
	llm := m["llm"].(map[string]any)
	if llm["status"] != "invalid" || llm["validationError"] != "Anthropic rejected the key (401 Unauthorized)" {
		t.Fatalf("llm status/validationError drifted: %v", llm)
	}
}

func TestConfigComponent_B4_GitHubAppMode(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	id := int64(4242)
	if err := c.db.Create(&organization.OrgCredential{
		OcOrgID:        "acme",
		Kind:           "app-installation",
		GitHubLogin:    "acme-org",
		IdentityName:   "AEP Bot",
		IdentityEmail:  "bot@aep.dev",
		IdentityLogin:  "aep[bot]",
		InstallationID: &id,
		SelectedRepos:  organization.JSONStringList{"acme/api", "acme/web"},
		Status:         "active",
		ConnectedAt:    time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed app row: %v", err)
	}

	resp := c.h.AsOrg("acme").Get(configPath)
	m := decodeCfg(t, resp.Body.Bytes())
	gp := m["gitProvider"].(map[string]any)
	if gp["mode"] != "app" || gp["installationId"] != float64(4242) {
		t.Fatalf("app-mode projection drifted: %v", gp)
	}
	repos, ok := gp["selectedRepos"].([]any)
	if !ok || len(repos) != 2 {
		t.Fatalf("app-mode selectedRepos drifted: %v", gp["selectedRepos"])
	}
}

func TestConfigComponent_B5_GitHubPatMode(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.gh.patHappy()
	if r := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":{"kind":"github","mode":"pat","pat":"ghp_live","githubLogin":"ada"}}`); r.Code != 200 {
		t.Fatalf("pat connect: %d %s", r.Code, r.Body.String())
	}

	resp := c.h.AsOrg("acme").Get(configPath)
	m := decodeCfg(t, resp.Body.Bytes())
	gp := m["gitProvider"].(map[string]any)
	if gp["mode"] != "pat" {
		t.Fatalf("pat-mode projection drifted: %v", gp)
	}
	if _, present := gp["installationId"]; present {
		t.Fatalf("pat-mode must omit installationId: %v", gp)
	}
}

func TestConfigComponent_B6_CustomIDP(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	seedCustomIDP(t, c.db, "acme")

	resp := c.h.AsOrg("acme").Get(configPath)
	m := decodeCfg(t, resp.Body.Bytes())
	idpSec := m["idp"].(map[string]any)
	if idpSec["kind"] != "custom" || idpSec["issuer"] != "https://byo.example" || idpSec["jwksUrl"] != "https://byo.example/jwks" {
		t.Fatalf("custom idp drifted: %v", idpSec)
	}
	if idpSec["hasClientSecret"] != true {
		t.Fatalf("hasClientSecret must reflect the stored secret: %v", idpSec)
	}
	if strings.Contains(resp.Body.String(), "the-stored-secret") {
		t.Fatalf("idp leaked the stored secret: %s", resp.Body.String())
	}
}

func TestConfigComponent_B7_NoOcOrgIdAnywhere(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.gh.patHappy()
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("llm connect: %d %s", r.Code, r.Body.String())
	}
	if r := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":{"kind":"github","mode":"pat","pat":"ghp_live","githubLogin":"ada"}}`); r.Code != 200 {
		t.Fatalf("gitProvider connect: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Get(configPath)
	if strings.Contains(resp.Body.String(), "ocOrgId") {
		t.Fatalf("ocOrgId must be dropped from all projections: %s", resp.Body.String())
	}
}

// --- C. PATCH /config — llm -------------------------------------------------

func TestConfigComponent_C1_FirstConnect(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)

	resp := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey))
	if resp.Code != 200 {
		t.Fatalf("connect: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	m := decodeCfg(t, resp.Body.Bytes())
	llm := m["llm"].(map[string]any)
	if llm["keyPrefix"] != goodAnthKey[:15] || llm["keyLast4"] != goodAnthKey[len(goodAnthKey)-4:] {
		t.Fatalf("post-write projection preview drifted: %v", llm)
	}
	// A following GET is identical.
	if g := c.h.AsOrg("acme").Get(configPath); g.Body.String() != resp.Body.String() {
		t.Fatalf("PATCH body must equal a following GET:\nPATCH %s\nGET   %s", resp.Body.String(), g.Body.String())
	}
}

func TestConfigComponent_C2_ReplaceKey(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("first connect: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey2))
	if resp.Code != 200 {
		t.Fatalf("replace: %d %s", resp.Code, resp.Body.String())
	}
	llm := decodeCfg(t, resp.Body.Bytes())["llm"].(map[string]any)
	if llm["keyPrefix"] != goodAnthKey2[:15] || llm["keyLast4"] != goodAnthKey2[len(goodAnthKey2)-4:] {
		t.Fatalf("replace did not swap the key preview: %v", llm)
	}
}

func TestConfigComponent_C3_ProbeFailsOldKeyStaysActive(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("first connect: %d %s", r.Code, r.Body.String())
	}
	before := c.h.AsOrg("acme").Get(configPath).Body.String()

	// Flip the probe to reject, then try to replace.
	c.anth.setStatus(http.StatusUnauthorized)
	resp := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey2))
	if resp.Code != 400 {
		t.Fatalf("probe fail: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeEnvelope(t, resp.Body.String()); len(p.Details) == 0 || p.Details[0].Field != "body.llm" {
		t.Fatalf("400 must point at body.llm: %s", resp.Body.String())
	}
	// The old key is untouched.
	if after := c.h.AsOrg("acme").Get(configPath).Body.String(); after != before {
		t.Fatalf("probe failure must not touch the old key:\nbefore %s\nafter  %s", before, after)
	}
}

func TestConfigComponent_C4_NullClearsWhileConnected(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":null}`)
	if resp.Code != 200 {
		t.Fatalf("clear: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeCfg(t, resp.Body.Bytes()); m["llm"] != nil {
		t.Fatalf("llm must be null after clear: %v", m["llm"])
	}
	// The credential row is purged.
	var count int64
	c.db.Model(&organization.OrgAnthropicCredential{}).Where("oc_org_id = ?", "acme").Count(&count)
	if count != 0 {
		t.Fatalf("llm:null must purge the row, found %d", count)
	}
}

func TestConfigComponent_C5_NullWhileDisconnectedIsNoOp(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":null}`)
	if resp.Code != 200 {
		t.Fatalf("idempotent clear: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if m := decodeCfg(t, resp.Body.Bytes()); m["llm"] != nil {
		t.Fatalf("llm must stay null: %v", m["llm"])
	}
}

func TestConfigComponent_C6_SchemaRejections(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	cases := []struct{ name, body string }{
		{"missing apiKey", `{"llm":{"kind":"anthropic"}}`},
		{"unknown kind", `{"llm":{"kind":"openai","apiKey":"x"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resp := c.h.AsOrg("acme").Patch(configPath, tc.body)
			if resp.Code != 400 {
				t.Fatalf("%s: want 400 schema rejection, got %d body=%s", tc.name, resp.Code, resp.Body.String())
			}
		})
	}
}

// --- D. PATCH /config — gitProvider -----------------------------------------

func TestConfigComponent_D1_PatConnect(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.gh.patHappy()
	resp := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":{"kind":"github","mode":"pat","pat":"ghp_live","githubLogin":"ada"}}`)
	if resp.Code != 200 {
		t.Fatalf("connect: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	gp := decodeCfg(t, resp.Body.Bytes())["gitProvider"].(map[string]any)
	if gp["mode"] != "pat" || gp["githubLogin"] != "ada" {
		t.Fatalf("pat connect projection drifted: %v", gp)
	}
}

func TestConfigComponent_D2_PatProbeFails(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.gh.on("GET", "/user", 401, `{"message":"Bad credentials"}`)

	resp := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":{"kind":"github","mode":"pat","pat":"bad","githubLogin":"ada"}}`)
	if resp.Code != 400 {
		t.Fatalf("probe fail: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeEnvelope(t, resp.Body.String()); len(p.Details) == 0 || p.Details[0].Field != "body.gitProvider" {
		t.Fatalf("400 must point at body.gitProvider: %s", resp.Body.String())
	}
	// Nothing persisted.
	var count int64
	c.db.Model(&organization.OrgCredential{}).Where("oc_org_id = ?", "acme").Count(&count)
	if count != 0 {
		t.Fatalf("failed probe must persist nothing, found %d", count)
	}
}

func TestConfigComponent_D3_AppModeSchemaRejected(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	// mode enum is pat-only → app is a schema 400 before the handler.
	resp := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":{"kind":"github","mode":"app","pat":"x","githubLogin":"ada"}}`)
	if resp.Code != 400 {
		t.Fatalf("mode app: want 400 schema rejection, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestConfigComponent_D4_NullPointsAtDisconnect(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	resp := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":null}`)
	if resp.Code != 400 {
		t.Fatalf("gitProvider null: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	p := componenttest.DecodeEnvelope(t, resp.Body.String())
	if len(p.Details) == 0 || p.Details[0].Field != "body.gitProvider" || !strings.Contains(p.Message, "disconnect") {
		t.Fatalf("400 must point at the disconnect action: %s", resp.Body.String())
	}
}

func TestConfigComponent_D5_PatOverAppIsConflict(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	c.gh.patHappy()
	// Existing ACTIVE app-installation row → the reused CredentialService.Connect
	// refuses a cross-mode PAT write with a 409 (disconnect before switching).
	id := int64(4242)
	if err := c.db.Create(&organization.OrgCredential{
		OcOrgID: "acme", Kind: "app-installation", GitHubLogin: "acme-org",
		IdentityName: "AEP Bot", IdentityEmail: "bot@aep.dev", IdentityLogin: "aep[bot]",
		InstallationID: &id, Status: "active", ConnectedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed app row: %v", err)
	}
	resp := c.h.AsOrg("acme").Patch(configPath, `{"gitProvider":{"kind":"github","mode":"pat","pat":"ghp","githubLogin":"ada"}}`)
	if resp.Code != 409 {
		t.Fatalf("pat-over-app: want 409, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeEnvelope(t, resp.Body.String()); len(p.Details) == 0 || p.Details[0].Field != "body.gitProvider" {
		t.Fatalf("409 must point at body.gitProvider: %s", resp.Body.String())
	}
}

// --- E. PATCH /config — idp -------------------------------------------------

func TestConfigComponent_E1_SwitchToCustom(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	resp := c.h.AsOrg("acme").Patch(configPath, `{"idp":{"kind":"custom","issuer":"https://byo.example","jwksUrl":"https://byo.example/jwks"}}`)
	if resp.Code != 200 {
		t.Fatalf("switch custom: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	idpSec := decodeCfg(t, resp.Body.Bytes())["idp"].(map[string]any)
	if idpSec["kind"] != "custom" || idpSec["issuer"] != "https://byo.example" {
		t.Fatalf("custom switch drifted: %v", idpSec)
	}
	// Persisted.
	g := decodeCfg(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes())["idp"].(map[string]any)
	if g["kind"] != "custom" || g["jwksUrl"] != "https://byo.example/jwks" {
		t.Fatalf("custom switch did not persist: %v", g)
	}
}

func TestConfigComponent_E2_ResetToPlatform(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	// Start custom, then reset.
	if r := c.h.AsOrg("acme").Patch(configPath, `{"idp":{"kind":"custom","issuer":"https://byo.example","jwksUrl":"https://byo.example/jwks"}}`); r.Code != 200 {
		t.Fatalf("to custom: %d %s", r.Code, r.Body.String())
	}
	resp := c.h.AsOrg("acme").Patch(configPath, `{"idp":{"kind":"platform"}}`)
	if resp.Code != 200 {
		t.Fatalf("reset: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	idpSec := decodeCfg(t, resp.Body.Bytes())["idp"].(map[string]any)
	if idpSec["kind"] != "platform" || idpSec["issuer"] != platformIss || idpSec["jwksUrl"] != platformJWKS {
		t.Fatalf("platform reset must restore cluster defaults: %v", idpSec)
	}
}

func TestConfigComponent_E3_NullRejected(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	resp := c.h.AsOrg("acme").Patch(configPath, `{"idp":null}`)
	if resp.Code != 400 {
		t.Fatalf("idp null: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeEnvelope(t, resp.Body.String()); len(p.Details) == 0 || p.Details[0].Field != "body.idp" {
		t.Fatalf("400 must point at body.idp: %s", resp.Body.String())
	}
}

func TestConfigComponent_E4_WholesaleReplaceClearsOmitted(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	// Establish a custom idp WITH a jwksUrl.
	if r := c.h.AsOrg("acme").Patch(configPath, `{"idp":{"kind":"custom","issuer":"https://byo.example","jwksUrl":"https://byo.example/jwks"}}`); r.Code != 200 {
		t.Fatalf("seed: %d %s", r.Code, r.Body.String())
	}
	// Re-send the section WITHOUT jwksUrl → wholesale replace clears it (no
	// legacy empty-means-keep carry-over).
	resp := c.h.AsOrg("acme").Patch(configPath, `{"idp":{"kind":"custom","issuer":"https://byo2.example"}}`)
	if resp.Code != 200 {
		t.Fatalf("replace: %d %s", resp.Code, resp.Body.String())
	}
	idpSec := decodeCfg(t, resp.Body.Bytes())["idp"].(map[string]any)
	if idpSec["issuer"] != "https://byo2.example" {
		t.Fatalf("issuer not replaced: %v", idpSec)
	}
	if idpSec["jwksUrl"] != "" {
		t.Fatalf("omitted jwksUrl must be CLEARED (wholesale replace), got %v", idpSec["jwksUrl"])
	}
}

// --- F. Merge semantics & atomicity -----------------------------------------

func TestConfigComponent_F1_PatchOnlyLLMLeavesOthers(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	// Establish a custom idp first.
	if r := c.h.AsOrg("acme").Patch(configPath, `{"idp":{"kind":"custom","issuer":"https://byo.example","jwksUrl":"https://byo.example/jwks"}}`); r.Code != 200 {
		t.Fatalf("seed idp: %d %s", r.Code, r.Body.String())
	}
	beforeIDP := sectionOf(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes(), "idp")

	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("llm patch: %d %s", r.Code, r.Body.String())
	}
	afterIDP := sectionOf(t, c.h.AsOrg("acme").Get(configPath).Body.Bytes(), "idp")
	if beforeIDP != afterIDP {
		t.Fatalf("patching llm must leave idp untouched:\nbefore %s\nafter  %s", beforeIDP, afterIDP)
	}
}

func TestConfigComponent_F2_MultiSectionSuccess(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},"idp":{"kind":"custom","issuer":"https://byo.example","jwksUrl":"https://byo.example/jwks"}}`)
	if resp.Code != 200 {
		t.Fatalf("multi-section: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	m := decodeCfg(t, resp.Body.Bytes())
	if m["llm"].(map[string]any)["status"] != "active" {
		t.Fatalf("llm not applied: %v", m["llm"])
	}
	if m["idp"].(map[string]any)["kind"] != "custom" {
		t.Fatalf("idp not applied: %v", m["idp"])
	}
}

func TestConfigComponent_F3_AtomicityLLMNotPersistedWhenGitFails(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	// gitProvider probe will fail (GET /user 401); llm probe passes.
	c.gh.on("GET", "/user", 401, `{"message":"Bad credentials"}`)

	resp := c.h.AsOrg("acme").Patch(configPath, `{"llm":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"},"gitProvider":{"kind":"github","mode":"pat","pat":"bad","githubLogin":"ada"}}`)
	if resp.Code != 400 {
		t.Fatalf("atomic fail: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
	if p := componenttest.DecodeEnvelope(t, resp.Body.String()); len(p.Details) == 0 || p.Details[0].Field != "body.gitProvider" {
		t.Fatalf("400 must point at body.gitProvider: %s", resp.Body.String())
	}
	// The valid llm section must NOT have been persisted (probe-before-persist).
	var llmCount, gitCount int64
	c.db.Model(&organization.OrgAnthropicCredential{}).Where("oc_org_id = ?", "acme").Count(&llmCount)
	c.db.Model(&organization.OrgCredential{}).Where("oc_org_id = ?", "acme").Count(&gitCount)
	if llmCount != 0 || gitCount != 0 {
		t.Fatalf("atomicity violated: llm rows=%d git rows=%d (both must be 0)", llmCount, gitCount)
	}
}

func TestConfigComponent_F4_EmptyPatchIsNoOp(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	before := c.h.AsOrg("acme").Get(configPath).Body.String()
	resp := c.h.AsOrg("acme").Patch(configPath, `{}`)
	if resp.Code != 200 {
		t.Fatalf("empty patch: want 200, got %d body=%s", resp.Code, resp.Body.String())
	}
	if resp.Body.String() != before {
		t.Fatalf("empty patch must return current projection unchanged:\nbefore %s\nafter  %s", before, resp.Body.String())
	}
}

func TestConfigComponent_F5_UnknownTopLevelKeyRejected(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	// The `llms` plural typo is an unknown field → the contract validator rejects it (400).
	resp := c.h.AsOrg("acme").Patch(configPath, `{"llms":{"kind":"anthropic","apiKey":"`+goodAnthKey+`"}}`)
	if resp.Code != 400 {
		t.Fatalf("unknown key: want 400, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestConfigComponent_F6_PatchEqualsFollowingGet(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	patchResp := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey))
	getResp := c.h.AsOrg("acme").Get(configPath)
	if patchResp.Body.String() != getResp.Body.String() {
		t.Fatalf("PATCH response must equal a following GET:\nPATCH %s\nGET   %s", patchResp.Body.String(), getResp.Body.String())
	}
}

func TestConfigComponent_F7_CommutativeAcrossSections(t *testing.T) {
	t.Parallel()
	// Order A: llm then idp. Order B: idp then llm. Same final state.
	final := func(llmFirst bool) string {
		c := newConfigHarness(t)
		llm := llmConnect(goodAnthKey)
		idpP := `{"idp":{"kind":"custom","issuer":"https://byo.example","jwksUrl":"https://byo.example/jwks"}}`
		if llmFirst {
			c.h.AsOrg("acme").Patch(configPath, llm)
			c.h.AsOrg("acme").Patch(configPath, idpP)
		} else {
			c.h.AsOrg("acme").Patch(configPath, idpP)
			c.h.AsOrg("acme").Patch(configPath, llm)
		}
		// Drop volatile timestamps for the comparison.
		return normalizeTimes(c.h.AsOrg("acme").Get(configPath).Body.String())
	}
	if a, b := final(true), final(false); a != b {
		t.Fatalf("section patches must commute:\nA %s\nB %s", a, b)
	}
}

// --- G. Auth & tenancy ------------------------------------------------------

func TestConfigComponent_G1_NoAuth401(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	if r := c.h.NoAuth().Get(configPath); r.Code != 401 {
		t.Fatalf("no-auth GET: want 401, got %d body=%s", r.Code, r.Body.String())
	}
	if r := c.h.NoAuth().Patch(configPath, `{}`); r.Code != 401 {
		t.Fatalf("no-auth PATCH: want 401, got %d body=%s", r.Code, r.Body.String())
	}
}

func TestConfigComponent_G2_TenantIsolation(t *testing.T) {
	t.Parallel()
	c := newConfigHarness(t)
	// Seed org B's llm row directly.
	if err := c.db.Create(&organization.OrgAnthropicCredential{
		OcOrgID: "orgb", KeyPrefix: "sk-ant-api03-BB", KeyLast4: "0000",
		Status: "active", ConnectedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed org B: %v", err)
	}
	// Org A sees only its own (null) llm — never B's.
	m := decodeCfg(t, c.h.AsOrg("orga").Get(configPath).Body.Bytes())
	if m["llm"] != nil {
		t.Fatalf("org A must not see org B's llm: %v", m["llm"])
	}
}

// G3 is intentionally NOT parallel: it swaps the process-global slog default to
// capture the audit line, so it must run in the sequential phase (before the
// parallel tests resume) to avoid crosstalk with their log output.
func TestConfigComponent_G3_AuditLogsSectionsNotSecrets(t *testing.T) {
	c := newConfigHarness(t)

	var buf bytes.Buffer
	var mu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, nil)))
	defer slog.SetDefault(prev)

	if r := c.h.AsOrg("acme").Patch(configPath, llmConnect(goodAnthKey)); r.Code != 200 {
		t.Fatalf("connect: %d %s", r.Code, r.Body.String())
	}

	mu.Lock()
	logs := buf.String()
	mu.Unlock()
	if !strings.Contains(logs, "orgconfig.patched") || !strings.Contains(logs, "llm") {
		t.Fatalf("patch must log the sections it carried: %s", logs)
	}
	if strings.Contains(logs, goodAnthKey) {
		t.Fatalf("audit log must NEVER carry the secret value: %s", logs)
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// --- H. Migration & spec regression -----------------------------------------

func TestConfigComponent_H2_SpecShape(t *testing.T) {
	t.Parallel()
	// Assert the /config surface stays typed in the committed contract.
	s := string(contracttest.SourceYAML(t))
	// paths and schemas live in one document.
	for _, want := range []string{"get-config", "update-config", "ConfigProjection", "ConfigPatch"} {
		if !strings.Contains(s, want) {
			t.Errorf("contract missing %q", want)
		}
	}
	// The /config ops must be typed — no untyped map bodies (additionalProperties: {}).
	if strings.Contains(configOpsBlock(s), "additionalProperties: {}") {
		t.Errorf("a /config op still serves an untyped additionalProperties:{} body")
	}
}

func TestConfigComponent_H1b_SkillsRenamed(t *testing.T) {
	t.Parallel()
	s := string(contracttest.SourceYAML(t))
	if !strings.Contains(s, "/skills") {
		t.Errorf("spec missing renamed /skills routes")
	}
	if strings.Contains(s, "/org/skills") {
		t.Errorf("spec still carries the old /org/skills routes")
	}
}

// --- test helpers -----------------------------------------------------------

func seedCustomIDP(t *testing.T, db *gorm.DB, org string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.Create(&organization.OrganizationIDPProfile{
		OrgID:                 org,
		Kind:                  "custom",
		Issuer:                "https://byo.example",
		JWKSURL:               "https://byo.example/jwks",
		PublisherClientID:     "pub-client",
		PublisherClientSecret: "the-stored-secret",
		CreatedAt:             now,
		UpdatedAt:             now,
	}).Error; err != nil {
		t.Fatalf("seed custom idp: %v", err)
	}
}

func sectionOf(t *testing.T, body []byte, key string) string {
	t.Helper()
	m := decodeCfg(t, body)
	raw, err := json.Marshal(m[key])
	if err != nil {
		t.Fatalf("marshal section %q: %v", key, err)
	}
	return string(raw)
}

// normalizeTimes blanks the connectedAt/lastValidatedAt values so two runs that
// necessarily differ only in timestamps compare equal (commutativity check).
func normalizeTimes(body string) string {
	m := map[string]any{}
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return body
	}
	var strip func(v any)
	strip = func(v any) {
		obj, ok := v.(map[string]any)
		if !ok {
			return
		}
		for k, val := range obj {
			switch k {
			case "connectedAt", "lastValidatedAt", "identityChangedAt":
				obj[k] = ""
			default:
				strip(val)
			}
		}
	}
	strip(m)
	out, _ := json.Marshal(m)
	return string(out)
}

// configOpsBlock returns just the lines belonging to path blocks whose key
// starts with "/config", so the additionalProperties check can't false-positive
// on a legacy map-bodied op under a different path. Path headers are 2-space
// indented under `paths:` (e.g. "  /config:", "  /config/idp/discovery:").
func configOpsBlock(spec string) string {
	var out strings.Builder
	inConfig := false
	for _, line := range strings.Split(spec, "\n") {
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(strings.TrimRight(line, " "), ":") {
			inConfig = strings.HasPrefix(line, "  /config")
		}
		if inConfig {
			out.WriteString(line)
			out.WriteByte('\n')
		}
	}
	return out.String()
}
