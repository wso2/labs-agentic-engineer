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

import type { components } from "../../generated/aep-api";
import type { ProjectCard, ProjectPage } from "../shell/scope";
import type { TurnBody } from "./turnScope";

// A project has a chat per view of the main panel. The Issues Page has its
// own agent on its own thread, and so does each open issue, on its card: each
// is a branch of the main chat, and takes the chat panel while its page is in
// view; everywhere else is the project's main chat. The contract names
// only the branch views (`ChatView`), an issue's by its number: the main chat
// is the absence of one, so "main" is never sent on the wire.

export type ChatView = "main" | components["schemas"]["ChatView"];

/**
 * The view whose own chat this page has: the Issues Page with no card open,
 * or with its own Questions card open (the card answers this chat's
 * questions); an issue's card, or the Questions card answering that issue's
 * chat (`issueNumber`). The panel holds it in place of the main chat.
 */
export function chatViewFor(page: ProjectPage, card: ProjectCard | null, issueNumber: number | null = null): ChatView {
  if (page !== "issues") return "main";
  if (card === "issue" || (card === "questions" && issueNumber !== null)) return "issue";
  return card === null || card === "questions" ? "issues" : "main";
}

/** The Questions card a view's questions are answered on (ADR-0002): over the page whose chat asked. */
export function questionsLink(projectName: string, view: ChatView, issueNumber?: number) {
  if (view === "main") return { to: "/projects/$projectName/questions" as const, params: { projectName } };
  return {
    to: "/projects/$projectName/issues/questions" as const,
    params: { projectName },
    ...(view === "issue" && issueNumber !== undefined ? { search: { issue: issueNumber } } : {}),
  };
}

/** The page a view's Questions card closes back to: the one it is over, or the issue's card. */
export function homeLink(projectName: string, view: ChatView, issueNumber?: number) {
  if (view === "issue" && issueNumber !== undefined) {
    return { to: "/projects/$projectName/issues/$number" as const, params: { projectName, number: String(issueNumber) } };
  }
  if (view === "issues") return { to: "/projects/$projectName/issues" as const, params: { projectName } };
  return { to: "/projects/$projectName" as const, params: { projectName } };
}

/** A view as the contract's query carries it: absent for the main chat; an issue's with its number. */
export function wireQuery(
  view?: ChatView,
  issueNumber?: number,
): { view: components["schemas"]["ChatView"]; issueNumber?: number } | undefined {
  if (view === "issues") return { view };
  if (view === "issue") return { view, ...(issueNumber !== undefined ? { issueNumber } : {}) };
  return undefined;
}

/**
 * A turn's body in a view. A branch's agent has no spec room and no scope (the
 * server refuses both on a view turn), so its turn is the user's words and the
 * view (an issue's, with its number); the main chat's body goes as built.
 */
export function viewTurnBody(view: ChatView, body: TurnBody, issueNumber?: number): TurnBody {
  const wire = wireQuery(view, issueNumber);
  return wire ? { instruction: body.instruction, ...wire } : body;
}
