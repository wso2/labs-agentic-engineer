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
	"strings"

	"github.com/wso2/aep/aep-api/internal/igen"
	"github.com/wso2/aep/aep-api/internal/organization/aestudio"
	"github.com/wso2/aep/aep-api/internal/platform/auth"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
)

// The AE Studio route group (/internal/v1/ae-studio/…): an org's AE Studio
// tools pod resolves a project to its GitHub repository on every request
// (04 §2). internalGate admits only the org's publisher client token here and
// binds its ouHandle as the org; the org is always read from the context,
// never the request. A project the org does not own is a 404, the same as one
// that does not exist.

// ProjectRepositoryLookup resolves an org's project to its repository
// (aestudio.ProjectRepositories).
type ProjectRepositoryLookup interface {
	Lookup(ctx context.Context, org, project string) (aestudio.ProjectRepository, error)
}

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
