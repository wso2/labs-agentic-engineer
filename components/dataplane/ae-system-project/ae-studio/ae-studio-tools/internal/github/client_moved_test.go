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

package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterWebhook_FindsExistingHookAcrossPages(t *testing.T) {
	const hookURL = "https://tools.example/webhooks/github"
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/acme/greeter/hooks", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Validation Failed","errors":[{"message":"Hook already exists on this repository"}]}`))
	})
	mux.HandleFunc("GET /repos/acme/greeter/hooks", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`[{"id":202,"config":{"url":"` + hookURL + `"}}]`))
			return
		}
		w.Header().Set("Link", `<`+srv.URL+`/repos/acme/greeter/hooks?per_page=100&page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[{"id":101,"config":{"url":"https://other.example/hook"}}]`))
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	c := New(Config{APIBase: srv.URL, Token: staticToken("t"), HookURL: hookURL, HookSecret: "s"})
	id, err := c.RegisterWebhook(context.Background(), "acme", "greeter", []string{"push"})
	if err != nil {
		t.Fatal(err)
	}
	if id != 202 {
		t.Fatalf("hook id = %d, want 202 (found on page 2)", id)
	}
}

// TestWrites_ReturnHTTPStatusError: a write GitHub refuses is a typed
// *HTTPStatusError, except DeleteWebhook, for which 404 is success (the hook
// is already gone).
func TestWrites_ReturnHTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := New(Config{APIBase: srv.URL, Token: staticToken("t")})
	for name, call := range map[string]func() error{
		"close":    func() error { return c.CloseIssue(context.Background(), "acme", "greeter", 7) },
		"register": func() error { _, err := c.RegisterWebhook(context.Background(), "acme", "greeter", nil); return err },
	} {
		if err := call(); !IsHTTPStatus(err, http.StatusNotFound) {
			t.Errorf("%s: err = %v, want *HTTPStatusError 404", name, err)
		}
	}
	if err := c.DeleteWebhook(context.Background(), "acme", "greeter", 9); err != nil {
		t.Errorf("delete: err = %v, want nil (404 is success)", err)
	}
}

// TestRegisterWebhook_NeverFollowsANextPageOffTheAPIBase: the hook list's
// Link header is followed only within the API base, so the gitpat is never
// sent to another host.
func TestRegisterWebhook_NeverFollowsANextPageOffTheAPIBase(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("request reached another host with Authorization %q", r.Header.Get("Authorization"))
	}))
	defer other.Close()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos/acme/greeter/hooks", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Hook already exists on this repository"}]}`))
	})
	mux.HandleFunc("GET /repos/acme/greeter/hooks", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<`+other.URL+`/repos/acme/greeter/hooks?page=2>; rel="next"`)
		_, _ = w.Write([]byte(`[]`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New(Config{APIBase: srv.URL, Token: staticToken("t"), HookURL: "https://tools.example/webhooks/github"})
	if _, err := c.RegisterWebhook(context.Background(), "acme", "greeter", nil); err == nil {
		t.Fatal("err = nil, want a refusal to follow the off-base next page")
	}
}

// TestHTTPStatusError_MessageLeavesTheBodyOut: the body stays a field for
// the caller; a logged error carries no GitHub content.
func TestHTTPStatusError_MessageLeavesTheBodyOut(t *testing.T) {
	e := &HTTPStatusError{StatusCode: http.StatusUnprocessableEntity, Body: `{"message":"title: Secret plan"}`, URL: "https://api.github.com/repos/a/b/issues"}
	if strings.Contains(e.Error(), "Secret plan") || !strings.Contains(e.Error(), "422") {
		t.Fatalf("Error() = %q", e.Error())
	}
}

func staticToken(v string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return v, nil }
}
