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

// Package projects resolves an AE Studio project, and the org's skills
// library, to its GitHub repository.
// The answer comes from aep-api on every call and is never cached in the pod
// (ticket 04 §2): a project moved or removed in AEP is seen on the next call,
// and an answer for one call is never reused for another.
package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/aepapi"
	"github.com/wso2/aep/ae-studio-tools/internal/platform"
)

// maxBody bounds what is read from aep-api's answer.
const maxBody = 64 << 10

// Repository is where a project's code lives.
type Repository struct{ Owner, Repo, DefaultBranch, CloneURL string }

var (
	// ErrUnknown means aep-api does not know the project in this pod's org.
	// It is a denial: the caller answers project_unknown (404) and runs no
	// git operation.
	ErrUnknown = errors.New("project unknown")
	// ErrUnavailable means aep-api could not answer (unreachable, 5xx, an
	// auth failure, or an unusable body). The caller answers
	// aep_api_unavailable (503).
	ErrUnavailable = errors.New("aep-api unavailable")
	// ErrMisconfigured marks an ErrUnavailable caused by the pod's own
	// credentials: the token endpoint rejected the publisher client, or
	// aep-api still answered 401/403 after a fresh token. The wire answer is
	// still aep_api_unavailable; callers check this to log one loud,
	// value-free event, since retrying will not help.
	ErrMisconfigured = errors.New("publisher credentials rejected")
)

// Resolver maps a project name to its repository, and the org to its skills
// repository.
type Resolver interface {
	Resolve(ctx context.Context, project string) (Repository, error)
	// ResolveSkills answers the org's skills repository (aep-api reconciles
	// the org's skills library first). It is never ErrUnknown: an org with no
	// skills repository is ErrUnavailable, as a turn cannot run without it.
	ResolveSkills(ctx context.Context) (Repository, error)
}

// NewAEPAPIResolver resolves through aep-api's
// GET /internal/v1/ae-studio/projects/{projectName}/repository and
// GET /internal/v1/ae-studio/skills/repository. c carries the org's publisher
// token (platform.NewAEPAPI), which scopes the answer to the pod's org. It
// takes the raw-op interface: the decision is the HTTP status, never a parsed
// error body (Q-2).
func NewAEPAPIResolver(c aepapi.ClientInterface) Resolver {
	return &aepAPIResolver{c: c}
}

type aepAPIResolver struct{ c aepapi.ClientInterface }

// Resolve maps by HTTP status and never reads an error body for a decision
// (Q-2): 200 → the repository, 404 → ErrUnknown, 401/403 (after the
// transport's one retry) or a rejected publisher client → ErrUnavailable and
// ErrMisconfigured, anything else or a transport failure → ErrUnavailable.
func (r *aepAPIResolver) Resolve(ctx context.Context, project string) (Repository, error) {
	resp, err := r.c.GetAeStudioProjectRepository(ctx, project)
	rep, err := repositoryAnswer(resp, err)
	if errors.Is(err, errNotFound) {
		return Repository{}, fmt.Errorf("%w: %q", ErrUnknown, project)
	}
	return rep, err
}

// ResolveSkills is Resolve's mapping for the org's skills repository, except
// that a 404 (the org has none) is ErrUnavailable.
func (r *aepAPIResolver) ResolveSkills(ctx context.Context) (Repository, error) {
	resp, err := r.c.GetAeStudioSkillsRepository(ctx)
	rep, err := repositoryAnswer(resp, err)
	if errors.Is(err, errNotFound) {
		return Repository{}, fmt.Errorf("%w: aep-api has no skills repository for the org", ErrUnavailable)
	}
	return rep, err
}

// errNotFound marks aep-api's 404 for the caller to name.
var errNotFound = errors.New("aep-api answered 404")

// repositoryAnswer maps one repository lookup's response by status: 200 →
// the repository (every field set), 404 → errNotFound, 401/403 or a rejected
// publisher client → ErrUnavailable and ErrMisconfigured, anything else or a
// transport failure → ErrUnavailable.
func repositoryAnswer(resp *http.Response, err error) (Repository, error) {
	if err != nil {
		if errors.Is(err, platform.ErrClientRejected) {
			return Repository{}, fmt.Errorf("%w: %w: %w", ErrUnavailable, ErrMisconfigured, err)
		}
		return Repository{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Repository{}, errNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return Repository{}, fmt.Errorf("%w: %w: aep-api answered %d", ErrUnavailable, ErrMisconfigured, resp.StatusCode)
	default:
		return Repository{}, fmt.Errorf("%w: aep-api answered %d", ErrUnavailable, resp.StatusCode)
	}
	var body aepapi.AEStudioProjectRepository
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return Repository{}, fmt.Errorf("%w: unreadable repository answer", ErrUnavailable)
	}
	if body.Owner == "" || body.Repo == "" || body.DefaultBranch == "" || body.CloneURL == "" {
		return Repository{}, fmt.Errorf("%w: repository answer with an empty field", ErrUnavailable)
	}
	return Repository{Owner: body.Owner, Repo: body.Repo, DefaultBranch: body.DefaultBranch, CloneURL: body.CloneURL}, nil
}
