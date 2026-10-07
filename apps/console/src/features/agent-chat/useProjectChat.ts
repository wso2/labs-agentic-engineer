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

import { useCallback, useEffect, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { parseInterviewCommand, START_COMMAND } from "@aep/contracts/commands";
import { designKey } from "../design/api/designModel";
import { specKey } from "../spec/api/specModel";
import { applyAgentWrite } from "../spec/collab/specDoc";
import { flushSpecRoom } from "../spec/collab/specRoom";
import { fetchConversationMessages, fetchCurrentConversationId } from "./api/conversation";
import { getActiveTurn, getTurn, openTurnStream, startTurn } from "./api/turns";
import { issuesListKey } from "../issues/api/issues";
import { createChatStore, type ProjectChat } from "./chatStore";
import { questionsPath, viewTurnBody, type ChatView } from "./chatView";
import { opensQuestionsCard } from "./openQuestions";
import { shellScope } from "../shell/scope";

// The app's chat stores, on the real transport, and the React side of them:
// the project's main chat, and one more per view of the main panel.

/** The project's main chat: it writes the spec room, so a turn flushes the room first. */
export const chatStore = createChatStore({
  api: {
    conversationId: fetchCurrentConversationId,
    history: fetchConversationMessages,
    startTurn,
    activeTurn: getActiveTurn,
    turn: getTurn,
    openStream: openTurnStream,
  },
  onAgentWrite: applyAgentWrite,
  beforeTurn: flushSpecRoom,
});

/**
 * The Issues Page's chat: its own thread, running turn and turns, all in the
 * issues view. The Issues agent writes no spec, so there is nothing to apply
 * or to flush, and its turns carry no room or scope.
 */
const issuesChatStore = createChatStore({
  api: {
    conversationId: (projectName) => fetchCurrentConversationId(projectName, "issues"),
    history: fetchConversationMessages,
    startTurn: (projectName, conversationId, body) => startTurn(projectName, conversationId, viewTurnBody("issues", body)),
    activeTurn: (projectName) => getActiveTurn(projectName, "issues"),
    turn: getTurn,
    openStream: openTurnStream,
  },
});

/** The chat store of a view of the main panel; "main" is the project's main chat. */
export function chatStoreFor(view: ChatView): typeof chatStore {
  return view === "issues" ? issuesChatStore : chatStore;
}

/** A project's chat in a view (the main chat by default), kept current while the caller is mounted. */
export function useProjectChat(projectName: string, view: ChatView = "main"): ProjectChat {
  const store = chatStoreFor(view);
  const subscribe = useCallback((fn: () => void) => store.subscribe(projectName, fn), [store, projectName]);
  const chat = useSyncExternalStore(subscribe, () => store.get(projectName));
  useEffect(() => store.watch(projectName), [store, projectName]);
  return chat;
}

/** The feature an interview is running for right now ("F2"), from the turn's instruction; null otherwise. */
export function interviewingIn(chat: ProjectChat): string | null {
  const instruction = chat.turn.phase === "idle" ? undefined : chat.turn.instruction;
  return (instruction && parseInterviewCommand(instruction)?.featureId) || null;
}

/** Whether a message can go now: the chat is loaded and no turn is running. */
export function canSend(chat: ProjectChat): boolean {
  return chat.status === "ready" && chat.turn.phase === "idle";
}

/**
 * When a turn ends, read again what the agent may have changed. The spec
 * model holds each feature's stage, which an interview moves on; the design
 * model, what a design turn wrote and replied (the overview's track is worked
 * out from both); the Issues agent files issues, so its turn ends on the
 * issue list. Mounted once, in the shell, so a turn that ends with the
 * chat closed still refreshes.
 */
export function useRefreshOnTurnEnd(): void {
  const queryClient = useQueryClient();
  useEffect(() => {
    const stopMain = chatStore.onTurnEnd((projectName) => {
      void queryClient.invalidateQueries({ queryKey: specKey(projectName) });
      void queryClient.invalidateQueries({ queryKey: designKey(projectName) });
    });
    const stopIssues = issuesChatStore.onTurnEnd((projectName) => {
      void queryClient.invalidateQueries({ queryKey: issuesListKey(projectName) });
    });
    return () => {
      stopMain();
      stopIssues();
    };
  }, [queryClient]);
}

/**
 * Open the Questions card when a turn this browser started asks questions
 * (ADR-0002), as the first of them lands, where `opensQuestionsCard` allows
 * it; elsewhere the chat's pointer is all. Each batch opens it once: closing
 * the card leaves it closed until the agent asks again. Mounted once, in the
 * shell, so it holds with the chat closed.
 */
export function useOpenQuestionsWhenAsked(): void {
  const router = useRouter();
  useEffect(() => {
    const opener = (view: ChatView) => (projectName: string) => {
      const leaf = router.state.matches.at(-1);
      const scope = shellScope({
        routeId: leaf?.routeId ?? "",
        params: (leaf?.params ?? {}) as { projectName?: string },
      });
      if (!opensQuestionsCard(scope, projectName, view)) return;
      void router.navigate({ to: questionsPath(view), params: { projectName } });
    };
    const stopMain = chatStore.onQuestionsAsked(opener("main"));
    const stopIssues = issuesChatStore.onQuestionsAsked(opener("issues"));
    return () => {
      stopMain();
      stopIssues();
    };
  }, [router]);
}

/**
 * Start the kickoff the platform held for reference documents that never
 * arrived (New project's Continue without documents): `/start`, sent once the
 * project's conversation is known to be empty. The server attaches the idea
 * from the project descriptor, so the bare command is enough.
 */
export function releaseKickoff(projectName: string): void {
  chatStore.seed(projectName, START_COMMAND);
}
