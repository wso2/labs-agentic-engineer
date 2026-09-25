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

package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/thunderapp"
	"github.com/wso2/aep/aep-api/internal/dependencies"
	"github.com/wso2/aep/aep-api/internal/projects"
)

func TestToConsumerURLMarker_MapsEnvConfigAndPath(t *testing.T) {
	got := toConsumerURLMarker(dependencies.TypeMarkers{
		ConsumerURLEnvConfig: "redirectUris",
		ConsumerURLPath:      "/callback",
	})
	want := projects.ConsumerURLMarker{EnvConfig: "redirectUris", Path: "/callback"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestToConsumerURLMarker_EmptyPathDefaultsWhenEnvConfigSet(t *testing.T) {
	got := toConsumerURLMarker(dependencies.TypeMarkers{
		ConsumerURLEnvConfig: "redirectUris",
	})
	if got.EnvConfig != "redirectUris" {
		t.Errorf("EnvConfig = %q, want redirectUris", got.EnvConfig)
	}
	if got.Path != dependencies.DefaultConsumerURLPath {
		t.Errorf("Path = %q, want default %q", got.Path, dependencies.DefaultConsumerURLPath)
	}
}

func TestToConsumerURLMarker_NoEnvConfigStaysEmpty(t *testing.T) {
	got := toConsumerURLMarker(dependencies.TypeMarkers{})
	if got != (projects.ConsumerURLMarker{}) {
		t.Fatalf("got %+v, want zero value", got)
	}
}

func TestThunderApplicationReader_FindByResource_404IsAPIMissing(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "404 page not found")
	}))
	t.Cleanup(srv.Close)

	client, err := thunderapp.New(thunderapp.Config{
		BaseURL:     srv.URL,
		BearerToken: "t",
		HTTPClient:  tlsClientFor(t, srv),
	})
	if err != nil {
		t.Fatalf("thunderapp.New: %v", err)
	}

	view, err := thunderApplicationReader{client: client}.FindByResource(context.Background(), "proj-idp", "default")
	if view != nil {
		t.Fatalf("want nil view on LIST 404; got %+v", view)
	}
	if !errors.Is(err, projects.ErrThunderApplicationAPIMissing) {
		t.Fatalf("FindByResource err = %v, want %v", err, projects.ErrThunderApplicationAPIMissing)
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
