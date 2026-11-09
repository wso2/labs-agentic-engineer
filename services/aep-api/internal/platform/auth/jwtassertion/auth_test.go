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

package jwtassertion

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestBuildBearerChallenge(t *testing.T) {
	tests := []struct {
		name                string
		resourceMetadataURL string
		errorCode           string
		want                string
	}{
		{
			name: "realm only",
			want: `Bearer realm="aep"`,
		},
		{
			name:      "with error code",
			errorCode: "invalid_token",
			want:      `Bearer realm="aep", error="invalid_token"`,
		},
		{
			name:                "with resource metadata URL",
			resourceMetadataURL: "https://aep.example.com/.well-known/oauth-protected-resource",
			want:                `Bearer realm="aep", resource_metadata="https://aep.example.com/.well-known/oauth-protected-resource"`,
		},
		{
			name:                "with error and resource metadata URL",
			resourceMetadataURL: "https://aep.example.com/.well-known/oauth-protected-resource",
			errorCode:           "invalid_token",
			want:                `Bearer realm="aep", error="invalid_token", resource_metadata="https://aep.example.com/.well-known/oauth-protected-resource"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildBearerChallenge(tt.resourceMetadataURL, tt.errorCode)
			if got != tt.want {
				t.Errorf("buildBearerChallenge() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAuthenticator_MissingHeader(t *testing.T) {
	metadataURL := "https://aep.example.com/.well-known/oauth-protected-resource"
	mw := Authenticator(Config{
		AllowedIssuers:      []string{"thunder"},
		AllowedAudiences:    []string{"aep-bff"},
		ResourceMetadataURL: metadataURL,
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when Authorization header is missing")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	want := `Bearer realm="aep", resource_metadata="https://aep.example.com/.well-known/oauth-protected-resource"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestAuthenticator_InvalidJWT(t *testing.T) {
	metadataURL := "https://aep.example.com/.well-known/oauth-protected-resource"
	mw := Authenticator(Config{
		AllowedIssuers:      []string{"thunder"},
		AllowedAudiences:    []string{"aep-bff"},
		ResourceMetadataURL: metadataURL,
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for invalid JWT")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	want := `Bearer realm="aep", error="invalid_token", resource_metadata="https://aep.example.com/.well-known/oauth-protected-resource"`
	if got := rec.Header().Get("WWW-Authenticate"); got != want {
		t.Errorf("WWW-Authenticate = %q, want %q", got, want)
	}
}

func TestValidateAudience(t *testing.T) {
	tests := []struct {
		name         string
		audiences    []string
		allowed      []string
		shouldAccept bool
	}{
		{
			name:         "exact match",
			audiences:    []string{"aep-bff"},
			allowed:      []string{"aep-bff"},
			shouldAccept: true,
		},
		{
			name:         "no match",
			audiences:    []string{"someone-else"},
			allowed:      []string{"aep-bff"},
			shouldAccept: false,
		},
		{
			name:         "prefix match",
			audiences:    []string{"aep-bff-v2"},
			allowed:      []string{"aep-bff*"},
			shouldAccept: true,
		},
		{
			name:         "multiple audiences first matches",
			audiences:    []string{"aep-bff", "other"},
			allowed:      []string{"aep-bff"},
			shouldAccept: true,
		},
		{
			name:         "rejects bare wildcard",
			audiences:    []string{"anything"},
			allowed:      []string{"*"},
			shouldAccept: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAudience(tt.audiences, tt.allowed)
			if tt.shouldAccept && err != nil {
				t.Errorf("expected accept, got error: %v", err)
			}
			if !tt.shouldAccept && err == nil {
				t.Error("expected rejection, got nil error")
			}
		})
	}
}

func TestValidateIssuer(t *testing.T) {
	if err := validateIssuer("thunder", []string{"thunder"}); err != nil {
		t.Errorf("expected accept, got %v", err)
	}
	if err := validateIssuer("thunder", []string{"other"}); err == nil {
		t.Error("expected rejection")
	}
	if err := validateIssuer("thunder", nil); err == nil {
		t.Error("expected error when no allowed issuers configured")
	}
}

// TokenClaims declares its own `Sub string json:"sub"` at depth 0 AND embeds
// jwt.RegisteredClaims whose Subject carries the same tag one level deeper.
// encoding/json decodes ONLY the shallower field, so consumers must read
// tc.Sub — the promoted tc.Subject is always empty. auth.JWTMiddleware
// projecting tc.Subject instead of tc.Sub lost the user identity on every
// authenticated request (turn commits fell back to the credential identity;
// found in e2e 2026-07-06). This pins the decode shape.
func TestWireClaims_SubShadowsEmbeddedSubject(t *testing.T) {
	var w WireClaims
	if err := json.Unmarshal([]byte(`{"sub":"user-123","ouHandle":"acme","grant_type":"authorization_code"}`), &w); err != nil {
		t.Fatal(err)
	}
	if w.Sub != "user-123" {
		t.Fatalf("w.Sub = %q, want %q", w.Sub, "user-123")
	}
	if w.Subject != "" {
		t.Fatalf("w.Subject = %q — the shadowing assumption changed; re-audit every Sub consumer", w.Subject)
	}
	// The embedded dialect.Raw decodes beside the registered claims: a
	// duplicate tag at that depth would drop both fields silently.
	if w.OuHandle != "acme" || w.GrantType != "authorization_code" {
		t.Fatalf("dialect claims not decoded: %+v", w.Raw)
	}
}

// validateIssuer and validateAudience are thin wrappers for tests that
// exercise the matcher directly with a raw allowed-list.
func validateIssuer(issuer string, allowed []string) error {
	return compileIssuers(allowed).match(issuer)
}

func validateAudience(audiences jwt.ClaimStrings, allowed []string) error {
	return compileAudiences(allowed).match(audiences)
}
