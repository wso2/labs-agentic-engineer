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
  ANSWER_PREFIX,
  isErrorToolOutput,
  isFileMutationTool,
  isQuestionTool,
  opForTool,
  type AskQuestionInput,
  type Op,
  type QuestionAnswer,
} from "@aep/agent-stream";
import { parseDesignCommand, parseInterviewCommand, parsePrototypeCommand, START_COMMAND } from "@aep/contracts/commands";
import type { ConversationMessage } from "./api/conversation";
import type { PrototypeFeedback } from "./turnScope";
import { parseQuestionsInput } from "./questionCards";

// The project conversation as the chat shows it: a list of items, and the
// pure operations the turn stream and the history make on it. Nothing here
// talks to the server or to React, so the chat store's rules are tested on
// plain arrays.

/** What a note offers next: open a version on the Builds card, interview a feature, or review a prototype. */
export type NoteAction =
  | { kind: "open-build"; label: string; version: string }
  | { kind: "interview"; label: string; featureId: string }
  /** Review a prototype: one component's, or the Prototype tab when the turn made several. */
  | { kind: "open-prototype"; label: string; component: string | null };

/** One row of the chat. */
export type ChatItem =
  | {
      kind: "user";
      id: string;
      text: string;
      /** Who sent it, when that is not the signed-in user or not known. */
      author?: string;
      /**
       * The prototype review the message sent: the wire text is only the
       * command, so the row reads as these requests. The history carries it
       * (the turn journals it), so a reload and a teammate read the same.
       */
      prototypeFeedback?: PrototypeFeedback;
      /** `sending` until the server accepts the turn; `failed` when it refused it. */
      state: "sending" | "sent" | "failed";
      turnId?: string;
    }
  | { kind: "agent"; id: string; turnId: string; text: string }
  /**
   * A line in the agent's voice about something the user started outside the
   * chat (a build). The console posts it; the conversation's history does not
   * carry it, so it lasts until the thread is read again. Its actions are the
   * next steps it offers.
   */
  | { kind: "note"; id: string; text: string; actions?: NoteAction[] }
  | {
      /** A file the agent wrote, as one compact line. */
      kind: "activity";
      id: string;
      turnId: string;
      toolCallId: string;
      op: Op;
      /** The room path ("specs/requirements/features/F4-spending-reports.md"). */
      path: string;
      /** `writing` while its body streams; `done` once written; `failed` when the bundle refused it. */
      state: "writing" | "done" | "failed";
      errorText?: string;
    }
  | {
      kind: "question";
      id: string;
      turnId: string;
      toolCallId: string;
      questions: AskQuestionInput[];
      /** The batch is still streaming: it shows, but cannot be answered yet. */
      streaming: boolean;
      /** Set once answered from this card; the card is then read-only. */
      answers?: QuestionAnswer[];
    }
  | { kind: "error"; id: string; text: string };

export type ActivityItem = Extract<ChatItem, { kind: "activity" }>;
export type QuestionItem = Extract<ChatItem, { kind: "question" }>;

/** Append streamed narration to the turn's current text row, or start one after anything else. */
export function appendAgentText(items: ChatItem[], turnId: string, delta: string): ChatItem[] {
  if (!delta) return items;
  const last = items.at(-1);
  if (last?.kind === "agent" && last.turnId === turnId) {
    return [...items.slice(0, -1), { ...last, text: last.text + delta }];
  }
  const n = items.filter((i) => i.kind === "agent" && i.turnId === turnId).length;
  return [...items, { kind: "agent", id: `${turnId}:text:${n}`, turnId, text: delta }];
}

/** Put an item in place by id (merging onto the one there), or at the end. */
function upsert<T extends ChatItem>(items: ChatItem[], item: T): ChatItem[] {
  const at = items.findIndex((i) => i.id === item.id);
  if (at < 0) return [...items, item];
  const next = [...items];
  next[at] = { ...next[at]!, ...item } as ChatItem;
  return next;
}

export function upsertActivity(
  items: ChatItem[],
  turnId: string,
  activity: Pick<ActivityItem, "toolCallId" | "op" | "path" | "state" | "errorText">,
): ChatItem[] {
  return upsert(items, { kind: "activity", id: `${turnId}:tool:${activity.toolCallId}`, turnId, ...activity });
}

