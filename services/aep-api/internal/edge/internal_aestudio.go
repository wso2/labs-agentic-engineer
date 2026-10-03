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
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"math"
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

// maxCompletionsAnswerBytes bounds the encoded completions of one answer. The
// tools pod reads at most 32 MiB of it (maxCompletionsBody,
// ae-studio-tools/internal/files/completions.go) and fails the whole decode
// past that, which would degrade every stub, the completed ones included; the
// margin is the warnings' room. Raise both together.
const maxCompletionsAnswerBytes = 24 << 20

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

// lookupAEStudioProject resolves one of org's projects to its repository.
// A project org does not own is a 404 with no data, the same as an unknown one.
func (s *internalServer) lookupAEStudioProject(ctx context.Context, org, project string) (aestudio.ProjectRepository, error) {
	if s.deps.AEStudioRepositories == nil {
		return aestudio.ProjectRepository{}, errServiceUnavailable("project repository lookup not configured")
	}
	repo, err := s.deps.AEStudioRepositories.Lookup(ctx, org, project)
	if errors.Is(err, aestudio.ErrProjectNotFound) {
		return aestudio.ProjectRepository{}, errNotFound("project not found")
	}
	if err != nil {
		slog.ErrorContext(ctx, "ae-studio project repository lookup failed", "org", org, "project", project, "error", err)
		return aestudio.ProjectRepository{}, errInternal("failed to resolve project repository")
	}
	return repo, nil
}

func (s *internalServer) GetAeStudioProjectRepository(ctx context.Context, request igen.GetAeStudioProjectRepositoryRequestObject) (igen.GetAeStudioProjectRepositoryResponseObject, error) {
	repo, err := s.lookupAEStudioProject(ctx, tenant.BoundOrgFromContext(ctx), request.ProjectName)
	if err != nil {
		return nil, err
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
	org := tenant.BoundOrgFromContext(ctx)
	// The project gates the call: its repository is not needed here, but a
	// project outside the token's org gets nothing completed.
	if _, err := s.lookupAEStudioProject(ctx, org, request.Body.Project); err != nil {
		return nil, err
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
	completed, warnings := s.deps.DependencyCompleter(ctx, org, writes)
	out := toIgenCompletions(completed, warnings, maxCompletionsAnswerBytes)
	if left := len(completed) - len(out.Completed); left > 0 {
		slog.WarnContext(ctx, "ae-studio completions over budget", "org", org, "left_out", left)
	}
	return igen.CompleteAeStudioDependencies200JSONResponse(out), nil
}

// toIgenCompletions projects the completer's result onto the wire, in path
// order so the body is deterministic, encoding at most budget bytes of
// completions. A completion that would go past it is left out, never
// truncated: its stub lands as written, and its success warning becomes the
// kind's "not completed" one (the dependency reads needs-input or
// needs-contract until a later save completes it).
func toIgenCompletions(completed map[string]spec.CompletedFile, warnings []spec.Warning, budget int) igen.AEStudioDependencyCompletions {
	out := igen.AEStudioDependencyCompletions{
		Completed: make([]igen.AEStudioCompletedDependency, 0, len(completed)),
		Warnings:  make([]igen.AEStudioWarning, 0, len(warnings)),
	}
	leftOut := map[string]bool{}
	used := len("[]")
	for _, p := range slices.Sorted(maps.Keys(completed)) {
		c := completed[p]
		files := make([]igen.AEStudioFile, 0, len(c.Files))
		for _, fp := range slices.Sorted(maps.Keys(c.Files)) {
			files = append(files, igen.AEStudioFile{Path: fp, Content: c.Files[fp]})
		}
		dep := igen.AEStudioCompletedDependency{Path: p, Definition: c.Definition, Files: files}
		size := encodedSize(dep) + len(",")
		if used+size > budget {
			leftOut[p] = true
			continue
		}
		used += size
		out.Completed = append(out.Completed, dep)
	}
	for _, w := range warnings {
		if leftOut[w.Path] {
			w = notCompletedWarning(w)
		}
		out.Warnings = append(out.Warnings, igen.AEStudioWarning{Path: w.Path, Code: w.Code, Message: w.Message})
	}
	return out
}

// encodedSize is v's length as JSON; a value that does not encode counts as
// over any budget.
func encodedSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return math.MaxInt / 2
	}
	return len(b)
}

// notCompletedWarning turns the success warning of a completion that was
// left out of the answer into its kind's "not completed" warning; any other
// warning passes through.
func notCompletedWarning(w spec.Warning) spec.Warning {
	const tooLarge = "this completion is too large to return with the others in this save"
	switch w.Code {
	case spec.WarningRegistryCopied:
		return spec.Warning{Path: w.Path, Code: spec.WarningRegistryUnreachable,
			Message: tooLarge + ", so the registered resource was not copied here; the dependency reads needs-input until a save that carries fewer dependencies completes it"}
	case spec.WarningProviderDocumentFetched:
		return spec.Warning{Path: w.Path, Code: spec.WarningProviderDocumentUnavailable,
			Message: tooLarge + ", so the provider's document did not land; the dependency reads needs-contract until a save that carries fewer dependencies completes it"}
	default:
		return w
	}
}

// RecordTurnUsage is replaced by Task 3.16, which owns the turn ledger. Until
// then the route is served by the contract and answers 503.
func (s *internalServer) RecordTurnUsage(_ context.Context, _ igen.RecordTurnUsageRequestObject) (igen.RecordTurnUsageResponseObject, error) {
	return nil, errServiceUnavailable("turn usage recording not configured")
}
