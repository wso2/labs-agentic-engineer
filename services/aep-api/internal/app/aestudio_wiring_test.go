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
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	authn "github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/ocauth"
)

type passThroughStrategy struct{}

func (passThroughStrategy) Decide(context.Context) ocauth.AuthMode { return ocauth.AuthModeUserJWT }

type staticM2M struct{}

func (staticM2M) Token() (string, error) { return "m2m-token", nil }
func (staticM2M) Invalidate()            {}

// seen is what OpenChoreo received on one request.
type seen struct{ authorization, impersonate string }

func recordingOC(t *testing.T) (*httptest.Server, func() seen) {
	t.Helper()
	var mu sync.Mutex
	var last seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		last = seen{authorization: r.Header.Get("Authorization"), impersonate: r.Header.Get("X-Impersonate-Org")}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"metadata":{"name":"ae-studio"},"spec":{"resources":[]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, func() seen { mu.Lock(); defer mu.Unlock(); return last }
}

// P-19: where the install has an M2M identity and impersonates orgs, the
// converge clients never pass the caller's user JWT through, whatever the
// request strategy decides; the status clients still do.
func TestConvergeOCConfig_M2MWithImpersonationWhereConfigured(t *testing.T) {
	srv, last := recordingOC(t)
	base := openchoreo.Config{
		BaseURL:             srv.URL,
		AuthProvider:        staticM2M{},
		RequestAuthStrategy: passThroughStrategy{},
		ImpersonateOrgResolver: func(_ context.Context, ns string) (string, error) {
			return "ou-of-" + ns, nil
		},
	}
	req := authn.WithAuthToken(context.Background(), "user-jwt")

	if _, err := openchoreo.NewResourceClient(convergeOCConfig(base)).GetResourceType(req, "acme", "ae-studio"); err != nil {
		t.Fatal(err)
	}
	if got := last(); got != (seen{authorization: "Bearer m2m-token", impersonate: "ou-of-acme"}) {
		t.Fatalf("converge call sent %+v", got)
	}

	if _, err := openchoreo.NewResourceClient(base).GetResourceType(req, "acme", "ae-studio"); err != nil {
		t.Fatal(err)
	}
	if got := last(); got != (seen{authorization: "Bearer user-jwt"}) {
		t.Fatalf("status call sent %+v", got)
	}
}

// Locally (one admin identity, no resolver) the converge config is the
// request's own.
func TestConvergeOCConfig_UnchangedWithoutImpersonation(t *testing.T) {
	for name, cfg := range map[string]openchoreo.Config{
		"no resolver":      {BaseURL: "http://oc", AuthProvider: staticM2M{}, RequestAuthStrategy: passThroughStrategy{}},
		"no auth provider": {BaseURL: "http://oc", RequestAuthStrategy: passThroughStrategy{}, ImpersonateOrgResolver: func(context.Context, string) (string, error) { return "x", nil }},
	} {
		if got := convergeOCConfig(cfg); got.RequestAuthStrategy != cfg.RequestAuthStrategy {
			t.Errorf("%s: strategy replaced", name)
		}
	}
}
