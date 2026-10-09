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

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

func TestIsPermanent(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{nil, false},
		{sourcecontrol.ErrAEStudioMisconfigured, true},
		{fmt.Errorf("wrapped: %w", sourcecontrol.ErrAEStudioMisconfigured), true},
		{sourcecontrol.ErrReferenceRejected, true},
		{&StatusError{Status: 400, Code: "validation_failed"}, true},
		{&StatusError{Status: 404, Code: "project_unknown"}, true},
		{&StatusError{Status: 409, Code: "no_default_key"}, true},
		{&StatusError{Status: 413, Code: "payload_too_large"}, true},
		{&StatusError{Status: 408}, false},
		{&StatusError{Status: 429, Code: "github_rate_limited"}, false},
		{&StatusError{Status: 502, Code: "agent_error"}, false},
		{sourcecontrol.ErrAEStudioUnavailable, false},
		{sourcecontrol.ErrAEStudioAbsent, true},
		{sourcecontrol.ErrOwnerNotAllowed, true},
		{&sourcecontrol.HTTPStatusError{StatusCode: 404}, true},
		{ErrTurnInProgress, false},
		{context.Canceled, false},
		{errors.New("other"), false},
	} {
		if got := sourcecontrol.IsPermanent(tc.err); got != tc.want {
			t.Errorf("sourcecontrol.IsPermanent(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

// A failed turn's error names its code and nothing else: the pod's free-text
// message (possibly a provider's error body) never reaches an error string,
// so it never reaches Temporal history or a run record.
func TestTurnFailedError_ClassifiesByCodeAndCarriesNoMessage(t *testing.T) {
	for _, tc := range []struct {
		code                         string
		interrupted, providerLimited bool
	}{
		{TurnCodeShutdown, true, false},
		{TurnCodeStreamDied, true, false},
		{TurnCodeProviderLimit, false, true},
		{"agent-error", false, false},
		{"output_truncated", false, false},
		{"internal", false, false},
		{"", false, false},
	} {
		e := &TurnFailedError{Code: tc.code}
		if e.Interrupted() != tc.interrupted || e.ProviderLimited() != tc.providerLimited {
			t.Errorf("%q: interrupted=%v providerLimited=%v, want %v %v", tc.code, e.Interrupted(), e.ProviderLimited(), tc.interrupted, tc.providerLimited)
		}
		if tc.code != "" && !strings.Contains(e.Error(), tc.code) {
			t.Errorf("%q: Error() = %q, want the code in it", tc.code, e.Error())
		}
		if sourcecontrol.IsPermanent(e) {
			t.Errorf("%q: IsPermanent must not classify a turn failure; the caller's retry policy does", tc.code)
		}
	}
}

// problemFor is the pod's problem answer carrying code, at the status the
// pod sends it with.
func problemFor(code string) answer {
	status := map[string]int{
		"project_unknown": 404, "ref_not_found": 404, "path_not_found": 404, "issue_not_found": 404,
		"milestone_not_found": 404, "tag_exists": 409, "not_fast_forward": 409, "repo_name_conflict": 409,
		"conflict": 409, "reference_rejected": 400, "owner_not_allowed": 403, "turn_in_progress": 409,
		"github_rate_limited": 429, "disk_full": 503, "aep_api_unavailable": 503,
	}[code]
	return answer{op: "op", status: status, problem: true, code: code}
}

// problem502 is a github_error naming GitHub's status.
func problem502(githubStatus int) answer {
	return answer{op: "op", status: 502, problem: true, code: "github_error", githubStatus: githubStatus}
}

func TestErrors_EveryCodeMapsToItsSentinel(t *testing.T) {
	for code, want := range map[string]error{
		"ref_not_found": sourcecontrol.ErrRefNotFound, "path_not_found": sourcecontrol.ErrPathNotFound,
		"issue_not_found": sourcecontrol.ErrIssueNotFound, "milestone_not_found": sourcecontrol.ErrMilestoneNotFound,
		"tag_exists": sourcecontrol.ErrTagAlreadyExists, "not_fast_forward": sourcecontrol.ErrRefNotFastForward,
		"repo_name_conflict": sourcecontrol.ErrRepoNameConflict, "conflict": sourcecontrol.ErrCommitConflict,
		"reference_rejected": sourcecontrol.ErrReferenceRejected, "owner_not_allowed": sourcecontrol.ErrOwnerNotAllowed,
		"project_unknown": sourcecontrol.ErrRepoNotFound, "disk_full": sourcecontrol.ErrAEStudioUnavailable,
		"aep_api_unavailable": sourcecontrol.ErrAEStudioUnavailable,
	} {
		if got := errorFromProblem(problemFor(code)); !errors.Is(got, want) {
			t.Errorf("%s → %v, want %v", code, got, want)
		}
	}
	if got := errorFromProblem(problem502(404)); !sourcecontrol.IsHTTPStatus(got, 404) || !sourcecontrol.IsPermanent(got) {
		t.Errorf("github_error 404 must be a permanent HTTPStatusError, got %v", got)
	}
}

func TestErrors_TypedAnswers(t *testing.T) {
	t.Run("turn_in_progress", func(t *testing.T) {
		if got := errorFromProblem(problemFor("turn_in_progress")); !errors.Is(got, ErrTurnInProgress) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("conflict carries the paths", func(t *testing.T) {
		a := problemFor("conflict")
		a.conflicts = []sourcecontrol.Conflict{{Path: "specs/a.md", BaseSHA: "x", CurrentSHA: "y"}}
		var cc *sourcecontrol.CommitConflictError
		if got := errorFromProblem(a); !errors.As(got, &cc) || len(cc.Conflicts) != 1 || cc.Conflicts[0].Path != "specs/a.md" || sourcecontrol.IsPermanent(got) {
			t.Fatalf("got %v, want a retryable CommitConflictError naming specs/a.md", got)
		}
	})
	t.Run("rate limit carries Retry-After", func(t *testing.T) {
		a := problemFor("github_rate_limited")
		a.retryAfter = 7 * time.Second
		var rl *sourcecontrol.RateLimitedError
		if got := errorFromProblem(a); !errors.As(got, &rl) || rl.RetryAfter != 7*time.Second || sourcecontrol.IsPermanent(got) {
			t.Fatalf("got %v, want a retryable RateLimitedError{7s}", got)
		}
	})
	t.Run("github_error without a status is a transient 502", func(t *testing.T) {
		if got := errorFromProblem(problem502(0)); !sourcecontrol.IsHTTPStatus(got, 502) || sourcecontrol.IsPermanent(got) {
			t.Fatalf("got %v", got)
		}
	})
	t.Run("github_error 4xx that GitHub keeps refusing is permanent", func(t *testing.T) {
		for _, s := range []int{400, 401, 404, 410, 422} {
			if got := errorFromProblem(problem502(s)); !sourcecontrol.IsPermanent(got) {
				t.Errorf("githubStatus %d: %v must be permanent", s, got)
			}
		}
		for _, s := range []int{403, 405, 409, 500} {
			if got := errorFromProblem(problem502(s)); sourcecontrol.IsPermanent(got) {
				t.Errorf("githubStatus %d: %v must stay retryable", s, got)
			}
		}
	})
	t.Run("reference_rejected keeps the detail", func(t *testing.T) {
		a := problemFor("reference_rejected")
		a.detail = "x.exe: type not allowed"
		if got := errorFromProblem(a); got.Error() != sourcecontrol.ErrReferenceRejected.Error()+": x.exe: type not allowed" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("a pod 4xx without a typed code is a permanent StatusError for sourcecontrol too", func(t *testing.T) {
		a := answer{op: "create-commit", status: 400, problem: true, code: "validation_failed"}
		var se *StatusError
		if got := errorFromProblem(a); !errors.As(got, &se) || se.Op != "create-commit" || !sourcecontrol.IsPermanent(got) {
			t.Fatalf("got %v, want a permanent StatusError", got)
		}
		if got := errorFromProblem(answer{op: "x", status: 502, problem: true, code: "agent_error"}); sourcecontrol.IsPermanent(got) {
			t.Fatalf("a 5xx StatusError must stay retryable: %v", got)
		}
	})
	// The pod's validator refusing a read addressed by `at` (get-head,
	// list-tree: the ref is the only input it can refuse there) is a refused
	// ref; the same code on any other op is not.
	t.Run("validation_failed on a ref read is ErrRefInvalid, still a StatusError", func(t *testing.T) {
		for _, op := range []string{"get-head", "list-tree"} {
			got := errorFromProblem(answer{op: op, status: 400, problem: true, code: "validation_failed"})
			var se *StatusError
			if !errors.Is(got, sourcecontrol.ErrRefInvalid) || !errors.As(got, &se) || se.Op != op || !sourcecontrol.IsPermanent(got) {
				t.Fatalf("%s: got %v, want ErrRefInvalid over a permanent StatusError", op, got)
			}
		}
		for _, op := range []string{"create-commit", "read-bundle", "read-file"} {
			if got := errorFromProblem(answer{op: op, status: 400, problem: true, code: "validation_failed"}); errors.Is(got, sourcecontrol.ErrRefInvalid) {
				t.Fatalf("%s: a 400 that is not a ref read must not be ErrRefInvalid: %v", op, got)
			}
		}
	})
	// trash-repo's local failure is the pod's own 500 trash_failed: a
	// retryable StatusError naming it, never a GitHub error.
	t.Run("trash_failed is the pod's own retryable refusal, not GitHub's", func(t *testing.T) {
		got := errorFromProblem(answer{op: "trash-repo", status: 500, problem: true, code: "trash_failed"})
		var se *StatusError
		var gh *sourcecontrol.HTTPStatusError
		if !errors.As(got, &se) || se.Code != "trash_failed" || se.Status != 500 || errors.As(got, &gh) ||
			sourcecontrol.IsPermanent(got) || errors.Is(got, sourcecontrol.ErrAEStudioUnavailable) {
			t.Fatalf("got %v, want a retryable StatusError trash_failed", got)
		}
	})
	t.Run("any 503 and a gateway answer are unavailable", func(t *testing.T) {
		for _, a := range []answer{{op: "x", status: 503, problem: true, code: "idp_unavailable"}, {op: "x", status: 502}, {op: "x", status: 404}} {
			if got := errorFromProblem(a); !errors.Is(got, sourcecontrol.ErrAEStudioUnavailable) {
				t.Errorf("%+v → %v, want unavailable", a, got)
			}
		}
	})
}

func TestAnswer_AuthRefused(t *testing.T) {
	for _, tc := range []struct {
		a    answer
		want bool
	}{
		{answer{status: 401}, true},
		{answer{status: 401, problem: true, code: "unauthorized"}, true},
		{answer{status: 403}, true},
		{answer{status: 403, problem: true, code: "org_mismatch"}, true},
		{answer{status: 403, problem: true, code: "owner_not_allowed"}, false},
		{answer{status: 403, problem: true, code: "forbidden"}, false},
		{answer{status: 404}, false},
	} {
		if got := tc.a.authRefused(); got != tc.want {
			t.Errorf("%+v: authRefused = %v, want %v", tc.a, got, tc.want)
		}
	}
}
