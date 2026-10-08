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

import { useCallback, useEffect, useMemo, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { parseInterviewCommand, START_COMMAND } from "@aep/contracts/commands";
import { designKey } from "../design/api/designModel";
import { specKey } from "../spec/api/specModel";
import { applyAgentWrite } from "../spec/collab/specDoc";
import { flushSpecRoom } from "../spec/collab/specRoom";
import { fetchConversationMessages, fetchCurrentConversationId } from "./api/conversation";
import { getActiveTurn, getTurn, IssueClosedError, openTurnStream, startTurn } from "./api/turns";
import { issueDetailKey, issuesListKey } from "../issues/api/issues";
import { createChatStore, type ProjectChat } from "./chatStore";
import { questionsLink, viewTurnBody, type ChatView } from "./chatView";
import { markIssueClosed, onIssueClosedMark } from "./closedIssues";
import { opensQuestionsCard } from "./openQuestions";
import { shellScope } from "../shell/scope";

// The app's chat stores, on the real transport, and the React side of them:
// the project's main chat, the Issues Page's, and one per issue.

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

type ChatStore = typeof chatStore;

/** What an issue's store tells: its project, and the issue. */
type IssueListener = (projectName: string, issueNumber: number) => void;

/** One store per issue, made when first asked for; its chats are by project, so each is one project's issue. */
const issueStores = new Map<number, ChatStore>();
const issueTurnEnds = new Set<IssueListener>();
const issueQuestionsAsked = new Set<IssueListener>();
/** Told when an issue's store is made, so a list of them can follow it. */
const issueStoresMade = new Set<() => void>();

/**
 * An issue's chat: its own thread, running turn and turns, in the issue view
 * by its number. Like the Issues agent, the issue's agent writes no spec. A
 * closed issue has no thread: the server's 409 `issue_closed`, on resolving
 * the thread or on sending to it, marks the issue closed (`closedIssues`).
 */
function issueChatStore(issueNumber: number): ChatStore {
  const known = issueStores.get(issueNumber);
  if (known) return known;
  const closedOn = async <T>(projectName: string, call: Promise<T>): Promise<T> => {
    try {
      return await call;
    } catch (err) {
      if (err instanceof IssueClosedError) markIssueClosed(projectName, issueNumber);
      throw err;
    }
  };
  const store = createChatStore({
    api: {
      conversationId: (projectName) => closedOn(projectName, fetchCurrentConversationId(projectName, "issue", issueNumber)),
      history: fetchConversationMessages,
      startTurn: (projectName, conversationId, body) =>
        closedOn(projectName, startTurn(projectName, conversationId, viewTurnBody("issue", body, issueNumber))),
      activeTurn: (projectName) => getActiveTurn(projectName, "issue", issueNumber),
      turn: getTurn,
      openStream: openTurnStream,
    },
  });
  store.onTurnEnd((projectName) => {
    for (const fn of issueTurnEnds) fn(projectName, issueNumber);
  });
  store.onQuestionsAsked((projectName) => {
    for (const fn of issueQuestionsAsked) fn(projectName, issueNumber);
  });
  issueStores.set(issueNumber, store);
  for (const fn of issueStoresMade) fn();
  return store;
}

/**
 * The chat store of a view of the main panel: "main" is the project's main
 * chat; "issue" is one issue's, by its number.
 */
export function chatStoreFor(view: ChatView, issueNumber?: number): ChatStore {
  if (view === "issue") {
    if (issueNumber === undefined) throw new Error("An issue's chat needs the issue's number.");
    return issueChatStore(issueNumber);
  }
  return view === "issues" ? issuesChatStore : chatStore;
}

/** A project's chat in a view (the main chat by default; an issue's by its number), kept current while the caller is mounted. */
export function useProjectChat(projectName: string, view: ChatView = "main", issueNumber?: number): ProjectChat {
  const store = chatStoreFor(view, issueNumber);
  const subscribe = useCallback((fn: () => void) => store.subscribe(projectName, fn), [store, projectName]);
  const chat = useSyncExternalStore(subscribe, () => store.get(projectName));
  useEffect(() => store.watch(projectName), [store, projectName]);
  return chat;
}

/** An issue's chat as the threads menu lists it: the issue, and what was said there. */
export interface IssueThreadActivity {
  issueNumber: number;
  /** What the user and the agent said. */
  count: number;
}

const spokenCount = (chat: ProjectChat) => chat.items.filter((i) => i.kind === "user" || i.kind === "agent").length;

/** "7:3,12:1": the project's issue chats with something said, by number. A string, so it compares by value. */
function issueThreadsSignature(projectName: string): string {
  return [...issueStores.entries()]
    .map(([issueNumber, store]) => [issueNumber, spokenCount(store.get(projectName))] as const)
    .filter(([, count]) => count > 0)
    .sort(([a], [b]) => a - b)
    .map(([issueNumber, count]) => `${issueNumber}:${count}`)
    .join(",");
}

function subscribeIssueThreads(projectName: string, fn: () => void): () => void {
  const stops = new Map<number, () => void>();
  const follow = () => {
    for (const [issueNumber, store] of issueStores) {
      if (!stops.has(issueNumber)) stops.set(issueNumber, store.subscribe(projectName, fn));
    }
  };
  const made = () => {
    follow();
    fn();
  };
  follow();
  issueStoresMade.add(made);
  return () => {
    issueStoresMade.delete(made);
    for (const stop of stops.values()) stop();
  };
}

/**
 * The project's issue chats that hold something, in this tab: those opened
 * here (an issue's card, Continue on #N) since it loaded, lowest number first.
 */
export function useIssueThreads(projectName: string): IssueThreadActivity[] {
  const subscribe = useCallback((fn: () => void) => subscribeIssueThreads(projectName, fn), [projectName]);
  const signature = useSyncExternalStore(subscribe, () => issueThreadsSignature(projectName));
  return useMemo(
    () =>
      signature
        .split(",")
        .filter(Boolean)
        .map((entry) => {
          const [issueNumber, count] = entry.split(":").map(Number);
          return { issueNumber: issueNumber!, count: count! };
        }),
    [signature],
  );
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
 * issue list; an issue's agent comments on, edits and closes its issue, so its
 * turn ends on the list and that issue's detail, as does finding the issue's
 * thread removed (the issue was closed). Mounted once, in the shell, so a turn
 * that ends with the chat closed still refreshes.
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
    const issueChanged = (projectName: string, issueNumber: number) => {
      void queryClient.invalidateQueries({ queryKey: issuesListKey(projectName) });
      void queryClient.invalidateQueries({ queryKey: issueDetailKey(projectName, issueNumber) });
    };
    issueTurnEnds.add(issueChanged);
    const stopClosed = onIssueClosedMark((projectName, issueNumber, marked) => {
      if (marked) issueChanged(projectName, issueNumber);
    });
    return () => {
      stopMain();
      stopIssues();
      issueTurnEnds.delete(issueChanged);
      stopClosed();
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
    const opener = (view: ChatView, issueNumber?: number) => (projectName: string) => {
      const leaf = router.state.matches.at(-1);
      const scope = shellScope({
        routeId: leaf?.routeId ?? "",
        params: (leaf?.params ?? {}) as { projectName?: string; number?: string },
        search: (leaf?.search ?? {}) as { issue?: unknown },
      });
      if (!opensQuestionsCard(scope, projectName, view, issueNumber)) return;
      void router.navigate(questionsLink(projectName, view, issueNumber));
    };
    const stopMain = chatStore.onQuestionsAsked(opener("main"));
    const stopIssues = issuesChatStore.onQuestionsAsked(opener("issues"));
    const issueAsked = (projectName: string, issueNumber: number) => opener("issue", issueNumber)(projectName);
    issueQuestionsAsked.add(issueAsked);
    return () => {
      stopMain();
      stopIssues();
      issueQuestionsAsked.delete(issueAsked);
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
