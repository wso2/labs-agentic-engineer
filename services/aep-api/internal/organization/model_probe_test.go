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

package organization

// UNIT tier — the probers against httptest endpoints: every answer an endpoint
// can give maps to one outcome, the key never follows a redirect, and the
// host enrichment reads Ollama's /api/show. No network leaves the process.

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/platform/modelconn"
	"github.com/wso2/aep/aep-api/internal/platform/netguard"
)

const probeKey = "probe-test-key-0123456789abcdef"

// probeRecorder is an endpoint scripted per path, recording what it was sent.
type probeRecorder struct {
	mu       sync.Mutex
	requests []*http.Request
	handle   func(w http.ResponseWriter, r *http.Request)
}

func (p *probeRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.requests = append(p.requests, r.Clone(context.Background()))
	p.mu.Unlock()
	p.handle(w, r)
}

func (p *probeRecorder) seen() []*http.Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]*http.Request(nil), p.requests...)
}

// probeServer starts a TLS endpoint running handle.
func probeServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *probeRecorder) {
	t.Helper()
	rec := &probeRecorder{handle: handle}
	srv := httptest.NewTLSServer(rec)
	t.Cleanup(srv.Close)
	return srv, rec
}

// routedClient reaches srv whatever host a URL names, so a target can say
// api.anthropic.com or ollama.com and still land on the test server.
func routedClient(srv *httptest.Server) *http.Client {
	roots := srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	addr := srv.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12},
	}}
}

// target is a probe of srv at host (routed there by routedClient).
func target(format modelconn.Format, host, path, model string) probeTarget {
	return probeTarget{Org: "acme", Format: format, BaseURL: "https://" + host + path, Host: host, Model: model, Key: probeKey}
}

func listing(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, `{"id":"`+id+`"}`)
	}
	return `{"data":[` + strings.Join(parts, ",") + `]}`
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	var ve *ValidationError
	var ue *UpstreamError
	switch {
	case errors.As(err, &ve) && ve.Code == code:
	case errors.As(err, &ue) && ue.Code == code:
	default:
		t.Fatalf("err = %v, want code %s", err, code)
	}
	if strings.Contains(err.Error(), probeKey) {
		t.Fatalf("the refusal echoes the key: %v", err)
	}
}

func TestProbe_ListingSaysWhetherTheModelIsServed(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  modelconn.Tristate
	}{{"claude-sonnet-5", modelconn.Yes}, {"claude-opus-9", modelconn.No}} {
		srv, rec := probeServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(listing("claude-sonnet-5", "claude-haiku-4-5")))
		})
		res, err := newModelProbers(routedClient(srv)).probe(context.Background(),
			target(modelconn.FormatAnthropic, modelconn.AnthropicHost, "/v1", tc.model))
		if err != nil || res.ModelListed != tc.want || res.AuthScheme != modelconn.AuthXAPIKey {
			t.Fatalf("%s: res=%+v err=%v, want listed=%s on x-api-key", tc.model, res, err, tc.want)
		}
		// Anthropic's own API: the runtimes know Claude's limits, and it reads images.
		if res.ContextWindow != nil || res.OutputLimit != nil || res.ImageInput != modelconn.Yes {
			t.Fatalf("first-party enrichment: %+v", res)
		}
		r := rec.seen()[0]
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != probeKey ||
			r.Header.Get("anthropic-version") == "" {
			t.Fatalf("listing request: %s %s headers=%v", r.Method, r.URL.Path, r.Header)
		}
	}
}

// Ollama's Anthropic endpoint answers 401 to x-api-key and 200 to Bearer
// (measured): the probe retries once and reports the scheme that worked.
func TestProbe_AnthropicFormatRetriesWithBearerAfter401(t *testing.T) {
	srv, rec := probeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+probeKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(listing("gpt-oss:20b")))
	})
	res, err := newModelProbers(routedClient(srv)).probe(context.Background(),
		target(modelconn.FormatAnthropic, "gateway.example.com", "/v1", "gpt-oss:20b"))
	if err != nil || res.AuthScheme != modelconn.AuthBearer || res.ModelListed != modelconn.Yes {
		t.Fatalf("res=%+v err=%v, want Bearer and listed", res, err)
	}
	if n := len(rec.seen()); n != 3 {
		t.Fatalf("requests = %d, want the x-api-key try, one Bearer retry and the public-listing check", n)
	}
	// Another host keeps the defaults, image input unknown.
	if res.ContextWindow == nil || *res.ContextWindow != defaultContextWindow || res.OutputLimit == nil ||
		*res.OutputLimit != defaultOutputLimit || res.ImageInput != modelconn.Unknown {
		t.Fatalf("defaults: %+v", res)
	}
}

