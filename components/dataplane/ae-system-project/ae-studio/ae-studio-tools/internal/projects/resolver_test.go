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

package projects

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
	"github.com/wso2/aep/ae-studio-tools/internal/platform"
)

// mustClient is an aep-api client at url's /internal/v1 (the prefix
// platform.NewAEPAPI adds) with a static bearer token.
func mustClient(t *testing.T, url, token string) *aepapi.ClientWithResponses {
	t.Helper()
	c, err := aepapi.NewClientWithResponses(url+"/internal/v1", aepapi.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+token)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestResolver_NoCacheAndMapping(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/internal/v1/ae-studio/projects/greeter/repository" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization missing")
		}
		w.Header().Set("Content-Type", "application/json")
		s := int(status.Load())
		w.WriteHeader(s)
		if s == http.StatusOK {
			_, _ = w.Write([]byte(`{"owner":"acme-gh","repo":"greeter","defaultBranch":"main","cloneUrl":"https://github.com/acme-gh/greeter.git"}`))
		}
	}))
	defer srv.Close()
	r := NewAEPAPIResolver(mustClient(t, srv.URL, "tok"))

	got, err := r.Resolve(context.Background(), "greeter")
	if err != nil {
		t.Fatal(err)
	}
	if want := (Repository{"acme-gh", "greeter", "main", "https://github.com/acme-gh/greeter.git"}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	_, _ = r.Resolve(context.Background(), "greeter")
	if calls.Load() != 2 {
		t.Fatalf("calls = %d; no cache: every call asks aep-api", calls.Load())
	}

	// An empty json error body must not turn a 404 into "unavailable": the
	// mapping is by status, the body is never parsed for a decision.
	status.Store(http.StatusNotFound)
	if _, err = r.Resolve(context.Background(), "greeter"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("404: err = %v, want ErrUnknown", err)
	}
	for _, s := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		status.Store(int32(s))
		_, err = r.Resolve(context.Background(), "greeter")
		if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrUnknown) || errors.Is(err, ErrMisconfigured) {
			t.Fatalf("%d: err = %v, want ErrUnavailable only", s, err)
		}
	}
	srv.Close()
	_, err = r.Resolve(context.Background(), "greeter")
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrMisconfigured) {
		t.Fatalf("unreachable: err = %v, want ErrUnavailable only (transient, not a denial)", err)
	}
}

func TestResolver_UnusableAnswerIsUnavailable(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     `{`,
		"empty owner":  `{"owner":"","repo":"greeter","defaultBranch":"main","cloneUrl":"https://x/y.git"}`,
		"empty repo":   `{"owner":"acme","repo":"","defaultBranch":"main","cloneUrl":"https://x/y.git"}`,
		"empty branch": `{"owner":"acme","repo":"greeter","defaultBranch":"","cloneUrl":"https://x/y.git"}`,
		"no cloneUrl":  `{"owner":"acme","repo":"greeter","defaultBranch":"main"}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		_, err := NewAEPAPIResolver(mustClient(t, srv.URL, "tok")).Resolve(context.Background(), "greeter")
		srv.Close()
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s: err = %v, want ErrUnavailable", name, err)
		}
	}
}

// tokenEndpoint answers client_credentials mints with tok-N, or status when
// it is set.
func tokenEndpoint(status int) *httptest.Server {
	var n atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"tok-%d","expires_in":3600}`, n.Add(1))
	}))
}

// A credential fault is still a 503 on the wire (ErrUnavailable) but is
// also ErrMisconfigured, so the caller can log it loudly. The aep-api rows
// go through platform.NewAEPAPI, so a 401 is the answer after its one retry.
func TestResolver_CredentialFaultsAreMisconfigured(t *testing.T) {
	cases := map[string]struct{ idpStatus, apiStatus int }{
		"aep-api 401 after retry": {0, http.StatusUnauthorized},
		"aep-api 403":             {0, http.StatusForbidden},
		"token endpoint 401":      {http.StatusUnauthorized, http.StatusOK},
		"token endpoint 400":      {http.StatusBadRequest, http.StatusOK},
	}
	for name, c := range cases {
		idp := tokenEndpoint(c.idpStatus)
		var apiCalls atomic.Int32
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			apiCalls.Add(1)
			w.WriteHeader(c.apiStatus)
		}))
		client, err := platform.NewAEPAPI(api.URL, &platform.ClientCredentials{TokenURL: idp.URL, ClientID: "ae-studio-acme", ClientSecret: "s"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = NewAEPAPIResolver(client).Resolve(context.Background(), "greeter")
		idp.Close()
		api.Close()
		if !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrMisconfigured) || errors.Is(err, ErrUnknown) {
			t.Fatalf("%s: err = %v, want ErrUnavailable and ErrMisconfigured", name, err)
		}
		if c.apiStatus == http.StatusUnauthorized && apiCalls.Load() != 2 {
			t.Fatalf("%s: aep-api calls = %d, want 2 (one retry)", name, apiCalls.Load())
		}
	}
}

func TestResolver_TokenEndpointDownIsUnavailableOnly(t *testing.T) {
	idp := tokenEndpoint(http.StatusServiceUnavailable)
	defer idp.Close()
	client, _ := platform.NewAEPAPI("http://127.0.0.1:1", &platform.ClientCredentials{TokenURL: idp.URL, ClientID: "c", ClientSecret: "s"})
	_, err := NewAEPAPIResolver(client).Resolve(context.Background(), "greeter")
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrMisconfigured) {
		t.Fatalf("err = %v, want ErrUnavailable only", err)
	}
}

// ResolveSkills asks aep-api for the org's skills repository on every call,
// mapped by status like Resolve, except that a 404 (the org has none) is
// ErrUnavailable: it is not a denial of any project, and a turn cannot run
// without its skills.
func TestResolver_ResolveSkills(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	status.Store(http.StatusOK)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/internal/v1/ae-studio/skills/repository" || r.Method != http.MethodGet {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		s := int(status.Load())
		w.WriteHeader(s)
		if s == http.StatusOK {
			_, _ = w.Write([]byte(`{"owner":"acme-gh","repo":"org-skills","defaultBranch":"main","cloneUrl":"https://github.com/acme-gh/org-skills.git"}`))
		}
	}))
	defer srv.Close()
	r := NewAEPAPIResolver(mustClient(t, srv.URL, "tok"))

	got, err := r.ResolveSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := (Repository{"acme-gh", "org-skills", "main", "https://github.com/acme-gh/org-skills.git"}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	_, _ = r.ResolveSkills(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("calls = %d; no cache: every call asks aep-api", calls.Load())
	}
	for _, s := range []int{http.StatusNotFound, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		status.Store(int32(s))
		_, err = r.ResolveSkills(context.Background())
		if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrUnknown) || errors.Is(err, ErrMisconfigured) {
			t.Fatalf("%d: err = %v, want ErrUnavailable only", s, err)
		}
	}
	status.Store(http.StatusForbidden)
	if _, err = r.ResolveSkills(context.Background()); !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrMisconfigured) {
		t.Fatalf("403: err = %v, want ErrUnavailable and ErrMisconfigured", err)
	}
}
