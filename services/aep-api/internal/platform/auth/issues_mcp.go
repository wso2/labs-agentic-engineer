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

package auth

// This file holds the gate for the Issues agent's project-fenced issue tools
// (POST /internal/v1/issues/mcp). Unlike AgentsScopedVerifier (org-wide
// discovery), the scope here is one project: both the org and the project come
// SOLELY from the signed ocOrgId + projectId claims of a token minted with
// IssueIssuesMCPToken, and an issue's agent is further fenced to the
// issueNumber claim of IssueIssueMCPToken. There is no publisher fallback —
// only aep-api mints these.

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
)

// IssuesMCPScope is the org + project an issues MCP token is fenced to, and
// the one issue when the token is an issue's (IssueIssueMCPToken). IssueNumber
// is 0 on the Issues view's token.
type IssuesMCPScope struct {
	OrgID       string
	ProjectID   string
	IssueNumber int
}

type issuesMCPScopeCtxKey struct{}

// WithIssuesMCPScope returns a copy of ctx carrying scope. Set by
// IssuesMCPVerifier.Middleware; read by the handler via IssuesMCPScopeFromContext.
func WithIssuesMCPScope(ctx context.Context, scope IssuesMCPScope) context.Context {
	return context.WithValue(ctx, issuesMCPScopeCtxKey{}, scope)
}

// IssuesMCPScopeFromContext returns the scope bound by IssuesMCPVerifier. ok is
// false when the request never passed the verifier; the handler then fails
// closed.
func IssuesMCPScopeFromContext(ctx context.Context) (IssuesMCPScope, bool) {
	scope, ok := ctx.Value(issuesMCPScopeCtxKey{}).(IssuesMCPScope)
	return scope, ok
}

// IssuesMCPVerifier authenticates the Issues agent to the issue tools and binds
// its project scope onto the request context.
type IssuesMCPVerifier struct {
	tokens *TaskTokenManager
}

// NewIssuesMCPVerifier builds the verifier. A nil manager yields a Middleware
// that fails closed with 503.
func NewIssuesMCPVerifier(tokens *TaskTokenManager) *IssuesMCPVerifier {
	return &IssuesMCPVerifier{tokens: tokens}
}

// Middleware verifies Authorization: Bearer <token> as a BFF-signed token with
// aud AudienceIssuesMCP and non-empty ocOrgId + projectId, binds the scope, and
// calls next. Every failure is a 401 with a generic body.
func (v *IssuesMCPVerifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v == nil || v.tokens == nil {
			http.Error(w, "issues mcp auth not configured", http.StatusServiceUnavailable)
			return
		}
		scope, err := v.resolveScope(r.Header.Get("Authorization"))
		if err != nil {
			slog.WarnContext(r.Context(), "issues mcp auth rejected", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithIssuesMCPScope(r.Context(), scope)))
	})
}

func (v *IssuesMCPVerifier) resolveScope(authHeader string) (IssuesMCPScope, error) {
	const prefix = "Bearer "
	if len(authHeader) <= len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return IssuesMCPScope{}, fmt.Errorf("bearer token required")
	}
	claims, err := v.tokens.Verify(authHeader[len(prefix):])
	if err != nil {
		return IssuesMCPScope{}, err
	}
	if !hasAudience(claims.Audience, AudienceIssuesMCP) {
		return IssuesMCPScope{}, fmt.Errorf("token audience %v is not %q", claims.Audience, AudienceIssuesMCP)
	}
	if claims.OcOrgID == "" || claims.ProjectID == "" {
		return IssuesMCPScope{}, fmt.Errorf("token lacks ocOrgId or projectId claim")
	}
	if claims.IssueNumber < 0 {
		return IssuesMCPScope{}, fmt.Errorf("token issueNumber %d is not an issue", claims.IssueNumber)
	}
	return IssuesMCPScope{OrgID: claims.OcOrgID, ProjectID: claims.ProjectID, IssueNumber: claims.IssueNumber}, nil
}
