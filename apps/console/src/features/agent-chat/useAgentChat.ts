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

import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { projectKeys } from "../projects/api/keys.js";
import {
  addMessage,
  clearFailedSends,
  chatKeyFor,
  dropTurnOutput,
  ensureUserMessage,
  getMessages,
  replaceMessages,
  claimSendInFlight,
  claimStreamFold,
  hasStreamFold,
  settleUserMessage,
  setTurnStatus,
  subscribe,
  type ChatMessage,
  type StartingUserMessage,
} from "./chatStore.js";
import {
  ConversationRotatedError,
  getActiveTurn,
  startCollabTurn,
  type TurnStatus,
} from "./api/turns.js";
import {
  conversationKeys,
  fetchCurrentConversationId,
  rotateCurrentConversation,
} from "./api/conversations.js";
import { attachAndFoldTurn } from "./runTurn.js";
import {
  applyConversationHistory,
  fetchConversationHistory,
} from "./useConversationLog.js";
import { useCurrentAuthor } from "./currentUser.js";

// The panel's behavior hook (#130), on the SHARED project thread (#430): the
// conversation id is server-minted and resolved per project, so every member
// reads and writes the same thread. The server's history is the truth and the
// local log is a paint-fast cache — rehydrate REPLACES it (D6) on mount, when
// a teammate's turn is noticed, and on tab refocus; the stream abort on
// unmount only detaches the VIEW — turns run detached server-side and a later
// mount re-attaches via replay.

// How often, while the panel is open, to ask whether someone ELSE started a
// turn. Push-free by design (D6): the activity-feed SSE can become a fifth
// trigger if idle-panel lag ever matters — push the signal, never the state.
const FOREIGN_TURN_POLL_MS = 12_000;

// …except while the panel has NOTHING on screen, where the same lag is the
// whole experience rather than a background refresh. That is the state a
// freshly created project lands in: the platform fires `/start` server-side
// (#562) and the panel opens before the turn exists, so the first check finds
// nothing and there is no local row to paint in the meantime.
const EMPTY_PANEL_POLL_MS = 2_000;

// How many of those fast checks to spend before settling. The fast cadence is
// for an ARRIVAL — the seconds between a panel opening and the turn it is
// waiting for appearing — not for a panel that is empty because the project
// simply has no conversation. Without a bound, a project that never used chat,
// or one just after a rotation cleared the log, polls every 2s for as long as
// the panel stays open.
const EMPTY_PANEL_FAST_POLLS = 8;

/**
 * How long to wait before asking again whether a turn is running.
 *
 * Keyed on "is there anything to look at" rather than on a project's age or an
 * arrival flag: an empty log is precisely the case where a poll interval is
 * indistinguishable from a broken product, and it stops being empty the moment
 * either the turn or its history lands. Bounded, because "empty" is also the
 * resting state of a project nobody has talked to. Exported for its own test —
 * the cadence is the fix, so it is the thing worth pinning.
 */
export function foreignTurnPollDelay(messages: ChatMessage[], pollsSoFar: number): number {
  return messages.length === 0 && pollsSoFar < EMPTY_PANEL_FAST_POLLS
    ? EMPTY_PANEL_POLL_MS
    : FOREIGN_TURN_POLL_MS;
}

/**
 * The user row that started a running turn, from the turn's own display record
 * (#562), or null when the turn carries none — a row written before the record
 * existed, where painting a blank bubble would be worse than painting nothing.
 *
 * The author rides along when the turn has an attributable one, so a teammate's
 * turn reads as theirs; without it the feed's fallback calls every unattributed
 * row "You", which is how a teammate's kickoff used to be mislabelled.
 *
 * Pure, and exported for its own test.
 */
