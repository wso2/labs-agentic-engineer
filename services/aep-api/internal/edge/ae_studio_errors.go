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
	"errors"
	"math"
	"net/http"

	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// aeStudioUnavailableRetryAfter is the Retry-After (seconds) on 503
// ae_studio_unavailable: the pod is coming up, provisioning or rolling.
const aeStudioUnavailableRetryAfter = 5

// classifyAEStudio is the ONE map from the AE Studio sentinels
// (sourcecontrol.ErrAEStudio*, ErrOwnerNotAllowed, *RateLimitedError) to the
// envelope. Slices keep the sentinel in the error chain (returning it, or
// apierr.WithCause on their fallback) and the edge speaks for it here, so
// every git-backed op answers the same status, code and Retry-After. nil when
// err carries none of them.
//
//	ErrAEStudioAbsent         409 github_not_connected       (a person connects GitHub)
//	ErrAEStudioUnavailable    503 ae_studio_unavailable      Retry-After: 5
//	ErrAEStudioMisconfigured  503 ae_studio_misconfigured    no Retry-After: an operator fixes it (C3)
//	ErrOwnerNotAllowed        409 owner_not_allowed          the repo is not under the connected account
//	*RateLimitedError         429 github_rate_limited        Retry-After: what GitHub said, when it said
func classifyAEStudio(err error) *apierr.Error {
	var rl *sourcecontrol.RateLimitedError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, sourcecontrol.ErrAEStudioAbsent):
		return &apierr.Error{Status: http.StatusConflict, Code: apierr.CodeGitHubNotConnected,
			Message: "GitHub is not connected for this organization — connect GitHub to continue"}
	case errors.Is(err, sourcecontrol.ErrAEStudioUnavailable):
		return &apierr.Error{Status: http.StatusServiceUnavailable, Code: apierr.CodeAEStudioUnavailable,
			Message: "AE Studio is not ready — try again in a few seconds", RetryAfter: aeStudioUnavailableRetryAfter}
	case errors.Is(err, sourcecontrol.ErrAEStudioMisconfigured):
		return &apierr.Error{Status: http.StatusServiceUnavailable, Code: apierr.CodeAEStudioMisconfigured,
			Message: "AE Studio is not configured on this platform — contact your platform admin"}
	case errors.Is(err, sourcecontrol.ErrOwnerNotAllowed):
		return &apierr.Error{Status: http.StatusConflict, Code: apierr.CodeOwnerNotAllowed,
			Message: "the project's repository is not under the organization's connected GitHub account"}
	case errors.As(err, &rl):
		return &apierr.Error{Status: http.StatusTooManyRequests, Code: apierr.CodeGitHubRateLimited,
			Message: "GitHub's rate limit was reached — try again later", RetryAfter: int(math.Ceil(rl.RetryAfter.Seconds()))}
	}
	return nil
}
