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

// errors.go — what an answer of ae-studio-tools means to aep-api: the typed
// errors callers branch on, and the one mapping from status + problem code.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/wso2/aep/aep-api/internal/sourcecontrol"
)

// ErrTurnInProgress: a different turn runs for the project (409
// turn_in_progress). Retry after it ends.
var ErrTurnInProgress = errors.New("ae studio: a different turn runs for the project")

// Failure codes the pod ends a turn with (the result line's code) that a
// caller branches on. The rest (agent-error, output_truncated, internal, ...)
// are the turn's own answer.
const (
	// TurnCodeShutdown: the pod shut down while the turn ran.
	TurnCodeShutdown = "shutdown"
	// TurnCodeStreamDied: the turn's stream inside the pod broke off.
	TurnCodeStreamDied = "stream-died"
	// TurnCodeProviderLimit: the model provider's rate or spend limit stopped
	// the turn.
	TurnCodeProviderLimit = "provider_limit"
)

// TurnFailedError is a turn the pod ended `failed`. It carries the result's
// code, and on a provider_limit the reset time the provider stated (zero when
// it stated none). The result's free-text message is the agent's, possibly a
// provider's error body, so the adapter never reads it and it cannot reach an
// error string, a log line or Temporal history. Whether to start the
// turn again, and when, is the caller's policy; IsPermanent does not judge it.
type TurnFailedError struct {
	Code    string
	ResetAt time.Time
}

func (e *TurnFailedError) Error() string {
	code := e.Code
	if code == "" {
		code = "no code"
	}
	return "ae studio: the turn failed (" + code + ")"
}

// Interrupted reports a turn that did not run to its own end (the pod shut
// down, or its stream died): a new turn can succeed.
func (e *TurnFailedError) Interrupted() bool {
	return e.Code == TurnCodeShutdown || e.Code == TurnCodeStreamDied
}

// ProviderLimited reports a turn the model provider's limit stopped: a new
// turn can succeed once the limit resets.
func (e *TurnFailedError) ProviderLimited() bool { return e.Code == TurnCodeProviderLimit }

// StatusError is an answer the adapter has no typed error for: the HTTP
// status and, when the pod sent a problem, its code and detail.
type StatusError struct {
	Op     string
	Status int
	Code   string
	Detail string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("ae studio: %s answered %d", e.Op, e.Status)
	if e.Code != "" {
		msg += " " + e.Code
	}
	if e.Detail != "" {
		msg += ": " + e.Detail
	}
	return msg
}

// Permanent reports a refusal the same request cannot change: any 4xx except
// 408 and 429 (the pod has no IsPermanent of its own; sourcecontrol.IsPermanent
// asks this).
func (e *StatusError) Permanent() bool {
	return e.Status >= 400 && e.Status < 500 &&
		e.Status != http.StatusRequestTimeout && e.Status != http.StatusTooManyRequests
}

// problemBodyLimit bounds how much of an error answer is read.
const problemBodyLimit = 64 << 10

// answer is a non-2xx reply of op, read and closed: its status and, when the
// body is a JSON object with a code, the problem's fields callers branch on.
type answer struct {
	op           string
	status       int
	problem      bool // the body is a JSON object with a code
	code, detail string
	githubStatus int                      // github_error: GitHub's status, 0 when it named none
	activeTurnID string                   // 409 turn_in_progress
	conflicts    []sourcecontrol.Conflict // 409 conflict on create-commit
	retryAfter   time.Duration            // 429: the Retry-After header
}

