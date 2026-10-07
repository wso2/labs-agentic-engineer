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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// sre-handoff/mcp is its own route group: only the install-time handoff key
// opens it, and no other internal credential does. Unconfigured, it is not
// mounted at all.
func TestInternalGate_SREHandoffMCP(t *testing.T) {
	stack := newInternalStack(t)
	unmounted := func() http.Handler {
		deps := stack.deps
		deps.SREHandoffAuth, deps.SREHandoffMCP = nil, nil
		return NewHandler(AppParams{InternalDeps: deps})
	}()
	userJWT := "Bearer " + stack.sign(auth.PublisherClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    pubIssuer,
			Audience:  jwt.ClaimStrings{"aep-console"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		OuHandle: "acme",
	})
	const path = "/internal/v1/sre-handoff/mcp"
	cases := []struct {
		name, bearer string
		h            http.Handler
		want         int
	}{
		{name: "handoff key", bearer: "Bearer " + sreHandoffKey, want: 200},
		{name: "no bearer", want: 401},
		{name: "another key", bearer: "Bearer " + strings.Repeat("x", len(sreHandoffKey)), want: 401},
		{name: "the key without the Bearer scheme", bearer: sreHandoffKey, want: 401},
		{name: "publisher token", bearer: "Bearer " + stack.mint("acme"), want: 401},
		{name: "ae-studio client token", bearer: "Bearer " + stack.mintStudio("acme"), want: 401},
		{name: "user JWT", bearer: userJWT, want: 401},
		{name: "unconfigured: not mounted", h: unmounted, bearer: "Bearer " + sreHandoffKey, want: 404},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.h
			if h == nil {
				h = stack.handler
			}
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			req.Header.Set("Content-Type", "application/json")
			if tc.bearer != "" {
				req.Header.Set("Authorization", tc.bearer)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
