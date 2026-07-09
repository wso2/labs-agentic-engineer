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

package genai

// genai_huma.go — the committed-truth turn HTTP edge (design §6 API delta):
//
//	POST …/agents/{id}/messages {useCase,instruction,target?}    → 202 {turnId}
//	GET  …/turns/{turnId}                                         → status
//	GET  …/turns/active                                           → status | 204
//	GET  …/turns/{turnId}/stream?from=N (SSE, Last-Event-ID)      → replay + tail
//	GET  …/agents/{id}/messages                                   → rehydrate
//
// The stream events are the raw agents StreamParts plus ONE aep-api terminal
// event (turn-committed / turn-failed), each stamped with `id: <index>`; the
// manifest part is backend plumbing and never appears.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wso2/aep/aep-api/internal/platform/humakit"
)

// startTurnMaxBodyBytes bounds the turn-create body. The body carries no
// file content (D13/D16 — the draft-era big-body guards are gone), so 64 KiB
// comfortably covers useCase + instruction + target.
const startTurnMaxBodyBytes = 64 << 10

// streamKeepAliveEvery paces `: keep-alive` comments on an idle attached
// stream so proxies keep the connection open and a dead client is noticed.
const streamKeepAliveEvery = 15 * time.Second

// --- Inputs / Outputs --------------------------------------------------------
// Every input embeds humakit.OrgScopedInput (org from the verified token; the
// IDOR fence). conversationId is a FE-chosen uuid; the service namespaces it
// into the authenticated tenant scope before it reaches the agents service.

type turnInput struct {
	humakit.OrgScopedInput
	ProjectName    string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ConversationID string `path:"conversationId" doc:"FE-chosen conversation uuid"`
	Body           struct {
		UseCase     string `json:"useCase" enum:"requirements-generate,requirements-chat,design-generate" doc:"Which generation/chat flow"`
		Instruction string `json:"instruction" doc:"User message / generation directive"`
		Target      string `json:"target,omitempty" doc:"Optional target (e.g. a doc type)"`
	}
}

type turnOutput struct {
	Body struct {
		TurnID string `json:"turnId" doc:"The started turn's id — poll/attach with it"`
	}
}

type turnStatusInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	TurnID      string `path:"turnId" doc:"Turn id from the 202 create response"`
}

type turnStatusOutput struct{ Body *TurnStatus }

type activeTurnInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
}

type turnStreamInput struct {
	humakit.OrgScopedInput
	ProjectName string `path:"projectName" doc:"Project name (DNS-label slug)"`
	TurnID      string `path:"turnId" doc:"Turn id from the 202 create response"`
	// From = -1 means "absent" (Huma cannot bind pointer params).
	From        int    `query:"from" default:"-1" minimum:"-1" doc:"Replay from this absolute event index (wins over Last-Event-ID)"`
	LastEventID string `header:"Last-Event-ID" doc:"Standard SSE resume: the last received event id"`
}

type rehydrateInput struct {
	humakit.OrgScopedInput
	ProjectName    string `path:"projectName" doc:"Project name (DNS-label slug)"`
	ConversationID string `path:"conversationId" doc:"FE-chosen conversation uuid"`
}

type rehydrateOutput struct{ Body json.RawMessage }

// turnConflictError renders the pinned 409 bodies verbatim (Huma writes a
// StatusError value directly): {"code":"turn_in_progress","activeTurnId":…}
// and {"code":"requirements_not_approved"}.
type turnConflictError struct {
	Code         string `json:"code"`
	ActiveTurnID string `json:"activeTurnId,omitempty"`
}

func (e *turnConflictError) Error() string  { return e.Code }
func (e *turnConflictError) GetStatus() int { return http.StatusConflict }

