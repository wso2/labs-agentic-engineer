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

import type { ShellScope } from "../shell/scope";
import type { ChatView } from "./chatView";

/**
 * Whether questions a turn of this browser's asked in `projectName` open the
 * Questions card by themselves, given where the user is (ADR-0002): on that
 * project's overview, or a card over it (Spec, Design, Prototype), where
 * interviews and design reviews happen. Anywhere else (the project's Builds,
 * Deploy, Issues, another project, the org) the chat's pointer is enough: the
 * card would move the user off the page they are working on. Not again when
 * the card is already open.
 *
 * The Issues chat's questions open their own card over the Issues page, and
 * only from the Issues page itself (not from an Issue card over it): the
 * overview's rule is the main chat's.
 */
export function opensQuestionsCard(scope: ShellScope, projectName: string, view: ChatView = "main"): boolean {
  if (scope.kind !== "project" || scope.projectName !== projectName) return false;
  if (view === "issues") return scope.page === "issues" && scope.card === null;
  return scope.page === "overview" && scope.card !== "questions";
}