export function upsertQuestion(
  items: ChatItem[],
  turnId: string,
  question: Pick<QuestionItem, "toolCallId" | "questions" | "streaming">,
): ChatItem[] {
  return upsert(items, { kind: "question", id: `${turnId}:q:${question.toolCallId}`, turnId, ...question });
}

/** Record a card's answers (read-only from then on), or clear them (answerable again). */
export function setAnswers(items: ChatItem[], itemId: string, answers: QuestionAnswer[] | null): ChatItem[] {
  return items.map((i) => {
    if (i.id !== itemId || i.kind !== "question") return i;
    if (answers) return { ...i, answers };
    const card = { ...i };
    delete card.answers;
    return card;
  });
}

/** Take back a question the SDK rejected: its streamed prefix was never asked. */
export function dropQuestion(items: ChatItem[], turnId: string, toolCallId: string): ChatItem[] {
  return items.filter((i) => i.id !== `${turnId}:q:${toolCallId}`);
}

/** A turn's output (not the user row that started it): cleared before a replay from the start re-adds it. */
export function dropTurnOutput(items: ChatItem[], turnId: string): ChatItem[] {
  return items.filter((i) => i.kind === "user" || i.kind === "error" || i.kind === "note" || i.turnId !== turnId);
}

/**
 * The one question card that still takes an answer: the newest one, unless
 * it is still streaming, was answered from its card, or any later message
 * reached the agent (a typed reply answers it too, ADR-0012). A failed send
 * supersedes nothing: the agent never saw it. Worked out from the log alone,
 * so a reload lands on the same answer.
 */
export function answerableQuestionId(items: ChatItem[]): string | null {
  for (let i = items.length - 1; i >= 0; i--) {
    const item = items[i]!;
    if (item.kind === "user" && item.state !== "failed") return null;
    if (item.kind === "question") return item.streaming || item.answers ? null : item.id;
  }
  return null;
}

/**
 * The feature an interview just wrote, and the row its turn ends on: after
 * an interview writes a feature, the chat offers there to walk what it
 * assumed and to go on to the next feature. It is the latest exchange's, and
 * only when that exchange is an interview's: the user's message answered the
 * agent's question (from its card or typed, as `answerableQuestionId` counts
 * an answer) and the turn then wrote a feature. A turn that edits a feature
 * for another reason (a design comment addressed, a plain request) wrote no
 * interview, and nothing follows it. Worked out from the log alone, so a
 * reload lands on the same answer.
 */
export function interviewWriteUp(
  items: ChatItem[],
  featurePaths: ReadonlySet<string>,
): { path: string; afterId: string } | null {
  let written: string | null = null;
  let i = items.length - 1;
  // The latest exchange: back to the message that started it.
  for (; i >= 0; i--) {
    const item = items[i]!;
    if (item.kind === "user" && item.state !== "failed") break;
    if (!written && item.kind === "activity" && item.state === "done" && featurePaths.has(item.path)) written = item.path;
  }
  if (!written || i < 0) return null;
  // The exchange before it: did the agent ask a question the message answered?
  for (i--; i >= 0; i--) {
    const item = items[i]!;
    if (item.kind === "user" && item.state !== "failed") return null;
    if (item.kind === "question") return { path: written, afterId: items.at(-1)!.id };
  }
  return null;
}

/**
 * How a user row reads. Its text is what went over the wire, and two kinds of
 * line carry machinery: an answer (`Answer to "…": Finance only`) reads as the
 * answer, with its question on the card above it; the kickoff (`/start <idea>`)
 * reads as the user's own idea, and a feature's interview as asking for it.
 */
export function userLineText(text: string): string {
  if (text.startsWith(ANSWER_PREFIX)) {
    const split = text.indexOf('": ');
    if (split >= 0) return text.slice(split + 3);
  }
  const trimmed = text.trim();
  if (trimmed === START_COMMAND) return "Start the project from the brief.";
  if (trimmed.startsWith(`${START_COMMAND} `)) return trimmed.slice(START_COMMAND.length + 1).trim();
  const interview = parseInterviewCommand(trimmed);
  if (interview) return `Interview ${interview.featureId}.`;
  const design = parseDesignCommand(trimmed);
  if (design) return design.featureIds.length > 0 ? `Design ${design.featureIds.join(", ")}.` : "Design the features.";
  const prototype = parsePrototypeCommand(trimmed);
  if (prototype) return prototype.component ? `Prototype ${prototype.component}.` : "Make the prototypes.";
  return text;
}

