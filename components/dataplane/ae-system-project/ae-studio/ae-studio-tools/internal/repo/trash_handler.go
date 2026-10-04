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

	"github.com/wso2/aep/ae-studio-tools/internal/gen"
)

// TrashRepo is POST /internal/v1/trash: owner/repo's mirror moves into
// trash/ (the engine's TrashRepo; the reaper purges it), so a deleted
// project leaves no clone behind. GitHub is not touched; no mirror is
// success. There is no org-wide form (05 §7).
func (h Handler) TrashRepo(ctx context.Context, req gen.TrashRepoRequestObject) (gen.TrashRepoResponseObject, error) {
	ref, err := h.ref(req.Body.Owner, req.Body.Repo, "")
	if err == nil {
		if err = h.ws.TrashRepo(ctx, ref); err == nil {
			return gen.TrashRepo204Response{}, nil
		}
	}
	return h.problem(ctx, "trash-repo", req.Body.Owner, req.Body.Repo, err)
}

// VisitTrashRepoResponse implements gen.TrashRepoResponseObject.
func (p problemReply) VisitTrashRepoResponse(w http.ResponseWriter) error { return p.write(w) }
