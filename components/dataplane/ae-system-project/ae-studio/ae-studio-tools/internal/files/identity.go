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

// Moved from services/aep-api/internal/sourcecontrol/git_ops_service.go
// (ResolveSaveIdentities); the aep-api copy is deleted in phase 4.

package files

import (
	"context"
	"errors"
	"log/slog"

	"github.com/wso2/aep/ae-studio-tools/internal/github"
	"github.com/wso2/aep/ae-studio-tools/internal/repo"
)

// IdentitySource names the gitpat user a save is committed as.
// github.CommitAuthor satisfies it (GET /user, cached; name falls back to
// the login, email to <login>@users.noreply.github.com).
type IdentitySource interface {
	Identity(ctx context.Context) (name, email string, err error)
}

// saveIdentities returns the author and committer of a save: both the gitpat
// user, as two distinct values so a caller changing one never aliases the
// other. A failed lookup does not gate the save (20 §5): both come back nil
// and the engine commits as its default AEP identity.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func (a Applier) saveIdentities(ctx context.Context) (author, committer *repo.GitIdentity) {
	name, email, err := a.Identity.Identity(ctx)
	if err != nil {
		slog.WarnContext(ctx, "files.identity_unavailable", identityErrorAttrs(err)...)
		return nil, nil
	}
	return &repo.GitIdentity{Name: name, Email: email}, &repo.GitIdentity{Name: name, Email: email}
}

// identityErrorAttrs names a failed identity lookup by class (and GitHub's
// status when it answered), never by the error's text.
//
//deadcode:keep wired in Task 2.9 (the Files socket's apply op)
func identityErrorAttrs(err error) []any {
	var rl *github.ErrRateLimited
	var se *github.StatusError
	switch {
	case errors.As(err, &rl):
		return []any{"class", "rate_limited"}
	case errors.As(err, &se):
		return []any{"class", "status", "status", se.Status}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return []any{"class", "canceled"}
	default:
		return []any{"class", "transport"}
	}
}
