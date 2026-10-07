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

import type { ProjectChat } from "./chatStore";
import type { TurnScope } from "./turnScope";

// What became of a hand-off the main chat announced, and the move New Issue
// makes. The choice is this browser's: it is kept in localStorage, keyed by
// the project and the hand-off's call, and reads as pending wherever the
// browser keeps nothing (the card then offers the choice again).

export type HandOffState = "pending" | "continued" | "stayed";

const key = (projectName: string, toolCallId: string) => `aep:handoff:${projectName}:${toolCallId}`;

/** The user's choice on a hand-off; pending until they made one here. */
export function getHandOff(projectName: string, toolCallId: string): HandOffState {
  try {
    const kept = localStorage.getItem(key(projectName, toolCallId));
    return kept === "continued" || kept === "stayed" ? kept : "pending";
  } catch {
    return "pending";
  }
}

export function setHandOff(projectName: string, toolCallId: string, state: HandOffState): void {
  try {
    localStorage.setItem(key(projectName, toolCallId), state);
  } catch {
    /* not kept: the card shows the choice until the page goes */
  }
}

/** What the move asks of the Issues chat's store. */
interface IssuesChatStore {
  open: (projectName: string) => Promise<void>;
  get: (projectName: string) => ProjectChat;
  send: (projectName: string, text: string, scope: TurnScope) => Promise<boolean>;
}

/**
 * Take a request on to the Issues chat: once its conversation has loaded
 * (`open` resolves when it is ready or failed), send the request as its next
 * message when it can take one; otherwise (a turn running there, a thread
 * that would not load, a send the server refused) put it in the Issues
 * composer, so the user's words are not lost and nothing is sent unasked.
 */
export async function continueInIssues(
  projectName: string,
  request: string,
  deps: { store: IssuesChatStore; compose: (text: string) => void },
): Promise<"sent" | "composed"> {
  const { store, compose } = deps;
  await store.open(projectName);
  const chat = store.get(projectName);
  if (chat.status === "ready" && chat.turn.phase === "idle" && (await store.send(projectName, request, { kind: "product" }))) {
    return "sent";
  }
  compose(request);
  return "composed";
}
