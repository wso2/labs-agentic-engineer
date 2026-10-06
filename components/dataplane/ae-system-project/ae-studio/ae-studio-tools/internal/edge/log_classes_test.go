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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wso2/aep/ae-studio-tools/internal/github"
)

// The two values a raw error could carry into a log line: a token-like
// string and a URL. Neither may reach it.
const (
	plantedToken = "planted-token-0123456789abcdef"
	plantedURL   = "https://planted.example.invalid/owner/repo.git"
)

// logLine returns the one JSON line whose msg is msg, failing when there is none.
func logLine(t *testing.T, logs string, msg string) map[string]any {
	t.Helper()
	for _, l := range strings.Split(strings.TrimSpace(logs), "\n") {
		var line map[string]any
		if json.Unmarshal([]byte(l), &line) == nil && line["msg"] == msg {
			return line
		}
	}
	t.Fatalf("no %s line in %s", msg, logs)
	return nil
}

func assertNoPlanted(t *testing.T, logs string) {
	t.Helper()
	if strings.Contains(logs, plantedToken) || strings.Contains(logs, "planted.example.invalid") {
		t.Fatalf("raw error text reached the log: %s", logs)
	}
}

// github.identity_failed names GitHub's answer by class and status, never the
// client's error text (which carries the request URL and GitHub's body).
func TestGitHubIdentityFailed_LogsClassNotText(t *testing.T) {
	planted := fmt.Errorf("GET %s?access_token=%s: %w", plantedURL, plantedToken,
		&github.HTTPStatusError{StatusCode: http.StatusUnauthorized, Body: plantedToken, URL: plantedURL})
	h := newHarness(t, withGitHubErr(planted))
	if rec := h.do("GET", "/internal/v1/github/identity", h.m2m(), "ou-1", nil); rec.Code != http.StatusBadGateway {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	line := logLine(t, h.logBuf.String(), "github.identity_failed")
	if line["class"] != "status" || line["status"] != float64(http.StatusUnauthorized) {
		t.Fatalf("line = %v, want class status, status 401", line)
	}
	if _, ok := line["error"]; ok {
		t.Fatalf("line still carries error: %v", line)
	}
	assertNoPlanted(t, h.logBuf.String())
}

func TestGitHubIdentityFailed_TransportIsAClass(t *testing.T) {
	h := newHarness(t, withGitHubErr(fmt.Errorf("dial %s: connection refused (%s)", plantedURL, plantedToken)))
	h.do("GET", "/internal/v1/github/identity", h.m2m(), "ou-1", nil)
	if line := logLine(t, h.logBuf.String(), "github.identity_failed"); line["class"] != "transport" {
		t.Fatalf("line = %v, want class transport", line)
	}
	assertNoPlanted(t, h.logBuf.String())
}

// internal.handler_failed logs the error's class, not its text: any handler
// error can carry git or GitHub text.
func TestInternalResponseError_LogsClassNotText(t *testing.T) {
	logs := captureLogs(t)
	rec := httptest.NewRecorder()
	writeResponseError(rec, httptest.NewRequest(http.MethodGet, "/internal/v1/repos/acme/greeter", nil),
		fmt.Errorf("git fetch %s with %s failed", plantedURL, plantedToken))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got %d", rec.Code)
	}
	line := logLine(t, logs.String(), "internal.handler_failed")
	if line["class"] != "other" || line["path"] != "/internal/v1/repos/acme/greeter" {
		t.Fatalf("line = %v, want class other and the path", line)
	}
	assertNoPlanted(t, logs.String())
}

func TestInternalResponseError_GitHubAnswerKeepsItsStatus(t *testing.T) {
	logs := captureLogs(t)
	writeResponseError(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/internal/v1/x", nil),
		&github.HTTPStatusError{StatusCode: http.StatusForbidden, Body: plantedToken, URL: plantedURL})
	line := logLine(t, logs.String(), "internal.handler_failed")
	if line["class"] != "status" || line["status"] != float64(http.StatusForbidden) {
		t.Fatalf("line = %v, want class status, status 403", line)
	}
	assertNoPlanted(t, logs.String())
}
