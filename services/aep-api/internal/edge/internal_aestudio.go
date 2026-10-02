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

package edge

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/organization/aestudio"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// The AE Studio route group (/internal/v1/ae-studio/…): an org's AE Studio
// tools pod resolves a project to its GitHub repository on every request
// (04 §2), and has the dependency stubs of a save completed here (04 §4), so
// the registry read and the fetch of a model-chosen URL never run in the pod
// that holds the org's git credential. internalGate admits only the org's publisher client token here and
// binds its ouHandle as the org; the org is always read from the context,
// never the request. A project the org does not own is a 404, the same as one
// that does not exist.

// ProjectRepositoryLookup resolves an org's project to its repository
// (aestudio.ProjectRepositories).
type ProjectRepositoryLookup interface {
	Lookup(ctx context.Context, org, project string) (aestudio.ProjectRepository, error)
}

// DependencyCompleter completes an org's dependency stub writes
// (spec.CompleteDependencies bound to the org registry and the guarded URL
// fetch).
type DependencyCompleter func(ctx context.Context, org string, writes []spec.WriteOp) (map[string]spec.CompletedFile, []spec.Warning)

// codePathInvalid is the Error code for a completions write whose path is not
// a dependency definition.
const codePathInvalid = "path_invalid"

// authenticateAEStudio verifies authHeader as an org's publisher client token
// (aud aep-publisher-<org>, ouHandle == <org>) and binds that org. A nil
// verifier, or any other token (a user JWT, the AE-only client, an
// ae-studio-<org> client), is a 401.
func authenticateAEStudio(ctx context.Context, verifier *auth.PublisherTokenVerifier, authHeader string) (context.Context, error) {
	const prefix = "Bearer "
	if len(authHeader) <= len(prefix) || !strings.EqualFold(authHeader[:len(prefix)], prefix) {
		return nil, errUnauthorized("publisher client token required")
	}
	claims, err := verifier.Verify(authHeader[len(prefix):]) // nil verifier: error, fails closed
	if err != nil {
		slog.WarnContext(ctx, "ae-studio internal op: bearer rejected", "error", err)
		return nil, errUnauthorized("publisher client token required")
	}
	return tenant.WithBoundOrg(ctx, claims.OrgHandle), nil
}

func (s *internalServer) GetAeStudioProjectRepository(ctx context.Context, request igen.GetAeStudioProjectRepositoryRequestObject) (igen.GetAeStudioProjectRepositoryResponseObject, error) {
	if s.deps.AEStudioRepositories == nil {
		return nil, errServiceUnavailable("project repository lookup not configured")
	}
	org := tenant.BoundOrgFromContext(ctx)
	repo, err := s.deps.AEStudioRepositories.Lookup(ctx, org, request.ProjectName)
	if errors.Is(err, aestudio.ErrProjectNotFound) {
		return nil, errNotFound("project not found")
	}
	if err != nil {
		slog.ErrorContext(ctx, "ae-studio project repository lookup failed", "org", org, "project", request.ProjectName, "error", err)
		return nil, errInternal("failed to resolve project repository")
	}
	return igen.GetAeStudioProjectRepository200JSONResponse{
		Owner:         repo.Owner,
		Repo:          repo.Repo,
		DefaultBranch: repo.DefaultBranch,
		CloneURL:      repo.CloneURL,
	}, nil
}

func (s *internalServer) CompleteAeStudioDependencies(ctx context.Context, request igen.CompleteAeStudioDependenciesRequestObject) (igen.CompleteAeStudioDependenciesResponseObject, error) {
	if s.deps.DependencyCompleter == nil {
		return nil, errServiceUnavailable("dependency completion not configured")
	}
	if request.Body == nil {
		return nil, apierr.BadRequest("request body required")
	}
	writes := make([]spec.WriteOp, 0, len(request.Body.Writes))
	seen := map[string]bool{}
	for _, w := range request.Body.Writes {
		if !spec.IsDependencyFilePath(w.Path) {
			return nil, apierr.New(http.StatusBadRequest, codePathInvalid,
				"not a dependency definition path (specs/design/dependencies/<name>/dependency.json): "+w.Path, nil)
		}
		if seen[w.Path] {
			return nil, apierr.New(http.StatusBadRequest, codePathInvalid, "path appears more than once: "+w.Path, nil)
		}
		seen[w.Path] = true
		writes = append(writes, spec.WriteOp{Path: w.Path, Content: w.Content})
	}
	completed, warnings := s.deps.DependencyCompleter(ctx, tenant.BoundOrgFromContext(ctx), writes)
	return igen.CompleteAeStudioDependencies200JSONResponse(toIgenCompletions(completed, warnings)), nil
}

// toIgenCompletions projects the completer's result onto the wire, in path
// order so the body is deterministic.
func toIgenCompletions(completed map[string]spec.CompletedFile, warnings []spec.Warning) igen.AEStudioDependencyCompletions {
	out := igen.AEStudioDependencyCompletions{
		Completed: make([]igen.AEStudioCompletedDependency, 0, len(completed)),
		Warnings:  make([]igen.AEStudioWarning, 0, len(warnings)),
	}
	for _, p := range slices.Sorted(maps.Keys(completed)) {
		c := completed[p]
		files := make([]igen.AEStudioFile, 0, len(c.Files))
		for _, fp := range slices.Sorted(maps.Keys(c.Files)) {
			files = append(files, igen.AEStudioFile{Path: fp, Content: c.Files[fp]})
		}
		out.Completed = append(out.Completed, igen.AEStudioCompletedDependency{Path: p, Definition: c.Definition, Files: files})
	}
	for _, w := range warnings {
		out.Warnings = append(out.Warnings, igen.AEStudioWarning{Path: w.Path, Code: w.Code, Message: w.Message})
	}
	return out
}
