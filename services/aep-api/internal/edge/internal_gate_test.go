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
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/routers"

	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// The INT-6 cycle fence must check the cycle id the handler is served, not the
// still-escaped form kin's router extracts: kin matches on the escaped path
// while the ServeMux hands handlers the decoded PathValue.
func TestInternalGate_FencesDecodedCycleID(t *testing.T) {
	runnerOps := []struct{ name, method, prefix, suffix string }{
		{"validation context", http.MethodGet, "/internal/v1/validation/", "/context"},
		{"credentials refresh", http.MethodPost, "/internal/v1/executions/", "/credentials/refresh"},
	}

	// (a) An escape that decodes to another org's cycle is fenced as that cycle.
	for _, op := range runnerOps {
		t.Run(op.name+": encoded other-org cycle → 403", func(t *testing.T) {
			s := newInternalStack(t)
			req := httptest.NewRequest(op.method, op.prefix+"%6Fther-org-x"+op.suffix, strings.NewReader(""))
			req.Header.Set("Authorization", "Bearer "+s.mint("org-acme"))
			rec := httptest.NewRecorder()
			s.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("want 403, got %d body=%s (fenced %q)", rec.Code, rec.Body.String(), *s.fenced)
			}
			if s.context.gotCycle != "" || s.refresh.gotExecution != "" {
				t.Fatalf("service reached: cycle=%q execution=%q", s.context.gotCycle, s.refresh.gotExecution)
			}
		})
	}

	// (b) For an encoded-but-valid id, the id the gate fenced is the id served.
	for _, op := range runnerOps {
		for _, tc := range []struct{ encoded, decoded string }{
			{"c%2D1", "c-1"},
			{"c1%2F..%2Fother-org-x", "c1/../other-org-x"},
		} {
			t.Run(op.name+": fenced == served for "+tc.encoded, func(t *testing.T) {
				s := newInternalStack(t)
				req := httptest.NewRequest(op.method, op.prefix+tc.encoded+op.suffix, strings.NewReader(""))
				req.Header.Set("Authorization", "Bearer "+s.mint("org-acme"))
				rec := httptest.NewRecorder()
				s.handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("want 200, got %d body=%s", rec.Code, rec.Body.String())
				}
				served := s.context.gotCycle + s.refresh.gotExecution // exactly one op ran
				if !slices.Equal(*s.fenced, []string{tc.decoded}) || served != tc.decoded {
					t.Fatalf("gate fenced %q, service served %q; want both %q", *s.fenced, served, tc.decoded)
				}
			})
		}
	}
}

// (c) An invalid escape fails closed. net/http rejects such a request URI
// with 400 before any handler runs, so a real request never reaches the gate;
// the gate's own unescape check is the backstop, answering 401 without a
// cycle lookup.
func TestInternalGate_InvalidEscapeFailsClosed(t *testing.T) {
	t.Run("net/http rejects the request line", func(t *testing.T) {
		s := newInternalStack(t)
		reached := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reached = true
			s.handler.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		if _, err := conn.Write([]byte("GET /internal/v1/validation/%zz/context HTTP/1.1\r\nHost: x\r\nAuthorization: Bearer " +
			s.mint("org-acme") + "\r\nConnection: close\r\n\r\n")); err != nil {
			t.Fatal(err)
		}
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || reached {
			t.Fatalf("status %d, handler reached %v; want 400 and not reached", resp.StatusCode, reached)
		}
	})

	for _, tc := range []struct{ op, param string }{
		{"runner-validation-context", "cycleId"},
		{"runner-refresh-credentials", "executionId"},
	} {
		t.Run("gate backstop: "+tc.op, func(t *testing.T) {
			s := newInternalStack(t)
			m := internalRouteMatch{
				route:      &routers.Route{Operation: &openapi3.Operation{OperationID: tc.op}},
				pathParams: map[string]string{tc.param: "%zz"},
			}
			_, err := authenticateInternal(context.Background(), s.deps, "Bearer "+s.mint("org-acme"), m)
			if err == nil {
				t.Fatal("want an error, got none")
			}
			rec := httptest.NewRecorder()
			writeResponseError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err)
			if rec.Code != http.StatusUnauthorized || len(*s.fenced) != 0 {
				t.Fatalf("status %d, fenced %q; want 401 and no lookup", rec.Code, *s.fenced)
			}
		})
	}
}

// HEAD on a GET operation authenticates exactly as the GET it is served as
// (the ServeMux serves HEAD through a GET pattern), the same as /api/v1, where
// the JWT middleware authenticates every method.
func TestInternalGate_HEADAuthenticatesLikeGET(t *testing.T) {
	s := newInternalStack(t)
	deps := s.deps
	deps.SREHandoff, deps.Issues = auth.NewSREHandoffVerifier("s3cr3t", "acme"), &fakeIssues{}
	h := NewHandler(AppParams{InternalDeps: deps})
	for _, tc := range []struct {
		name, path, bearer string
		want               int
	}{
		{"runner op, valid bearer", "/internal/v1/validation/c1/context", "Bearer " + s.mint("org-acme"), 200},
		{"runner op, no bearer", "/internal/v1/validation/c1/context", "", 401},
		{"runner op, other org's bearer", "/internal/v1/validation/c1/context", "Bearer " + s.mint("org-other"), 403},
		{"runner op, encoded other-org cycle", "/internal/v1/validation/%6Fther-org-x/context", "Bearer " + s.mint("org-acme"), 403},
		{"sre op, sre bearer", "/internal/v1/sre/projects/p/issues", "Bearer s3cr3t", 200},
		{"sre op, no bearer", "/internal/v1/sre/projects/p/issues", "", 401},
		{"sre op, publisher token", "/internal/v1/sre/projects/p/issues", "Bearer " + s.mint("acme"), 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodHead, tc.path, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", tc.bearer)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("HEAD %s: status %d, want %d (body %s)", tc.path, rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// Every operation of the embedded internal spec has a gate entry, and every
// gate entry names a spec operation (and, for a runner op, one of its path
// parameters). A typo'd id or a new spec op without a gate entry fails here
// instead of silently answering 401 at runtime. call-mcp-tool is excluded
// from the embedded spec, so it is never walked.
func TestInternalGate_CoversEverySpecOperation(t *testing.T) {
	doc, err := igen.GetSpec()
	if err != nil {
		t.Fatalf("load embedded internal spec: %v", err)
	}
	seen := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			seen[op.OperationID] = true
			g, ok := internalOpGates[op.OperationID]
			if !ok {
				t.Errorf("%s %s: operation %q has no internalOpGates entry", method, path, op.OperationID)
				continue
			}
			if g.credential == runnerCredential && !hasPathParam(item, op, g.cycleParam) {
				t.Errorf("%s: cycle param %q is not a path parameter of %s %s", op.OperationID, g.cycleParam, method, path)
			}
		}
	}
	for id := range internalOpGates {
		if !seen[id] {
			t.Errorf("internalOpGates entry %q names no operation of the embedded spec", id)
		}
	}
}

func hasPathParam(item *openapi3.PathItem, op *openapi3.Operation, name string) bool {
	for _, ps := range []openapi3.Parameters{item.Parameters, op.Parameters} {
		if p := ps.GetByInAndName(openapi3.ParameterInPath, name); p != nil {
			return true
		}
	}
	return false
}
