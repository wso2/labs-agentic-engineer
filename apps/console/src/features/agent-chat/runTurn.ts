/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// Fold a turn's SSE stream into chat-store messages (#130). Collab turns put
// the FILE changes in the live room doc, so the panel folds only narration
// (text deltas), tool results (cards), errors, and the one aep-api terminal.

import {
  DECLARE_PLAN_TOOL,
  parseSseStream,
  toChange,
  opForTool,
  readToolInputPath,
  type StreamPart,
} from "@aep/agent-stream";
import {
  appendAssistantText,
  addMessage,
  upsertToolMessage,
  upsertQuestionMessage,
  upsertPlanMessage,
  setTurnStatus,
  notifyTurnEnd,
} from "./chatStore.js";
import {
  parseDeclarePlan,
  peekPlan,
  planDeclared,
  planFileWriting,
  planFileStreamed,
  planFileSettled,
  planTurnEnded,
  WRITE_TOOLS,
} from "./planStore.js";
import { extractStreamingQuestions, isQuestionTool, parseQuestionsInput } from "./questionCards.js";
import { getTurn, isTurnStreamNotFound, openTurnStream } from "./api/turns.js";
import {
  DRAFT_EXTERNAL_RESOURCE_TOOL,
  parseRegisterDraft,
  publishRegisterDraft,
} from "./registerDraftStore.js";

const FILE_TOOLS = new Set(["addFile", "editFile", "removeFile"]);

function publishDraftFromInput(chatKey: string, input: unknown): void {
  let value = input;
  if (typeof value === "string") {
    try {
      value = JSON.parse(value) as unknown;
    } catch {
      return;
    }
  }
  const draft = parseRegisterDraft(value);
  if (draft) publishRegisterDraft(chatKey, draft);
}

const ATTACH_404_MAX_ATTEMPTS = 8;
const ATTACH_404_BASE_MS = 250; // 250, 500, 1000, ... capped

function attachBackoffMs(attempt: number): number {
  return Math.min(ATTACH_404_BASE_MS * 2 ** attempt, 4000);
}

async function sleep(ms: number, signal: AbortSignal): Promise<void> {
  if (ms <= 0) return;
  await new Promise<void>((resolve, reject) => {
    const onAbort = () => {
      clearTimeout(t);
      signal.removeEventListener("abort", onAbort);
      reject(new DOMException("Aborted", "AbortError"));
    };
    const t = setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    if (signal.aborted) {
      onAbort();
      return;
    }
    signal.addEventListener("abort", onAbort);
  });
}

function settleFromTurnStatus(
  chatKey: string,
  turnId: string,
  status: { status?: string; message?: string } | null | undefined,
  onCommitted?: () => void,
  askedQuestion = false,
): boolean {
  if (status?.status === "completed") {
    setTurnStatus(chatKey, turnId, "completed");
    planTurnEnded(chatKey, turnId, "completed", askedQuestion);
    notifyTurnEnd(chatKey, "completed");
    onCommitted?.();
    return true;
  }
  if (status?.status === "failed") {
    setTurnStatus(chatKey, turnId, "failed");
    planTurnEnded(chatKey, turnId, "failed");
    notifyTurnEnd(chatKey, "failed");
    addMessage(chatKey, {
      role: "error",
      content: status.message ?? "The agent turn failed.",
    });
    return true;
  }
  return false;
}

/**
 * Attach to a running turn's stream and fold it to its terminal. Resolves
 * when the turn reaches a terminal (or the signal aborts — the turn keeps
 * running server-side; a later re-attach replays it). A severed stream falls
 * back to one authoritative status poll. `onCommitted` fires once when the
 * turn completes (turn-committed, or the fallback poll landing on completed)
 * so the caller can refresh caches the commit invalidated.
 */
