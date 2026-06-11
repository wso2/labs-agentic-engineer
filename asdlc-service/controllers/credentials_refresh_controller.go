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
	"log/slog"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/utils/validate"
	"github.com/wso2/asdlc/asdlc-service/middleware"
	"github.com/wso2/asdlc/asdlc-service/services"
	"github.com/wso2/asdlc/asdlc-service/utils"
)

// CredentialsRefreshController issues fresh GitHub tokens to the agent's
// workspace credential helper. Authn is the Task JWT (RS256, verified
// against the BFF's JWKS by the taskMux middleware).
//
// The Task JWT carries verified `taskId` and `ocOrgId` claims; the
// controller projects them out of the request context and hands them to
// the service. There is no longer a callback into the BFF — the trust
// chain is JWT signature → JWKS → BFF private key.
type CredentialsRefreshController interface {
	Refresh(w http.ResponseWriter, r *http.Request)
}

type credentialsRefreshController struct {
	service services.CredentialsRefreshService
}

func NewCredentialsRefreshController(service services.CredentialsRefreshService) CredentialsRefreshController {
	return &credentialsRefreshController{service: service}
}

func (c *credentialsRefreshController) Refresh(w http.ResponseWriter, r *http.Request) {
	claims := middleware.TaskBearerClaims(r.Context())
	if claims == nil {
		utils.WriteErrorResponse(w, http.StatusUnauthorized, "missing bearer claims")
		return
	}
	// Validate JWT claim shape before they reach storage paths. The signature
	// is already verified, but a mis-issued token with a malformed claim
	// shouldn't be able to escape the per-org KV subtree.
	if err := validate.UUID(claims.TaskID); err != nil {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "taskId claim: "+err.Error())
		return
	}
	if err := validate.Slug(claims.OcOrgID); err != nil {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "ocOrgId claim: "+err.Error())
		return
	}

	resp, err := c.service.Refresh(r.Context(), claims.TaskID, claims.OcOrgID)
	if err != nil {
		slog.ErrorContext(r.Context(), "refresh credentials failed",
			"taskId", claims.TaskID, "ocOrgId", claims.OcOrgID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to refresh credentials")
		return
	}

	utils.WriteSuccessResponse(w, http.StatusOK, resp)
}
