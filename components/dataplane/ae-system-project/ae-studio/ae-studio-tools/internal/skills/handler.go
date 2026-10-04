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

package skills

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// Handler serves POST /internal/v1/repos/{owner}/{repo}/skills-mirror
// (mirror-skills). The edge embeds it in its /internal/v1 server; the gate,
// the body cap, the request validator and the path owner guard ran before
// it. Errors map through repo.Problem, as the git content ops do.
type Handler struct {
	mirror Mirror
	// cloneURL addresses an owner/repo's origin (repo.GitHubCloneURL).
	cloneURL func(owner, name string) string
	// owner is the org's connected GitHub account (AE_GITHUB_OWNER), the
	// only owner the body's skillsRepo may name (the edge's guard sees only
	// the path); empty refuses every request.
	owner string
}

// NewHandler serves m, cloning owner/repo from cloneURL, for the connected
// account owner.
func NewHandler(m Mirror, cloneURL func(owner, name string) string, owner string) Handler {
	return Handler{mirror: m, cloneURL: cloneURL, owner: owner}
}

// MirrorSkills mirrors the body's skills repository into owner/repo.
func (h Handler) MirrorSkills(ctx context.Context, req gen.MirrorSkillsRequestObject) (gen.MirrorSkillsResponseObject, error) {
	if req.Body == nil {
		return problemReply(repo.NewProblem(http.StatusBadRequest, "validation_failed", "the request does not match the contract")), nil
	}
	sk := req.Body.SkillsRepo
	if h.owner == "" || !strings.EqualFold(h.owner, sk.Owner) {
		return problemReply(repo.NewProblem(http.StatusForbidden, "owner_not_allowed", "the skills repository's owner is not the org's connected GitHub account")), nil
	}
	project, err := repo.NewRepoRef(req.Owner, req.Repo, req.Params.DefaultBranch, h.cloneURL)
	var skillsRef repo.RepoRef
	if err == nil {
		skillsRef, err = repo.NewRepoRef(sk.Owner, sk.Repo, sk.DefaultBranch, h.cloneURL)
	}
	if err == nil {
		var res repo.CommitResult
		if res, err = h.mirror.Mirror(ctx, project, skillsRef, req.Body.Pinned); err == nil {
			return gen.MirrorSkills200JSONResponse{CommitSha: res.CommitSHA, Changed: res.Changed}, nil
		}
	}
	// A library read failure names the skills repository, anything else the
	// project.
	owner, name := req.Owner, req.Repo
	if errors.Is(err, errCatalogUnreadable) {
		owner, name = sk.Owner, sk.Repo
	}
	p, err := repo.Problem(ctx, "mirror-skills", owner, name, err)
	if err != nil {
		return nil, err
	}
	return problemReply(p), nil
}

// problemReply is a problem answer to mirror-skills.
type problemReply gen.Problem

// VisitMirrorSkillsResponse implements gen.MirrorSkillsResponseObject.
func (p problemReply) VisitMirrorSkillsResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	return json.NewEncoder(w).Encode(gen.Problem(p))
}
