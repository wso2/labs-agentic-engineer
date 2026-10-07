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
// own agent on its own thread; everywhere else, the Issue card over it
// included, is the project's main chat. The contract names only the issues
// view (`ChatView`): the main chat is the absence of one, so "main" is never
// sent on the wire.

export type ChatView = "main" | components["schemas"]["ChatView"];

/**
 * The view whose chat the user is talking to from here: the Issues Page with
 * no card open, or with its own Questions card open (the card answers this
 * chat's questions).
 */
export function chatViewFor(page: ProjectPage, card: ProjectCard | null): ChatView {
  return page === "issues" && (card === null || card === "questions") ? "issues" : "main";
}

/** The Questions card a view's questions are answered on (ADR-0002): over the page whose chat asked. */
export function questionsPath(view: ChatView): "/projects/$projectName/questions" | "/projects/$projectName/issues/questions" {
  return view === "issues" ? "/projects/$projectName/issues/questions" : "/projects/$projectName/questions";
}

/** The page a view's Questions card closes back to: the one it is over. */
export function homePath(view: ChatView): "/projects/$projectName" | "/projects/$projectName/issues" {
  return view === "issues" ? "/projects/$projectName/issues" : "/projects/$projectName";
}

/** A view as the contract carries it: absent for the main chat. */
export function wireView(view?: ChatView): components["schemas"]["ChatView"] | undefined {
  return view === "issues" ? "issues" : undefined;
}

/**
 * A turn's body in a view. The Issues agent has no spec room and no scope (the
 * server refuses both on a view turn), so its turn is the user's words and the
 * view; the main chat's body goes as built.
 */
export function viewTurnBody(view: ChatView, body: TurnBody): TurnBody {
  const wire = wireView(view);
  return wire ? { instruction: body.instruction, view: wire } : body;
}
