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

package sourcecontrol

import (
	"context"
	"log/slog"

	"github.com/wso2/aep/aep-api/internal/platform/secrets"
)

// save_identity.go — who aep-api's own commits and tags are made by: the
// org credential's identity (falling back to "AEP" <noreply@aep.dev>), as
// author and committer of every commit and tagger of every tag. One
// decorator over the Git port sets it, so the writers name no credential.

// WithSaveIdentity is git with every Commit and Tag that names no identity
// stamped with the org credential's. When the credential cannot be resolved
// the request goes out unstamped and the pod authors it as the org's GitHub
// user (the same account), logged as git.identity_unresolved.
func WithSaveIdentity(git Git, creds secrets.Resolver) Git {
	return saveIdentityGit{Git: git, creds: creds}
}

type saveIdentityGit struct {
	Git
	creds secrets.Resolver
}

func (g saveIdentityGit) Commit(ctx context.Context, ref RepoRef, req CommitRequest) (CommitResult, error) {
	if req.Author == nil {
		if id := g.identity(ctx, ref.Org, "commit"); id != nil {
			author := *id
			req.Author = &author
			if req.Committer == nil {
				committer := *id
				req.Committer = &committer
			}
		}
	}
	return g.Git.Commit(ctx, ref, req)
}

func (g saveIdentityGit) Tag(ctx context.Context, ref RepoRef, spec TagSpec) error {
	if spec.Tagger == nil {
		spec.Tagger = g.identity(ctx, ref.Org, "tag")
	}
	return g.Git.Tag(ctx, ref, spec)
}

// identity is the org credential's save identity, nil when it cannot be
// resolved.
func (g saveIdentityGit) identity(ctx context.Context, org, op string) *GitIdentity {
	if g.creds == nil {
		return nil
	}
	cred, err := g.creds.Resolve(ctx, org)
	if err != nil {
		slog.WarnContext(ctx, "git.identity_unresolved", "org", org, "op", op)
		return nil
	}
	return SaveIdentity(cred)
}

// SaveIdentity is cred's identity as a git identity, with the AEP default
// name and email filling whatever the credential does not carry.
func SaveIdentity(cred secrets.Credential) *GitIdentity {
	id := cred.Identity()
	if id.Name == "" {
		id.Name = "AEP"
	}
	if id.Email == "" {
		id.Email = "noreply@aep.dev"
	}
	return &GitIdentity{Name: id.Name, Email: id.Email}
}
