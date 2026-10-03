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

// SREHandoffVerifier authenticates the OpenChoreo SRE agent on aep-api's SRE
// handoff MCP surface. The agent cannot present a Thunder JWT: its extensions
// loader (agents/sre-agent/src/extensions/config.py) resolves an MCP server's
// `headers` from `${VAR}` once at process start and never refreshes them, so
// a short-lived token would expire mid pod-lifetime. The credential is
// instead one long-lived random key that `aectl sre install` generates and
// writes into both aep-api's Secret and the agent's. One observability plane
// runs one SRE agent with one static header, so one key is all it can carry.
// The key authenticates the agent, not an org: each tool call names its org,
// and the tools verify that claim against the observer's recorded alerts.
//
// It guards exactly one mount, the SRE handoff MCP surface, and is never a
// substitute for Thunder JWT verification anywhere else.

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
)

// SREHandoffVerifier checks the SRE agent's bearer against the install-time
// handoff key.
type SREHandoffVerifier struct {
	token []byte
}

// NewSREHandoffVerifier returns a verifier for token, or nil when token is
// empty: an installation without the SRE handoff has no surface to guard.
func NewSREHandoffVerifier(token string) *SREHandoffVerifier {
	if token == "" {
		return nil
	}
	return &SREHandoffVerifier{token: []byte(token)}
}

// Verify reports whether bearer (the raw `Authorization` header value) is
// "Bearer <key>", comparing the key in constant time. A nil verifier rejects.
func (v *SREHandoffVerifier) Verify(bearer string) bool {
	if v == nil {
		return false
	}
	const prefix = "Bearer "
	if len(bearer) <= len(prefix) || bearer[:len(prefix)] != prefix {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(bearer[len(prefix):]), v.token) == 1
}

// Middleware answers 401 unless the request carries the handoff key.
func (v *SREHandoffVerifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !v.Verify(r.Header.Get("Authorization")) {
			slog.WarnContext(r.Context(), "sre handoff auth rejected")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
