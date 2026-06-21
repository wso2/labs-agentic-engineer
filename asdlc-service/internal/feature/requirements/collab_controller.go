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

package requirements

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	jwtmw "github.com/wso2/asdlc/asdlc-service/middleware/jwt"
	"github.com/wso2/asdlc/asdlc-service/models"
	"github.com/wso2/asdlc/asdlc-service/utils"
)

// CollabController handles endpoints for real-time collaborative editing sessions.
type CollabController interface {
	GetCollabSession(w http.ResponseWriter, r *http.Request)
	ValidateCollabAccess(w http.ResponseWriter, r *http.Request)
}

// repoOracle is the §6.6g project-ownership oracle: a project's repo row
// exists (for the given org) iff the project was provisioned under that org.
// gitrepo.RepoService satisfies it; defined here (consumer side) so the
// requirements feature needn't import gitrepo concretely.
type repoOracle interface {
	GetRepo(ctx context.Context, orgID, projectID string) (*models.GitRepository, error)
}

type collabUserClaims struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
	jwt.RegisteredClaims
}

type collabController struct {
	repos repoOracle
}

func NewCollabController(repos repoOracle) CollabController {
	return &collabController{repos: repos}
}

func (c *collabController) GetCollabSession(w http.ResponseWriter, r *http.Request) {
	org := r.PathValue("orgHandle")
	project := r.PathValue("projectName")
	if !requireOrgHandle(w, org) || !requireProjectName(w, project) {
		return
	}

	// This route is org-scoped (the central tenant gate already verified the
	// caller's JWT org == {orgHandle}). Confirm the project exists under the
	// org via the repo oracle — 404 otherwise.
	if repo, err := c.repos.GetRepo(r.Context(), org, project); err != nil || repo == nil {
		slog.ErrorContext(r.Context(), "get project for collab session failed", "error", err, "org", org, "project", project)
		utils.WriteErrorResponse(w, http.StatusNotFound, "project not found")
		return
	}

	// Resolve the caller's display name from the JWT so the client can render
	// awareness/peer carets without needing to decode the ID token itself.
	name, email := parseDisplayIdentity(r.Header.Get("Authorization"))

	utils.WriteSuccessResponse(w, http.StatusOK, map[string]string{
		"roomId":   "spec-" + org + "-" + project,
		"wsUrl":    "/collab",
		"userName": name,
		"email":    email,
	})
}

func parseDisplayIdentity(authHeader string) (name, email string) {
	if authHeader == "" {
		return "", ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", ""
	}
	claims := &collabUserClaims{}
	// Signature is verified upstream (the gated route's jwt middleware /
	// the validate route's jwt wrap); this only extracts display fields.
	if _, _, err := jwt.NewParser().ParseUnverified(parts[1], claims); err != nil {
		return "", ""
	}
	name = claims.Name
	if name == "" {
		given := strings.TrimSpace(claims.GivenName)
		family := strings.TrimSpace(claims.FamilyName)
		if strings.EqualFold(family, "user") {
			family = ""
		}
		name = strings.TrimSpace(given + " " + family)
	}
	if name == "" {
		name, _ = claims.GetSubject()
	}
	return name, claims.Email
}

// ValidateCollabAccess is the server-to-server endpoint the collab-server calls
// (forwarding the end-user's Bearer + an X-Room-Id header) to authorize a user
// for a collaboration room. The route is wrapped with the standard jwt
// middleware so the Bearer signature is verified BEFORE this handler runs
// (forged/unsigned/expired tokens never reach here); this handler then enforces
// that the verified caller's org owns the room's project (the §6.6g oracle).
//
// Room IDs are `spec-<orgHandle>-<projectName>`. Because the verified org is
// known from the token, the room prefix `spec-<org>-` is stripped
// unambiguously to recover the project, sidestepping the hyphen-ambiguity of a
// blind 3-way split.
func (c *collabController) ValidateCollabAccess(w http.ResponseWriter, r *http.Request) {
	claims := jwtmw.ClaimsFromContext(r.Context())
	org := jwtmw.ResolveOuHandle(claims)
	if claims == nil || org == "" {
		utils.WriteErrorResponse(w, http.StatusUnauthorized, "invalid or missing token")
		return
	}

	roomID := strings.TrimSpace(r.Header.Get("X-Room-Id"))
	if roomID == "" {
		utils.WriteErrorResponse(w, http.StatusBadRequest, "missing X-Room-Id")
		return
	}
	// The room must belong to the verified caller's org.
	prefix := "spec-" + org + "-"
	if !strings.HasPrefix(roomID, prefix) {
		slog.WarnContext(r.Context(), "collab validate denied: room not in caller org",
			"org", org, "room", roomID)
		utils.WriteErrorResponse(w, http.StatusForbidden, "forbidden")
		return
	}
	project := strings.TrimPrefix(roomID, prefix)
	if project == "" {
		utils.WriteErrorResponse(w, http.StatusForbidden, "forbidden")
		return
	}
	// Project-ownership oracle: the project's repo row must exist for this org.
	if repo, err := c.repos.GetRepo(r.Context(), org, project); err != nil || repo == nil {
		slog.WarnContext(r.Context(), "collab validate denied: project not found for org",
			"org", org, "project", project, "error", err)
		utils.WriteErrorResponse(w, http.StatusForbidden, "forbidden")
		return
	}

	name, email := parseDisplayIdentity(r.Header.Get("Authorization"))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
		"name":  name,
		"email": email,
	})
}
