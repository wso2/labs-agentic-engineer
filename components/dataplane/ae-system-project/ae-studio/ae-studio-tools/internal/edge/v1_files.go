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
	"net/http"

	"github.com/wso2/aep/ae-studio-tools/internal/files"
	v1gen "github.com/wso2/aep/ae-studio-tools/internal/gen/v1"
)

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

func (p problemResponse) VisitListFilesResponse(w http.ResponseWriter) error { return p.write(w) }

func (p problemResponse) VisitReadFileResponse(w http.ResponseWriter) error { return p.write(w) }

func (p problemResponse) VisitReadFileBundleResponse(w http.ResponseWriter) error {
	return p.write(w)
}
