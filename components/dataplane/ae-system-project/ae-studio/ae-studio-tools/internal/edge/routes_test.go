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

package edge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/ae-studio-tools/internal/auth"
	"github.com/wso2/aep/ae-studio-tools/internal/config"
	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/projects/projectstest"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
	"github.com/wso2/aep/ae-studio-tools/internal/turns"
	"github.com/wso2/aep/ae-studio-tools/internal/turns/turnstest"
	"github.com/wso2/aep/ae-studio-tools/internal/webhook"
)

const (
	testIssuer   = "http://idp"
	testClientID = "ae-studio-internal-client"
	// testWebhookSecret is a fixture, not a real secret.
	testWebhookSecret = "test-webhook-secret"
)

// fakeGitHub is the github.Identity the harness serves: e2e-bot/42, or err.
// calls counts the requests that reached the handler.
type fakeGitHub struct {
	err   error
	calls *int
}

func (f fakeGitHub) Whoami(context.Context) (string, int64, error) {
	*f.calls++
	if f.err != nil {
		return "", 0, f.err
	}
	return "e2e-bot", 42, nil
}

type harness struct {
	t           *testing.T
	key         *rsa.PrivateKey
	handler     http.Handler
	logBuf      *syncBuffer
	githubCalls int
	// projects is aep-api as the Files reader sees it; empty unless
	// withProjects seeds it.
	projects *projectstest.Fake
	// engine is the studio-data engine behind the Files reader and the
	// reference store.
	engine *repo.Engine
	// server serves handler over real HTTP for the streaming tests
	// (postStream), started on first use.
	server *httptest.Server
}

// harnessDeps is what the options adjust before Routes is built.
type harnessDeps struct {
	gh       fakeGitHub
	projects map[string]projects.Repository
	// turnSocket is the Turn socket's path; a missing socket unless
	// withTurnSocket names one.
	turnSocket string
}

type harnessOpt func(*harnessDeps)

func withGitHubErr(err error) harnessOpt { return func(d *harnessDeps) { d.gh.err = err } }

// withProjects is aep-api's project → repository answers.
func withProjects(repos map[string]projects.Repository) harnessOpt {
	return func(d *harnessDeps) { d.projects = repos }
}

// withTurnSocket points the turns relay at a fake Turn socket.
func withTurnSocket(s *turnstest.Server) harnessOpt {
	return func(d *harnessDeps) { d.turnSocket = s.Path() }
}

func withGitHubStatus(status int) harnessOpt {
	return withGitHubErr(&github.HTTPStatusError{StatusCode: status})
}

// newHarness serves the real Routes over a fake GitHub and a test JWKS, and
// captures the default slog logger as JSON lines for the test's duration.
func newHarness(t *testing.T, opts ...harnessOpt) *harness {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	jwks := auth.JWKS{Keys: []auth.JSONWebKey{{
		Kty: "RSA", Kid: "k1", Use: "sig", Alg: "RS256",
		N: base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
		E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
	}}}
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jwks)
	}))
	t.Cleanup(idp.Close)

	h := &harness{t: t, key: key}
	deps := harnessDeps{gh: fakeGitHub{calls: &h.githubCalls}, turnSocket: filepath.Join(t.TempDir(), "absent.sock")}
	for _, o := range opts {
		o(&deps)
	}
	h.projects = projectstest.NewFake(deps.projects)
	engine, _, err := repo.New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		OrgID: "ou-1", OrgHandle: "default",
		IDPIssuer: testIssuer, IDPJWKSURL: idp.URL,
		UserAudiences: []string{"aep-console-client"},
		M2MClientID:   testClientID,
		GitHubOwner:   "Acme-GH",
	}
	h.logBuf = captureLogs(t)
	h.handler = Routes(Deps{
		Cfg:        cfg,
		Verifier:   auth.NewVerifier(cfg.IDPIssuer, auth.NewJWKSCache(cfg.IDPJWKSURL)),
		GitHub:     deps.gh,
		Webhook:    WebhookHandler(testWebhookSecret, webhook.Unwired()),
		Files:      files.Reader{Engine: engine, Projects: h.projects},
		References: engine,
		Projects:   h.projects,
		Turns:      turns.Relay{Turns: turns.NewClient(deps.turnSocket)},
	})
	h.engine = engine
	return h
}

