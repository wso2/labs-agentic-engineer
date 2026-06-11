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
	"errors"
	"log/slog"
	"net/http"

	"github.com/wso2/asdlc/asdlc-service/services"
	"github.com/wso2/asdlc/asdlc-service/utils"
)

// WebhookRegistrationController handles per-repo GitHub webhook registration.
//
// This is distinct from the BFF's inbound webhook receiver — this controller
// is what the BFF calls when provisioning a repo to install the hook on
// GitHub's side.
type WebhookRegistrationController interface {
	Register(w http.ResponseWriter, r *http.Request)
	Deregister(w http.ResponseWriter, r *http.Request)
}

type webhookRegistrationController struct {
	service services.WebhookService
}

func NewWebhookRegistrationController(service services.WebhookService) WebhookRegistrationController {
	return &webhookRegistrationController{service: service}
}

type registerWebhookResponse struct {
	HookID *int64 `json:"hookId,omitempty"`
	// Strategy reports which strategy the credential dispatched. "per-repo"
	// when a hook ID is returned; "platform" when the call was a no-op
	// (Phase 2 App-mode platform-wide delivery).
	Strategy string `json:"strategy"`
}

func (c *webhookRegistrationController) Register(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if !requireProjectIDSlug(w, projectID) {
		return
	}

	hookID, err := c.service.Register(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, services.ErrRepoNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "repository not found")
			return
		}
		slog.ErrorContext(r.Context(), "register webhook failed", "project", projectID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to register webhook")
		return
	}

	resp := registerWebhookResponse{HookID: hookID, Strategy: "per-repo"}
	if hookID == nil {
		resp.Strategy = "platform"
	}
	utils.WriteSuccessResponse(w, http.StatusCreated, resp)
}

func (c *webhookRegistrationController) Deregister(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")
	if !requireProjectIDSlug(w, projectID) {
		return
	}

	if err := c.service.Deregister(r.Context(), projectID); err != nil {
		if errors.Is(err, services.ErrRepoNotFound) {
			utils.WriteErrorResponse(w, http.StatusNotFound, "repository not found")
			return
		}
		slog.ErrorContext(r.Context(), "deregister webhook failed", "project", projectID, "error", err)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to deregister webhook")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
