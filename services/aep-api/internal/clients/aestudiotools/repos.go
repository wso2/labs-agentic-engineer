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

package aestudiotools

// repos.go — repository lifecycle on the pod: create (or adopt) the GitHub
// repository, and trash the pod's mirror and reference store of one.

import (
	"context"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// CreateOrgRepo creates ref.Repo under ref.Owner (the org's connected GitHub
// account) and answers its clone URL; req.Name is not read (the ref names
// the repository) and the pod always initialises it. A taken name is
// ErrRepoNameConflict unless req.AdoptExisting, which answers the existing
// repository.
//
//deadcode:keep wired in Task 4.13 (GitHub REST callers move to the adapter)
func (a *Adapter) CreateOrgRepo(ctx context.Context, ref RepoRef, req sourcecontrol.CreateOrgRepoRequest) (string, error) {
	if err := validRef(ref); err != nil {
		return "", err
	}
	body := gen.CreateRepoRequest{Owner: ref.Owner, Name: ref.Repo, Private: req.Private, Description: req.Description, AdoptExisting: req.AdoptExisting}
	var reply gen.RepoCoordinates
	err := a.do(ctx, ref.Org, "create-repo", &reply, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.CreateRepo(ctx, &gen.CreateRepoParams{XImpersonateOrg: org}, body, auth)
	})
	return reply.CloneURL, err
}

// TrashRepo moves the pod's mirror and reference store of ref to its trash;
// a repository the pod holds nothing of is success.
//
//deadcode:keep wired in Task 4.18 (project delete trashes through TrashOps)
func (a *Adapter) TrashRepo(ctx context.Context, ref RepoRef) error {
	if err := validRef(ref); err != nil {
		return err
	}
	return a.do(ctx, ref.Org, "trash-repo", nil, func(ctx context.Context, c *gen.Client, org string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.TrashRepo(ctx, &gen.TrashRepoParams{XImpersonateOrg: org}, gen.TrashRepoRequest{Owner: ref.Owner, Repo: ref.Repo}, auth)
	})
}