export function userMessageForTurn(active: TurnStatus): StartingUserMessage | null {
  if (!active.instruction) return null;
  return {
    role: "user",
    content: active.instruction,
    turnId: active.turnId,
    status: "in_flight",
    ...(active.authorId
      ? {
          author: {
            id: active.authorId,
            displayName: active.authorDisplayName || active.authorId,
          },
        }
      : {}),
    createdAt: Date.parse(active.createdAt) || Date.now(),
  };
}

export interface AgentChat {
  messages: ChatMessage[];
  isSending: boolean;
  /** The turn currently streaming into this log, if any (task 3: the
   *  authoritative "running" signal for the feed, incl. re-attached turns). */
  activeTurnId: string | undefined;
  /** False until the project's thread id has resolved — sends are held. */
  conversationReady: boolean;
  /** The resolve failed after retries — the composer is disabled and the
   *  panel should say why (a focus refetch retries automatically). */
  conversationError: boolean;
  /**
   * The server's history has been read at least once for this thread.
   *
   * `conversationReady` says only that the thread has an id. An INJECTED
   * command must additionally wait for this: the whole point of the panel's
   * backstop is that it can see an exchange this browser did not take part in,
   * and until the rehydrate lands the log is empty and every such exchange is
   * invisible. A user's own words never wait on it.
   */
  historyReady: boolean;
  /**
   * Send a message, optionally with chat attachments (#428). Resolves TRUE once
   * the turn has been accepted by the server, FALSE when the send was refused
   * (a 409 turn-already-running or rotated thread, a network failure, or a
   * rejected multipart body).
   *
   * The caller needs that answer: attachment bytes live nowhere but the
   * browser, so clearing the composer on a refused send would cost the user a
   * re-pick of every file from disk (ADR-0019). It resolves when the turn
   * STARTS, not when it finishes — the stream is folded in the background.
   */
  send: (instruction: string, files?: File[]) => Promise<boolean>;
  /** Rotate to a fresh PROJECT-WIDE thread (header action, D4). The caller
   *  owns the confirmation — this just performs the rotation. */
  newConversation: () => void;
}

