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

package thunderapp_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/thunderapp"
)

const (
	testResource = "proj-idp"
	testEnv      = "default"
)

func mustClient(t *testing.T, cfg thunderapp.Config) *thunderapp.Client {
	t.Helper()
	c, err := thunderapp.New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func crJSON(redirect string, ready bool, gen, observed int64) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"generation": gen},
		"spec":     map[string]any{"redirectUris": redirect},
		"status":   map[string]any{"ready": ready, "observedGeneration": observed},
	}
}

func listJSON(items ...map[string]any) map[string]any {
	if items == nil {
		items = []map[string]any{}
	}
	return map[string]any{"kind": "ThunderApplicationList", "items": items}
}

// TestFindByResource_200DecodesSpecStatusGeneration pins the cluster-wide LIST
// the deploy-wait uses: OpenChoreo labels, not a namespaced GET of the rendered
// r-<resource>-<env>-<hash8> object name.
func TestFindByResource_200DecodesSpecStatusGeneration(t *testing.T) {
	t.Parallel()
	const tok = "sa-token"
	var gotAuth, gotPath, gotSelector string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotSelector = r.URL.Query().Get("labelSelector")
		_ = json.NewEncoder(w).Encode(listJSON(crJSON("http://web.local/callback", true, 2, 2)))
	}))
	t.Cleanup(srv.Close)

	c := mustClient(t, thunderapp.Config{
		BaseURL:     srv.URL,
		BearerToken: tok,
		HTTPClient:  tlsClientFor(t, srv),
	})
	view, err := c.FindByResource(context.Background(), testResource, testEnv)
	if err != nil {
		t.Fatalf("FindByResource: %v", err)
	}
	if view == nil {
		t.Fatal("FindByResource: want view, got nil")
	}
	if gotPath != "/apis/aep.wso2.com/v1alpha1/thunderapplications" {
		t.Errorf("path = %q, want cluster-wide list (no namespace)", gotPath)
	}
	wantSel := "openchoreo.dev/resource=" + testResource + ",openchoreo.dev/environment=" + testEnv
	if gotSelector != wantSel {
		t.Errorf("labelSelector = %q, want %q", gotSelector, wantSel)
	}
	if gotAuth != "Bearer "+tok {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if view.RedirectURIs != "http://web.local/callback" {
		t.Errorf("RedirectURIs = %q", view.RedirectURIs)
	}
	if !view.Ready {
		t.Error("Ready = false, want true")
	}
	if view.Generation != 2 || view.ObservedGeneration != 2 {
		t.Errorf("generations = %d/%d, want 2/2", view.Generation, view.ObservedGeneration)
	}
}

func TestFindByResource_EmptyListReturnsNilNil(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(listJSON())
	}))
	t.Cleanup(srv.Close)

	c := mustClient(t, thunderapp.Config{
		BaseURL:     srv.URL,
		BearerToken: "t",
		HTTPClient:  tlsClientFor(t, srv),
	})
	view, err := c.FindByResource(context.Background(), testResource, testEnv)
	if err != nil {
		t.Fatalf("FindByResource: %v", err)
	}
	if view != nil {
		t.Fatalf("want (nil, nil) on empty list; got %+v", view)
	}
}

func TestFindByResource_TwoItemsReturnsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(listJSON(
			crJSON("http://a/callback", true, 1, 1),
			crJSON("http://b/callback", true, 1, 1),
		))
	}))
	t.Cleanup(srv.Close)

	c := mustClient(t, thunderapp.Config{
		BaseURL:     srv.URL,
		BearerToken: "t",
		HTTPClient:  tlsClientFor(t, srv),
	})
	view, err := c.FindByResource(context.Background(), testResource, testEnv)
	if err == nil {
		t.Fatal("want error on two matches")
	}
	if view != nil {
		t.Fatalf("want nil view on two matches; got %+v", view)
	}
	if !strings.Contains(err.Error(), "2 ThunderApplications") {
		t.Errorf("error = %v, want match count", err)
	}
}

func TestFindByResource_5xxReturnsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	t.Cleanup(srv.Close)

	c := mustClient(t, thunderapp.Config{
		BaseURL:     srv.URL,
		BearerToken: "t",
		HTTPClient:  tlsClientFor(t, srv),
	})
	view, err := c.FindByResource(context.Background(), testResource, testEnv)
	if err == nil {
		t.Fatal("want error on 5xx")
	}
	if view != nil {
		t.Fatalf("want nil view on 5xx; got %+v", view)
	}
	if !strings.Contains(err.Error(), "500") && !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want status/body hint", err)
	}
}

func TestFindByResource_TokenFileReadPerRequest(t *testing.T) {
	t.Parallel()
	tokPath := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokPath, []byte(" first-token \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotAuth []string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(listJSON())
	}))
	t.Cleanup(srv.Close)

	c := mustClient(t, thunderapp.Config{
		BaseURL:    srv.URL,
		TokenFile:  tokPath,
		HTTPClient: tlsClientFor(t, srv),
	})
	if _, err := c.FindByResource(context.Background(), testResource, testEnv); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := os.WriteFile(tokPath, []byte("rotated-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.FindByResource(context.Background(), testResource, testEnv); err != nil {
		t.Fatalf("second: %v", err)
	}
	if len(gotAuth) != 2 {
		t.Fatalf("want 2 requests, got %d", len(gotAuth))
	}
	if gotAuth[0] != "Bearer first-token" {
		t.Errorf("first Authorization = %q", gotAuth[0])
	}
	if gotAuth[1] != "Bearer rotated-token" {
		t.Errorf("second Authorization = %q, want rotated token", gotAuth[1])
	}
}

func TestNew_CAFileUnreadable(t *testing.T) {
	t.Parallel()
	_, err := thunderapp.New(thunderapp.Config{
		BaseURL: "https://kube.example",
		CAFile:  filepath.Join(t.TempDir(), "missing.crt"),
	})
	if err == nil {
		t.Fatal("want error when CAFile is set but unreadable")
	}
	if !strings.Contains(err.Error(), "CA file") {
		t.Errorf("error = %v, want CA file hint", err)
	}
}

func TestNew_CAFileNotPEM(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(p, []byte("not-a-cert"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := thunderapp.New(thunderapp.Config{
		BaseURL: "https://kube.example",
		CAFile:  p,
	})
	if err == nil {
		t.Fatal("want error when CAFile is not PEM")
	}
	if !strings.Contains(err.Error(), "not valid PEM") {
		t.Errorf("error = %v, want PEM hint", err)
	}
}

func tlsClientFor(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	cert, err := x509.ParseCertificate(srv.TLS.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatalf("parse test cert: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
}
