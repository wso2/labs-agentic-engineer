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
  chatKeyFor,
  dropTurnOutput,
  getMessages,
  replaceMessages,
  setTurnStatus,
  subscribe,
  type ChatMessage,
} from "./chatStore.js";
import {
  ConversationRotatedError,
  getActiveTurn,
  getConversationMessages,
  startCollabTurn,
} from "./api/turns.js";
import {
  conversationKeys,
  fetchCurrentConversationId,
  rotateConversation,
} from "./api/conversations.js";
import { attachAndFoldTurn } from "./runTurn.js";
import { projectableHistory } from "./history.js";
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
  send: (instruction: string) => void;
  /** Rotate to a fresh PROJECT-WIDE thread (header action, D4). The caller
   *  owns the confirmation — this just performs the rotation. */
  newConversation: () => void;
}

export function useAgentChat(org: string, projectName: string): AgentChat {
  const chatKey = chatKeyFor(org, projectName);
  const messages = useSyncExternalStore(
    useCallback((fn: () => void) => subscribe(chatKey, fn), [chatKey]),
    () => getMessages(chatKey),
  );
  const [isSending, setIsSending] = useState(false);
  const [activeTurnId, setActiveTurnId] = useState<string | undefined>(undefined);
  const abortRef = useRef<AbortController | null>(null);
  // Mirrors the attachment state for the poll/refocus triggers: a rehydrate
  // REPLACE must never run mid-fold, or it would clobber streamed partials.
  const attachedRef = useRef(false);
  const author = useCurrentAuthor();
  const queryClient = useQueryClient();

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
  const onTurnCommitted = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: projectKeys.detail(projectName) });
  }, [queryClient, projectName]);

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
    // message the user still needs to retry. They persist until the next
    // rotation clears the log.
    const rehydrate = async () => {
      if (attachedRef.current) return;
      const history = await getConversationMessages(projectName, conversationId);
      if (ac.signal.aborted || attachedRef.current || history === null) return;
      const localOnly = getMessages(chatKey).filter(
        (m) => m.role === "error" || (m.role === "user" && m.status === "failed"),
      );
      replaceMessages(chatKey, [...projectableHistory(history), ...localOnly]);
    };

    // Attach to a running turn (ours or a teammate's) and fold it live.
    const attach = async (turnId: string) => {
      if (attachedRef.current) return;
      attachedRef.current = true;
      setIsSending(true);
      setActiveTurnId(turnId);
      dropTurnOutput(chatKey, turnId); // replay-from-0 re-adds it all
      try {
        await attachAndFoldTurn(chatKey, projectName, turnId, ac.signal, onTurnCommitted);
      } catch {
        // surfaced by the fold's error handling; the view just settles
      } finally {
        attachedRef.current = false;
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
      const active = await getActiveTurn(projectName);
      if (ac.signal.aborted || !active || active.status !== "running") return;
      if (active.useCase !== "general") return; // another flow's turn
      if (!attachableOrReResolve(active)) return;
      await attach(active.turnId);
    })();

    // Foreign-turn trigger: a teammate's send while this panel idles. The
    // rehydrate first pulls their user message (attribution for the feed's
    // composer lock), then the attach folds the stream live.
    const poll = setInterval(() => {
      if (attachedRef.current || ac.signal.aborted) return;
      void (async () => {
        const active = await getActiveTurn(projectName);
        if (ac.signal.aborted || attachedRef.current) return;
        if (!active || active.status !== "running" || active.useCase !== "general") return;
        if (!attachableOrReResolve(active)) return;
        await rehydrate();
        if (!ac.signal.aborted) await attach(active.turnId);
      })();
    }, FOREIGN_TURN_POLL_MS);

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
      clearInterval(poll);
      document.removeEventListener("visibilitychange", onVisible);
      attachedRef.current = false;
      setIsSending(false);
      setActiveTurnId(undefined);
    };
  }, [chatKey, org, projectName, conversationId, onTurnCommitted, queryClient]);

  const send = useCallback(
    (instruction: string) => {
      const text = instruction.trim();
      if (!text || isSending || !conversationId) return;
      setIsSending(true);
      void (async () => {
        let turnId: string;
        try {
          turnId = await startCollabTurn(projectName, conversationId, text);
        } catch (err) {
          addMessage(chatKey, {
            role: "user",
            content: text,
            status: "failed",
            author,
            createdAt: Date.now(),
          });
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
          return;
        }
        setActiveTurnId(turnId);
        addMessage(chatKey, {
          role: "user",
          content: text,
          turnId,
          status: "in_flight",
          author,
          createdAt: Date.now(),
        });
        const signal = abortRef.current?.signal ?? new AbortController().signal;
        attachedRef.current = true;
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
          attachedRef.current = false;
          if (!signal.aborted) {
            setIsSending(false);
            setActiveTurnId(undefined);
          }
        }
      })();
    },
    [chatKey, projectName, conversationId, isSending, author, onTurnCommitted, queryClient],
  );

  // Rotation (D4): a PROJECT-WIDE act — the demoted thread stops being current
  // for every member; theirs catch up via the 409 fence + their own triggers.
  // The local clear is cosmetic (the effect re-run on the new id rehydrates an
  // empty thread anyway); errors surface in the log like a failed send.
  const newConversation = useCallback(() => {
    void (async () => {
      try {
        const fresh = await rotateConversation(projectName);
        replaceMessages(chatKey, []);
        queryClient.setQueryData(conversationKeys.current(projectName), fresh);
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
    conversationError: conversation.isError,
    send,
    newConversation,
  };
}
