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

package files

import (
	"context"
	"errors"

	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Handler serves the files feature's one remaining operation, the reference
// documents upload. The spec file reads and writes now run in the per-org
// AE Studio pod. The operation is org-scoped: the tenant gate bound the token
// org before it runs.
type Handler struct {
	files   spec.FilesService
	kickoff kickoffStarter
}

// kickoffStarter fires a project's opening `/start` turn (#562). The
// references upload is the SECOND of its two triggers: a create that declared
// documents were coming holds the kickoff, because they are the primary brief
// and an interview run before they land is conducted blind. *spec.Service
// satisfies it; the port is declared here so the slice keeps no genai edge.
// Nil is a documented no-op.
type kickoffStarter interface {
	Kickoff(ctx context.Context, orgID, projectID string)
}

// New returns the slice's handler.
func New(files spec.FilesService) *Handler {
	return &Handler{files: files}
}

// WithKickoffStarter wires the held kickoff the references upload releases.
// Chained rather than added to New: every other caller of this handler is
// unrelated to project creation, and a fourth positional dependency on the
// constructor would say otherwise.
func (h *Handler) WithKickoffStarter(k kickoffStarter) *Handler {
	h.kickoff = k
	return h
}

// mapFilesError maps the files service's typed errors onto the envelope —
// the strict-server port of the feature's Huma-era mapper.
func mapFilesError(err error) error {
	switch {
	case errors.Is(err, spec.ErrProjectRepoNotFound):
		return apierr.NotFound("project repository not found")
	case errors.Is(err, spec.ErrFileNotFound):
		return apierr.NotFound("file not found")
	case errors.Is(err, spec.ErrPathInvalid):
		return apierr.BadRequest(err.Error())
	case errors.Is(err, sourcecontrol.ErrRefNotFastForward):
		// Workspace.Mutate exhausted its CAS retries: the ref tip moved under
		// us on every attempt. That is a concurrent-write conflict, not a
		// server fault — surface it as a retryable 409, never a 500.
		return apierr.Conflict("the repository changed during the write; retry")
	case errors.Is(err, gitfs.ErrDiskAdmission):
		return apierr.ServiceUnavailable("workspace disk is full — try again in a few minutes, or contact your platform admin")
	default:
		return apierr.Internal("internal error")
	}
}
