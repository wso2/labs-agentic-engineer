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

package mcpdiscovery

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/platform/auth"
)

// playgroundTokenTTLSeconds mirrors TaskTokenManager's 5-minute MCP token TTL.
// The manager does not expose that TTL as a named constant, so this is the
// "known 5-min TTL" restated here for the response body; if the manager's TTL
// ever changes, this must change with it.
const playgroundTokenTTLSeconds = 300

// playgroundTokenRequest is the optional JSON body for
// POST /internal/v1/mcp/playground-token. An absent/empty body is valid — the
// org defaults to "default".
type playgroundTokenRequest struct {
	OrgHandle string `json:"orgHandle"`
}

// playgroundTokenResponse mirrors what the @aep/playground CLI mints
// against: a bearer token plus its lifetime, so the caller knows when it must
// re-mint (it always does, every turn).
type playgroundTokenResponse struct {
	Token            string `json:"token"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
}

// NewPlaygroundTokenHandler mints a short-lived BFF-signed MCP token (aud
// auth.AudienceMCP, scoped to one org) for a human driving the
// @aep/playground CLI locally against a real aep-api.
//
// LOCAL DEV ONLY. The route this handler backs is mounted ONLY when
// PLAYGROUND_TOKEN_ENABLED=true (routes.go), a flag only
// deployments/docker-compose.yml sets — everywhere else the route is simply
// ABSENT (404 by omission, never present-but-403). There is deliberately NO
// caller authentication here: the flag itself, off by default, is the whole
// gate. Production agent→BFF authentication remains an OPEN DECISION this
// endpoint does not prejudge — it exists solely so a developer can obtain a
// working token for manual playground runs without that decision being
// settled first. A token is minted FRESH on every call; the playground CLI
// calls this once per turn and never caches the result.
func NewPlaygroundTokenHandler(tokens *auth.TaskTokenManager) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req playgroundTokenRequest
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10) // tiny contract; cap the buffer
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		orgHandle := req.OrgHandle
		if orgHandle == "" {
			orgHandle = "default"
		}

		token, err := tokens.IssueMCPToken(orgHandle)
		if err != nil {
			http.Error(w, "failed to mint playground token", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(playgroundTokenResponse{
			Token:            token,
			ExpiresInSeconds: playgroundTokenTTLSeconds,
		})
	})
}
