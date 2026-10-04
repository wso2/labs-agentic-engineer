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
	"time"
)

var (
	// ErrAEStudioAbsent: the org has no AE Studio (no GitHub token yet).
	ErrAEStudioAbsent = errors.New("ae studio: absent for the org")
	// ErrAEStudioUnavailable: the org's AE Studio is not serving right now
	// (provisioning, failed, unreachable, out of disk, its IdP down). Retry.
	ErrAEStudioUnavailable = errors.New("ae studio: unavailable")
	// ErrAEStudioMisconfigured: aep-api's own AE-only client cannot call the
	// pod, its credentials are missing or refused (C3, C5). Permanent: no
	// retry fixes it, an operator must.
	ErrAEStudioMisconfigured = errors.New("ae studio: aep-api's AE-only client is misconfigured")
	// ErrTurnInProgress: a different turn runs for the project (409
	// turn_in_progress). Retry after it ends.
	ErrTurnInProgress = errors.New("ae studio: a different turn runs for the project")
	// ErrReferenceRejected: the pod refused a reference file (400
	// reference_rejected); the error text carries the pod's detail.
	ErrReferenceRejected = errors.New("ae studio: reference rejected")
)

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
// error string, a log line or Temporal history (R1-M4). Whether to start the
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

// IsPermanent reports whether retrying the same call cannot succeed: a
// misconfigured AE-only client, a refused reference, or any other 4xx
// except 408 and 429. Temporal activities return such errors non-retryable.
func IsPermanent(err error) bool {
	if errors.Is(err, ErrAEStudioMisconfigured) || errors.Is(err, ErrReferenceRejected) {
		return true
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status >= 400 && se.Status < 500 &&
			se.Status != http.StatusRequestTimeout && se.Status != http.StatusTooManyRequests
	}
	return false
}

// problemBodyLimit bounds how much of an error answer is read.
const problemBodyLimit = 64 << 10

// answer is a non-2xx reply, read and closed: its status and, when the body
// is a JSON object with a code, the code, detail and (409 turn_in_progress)
// the running turn's id.
type answer struct {
	status       int
	problem      bool // the body is a JSON object with a code
	code, detail string
	activeTurnID string
}

// readAnswer reads and closes a non-2xx response.
func readAnswer(resp *http.Response) answer {
	defer func() { _ = resp.Body.Close() }()
	a := answer{status: resp.StatusCode}
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mt != "application/problem+json" && mt != "application/json" {
		return a
	}
	var body struct {
		Code         string `json:"code"`
		Detail       string `json:"detail"`
		ActiveTurnID string `json:"activeTurnId"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, problemBodyLimit)).Decode(&body) == nil && body.Code != "" {
		a.problem, a.code, a.detail, a.activeTurnID = true, body.Code, body.Detail, body.ActiveTurnID
	}
	return a
}

// authRefused is a refusal of the AE-only token itself: a 401, or a 403 that
// is not the pod's owner_not_allowed verdict on the request.
func (a answer) authRefused() bool {
	return a.status == http.StatusUnauthorized || (a.status == http.StatusForbidden && a.code != "owner_not_allowed")
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

// toError maps a non-auth refusal of op to the error callers branch on.
func (a answer) toError(op string) error {
	switch {
	case a.status == http.StatusConflict && a.code == "turn_in_progress":
		return fmt.Errorf("%w (active turn %s)", ErrTurnInProgress, a.activeTurnID)
	case a.status == http.StatusBadRequest && a.code == "reference_rejected":
		return fmt.Errorf("%w: %s", ErrReferenceRejected, a.detail)
	case a.status == http.StatusServiceUnavailable:
		return fmt.Errorf("%w: %s answered 503 %s", ErrAEStudioUnavailable, op, a.code)
	case a.gatewayGone():
		return fmt.Errorf("%w: %s answered %d from the gateway", ErrAEStudioUnavailable, op, a.status)
	}
	return &StatusError{Op: op, Status: a.status, Code: a.code, Detail: a.detail}
}