// readAnswer reads and closes a non-2xx response of op.
func readAnswer(resp *http.Response, op string) answer {
	defer func() { _ = resp.Body.Close() }()
	a := answer{op: op, status: resp.StatusCode, retryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mt != "application/problem+json" && mt != "application/json" {
		return a
	}
	var body struct {
		Code         string `json:"code"`
		Detail       string `json:"detail"`
		GithubStatus int    `json:"githubStatus"`
		ActiveTurnID string `json:"activeTurnId"`
		Conflicts    []struct {
			Path       string `json:"path"`
			BaseSha    string `json:"baseSha"`
			CurrentSha string `json:"currentSha"`
		} `json:"conflicts"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, problemBodyLimit)).Decode(&body) != nil || body.Code == "" {
		return a
	}
	a.problem, a.code, a.detail, a.githubStatus, a.activeTurnID = true, body.Code, body.Detail, body.GithubStatus, body.ActiveTurnID
	for _, c := range body.Conflicts {
		a.conflicts = append(a.conflicts, sourcecontrol.Conflict{Path: c.Path, BaseSHA: c.BaseSha, CurrentSHA: c.CurrentSha})
	}
	return a
}

// retryAfter reads a Retry-After of whole seconds (the pod's form); anything
// else is zero, "not said".
func retryAfter(v string) time.Duration {
	s, err := strconv.Atoi(v)
	if err != nil || s <= 0 {
		return 0
	}
	return time.Duration(s) * time.Second
}

// authRefused is a refusal of the AE-only token itself: a 401, or a 403 the
// pod's auth layer wrote (no problem body, or org_mismatch). Any other 403 is
// the pod's verdict on the request (owner_not_allowed), never a token fault.
func (a answer) authRefused() bool {
	switch a.status {
	case http.StatusUnauthorized:
		return true
	case http.StatusForbidden:
		return !a.problem || a.code == "org_mismatch"
	}
	return false
}

// gatewayGone is an answer no pod handler wrote (no problem body) on a
// status that means the route or its backend is gone: the cached endpoint
// may be stale.
func (a answer) gatewayGone() bool {
	if a.problem {
		return false
	}
	switch a.status {
	case http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// codeSentinels are the pod's problem codes that name one port error.
var codeSentinels = map[string]error{
	"project_unknown":     sourcecontrol.ErrRepoNotFound,
	"ref_not_found":       sourcecontrol.ErrRefNotFound,
	"path_not_found":      sourcecontrol.ErrPathNotFound,
	"issue_not_found":     sourcecontrol.ErrIssueNotFound,
	"milestone_not_found": sourcecontrol.ErrMilestoneNotFound,
	"tag_exists":          sourcecontrol.ErrTagAlreadyExists,
	"not_fast_forward":    sourcecontrol.ErrRefNotFastForward,
	"repo_name_conflict":  sourcecontrol.ErrRepoNameConflict,
	"owner_not_allowed":   sourcecontrol.ErrOwnerNotAllowed,
	"disk_full":           sourcecontrol.ErrAEStudioUnavailable,
	"aep_api_unavailable": sourcecontrol.ErrAEStudioUnavailable,
}

// refReadOps are the reads whose only input the pod's request validator can
// refuse, past the owner and repo validRef already checks, is the ref (`at`,
// with `local`). A 400 validation_failed there is a refused ref; on any other
// op the same code may be about something else (a bundle filter, a path, a
// commit body), so it stays a plain StatusError.
var refReadOps = map[string]bool{"get-head": true, "list-tree": true}

// errorFromProblem maps a non-auth refusal to the error callers branch on:
// the code first (one code is one error), then the status (any 503, or a
// gateway's answer, is the pod not serving), else a StatusError.
func errorFromProblem(a answer) error {
	if a.status == http.StatusBadRequest && a.code == "validation_failed" && refReadOps[a.op] {
		// Still the StatusError too, so a caller asking for the pod's answer
		// (or IsPermanent) reads it as before.
		return fmt.Errorf("%w: %w", sourcecontrol.ErrRefInvalid,
			&StatusError{Op: a.op, Status: a.status, Code: a.code, Detail: a.detail})
	}
	if sentinel, ok := codeSentinels[a.code]; ok {
		return fmt.Errorf("%w (ae studio: %s answered %d %s)", sentinel, a.op, a.status, a.code)
	}
	switch {
	case a.code == "turn_in_progress":
		return fmt.Errorf("%w (active turn %s)", ErrTurnInProgress, a.activeTurnID)
	case a.code == "reference_rejected":
		return fmt.Errorf("%w: %s", sourcecontrol.ErrReferenceRejected, a.detail)
	case a.code == "conflict":
		return &sourcecontrol.CommitConflictError{Conflicts: a.conflicts}
	case a.code == "github_rate_limited":
		return &sourcecontrol.RateLimitedError{RetryAfter: a.retryAfter}
	case a.code == "github_error":
		status := a.githubStatus
		if status == 0 {
			status = http.StatusBadGateway
		}
		return &sourcecontrol.HTTPStatusError{StatusCode: status, Body: a.detail, URL: "ae-studio " + a.op}
	case a.status == http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %s answered 503 %s", sourcecontrol.ErrAEStudioUnavailable, a.op, a.code)
	case a.gatewayGone():
		return fmt.Errorf("%w: %s answered %d from the gateway", sourcecontrol.ErrAEStudioUnavailable, a.op, a.status)
	}
	return &StatusError{Op: a.op, Status: a.status, Code: a.code, Detail: a.detail}
}
