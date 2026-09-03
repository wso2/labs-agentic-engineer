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

package genaiturns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wso2/aep/aep-api/internal/clients/agentsvc"
	"github.com/wso2/aep/aep-api/internal/gen"
	"github.com/wso2/aep/aep-api/internal/platform/apierr"
	"github.com/wso2/aep/aep-api/internal/platform/gitfs"
	"github.com/wso2/aep/aep-api/internal/platform/tenant"
	"github.com/wso2/aep/aep-api/internal/spec"
)

// Handler serves the committed-truth turn edge. The deny-by-default tenant gate
// binds the active org before this runs, so org is read from context.
type Handler struct{ genai *spec.Service }

// New returns the slice's handler.
func New(genai *spec.Service) *Handler { return &Handler{genai: genai} }

// createTurnMaxInstructionBytes mirrors the retired startTurnMaxBodyBytes.
const createTurnMaxInstructionBytes = 64 << 10

// GenAI feature on the strict interface — the committed-truth turn edge:
//
//	create-turn      POST …/agents/{conversationId}/messages → 202 {turnId}
//	get-turn         GET  …/turns/{turnId}                   → status
//	get-active-turn  GET  …/turns/active                     → status | 204
//	stream-turn      GET  …/turns/{turnId}/stream?from=N     → SSE replay + tail
//	get-conversation GET  …/agents/{conversationId}/messages → rehydrate
//
// The stream events are the raw agents StreamParts plus ONE aep-api terminal
// event (turn-committed / turn-failed), each stamped with `id: <index>`; the
// manifest part is backend plumbing and never appears. The pinned 409 bodies
// ({"code":"turn_in_progress",…} / {"code":"requirements_missing"}) predate
// the flat error envelope and are preserved verbatim via a custom response
// type rather than the envelope writers.

// genaiStreamKeepAliveEvery paces `: keep-alive` comments on an idle attached
// stream so proxies keep the connection open and a dead client is noticed.
const genaiStreamKeepAliveEvery = 15 * time.Second

func (h *Handler) CreateTurn(ctx context.Context, request gen.CreateTurnRequestObject) (gen.CreateTurnResponseObject, error) {
	// Two content types (#428), and `JSONBody` rather than `Body` because the
	// generator names one field per type once there is more than one.
	//
	// multipart is the arm that carries chat attachments. It exists because the
	// bytes have nowhere else to be: nothing stores them (ADR-0019), so they ride
	// this request into the detached turn and are durable only as parts of the
	// conversation's history afterwards.
	in := spec.TurnInput{ConversationID: request.ConversationID}
	switch {
	case request.MultipartBody != nil:
		parsed, err := readMultipartTurn(request.MultipartBody)
		if err != nil {
			return nil, err
		}
		in.Instruction = parsed.Instruction
		in.Target = parsed.Target
		in.Collab = parsed.Collab
		in.Attachments = parsed.Attachments
		anchor, err := parseAnchorField(parsed.Anchor)
		if err != nil {
			return nil, err
		}
		aim, err := aimFromJSON(anchor, parsed.Intent)
		if err != nil {
			return nil, err
		}
		in.Aim = aim
	case request.JSONBody != nil:
		in.Instruction = request.JSONBody.Instruction
		in.Target = request.JSONBody.Target
		in.Collab = request.JSONBody.Collab
		aim, err := aimFromJSON(request.JSONBody.Anchor, string(request.JSONBody.Intent))
		if err != nil {
			return nil, err
		}
		in.Aim = aim
	default:
		return nil, apierr.BadRequest("request body is required")
	}
	// The retired edge capped this body at 64 KiB (it carries no file content —
	// useCase + instruction + target); the edge-wide 10 MiB cap alone would be
	// a 160x loosening on a payload that is buffered whole and forwarded to
	// the agents service. Attachments are capped separately and on their own
	// bytes (see attachments.go) — they are the one thing on this body that IS
	// file content.
	if len(in.Instruction) > createTurnMaxInstructionBytes {
		return nil, apierr.New(http.StatusRequestEntityTooLarge, "request_too_large",
			"instruction exceeds the size limit", nil)
	}
	// Text is required even when files are attached: the shared TurnSpec
	// validator rejects an empty chat turn, so accepting one here would only
	// move the failure to a worse place — mid-dispatch, after the turn row
	// exists.
	if strings.TrimSpace(in.Instruction) == "" {
		return nil, apierr.BadRequest("instruction is required")
	}
	org := tenant.BoundOrgFromContext(ctx)
	turnID, err := h.genai.StartTurn(ctx, org, request.ProjectName, in)
	if err != nil {
		if conflict, ok := turnConflictOf(err); ok {
			return conflict, nil
		}
		return nil, mapGenAITurnError(ctx, err)
	}
	return gen.CreateTurn202JSONResponse(gen.TurnOutputBody{TurnID: turnID}), nil
}

