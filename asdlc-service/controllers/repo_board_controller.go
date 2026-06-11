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
	"github.com/wso2/asdlc/asdlc-service/utils"
)

// RepoBoardController handles HTTP requests for the GitHub Projects v2 board
// owned by a project's repo. Renamed from the original BoardController on
// fold-in so the BFF-side BoardController (which aggregates ComponentTask
// rows on top) keeps its name.
type RepoBoardController interface {
	GetBoard(w http.ResponseWriter, r *http.Request)
	MoveIssueToStatus(w http.ResponseWriter, r *http.Request)
}

type repoBoardController struct {
	service services.RepoBoardService
}

func NewRepoBoardController(service services.RepoBoardService) RepoBoardController {
	return &repoBoardController{service: service}
}

func (c *repoBoardController) GetBoard(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")

	result, err := c.service.GetBoard(r.Context(), projectID)
	if err != nil {
		slog.ErrorContext(r.Context(), "get board failed", "error", err, "project", projectID)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to get board")
		return
	}

	utils.WriteSuccessResponse(w, http.StatusOK, result)
}

func (c *repoBoardController) MoveIssueToStatus(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("projectId")

	var req struct {
		IssueURL     string `json:"issueUrl"`
		TargetStatus string `json:"targetStatus"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IssueURL == "" || req.TargetStatus == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "issueUrl and targetStatus are required")
		return
	}

	if err := c.service.MoveIssueToStatus(r.Context(), projectID, req.IssueURL, req.TargetStatus); err != nil {
		slog.ErrorContext(r.Context(), "move board item failed", "error", err, "project", projectID, "issueUrl", req.IssueURL)
		utils.WriteErrorResponse(w, http.StatusInternalServerError, "failed to move board item")
		return
	}

	utils.WriteSuccessResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}
