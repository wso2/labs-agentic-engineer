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

package repo

import (
	"context"
	"net/http"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
)

// TrashRepo is POST /internal/v1/trash: owner/repo's mirror and its stored
// reference documents move into trash/ (the engine's TrashRepo and
// TrashReferences; the reaper purges trash), so a deleted project leaves
// neither a clone nor its documents behind. The owner must be the connected
// account (the edge's path guard cannot see a body), else 403
// owner_not_allowed and nothing moves. GitHub is not touched; nothing
// stored is success. There is no org-wide form (05 §7).
func (h Handler) TrashRepo(ctx context.Context, req gen.TrashRepoRequestObject) (gen.TrashRepoResponseObject, error) {
	owner, name := req.Body.Owner, req.Body.Repo
	if h.owner == "" || !strings.EqualFold(h.owner, owner) {
		return newProblemReply(http.StatusForbidden, "owner_not_allowed", "the repository's owner is not the org's connected GitHub account"), nil
	}
	ref, err := h.ref(owner, name, "")
	if err == nil {
		err = h.ws.TrashRepo(ctx, ref)
	}
	if err == nil {
		err = h.ws.TrashReferences(ctx, OwnerRepo{Owner: owner, Repo: name})
	}
	if err == nil {
		return gen.TrashRepo204Response{}, nil
	}
	return h.problem(ctx, "trash-repo", owner, name, err)
}

// VisitTrashRepoResponse implements gen.TrashRepoResponseObject.
func (p problemReply) VisitTrashRepoResponse(w http.ResponseWriter) error { return p.write(w) }
