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

// Package projects resolves an AE Studio project to its GitHub repository.
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
	// auth failure, or an unusable body). It is transient: the caller
	// answers aep_api_unavailable (503).
	ErrUnavailable = errors.New("aep-api unavailable")
)

// Resolver maps a project name to its repository.
type Resolver interface {
	Resolve(ctx context.Context, project string) (Repository, error)
}

// NewAEPAPIResolver resolves through aep-api's
// GET /internal/v1/ae-studio/projects/{projectName}/repository. c carries the
// org's publisher token (platform.NewAEPAPI), which scopes the answer to the
// pod's org.
//
//deadcode:keep wired in Task 2.7
func NewAEPAPIResolver(c *aepapi.ClientWithResponses) Resolver {
	return &aepAPIResolver{c: c}
}

type aepAPIResolver struct{ c *aepapi.ClientWithResponses }

// Resolve maps by HTTP status and never reads an error body for a decision
// (Q-2): 200 → the repository, 404 → ErrUnknown, anything else or a
// transport failure → ErrUnavailable.
//
//deadcode:keep wired in Task 2.7
func (r *aepAPIResolver) Resolve(ctx context.Context, project string) (Repository, error) {
	resp, err := r.c.GetAeStudioProjectRepository(ctx, project)
	if err != nil {
		return Repository{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Repository{}, fmt.Errorf("%w: %q", ErrUnknown, project)
	default:
		return Repository{}, fmt.Errorf("%w: aep-api answered %d", ErrUnavailable, resp.StatusCode)
	}
	var body aepapi.AEStudioProjectRepository
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&body); err != nil {
		return Repository{}, fmt.Errorf("%w: unreadable repository answer", ErrUnavailable)
	}
	if body.Owner == "" || body.Repo == "" {
		return Repository{}, fmt.Errorf("%w: repository answer without owner or repo", ErrUnavailable)
	}
	return Repository{Owner: body.Owner, Repo: body.Repo, DefaultBranch: body.DefaultBranch, CloneURL: body.CloneURL}, nil
}
