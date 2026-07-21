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
 * Add or update a card in place, keyed by (role, toolCallId) — ONE definition
 * of the replay-dedupe rule for tool and question cards alike: a re-fold of
 * the same frame hits the existing card instead of duplicating it, and a
 * blank toolCallId always appends (never a false in-place hit). `merge` lets
 * a caller keep fields the fresh fold doesn't know (a recorded answer).
 */
function upsertByToolCallId<R extends "tool" | "question">(
  key: string,
  role: R,
  msg: WithoutId<Extract<ChatMessage, { role: R }>>,
  merge?: (existing: Extract<ChatMessage, { role: R }>) => Partial<ChatMessage>,
): void {
  const messages = [...load(key)];
  const withKey = msg as { toolCallId: string };
  const idx = withKey.toolCallId
    ? messages.findIndex(
        (m) => m.role === role && (m as { toolCallId?: string }).toolCallId === withKey.toolCallId,
      )
    : -1;
  if (idx >= 0) {
    const existing = messages[idx]!;
    messages[idx] = {
      ...existing,
      ...msg,
      ...(merge ? merge(existing as Extract<ChatMessage, { role: R }>) : {}),
      id: existing.id,
    } as ChatMessage;
  } else {
    messages.push({ ...msg, id: nextId() } as ChatMessage);
  }
  persist(key, messages);
}

/**
 * Add or update a tool card: a "streaming" card ("Creating <file>") is written
 * the moment the path resolves mid tool-input, then flipped to "done" on the
 * tool-result — same card, no duplicate row.
 */
export function upsertToolMessage(
  key: string,
  msg: WithoutId<Extract<ChatMessage, { role: "tool" }>>,
): void {
  upsertByToolCallId(key, "tool", msg);
}

/**
 * Add a question card; a replay-from-0 re-fold keeps any answer already
 * recorded on the existing card.
 */
export function upsertQuestionMessage(
  key: string,
  msg: WithoutId<Extract<ChatMessage, { role: "question" }>>,
): void {
  upsertByToolCallId(key, "question", msg, (existing) =>
    existing.answer ? { answer: existing.answer } : {},
  );
}

/**
 * Record the card's answer (ADR-0012) — the card flips read-only. Keyed by the
 * message id (unique per card), NOT toolCallId: blank toolCallIds are legal on
 * cards and must never gang-answer every blank-id card at once.
 */
export function answerQuestion(
  key: string,
  messageId: string,
  answer: { selected: string[]; freeText?: string },
): void {
  if (!messageId) return;
  persist(
    key,
    load(key).map((m) => (m.role === "question" && m.id === messageId ? { ...m, answer } : m)),
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