func (h *harness) sign(claims jwt.MapClaims) string {
	h.t.Helper()
	claims["iss"] = testIssuer
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(h.key)
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (h *harness) user(handle, ouID string) string {
	return h.sign(jwt.MapClaims{"aud": "aep-console-client", "sub": "u1", "ouId": ouID, "ouHandle": handle})
}

func (h *harness) m2m() string {
	return h.sign(jwt.MapClaims{"aud": testClientID, "client_id": testClientID, "grant_type": "client_credentials", "sub": "app-1"})
}

func (h *harness) do(method, path, token, org string, body io.Reader) *httptest.ResponseRecorder {
	h.t.Helper()
	r := httptest.NewRequest(method, path, body)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if org != "" {
		r.Header.Set("X-Impersonate-Org", org)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, r)
	return rec
}

func (h *harness) logs() string { return h.logBuf.String() }

// logLine returns the last log line whose msg is msg.
func (h *harness) logLine(msg string) map[string]any {
	h.t.Helper()
	var found map[string]any
	for _, l := range strings.Split(strings.TrimSpace(h.logs()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(l), &m) == nil && m["msg"] == msg {
			found = m
		}
	}
	if found == nil {
		h.t.Fatalf("no %q log line in %s", msg, h.logs())
	}
	return found
}

func TestRoutes_UnknownV1PathGatedFirst(t *testing.T) {
	h := newHarness(t) // real Routes over a fake GitHub and a test JWKS
	cases := []struct {
		method, path, token, org string
		want                     int
	}{
		{"GET", "/v1/projects/p/files", "", "", 401},
		{"GET", "/v1/projects/p/files", h.m2m(), "", 401},
		{"GET", "/v1/projects/p/files", h.user("e2e-other", "ou-2"), "", 403},
		{"GET", "/v1/projects/p/files", h.user("default", "ou-1"), "", 404}, // project_unknown
		{"GET", "/internal/v1/github/identity", "", "ou-1", 401},
		{"GET", "/internal/v1/github/identity", h.user("default", "ou-1"), "ou-1", 401},
		{"GET", "/internal/v1/github/identity", h.m2m(), "", 403},
		{"GET", "/internal/v1/github/identity", h.m2m(), "ou-2", 403},
		{"GET", "/internal/v1/github/identity", h.m2m(), "ou-1", 200},
		{"GET", "/internal/v1/nope", "", "", 401},
		{"GET", "/internal/v1/nope", h.m2m(), "ou-1", 404},
		{"POST", "/internal/v1/github/identity", h.m2m(), "ou-1", 404},
		{"GET", "/admin", "", "", 404},
		{"GET", "/webhooks/github", "", "", 404},
		{"POST", "/webhooks/github", "", "", 401}, // HMAC gate, no signature
		{"POST", "/webhooks/github/", "", "", 404},
		// A method the contract does not declare is 404, even HEAD on a GET op.
		{"HEAD", "/internal/v1/github/identity", h.m2m(), "ou-1", 404},
		// Group roots without the trailing slash: gated, then 404 (no redirect).
		{"GET", "/v1", "", "", 401},
		{"GET", "/v1", h.user("default", "ou-1"), "", 404},
		{"GET", "/internal/v1", "", "", 401},
		{"GET", "/internal/v1", h.m2m(), "ou-1", 404},
		// Dot segments are never cleaned and redirected: gated by the group
		// the raw path names, then 404.
		{"GET", "/internal/v1/../v1/x", "", "", 401},
		{"GET", "/internal/v1/./github/identity", h.m2m(), "ou-1", 404},
		{"GET", "/internal/v1//github/identity", h.m2m(), "ou-1", 404},
		{"GET", "/v1/../internal/v1/github/identity", "", "", 401},
		{"GET", "/v1/./x", h.user("default", "ou-1"), "", 404},
		{"GET", "/x/../internal/v1/github/identity", h.m2m(), "ou-1", 404},
		{"POST", "/webhooks/./github", "", "", 404},
		{"GET", "/healthz", "", "", 404}, // health is on the health port only
		{"GET", "/readyz", "", "", 404},
	}
	for _, c := range cases {
		rec := h.do(c.method, c.path, c.token, c.org, nil)
		if rec.Code != c.want {
			t.Errorf("%s %s = %d, want %d (%s)", c.method, c.path, rec.Code, c.want, rec.Body.String())
		}
		if c.want >= 400 && rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s %s content-type %q", c.method, c.path, rec.Header().Get("Content-Type"))
		}
	}
	if h.githubCalls != 1 {
		t.Fatalf("the identity handler ran %d times, want 1 (only the one 200 row)", h.githubCalls)
	}
	// /v1/projects/p/files is a real route now: the 404 is aep-api not knowing p.
	if rec := h.do("GET", "/v1/projects/p/files", h.user("default", "ou-1"), "", nil); !strings.Contains(rec.Body.String(), `"code":"project_unknown"`) {
		t.Fatalf("unknown project body = %s", rec.Body.String())
	}
	var body struct {
		Login string
		ID    int64
	}
	_ = json.Unmarshal(h.do("GET", "/internal/v1/github/identity", h.m2m(), "ou-1", nil).Body.Bytes(), &body)
	if body.Login != "e2e-bot" || body.ID != 42 {
		t.Fatalf("identity = %+v", body)
	}
}