func (h *Handler) GetTurn(ctx context.Context, request gen.GetTurnRequestObject) (gen.GetTurnResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	st, err := h.genai.TurnStatus(ctx, org, request.ProjectName, request.TurnID)
	if err != nil {
		return nil, mapGenAITurnError(ctx, err)
	}
	return gen.GetTurn200JSONResponse(turnStatusModel(st)), nil
}

func (h *Handler) GetActiveTurn(ctx context.Context, request gen.GetActiveTurnRequestObject) (gen.GetActiveTurnResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	st, err := h.genai.ActiveTurn(ctx, org, request.ProjectName)
	if err != nil {
		return nil, mapGenAITurnError(ctx, err)
	}
	if st == nil {
		return gen.GetActiveTurn204Response{}, nil
	}
	return gen.GetActiveTurn200JSONResponse(turnStatusModel(st)), nil
}

func (h *Handler) StreamTurn(ctx context.Context, request gen.StreamTurnRequestObject) (gen.StreamTurnResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	// ?from wins over Last-Event-ID; the header names the last RECEIVED
	// event, so resumption starts at the next index.
	from := 0
	switch {
	case request.Params.From != nil && *request.Params.From >= 0:
		from = *request.Params.From
	case request.Params.LastEventID != "":
		// Opaque per the SSE spec: malformed values are ignored (full replay),
		// matching the retired edge's lenient Atoi.
		if last, err := strconv.Atoi(request.Params.LastEventID); err == nil {
			from = last + 1
		}
	}
	sub, err := h.genai.AttachTurn(ctx, org, request.ProjectName, request.TurnID, from)
	if err != nil {
		return nil, mapGenAITurnError(ctx, err) // pre-stream 404 / 409
	}
	// The strict wrapper calls Visit after this handler returns — streaming
	// happens there, so the run closure captures ctx (client-gone signal) and
	// the subscription.
	return turnStreamResponse{run: func(w io.Writer, flush func()) {
		defer sub.Cancel()
		streamTurnSubscription(ctx, w, flush, sub)
	}}, nil
}

// ListConversations resolves the project's chat threads (#430) — one element
// today, the current thread, lazily created on first read so every member
// converges on it. Plural-shaped so the multi-conversation future grows the
// array instead of renaming the endpoint.
func (h *Handler) ListConversations(ctx context.Context, request gen.ListConversationsRequestObject) (gen.ListConversationsResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	rows, err := h.genai.ListConversations(ctx, org, request.ProjectName)
	if err != nil {
		return nil, mapGenAITurnError(ctx, err)
	}
	views := make([]gen.ProjectConversationView, 0, len(rows))
	for _, row := range rows {
		views = append(views, conversationView(row))
	}
	return gen.ListConversations200JSONResponse(gen.ProjectConversationList{Conversations: views}), nil
}

// RotateConversation starts a fresh thread for the whole project (#430 D4).
// Deliberately ungated — rotation is the escape hatch from an abandoned
// interview; the console owns the confirmation that names what is at stake.
func (h *Handler) RotateConversation(ctx context.Context, request gen.RotateConversationRequestObject) (gen.RotateConversationResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	row, err := h.genai.RotateConversation(ctx, org, request.ProjectName)
	if err != nil {
		return nil, mapGenAITurnError(ctx, err)
	}
	return gen.RotateConversation201JSONResponse(conversationView(*row)), nil
}

func conversationView(row spec.ProjectConversation) gen.ProjectConversationView {
	return gen.ProjectConversationView{
		ConversationID: row.ID,
		CreatedAt:      row.CreatedAt,
		CreatedBy:      row.CreatedBy,
		Current:        row.Current,
	}
}

func (h *Handler) GetConversation(ctx context.Context, request gen.GetConversationRequestObject) (gen.GetConversationResponseObject, error) {
	org := tenant.BoundOrgFromContext(ctx)
	raw, err := h.genai.Rehydrate(ctx, org, request.ProjectName, request.ConversationID)
	if err != nil {
		return nil, mapGenAIRehydrateError(err)
	}
	return conversationJSONResponse(raw), nil
}

// ---- responses ---------------------------------------------------------------

