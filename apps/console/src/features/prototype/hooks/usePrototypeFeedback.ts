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

import { useCallback, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { PROTOTYPE_COMMAND } from "@aep/contracts/commands";
import { prototypeArtifactPath } from "@aep/prototype-model";
import { useSession } from "../../../auth/SessionContext";
import {
  addMessage,
  chatKeyFor,
  claimSendInFlight,
  clearFailedSends,
  settleUserMessage,
} from "../../agent-chat/chatStore";
import { conversationKeys, fetchCurrentConversationId } from "../../agent-chat/api/conversations";
import { ConversationRotatedError, startCollabTurn } from "../../agent-chat/api/turns";
import { useCurrentAuthor } from "../../agent-chat/currentUser";
import { followTurnToEnd } from "../../agent-chat/followTurn";
import { prototypeKeys } from "../api/keys";
import type { PrototypeAnnotation } from "../model/annotations";

export interface PrototypeFeedback {
  /** The queued requests, in the order they were added. */
  annotations: PrototypeAnnotation[];
  add: (annotation: Omit<PrototypeAnnotation, "id">) => void;
  remove: (id: string) => void;
  /** Send the whole queue as ONE `/prototype` turn. False when it did not land. */
  sendAll: () => Promise<boolean>;
  /** A batch is on its way: dispatching, or its turn is running. */
  sending: boolean;
  /** Why the last Send all did not land; the queue is still there to retry. */
  error: string | null;
  /** False until the project's thread is known; nothing can be sent before it. */
  ready: boolean;
}

const FAILED = "The feedback turn did not finish, so the prototype was not revised. Your requests are still queued — send them again when you're ready.";

/**
 * The Annotate batch for one web-application's prototype (#817): the queue the
 * reviewer builds, and the one Send all that posts it.
 *
 * The batch rides a single `/prototype` turn as its typed `prototypeFeedback`
 * field — the instruction is exactly the command, nothing is written into it.
 * The turn is COMMITTED-TRUTH, not room-scoped: the review page reads the
 * prototype from git, so the rewrite must be in git when the turn reports
 * completed, or the one refresh below would read the old file.
 *
 * It is recorded in the project's chat like any send (a row now, the stream
 * folded into the log), and its end decides the queue's fate: completed →
 * invalidate the prototype read ONCE and clear the queue; failed, or an end
 * this browser never saw → keep the queue and invalidate nothing.
 *
 * The queue lives in this hook's state: it survives re-renders and a refreshed
 * model, not leaving the page.
 */
export function usePrototypeFeedback(projectName: string, component: string): PrototypeFeedback {
  const { orgHandle } = useSession();
  const author = useCurrentAuthor();
  const queryClient = useQueryClient();
  const [annotations, setAnnotations] = useState<PrototypeAnnotation[]>([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inFlight = useRef(false);
  // The same query the chat panel resolves its thread with, by the same key.
  const conversation = useQuery({
    queryKey: conversationKeys.current(projectName),
    queryFn: () => fetchCurrentConversationId(projectName),
    staleTime: Infinity,
  });
  const conversationId = conversation.data;

  const add = useCallback((annotation: Omit<PrototypeAnnotation, "id">) => {
    setAnnotations((queue) => [...queue, { ...annotation, id: crypto.randomUUID() }]);
  }, []);
  const remove = useCallback((id: string) => setAnnotations((queue) => queue.filter((a) => a.id !== id)), []);

  const sendAll = useCallback(async (): Promise<boolean> => {
    if (inFlight.current || !conversationId || annotations.length === 0) return false;
    inFlight.current = true;
    setSending(true);
    setError(null);
    const batch = annotations;
    const chatKey = chatKeyFor(orgHandle ?? "default", projectName);
    const messageId = addMessage(chatKey, {
      role: "user",
      content: PROTOTYPE_COMMAND,
      status: "in_flight",
      author,
      createdAt: Date.now(),
    });
    const release = claimSendInFlight(chatKey);
    try {
      let turnId: string;
      try {
        turnId = await startCollabTurn(projectName, conversationId, PROTOTYPE_COMMAND, [], false, undefined, {
          prototypePath: prototypeArtifactPath(component),
          annotations: batch,
        });
      } catch (err) {
        settleUserMessage(chatKey, messageId, { failed: true });
        const message = err instanceof Error ? err.message : "Failed to reach the agent.";
        addMessage(chatKey, { role: "error", content: message });
        setError(message);
        if (err instanceof ConversationRotatedError) {
          void queryClient.invalidateQueries({ queryKey: conversationKeys.current(projectName) });
        }
        return false;
      } finally {
        release();
      }
      clearFailedSends(chatKey);
      settleUserMessage(chatKey, messageId, { turnId });
      const end = await followTurnToEnd(chatKey, projectName, turnId);
      if (end !== "completed") {
        setError(FAILED);
        return false;
      }
      // Only the batch that was sent leaves the queue — a request added while
      // the turn ran was not part of it.
      const sent = new Set(batch.map((a) => a.id));
      setAnnotations((queue) => queue.filter((a) => !sent.has(a.id)));
      await queryClient.invalidateQueries({ queryKey: prototypeKeys.file(projectName, component) });
      return true;
    } finally {
      inFlight.current = false;
      setSending(false);
    }
  }, [annotations, author, component, conversationId, orgHandle, projectName, queryClient]);

  return { annotations, add, remove, sendAll, sending, error, ready: Boolean(conversationId) };
}
