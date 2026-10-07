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

import {
  isFileMutationTool,
  isQuestionTool,
  opForTool,
  parseSseFrames,
  readToolInputPath,
  toChange,
  type AskQuestionInput,
  type Op,
  type StreamPart,
} from "@aep/agent-stream";
import { isTurnStreamNotFound, isTurnStreamReplayTruncated, type TurnRead } from "./api/turns";
import { turnFailureText, type TurnFailure } from "./lib/turnFailure";
import { extractStreamingQuestions, parseQuestionsInput } from "./questionCards";

// Attach to a turn's SSE stream on the design agent and fold it into the chat,
// to its terminal. Adapted from the old console's agent-chat/runTurn.ts
// (attachAndFoldTurn): the same frames, the same pre-stream 404 retry, the
// resume after a severed stream at the frame after the last one folded (each
// frame's id is its index in the turn's replay buffer), the wait on a replay
// the pod refused as truncated, and the status read that settles a stream
// that could not be resumed. It writes into a `TurnSink` instead of the old
// console's global store, and it leaves out what this app does not show yet
// (the plan checklist, the register draft, the provider-wait label).

/** Where a turn's stream lands. */
export interface TurnSink {
  text: (delta: string) => void;
  /** A file the agent is writing or wrote, by its room path. */
  activity: (activity: {
    toolCallId: string;
    op: Op;
    path: string;
    state: "writing" | "done" | "failed";
    errorText?: string;
  }) => void;
  question: (question: { toolCallId: string; questions: AskQuestionInput[]; streaming: boolean }) => void;
  withdrawQuestion: (toolCallId: string) => void;
  /** A file write the bundle accepted: its `tool-result`, carrying the call's input. */
  wrote: (part: StreamPart) => void;
  error: (text: string) => void;
  /**
   * The turn reached its terminal. Called at most once, never on a detach.
   * `from` says how the end was learned: its terminal frame (`stream`), or a
   * status read (`status`), after which what the turn said is in the
   * persisted history, not necessarily in what was folded.
   */
  ended: (outcome: "completed" | "failed", from: "stream" | "status") => void;
}

/** The two calls the fold makes: the stream, and the status that settles a severed one. */
export interface TurnStreamApi {
  openStream: (projectName: string, turnId: string, from: number, signal: AbortSignal) => Promise<ReadableStream<Uint8Array>>;
  turn: (projectName: string, turnId: string) => Promise<TurnRead>;
}

const ATTACH_404_MAX_ATTEMPTS = 8;
/** Attaches in a row that bring no new frame before the fold stops resuming and asks the status. */
const RESUME_MAX_STALLS = 4;
/** How often a truncated replay's turn is asked whether it has ended. */
const TRUNCATED_STATUS_POLL_MS = 5_000;

function attachBackoffMs(attempt: number): number {
  return Math.min(250 * 2 ** attempt, 4000);
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal.aborted) {
      reject(new DOMException("Aborted", "AbortError"));
      return;
    }
    const onAbort = () => {
      clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal.addEventListener("abort", onAbort, { once: true });
  });
}

/**
 * Fold a turn's stream into `sink` until the turn ends or `signal` aborts
 * (a detach: the turn runs on, and a later attach replays it). The first
 * attach replays from frame 0. A stream that ends without its terminal is
 * attached again at the frame after the last one folded, until attaches stop
 * bringing anything new; then one authoritative status read settles it (a
 * turn still running there is left running: the caller's next attach finds
 * it). A replay the pod refuses as truncated (409 `replay_truncated`: a long
 * running turn overflowed its buffer) is waited out on the turn's status. A
 * turn the pod no longer holds rejects: the stream is lost.
 */
