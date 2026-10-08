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

package edge

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/config"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol/issues"
)

// The Issues agent's and an issue agent's tools are mounted behind their own
// verifier: an issues or issue token opens them, a discovery token does not,
// and neither issues-audience token opens the discovery surface.
func TestIssuesMCPSurface_MountedBehindItsOwnAudience(t *testing.T) {
	mgr, err := auth.NewTaskTokenManager(auth.TaskTokenConfig{
		PrivateKey: string(encodePKCS1(t, mustGenerateRSAKey(t))),
		Issuer:     "aep-bff", Audience: "git-service", TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewTaskTokenManager: %v", err)
	}
	// tools/list never reaches the issue service; any other call would panic.
	var svc struct{ sourcecontrol.IssueService }
	srv := httptest.NewServer(NewHandler(AppParams{
		Config:    config.Config{},
		Deps:      Deps{TaskTokens: mgr},
		IssuesMCP: issues.NewUserMCPHandler(svc, issues.IssueAgentPorts{}),
	}))
	t.Cleanup(srv.Close)

	issuesTok, err := mgr.IssueIssuesMCPToken("acme", "acme-expenses")
	if err != nil {
		t.Fatal(err)
	}
	issueTok, err := mgr.IssueIssueMCPToken("acme", "acme-expenses", 7)
	if err != nil {
		t.Fatal(err)
	}
	discoveryTok, err := mgr.IssueMCPToken("acme")
	if err != nil {
		t.Fatal(err)
	}
	post := func(path, bearer string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path,
			bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post("/internal/v1/issues/mcp", issuesTok); got != http.StatusOK {
		t.Errorf("issues token on issues surface = %d, want 200", got)
	}
	if got := post("/internal/v1/issues/mcp", issueTok); got != http.StatusOK {
		t.Errorf("issue token on issues surface = %d, want 200", got)
	}
	if got := post("/internal/v1/mcp", issueTok); got != http.StatusUnauthorized {
		t.Errorf("issue token on discovery surface = %d, want 401", got)
	}
	if got := post("/internal/v1/issues/mcp", discoveryTok); got != http.StatusUnauthorized {
		t.Errorf("discovery token on issues surface = %d, want 401", got)
	}
	if got := post("/internal/v1/mcp", issuesTok); got != http.StatusUnauthorized {
		t.Errorf("issues token on discovery surface = %d, want 401", got)
	}
}
