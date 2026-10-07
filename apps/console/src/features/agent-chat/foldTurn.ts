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
  parseSseStream,
  readToolInputPath,
  toChange,
  type AskQuestionInput,
  type Op,
  type StreamPart,
} from "@aep/agent-stream";
import type { TurnStatus } from "./api/turns";
import { isTurnStreamNotFound } from "./api/turns";
import { turnFailureText, type TurnFailure } from "./lib/turnFailure";
import { extractStreamingQuestions, parseQuestionsInput } from "./questionCards";

// Attach to a turn's SSE stream and fold it into the chat, to its terminal.
// Adapted from the old console's agent-chat/runTurn.ts (attachAndFoldTurn): the
// same frames, the same pre-stream 404 retry and the same severed-stream
// fallback poll. It writes into a `TurnSink` instead of the old console's global
// store, and it leaves out what this app does not show yet (the plan
// checklist, the register draft, the provider-wait label).

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
  /** The turn reached its terminal. Called at most once, never on a detach. */
  ended: (outcome: "completed" | "failed") => void;
}

/** The two calls the fold makes: the stream, and the status that settles a severed one. */
export interface TurnStreamApi {
  openStream: (projectName: string, turnId: string, from: number, signal: AbortSignal) => Promise<ReadableStream<Uint8Array>>;
  turn: (projectName: string, turnId: string) => Promise<TurnStatus | null>;
}

const ATTACH_404_MAX_ATTEMPTS = 8;

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
 * (a detach: the turn runs on, and a later attach replays it). A stream that
 * ends without a terminal is settled by one authoritative status read.
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
  const end = (outcome: "completed" | "failed") => {
    if (ended) return;
    ended = true;
    sink.ended(outcome);
  };
  const settle = (status: ({ status?: string } & TurnFailure) | null): boolean => {
    if (status?.status === "completed") {
      end("completed");
      return true;
    }
    if (status?.status === "failed") {
      sink.error(turnFailureText(status));
      end("failed");
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
      case "turn-committed":
        end("completed");
        break;
      case "turn-failed":
        sink.error(turnFailureText(part as TurnFailure));
        end("failed");
        break;
      default:
        break; // start/finish plumbing: nothing to show
    }
  };

  try {
    for (let attempt = 0; ; attempt++) {
      try {
        const body = await api.openStream(projectName, turnId, 0, signal);
        for await (const part of parseSseStream(body)) {
          if (signal.aborted) return;
          fold(part);
        }
        break; // the stream ended: at its terminal, or severed
      } catch (err) {
        if (signal.aborted) return;
        if (!isTurnStreamNotFound(err) || attempt >= ATTACH_404_MAX_ATTEMPTS - 1) throw err;
        // The turn may already be over on another replica.
        if (settle(await api.turn(projectName, turnId))) return;
        await sleep(attachBackoffMs(attempt), signal);
      }
    }
  } catch (err) {
    if (signal.aborted) return; // a detach, not a failure
    throw err;
  } finally {
    finalizeStreamingQuestions();
  }

  if (ended || signal.aborted) return;
  // Severed before the terminal: one authoritative read settles it. A turn
  // still running there is left running; the caller's next attach finds it.
  settle(await api.turn(projectName, turnId));
}
