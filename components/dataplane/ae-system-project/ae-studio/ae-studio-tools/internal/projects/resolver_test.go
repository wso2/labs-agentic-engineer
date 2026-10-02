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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
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
	// mapping is by status, the body is never parsed for a decision (Q-2).
	status.Store(http.StatusNotFound)
	if _, err = r.Resolve(context.Background(), "greeter"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("404: err = %v, want ErrUnknown", err)
	}
	for _, s := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusUnauthorized} {
		status.Store(int32(s))
		if _, err = r.Resolve(context.Background(), "greeter"); !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrUnknown) {
			t.Fatalf("%d: err = %v, want ErrUnavailable", s, err)
		}
	}
	srv.Close()
	if _, err = r.Resolve(context.Background(), "greeter"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unreachable: err = %v, want ErrUnavailable (transient, not a denial)", err)
	}
}

func TestResolver_UnusableAnswerIsUnavailable(t *testing.T) {
	for name, body := range map[string]string{
		"not json":    `{`,
		"empty owner": `{"owner":"","repo":"greeter","defaultBranch":"main","cloneUrl":"https://x/y.git"}`,
		"empty repo":  `{"owner":"acme","repo":"","defaultBranch":"main","cloneUrl":"https://x/y.git"}`,
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