// turnConflictOf maps the StartTurn conflict rejections onto the contract's
// 409 TurnConflict body ({"code":"turn_in_progress","activeTurnId"} /
// {"code":"requirements_missing"} / {"code":"conversation_rotated"} — declared
// in the contract, generated type); every other error stays on the envelope
// path (mapGenAITurnError).
func turnConflictOf(err error) (gen.CreateTurnResponseObject, bool) {
	var inProgress *spec.TurnInProgressError
	if errors.As(err, &inProgress) {
		return gen.CreateTurn409JSONResponse(gen.TurnConflict{
			Code: gen.TurnInProgress, ActiveTurnID: inProgress.ActiveTurnID,
		}), true
	}
	if errors.Is(err, spec.ErrConversationRotated) {
		// #430 single-era rule: the addressed thread is no longer current —
		// the console re-resolves via list-conversations and retries.
		return gen.CreateTurn409JSONResponse(gen.TurnConflict{
			Code: gen.ConversationRotated,
		}), true
	}
	return nil, false
}

// turnStreamResponse adapts the SSE loop onto the generated stream-turn
// response interface: Visit stamps the event-stream preamble (sseStream) and
// hands the body writer + flush to the captured run closure.
type turnStreamResponse struct {
	run func(w io.Writer, flush func())
}

func (r turnStreamResponse) VisitStreamTurnResponse(w http.ResponseWriter) error {
	return sseStream(w, r.run)
}

// conversationJSONResponse writes the agents service's conversation JSON
// verbatim. The generated 200 type (map[string]interface{}, from the
// contract's free-form object schema) would re-encode the payload — losing
// number fidelity and key order — so the raw passthrough keeps the byte-exact
// body the retired Huma edge served.
type conversationJSONResponse json.RawMessage

func (r conversationJSONResponse) VisitGetConversationResponse(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(r)
	return err
}

// turnStatusModel converts the feature's read view into the contract schema
// type (field-for-field; the JSON tags are identical).
func turnStatusModel(st *spec.TurnStatus) gen.TurnStatus {
	return gen.TurnStatus{
		TurnID:         st.TurnID,
		ConversationID: st.ConversationID,
		UseCase:        st.UseCase,
		Status:         st.Status,
		CommitSha:      st.CommitSHA,
		Reason:         st.Reason,
		Paths:          st.Paths,
		NoChanges:      st.NoChanges,
		Message:        st.Message,
		CreatedAt:      st.CreatedAt,
		UpdatedAt:      st.UpdatedAt,

		// The turn's display record (#562) — what a client attaching to a
		// running turn paints as the message that started it.
		Instruction:       st.Instruction,
		AuthorID:          st.AuthorID,
		AuthorDisplayName: st.AuthorDisplayName,
	}
}

// ---- streaming ----------------------------------------------------------------