func TestProbe_KeyRejected(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv, _ := probeServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })
		_, err := newModelProbers(routedClient(srv)).probe(context.Background(),
			target(modelconn.FormatOpenAICompatible, "gateway.example.com", "/v1", "m"))
		wantCode(t, err, "llm_key_rejected")
	}
}

// An endpoint with no listing gets one max_tokens=1 request instead: 200 or
// 400 proves the key, and whether the model is served stays unknown.
func TestProbe_NoListingFallsBackToOneRequest(t *testing.T) {
	for _, tc := range []struct {
		format   modelconn.Format
		fallback string
		answer   int
	}{
		{modelconn.FormatAnthropic, "/v1/messages", http.StatusOK},
		{modelconn.FormatAnthropic, "/v1/messages", http.StatusBadRequest},
		{modelconn.FormatOpenAICompatible, "/v1/chat/completions", http.StatusOK},
	} {
		srv, rec := probeServer(t, func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/models") {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(tc.answer)
		})
		res, err := newModelProbers(routedClient(srv)).probe(context.Background(),
			target(tc.format, "gateway.example.com", "/v1", "some-model"))
		if err != nil || res.ModelListed != modelconn.Unknown {
			t.Fatalf("%s %d: res=%+v err=%v, want unknown", tc.fallback, tc.answer, res, err)
		}
		seen := rec.seen()
		last := seen[len(seen)-1]
		if last.Method != http.MethodPost || last.URL.Path != tc.fallback {
			t.Fatalf("fallback request = %s %s, want POST %s", last.Method, last.URL.Path, tc.fallback)
		}
	}
}

// A redirect is never followed, even by a client that would follow one: Go
// forwards x-api-key across a redirect, so following it would hand the key to
// another host. The target never sees a request.
func TestProbe_ARedirectIsUnreachableAndTheTargetNeverSeesTheKey(t *testing.T) {
	elsewhere, elsewhereRec := probeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(listing("m")))
	})
	origin, _ := probeServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusFound)
	})
	following := origin.Client() // follows redirects, and trusts both test servers
	_, err := newModelProbers(following).probe(context.Background(), probeTarget{
		Org: "acme", Format: modelconn.FormatAnthropic, BaseURL: origin.URL + "/v1", Host: "127.0.0.1", Model: "m", Key: probeKey,
	})
	wantCode(t, err, "llm_unreachable")
	if n := len(elsewhereRec.seen()); n != 0 {
		t.Fatalf("the redirect target saw %d request(s); the key must never follow a redirect", n)
	}
}

func TestProbe_429(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		body    string
		limited bool
	}{
		{name: "retry-after proves the key", headers: map[string]string{"Retry-After": "600"}, limited: true},
		{name: "a rate-limit header proves the key", headers: map[string]string{"X-RateLimit-Reset-Requests": "1m"}, limited: true},
		{name: "a rate-limit error body proves the key", body: `{"type":"error","error":{"type":"rate_limit_error"}}`, limited: true},
		{name: "a bare 429 is a WAF refusal", body: `blocked`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := probeServer(t, func(w http.ResponseWriter, _ *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(tc.body))
			})
			res, err := newModelProbers(routedClient(srv)).probe(context.Background(),
				target(modelconn.FormatOpenAICompatible, "gateway.example.com", "/v1", "m"))
			if !tc.limited {
				wantCode(t, err, "llm_unexpected_status")
				return
			}
			if err != nil || !res.ProviderLimited || res.ModelListed != modelconn.Unknown {
				t.Fatalf("res=%+v err=%v, want the key proved with a provider-limit warning", res, err)
			}
		})
	}
}

