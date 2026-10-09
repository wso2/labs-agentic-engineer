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

	"github.com/wso2/aep/ae-studio-tools/internal/gen/mcpsock"
)

// The MCP socket's snapshot operations (design/route-groups.md): the agent's
// project lookup and its skills lookup. Each resolves through aep-api on
// every call and writes the snapshots the agent then reads from its
// read-only /snapshots mount. Errors share the Files error map
// (filesProblem): project_unknown and ref_not_found 404, path_invalid 400,
// aep_api_unavailable and disk_full 503, any other git failure github_error
// 502.

// LookupProject resolves the project, writes its snapshot at `at` (the tip
// when omitted) with the stored references overlaid, and the Org skills
// snapshot, and answers both shas, the reference names and the descriptor's
// idea (omitted when there is none).
func (s mcpSocketServer) LookupProject(ctx context.Context, req mcpsock.LookupProjectRequestObject) (mcpsock.LookupProjectResponseObject, error) {
	snap, err := s.snapshots.Snapshot(ctx, req.ProjectName, req.Params.At)
	if err != nil {
		return filesProblem(ctx, "lookup-project", req.ProjectName, err), nil
	}
	return mcpsock.LookupProject200JSONResponse{
		Known:      true,
		HeadSha:    snap.HeadSHA,
		SkillsSha:  snap.SkillsSHA,
		References: snap.References,
		Idea:       snap.Idea,
	}, nil
}

// GetSkills writes the Org skills snapshot at the skills repository's tip
// and answers its sha.
func (s mcpSocketServer) GetSkills(ctx context.Context, _ mcpsock.GetSkillsRequestObject) (mcpsock.GetSkillsResponseObject, error) {
	sha, err := s.snapshots.SkillsSnapshot(ctx)
	if err != nil {
		return filesProblem(ctx, "get-skills", "", err), nil
	}
	return mcpsock.GetSkills200JSONResponse{SkillsSha: sha}, nil
}

// VisitGetSkillsResponse lets a Files problem answer GET /skills
// (VisitLookupProjectResponse is shared with the Files socket's lookup).
func (p problemResponse) VisitGetSkillsResponse(w http.ResponseWriter) error { return p.write(w) }