// A message's text parts, joined. Content is model-shaped and varies by role
// (the contract leaves it untyped): a plain string, or an array of parts of
// which only `text` parts are prose.
function contentText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return "";
  return content
    .map((p) => (isPart(p) && p.type === "text" && typeof p.text === "string" ? p.text : ""))
    .join("");
}

interface Part {
  type?: unknown;
  text?: unknown;
  toolName?: unknown;
  toolCallId?: unknown;
  input?: unknown;
  output?: unknown;
}

function isPart(value: unknown): value is Part {
  return typeof value === "object" && value !== null;
}

function parts(content: unknown): Part[] {
  return Array.isArray(content) ? content.filter(isPart) : [];
}

/** Tool-call ids whose result is an error: a call the SDK rejected, or a write the bundle refused. */
function failedToolCalls(history: ConversationMessage[]): Set<string> {
  const ids = new Set<string>();
  for (const m of history) {
    if (m.role !== "tool") continue;
    for (const p of parts(m.content)) {
      if (p.type !== "tool-result" || typeof p.toolCallId !== "string") continue;
      // A write's own verdict, bare or in the SDK's `{ type: "json", value }` wrapper.
      const output = p.output as { type?: unknown; value?: unknown; ok?: unknown } | undefined;
      const verdict = (output?.type === "json" ? output.value : output) as { ok?: unknown } | undefined;
      if (isErrorToolOutput(p.output) || verdict?.ok === false) ids.add(p.toolCallId);
    }
  }
  return ids;
}

/**
 * The server's history as chat items, in order: user rows, the agent's prose,
 * a line for each file it wrote, and a card for each question it asked (so a
 * question still waiting survives a reload and stays answerable). A question
 * the SDK rejected, and a write the bundle refused, drop out.
 *
 * Ids are position-stable (`h<n>`), so the same history projects to the same
 * ids every time.
 */
export function historyItems(history: ConversationMessage[]): ChatItem[] {
  const out: ChatItem[] = [];
  const failed = failedToolCalls(history);
  for (const m of history) {
    if (m.role === "user") {
      const text = contentText(m.content).trim();
      if (!text) continue;
      out.push({
        kind: "user",
        id: `h${out.length}`,
        text,
        state: "sent",
        ...(m.author ? { author: m.author.displayName } : {}),
        ...(m.prototypeFeedback ? { prototypeFeedback: m.prototypeFeedback } : {}),
      });
      continue;
    }
    if (m.role !== "assistant") continue;
    // Prose and tool calls in the order the agent made them.
    let prose = "";
    const flush = () => {
      if (prose.trim()) out.push({ kind: "agent", id: `h${out.length}`, turnId: "history", text: prose.trim() });
      prose = "";
    };
    if (typeof m.content === "string") prose = m.content;
    for (const p of parts(m.content)) {
      if (p.type === "text" && typeof p.text === "string") {
        prose += p.text;
        continue;
      }
      if (p.type !== "tool-call" || typeof p.toolName !== "string") continue;
      const toolCallId = typeof p.toolCallId === "string" ? p.toolCallId : "";
      if (failed.has(toolCallId)) continue;
      if (isQuestionTool(p.toolName)) {
        const questions = parseQuestionsInput(p.toolName, p.input);
        if (!questions) continue;
        flush();
        out.push({ kind: "question", id: `h${out.length}`, turnId: "history", toolCallId, questions, streaming: false });
      } else if (isFileMutationTool(p.toolName)) {
        const path = (p.input as { path?: unknown } | undefined)?.path;
        if (typeof path !== "string") continue;
        flush();
        out.push({
          kind: "activity",
          id: `h${out.length}`,
          turnId: "history",
          toolCallId,
          op: opForTool(p.toolName),
          path,
          state: "done",
        });
      }
    }
    flush();
  }
  return out;
}
