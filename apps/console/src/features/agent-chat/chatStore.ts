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
import type { components } from "../../generated/aep-api";

type TurnAnchor = components["schemas"]["TurnAnchor"];

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
      /**
       * What this message was aimed at (#666) — the passage of a spec document
       * the user selected before typing it. Rendered as a frozen tag above the
       * text: it records what was pointed at WHEN THE MESSAGE WAS SENT and is
       * never re-checked against the current document (console ADR-0024), so a
       * thread read months later still says what was meant.
       *
       * Optional and absent for every ordinary chat message. On rehydrate it
       * comes from the turn journal, which is what makes the tag survive a
       * reload.
       */
      anchor?: TurnAnchor;
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
       * written. Deliberately independent of `ok`: "this file is finished" and
       * "the bundle accepted it" are two facts arriving on two frames, and a
       * card that conflated them would tick a write the write-gates can still
       * reject.
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
  | {
      id: string;
      role: "plan";
      turnId: string;
      /** Correlates the row with its declare_plan tool-call (replay-stable). */
      toolCallId: string;
      /** How many paths this call genuinely ADDED to the turn's plan — the
       *  union in planStore ignores restated entries, and a call that adds
       *  nothing never makes a row. */
      added: number;
      /** True when the plan already held entries, so the row reads as growth
       *  ("Planned N more") rather than as the plan ("Planned N documents"). */
      grew: boolean;
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
export type WithoutId<T> = T extends unknown ? Omit<T, "id"> : never;

/** The shape `ensureUserMessage` takes: a user row that names its turn. */
export type StartingUserMessage = WithoutId<Extract<ChatMessage, { role: "user" }>> & {
  turnId: string;
};

/** Append a message; returns its generated id, for callers that must update
 *  the row later (an optimistic send waiting on its turn id). */
export function addMessage(key: string, msg: WithoutId<ChatMessage>): string {
  const id = nextId();
  persist(key, [...load(key), { ...msg, id } as ChatMessage]);
  return id;
}

/**
 * Settle an optimistic user row once the dispatch answers.
 *
 * A send paints its row BEFORE the POST — that request resolves the repo, the
 * workspace ref, the Anthropic key, two git heads and two snapshot extracts
 * before it returns a turn id, and the user watching their own message not
 * appear for all of that has no way to tell a slow platform from a dropped
 * one. So the row goes up first and is settled here: stamped with the turn it
 * became, or marked failed if there wasn't one.
 *
 * Addressed by MESSAGE id rather than turn id, which is the whole point —
 * until this runs the row has no turn id to be addressed by.
 */
export function settleUserMessage(
  key: string,
  messageId: string,
  settled: { turnId: string } | { failed: true },
): void {
  persist(
    key,
    load(key).map((m) =>
      m.role === "user" && m.id === messageId
        ? "turnId" in settled
          ? { ...m, turnId: settled.turnId }
          : { ...m, status: "failed" as const }
        : m,
    ),
  );
}

/**
 * Add or update a card in place, keyed by (role, toolCallId) — ONE definition
 * of the replay-dedupe rule for tool and question cards alike: a re-fold of the
 * same frame hits the existing card instead of duplicating it, and a blank
 * toolCallId always appends (never a false in-place hit). `merge` lets a caller
 * keep fields the fresh fold doesn't know (e.g. a recorded answer).
 */
function upsertByToolCallId<R extends "tool" | "question" | "plan">(
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

/**
 * Add a plan activity row (#576, ADR-0025) — the declare_plan call surfacing
 * in the chat like any other tool step. Keyed by toolCallId so the belt-and-
 * braces double publish (tool-input-end, then tool-call) lands on one row.
 */
export function upsertPlanMessage(
  key: string,
  msg: WithoutId<Extract<ChatMessage, { role: "plan" }>>,
): void {
  upsertByToolCallId(key, "plan", msg);
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

/**
 * Append the user row that STARTED a turn, unless the log already has one.
 *
 * For a turn this browser did not send — a teammate's, or the platform's own
 * kickoff at project creation — there is no optimistic row and the server's
 * transcript will not carry one until the turn ENDS, because the conversation
 * store persists a turn's history only then. Without this the panel renders
 * the agent narrating under a blank space for the whole turn, which on a fresh
 * project is the user's entire first impression of the product.
 *
 * Idempotent on turnId, because every path that can call it runs more than
 * once: mount, the foreign-turn poll, and a re-attach after a dropped stream.
 * The sender's own row already carries the turnId, so their send no-ops here.
 *
 * The row is TRANSIENT. When the turn lands, the rehydrate replaces the log
 * with the server's history — which by then does carry the real message — and
 * this row goes with it. That is the intended handoff, not a leak.
 */
export function ensureUserMessage(key: string, msg: StartingUserMessage): void {
  const messages = load(key);
  if (messages.some((m) => m.role === "user" && m.turnId === msg.turnId)) return;
  persist(key, [...messages, { ...msg, id: nextId() } as ChatMessage]);
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


// --- chat open requests (#666: Discuss opens the panel without seeding) ----
//
// `pendingSeed` above also opens the panel, but it opens it AROUND A MESSAGE it
// then auto-sends. An anchored Discuss has already sent its own turn — with the
// anchor attached, which a seed cannot carry — so it needs the open on its own.
// Reusing the seed slot would send the text twice.
//
// A COUNT rather than a flag: two Discusses in a row must both open the panel,
// and a flag that was already true is indistinguishable from one nobody set.

const chatOpenRequests = new Map<string, number>();
const chatOpenListeners = new Map<string, Set<() => void>>();

/** Ask for the project's chat panel to be shown. */
export function requestChatOpen(key: string): void {
  chatOpenRequests.set(key, (chatOpenRequests.get(key) ?? 0) + 1);
  for (const fn of chatOpenListeners.get(key) ?? []) fn();
}

/** How many times the panel has been asked for. Monotonic; a changed value is
 *  the signal, never the number itself. */
export function peekChatOpenRequest(key: string): number {
  return chatOpenRequests.get(key) ?? 0;
}

export function subscribeChatOpen(key: string, fn: () => void): () => void {
  const set = chatOpenListeners.get(key) ?? new Set();
  set.add(fn);
  chatOpenListeners.set(key, set);
  return () => set.delete(fn);
}

// --- pendingSeed (#252 Task 5: "Resolve via chat") ------------------------
//
// The "Resolve via chat" action (dep card / drawer / build drawer — Task 9)
// and the chat panel (AgentChatPanel, mounted by AppLayout) are SIBLINGS
// under AppLayout, not ancestor/descendant — there's no shared React state to
// prop-drill a seed message through. This in-memory slot (never persisted:
// a one-shot signal, not chat history) is the cross-subtree handoff, mirroring
// the message-log's own Map+listeners+subscribe shape above. The panel
// consumes it exactly once (get-and-clear) so a re-render never re-sends it.

/**
 * A seed, plus whether the panel may refuse it.
 *
 * Most seeds are the user SPEAKING — submitted interview answers, a dependency
 * they want discussed — and refusing one would destroy the only copy of what
 * they said. `guarded` marks the exception: an INJECTED flow command, which
 * nobody typed and which is destructive to send into an open exchange. Landing
 * on an unanswered question form, a `/start` reads to the start skill as the
 * user's skip valve, so the interview is silently replaced by the agent's own
 * answers (see `agentEngaged`).
 *
 * The flag rather than a check on the text: "may this be refused" is a property
 * of WHY the seed was written, and the caller is the only party that knows it.
 */
export interface PendingSeed {
  message: string;
  /** The panel may drop this rather than send it into an open exchange. */
  guarded: boolean;
}

const pendingSeeds = new Map<string, PendingSeed>();
const seedListeners = new Map<string, Set<() => void>>();

/**
 * Set the one-shot seed message the panel will auto-send next time it looks.
 *
 * `guarded` defaults to false — the safe default for a seed carrying the user's
 * own words, which is every caller but the spec card's start CTA.
 */
export function setPendingSeed(key: string, message: string, guarded = false): void {
  pendingSeeds.set(key, { message, guarded });
  stampSeedForActivity(key);
  for (const fn of seedListeners.get(key) ?? []) fn();
}

/** Non-destructive read — for callers (e.g. "should the panel open?") that
 *  only need to know a seed is waiting, without consuming it. */
export function peekPendingSeed(key: string): PendingSeed | null {
  return pendingSeeds.get(key) ?? null;
}

/** Get-and-clear: the seed is consumed exactly once. Also notifies seed
 *  listeners (mirroring `setPendingSeed`) — `useHasPendingSeed`'s
 *  `useSyncExternalStore` snapshot flips from true back to false only when
 *  a listener fires; without this, it would stay stuck `true` after the
 *  panel consumes the seed. */
export function consumePendingSeed(key: string): PendingSeed | null {
  const seed = pendingSeeds.get(key);
  if (seed === undefined) return null;
  pendingSeeds.delete(key);
  clearSeedActivity(key);
  for (const fn of seedListeners.get(key) ?? []) fn();
  return seed;
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

// --- Log-write guards (#606) ---------------------------------------------
//
// Rehydrating the log from server truth used to belong to `useAgentChat`, so
// the two conditions that must block a REPLACE lived there as component refs.
// #606 gives the log THREE writers — the chat panel, the spec workspace and
// the overview's spec card — because a member arriving without the panel
// otherwise has no log at all, and every surface that reads one then reads an
// empty conversation.
//
// The guards move here because they describe the LOG, not any one hook: any
// writer must be able to ask "is it safe to replace this right now", and the
// answer cannot live inside one of them.
//
// Both are per chatKey, and both are FAIL-CLOSED — an unknown key is safe to
// replace, a marked key is not:
//
// - ATTACHED: a turn stream is being folded into this log. A replace would
//   wash out the streamed partials the fold has appended so far, and (worse)
//   the streaming question prefix `useRoomQuestion` is mirroring into the room.
// - SENDING: a local send is mid-dispatch. Its optimistic user row has no turn
//   id yet and the server has no record of it, so it survives neither the
//   replace nor the `localOnly` filter that keeps error/failed rows — the
//   user's own message would vanish between typing and dispatch.
//
// Ref-counted rather than boolean, for the same reason `registerDeterministicFlush`
// is: a remount can overlap two registrations for one key, and the first
// cleanup must not clear the second's claim.

const attachedFolds = new Map<string, number>();
const inFlightSends = new Map<string, number>();

function claim(counts: Map<string, number>, key: string): () => void {
  counts.set(key, (counts.get(key) ?? 0) + 1);
  notifyLocalTurnActivity(key);
  let released = false;
  return () => {
    if (released) return; // idempotent: cleanup may run twice (StrictMode)
    released = true;
    const remaining = (counts.get(key) ?? 1) - 1;
    if (remaining <= 0) counts.delete(key);
    else counts.set(key, remaining);
    notifyLocalTurnActivity(key);
  };
}

/** Mark a turn stream as being folded into `key`'s log. Call the returned
 *  function when the fold ends, however it ends. */
export function claimStreamFold(key: string): () => void {
  return claim(attachedFolds, key);
}

/**
 * Is a fold already live for `key`? The log has more than one folder now — the
 * panel, and the quiet anchored send (#666) — and two concurrent folds of the
 * same turn would interleave one stream on top of itself. Whoever would start
 * a fold asks this first; component-local refs cannot answer it.
 */
export function hasStreamFold(key: string): boolean {
  return (attachedFolds.get(key) ?? 0) > 0;
}

/** Mark a local send as mid-dispatch for `key`. Call the returned function
 *  once the dispatch resolves, successfully or not. */
export function claimSendInFlight(key: string): () => void {
  return claim(inFlightSends, key);
}

/**
 * May a caller REPLACE `key`'s log with server truth right now?
 *
 * False while a fold or a send owns it. A blocked rehydrate is DROPPED, never
 * queued: the surfaces that rehydrate all re-ask on their own triggers (mount,
 * refocus, the agent peer leaving the room), and the fold that blocked this one
 * is itself appending fresher content than the replace would have written.
 */
export function canReplaceLog(key: string): boolean {
  return !hasLiveClaims(key);
}

/** A dispatch or fold this browser currently owns for `key` — the shared base
 *  of `canReplaceLog` and `hasLocalTurnActivity`, so a future claim map cannot
 *  be added to one reading and silently missed by the other. */
function hasLiveClaims(key: string): boolean {
  return inFlightSends.has(key) || attachedFolds.has(key);
}

// --- Local turn activity (#635) -------------------------------------------
//
// `spec.agent` lags a send by the dispatch round-trip: interview answers leave
// through the seed slot the instant the question form submits, but the turn
// carrying them has no `agent_turns` row until StartTurn answers — seconds
// later, longer under load. In that window the status endpoint reads idle, the
// question form is gone, and an empty project has no files, so every signal
// the spec workspace checks said "nothing running" and it offered Retry
// against an interview mid-flight — #629's hazard surviving as a race.
//
// This browser knows better. The seed slot, the send claim and the fold claim
// chain without a gap from form-submit to the turn's terminal frame (`send()`
// releases the send claim and takes the fold claim in one synchronous
// continuation), and every failure path releases its claim — a refused
// dispatch, a severed stream, a chatKey rotation. So "any of the three is
// live" is precisely "this browser holds evidence of a turn the status
// endpoint may not report yet". The CLAIMS need no expiry timer — the signal
// collapses the moment a send is refused or a turn dies, letting Retry
// surface honestly. The SEED is the one stage with no failure path of its
// own: its sole consumer sits behind gates (the conversation id resolving,
// the history rehydrate landing) that an outage can hold shut indefinitely,
// and a seed nobody consumes would otherwise pin a working state that HIDES
// Retry — strictly worse than the gap being closed. So only the seed's
// contribution expires, on a TTL generous against a slow panel mount; the
// seed itself stays consumable, exactly as before.
//
// Browser-local by nature: a teammate's browser holds no claim for a send
// made here. Their pane recovers through the status poll as it always did —
// this only closes the gap for the member who just submitted.

const localTurnActivityListeners = new Map<string, Set<() => void>>();

function notifyLocalTurnActivity(key: string): void {
  for (const fn of localTurnActivityListeners.get(key) ?? []) fn();
}

/** How long a WAITING seed counts as turn activity. Normal consumption is
 *  near-immediate (the panel is mounted, or mounts on the seed's own signal),
 *  so this bounds only the pathological stall where the consumer's gates
 *  never open. */
export const SEED_ACTIVITY_TTL_MS = 30_000;

const seedActivitySetAt = new Map<string, number>();
const seedActivityTimers = new Map<string, ReturnType<typeof setTimeout>>();

function stampSeedForActivity(key: string): void {
  seedActivitySetAt.set(key, Date.now());
  clearTimeout(seedActivityTimers.get(key));
  // The expiry is an EDGE subscribers must hear about — without the wake-up
  // call, a pane holding "working" on this seed would keep it until some
  // unrelated re-render happened to re-read the snapshot.
  seedActivityTimers.set(
    key,
    setTimeout(() => {
      seedActivityTimers.delete(key);
      notifyLocalTurnActivity(key);
    }, SEED_ACTIVITY_TTL_MS),
  );
}

function clearSeedActivity(key: string): void {
  seedActivitySetAt.delete(key);
  clearTimeout(seedActivityTimers.get(key));
  seedActivityTimers.delete(key);
}

/** True while THIS browser holds live evidence of a turn for `key`: a seed
 *  waiting to send (within its TTL), a dispatch awaiting its turn id, or a
 *  stream being folded. */
export function hasLocalTurnActivity(key: string): boolean {
  const setAt = pendingSeeds.has(key) ? seedActivitySetAt.get(key) : undefined;
  const seedLive = setAt !== undefined && Date.now() - setAt < SEED_ACTIVITY_TTL_MS;
  return seedLive || hasLiveClaims(key);
}

/** Fires on every edge of `hasLocalTurnActivity`: seed set or consumed, claim
 *  taken or released. */
export function subscribeLocalTurnActivity(key: string, fn: () => void): () => void {
  const unsubscribeSeed = subscribeSeed(key, fn);
  const set = localTurnActivityListeners.get(key) ?? new Set();
  set.add(fn);
  localTurnActivityListeners.set(key, set);
  return () => {
    unsubscribeSeed();
    set.delete(fn);
  };
}
