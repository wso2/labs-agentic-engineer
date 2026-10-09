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
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/wso2/aep/aep-api/internal/clients/openchoreo"
	"github.com/wso2/aep/aep-api/internal/organization"
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

// Where the install has an M2M identity and impersonates orgs,
// AE Studio's clients (status reads and converge writes alike) never pass the
// caller's user JWT through, whatever the request strategy decides; the
// request's own clients still do.
func TestAEStudioOCConfig_M2MWithImpersonationWhereConfigured(t *testing.T) {
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
	want := seen{authorization: "Bearer m2m-token", impersonate: "ou-of-acme"}

	studio := aeStudioOC(base)
	if _, err := studio.Resources.GetResourceType(req, "acme", "ae-studio"); err != nil { // a status read
		t.Fatal(err)
	}
	if got := last(); got != want {
		t.Fatalf("status read sent %+v", got)
	}
	if _, err := studio.Resources.UpdateResourceType(req, "acme", &openchoreo.ResourceType{Metadata: openchoreo.OCObjectMeta{Name: "ae-studio"}}); err != nil { // a converge write
		t.Fatal(err)
	}
	if got := last(); got != want {
		t.Fatalf("converge write sent %+v", got)
	}

	if _, err := openchoreo.NewResourceClient(base).GetResourceType(req, "acme", "ae-studio"); err != nil {
		t.Fatal(err)
	}
	if got := last(); got != (seen{authorization: "Bearer user-jwt"}) {
		t.Fatalf("the request's own client sent %+v", got)
	}
}

// Locally (one admin identity, no resolver) AE Studio's config is the
// request's own.
func TestAEStudioOCConfig_UnchangedWithoutImpersonation(t *testing.T) {
	for name, cfg := range map[string]openchoreo.Config{
		"no resolver":      {BaseURL: "http://oc", AuthProvider: staticM2M{}, RequestAuthStrategy: passThroughStrategy{}},
		"no auth provider": {BaseURL: "http://oc", RequestAuthStrategy: passThroughStrategy{}, ImpersonateOrgResolver: func(context.Context, string) (string, error) { return "x", nil }},
	} {
		if got := aeStudioOCConfig(cfg); got.RequestAuthStrategy != cfg.RequestAuthStrategy {
			t.Errorf("%s: strategy replaced", name)
		}
	}
}

type profilesByOrg map[string]*organization.OrganizationIDPProfile

func (p profilesByOrg) GetProfileByOrgID(_ context.Context, org string) (*organization.OrganizationIDPProfile, error) {
	if org == "down" {
		return nil, errors.New("db down")
	}
	return p[org], nil
}

// The ae-studio/ gate's binding is the client id recorded on the org's IDP
// profile; an org with no profile, or none recorded, has no client.
func TestStudioClientRecords(t *testing.T) {
	r := studioClientRecords{profiles: profilesByOrg{
		"acme":   {StudioClientID: "ae-studio-acme"},
		"globex": {},
	}}
	for org, want := range map[string]string{"acme": "ae-studio-acme", "globex": "", "initech": ""} {
		if got, err := r.StudioClientID(context.Background(), org); err != nil || got != want {
			t.Errorf("%s: got %q %v, want %q", org, got, err, want)
		}
	}
	if _, err := r.StudioClientID(context.Background(), "down"); err == nil {
		t.Error("a failed profile read must be an error, not an empty id")
	}
}
