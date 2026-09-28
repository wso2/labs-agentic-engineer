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

package organization

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// The connect callback lands the user on the console's credentials page (its
// routes carry no org), with the outcome in the query.
func TestConnectCallback_RedirectsToTheConsoleCredentialsPage(t *testing.T) {
	t.Parallel()
	bearer := NewBearerService("state-signing-key", time.Minute)
	state, err := bearer.IssueConnectState("acme", "ada", 0, time.Minute)
	if err != nil {
		t.Fatalf("issue state: %v", err)
	}
	c := NewOrgGitHubController(nil, nil, bearer, "", "https://console.example.com", "")
	for name, query := range map[string]string{
		"neither code nor installation": "",
		"a malformed installation id":   "&installation_id=not-a-number",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, ConnectCallbackPath+"?state="+url.QueryEscape(state)+query, nil)
			rec := httptest.NewRecorder()
			c.HandleConnectCallback(rec, req)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want 303", rec.Code)
			}
			if got, want := rec.Header().Get("Location"), "https://console.example.com/settings/credentials?error=callback_invalid"; got != want {
				t.Fatalf("Location = %q, want %q", got, want)
			}
		})
	}
}
