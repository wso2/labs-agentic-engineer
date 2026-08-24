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

// Per-project chat log + conversation identity for the AI panel (#130).
// Simplified from the legacy console's chatStore: localStorage-persisted,
// capped, transient by design (quota errors drop silently). One conversation
// uuid per (org, project), minted lazily on first send — the BFF's
// conversation store is the durable history; this log is display state.

import type { AskQuestionInput, QuestionAnswer } from "@aep/agent-stream";

export type ChatMessage =
  | {
      id: string;
      role: "user";
      content: string;
      turnId?: string;
      status: "in_flight" | "completed" | "failed";
      /**
       * Who sent this turn. Optional so logs persisted before multi-user
       * attribution (#130 follow-up) still parse — an absent author means
       * "the signed-in user" for display purposes.
       */
      author?: { id: string; displayName: string };
      /**
       * Epoch millis the message was sent, for the feed's author-line time
       * (task 3). Optional: rehydrated history carries no server timestamp,
       * and logs from before this field simply render without a time.
       */
      createdAt?: number;
      /**
       * File NAMES attached to this message (#428) — never bytes. The bytes are
       * conversation-scoped model content the platform never stores (ADR-0019),
       * so there is nothing here to re-send or re-render from; these names exist
       * only to say what went with the message.
       *
       * Optional and absent for every message without attachments, so logs
       * persisted before this field still parse. On rehydrate they come from the
       * turn journal, which is why chips survive a reload.
       */
      attachments?: string[];
    }
  | { id: string; role: "assistant"; turnId: string; content: string }
  | {
      id: string;
      role: "tool";
      turnId: string;
      /** Correlates the streaming card with its tool-result (== toolCallId). */
      toolCallId: string;
      /**
       * The tool's STREAM lifecycle: `streaming` while its input is still
       * arriving, `done` once the input stream closed (`tool-input-end`) — for a
       * file tool the input IS the body, so that is the moment the file is fully
       * written. Deliberately independent of `ok`: one step can carry several
       * file writes, and the SDK flushes every result only after the LAST call
       * in that step, so "this file is finished" and "the bundle accepted it"
       * happen at different times and cannot share a field.
       */
      status: "streaming" | "done";
      op: string;
      path: string;
      /**
       * The bundle's verdict, or `undefined` while it is still unknown (input
       * closed, result not in yet). Never assume a value here: guessing `true`
       * would show a success tick on a write the gates may still reject.
       */
      ok?: boolean;
      errorText?: string;
    }
  | {
      id: string;
      role: "question";
      turnId: string;
      /** Correlates the card with its ask_question(s) tool-call (replay-stable). */
      toolCallId: string;
      /** One entry (ask_question) or several (ask_questions) — rendered as one card. */
      questions: AskQuestionInput[];
      /** Set once answered via the card — one entry per question; flips it read-only. */
      answers?: QuestionAnswer[];
      /**
       * True while the batch is still streaming off the wire (#270 latency):
       * `questions` holds the prefix parsed so far and grows in place; submit
       * stays gated until the complete tool-call flips this false. NOTE: the
       * upsert spreads over the existing card, so finalizers must pass an
       * explicit `streaming: false` — omitting the field keeps the old value.
       */
      streaming?: boolean;
    }
  | { id: string; role: "error"; content: string };

const MAX_MESSAGES = 200;

const logs = new Map<string, ChatMessage[]>();
const listeners = new Map<string, Set<() => void>>();

function storageKey(org: string, project: string): string {
  return `aep.chat.v1.${org}.${project}`;
}

/**
 * A persisted message is renderable only if it matches the CURRENT schema.
 * Guards against a log written by an older build (the `aep.chat.v1` key is
 * shared across branches/sessions) — e.g. a `question` message from before the
 * `questions[]` shape — which would otherwise crash the card renderer. Such
 * stale entries are dropped, not migrated: they are transient display state.
 */
function isRenderable(m: ChatMessage): boolean {
  if (m.role === "question") return Array.isArray(m.questions) && m.questions.length > 0;
  return true;
}

