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

package controllers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/services"
)

// JWKSController serves the BFF Task JWT public key set at
// /auth/external/jwks.json. The endpoint is public (no auth) — verifiers
// (today: git-service) need to fetch it before any authenticated call.
type JWKSController interface {
	GetJWKS(w http.ResponseWriter, r *http.Request)
}

type jwksController struct {
	taskTokens *services.TaskTokenManager
}

// NewJWKSController returns a controller that serves the active signing
// public key. taskTokens may be nil when Task JWT issuance is not configured;
// in that case the endpoint returns an empty JWK set.
func NewJWKSController(taskTokens *services.TaskTokenManager) JWKSController {
	return &jwksController{taskTokens: taskTokens}
}

func (c *jwksController) GetJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if c.taskTokens == nil {
		_ = json.NewEncoder(w).Encode(services.JWKSResponse{Keys: []services.JWK{}})
		return
	}

	if err := json.NewEncoder(w).Encode(c.taskTokens.JWKS()); err != nil {
		slog.ErrorContext(r.Context(), "failed to encode JWKS", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
}
