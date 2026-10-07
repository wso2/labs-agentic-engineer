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

package organization_test

// A fake model endpoint for the DB and component tiers: one TLS server that
// answers for EVERY host (the connection's URL may name api.anthropic.com or
// ollama.com), so the real prober runs end to end without leaving the process.
// It lists models, answers Ollama's /api/show, and records what it was sent.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// modelEndpointRequest is one request the fake saw.
type modelEndpointRequest struct {
	Host, Method, Path, APIKey, Authorization string
}

type modelEndpoint struct {
	srv *httptest.Server

	mu       sync.Mutex
	status   int      // what /models answers; 200 lists models
	models   []string // the listing
	requests []modelEndpointRequest
}

// newModelEndpoint starts the fake answering status on /models.
func newModelEndpoint(t testing.TB, status int) *modelEndpoint {
	t.Helper()
	e := &modelEndpoint{status: status, models: []string{"claude-sonnet-5-5", "claude-sonnet-5", "claude-haiku-4-5", "gpt-oss:20b", "glm-5.3"}}
	e.srv = httptest.NewTLSServer(http.HandlerFunc(e.serve))
	t.Cleanup(e.srv.Close)
	return e
}

func (e *modelEndpoint) serve(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	e.requests = append(e.requests, modelEndpointRequest{
		Host: r.Host, Method: r.Method, Path: r.URL.Path,
		APIKey: r.Header.Get("x-api-key"), Authorization: r.Header.Get("Authorization"),
	})
	status, models := e.status, append([]string(nil), e.models...)
	e.mu.Unlock()

	w.Header().Set("content-type", "application/json")
	switch {
	case r.URL.Path == "/api/show":
		_, _ = w.Write([]byte(`{"capabilities":["completion","tools"],"model_info":{"gptoss.context_length":131072}}`))
	case strings.HasSuffix(r.URL.Path, "/models"):
		if status != http.StatusOK {
			if status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "60") // a provider's rate limit, not a WAF refusal
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"type":"error"}}`))
			return
		}
		data := make([]map[string]string, 0, len(models))
		for _, m := range models {
			data = append(data, map[string]string{"id": m})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// setStatus changes what /models answers from now on.
func (e *modelEndpoint) setStatus(status int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status = status
}

// seen returns the requests so far.
func (e *modelEndpoint) seen() []modelEndpointRequest {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]modelEndpointRequest(nil), e.requests...)
}

// client is an http.Client that reaches the fake whatever host a URL names,
// verifying the fake's certificate against the name it was issued for.
func (e *modelEndpoint) client() *http.Client {
	roots := e.srv.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	addr := e.srv.Listener.Addr().String()
	return &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
		TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.com", MinVersion: tls.VersionTLS12},
	}}
}
