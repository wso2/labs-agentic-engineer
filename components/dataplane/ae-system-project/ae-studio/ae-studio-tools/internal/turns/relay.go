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

package turns

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/wso2/aep/ae-studio-tools/internal/gen/turnsock"
	"github.com/wso2/aep/ae-studio-tools/internal/problem"
)

const (
	// turnBudget bounds one relayed turn from its start: the agent ends a
	// turn at 30 min, and the gateway route (tools-turns) allows 35.
	turnBudget = 35 * time.Minute
	// callerWriteTimeout bounds one frame's write to the caller. A caller
	// that stops reading is treated as gone, so the relay keeps draining.
	callerWriteTimeout = 30 * time.Second
	// maxFrameBytes caps one NDJSON line (the agent's replay buffer holds
	// 16 MiB in all).
	maxFrameBytes = 16 << 20
	// maxRefusalBytes caps the body of a refused start.
	maxRefusalBytes = 64 << 10
	// streamDiedResult ends the caller's stream when the agent's ends
	// without a result frame (the reason vocabulary of turn-failed).
	streamDiedResult = `{"type":"result","status":"failed","code":"stream-died"}`
)

// Starter starts or reattaches to a turn on the Turn socket (Client).
type Starter interface {
	Start(ctx context.Context, body Body) (io.ReadCloser, int, error)
}

// Turn is one start request: the fields the relay logs, and the request
// itself.
type Turn struct {
	ID, Project, Kind string
	Body              Body
}

// Relay runs a turn on the Turn socket and streams its frames to the caller
// The turn belongs to the agent: the relay never
// ends it. When the caller goes away it keeps reading to the result, so
// turns.result is always logged; a retry with the same turnId reattaches on
// the agent's side.
type Relay struct {
	Turns Starter
	// Log receives turns.start and turns.result; slog.Default() when nil.
	Log *slog.Logger
}

// Serve answers one start: the agent's NDJSON stream on 200, every line
// relayed unchanged (keep-alive lines included), or its refusal (409
// turn_in_progress {activeTurnId} as JSON; a problem with status 400, 404,
// 409, 413 or 503 passed through). An unreachable socket is 503
// agent_unavailable, any other answer 502 agent_error.
func (rl Relay) Serve(w http.ResponseWriter, r *http.Request, t Turn) {
	log := rl.Log
	if log == nil {
		log = slog.Default()
	}
	attrs := []any{"kind", t.Kind, "project", t.Project, "turnId", t.ID}
	// Detached from the caller: its leaving must not end the read.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), turnBudget)
	defer cancel()
	stream, status, err := rl.Turns.Start(ctx, t.Body)
	if err != nil {
		log.Warn("turns.socket_failed", append(attrs, "error", err)...)
		problem.Write(w, http.StatusServiceUnavailable, "agent_unavailable", "the design agent could not be reached")
		return
	}
	defer func() { _ = stream.Close() }()
	if status != http.StatusOK {
		if !writeRefusal(w, status, stream) {
			log.Warn("turns.socket_failed", append(attrs, "status", status)...)
			problem.Write(w, http.StatusBadGateway, "agent_error", fmt.Sprintf("the design agent answered %d", status))
		}
		return
	}
	log.Info("turns.start", attrs...)
	result := relayFrames(w, r.Context(), stream)
	log.Info("turns.result", append(attrs, "status", result)...)
}

// writeRefusal passes a declared refusal through and reports whether it did:
// 409 TurnInProgress as JSON, or a problem with a code on 400, 404, 409, 413
// or 503. A 413 stays a 413 (the agent's payload_too_large), never a 502.
func writeRefusal(w http.ResponseWriter, status int, body io.Reader) bool {
	raw, err := io.ReadAll(io.LimitReader(body, maxRefusalBytes))
	if err != nil {
		return false
	}
	if status == http.StatusConflict {
		var tip turnsock.TurnInProgress
		if json.Unmarshal(raw, &tip) == nil && tip.Code == turnsock.TurnInProgressCodeTurnInProgress && tip.ActiveTurnID != (openapi_types.UUID{}) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(tip)
			return true
		}
	}
	switch status {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusServiceUnavailable:
	default:
		return false
	}
	var p turnsock.Problem
	if json.Unmarshal(raw, &p) != nil || p.Code == "" {
		return false
	}
	detail := p.Detail
	if detail == "" {
		detail = "the design agent refused the turn"
	}
	problem.Write(w, status, p.Code, detail)
	return true
}

// relayFrames copies the stream to the caller line by line while it is
// there, reads on to the result frame either way, and answers the result's
// status. A stream that ends without a result is failed, and the caller (if
// still there) gets streamDiedResult as its last line.
func relayFrames(w http.ResponseWriter, callerCtx context.Context, stream io.Reader) string {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	c := &caller{w: w, rc: http.NewResponseController(w), ctx: callerCtx}
	c.flush()
	sc := bufio.NewScanner(stream)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		c.send(line)
		if status, ok := resultStatus(line); ok {
			return status
		}
	}
	c.send([]byte(streamDiedResult))
	return "failed"
}

// resultStatus reports whether line is the result frame, and its status
// (completed or failed; anything else counts as failed).
func resultStatus(line []byte) (string, bool) {
	var f struct {
		Type   turnsock.ResultFrameType
		Status turnsock.ResultFrameStatus
	}
	if json.Unmarshal(line, &f) != nil || f.Type != turnsock.Result {
		return "", false
	}
	if f.Status != turnsock.Completed {
		return string(turnsock.Failed), true
	}
	return string(f.Status), true
}

// caller writes frames to the client until it goes away: its context ends,
// or a write or flush fails (or stalls past callerWriteTimeout). After that
// every send is dropped.
type caller struct {
	w    http.ResponseWriter
	rc   *http.ResponseController
	ctx  context.Context
	gone bool
}

func (c *caller) send(line []byte) {
	if c.gone || c.ctx.Err() != nil {
		c.gone = true
		return
	}
	_ = c.rc.SetWriteDeadline(time.Now().Add(callerWriteTimeout))
	frame := make([]byte, 0, len(line)+1)
	if _, err := c.w.Write(append(append(frame, line...), '\n')); err != nil {
		c.gone = true
		return
	}
	c.flush()
}

func (c *caller) flush() {
	if err := c.rc.Flush(); err != nil {
		c.gone = true
	}
}
