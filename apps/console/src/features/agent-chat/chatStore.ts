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

import type { AskQuestionOption } from "@aep/agent-stream";

// Per-project chat log + conversation identity for the AI panel (#130).
// Simplified from the legacy console's chatStore: localStorage-persisted,
// capped, transient by design (quota errors drop silently). One conversation
// uuid per (org, project), minted lazily on first send — the BFF's
// conversation store is the durable history; this log is display state.

export type ChatMessage =
  | {
      id: string;
      role: "user";
      content: string;
      turnId?: string;
      status: "in_flight" | "completed" | "failed";
    }
  | { id: string; role: "assistant"; turnId: string; content: string }
  | {
      id: string;
      role: "tool";
      turnId: string;
      /** Correlates the streaming card with its tool-result (== toolCallId). */
      toolCallId: string;
      /** `streaming` while the tool input is still arriving; `done` on result. */
      status: "streaming" | "done";
      op: string;
      path: string;
      ok: boolean;
      errorText?: string;
    }
  | {
      id: string;
      role: "question";
      turnId: string;
      /** Correlates the card with its ask_question tool-call (replay-stable). */
      toolCallId: string;
      question: string;
      options: AskQuestionOption[];
      multiSelect?: boolean;
      /** Set when answered via the card — renders read-only with the choice held. */
      answer?: { selected: string[]; freeText?: string };
    }
  | { id: string; role: "error"; content: string };

const MAX_MESSAGES = 200;

const logs = new Map<string, ChatMessage[]>();
const listeners = new Map<string, Set<() => void>>();

function storageKey(org: string, project: string): string {
  return `aep.chat.v1.${org}.${project}`;
}

function convKey(org: string, project: string): string {
  return `aep.chat.conv.${org}.${project}`;
}

function load(key: string): ChatMessage[] {
  const cached = logs.get(key);
  if (cached) return cached;
  let messages: ChatMessage[] = [];
  try {
    const raw = localStorage.getItem(key);
    if (raw) messages = JSON.parse(raw) as ChatMessage[];
  } catch {
    messages = [];
  }
  logs.set(key, messages);
  return messages;
}

function persist(key: string, messages: ChatMessage[]): void {
  logs.set(key, messages);
  try {
    localStorage.setItem(key, JSON.stringify(messages.slice(-MAX_MESSAGES)));
  } catch {
    // transient by design — a full quota drops history, not the session
  }
  for (const fn of listeners.get(key) ?? []) fn();
}

export function chatKeyFor(org: string, project: string): string {
  return storageKey(org, project);
}

export function getMessages(key: string): ChatMessage[] {
  return load(key);
}

export function subscribe(key: string, fn: () => void): () => void {
  const set = listeners.get(key) ?? new Set();
  set.add(fn);
  listeners.set(key, set);
  return () => set.delete(fn);
}

let counter = 0;
function nextId(): string {
  counter += 1;
  return `m-${Date.now()}-${counter}`;
}

// Omit must distribute over the message union (a plain Omit collapses it to
// the common fields).
type WithoutId<T> = T extends unknown ? Omit<T, "id"> : never;

export function addMessage(key: string, msg: WithoutId<ChatMessage>): void {
  persist(key, [...load(key), { ...msg, id: nextId() } as ChatMessage]);
}

/**
 * Add or update a tool card in place, keyed by `toolCallId`. A "streaming" card
 * ("Creating <file>") is written the moment the path resolves mid tool-input,
 * then flipped to "done" ("Created <file>") on the tool-result — same card, no
 * duplicate row. A blank toolCallId always appends (never a false in-place hit).
 */
export function upsertToolMessage(
  key: string,
  msg: WithoutId<Extract<ChatMessage, { role: "tool" }>>,
): void {
  const messages = [...load(key)];
  const idx = msg.toolCallId
    ? messages.findIndex(
        (m) => m.role === "tool" && m.toolCallId === msg.toolCallId,
      )
    : -1;
  if (idx >= 0) {
    messages[idx] = { ...messages[idx], ...msg, id: messages[idx]!.id } as ChatMessage;
  } else {
    messages.push({ ...msg, id: nextId() } as ChatMessage);
  }
  persist(key, messages);
}

/**
 * Add a question card, keyed by `toolCallId` — a replay-from-0 re-attach folds
 * the same tool-call again, and the in-place hit keeps the card (and any
 * recorded answer) instead of duplicating it. A blank toolCallId always appends.
 */
export function upsertQuestionMessage(
  key: string,
  msg: WithoutId<Extract<ChatMessage, { role: "question" }>>,
): void {
  const messages = [...load(key)];
  const idx = msg.toolCallId
    ? messages.findIndex((m) => m.role === "question" && m.toolCallId === msg.toolCallId)
    : -1;
  if (idx >= 0) {
    const existing = messages[idx] as Extract<ChatMessage, { role: "question" }>;
    messages[idx] = { ...existing, ...msg, ...(existing.answer ? { answer: existing.answer } : {}), id: existing.id };
  } else {
    messages.push({ ...msg, id: nextId() } as ChatMessage);
  }
  persist(key, messages);
}

/** Record the card's answer (ADR-0012) — the card flips read-only. */
export function answerQuestion(
  key: string,
  toolCallId: string,
  answer: { selected: string[]; freeText?: string },
): void {
  persist(
    key,
    load(key).map((m) =>
      m.role === "question" && m.toolCallId === toolCallId ? { ...m, answer } : m,
    ),
  );
}

/** Streamed text accumulates into the turn's last assistant message. */
export function appendAssistantText(
  key: string,
  turnId: string,
  delta: string,
): void {
  if (!delta) return;
  const messages = [...load(key)];
  const last = messages[messages.length - 1];
  if (last?.role === "assistant" && last.turnId === turnId) {
    messages[messages.length - 1] = { ...last, content: last.content + delta };
  } else {
    messages.push({ id: nextId(), role: "assistant", turnId, content: delta });
  }
  persist(key, messages);
}

export function setTurnStatus(
  key: string,
  turnId: string,
  status: "completed" | "failed",
): void {
  persist(
    key,
    load(key).map((m) =>
      m.role === "user" && m.turnId === turnId ? { ...m, status } : m,
    ),
  );
}

/** Remove a turn's streamed output before a replay-from-0 re-attach. */
export function dropTurnOutput(key: string, turnId: string): void {
  persist(
    key,
    load(key).filter(
      (m) => m.role === "user" || !("turnId" in m) || m.turnId !== turnId,
    ),
  );
}

export function replaceMessages(key: string, messages: ChatMessage[]): void {
  persist(
    key,
    messages.map((m) => ({ ...m, id: nextId() })),
  );
}

/** The project's conversation uuid; minted + persisted on first use. */
export function conversationIdFor(
  org: string,
  project: string,
  { create }: { create: boolean },
): string | null {
  const key = convKey(org, project);
  try {
    const existing = localStorage.getItem(key);
    if (existing) return existing;
    if (!create) return null;
    const fresh = crypto.randomUUID();
    localStorage.setItem(key, fresh);
    return fresh;
  } catch {
    return create ? crypto.randomUUID() : null;
  }
}
