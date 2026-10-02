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
	v1gen "github.com/wso2/aep/ae-studio-tools/internal/gen/v1"
	"github.com/wso2/aep/ae-studio-tools/internal/platform"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
	"github.com/wso2/aep/ae-studio-tools/internal/projects"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// aepAPIRetryAfter is the Retry-After (seconds) on aep_api_unavailable.
const aepAPIRetryAfter = "5"

// v1Server implements the /v1 Files operations (read-only) over files.Reader.
type v1Server struct {
	files files.Reader
}

var _ v1gen.StrictServerInterface = v1Server{}

// ListFiles lists the files at the branch tip under prefix.
func (s v1Server) ListFiles(ctx context.Context, req v1gen.ListFilesRequestObject) (v1gen.ListFilesResponseObject, error) {
	metas, err := s.files.List(ctx, req.ProjectName, req.Params.Prefix)
	if err != nil {
		return filesProblem(ctx, "list", req.ProjectName, err), nil
	}
	out := make(v1gen.ListFiles200JSONResponse, 0, len(metas))
	for _, m := range metas {
		out = append(out, v1gen.FileMeta{Path: m.Path, Sha: m.SHA, Size: m.Size})
	}
	return out, nil
}

// ReadFile reads one file at the tip or at a pinned commit.
func (s v1Server) ReadFile(ctx context.Context, req v1gen.ReadFileRequestObject) (v1gen.ReadFileResponseObject, error) {
	c, err := s.files.ReadAt(ctx, req.ProjectName, req.Path, req.Params.Ref)
	if err != nil {
		return filesProblem(ctx, "read", req.ProjectName, err), nil
	}
	return v1gen.ReadFile200JSONResponse{Path: c.Path, Content: c.Content, Sha: c.SHA}, nil
}

// ReadFileBundle reads every readable file under prefix at one commit.
func (s v1Server) ReadFileBundle(ctx context.Context, req v1gen.ReadFileBundleRequestObject) (v1gen.ReadFileBundleResponseObject, error) {
	b, err := s.files.Bundle(ctx, req.ProjectName, req.Params.Prefix, req.Params.Ref)
	if err != nil {
		return filesProblem(ctx, "bundle", req.ProjectName, err), nil
	}
	out := v1gen.ReadFileBundle200JSONResponse{CommitSha: b.CommitSHA, Files: make([]v1gen.FileContent, 0, len(b.Files))}
	for _, f := range b.Files {
		out.Files = append(out.Files, v1gen.FileContent{Path: f.Path, Content: f.Content, Sha: f.SHA})
	}
	return out, nil
}

// filesProblem maps a Files error to its problem response and logs what an
// operator needs. Resolver errors are logged by class only: their text can
// carry a URL (url.Error), and nothing token-shaped may reach a log line.
func filesProblem(ctx context.Context, op, project string, err error) problemResponse {
	switch {
	case errors.Is(err, files.ErrPathInvalid):
		return problemResponse{status: http.StatusBadRequest, code: "path_invalid", detail: "the path or ref is not readable through this API"}
	case errors.Is(err, files.ErrFileNotFound):
		return problemResponse{status: http.StatusNotFound, code: "path_not_found", detail: "no such file at this commit"}
	case errors.Is(err, repo.ErrRefNotFound):
		return problemResponse{status: http.StatusNotFound, code: "ref_not_found", detail: "the ref names no commit in this repository"}
	case errors.Is(err, projects.ErrUnknown):
		return problemResponse{status: http.StatusNotFound, code: "project_unknown", detail: "no such project in this org"}
	case errors.Is(err, projects.ErrMisconfigured):
		// Retrying will not help, so no Retry-After; the loud event is the
		// operator's signal that the publisher credentials are wrong.
		cause := "aep_api"
		if errors.Is(err, platform.ErrClientRejected) {
			cause = "token_endpoint"
		}
		slog.ErrorContext(ctx, "aep_api.auth_rejected", "op", op, "project", project, "cause", cause)
		return problemResponse{status: http.StatusServiceUnavailable, code: "aep_api_unavailable", detail: "aep-api could not resolve the project"}
	case errors.Is(err, projects.ErrUnavailable):
		slog.WarnContext(ctx, "aep_api.unavailable", "op", op, "project", project)
		return problemResponse{status: http.StatusServiceUnavailable, code: "aep_api_unavailable", detail: "aep-api could not resolve the project", retryAfter: aepAPIRetryAfter}
	case errors.Is(err, repo.ErrDiskFull):
		slog.WarnContext(ctx, "files.disk_full", "op", op, "project", project)
		return problemResponse{status: http.StatusServiceUnavailable, code: "disk_full", detail: "the studio's disk is full"}
	default:
		// A git failure: the clone or fetch from GitHub, or the local mirror.
		// Engine errors name the git command and its stderr, never a token
		// (askpass keeps it out of argv).
		slog.WarnContext(ctx, "files.git_failed", "op", op, "project", project, "error", err)
		return problemResponse{status: http.StatusBadGateway, code: "github_error", detail: "the repository could not be read"}
	}
}

// problemResponse is a problem+json answer for any /v1 Files operation. It
// stands in for the generated per-status types, which cannot carry
// Retry-After.
type problemResponse struct {
	status       int
	code, detail string
	retryAfter   string
}

func (p problemResponse) write(w http.ResponseWriter) error {
	if p.retryAfter != "" {
		w.Header().Set("Retry-After", p.retryAfter)
	}
	problem.Write(w, p.status, p.code, p.detail)
	return nil
}

func (p problemResponse) VisitListFilesResponse(w http.ResponseWriter) error { return p.write(w) }

func (p problemResponse) VisitReadFileResponse(w http.ResponseWriter) error { return p.write(w) }

func (p problemResponse) VisitReadFileBundleResponse(w http.ResponseWriter) error {
	return p.write(w)
}
