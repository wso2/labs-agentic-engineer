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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/wso2/aep/aep-api/internal/ops"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// fakeIssues implements sourcecontrol.IssueService (issue_service.go:32); the
// methods the SRE ops do not call panic through the nil embed.
type fakeIssues struct {
	sourcecontrol.IssueService
	gotOrg string
}

func (f *fakeIssues) ListIssues(_ context.Context, orgID, _ string, _ []string) ([]sourcecontrol.IssueInfo, error) {
	f.gotOrg = orgID
	return nil, nil
}

func (f *fakeIssues) CreateIssue(_ context.Context, orgID, _ string, _ sourcecontrol.CreateIssueRequest) (*sourcecontrol.IssueResult, error) {
	f.gotOrg = orgID
	return &sourcecontrol.IssueResult{Number: 7, URL: "https://github.com/o/r/issues/7"}, nil
}

type fakeReports struct {
	ops.Repository
	got *ops.RcaAgentReport
}

func (f *fakeReports) Create(_ context.Context, r *ops.RcaAgentReport) error { f.got = r; return nil }

// The credential × route-group matrix: the SRE handoff bearer
// opens sre/… and nothing else, nothing else opens sre/…, and a nil verifier
// admits nobody.
func TestInternalGate_SRE(t *testing.T) {
	stack := newInternalStack(t)
	issues, reports := &fakeIssues{}, &fakeReports{}
	build := func(v *auth.SREHandoffVerifier, runnerAuth *auth.RunnerAuthorizer) http.Handler {
		deps := stack.deps
		deps.RunnerAuth = runnerAuth
		deps.SREHandoff, deps.Issues, deps.RcaReports = v, issues, reports
		user := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), &auth.Claims{Subject: "u", OuHandle: "acme"})))
			})
		}
		return NewHandler(AppParams{InternalDeps: deps, InboundAuth: user})
	}
	verifier := auth.NewSREHandoffVerifier("s3cr3t", "acme")
	on := build(verifier, stack.deps.RunnerAuth)
	off := build(nil, stack.deps.RunnerAuth)
	noRunner := build(verifier, nil)
	body := `{"title":"t","body":"b"}`
	invalid := `{"title":1}`
	userJWT, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "u", "ouHandle": "acme"}).SignedString([]byte("user-key"))
	if err != nil {
		t.Fatal(err)
	}
	report := `{"project":"p","title":"t","summary":"s","diagnosis":"d","classification":"none"}`
	cases := []struct {
		name         string
		h            http.Handler
		method, path string
		bearer, body string
		want         int
	}{
		{"sre bearer lists issues", on, "GET", "/internal/v1/sre/projects/p/issues", "Bearer s3cr3t", "", 200},
		{"sre bearer creates issue", on, "POST", "/internal/v1/sre/projects/p/issues", "Bearer s3cr3t", body, 200},
		{"sre bearer posts report", on, "POST", "/internal/v1/sre/rca-reports", "Bearer s3cr3t", report, 201},
		{"no bearer", on, "GET", "/internal/v1/sre/projects/p/issues", "", "", 401},
		{"wrong bearer", on, "GET", "/internal/v1/sre/projects/p/issues", "Bearer nope", "", 401},
		{"user JWT on sre", on, "GET", "/internal/v1/sre/projects/p/issues", "Bearer " + userJWT, "", 401},
		{"user JWT posts report", on, "POST", "/internal/v1/sre/rca-reports", "Bearer " + userJWT, report, 401},
		{"no bearer, schema-invalid body", on, "POST", "/internal/v1/sre/projects/p/issues", "", invalid, 401},
		{"wrong bearer, schema-invalid body", on, "POST", "/internal/v1/sre/projects/p/issues", "Bearer nope", invalid, 401},
		{"sre bearer, schema-invalid body", on, "POST", "/internal/v1/sre/projects/p/issues", "Bearer s3cr3t", invalid, 400},
		{"publisher token on sre", on, "GET", "/internal/v1/sre/projects/p/issues", "Bearer " + stack.mint("acme"), "", 401},
		{"publisher token posts report", on, "POST", "/internal/v1/sre/rca-reports", "Bearer " + stack.mint("acme"), report, 401},
		{"no verifier configured", off, "GET", "/internal/v1/sre/projects/p/issues", "Bearer s3cr3t", "", 401},
		{"no verifier configured, report", off, "POST", "/internal/v1/sre/rca-reports", "Bearer s3cr3t", report, 401},
		{"sre bearer on a runner op", on, "GET", "/internal/v1/runs/c/validation-context", "Bearer s3cr3t", "", 401},
		{"no runner auth: sre still served", noRunner, "GET", "/internal/v1/sre/projects/p/issues", "Bearer s3cr3t", "", 200},
		{"no runner auth: runner op 503", noRunner, "GET", "/internal/v1/runs/c/validation-context", "Bearer " + stack.mint("org-acme"), "", 503},
		{"rca report POST off /api/v1", on, "POST", "/api/v1/rca-agent/reports", "", report, 405},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.bearer != "" {
				req.Header.Set("Authorization", tc.bearer)
			}
			w := httptest.NewRecorder()
			tc.h.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.want, w.Body)
			}
		})
	}
	if issues.gotOrg != "acme" {
		t.Errorf("issue service got org %q, want acme (the verifier's org, never request input)", issues.gotOrg)
	}
	if reports.got == nil || reports.got.OrgID != "acme" {
		t.Errorf("report org = %v, want acme", reports.got)
	}
}
