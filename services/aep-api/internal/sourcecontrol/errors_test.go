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

package sourcecontrol_test

// IsPermanent decides whether a caller keeps asking. The table below is the
// whole contract, and the NEGATIVE half carries as much weight as the positive
// one: classifying a secondary rate limit as permanent would fail runs GitHub
// was only asking us to slow down for.

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func TestIsPermanent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil is not a failure at all", err: nil, want: false},

		{name: "missing repo row", err: sourcecontrol.ErrRepoNotFound, want: true},
		{name: "missing issue", err: sourcecontrol.ErrIssueNotFound, want: true},
		{name: "missing milestone", err: sourcecontrol.ErrMilestoneNotFound, want: true},
		{
			name: "wrapped sentinel — the supervisor sees these through a port",
			err:  fmt.Errorf("poll milestone: %w", sourcecontrol.ErrRepoNotFound),
			want: true,
		},

		{
			name: "404 — the repository is gone for this credential",
			err:  &sourcecontrol.HTTPStatusError{StatusCode: http.StatusNotFound},
			want: true,
		},
		{
			name: "410 gone",
			err:  &sourcecontrol.HTTPStatusError{StatusCode: http.StatusGone},
			want: true,
		},
		{
			name: "401 — the credential was rejected, and tokens are minted per request",
			err:  &sourcecontrol.HTTPStatusError{StatusCode: http.StatusUnauthorized},
			want: true,
		},
		{
			name: "graphql NOT_FOUND — the 404 of the GraphQL surface",
			err: &sourcecontrol.GraphQLError{Errors: []sourcecontrol.GraphQLErrorDetail{{
				Type:    sourcecontrol.GraphQLTypeNotFound,
				Message: "Could not resolve to a Repository with the name 'org/proj'.",
			}}},
			want: true,
		},

		{
			name: "403 — GitHub's secondary rate limit wears this status and clears itself",
			err:  &sourcecontrol.HTTPStatusError{StatusCode: http.StatusForbidden},
			want: false,
		},
		{
			name: "500 is the blip retries exist for",
			err:  &sourcecontrol.HTTPStatusError{StatusCode: http.StatusInternalServerError},
			want: false,
		},
		{
			name: "graphql RATE_LIMITED, same reason as 403",
			err: &sourcecontrol.GraphQLError{Errors: []sourcecontrol.GraphQLErrorDetail{{
				Type: "RATE_LIMITED", Message: "API rate limit exceeded",
			}}},
			want: false,
		},
		{
			name: "repo not ready is a state the mirror heals out of",
			err:  sourcecontrol.ErrRepoNotReady,
			want: false,
		},
		{name: "transport failure", err: errors.New("dial tcp: connection refused"), want: false},

		{name: "AE Studio absent: GitHub is not connected, an answer", err: fmt.Errorf("head: %w", sourcecontrol.ErrAEStudioAbsent), want: true},
		{name: "AE-only token refused after a refresh (C3)", err: sourcecontrol.ErrAEStudioMisconfigured, want: true},
		{name: "repository owner is not the org's", err: sourcecontrol.ErrOwnerNotAllowed, want: true},
		{name: "reference document rejected", err: sourcecontrol.ErrReferenceRejected, want: true},
		{name: "AE Studio unavailable is the retry case", err: sourcecontrol.ErrAEStudioUnavailable, want: false},
		{name: "GitHub rate limit clears on its own", err: &sourcecontrol.RateLimitedError{RetryAfter: time.Minute}, want: false},
		{name: "GitHub refused the request itself (400)", err: &sourcecontrol.HTTPStatusError{StatusCode: http.StatusBadRequest}, want: true},
		{name: "GitHub's validation refused it (422)", err: &sourcecontrol.HTTPStatusError{StatusCode: http.StatusUnprocessableEntity}, want: true},
		{name: "405 not mergeable is reconciled by its caller", err: &sourcecontrol.HTTPStatusError{StatusCode: http.StatusMethodNotAllowed}, want: false},
		{name: "an adapter refusal that calls itself permanent", err: fmt.Errorf("x: %w", selfClassified(true)), want: true},
		{name: "an adapter refusal that calls itself transient", err: selfClassified(false), want: false},
		{
			name: "a commit conflict is re-read and retried by its caller",
			err:  &sourcecontrol.CommitConflictError{Conflicts: []sourcecontrol.Conflict{{Path: "a.md"}}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, sourcecontrol.IsPermanent(tt.err))
		})
	}
}

// selfClassified is an adapter error that says whether it is permanent.
type selfClassified bool

func (s selfClassified) Error() string   { return "adapter refusal" }
func (s selfClassified) Permanent() bool { return bool(s) }
