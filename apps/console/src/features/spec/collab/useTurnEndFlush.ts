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

import { useEffect, useRef } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  registerDeterministicFlush,
  subscribeTurnEnd,
} from "../../agent-chat/chatStore.js";
import {
  invalidateDependencyFreshness,
  scheduleFreshnessPoll,
} from "../api/dependencyFreshness.js";
import type { CollabSpec } from "./useCollabSpec";

/**
 * Chat-path turn-end flush (#252 Task 5 — closes the 60s room→git race, see
 * the feature's "Freshness" design note). SpecView is the only place with a
 * live collab connection, so it's the only place that can force the SAME
 * room flush Build already uses (`useCollabSpec.flush()` — SpecView's
 * `onBuild` awaits it before checking preflight). The chat panel is a
 * SIBLING under AppLayout, not an ancestor/descendant, so "a turn just
 * ended" crosses subtrees via chatStore's turn-end bus rather than a shared
 * prop/context.
 *
 * On a terminal frame: if the room is connected, force-commit it then
 * invalidate the dependencies + preflight reads immediately (deterministic —
 * no waiting on the debounced committer). On a flush error, or when the room
 * isn't connected at all (offline/solo — nothing to force-commit), fall back
 * to the same refetch-on-turn-done + short poll used by
 * `useTurnEndDependencyRefresh` (the accepted residual: the debounced
 * committer recovers within its own quiet period regardless).
 *
 * While mounted, registers as a deterministic flush owner for `chatKey`
 * (`registerDeterministicFlush`) so `useTurnEndDependencyRefresh` — mounted
 * alongside the chat panel on every route, including this one when the chat
 * is open on the Spec route — knows to skip its own immediate invalidate and
 * let this hook's post-flush invalidate be the one that lands (fix wave 1,
 * Important #1: the two hooks otherwise race, since `notifyTurnEnd` fires
 * synchronously but the flush below is async).
 */
export function useTurnEndFlush(
  chatKey: string,
  projectName: string,
  collab: Pick<CollabSpec, "status" | "flush">,
): void {
  const queryClient = useQueryClient();
  // Read at fire-time, not effect-setup time: `collab` is a fresh object each
  // SpecView render (status/flush can change — e.g. connecting → connected —
  // without the chatKey/projectName identity changing), so a plain closure
  // captured at subscribe-time would go stale.
  const collabRef = useRef(collab);
  collabRef.current = collab;
  const cancelPollRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    // The flush itself, not just the claim: a turn about to be dispatched
    // lands the room through this same owner (`flushRoomBeforeDispatch`), and
    // this hook is mounted exactly where the room lives. Read through the ref
    // for the reason the turn-end path does — `collab` is a fresh object each
    // render, and a closure captured at subscribe time goes stale.
    const unregisterDeterministicFlush = registerDeterministicFlush(chatKey, () =>
      collabRef.current.flush(),
    );
    const unsubscribe = subscribeTurnEnd(chatKey, () => {
      cancelPollRef.current?.();
      cancelPollRef.current = null;
      const current = collabRef.current;
      // `current.flush()` itself already resolves immediately when
      // offline/solo (see useCollabSpec.ts's `flush`), so this status check
      // duplicates that guard. Kept deliberately (Minor #3, fix wave 1): it
      // lets the not-connected case skip straight to the
      // scheduleFreshnessPoll fallback (immediate invalidate + a 65s
      // backstop) instead of a flush()-then-invalidate that would only
      // invalidate once, with no backstop.
      if (current.status !== "connected") {
        cancelPollRef.current = scheduleFreshnessPoll(queryClient, projectName);
        return;
      }
      current
        .flush()
        .then(() => invalidateDependencyFreshness(queryClient, projectName))
        .catch(() => {
          cancelPollRef.current = scheduleFreshnessPoll(queryClient, projectName);
        });
    });
    return () => {
      unsubscribe();
      unregisterDeterministicFlush();
      cancelPollRef.current?.();
      cancelPollRef.current = null;
    };
  }, [chatKey, projectName, queryClient]);
}
