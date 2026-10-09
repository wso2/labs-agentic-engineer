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

import type { PrototypeManifest } from "@wso2/prototype-kit/manifest";
import type { PrototypeFeedback } from "../../agent-chat/turnScope";

// How a review's Send all reads in the chat. The turn carries typed requests
// (ids), which is what the agent needs; the user should see what they asked
// for in the words of the prototype: the screen, where in it (role and
// state), the elements and their text. Worked out from the batch the turn
// journaled, so a reload and a teammate read the same lines.

const SHOWN_TEXT = 280;

function nameOf(items: readonly { id: string; name: string }[], id: string): string {
  return items.find((i) => i.id === id)?.name ?? id;
}

function clip(text: string): string {
  return text.length > SHOWN_TEXT ? `${text.slice(0, SHOWN_TEXT).trimEnd()}…` : text;
}

/**
 * The batch as a short message: a heading, then per request its number, the
 * screen with its role and state, the elements' ids and the reviewer's text.
 * Names come from the manifest when it has them.
 */
export function feedbackSummary(feedback: PrototypeFeedback, manifest: PrototypeManifest | null): string {
  const n = feedback.requests.length;
  const lines = [`Feedback on the ${manifest?.name ?? feedback.component} prototype (${n} ${n === 1 ? "comment" : "comments"})`];
  feedback.requests.forEach((r, i) => {
    const screen = manifest ? nameOf(manifest.screens, r.screenId) : r.screenId;
    const role = manifest ? nameOf(manifest.roles, r.roleId) : r.roleId;
    const state = manifest ? nameOf(manifest.states, r.stateId) : r.stateId;
    const about = r.elementIds.length > 0 ? r.elementIds.join(", ") : "whole screen";
    lines.push(`${i + 1}. ${screen} (${role}, ${state}) — ${about}: ${clip(r.text)}`);
  });
  return lines.join("\n");
}