// streamTurnSubscription writes a subscription as SSE: replayed then live
// events, each as `id: <index>` + `data: <raw part>`, and — once the terminal
// event has been written — the trailing `data: [DONE]`. A subscriber dropped
// for falling behind (or an expired buffer) ends WITHOUT [DONE]; the client
// resumes with ?from. Keep-alive comments pace idle waits.
func streamTurnSubscription(ctx context.Context, w io.Writer, flush func(), sub *spec.TurnSubscription) {
	writeEvent := func(ev spec.BrokerEvent) bool {
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
	keepAlive := time.NewTicker(genaiStreamKeepAliveEvery)
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

// sseStream stamps the standard SSE response preamble — the four event-stream
// headers, an explicit 200, and an initial flush so the headers reach the
// client before the first frame — then hands the body writer + a per-chunk
// flush func to run. It is the strict-server re-home of humakit.SSEBody:
// every streaming operation's response type wraps this in its
// VisitXxxResponse(w) method (frame framing and loop logic stay per-feature).
func sseStream(w http.ResponseWriter, run func(w io.Writer, flush func())) error {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flush := func() {
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	flush()
	run(w, flush)
	return nil
}

// ---- error mapping --------------------------------------------------------------

// mapGenAITurnError maps pre-202/pre-stream turn failures onto the envelope.
// The two StartTurn conflict rejections are handled before this mapper
// (turnConflictOf — their pinned bodies bypass the envelope); upstream agents
// failures ride the shared edge policy (502 default, no overrides — dispatch
// happens post-202, so upstream errors here are limited to rehydrate-like
// paths).
func mapGenAITurnError(ctx context.Context, err error) error {
	if mapped, ok := mapAgentsUpstreamError(err, nil); ok {
		return mapped
	}
	switch {
	case errors.Is(err, spec.ErrProjectRepoNotFound):
		return apierr.NotFound("project repository not found")
	case errors.Is(err, spec.ErrTurnNotFound):
		return apierr.NotFound("turn not found")
	case errors.Is(err, spec.ErrInvalidConversationID):
		return apierr.BadRequest("invalid conversation id")
	case errors.Is(err, spec.ErrEmptyInstruction):
		return apierr.BadRequest(spec.ErrEmptyInstruction.Error())
	case errors.Is(err, spec.ErrCollabNoToken):
		return apierr.BadRequest(spec.ErrCollabNoToken.Error())
	case errors.Is(err, spec.ErrNoAnthropicKey):
		return apierr.BadRequest(spec.ErrNoAnthropicKey.Error())
	case errors.Is(err, spec.ErrTurnBufferTruncated):
		return apierr.Conflict(spec.ErrTurnBufferTruncated.Error())
	case errors.Is(err, spec.ErrSkillsRepoUnavailable):
		// The org's _skills repo is unusable (row missing/unprovisionable or
		// backing repo gone — e.g. deleted externally under a lingering row).
		// A platform-side failure, not a client error: 503 with a clear
		// message, cause in the logs. Recovery is a manual operator action
		// today (drop the stale `_skills` row; the next resolve re-provisions).
		slog.ErrorContext(ctx, "genai turn: org skills repository unavailable", "error", err)
		return apierr.ServiceUnavailable("org skills repository unavailable — contact your platform admin")
	case errors.Is(err, spec.ErrConversationsUnavailable):
		// A wiring bug (the thread store was not assembled), never a client
		// error — logged 503 with a clear message, same posture as the
		// skills-repo arm above.
		slog.ErrorContext(ctx, "genai turn: conversation store not configured", "error", err)
		return apierr.ServiceUnavailable(spec.ErrConversationsUnavailable.Error())
	case errors.Is(err, gitfs.ErrDiskAdmission):
		// New snapshot dests are refused at ≥90% workspace usage. Operator
		// recovery (reap / prune), not a client bug — 503 with a sentence
		// the console can show, cause in the logs. Same posture as skills.
		slog.ErrorContext(ctx, "genai turn: workspace disk admission refused", "error", err)
		return apierr.ServiceUnavailable("workspace disk is full — try again in a few minutes, or contact your platform admin")
	default:
		return genaiInternalError(ctx, "genai turn", err)
	}
}

func mapGenAIRehydrateError(err error) error {
	// The rehydrate handler does not thread its request ctx into the mapper;
	// the internal-error log carries the cause, which is what matters here.
	ctx := context.Background()
	switch {
	case errors.Is(err, spec.ErrConversationNotFound):
		return apierr.NotFound("conversation not found")
	case errors.Is(err, spec.ErrProjectRepoNotFound):
		return apierr.NotFound("project repository not found")
	case errors.Is(err, spec.ErrInvalidConversationID):
		return apierr.BadRequest("invalid conversation id")
	default:
		if mapped, ok := mapAgentsUpstreamError(err, nil); ok {
			return mapped
		}
		return genaiInternalError(ctx, "genai rehydrate", err)
	}
}

// genaiInternalError is the single opaque-500 exit for the genai edge: it logs
// the underlying cause (nothing else in the default branch does — the client
// sees only "internal error") and returns the pinned 500. Every default-500
// path routes through here so a swallowed cause can never reach production
// again.
func genaiInternalError(ctx context.Context, scope string, err error) error {
	slog.ErrorContext(ctx, "genai: unmapped internal error", "scope", scope, "error", err)
	return apierr.Internal("internal error")
}

// mapAgentsUpstreamError applies the shared BFF edge policy for a pre-stream
// agents-service failure: if err is an *agentsvc.UpstreamError, the caller's
// per-status override (e.g. a typed 409 in-progress body) wins, and any other
// upstream status maps to 502. Returns (nil, false) when err is not an
// upstream error, so feature-specific sentinel mapping stays in the caller.
// (Strict-server re-home of humakit.MapAgentsUpstreamError.)
func mapAgentsUpstreamError(err error, overrides map[int]error) (error, bool) {
	var ue *agentsvc.UpstreamError
	if !errors.As(err, &ue) {
		return nil, false
	}
	if mapped, ok := overrides[ue.StatusCode]; ok {
		return mapped, true
	}
	return apierr.BadGateway("agents service error"), true
}