export function useAgentChat(
  org: string,
  projectName: string,
  options: { collab?: boolean } = {},
): AgentChat {
  const collab = options.collab ?? true;
  const chatKey = chatKeyFor(org, projectName);
  const messages = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => getMessages(chatKey),
  );
  const [isSending, setIsSending] = useState(false);
  const [historyReady, setHistoryReady] = useState(false);
  const [activeTurnId, setActiveTurnId] = useState<string | undefined>(undefined);
  const abortRef = useRef<AbortController | null>(null);
  // Mirrors the attachment state for the poll/refocus triggers: a rehydrate
  // REPLACE must never run mid-fold, or it would clobber streamed partials.
  //
  // Since #606 the log has other writers (the spec workspace, the overview's
  // spec card), so both guards are ALSO published to the store — see
  // `markAttached` / `markSending` below. The refs stay because this hook reads
  // them synchronously for its own concurrency control ("am I already
  // attached"), which is a different question from "may anyone replace the log".
  const attachedRef = useRef(false);
  // A local send whose dispatch has not answered yet. `attachedRef` cannot
  // cover this window: the turn row exists server-side the moment StartTurn
  // returns 202, but this client does not learn its id until the response
  // lands — so the poll could find that turn, attach it, and fold it, while
  // `send` was about to fold the very same stream. Two concurrent folds, and
  // (since the optimistic row has no turn id yet) a second user bubble beside
  // the one the user is already looking at.
  const pendingStartRef = useRef(false);
  const author = useCurrentAuthor();
  const queryClient = useQueryClient();

  // The ONLY writers of the two guards above, so the local ref and the store
  // claim it publishes cannot drift apart. Both are idempotent: setting a guard
  // that is already set keeps the single outstanding claim.
  const foldClaimRef = useRef<(() => void) | null>(null);
  const sendClaimRef = useRef<(() => void) | null>(null);
  const markAttached = useCallback(
    (on: boolean) => {
      attachedRef.current = on;
      if (on) foldClaimRef.current ??= claimStreamFold(chatKey);
      else {
        foldClaimRef.current?.();
        foldClaimRef.current = null;
      }
    },
    [chatKey],
  );
  const markSending = useCallback(
    (on: boolean) => {
      pendingStartRef.current = on;
      if (on) sendClaimRef.current ??= claimSendInFlight(chatKey);
      else {
        sendClaimRef.current?.();
        sendClaimRef.current = null;
      }
    },
    [chatKey],
  );
  // A chatKey change (rotation, project switch) strands any claim held under
  // the old key — release it rather than leaving that log permanently unreplaceable.
  useEffect(
    () => () => {
      foldClaimRef.current?.();
      foldClaimRef.current = null;
      sendClaimRef.current?.();
      sendClaimRef.current = null;
    },
    [chatKey],
  );

  // The project's current thread id (#430) — server-resolved, shared by every
  // member. staleTime Infinity: the id only moves on rotation, and rotation
  // paths update this cache explicitly (newConversation below, and the
  // conversation_rotated 409 in send). refetchOnWindowFocus "always" bypasses
  // that freshness (a focus refetch normally skips fresh data) — it is the
  // RECOVERY path when the resolve failed outright: with no id the main
  // effect never runs, so none of its triggers exist, and without this the
  // composer would stay disabled until a remount.
  const conversation = useQuery({
    queryKey: conversationKeys.current(projectName),
    queryFn: () => fetchCurrentConversationId(projectName),
    staleTime: Infinity,
    refetchOnWindowFocus: "always",
  });
  const conversationId = conversation.data;

  // A committed turn changed spec files in git; refetch the project cache tree
  // (spec file list included) so views keyed off committed truth — e.g. the
  // Architecture tab's "Designing…" state — settle instead of serving the
  // staleTime-Infinity snapshot until a reload.
  //
  // The THREAD is the same kind of stale, and the commit is the same signal.
  // The fold appends turn rows into `chatStore` live, but the authority that
  // replaces them is the `staleTime: Infinity` history query — refreshed
  // only on a cold mount, a refocus, or a poll that finds a RUNNING turn. None
  // of those is "the turn just finished", so a turn ending while the user sits
  // still left the cache one turn behind, and the next surface to mount
  // `useConversationLog` replayed that snapshot over the live rows. On a new
  // project the snapshot is the EMPTY thread the panel read before the
  // platform's kickoff dispatched, so the replay emptied the log outright and
  // the spec workspace fell back to "Nothing written yet" plus a Retry while
  // the agent stood waiting on the questions it had just asked.
  //
  // READ the thread here rather than merely invalidating it. An invalidation
  // leaves the stale entry in place, and react-query serves that entry to the
  // next observer synchronously while it refetches behind it — so the spec
  // workspace would still paint one frame of the pre-turn snapshot, wiping the
  // question and flashing "Nothing written yet" before the fresh read restored
  // it. Reading AT the commit means the cache already holds post-turn truth by
  // the time any surface mounts, and there is no window to paint.
  //
  // Through the shared cache entry (#606), so this is the same request every
  // other surface would have made rather than a second one racing it. Failure
  // is a no-op: `fetchConversationHistory` answers null and the existing
  // triggers (refocus, cold mount) remain the recovery path.
  const onTurnCommitted = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: projectKeys.detail(projectName) });
    if (conversationId) {
      void fetchConversationHistory(queryClient, projectName, conversationId);
    }
  }, [queryClient, projectName, conversationId]);

  useEffect(() => {
    if (!conversationId) return;
    const ac = new AbortController();
    abortRef.current = ac;

    // Pre-#430 identity cleanup: the FE-minted conversation uuid is dead —
    // nothing reads it, and leaving it would leak one key per project forever.
    try {
      localStorage.removeItem(`aep.chat.conv.${org}.${projectName}`);
    } catch {
      // best-effort
    }

    // D6: server truth replaces the local paint-cache — ALWAYS, not only when
    // the cache is empty (the pre-#430 rule, correct for a private thread and
    // wrong for a shared one: a teammate's messages otherwise never appear).
    // Skipped while attached to a stream; the fold is appending live and the
    // replay already reconstructed the thread.
    //
    // LOCAL-ONLY rows survive the replace: a failed send's user row (the
    // typed text) and its error row exist nowhere server-side, and washing
    // them out on the next refocus would silently destroy the one copy of a
    // message the user still needs to retry. They are cleared by the next
    // SUCCESSFUL send (clearFailedSends) or by a rotation — without that bound
    // a failure stayed pinned below newer turns forever, reading as a retry.
    const rehydrate = async () => {
      // Not while a local send is mid-dispatch either. The optimistic row has
      // no turn id yet and the server has no record of it, so it survives
      // neither the REPLACE nor the `localOnly` filter (which keeps error and
      // failed rows only) — a tab switch inside the ~2s dispatch would drop
      // the user's own message, and `settleUserMessage` would then no-op onto
      // an id that no longer exists.
      if (attachedRef.current || pendingStartRef.current) return;
      // Through the SHARED cache entry (#606), not a bare fetch: the spec
      // workspace and the overview's spec card observe the same key, so this
      // read serves them too instead of racing a second identical request.
      const history = await fetchConversationHistory(
        queryClient,
        projectName,
        conversationId,
      );
      if (ac.signal.aborted) return;
      applyConversationHistory(chatKey, history);
    };

    // Attach to a running turn (ours or a teammate's, or the platform's own
    // kickoff) and fold it live.
    const attach = async (active: TurnStatus) => {
      if (attachedRef.current) return;
      const turnId = active.turnId;
      markAttached(true);
      setIsSending(true);
      setActiveTurnId(turnId);
      dropTurnOutput(chatKey, turnId); // replay-from-0 re-adds it all
      // Paint who started this turn and what they said, BEFORE folding a
      // single frame of the agent's reply. The turn row carries both (#562);
      // nothing else can, because the conversation store does not persist a
      // turn's transcript until it ends. No-ops for a turn this browser sent —
      // its own row already carries the id.
      const startedBy = userMessageForTurn(active);
      if (startedBy) ensureUserMessage(chatKey, startedBy);
      try {
        await attachAndFoldTurn(chatKey, projectName, turnId, ac.signal, onTurnCommitted);
      } catch {
        // surfaced by the fold's error handling; the view just settles
      } finally {
        markAttached(false);
        if (!ac.signal.aborted) {
          setIsSending(false);
          setActiveTurnId(undefined);
        }
      }
    };

    // A running turn is attachable only when it belongs to THIS thread. A
    // turn from another thread means a teammate rotated (their turn runs in
    // the new current thread, or a demoted thread's turn is still draining):
    // folding it here would splice one thread's narration into another's log
    // — so re-resolve instead, and the effect re-run on the new id picks it
    // up properly.
    const attachableOrReResolve = (active: { conversationId: string }): boolean => {
      if (active.conversationId === conversationId) return true;
      void queryClient.invalidateQueries({
        queryKey: conversationKeys.current(projectName),
      });
      return false;
    };

    // Mount (or rotation — a new id re-runs this effect): rehydrate, then
    // re-attach to a still-running chat turn (replay from 0).
    void (async () => {
      await rehydrate();
      if (ac.signal.aborted) return;
      // Flagged even when the read FAILED. It reports "we have asked", not
      // "we know" — and holding an injected command forever on a history the
      // server will not serve would strand the only recovery a stalled project
      // has. The engaged check still runs on whatever did land.
      setHistoryReady(true);
      const active = await getActiveTurn(projectName);
      if (ac.signal.aborted || !active || active.status !== "running") return;
      if (active.useCase !== "general") return; // another flow's turn
      if (!attachableOrReResolve(active)) return;
      // A quiet anchored send (#666) may already be folding this turn; a
      // second fold would interleave the same stream on top of itself.
      if (hasStreamFold(chatKey)) return;
      await attach(active);
    })();

    // Foreign-turn trigger: a turn this browser did not send — a teammate's,
    // or the platform's own kickoff at project creation. The rehydrate first
    // pulls the user message (attribution for the feed's composer lock), then
    // the attach folds the stream live.
    //
    // Self-rescheduling rather than a fixed interval so the cadence can follow
    // how much the panel has to show. On a project that was just created the
    // panel mounts BEFORE the kickoff has dispatched, so the one mount check
    // finds nothing and the user watches an empty pane until the next tick —
    // which at the idle cadence is most of a minute's worth of nothing at the
    // one moment the product is trying to prove it is working.
    let pollTimer: ReturnType<typeof setTimeout> | undefined;
    let pollsSoFar = 0;
    const scheduleNextPoll = () => {
      if (ac.signal.aborted) return;
      pollTimer = setTimeout(
        runPoll,
        foreignTurnPollDelay(getMessages(chatKey), pollsSoFar),
      );
      pollsSoFar += 1;
    };
    const runPoll = () => {
      if (ac.signal.aborted) return;
      if (attachedRef.current || pendingStartRef.current || hasStreamFold(chatKey)) {
        scheduleNextPoll();
        return;
      }
      void (async () => {
        try {
          const active = await getActiveTurn(projectName);
          if (ac.signal.aborted || attachedRef.current || hasStreamFold(chatKey)) return;
          if (!active || active.status !== "running" || active.useCase !== "general") return;
          if (!attachableOrReResolve(active)) return;
          await rehydrate();
          if (!ac.signal.aborted) await attach(active);
        } finally {
          scheduleNextPoll();
        }
      })();
    };
    scheduleNextPoll();

    // Refocus trigger: the user was away; catch the thread up — and re-check
    // WHICH thread is current first, because a teammate may have rotated
    // while this panel idled (staleTime Infinity never re-asks on its own,
    // and rehydrating the demoted id would keep succeeding forever). If the
    // id moved, the invalidation re-runs this effect on the new one; if not,
    // the manual rehydrate covers ordinary freshness.
    const onVisible = () => {
      if (document.visibilityState !== "visible") return;
      void queryClient.invalidateQueries({
        queryKey: conversationKeys.current(projectName),
      });
      void rehydrate();
    };
    document.addEventListener("visibilitychange", onVisible);

    return () => {
      ac.abort();
      if (pollTimer !== undefined) clearTimeout(pollTimer);
      document.removeEventListener("visibilitychange", onVisible);
      markAttached(false);
      markSending(false);
      setHistoryReady(false);
      setIsSending(false);
      setActiveTurnId(undefined);
    };
  }, [chatKey, org, projectName, conversationId, onTurnCommitted, queryClient, markAttached, markSending]);

  const send = useCallback(
    async (instruction: string, files: File[] = []): Promise<boolean> => {
      const text = instruction.trim();
      if (!text || isSending || !conversationId) return false;
      setIsSending(true);
      markSending(true);
      // Names only, and only when there are any: a message without attachments
      // must persist exactly the row shape it did before this feature.
      const attachments = files.length > 0 ? files.map((f) => f.name) : undefined;
      // The row goes up NOW, not after the dispatch answers. `startCollabTurn`
      // resolves the repo, the workspace ref, the org's Anthropic key, two git
      // heads and two snapshot extracts before it returns a turn id — and the
      // user watching their own message not appear for all of that cannot tell
      // a slow platform from a dropped message. It carries no turnId yet;
      // `buildFeed` links a turn block to the nearest preceding user message
      // when it cannot link by id, which is exactly this window.
      const messageId = addMessage(chatKey, {
        role: "user",
        content: text,
        status: "in_flight",
        author,
        createdAt: Date.now(),
        ...(attachments ? { attachments } : {}),
      });
      let turnId: string;
      try {
        turnId = await startCollabTurn(projectName, conversationId, text, files, collab);
      } catch (err) {
        // The row the user is already looking at becomes the failed one —
        // adding a second copy beside it would read as two sends.
        markSending(false);
        settleUserMessage(chatKey, messageId, { failed: true });
        addMessage(chatKey, {
          role: "error",
          content: err instanceof Error ? err.message : "Failed to reach the agent.",
        });
        // conversation_rotated (#430): the resolved id went stale under us —
        // a teammate rotated. Re-resolve; the effect re-runs on the new id
        // and rehydrates the fresh thread (the failed rows above live only
        // in the local cache and wash out with it, correctly).
        if (err instanceof ConversationRotatedError) {
          void queryClient.invalidateQueries({
            queryKey: conversationKeys.current(projectName),
          });
        }
        setIsSending(false);
        return false;
      }
      setActiveTurnId(turnId);
      // The send worked, so any earlier failure in this thread is history. Those
      // rows are local-only and the rehydrate re-appends them AFTER the server
      // history on every refocus, so leaving them would keep a stale failure
      // pinned below newer turns — looking like a retry that never happened.
      // The row just added is in_flight, not failed, so it survives this.
      clearFailedSends(chatKey);
      settleUserMessage(chatKey, messageId, { turnId });
      // The fold runs DETACHED from here on. The caller is unblocked as soon as
      // the turn is accepted, which is the only fact it needs to decide whether
      // to clear the composer — waiting for the terminal would hold the
      // attachment cards on screen for the whole turn.
      void (async () => {
        const signal = abortRef.current?.signal ?? new AbortController().signal;
        // Someone else got there first. `pendingStartRef` holds the POLL off,
        // but the mount's own rehydrate → getActiveTurn runs before it is set
        // — an auto-sent seed fires the moment `conversationReady` flips,
        // concurrently with that sequence — so `attach` can already own this
        // turn. Folding it a second time would replay the whole stream on top
        // of itself after `dropTurnOutput`.
        markSending(false);
        if (attachedRef.current) {
          setIsSending(false);
          return;
        }
        markAttached(true);
        try {
          await attachAndFoldTurn(chatKey, projectName, turnId, signal, onTurnCommitted);
        } catch {
          if (!signal.aborted) {
            setTurnStatus(chatKey, turnId, "failed");
            addMessage(chatKey, {
              role: "error",
              content: "Lost the agent's stream — reopen the panel to re-attach.",
            });
          }
        } finally {
          markAttached(false);
          if (!signal.aborted) {
            setIsSending(false);
            setActiveTurnId(undefined);
          }
        }
      })();
      return true;
    },
    [chatKey, projectName, conversationId, isSending, author, onTurnCommitted, queryClient, collab, markAttached, markSending],
  );

  // Rotation (D4): a PROJECT-WIDE act — the demoted thread stops being current
  // for every member; theirs catch up via the 409 fence + their own triggers.
  // The local clear is cosmetic (the effect re-run on the new id rehydrates an
  // empty thread anyway); errors surface in the log like a failed send.
  const newConversation = useCallback(() => {
    void (async () => {
      try {
        await rotateCurrentConversation(queryClient, projectName);
        replaceMessages(chatKey, []);
      } catch (err) {
        addMessage(chatKey, {
          role: "error",
          content: err instanceof Error ? err.message : "Failed to start a new conversation.",
        });
      }
    })();
  }, [chatKey, projectName, queryClient]);

  return {
    messages,
    isSending,
    activeTurnId,
    conversationReady: Boolean(conversationId),
    historyReady,
    conversationError: conversation.isError,
    send,
    newConversation,
  };
}
