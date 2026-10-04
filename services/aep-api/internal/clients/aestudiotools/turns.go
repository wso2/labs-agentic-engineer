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

// turns.go — start-repo-turn: start (or reattach to) a kickoff or plan turn
// in the org's pod and read its NDJSON stream as TurnEvents.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/wso2/aep/aep-api/internal/clients/aestudiotools/gen"
)

// Turn kinds the pod starts for aep-api.
const (
	TurnKindStart = "start" // the kickoff interview
	TurnKindPlan  = "plan"  // planning a milestone into Tasks
)

// TurnEvent types on the stream.
const (
	EventTaskOp    = "task-op"
	EventKeepAlive = "keep-alive" // every 15 s; yielded, so a watchdog can see the turn is alive (D-2)
	EventResult    = "result"     // the last event
)

// maxTurnLine bounds one NDJSON line (a task-op carries a whole Task).
const maxTurnLine = 16 << 20

// Credit is who the turn's commits and records are credited to.
type Credit struct{ UserID, Name, Email string }

// PlanScope is the milestone a plan turn covers and which of its stories
// already have Tasks.
type PlanScope struct {
	Tag     string
	Stories []PlanStory
}

// PlanStory is one story of the milestone; Covered stories already have
// Tasks and are left alone.
type PlanStory struct {
	Number  int
	Title   string
	Covered bool
}

// PlanContextFile is one existing-Task render under its tasks/<n>.md name.
type PlanContextFile struct{ Path, Body string }

// TurnRequest starts one turn. TurnID (a UUID) makes it idempotent: the same
// id reattaches to the running or finished turn.
type TurnRequest struct {
	TurnID, Project, Kind string
	Credit                Credit
	Scope                 *PlanScope
	TaskContext           []PlanContextFile
	Text                  string
}

// TurnEvent is one line of the turn stream: a task-op (Op, Output), a
// keep-alive, or the final result (Status completed|failed, Code, and on a
// provider_limit the ResetAt the provider stated, zero when it stated none).
// The result's free-text message is deliberately not read (see
// TurnFailedError).
type TurnEvent struct {
	Type, Op string
	Output   json.RawMessage
	Status   string
	Code     string
	ResetAt  time.Time
}

// Turns starts turns in an org's pod.
type Turns interface {
	StartTurn(ctx context.Context, ref RepoRef, req TurnRequest) (iter.Seq2[TurnEvent, error], error)
}

// StartTurn starts (or reattaches to) a turn and returns its events, the
// result last. A different turn running for the project is
// ErrTurnInProgress. The stream is bounded by ctx only; ranging over it to
// the end, or breaking off, closes it, and it can be ranged over once. A
// stream that ends without a result is ErrAEStudioUnavailable.
func (a *Adapter) StartTurn(ctx context.Context, ref RepoRef, req TurnRequest) (iter.Seq2[TurnEvent, error], error) {
	if err := validRef(ref); err != nil {
		return nil, err
	}
	body, err := turnBody(req)
	if err != nil {
		return nil, err
	}
	resp, err := a.send(ctx, ref.Org, "start-repo-turn", func(ctx context.Context, c *gen.Client, impersonateOrg string, auth gen.RequestEditorFn) (*http.Response, error) {
		return c.StartRepoTurn(ctx, ref.Owner, ref.Repo, &gen.StartRepoTurnParams{XImpersonateOrg: impersonateOrg}, body, auth)
	}, always)
	if err != nil {
		return nil, err
	}
	return a.turnEvents(ctx, ref.Org, resp.Body), nil
}

func turnBody(req TurnRequest) (gen.TurnRequest, error) {
	id, err := uuid.Parse(req.TurnID)
	if err != nil {
		return gen.TurnRequest{}, fmt.Errorf("ae studio: turnId %q is not a UUID", req.TurnID)
	}
	if req.Kind != TurnKindStart && req.Kind != TurnKindPlan {
		return gen.TurnRequest{}, fmt.Errorf("ae studio: unknown turn kind %q", req.Kind)
	}
	b := gen.TurnRequest{
		TurnID:  id,
		Project: req.Project,
		Kind:    gen.TurnRequestKind(req.Kind),
		Credit:  gen.TurnCredit{UserID: req.Credit.UserID, Name: req.Credit.Name, Email: req.Credit.Email},
		Text:    req.Text,
	}
	if req.Scope != nil {
		b.Scope = gen.PlanScope{Tag: req.Scope.Tag, Stories: make([]gen.PlanStory, 0, len(req.Scope.Stories))}
		for _, s := range req.Scope.Stories {
			b.Scope.Stories = append(b.Scope.Stories, gen.PlanStory{Number: s.Number, Title: s.Title, Covered: s.Covered})
		}
	}
	for _, f := range req.TaskContext {
		b.TaskContext = append(b.TaskContext, gen.PlanContextFile{Path: f.Path, Body: f.Body})
	}
	return b, nil
}

// turnEvents reads the NDJSON stream, one event per non-empty line, until
// the result. It closes body when the range ends.
func (a *Adapter) turnEvents(ctx context.Context, org string, body io.ReadCloser) iter.Seq2[TurnEvent, error] {
	used := false
	return func(yield func(TurnEvent, error) bool) {
		if used {
			yield(TurnEvent{}, errors.New("ae studio: the turn stream was already read"))
			return
		}
		used = true
		defer func() { _ = body.Close() }()
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64<<10), maxTurnLine)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			var ev struct {
				Type   string          `json:"type"`
				Op     string          `json:"op"`
				Output json.RawMessage `json:"output"`
				Status  string          `json:"status"`
				Code    string          `json:"code"`
				ResetAt string          `json:"resetAt"`
			}
			if err := json.Unmarshal(line, &ev); err != nil {
				yield(TurnEvent{}, fmt.Errorf("ae studio: malformed turn stream line: %w", err))
				return
			}
			if ev.Type == "" {
				yield(TurnEvent{}, errors.New("ae studio: turn stream line without a type"))
				return
			}
			out := TurnEvent{Type: ev.Type, Op: ev.Op, Output: ev.Output, Status: ev.Status, Code: ev.Code, ResetAt: resetAtOf(ev.ResetAt)}
			if !yield(out, nil) || ev.Type == EventResult {
				return
			}
		}
		if err := sc.Err(); errors.Is(err, bufio.ErrTooLong) {
			yield(TurnEvent{}, fmt.Errorf("ae studio: turn stream line over %d bytes", maxTurnLine))
			return
		} else if err != nil {
			yield(TurnEvent{}, a.transportFailed(ctx, org, "start-repo-turn stream", err))
			return
		}
		yield(TurnEvent{}, fmt.Errorf("%w: the turn stream ended without a result", ErrAEStudioUnavailable))
	}
}

// resetAtOf reads a result's resetAt. It only shortens or lengthens a wait,
// so a value that is not an RFC 3339 time is dropped (the caller's fixed
// delay applies) rather than failing a turn that has already ended.
func resetAtOf(raw string) time.Time {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return t
}
