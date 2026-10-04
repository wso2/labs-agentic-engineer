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
	"net/http"
	"strings"

	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

// The owner guard (Q-8): every /internal/v1/repos/{owner}/{repo}/… operation
// acts on a repository of the org's connected GitHub account (AE_GITHUB_OWNER)
// only. One middleware applies it, behind the gate and the validator (a
// malformed owner is the validator's 400) and ahead of every operation, so
// nothing is read from or written to a foreign owner's repository (a
// multipart upload's parts are not read either: the validator skips them).
// Its code, owner_not_allowed, is a verdict on the request: aep-api must not
// read this 403 as a refused token.

// reposScope is the address prefix of the repository-scoped operations.
const reposScope = internalV1 + "/repos/"

// ownerGuard answers 403 owner_not_allowed for a repository-scoped request
// whose owner is not connected's; every other request goes to next.
func ownerGuard(connected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if owner, scoped := repoOwner(r); scoped && !connectedOwner(connected, owner) {
			problem.Write(w, http.StatusForbidden, "owner_not_allowed", "the repository's owner is not the org's connected GitHub account")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// repoOwner is the {owner} segment of a /internal/v1/repos/{owner}/…
// address; scoped is false for any other address (POST /internal/v1/repos
// guards its body's owner itself). The validator already held the segment to
// the contract's pattern, which admits no escapes.
func repoOwner(r *http.Request) (owner string, scoped bool) {
	rest, ok := strings.CutPrefix(r.URL.EscapedPath(), reposScope)
	if !ok {
		return "", false
	}
	owner, _, scoped = strings.Cut(rest, "/")
	return owner, scoped
}

// connectedOwner reports whether owner is the org's connected GitHub
// account. GitHub owner names are case-insensitive; an unset account matches
// nothing.
func connectedOwner(connected, owner string) bool {
	return connected != "" && strings.EqualFold(connected, owner)
}