// Every 429 writes one model_provider_429 line, with the key's literal removed
// from the body it quotes. Not parallel: it swaps the process-global logger.
func TestProbe_429IsLoggedWithoutTheKey(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	srv, _ := probeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"slow down, ` + probeKey + `"}`))
	})
	if _, err := newModelProbers(routedClient(srv)).probe(context.Background(),
		target(modelconn.FormatOpenAICompatible, "gateway.example.com", "/v1", "m")); err != nil {
		t.Fatalf("probe: %v", err)
	}
	logs := buf.String()
	if strings.Count(logs, `"msg":"model_provider_429"`) != 1 || !strings.Contains(logs, `"host":"gateway.example.com"`) ||
		!strings.Contains(logs, `"org":"acme"`) || !strings.Contains(logs, `"retry-after":"30"`) {
		t.Fatalf("want one model_provider_429 line naming org, host and the limit header: %s", logs)
	}
	if strings.Contains(logs, probeKey) {
		t.Fatalf("the 429 line carries the key literal: %s", logs)
	}
}

// A key straddling where a quoted body is cut leaves no prefix behind: the
// key is removed before the body is cut, in the refusal (200 bytes) and in
// the 429 log line (300). Not parallel: it swaps the process-global logger.
func TestProbe_AKeyAcrossTheCutIsScrubbedWhole(t *testing.T) {
	straddling := func(cut int) []byte {
		return []byte(strings.Repeat("x", cut-len(probeKey)/2) + probeKey + " trailing")
	}
	prefix := probeKey[:len(probeKey)/2]

	_, err := statusVerdict(target(modelconn.FormatOpenAICompatible, "gateway.example.com", "/v1", "m"),
		modelconn.AuthBearer, probeResponse{status: http.StatusTeapot, body: straddling(200)})
	if err == nil || strings.Contains(err.Error(), prefix) {
		t.Fatalf("the refusal quotes part of the key: %v", err)
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	logProvider429(context.Background(), target(modelconn.FormatOpenAICompatible, "gateway.example.com", "/v1", "m"),
		probeResponse{status: http.StatusTooManyRequests, body: straddling(300)})
	if !strings.Contains(buf.String(), `"msg":"model_provider_429"`) || strings.Contains(buf.String(), prefix) {
		t.Fatalf("the 429 line quotes part of the key: %s", buf.String())
	}
}

func TestProbe_OtherStatuses(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{http.StatusInternalServerError, "llm_upstream_error"},
		{http.StatusBadGateway, "llm_upstream_error"},
		{http.StatusTeapot, "llm_unexpected_status"},
		{http.StatusBadRequest, "llm_unexpected_status"},
	} {
		srv, _ := probeServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })
		_, err := newModelProbers(routedClient(srv)).probe(context.Background(),
			target(modelconn.FormatOpenAICompatible, "gateway.example.com", "/v1", "m"))
		wantCode(t, err, tc.code)
	}
	// A 5xx is the endpoint's fault: an UpstreamError, which the edge maps to 502.
	srv, _ := probeServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	_, err := newModelProbers(routedClient(srv)).probe(context.Background(),
		target(modelconn.FormatOpenAICompatible, "gateway.example.com", "/v1", "m"))
	var ue *UpstreamError
	if !errors.As(err, &ue) {
		t.Fatalf("a 5xx must be an UpstreamError, got %T", err)
	}
}

func TestProbe_NetworkFailureIsUnreachable(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	client := routedClient(srv)
	srv.Close() // dead endpoint → connection refused
	_, err := newModelProbers(client).probe(context.Background(),
		target(modelconn.FormatOpenAICompatible, "gateway.example.com", "/v1", "m"))
	wantCode(t, err, "llm_unreachable")
}

// The production client refuses a host that resolves to a non-public
// address, and the refusal names the host as typed, never an address.
func TestProbe_APrivateHostIsRefused(t *testing.T) {
	srv, rec := probeServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(listing("m"))) })
	_, err := newModelProbers(netguard.NewClient(modelProbeTimeout, netguard.NoRedirects())).probe(context.Background(), probeTarget{
		Org: "acme", Format: modelconn.FormatOpenAICompatible, BaseURL: srv.URL + "/v1", Host: "127.0.0.1", Model: "m", Key: probeKey,
	})
	wantCode(t, err, "llm_host_refused")
	if n := len(rec.seen()); n != 0 {
		t.Fatalf("a refused host still received %d request(s)", n)
	}
}

// On ollama.com the probe asks /api/show for the model's context window and
// whether it reads images, with the connection's own key on its own host.
func TestProbe_OllamaEnrichment(t *testing.T) {
	for _, tc := range []struct {
		caps      string
		wantImage modelconn.Tristate
	}{{`["completion","vision"]`, modelconn.Yes}, {`["completion","tools"]`, modelconn.No}} {
		srv, rec := probeServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/show" {
				_, _ = w.Write([]byte(`{"capabilities":` + tc.caps + `,"model_info":{"glm.context_length":1048576}}`))
				return
			}
			_, _ = w.Write([]byte(listing("glm-5.3")))
		})
		res, err := newModelProbers(routedClient(srv)).probe(context.Background(),
			target(modelconn.FormatOpenAICompatible, modelconn.OllamaHost, "/v1", "glm-5.3"))
		if err != nil || res.ContextWindow == nil || *res.ContextWindow != 1048576 || res.ImageInput != tc.wantImage ||
			res.OutputLimit == nil || *res.OutputLimit != defaultOutputLimit {
			t.Fatalf("caps %s: res=%+v err=%v", tc.caps, res, err)
		}
		var show *http.Request
		for _, r := range rec.seen() {
			if r.URL.Path == "/api/show" {
				show = r
			}
		}
		if show == nil {
			t.Fatal("no /api/show request")
		}
		if show.Method != http.MethodPost || show.URL.Path != "/api/show" || show.Host != modelconn.OllamaHost ||
			show.Header.Get("Authorization") != "Bearer "+probeKey {
			t.Fatalf("show request: %s %s host=%s", show.Method, show.URL.Path, show.Host)
		}
	}
}

// A host that will not say keeps the defaults; enrichment never fails a save.
func TestProbe_OllamaEnrichmentFailureKeepsTheDefaults(t *testing.T) {
	srv, _ := probeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(listing("glm-5.3")))
	})
	res, err := newModelProbers(routedClient(srv)).probe(context.Background(),
		target(modelconn.FormatOpenAICompatible, modelconn.OllamaHost, "/v1", "glm-5.3"))
	if err != nil || res.ContextWindow == nil || *res.ContextWindow != defaultContextWindow || res.ImageInput != modelconn.Unknown {
		t.Fatalf("res=%+v err=%v, want the defaults", res, err)
	}
}

// An Anthropic listing that pages and did not name the model cannot say "no".
func TestProbe_APagedListingIsUnknown(t *testing.T) {
	if got := modelListed([]byte(`{"data":[{"id":"a"}],"has_more":true}`), "b"); got != modelconn.Unknown {
		t.Fatalf("paged listing = %s, want unknown", got)
	}
	if got := modelListed([]byte(`not json`), "b"); got != modelconn.Unknown {
		t.Fatalf("unreadable listing = %s, want unknown", got)
	}
}

// Ollama's listing answers anyone (measured: 200 with no key or a bad one), so
// a listing that also answers an unauthenticated request proves nothing: the
// key is proved with one request, and that request's scheme is the one kept.
func TestProbe_APublicListingDoesNotProveTheKey(t *testing.T) {
	for _, tc := range []struct {
		name       string
		send       int // what the one request answers to the right credential
		wantErr    string
		wantListed modelconn.Tristate
		model      string
	}{
		{name: "a good key", send: http.StatusOK, wantListed: modelconn.Yes, model: "gpt-oss:20b"},
		{name: "a good key and an unserved model", send: http.StatusNotFound, wantListed: modelconn.No, model: "not-served"},
		{name: "a bad key", send: http.StatusUnauthorized, wantErr: "llm_key_rejected", model: "gpt-oss:20b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, rec := probeServer(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/models") {
					_, _ = w.Write([]byte(listing("gpt-oss:20b"))) // for anyone
					return
				}
				if r.Header.Get("Authorization") != "Bearer "+probeKey || tc.send == http.StatusUnauthorized {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(tc.send)
			})
			res, err := newModelProbers(routedClient(srv)).probe(context.Background(),
				target(modelconn.FormatAnthropic, "gateway.example.com", "/v1", tc.model))
			if tc.wantErr != "" {
				wantCode(t, err, tc.wantErr)
				return
			}
			if err != nil || res.AuthScheme != modelconn.AuthBearer || res.ModelListed != tc.wantListed {
				t.Fatalf("res=%+v err=%v, want Bearer (from the proving request) and listed=%s", res, err, tc.wantListed)
			}
			var unauthenticated bool
			for _, r := range rec.seen() {
				unauthenticated = unauthenticated || (r.URL.Path == "/v1/models" && r.Header.Get("Authorization") == "" && r.Header.Get("x-api-key") == "")
			}
			if !unauthenticated {
				t.Fatal("the probe never checked whether the listing is public")
			}
		})
	}
}
