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

package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// thunderDocument is the shape ThunderID 1.0 answers (checked on a live
// cluster): the issuer is the public URL, the endpoints sit under it.
func thunderDocument(issuer string) string {
	return `{"issuer":"` + issuer + `","jwks_uri":"` + issuer + `/oauth2/jwks","token_endpoint":"` + issuer + `/oauth2/token","authorization_endpoint":"` + issuer + `/oauth2/authorize"}`
}

func serve(t *testing.T, status int, body func(issuer string) string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != discoveryPath {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body(srv.URL)))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDiscover_ReadsTheEndpointsTheDocumentNames(t *testing.T) {
	srv := serve(t, http.StatusOK, thunderDocument)
	got, err := Discover(context.Background(), srv.Client(), srv.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	want := Endpoints{Issuer: srv.URL, JWKSURL: srv.URL + "/oauth2/jwks", TokenURL: srv.URL + "/oauth2/token"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !got.Configured() {
		t.Fatal("discovered endpoints must read as configured")
	}
}

func TestDiscover_Refusals(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   func(string) string
		want   string
	}{
		"another issuer": {http.StatusOK, func(string) string { return thunderDocument("http://other.example") }, "names issuer"},
		"no token endpoint": {http.StatusOK, func(iss string) string {
			return `{"issuer":"` + iss + `","jwks_uri":"` + iss + `/oauth2/jwks"}`
		}, "no jwks_uri or no token_endpoint"},
		"not a document": {http.StatusOK, func(string) string { return "<html>" }, "not a discovery document"},
		"not found":      {http.StatusNotFound, thunderDocument, "HTTP 404"},
	} {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, tc.status, tc.body)
			_, err := Discover(context.Background(), srv.Client(), srv.URL)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want containing %q", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "<html>") {
				t.Fatalf("the error must not carry the body: %v", err)
			}
		})
	}
	if _, err := Discover(context.Background(), nil, ""); err == nil {
		t.Fatal("an empty issuer must be refused")
	}
	if _, err := Discover(context.Background(), nil, "thunder.local"); err == nil {
		t.Fatal("a relative issuer must be refused")
	}
}

// The in-cluster Service and the HTTPS gateway reach the same IdP on another
// address: the paths stay, the authority changes, the issuer does not.
func TestOnBase(t *testing.T) {
	pub := Endpoints{
		Issuer:   "http://thunder.openchoreo.localhost:8080",
		JWKSURL:  "http://thunder.openchoreo.localhost:8080/oauth2/jwks",
		TokenURL: "http://thunder.openchoreo.localhost:8080/oauth2/token",
	}
	inCluster := pub.OnBase("http://thunder-service.thunder.svc.cluster.local:8090/")
	if inCluster.JWKSURL != "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/jwks" ||
		inCluster.TokenURL != "http://thunder-service.thunder.svc.cluster.local:8090/oauth2/token" ||
		inCluster.Issuer != pub.Issuer {
		t.Fatalf("in-cluster: %+v", inCluster)
	}
	gateway := pub.OnBase("https://thunder.openchoreo.localhost:8443")
	if gateway.JWKSURL != "https://thunder.openchoreo.localhost:8443/oauth2/jwks" {
		t.Fatalf("gateway: %+v", gateway)
	}
	if got := pub.OnBase(""); got != pub {
		t.Fatalf("an empty base must leave the endpoints alone: %+v", got)
	}
	if got := pub.OnBase("not a url"); got != pub {
		t.Fatalf("a relative base must leave the endpoints alone: %+v", got)
	}
	if (Endpoints{}).Configured() {
		t.Fatal("the zero value is not configured")
	}
}