// RegisterGenAI registers the turn create/status/active/stream + chat
// rehydrate operations.
func RegisterGenAI(api huma.API, svc GenAIService) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-turn",
		Method:        http.MethodPost,
		Path:          "/projects/{projectName}/agents/{conversationId}/messages",
		Summary:       "Start a generation/chat turn (202 — runs detached; attach via the turn stream)",
		Tags:          []string{"GenAI"},
		Security:      humakit.SecurityUserJWT,
		DefaultStatus: http.StatusAccepted,
		MaxBodyBytes:  startTurnMaxBodyBytes,
	}, func(ctx context.Context, in *turnInput) (*turnOutput, error) {
		turnID, err := svc.StartTurn(ctx, in.OrgHandle, in.ProjectName, TurnInput{
			UseCase:        in.Body.UseCase,
			ConversationID: in.ConversationID,
			Instruction:    in.Body.Instruction,
			Target:         in.Body.Target,
		})
		if err != nil {
			return nil, mapTurnError(ctx, err)
		}
		out := &turnOutput{}
		out.Body.TurnID = turnID
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-turn",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/turns/{turnId}",
		Summary:     "Get one turn's status",
		Tags:        []string{"GenAI"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *turnStatusInput) (*turnStatusOutput, error) {
		st, err := svc.TurnStatus(ctx, in.OrgHandle, in.ProjectName, in.TurnID)
		if err != nil {
			return nil, mapTurnError(ctx, err)
		}
		return &turnStatusOutput{Body: st}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-active-turn",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/turns/active",
		Summary:     "Get the project's running turn (204 when none)",
		Tags:        []string{"GenAI"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *activeTurnInput) (*huma.StreamResponse, error) {
		st, err := svc.ActiveTurn(ctx, in.OrgHandle, in.ProjectName)
		if err != nil {
			return nil, mapTurnError(ctx, err)
		}
		// Manual body write: 200 with the status JSON, or a bodyless 204 —
		// a typed output cannot express the 204-no-body arm.
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			if st == nil {
				hctx.SetStatus(http.StatusNoContent)
				return
			}
			hctx.SetHeader("Content-Type", "application/json")
			hctx.SetStatus(http.StatusOK)
			_ = json.NewEncoder(hctx.BodyWriter()).Encode(st)
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "stream-turn",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/turns/{turnId}/stream",
		Summary:     "Attach to a turn's part stream (SSE — replay from ?from / Last-Event-ID, then live-tail)",
		Tags:        []string{"GenAI"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *turnStreamInput) (*huma.StreamResponse, error) {
		// ?from wins over Last-Event-ID; the header names the last RECEIVED
		// event, so resumption starts at the next index.
		from := 0
		switch {
		case in.From >= 0:
			from = in.From
		case in.LastEventID != "":
			if last, err := strconv.Atoi(in.LastEventID); err == nil {
				from = last + 1
			}
		}
		sub, err := svc.AttachTurn(ctx, in.OrgHandle, in.ProjectName, in.TurnID, from)
		if err != nil {
			return nil, mapTurnError(ctx, err) // pre-stream 404 / 409
		}
		return &huma.StreamResponse{Body: humakit.SSEBody(func(w io.Writer, flush func()) {
			defer sub.Cancel()
			streamSubscription(ctx, w, flush, sub)
		})}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-conversation",
		Method:      http.MethodGet,
		Path:        "/projects/{projectName}/agents/{conversationId}/messages",
		Summary:     "Rehydrate a chat conversation (messages only)",
		Tags:        []string{"GenAI"},
		Security:    humakit.SecurityUserJWT,
	}, func(ctx context.Context, in *rehydrateInput) (*rehydrateOutput, error) {
		raw, err := svc.Rehydrate(ctx, in.OrgHandle, in.ProjectName, in.ConversationID)
		if err != nil {
			return nil, mapRehydrateError(err)
		}
		return &rehydrateOutput{Body: raw}, nil
	})
}

// streamSubscription writes a subscription as SSE: replayed then live events,
// each as `id: <index>` + `data: <raw part>`, and — once the terminal event
// has been written — the trailing `data: [DONE]`. A subscriber dropped for
// falling behind (or an expired buffer) ends WITHOUT [DONE]; the client
// resumes with Last-Event-ID. Keep-alive comments pace idle waits.
func streamSubscription(ctx context.Context, w io.Writer, flush func(), sub *TurnSubscription) {
	writeEvent := func(ev BrokerEvent) bool {
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Index, ev.Data); err != nil {
			return false
		}
		flush()
		return true
	}
	done := func() {
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
		flush()
	}

	for _, ev := range sub.Replay {
		if !writeEvent(ev) {
			return
		}
		if ev.Terminal {
			done()
			return
		}
	}
	keepAlive := time.NewTicker(streamKeepAliveEvery)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return // client went away
		case <-keepAlive.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flush()
		case ev, ok := <-sub.C:
			if !ok {
				return // dropped subscriber / expired buffer — no [DONE]
			}
			if !writeEvent(ev) {
				return
			}
			if ev.Terminal {
				done()
				return
			}
		}
	}
}

