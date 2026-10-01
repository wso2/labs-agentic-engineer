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
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// Every internal op names its caller in its tags (03 §4). Read from the YAML,
// not igen.GetSpec, because call-mcp-tool is excluded from generation.
func TestInternalSpec_TagsNameTheCaller(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../../../packages/contracts/api/internal/v1/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	callers := map[string]bool{"Runner": true, "AE Studio": true, "SRE": true}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if len(op.Tags) == 0 {
				t.Errorf("%s %s has no tag", method, path)
			}
			for _, tag := range op.Tags {
				if !callers[tag] {
					t.Errorf("%s %s tag %q is not a caller", method, path, tag)
				}
			}
		}
	}
	if doc.Paths.Find("/mcp") == nil || doc.Paths.Find("/mcp").Post == nil {
		t.Error("call-mcp-tool is not declared")
	}
}

// Today's runner request shapes still pass the validator (Review Focus 1):
// credhelper.ts POSTs `{}` as JSON to refresh (an op with no declared body),
// validation_context.ts GETs the context with no body.
func TestInternalValidator_AcceptsRunnerRequests(t *testing.T) {
	s := newInternalStack(t)
	for _, tc := range []struct{ method, path, contentType, body string }{
		{http.MethodPost, "/internal/v1/executions/cyc-1/credentials/refresh", "application/json", "{}"},
		{http.MethodGet, "/internal/v1/validation/cyc-1/context", "", ""},
	} {
		var body io.Reader
		if tc.body != "" {
			body = strings.NewReader(tc.body)
		}
		req := httptest.NewRequest(tc.method, tc.path, body)
		if tc.contentType != "" {
			req.Header.Set("Content-Type", tc.contentType)
		}
		req.Header.Set("Authorization", "Bearer "+s.mint("org-acme"))
		w := httptest.NewRecorder()
		s.handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s %s = %d, want 200 (body %s)", tc.method, tc.path, w.Code, w.Body)
		}
	}
}

// A declared body over 1 MiB is answered 413 by capInternalBody itself: the
// handler behind it never runs. /mcp is a route miss for the embedded spec
// (call-mcp-tool is excluded from generation), so this is the default cap.
func TestInternalBodyCap_Default(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
	h := capInternalBody(internalRouter(), map[string]int64{}, next)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", strings.NewReader(strings.Repeat("x", 1<<20+1))))
	if w.Code != http.StatusRequestEntityTooLarge || called {
		t.Fatalf("status %d, handler called %v; want 413 and not called", w.Code, called)
	}
	if !strings.Contains(w.Body.String(), `"request_too_large"`) {
		t.Errorf("body %s, want the request_too_large envelope", w.Body)
	}
}

// A body of unknown length (chunked) is bounded while it is read: the handler
// sees at most 1 MiB and then a *http.MaxBytesError.
func TestInternalBodyCap_UnknownLength(t *testing.T) {
	var seen int
	var readErr error
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		_, readErr = buf.ReadFrom(r.Body)
		seen = buf.Len()
	})
	h := capInternalBody(internalRouter(), map[string]int64{}, next)
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/mcp", strings.NewReader(strings.Repeat("x", 1<<20+1)))
	req.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), req)
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) || seen > 1<<20 {
		t.Fatalf("handler read %d bytes, err %v; want ≤ 1 MiB and a MaxBytesError", seen, readErr)
	}
}

// A per-op cap overrides the default (phase 4 uses this for webhook-events).
func TestInternalBodyCap_PerOp(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := new(bytes.Buffer).ReadFrom(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		}
	})
	caps := map[string]int64{"runner-refresh-credentials": 2 << 20}
	h := capInternalBody(internalRouter(), caps, next)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/internal/v1/executions/c/credentials/refresh", strings.NewReader(strings.Repeat("x", 1<<20+1))))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200 under a 2 MiB per-op cap", w.Code)
	}
}