export async function foldTurn(input: {
  api: TurnStreamApi;
  projectName: string;
  turnId: string;
  signal: AbortSignal;
  sink: TurnSink;
}): Promise<void> {
  const { api, projectName, turnId, signal, sink } = input;
  let ended = false;
  const end = (outcome: "completed" | "failed", from: "stream" | "status") => {
    if (ended) return;
    ended = true;
    sink.ended(outcome, from);
  };
  const settle = (read: TurnRead | ({ status?: string } & TurnFailure)): boolean => {
    if (read === null || read === "gone") return false;
    const status = read;
    if (status.status === "completed") {
      end("completed", "status");
      return true;
    }
    if (status.status === "failed") {
      sink.error(turnFailureText(status));
      end("failed", "status");
      return true;
    }
    return false;
  };

  // Per tool call, its streamed input so far: a file's path is read as soon
  // as it closes (a "Writing …" line before the body is done), and a question
  // batch shows each question as its object closes. Keyed by the input
  // stream's id, which is the eventual toolCallId.
  const inputs = new Map<string, { toolName: string; buf: string; shown: boolean; shownQuestions: number }>();

  // A batch whose complete call never arrives must not stay unanswerable:
  // whatever questions streamed are the card.
  const finalizeStreamingQuestions = () => {
    for (const [id, st] of inputs) {
      if (st.shownQuestions === 0) continue;
      sink.question({ toolCallId: id, questions: extractStreamingQuestions(st.toolName, st.buf), streaming: false });
      st.shownQuestions = 0;
    }
  };

  const fold = (part: StreamPart) => {
    switch (part.type) {
      case "text-delta":
        sink.text(part.delta ?? part.text ?? "");
        break;
      case "tool-input-start":
        if (part.id && part.toolName && (isFileMutationTool(part.toolName) || isQuestionTool(part.toolName))) {
          inputs.set(part.id, { toolName: part.toolName, buf: "", shown: false, shownQuestions: 0 });
        }
        break;
      case "tool-input-delta": {
        const st = part.id ? inputs.get(part.id) : undefined;
        if (!st || !part.id) break;
        st.buf += part.delta ?? "";
        if (isQuestionTool(st.toolName)) {
          const streamed = extractStreamingQuestions(st.toolName, st.buf);
          if (streamed.length > st.shownQuestions) {
            st.shownQuestions = streamed.length;
            sink.question({ toolCallId: part.id, questions: streamed, streaming: true });
          }
          break;
        }
        if (st.shown) break;
        const path = readToolInputPath(st.buf);
        if (path) {
          st.shown = true;
          sink.activity({ toolCallId: part.id, op: opForTool(st.toolName), path, state: "writing" });
        }
        break;
      }
      case "tool-call": {
        // ask_question / ask_questions (ADR-0012): the complete call is the
        // card, replacing whatever prefix streamed.
        if (!isQuestionTool(part.toolName) || !part.toolCallId) break;
        if (part.invalid) {
          // The SDK rejected the input: nothing was asked, and a retry follows.
          inputs.delete(part.toolCallId);
          sink.withdrawQuestion(part.toolCallId);
          break;
        }
        const questions = parseQuestionsInput(part.toolName!, part.input);
        if (!questions) {
          finalizeStreamingQuestions();
          break;
        }
        inputs.delete(part.toolCallId);
        sink.question({ toolCallId: part.toolCallId, questions, streaming: false });
        break;
      }
      case "tool-result": {
        if (!part.toolName || !isFileMutationTool(part.toolName)) break;
        const change = toChange(part);
        const ok = change.result?.ok !== false;
        sink.activity({
          toolCallId: part.toolCallId ?? "",
          op: change.op,
          path: change.path,
          state: ok ? "done" : "failed",
          ...(change.result && !change.result.ok ? { errorText: change.result.message } : {}),
        });
        if (ok) sink.wrote(part);
        break;
      }
      case "error":
        // A coded error is the turn's failure; the `turn-failed` after it says it once.
        if ((part as { code?: string }).code) break;
        sink.error(typeof part.error === "string" ? part.error : "The agent hit an error.");
        break;
      case "turn-completed":
        end("completed", "stream");
        break;
      case "turn-failed":
        sink.error(turnFailureText(part as TurnFailure));
        end("failed", "stream");
        break;
      default:
        break; // start/finish plumbing: nothing to show
    }
  };

  /** Read the turn's status until it ends: true once settled, false when the pod no longer holds it. */
  const settleWhenEnded = async (): Promise<boolean> => {
    for (;;) {
      const read = await api.turn(projectName, turnId);
      if (signal.aborted) return true;
      if (read === "gone") return false;
      if (settle(read)) return true;
      await sleep(TRUNCATED_STATUS_POLL_MS, signal);
    }
  };

  // The next frame index to ask the pod for.
  let from = 0;
  // A frame without an id cannot be resumed after: attaching again would
  // replay it (a duplicate step) or skip it.
  let resumable = true;
  try {
    let notFound = 0;
    let stalls = 0;
    for (;;) {
      let delivered = 0;
      // Set only while a frame is folded: an error thrown then is a bug in
      // the fold, not a broken stream, and is never retried past.
      let folding = false;
      try {
        const body = await api.openStream(projectName, turnId, from, signal);
        for await (const frame of parseSseFrames(body)) {
          if (signal.aborted) return;
          if (frame.id !== undefined && frame.id < from) continue; // folded before the drop
          folding = true;
          fold(frame.part);
          folding = false;
          if (frame.id === undefined) resumable = false;
          else from = frame.id + 1;
          delivered += 1;
        }
      } catch (err) {
        if (signal.aborted) return;
        if (folding) throw err;
        if (isTurnStreamReplayTruncated(err)) {
          if (await settleWhenEnded()) return;
          throw err;
        }
        if (isTurnStreamNotFound(err)) {
          // The turn may already be over, or not streamable yet.
          const read = await api.turn(projectName, turnId);
          if (settle(read)) return;
          if (read === "gone" || ++notFound >= ATTACH_404_MAX_ATTEMPTS) throw err;
          await sleep(attachBackoffMs(notFound - 1), signal);
          continue;
        }
        // Any other refusal, no answer, or the stream breaking mid-read: the
        // pod may be restarting. An attach that brought nothing new.
      }
      if (ended || !resumable) break;
      stalls = delivered > 0 ? 0 : stalls + 1;
      if (stalls >= RESUME_MAX_STALLS) break;
      // A drop mid-stream attaches again at once; an attach that brought
      // nothing waits before the next.
      if (stalls > 0) await sleep(attachBackoffMs(stalls - 1), signal);
    }
  } catch (err) {
    if (signal.aborted) return; // a detach, not a failure
    throw err;
  } finally {
    finalizeStreamingQuestions();
  }

  if (ended || signal.aborted) return;
  // Not resumable: one authoritative read settles it. A turn still running
  // there is left running; the caller's next attach finds it.
  settle(await api.turn(projectName, turnId));
}