export async function attachAndFoldTurn(
  chatKey: string,
  projectName: string,
  turnId: string,
  signal: AbortSignal,
  onCommitted?: () => void,
): Promise<void> {
  let sawTerminal = false;
  // Did this turn put a question to the user? A turn that ends on a question
  // has not finished its work — the answer's turn carries it on — so the plan
  // is paused rather than dissolved (#576).
  let askedQuestion = false;
  // Per tool call: accumulate its streamed input args so the path can be read as
  // soon as it closes (path is the first schema property), then show a "Creating
  // <file>" card BEFORE the tool finishes. Keyed by the input-stream id (== the
  // eventual toolCallId). `carded` guards against re-emitting once shown.
  // Question batches ride the same map: `shownQuestions` counts the complete
  // question objects already surfaced off the partial buffer (#270 latency —
  // the batch JSON streams for 10+ seconds; each question renders as soon as
  // its object closes instead of waiting for the full tool-call).
  const inputs = new Map<
    string,
    { toolName: string; buf: string; carded: boolean; shownQuestions: number }
  >();

  // A turn that never delivers the batch's complete `tool-call` (severed
  // stream, mid-call abort, malformed tail) must not strand a card in the
  // gated `streaming` state: whatever complete questions already streamed ARE
  // the card — flip it final so it becomes answerable.
  const finalizeStreamingQuestions = (): void => {
    for (const [id, st] of inputs) {
      if (st.shownQuestions > 0) {
        upsertQuestionMessage(chatKey, {
          role: "question",
          turnId,
          toolCallId: id,
          questions: extractStreamingQuestions(st.toolName, st.buf),
          streaming: false,
        });
        st.shownQuestions = 0;
      }
    }
  };

  /**
   * The pre-verdict writes of one file card: identical but for the status, and
   * both deliberately WITHOUT `ok`. The store merges each write onto the card, so
   * an `ok` set at either stage would survive into the settled state and show a
   * success tick the tool-result never granted.
   */
  const writeFileCard = (
    toolCallId: string,
    st: { toolName: string },
    path: string,
    status: "streaming" | "done",
  ): void => {
    upsertToolMessage(chatKey, {
      role: "tool",
      turnId,
      toolCallId,
      status,
      op: opForTool(st.toolName),
      path,
    });
  };

  /**
   * Fold one declare_plan payload (#576, ADR-0025). Idempotent through the
   * store's union, so the belt-and-braces double publish (input-end, then the
   * complete tool-call — same pattern as the register draft) cannot double
   * anything; the chat row appears only when the call genuinely added entries.
   */
  const publishPlan = (input: unknown, toolCallId: string | undefined): void => {
    const paths = parseDeclarePlan(input);
    if (!paths || paths.length === 0) return;
    const existing = peekPlan(chatKey);
    const grew = existing?.turnId === turnId && existing.entries.length > 0;
    const added = planDeclared(chatKey, turnId, paths);
    if (added > 0) {
      upsertPlanMessage(chatKey, {
        role: "plan",
        turnId,
        toolCallId: toolCallId ?? "",
        added,
        grew,
      });
    }
  };

  const fold = (part: StreamPart): void => {
    switch (part.type) {
      case "text-delta":
        appendAssistantText(chatKey, turnId, part.delta ?? part.text ?? "");
        break;
      case "tool-input-start":
        if (
          part.toolName &&
          part.id &&
          (FILE_TOOLS.has(part.toolName) ||
            isQuestionTool(part.toolName) ||
            part.toolName === DRAFT_EXTERNAL_RESOURCE_TOOL ||
            part.toolName === DECLARE_PLAN_TOOL)
        ) {
          inputs.set(part.id, { toolName: part.toolName, buf: "", carded: false, shownQuestions: 0 });
        }
        break;
      case "tool-input-delta": {
        const st = part.id ? inputs.get(part.id) : undefined;
        if (!st) break;
        st.buf += part.delta ?? "";
        if (isQuestionTool(st.toolName)) {
          const streamed = extractStreamingQuestions(st.toolName, st.buf);
          if (streamed.length > st.shownQuestions) {
            st.shownQuestions = streamed.length;
            upsertQuestionMessage(chatKey, {
              role: "question",
              turnId,
              toolCallId: part.id!,
              questions: streamed,
              streaming: true,
            });
          }
          break;
        }
        if (st.toolName === DRAFT_EXTERNAL_RESOURCE_TOOL) break;
        if (st.toolName === DECLARE_PLAN_TOOL) break;
        if (st.carded) break;
        const path = readToolInputPath(st.buf);
        if (path) {
          st.carded = true;
          writeFileCard(part.id!, st, path, "streaming");
          // A REMOVAL is not a write: following it would send the editor to a
          // document about to vanish, and settling its entry below would tick
          // a deleted file green.
          if (WRITE_TOOLS.has(st.toolName)) planFileWriting(chatKey, turnId, path);
        }
        break;
      }
      case "tool-input-end": {
        // The file's body is COMPLETE here — for a file tool the input is the
        // body, so a closed input stream means nothing more is coming. Stop the
        // spinner now and leave `ok` unset until the result settles it.
        //
        // Why this event and not `tool-result`: this one says the BODY is
        // written, which is a different fact from the bundle having accepted it,
        // and the two must not share a field. The verdict follows one frame
        // later — the agents service settles each write at its own call
        // (write-ledger.ts), so a batched step no longer parks the earlier
        // files' verdicts behind the last file's body.
        const st = part.id ? inputs.get(part.id) : undefined;
        if (st?.toolName === DRAFT_EXTERNAL_RESOURCE_TOOL) {
          publishDraftFromInput(chatKey, st.buf);
          break;
        }
        if (st?.toolName === DECLARE_PLAN_TOOL) {
          publishPlan(st.buf, part.id);
          break;
        }
        if (!st || !st.carded) break; // no card was ever shown (path never resolved)
        const path = readToolInputPath(st.buf);
        if (!path) break;
        writeFileCard(part.id!, st, path, "done");
        // The body is complete; the VERDICT settles the entry below.
        if (WRITE_TOOLS.has(st.toolName)) planFileStreamed(chatKey, turnId, path);
        break;
      }
      case "tool-call": {
        if (part.toolName === DRAFT_EXTERNAL_RESOURCE_TOOL) {
          publishDraftFromInput(chatKey, part.input);
          break;
        }
        if (part.toolName === DECLARE_PLAN_TOOL) {
          publishPlan(part.input, part.toolCallId);
          break;
        }
        // ask_question / ask_questions (ADR-0012): the COMPLETE call is the
        // authoritative card — it replaces whatever prefix streamed above and
        // clears the `streaming` gate. Malformed input → the streamed prefix
        // (individually-validated questions) is finalized as the card; with no
        // prefix, no card (the agent's prose still carries it).
        if (!isQuestionTool(part.toolName)) break;
        askedQuestion = true;
        const questions = parseQuestionsInput(part.toolName!, part.input);
        if (!questions) {
          finalizeStreamingQuestions();
          break;
        }
        if (part.toolCallId) inputs.delete(part.toolCallId);
        // Landing in the chat log is ALSO what surfaces the question on the
        // spec panel: useRoomQuestion subscribes to this log and mirrors
        // answerable questions into the room's shared Yjs map (single path —
        // no doc-publication race here).
        upsertQuestionMessage(chatKey, {
          role: "question",
          turnId,
          toolCallId: part.toolCallId ?? "",
          questions,
          streaming: false,
        });
        break;
      }
      case "tool-result": {
        if (!part.toolName || !FILE_TOOLS.has(part.toolName)) break;
        const change = toChange(part);
        if (WRITE_TOOLS.has(part.toolName)) {
          planFileSettled(chatKey, turnId, change.path, change.result?.ok !== false);
        }
        upsertToolMessage(chatKey, {
          role: "tool",
          turnId,
          toolCallId: part.toolCallId ?? "",
          status: "done",
          op: change.op,
          path: change.path,
          ok: change.result?.ok !== false,
          ...(change.result && !change.result.ok
            ? { errorText: change.result.message }
            : {}),
        });
        break;
      }
      case "error":
        addMessage(chatKey, {
          role: "error",
          content: typeof part.error === "string" ? part.error : "The agent hit an error.",
        });
        break;
      case "turn-committed":
        sawTerminal = true;
        setTurnStatus(chatKey, turnId, "completed");
        planTurnEnded(chatKey, turnId, "completed", askedQuestion);
        // Turn-end flush (#252 Task 5): the terminal frame is the signal the
        // chat panel's fallback + the spec view's deterministic room flush
        // both react to (see chatStore's turn-end bus + useTurnEndFlush).
        notifyTurnEnd(chatKey, "completed");
        onCommitted?.();
        break;
      case "turn-failed":
        sawTerminal = true;
        setTurnStatus(chatKey, turnId, "failed");
        planTurnEnded(chatKey, turnId, "failed");
        notifyTurnEnd(chatKey, "failed");
        addMessage(chatKey, {
          role: "error",
          content:
            (part as { message?: string }).message ?? "The agent turn failed.",
        });
        break;
      default:
        break; // start/finish plumbing — nothing to render
    }
  };

  try {
    let attempt = 0;
    for (;;) {
      try {
        const body = await openTurnStream(projectName, turnId, 0, signal);
        for await (const part of parseSseStream(body)) {
          if (signal.aborted) return;
          fold(part);
        }
        break; // stream ended cleanly (terminal or severed)
      } catch (err) {
        if (signal.aborted) return; // unmount/navigation — not a failure
        if (!isTurnStreamNotFound(err) || attempt >= ATTACH_404_MAX_ATTEMPTS - 1) {
          throw err;
        }
        // Turn may already be terminal on another replica — settle via getTurn.
        const status = await getTurn(projectName, turnId);
        if (settleFromTurnStatus(chatKey, turnId, status, onCommitted, askedQuestion)) {
          return;
        }
        await sleep(attachBackoffMs(attempt), signal);
        attempt += 1;
      }
    }
  } catch (err) {
    // sleep / openTurnStream may reject AbortError after the inner catch's
    // aborted check — treat detach as success, matching prior semantics.
    if (signal.aborted) return;
    throw err;
  } finally {
    // Stream over (terminal, severed, aborted, or settled via pre-stream
    // getTurn): nothing further can flip a streaming card. Do NOT finalize
    // inside the per-attempt catch — that would settle question cards mid-turn.
    finalizeStreamingQuestions();
  }

  if (sawTerminal || signal.aborted) return;
  // Severed before the terminal — one authoritative poll settles the bubble
  // (and is itself a "terminal frame arrived" for turn-end purposes: the
  // fallback poll IS how this turn's end is observed here).
  const status = await getTurn(projectName, turnId);
  settleFromTurnStatus(chatKey, turnId, status, onCommitted, askedQuestion);
}