function load(key: string): ChatMessage[] {
  const cached = logs.get(key);
  if (cached) return cached;
  let messages: ChatMessage[] = [];
  try {
    const raw = localStorage.getItem(key);
    if (raw) messages = (JSON.parse(raw) as ChatMessage[]).filter(isRenderable);
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
 * of the replay-dedupe rule for tool and question cards alike: a re-fold of the
 * same frame hits the existing card instead of duplicating it, and a blank
 * toolCallId always appends (never a false in-place hit). `merge` lets a caller
 * keep fields the fresh fold doesn't know (e.g. a recorded answer).
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
 * Add or update a tool card — same card across its whole life, no duplicate row.
 * Three writes, in order: a "streaming" card ("Creating <file>") the moment the
 * path resolves mid tool-input; `done` when that input stream closes (the body is
 * complete); then the verdict when the tool-result arrives. Each write MERGES
 * onto the previous, so a field set early survives — which is why `ok` is left
 * unset until the result actually settles it.
 */
export function upsertToolMessage(
  key: string,
  msg: WithoutId<Extract<ChatMessage, { role: "tool" }>>,
): void {
  upsertByToolCallId(key, "tool", msg);
}

/**
 * Add a question card (ADR-0012); a replay-from-0 re-fold keeps any answers
 * already recorded on the existing card.
 */
export function upsertQuestionMessage(
  key: string,
  msg: WithoutId<Extract<ChatMessage, { role: "question" }>>,
): void {
  upsertByToolCallId(key, "question", msg, (existing) =>
    existing.answers ? { answers: existing.answers } : {},
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

/**
 * Drop the local-only rows a failed send left behind — the `failed` user row and
 * any `error` rows.
 *
 * Those rows exist nowhere server-side, so the D6 rehydrate deliberately
 * re-appends them after the server history on every mount, refocus and foreign
 * turn (see useAgentChat). That preserved a message the user still needed, but
 * it had no expiry: a failure stayed pinned to the BOTTOM of the thread
 * forever, rendering after newer successful turns and reading as though
 * something were retrying.
 *
 * A successful send is the signal that the failure is history — the user
 * demonstrably got their message through — so the caller clears them there. It
 * is also safe to lose them by then: a refused send keeps the typed text AND the
 * attachment cards in the composer (ADR-0019), so the failed row stopped being
 * the only copy.
 *
 * Only clears rows this client recorded; server history is untouched. A no-op
 * when there is nothing to drop, so it never triggers a needless persist or a
 * React remount of the whole log.
 */
export function clearFailedSends(key: string): void {
  const current = load(key);
  const kept = current.filter(
    (m) => !(m.role === "error" || (m.role === "user" && m.status === "failed")),
  );
  if (kept.length === current.length) return;
  persist(key, kept);
}

export function replaceMessages(key: string, messages: ChatMessage[]): void {
  // Rows that arrive with an id KEEP it. The D6 rehydrate replaces the whole
  // log repeatedly (mount, foreign turn, refocus); minting fresh ids each
  // time made every replace a full React remount, a full localStorage
  // rewrite, and — worst — re-armed the panel's one-per-question
  // auto-navigation, which keys off ids it has already seen.
  // projectableHistory supplies position-stable ids for exactly this reason;
  // minting here is the fallback for callers that don't.
  persist(
    key,
    messages.map((m) => (m.id ? m : { ...m, id: nextId() })),
  );
}

// The conversation ID is no longer stored here (#430): it is SERVER-minted,
// stored against the project, and resolved via the conversations endpoint
// (api/conversations.ts + useAgentChat) — which is what makes the thread
// shared across every member's browser. This store keeps only the local
// display log, demoted to a paint-fast cache of the server thread.

// --- pendingSeed (#252 Task 5: "Resolve via chat") ------------------------
//
// The "Resolve via chat" action (dep card / drawer / build drawer — Task 9)
// and the chat panel (AgentChatPanel, mounted by AppLayout) are SIBLINGS
// under AppLayout, not ancestor/descendant — there's no shared React state to
// prop-drill a seed message through. This in-memory slot (never persisted:
// a one-shot signal, not chat history) is the cross-subtree handoff, mirroring
// the message-log's own Map+listeners+subscribe shape above. The panel
// consumes it exactly once (get-and-clear) so a re-render never re-sends it.

const pendingSeeds = new Map<string, string>();
const seedListeners = new Map<string, Set<() => void>>();

/** Set the one-shot seed message the panel will auto-send next time it looks. */
export function setPendingSeed(key: string, message: string): void {
  pendingSeeds.set(key, message);
  for (const fn of seedListeners.get(key) ?? []) fn();
}

/** Non-destructive read — for callers (e.g. "should the panel open?") that
 *  only need to know a seed is waiting, without consuming it. */
export function peekPendingSeed(key: string): string | null {
  return pendingSeeds.get(key) ?? null;
}

/** Get-and-clear: the seed is consumed exactly once. Also notifies seed
 *  listeners (mirroring `setPendingSeed`) — `useHasPendingSeed`'s
 *  `useSyncExternalStore` snapshot flips from true back to false only when
 *  a listener fires; without this, it would stay stuck `true` after the
 *  panel consumes the seed. */
export function consumePendingSeed(key: string): string | null {
  const msg = pendingSeeds.get(key);
  if (msg === undefined) return null;
  pendingSeeds.delete(key);
  for (const fn of seedListeners.get(key) ?? []) fn();
  return msg;
}

export function subscribeSeed(key: string, fn: () => void): () => void {
  const set = seedListeners.get(key) ?? new Set();
  set.add(fn);
  seedListeners.set(key, set);
  return () => set.delete(fn);
}

// --- Turn-end bus (#252 Task 5: freshness / turn-end flush) ---------------
//
// "A collab turn's terminal frame arrived" (runTurn.ts's `turn-committed` /
// `turn-failed`, or its severed-stream poll fallback) is broadcast here so
// BOTH the chat panel's universal refetch-on-turn-done fallback AND the spec
// view's deterministic room flush (useTurnEndFlush — only available where the
// collab connection actually lives, i.e. only while SpecView is mounted) can
// react to the same event without one owning a reference to the other.

export type TurnEndStatus = "completed" | "failed";

const turnEndListeners = new Map<string, Set<(status: TurnEndStatus) => void>>();

export function notifyTurnEnd(key: string, status: TurnEndStatus): void {
  for (const fn of turnEndListeners.get(key) ?? []) fn(status);
}

export function subscribeTurnEnd(
  key: string,
  fn: (status: TurnEndStatus) => void,
): () => void {
  const set = turnEndListeners.get(key) ?? new Set();
  set.add(fn);
  turnEndListeners.set(key, set);
  return () => set.delete(fn);
}

// --- Deterministic-flush registration (fix wave 1, Important #1) ----------
//
// `notifyTurnEnd` above dispatches to its subscribers SYNCHRONOUSLY.
// `useTurnEndFlush` (SpecView — only place the collab room lives) reacts by
// force-flushing the room then invalidating, which is necessarily ASYNC.
// `useTurnEndDependencyRefresh` (AgentChatPanel — mounted on every route) is
// the universal fallback and used to invalidate immediately and
// unconditionally. When both hooks are mounted for the same chatKey (chat
// open on the Spec route — the common case), that immediate invalidate
// landed BEFORE the deterministic flush did, briefly showing the
// freshly-resolved dependency's OLD status — defeating the point of the
// forced flush.
//
// This registry lets the fallback hook ask "is a deterministic flush owner
// live for this key right now?" and skip its own immediate invalidate when
// so, leaving that to the deterministic path's post-flush invalidate.
// Ref-counted (not a plain Set) so two overlapping registrations for the
// same key (e.g. a remount) can't have one's cleanup clear the other's.

const deterministicFlushKeys = new Map<string, number>();

/** Mark a deterministic flush listener as live for `key`. Call the returned
 *  function on unmount/cleanup. */
export function registerDeterministicFlush(key: string): () => void {
  deterministicFlushKeys.set(key, (deterministicFlushKeys.get(key) ?? 0) + 1);
  return () => {
    const remaining = (deterministicFlushKeys.get(key) ?? 1) - 1;
    if (remaining <= 0) deterministicFlushKeys.delete(key);
    else deterministicFlushKeys.set(key, remaining);
  };
}

/** True while at least one deterministic flush listener is registered for `key`. */
export function hasDeterministicFlush(key: string): boolean {
  return (deterministicFlushKeys.get(key) ?? 0) > 0;
}
