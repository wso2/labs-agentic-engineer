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

package githubtest

// Stub is a route-registry httptest fake for the plain JSON GitHub endpoints —
// repo create/adopt, issues, webhook registration — the github client is
// driven against in tests (repo content runs on the git engine, never here).
// Moved from aep-api's platform/gittest.
//
// Register a fixed status+body per "METHOD /path" with On(); an unregistered
// path 404s (so a caller's own not-found handling is exercised). Every request
// — registered or not — is recorded for assertion via Requests(). Routing is on
// r.URL.Path; query strings are ignored for matching but captured in the record.
//
// Three registration forms, narrowest first:
//
//   - On(method, path, status, body) — one fixed reply, forever.
//   - OnSequence(method, path, responses...) — a scripted reply per call, for
//     flows where one route must answer differently before and after a side
//     effect (a create-then-recover retry hits the same list endpoint twice).
//   - OnFunc(method, path, handler) — an arbitrary handler, for replies that
//     depend on the request itself (paginated pages keyed on ?page=, or the
//     single POST /graphql route serving several distinct operations).
//
// A later registration for a route replaces the earlier one whichever form is
// used.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// RecordedRequest is a captured inbound request. Body holds the raw request
// body bytes (already read); Header is a clone taken at receive time.
type RecordedRequest struct {
	Method string
	Path   string
	Query  string
	Body   string
	Header http.Header
}

// Stub wraps an httptest.Server. Embed exposes .URL and .Close.
type Stub struct {
	*httptest.Server
	mu       sync.Mutex
	routes   map[string]http.HandlerFunc
	requests []RecordedRequest
}

// NewStub starts a route-registry server and registers Close on t.Cleanup.
func NewStub(t *testing.T) *Stub {
	t.Helper()
	s := &Stub{routes: map[string]http.HandlerFunc{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, RecordedRequest{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   string(body),
			Header: r.Header.Clone(),
		})
		h := s.routes[r.Method+" "+r.URL.Path]
		s.mu.Unlock()
		if h == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		h(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// On registers a fixed status+body for an exact method+path. A later On for the
// same route replaces the earlier one.
func (s *Stub) On(method, path string, status int, body string) {
	s.OnFunc(method, path, writeJSON(status, body))
}

// OnFunc registers an arbitrary handler for an exact method+path, replacing any
// earlier registration for that route. Use it when the reply depends on the
// request — pagination keyed on ?page=, or the single POST /graphql route whose
// operations are distinguishable only by body.
func (s *Stub) OnFunc(method, path string, handler http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[method+" "+path] = handler
}

// Response is one scripted reply in an OnSequence script.
type Response struct {
	Status int
	Body   string
}

// OnSequence scripts successive replies for an exact method+path: the Nth call
// gets responses[N-1], and every call past the end repeats the last response.
// This is what On cannot express — a route whose answer changes because an
// intervening call had a side effect (list-empty → create-conflicts →
// list-now-populated recovery). Panics when the script is empty, which is
// always a test bug.
func (s *Stub) OnSequence(method, path string, responses ...Response) {
	if len(responses) == 0 {
		panic("githubtest: OnSequence needs at least one response")
	}
	var mu sync.Mutex
	calls := 0
	s.OnFunc(method, path, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := calls
		if i >= len(responses) {
			i = len(responses) - 1
		}
		calls++
		mu.Unlock()
		writeJSON(responses[i].Status, responses[i].Body)(w, r)
	})
}

// writeJSON is the fixed-reply handler shared by On and OnSequence.
func writeJSON(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

// Requests returns a copy of every request received so far, in arrival order.
func (s *Stub) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RecordedRequest, len(s.requests))
	copy(out, s.requests)
	return out
}
