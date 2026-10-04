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

package aestudiotools

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func TestGitHubIdentity(t *testing.T) {
	t.Run("decodes the gitpat's user", func(t *testing.T) {
		var method, path string
		srv := identityServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
			method, path = r.Method, r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"login":"acme-bot","id":4242,"name":"Acme Bot"}`))
		})
		a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
		got, err := a.GitHubIdentity(context.Background(), "default")
		if err != nil {
			t.Fatal(err)
		}
		if *got != (sourcecontrol.GitHubUser{Login: "acme-bot", ID: 4242, Name: "Acme Bot"}) || method != http.MethodGet || path != "/github/identity" {
			t.Fatalf("identity=%+v request=%s %s", got, method, path)
		}
	})
	t.Run("rate limited is a retryable StatusError", func(t *testing.T) {
		srv := identityServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
			w.Header().Set("Retry-After", "30")
			writeProblem(w, http.StatusTooManyRequests, "github_rate_limited", "")
		})
		a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
		_, err := a.GitHubIdentity(context.Background(), "default")
		var se *StatusError
		if !errors.As(err, &se) || se.Status != 429 || se.Code != "github_rate_limited" || IsPermanent(err) {
			t.Fatalf("err = %v, want a retryable 429 StatusError", err)
		}
	})
	t.Run("a garbled 200 is an error", func(t *testing.T) {
		srv := identityServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) { _, _ = w.Write([]byte(`{"login":`)) })
		a := newAdapter(t, fixedTarget(srv.URL, "ou-123"), &countingTokens{})
		if _, err := a.GitHubIdentity(context.Background(), "default"); err == nil {
			t.Fatal("want a decode error")
		}
	})
}
