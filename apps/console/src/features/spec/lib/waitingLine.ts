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

import type { SpecFirstRunStage } from "../../projects/lib/specFirstRun";

/**
 * What the spec view's main pane says while it has no file to show (#485).
 *
 * A ladder that ALWAYS resolves to something. Gating it on a resolved
 * interview stage is what produced the state this replaces: the pane
 * auto-selects `files[0]`, so it reaches this branch only when there are no
 * files at all — and "Select a file to view its content." is a instruction the
 * user cannot follow, offered at exactly the moment nothing is selectable.
 */
export function specWaitingLine(input: {
  stage: SpecFirstRunStage;
  /** An agent peer is live in the room — it is writing files right now. */
  writing: boolean;
}): string {
  if (input.writing) return "The agent is drafting the PRD…";
  if (input.stage === "starting" || input.stage === "reading" || input.stage === "questions") {
    return "The agent is looking at your idea…";
  }
  // The terminal rung. It claims nothing about a stage — it says the one thing
  // that is true whenever this pane is empty and something is under way.
  return "The agent is working on your spec. This view fills in as it writes.";
}
