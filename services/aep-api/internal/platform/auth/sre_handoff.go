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

// SREHandoffVerifier closes the credential gap the SRE-agent handoff design
// left open (docs/design/draft/2026-09-17-sre-agent-extensions-handoff.md §7):
// aep-mcp-server forwards the OpenChoreo SRE agent's bearer straight through
// to aep-api, but that bearer can never be a normal Thunder user/service JWT —
// the generic OC extensions loader (agents/sre-agent/src/extensions/config.py)
// resolves an MCP server's `headers` from `${VAR}` ONCE at process start and
// never refreshes them, so a short-lived Thunder OAuth token would expire mid
// pod-lifetime. This is a dedicated, long-lived, narrowly-scoped shared
// secret instead — the S2S analogue of PublisherTokenVerifier/RunnerAuthorizer,
// not a widening of the Thunder JWT verifier.
//
// Disabled by default (secure default): both Secret and Org must be
// configured, or every presented bearer is rejected and the caller falls
// through to normal Thunder JWT verification (see edge.mountSurfaces).

import (
	"crypto/subtle"
	"strings"

	"github.com/wso2/aep/aep-api/internal/authz"
)

// sreHandoffPermissions is what this identity is authorized to do, and the
// whole of it. The three operations aep-mcp-server exposes are list-issues,
// create-issue and promote-task-from-issue; the first is a read of a project's
// issues, and the other two each START A RUN — creating an issue IS the
// dispatch when aep-api's classification says it should be (see
// services/aep-mcp-server/src/aepClient.ts), and promoting turns an issue into
// a coding task and dispatches it. So: the build pair, nothing else.
//
// This exists because the permission gate decides from the scope claim alone,
// and these synthetic claims used to carry none. That left the three
// operations un-gateable, and so carved out of the gate entirely — a hole in
// front of it rather than a decision inside it. Naming the permissions here
// makes the agent an ordinary principal the gate can reason about: it holds
// what it needs, it is refused everything else by the same deny-by-default
// rule as a person, and adding a tool to the MCP server that needs more than
// this fails at the gate rather than silently inheriting a bypass.
//
// Deliberately NOT ae:observability-view, though the agent is the SRE agent:
// permissions describe what an operation does, not who tends to call it, and
// nothing here reads an alert or an RCA report.
var sreHandoffPermissions = []authz.Permission{
	authz.PermissionBuildView,
	authz.PermissionBuild,
}

// sreHandoffScope is sreHandoffPermissions in the space-delimited form
// Claims.Scope carries, built once rather than written out as a literal so the
// two cannot drift.
var sreHandoffScope = func() string {
	parts := make([]string, len(sreHandoffPermissions))
	for i, p := range sreHandoffPermissions {
		parts[i] = string(p)
	}
	return strings.Join(parts, " ")
}()

// SREHandoffVerifier verifies aep-mcp-server's forwarded SRE-handoff bearer
// and resolves the one org it is scoped to. One verifier instance is scoped
// to exactly one org, matching today's one-install-per-org deployment model
// (aectl sre install, docker-compose) — see the type's doc comment.
type SREHandoffVerifier struct {
	secret string
	org    string
}

// NewSREHandoffVerifier builds a verifier bound to org. Returns nil when
// secret or org is empty, so an unconfigured deployment leaves the SRE
// handoff shortcut entirely absent rather than failing open.
func NewSREHandoffVerifier(secret, org string) *SREHandoffVerifier {
	if secret == "" || org == "" {
		return nil
	}
	return &SREHandoffVerifier{secret: secret, org: org}
}

// Verify checks bearer (the raw `Authorization` header value, e.g.
// "Bearer <token>") against the configured secret in constant time and
// returns synthetic Claims carrying the bound org and this identity's
// permissions on success. The returned Claims flow through auth.WithClaims
// exactly like a verified Thunder JWT's projection, so tenantGate binds the
// org and the permission gate reads the scope, both with no changes of their
// own — see sreHandoffPermissions for what the scope is and why.
func (v *SREHandoffVerifier) Verify(bearer string) (*Claims, bool) {
	if v == nil {
		return nil, false
	}
	const prefix = "Bearer "
	if len(bearer) <= len(prefix) || bearer[:len(prefix)] != prefix {
		return nil, false
	}
	token := bearer[len(prefix):]
	if subtle.ConstantTimeCompare([]byte(token), []byte(v.secret)) != 1 {
		return nil, false
	}
	return &Claims{
		Subject:  "sre-handoff",
		ClientID: "aep-mcp-server",
		OuHandle: v.org,
		Scope:    sreHandoffScope,
	}, true
}
