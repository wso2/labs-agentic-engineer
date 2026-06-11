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

package openchoreo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/asdlc/asdlc-service/middleware"
)

type fakeAuthProvider struct{ tok string }

func (f fakeAuthProvider) Token() (string, error) { return f.tok, nil }
func (f fakeAuthProvider) Invalidate()            {}

func TestNamespaceFromPath(t *testing.T) {
	cases := map[string]string{
		"/api/v1/namespaces/wc-abc/components":   "wc-abc",
		"/api/v1/namespaces/wc-abc/components/x": "wc-abc",
		"/api/v1/namespaces/wc-abc":              "wc-abc",
		"/api/v1/namespaces":                     "",
		"/api/v1/namespaces/":                    "",
		"/healthz":                               "",
		"":                                       "",
	}
	for path, want := range cases {
		if got := namespaceFromPath(path); got != want {
			t.Errorf("namespaceFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}

// captureServer records the impersonation + auth headers of the last request.
func captureServer(t *testing.T, impersonate, auth *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*impersonate = r.Header.Get("X-Impersonate-Org")
		*auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
}

func TestImpersonateOrgHeader_M2MPath_SetsHeader(t *testing.T) {
	var impersonate, auth string
	srv := captureServer(t, &impersonate, &auth)
	defer srv.Close()

	c, err := newGenClient(Config{
		BaseURL:      srv.URL,
		AuthProvider: fakeAuthProvider{tok: "m2m-token"},
		ImpersonateOrgResolver: func(_ context.Context, ns string) (string, error) {
			if ns == "wc-abc" {
				return "org-uuid-123", nil
			}
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// No user JWT in context -> M2M path -> impersonation header set from the
	// resolved namespace, M2M token attached.
	if _, err := c.GetNamespaceWithResponse(context.Background(), "wc-abc"); err != nil {
		t.Fatal(err)
	}
	if impersonate != "org-uuid-123" {
		t.Errorf("X-Impersonate-Org = %q, want org-uuid-123", impersonate)
	}
	if auth != "Bearer m2m-token" {
		t.Errorf("Authorization = %q, want Bearer m2m-token", auth)
	}
}

func TestImpersonateOrgHeader_UserJWTPath_NoHeader(t *testing.T) {
	var impersonate, auth string
	srv := captureServer(t, &impersonate, &auth)
	defer srv.Close()

	resolverCalled := false
	c, err := newGenClient(Config{
		BaseURL:      srv.URL,
		AuthProvider: fakeAuthProvider{tok: "m2m-token"},
		ImpersonateOrgResolver: func(_ context.Context, _ string) (string, error) {
			resolverCalled = true
			return "org-uuid-123", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// User JWT in context -> forwarded; impersonation header is NOT set and the
	// resolver is not consulted (platform-api routes by the JWT's ouId).
	ctx := middleware.WithAuthToken(context.Background(), "user-jwt")
	if _, err := c.GetNamespaceWithResponse(ctx, "wc-abc"); err != nil {
		t.Fatal(err)
	}
	if impersonate != "" {
		t.Errorf("user-JWT path must not set X-Impersonate-Org, got %q", impersonate)
	}
	if auth != "Bearer user-jwt" {
		t.Errorf("Authorization = %q, want Bearer user-jwt", auth)
	}
	if resolverCalled {
		t.Error("resolver must not be called on the user-JWT path")
	}
}

func TestImpersonateOrgHeader_ServiceIdentity_OverridesUserJWT(t *testing.T) {
	var impersonate, auth string
	srv := captureServer(t, &impersonate, &auth)
	defer srv.Close()

	c, err := newGenClient(Config{
		BaseURL:      srv.URL,
		AuthProvider: fakeAuthProvider{tok: "m2m-token"},
		ImpersonateOrgResolver: func(_ context.Context, ns string) (string, error) {
			if ns == "wc-abc" {
				return "org-uuid-123", nil
			}
			return "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A user JWT is present in ctx (the call runs inside the user's HTTP
	// request), but the call is explicitly marked service-identity — an
	// orchestration write (dispatch/cascade). The user JWT must NOT be
	// forwarded; instead the M2M token is attached and the impersonation
	// header is set. This is the dispatch-path regression: without the
	// override the user JWT (or an injected service token) suppressed the
	// header and the write mis-routed to the wrong org.
	ctx := middleware.WithServiceIdentity(middleware.WithAuthToken(context.Background(), "user-jwt"))
	if _, err := c.GetNamespaceWithResponse(ctx, "wc-abc"); err != nil {
		t.Fatal(err)
	}
	if impersonate != "org-uuid-123" {
		t.Errorf("service-identity call must set X-Impersonate-Org, got %q", impersonate)
	}
	if auth != "Bearer m2m-token" {
		t.Errorf("service-identity call must use the M2M token, got %q", auth)
	}
}

func TestImpersonateOrgHeader_ResolverError_AbortsRequest(t *testing.T) {
	var impersonate, auth string
	srv := captureServer(t, &impersonate, &auth)
	defer srv.Close()

	c, err := newGenClient(Config{
		BaseURL:      srv.URL,
		AuthProvider: fakeAuthProvider{tok: "m2m-token"},
		ImpersonateOrgResolver: func(_ context.Context, _ string) (string, error) {
			return "", context.DeadlineExceeded
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// A resolver error must abort the call (never silently mis-route to the
	// service identity's own org).
	if _, err := c.GetNamespaceWithResponse(context.Background(), "wc-abc"); err == nil {
		t.Fatal("expected error when resolver fails, got nil")
	}
}
