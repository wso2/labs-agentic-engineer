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
// pod-lifetime. This is a dedicated, long-lived, narrowly-scoped credential
// instead — the S2S analogue of PublisherTokenVerifier/RunnerAuthorizer, not a
// widening of the Thunder JWT verifier.
//
// The credential itself is the per-org token aep-api's sreagent reconciler
// mints and stores in org_secrets (sreagent.Tokens, key "sre/handoff-token"),
// looked up fresh on every request through TokenGetter — never a static
// secret read once from the environment. Disabled by default (secure
// default): both org and a TokenGetter must be configured, or every presented
// bearer is rejected and the caller falls through to normal Thunder JWT
// verification (see edge.mountSurfaces).

import (
	"context"
	"crypto/subtle"
	"log/slog"
)

// TokenGetter resolves the current handoff token minted for an org. It is
// satisfied by sreagent.Tokens without this package importing sreagent —
// auth stays a leaf package; sreagent depends the other way (it already
// consumes secrets.CredentialStore).
type TokenGetter interface {
	// Get returns the org's minted handoff token; ok=false when none has
	// been minted yet.
	Get(ctx context.Context, org string) (token string, ok bool, err error)
}

// SREHandoffVerifier verifies aep-mcp-server's forwarded SRE-handoff bearer
// against the token minted for the one org it is scoped to, matching today's
// one-install-per-org deployment model (aectl sre install, docker-compose) —
// see the type's doc comment.
type SREHandoffVerifier struct {
	org    string
	tokens TokenGetter
}

// NewSREHandoffVerifier builds a verifier bound to org, resolving the current
// token from tokens on every Verify call. Returns nil when org is empty or
// tokens is nil, so an unconfigured deployment leaves the SRE handoff
// shortcut entirely absent rather than failing open.
func NewSREHandoffVerifier(org string, tokens TokenGetter) *SREHandoffVerifier {
	if org == "" || tokens == nil {
		return nil
	}
	return &SREHandoffVerifier{org: org, tokens: tokens}
}

// Verify checks bearer (the raw `Authorization` header value, e.g.
// "Bearer <token>") against the token minted for the configured org, in
// constant time, and returns synthetic Claims carrying that org on success.
// The returned Claims flow through auth.WithClaims exactly like a verified
// Thunder JWT's projection, so tenantGate binds the org with no changes of
// its own. Rejects when the prefix is missing, when no token has been minted
// for the org yet, on a token-store error, and on a mismatch.
func (v *SREHandoffVerifier) Verify(ctx context.Context, bearer string) (*Claims, bool) {
	if v == nil {
		return nil, false
	}
	const prefix = "Bearer "
	if len(bearer) <= len(prefix) || bearer[:len(prefix)] != prefix {
		return nil, false
	}
	token := bearer[len(prefix):]

	minted, ok, err := v.tokens.Get(ctx, v.org)
	if err != nil {
		slog.WarnContext(ctx, "sre handoff: token lookup failed")
		return nil, false
	}
	if !ok {
		return nil, false
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(minted)) != 1 {
		return nil, false
	}
	return &Claims{Subject: "sre-handoff", ClientID: "aep-mcp-server", OuHandle: v.org}, true
}
