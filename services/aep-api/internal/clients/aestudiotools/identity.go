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

// identity.go — get-github-identity: the GitHub user the org's gitpat
// belongs to, as the pod sees it.

import (
	"context"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// GitHubIdentity answers the GitHub user of the org's gitpat (name and email
// only when public). A GitHub rate limit is a *RateLimitedError, any other
// GitHub failure an *HTTPStatusError with GitHub's status.
func (a *Adapter) GitHubIdentity(ctx context.Context, org string) (*sourcecontrol.GitHubUser, error) {
	var body gen.GitHubIdentity
	err := a.do(ctx, org, "get-github-identity", &body, func(ctx context.Context, c *gen.Client, impersonateOrg string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.GetGithubIdentity(ctx, &gen.GetGithubIdentityParams{XImpersonateOrg: impersonateOrg}, auth)
	})
	if err != nil {
		return nil, err
	}
	return &sourcecontrol.GitHubUser{Login: body.Login, ID: body.ID, Name: body.Name, Email: body.Email}, nil
}
