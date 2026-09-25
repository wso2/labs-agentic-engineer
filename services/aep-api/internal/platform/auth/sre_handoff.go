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

import "crypto/subtle"

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
// returns synthetic Claims carrying the bound org on success. The returned
// Claims flow through auth.WithClaims exactly like a verified Thunder JWT's
// projection, so tenantGate binds the org with no changes of its own.
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
	return &Claims{Subject: "sre-handoff", ClientID: "aep-mcp-server", OuHandle: v.org}, true
}