func TestRoutes_InternalAccessLog(t *testing.T) {
	h := newHarness(t)
	tok := h.m2m()
	h.do("GET", "/internal/v1/github/identity?x=secret-query", tok, "ou-1", nil)
	line := h.logLine("internal.access")
	if line["path"] != "/internal/v1/github/identity" || line["status"] != float64(200) || line["method"] != "GET" {
		t.Fatalf("access log = %v", line)
	}
	if _, ok := line["ms"].(float64); !ok {
		t.Fatalf("access log ms = %v", line["ms"])
	}
	if strings.Contains(h.logs(), "Bearer") || strings.Contains(h.logs(), tok) || strings.Contains(h.logs(), "secret-query") {
		t.Fatal("token or query in logs")
	}

	h.do("GET", "/internal/v1/github/identity", "", "", nil)
	if line := h.logLine("internal.access"); line["status"] != float64(401) {
		t.Fatalf("a gate refusal is logged too: %v", line)
	}
}

func TestRoutes_GitHubErrorIsProblem(t *testing.T) {
	h := newHarness(t, withGitHubStatus(401))
	rec := h.do("GET", "/internal/v1/github/identity", h.m2m(), "ou-1", nil)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), `"code":"github_error"`) ||
		!strings.Contains(rec.Body.String(), "401") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("content-type %q", rec.Header().Get("Content-Type"))
	}
}

func TestRoutes_GitHubRateLimitIs429(t *testing.T) {
	h := newHarness(t, withGitHubErr(&github.HTTPStatusError{StatusCode: http.StatusTooManyRequests, RetryAfter: 30 * time.Second}))
	rec := h.do("GET", "/internal/v1/github/identity", h.m2m(), "ou-1", nil)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "30" ||
		!strings.Contains(rec.Body.String(), `"code":"github_rate_limited"`) {
		t.Fatalf("got %d Retry-After=%q %s", rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
}

func TestRoutes_InternalBodyCapRunsBeforeGate(t *testing.T) {
	h := newHarness(t)
	big := bytes.NewReader(make([]byte, internalBodyBytes+1))
	rec := h.do("GET", "/internal/v1/github/identity", "", "", big)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"code":"payload_too_large"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}
