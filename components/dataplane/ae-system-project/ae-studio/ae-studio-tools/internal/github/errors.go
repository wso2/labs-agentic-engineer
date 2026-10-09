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

package github

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Sentinels the client returns where GitHub's answer has a domain meaning.
var (
	// ErrIssueNotFound: GetIssue / ListIssueComments when the repo holds no
	// issue with that number (GitHub answered 404, or GraphQL a null issue).
	ErrIssueNotFound = errors.New("issue not found")
	// ErrMilestoneNotFound: the milestone reads when no milestone carries
	// that number.
	ErrMilestoneNotFound = errors.New("milestone not found")
	// ErrRepoNameConflict: CreateOrgRepo when the name is already taken.
	ErrRepoNameConflict = errors.New("repo name already taken")
)

// HTTPStatusError is every non-2xx answer GitHub gives that the client does
// not map to a sentinel. Body is GitHub's answer, truncated; it never holds
// the token, and Error() leaves it out so a logged error carries no GitHub
// content. A rate limit (a 429, or a 403 carrying Retry-After or
// X-RateLimit-Remaining: 0) is normalised to StatusCode 429 with RetryAfter
// set, so callers branch on one shape.
type HTTPStatusError struct {
	StatusCode int
	Body       string
	URL        string
	// RetryAfter is how long to wait; set only on a rate limit.
	RetryAfter time.Duration
}

func (e *HTTPStatusError) Error() string {
	return fmt.Sprintf("github API %s: status %d", e.URL, e.StatusCode)
}

// IsHTTPStatus reports whether err is an *HTTPStatusError with that code.
func IsHTTPStatus(err error, code int) bool {
	var he *HTTPStatusError
	return errors.As(err, &he) && he.StatusCode == code
}

// RateLimited reports whether err is GitHub's rate limit, and for how long to
// wait.
func RateLimited(err error) (retryAfter time.Duration, ok bool) {
	var he *HTTPStatusError
	if errors.As(err, &he) && he.StatusCode == 429 {
		return he.RetryAfter, true
	}
	return 0, false
}

// ErrorAttrs names a failed GitHub call for a log line by class, and GitHub's
// status when it answered, never by the error's text (which names the
// request URL): rate_limited, status (with status), canceled or transport.
func ErrorAttrs(err error) []any {
	var se *HTTPStatusError
	if _, limited := RateLimited(err); limited {
		return []any{"class", "rate_limited"}
	}
	switch {
	case errors.As(err, &se):
		return []any{"class", "status", "status", se.StatusCode}
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return []any{"class", "canceled"}
	default:
		return []any{"class", "transport"}
	}
}

// GraphQLError carries the errors[] of a GraphQL response, which answers 200
// with a populated errors[] rather than an HTTP status. The whole array is
// kept because the machine-readable Type is what callers branch on
// (NOT_FOUND, RATE_LIMITED).
type GraphQLError struct {
	Errors []GraphQLErrorDetail
	// Query is the operation that failed, for debug logging at the call site.
	Query string
}

// GraphQLErrorDetail is one entry of a GraphQL response's errors[]. Path
// elements are field names or list indices, hence any.
type GraphQLErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Path    []any  `json:"path"`
}

func (e *GraphQLError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, d := range e.Errors {
		if d.Type != "" {
			msgs = append(msgs, d.Type+": "+d.Message)
			continue
		}
		msgs = append(msgs, d.Message)
	}
	return "github graphql error: " + strings.Join(msgs, "; ")
}

// IsGraphQLType reports whether err is a *GraphQLError carrying at least one
// error of the given type (e.g. "NOT_FOUND").
func IsGraphQLType(err error, typ string) bool {
	var ge *GraphQLError
	if !errors.As(err, &ge) {
		return false
	}
	for _, d := range ge.Errors {
		if d.Type == typ {
			return true
		}
	}
	return false
}