// mapTurnError maps pre-202/pre-stream failures to the BFF status. The 409
// bodies are the pinned {"code": …} shapes; upstream statuses map per the
// shared edge policy (dispatch happens post-202 now, so upstream errors here
// are limited to rehydrate-like paths).
func mapTurnError(ctx context.Context, err error) error {
	var inProgress *TurnInProgressError
	if errors.As(err, &inProgress) {
		return &turnConflictError{Code: "turn_in_progress", ActiveTurnID: inProgress.ActiveTurnID}
	}
	if mapped, ok := humakit.MapAgentsUpstreamError(err, nil); ok {
		return mapped
	}
	switch {
	case errors.Is(err, ErrProjectRepoNotFound):
		return huma.Error404NotFound("project repository not found")
	case errors.Is(err, ErrTurnNotFound):
		return huma.Error404NotFound("turn not found")
	case errors.Is(err, ErrInvalidUseCase):
		return huma.Error400BadRequest("invalid use case")
	case errors.Is(err, ErrInvalidConversationID):
		return huma.Error400BadRequest("invalid conversation id")
	case errors.Is(err, ErrEmptyInstruction):
		return huma.Error400BadRequest(ErrEmptyInstruction.Error())
	case errors.Is(err, ErrNoAnthropicKey):
		return huma.Error400BadRequest(ErrNoAnthropicKey.Error())
	case errors.Is(err, ErrRequirementsNotApproved):
		return &turnConflictError{Code: "requirements_not_approved"}
	case errors.Is(err, ErrTurnBufferTruncated):
		return huma.Error409Conflict(ErrTurnBufferTruncated.Error())
	case errors.Is(err, ErrSkillsRepoUnavailable):
		// The org's _skills repo is unusable (row missing/unprovisionable or
		// backing repo gone — e.g. deleted externally under a lingering row).
		// A platform-side failure, not a client error: 503 with a clear
		// message, cause in the logs. Recovery is a manual operator action
		// today (drop the stale `_skills` row; the next resolve re-provisions).
		slog.ErrorContext(ctx, "genai turn: org skills repository unavailable", "error", err)
		return huma.Error503ServiceUnavailable("org skills repository unavailable — contact your platform admin")
	default:
		return internalError(ctx, "genai turn", err)
	}
}

func mapRehydrateError(err error) error {
	// The rehydrate handler does not thread its request ctx into the mapper;
	// the internal-error log carries the cause, which is what matters here.
	ctx := context.Background()
	switch {
	case errors.Is(err, ErrConversationNotFound):
		return huma.Error404NotFound("conversation not found")
	case errors.Is(err, ErrProjectRepoNotFound):
		return huma.Error404NotFound("project repository not found")
	case errors.Is(err, ErrInvalidConversationID):
		return huma.Error400BadRequest("invalid conversation id")
	default:
		if mapped, ok := humakit.MapAgentsUpstreamError(err, nil); ok {
			return mapped
		}
		return internalError(ctx, "genai rehydrate", err)
	}
}

// internalError is the single opaque-500 exit for the genai edge: it logs the
// underlying cause (nothing else in the default branch does — the client sees
// only "internal error") and returns the pinned 500. Every default-500 path
// routes through here so a swallowed cause can never reach production again.
func internalError(ctx context.Context, scope string, err error) error {
	slog.ErrorContext(ctx, "genai: unmapped internal error", "scope", scope, "error", err)
	return huma.Error500InternalServerError("internal error")
}
