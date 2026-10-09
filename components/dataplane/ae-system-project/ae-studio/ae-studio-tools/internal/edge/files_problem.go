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
	"net/http"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	"github.com/wso2/aep/ae-studio-tools/internal/platform"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// The one error map of the Files operations, shared by /v1 (v1_files.go) and
// the Files socket (files_sock.go) so the two answer every class alike.

// aepAPIRetryAfter is the Retry-After (seconds) on aep_api_unavailable.
const aepAPIRetryAfter = "5"

// filesProblem maps a Files error to its problem response and logs what an
// operator needs. Resolver errors are logged by class only: their text can
// carry a URL (url.Error), and nothing token-shaped may reach a log line.
func filesProblem(ctx context.Context, op, project string, err error) problemResponse {
	switch {
	case errors.Is(err, files.ErrPathInvalid):
		// An apply's refusal of one path names it, so the Room can set that
		// path aside and save the rest.
		var refused *files.PathRefusedError
		path := ""
		if errors.As(err, &refused) {
			path = refused.Path
		}
		return problemResponse{status: http.StatusBadRequest, code: "path_invalid", detail: "the request names a path or ref this API refuses", path: path}
	case errors.Is(err, files.ErrFileNotFound):
		return problemResponse{status: http.StatusNotFound, code: "path_not_found", detail: "no such file at this commit"}
	case errors.Is(err, repo.ErrRefNotFound):
		return problemResponse{status: http.StatusNotFound, code: "ref_not_found", detail: "the ref names no commit in this repository"}
	case errors.Is(err, projects.ErrUnknown):
		return problemResponse{status: http.StatusNotFound, code: "project_unknown", detail: "no such project in this org"}
	case errors.Is(err, projects.ErrMisconfigured):
		// Retrying will not help, so no Retry-After; the loud event is the
		// operator's signal that the ae-studio client credentials are wrong.
		cause := "aep_api"
		if errors.Is(err, platform.ErrClientRejected) {
			cause = "token_endpoint"
		}
		slog.ErrorContext(ctx, "aep_api.auth_rejected", "op", op, "project", project, "cause", cause)
		return problemResponse{status: http.StatusServiceUnavailable, code: "aep_api_unavailable", detail: "aep-api could not resolve the project"}
	case errors.Is(err, projects.ErrUnavailable):
		slog.WarnContext(ctx, "aep_api.unavailable", "op", op, "project", project)
		return problemResponse{status: http.StatusServiceUnavailable, code: "aep_api_unavailable", detail: "aep-api could not resolve the project", retryAfter: aepAPIRetryAfter}
	case errors.Is(err, repo.ErrRefNotFastForward):
		// The push lost every CAS retry to concurrent writers; the caller
		// re-reads and retries.
		slog.WarnContext(ctx, "files.not_fast_forward", "op", op, "project", project)
		return problemResponse{status: http.StatusConflict, code: "not_fast_forward", detail: "the branch moved during the save; re-read and retry"}
	case errors.Is(err, repo.ErrDiskFull):
		slog.WarnContext(ctx, "files.disk_full", "op", op, "project", project)
		return problemResponse{status: http.StatusServiceUnavailable, code: "disk_full", detail: "the studio's disk is full"}
	default:
		// A git failure: the clone or fetch from GitHub, or the local mirror.
		// The engine's text names the git command and the clone URL, so the
		// line carries the repository and a class only.
		var re *files.RepoError
		repoName := ""
		if errors.As(err, &re) {
			repoName = re.Repo
		}
		slog.WarnContext(ctx, "files.git_failed", "op", op, "project", project, "repo", repoName, "class", repo.ErrorClass(err))
		return problemResponse{status: http.StatusBadGateway, code: "github_error", detail: "the repository could not be read"}
	}
}

// problemResponse is a problem+json answer for any Files operation, on /v1
// or the Files socket. It stands in for the generated per-status types,
// which cannot carry Retry-After.
type problemResponse struct {
	status       int
	code, detail string
	retryAfter   string
	// path is the one path an apply's write rules refused, or "".
	path string
}

func (p problemResponse) write(w http.ResponseWriter) error {
	if p.retryAfter != "" {
		w.Header().Set("Retry-After", p.retryAfter)
	}
	problem.WriteWithPath(w, p.status, p.code, p.detail, p.path)
	return nil
}
