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

// The seeded chat message (#252 Task 5, leaned out by Task 17): seeded into
// the project's EXISTING collab conversation (no new conversation, no new
// useCase — turns.ts is unchanged) to ask the architect to act on ONE
// dependency. Lives alongside promptStore.ts (same "instruction the console
// hands the agent" responsibility) but in its own file since its shape is
// unrelated to the create-prompt storage promptStore.ts owns.
//
// The actual on-card/drawer BUTTON/HAMBURGER (Task 9/10/15/17's job) is what
// fires this; this file only builds the message it sends (via the
// pendingSeed plumbing in chatStore.ts + useResolveDependencyViaChat).
//
// #252 Task 17 (Q4): this message used to embed the dependency's full JSON
// entry + a resolution playbook — every field the agent needed, spelled out
// here. That duplicated context the agent already has: the chat agent reads
// the dependency's CURRENT entry straight from design.json in its own turn
// snapshot, and loads the architecture skill via collab steering
// (Task 16 added the resolve/reconsider-a-named-dependency playbook there).
// So the seed message only needs to NAME the dependency, the component, and
// the intent — everything else is redundant noise the agent already has a
// better (live) source for.

import type { components } from "../../../generated/aep-api";

type Dependency = components["schemas"]["Dependency"];

/**
 * Why a dependency's chat turn is being seeded:
 *  - "resolve": the dependency is unresolved — Select a provider or Resolve
 *    on the definition view, or the design-view card's button.
 *  - "reconsider": the dependency is already resolved — the hamburger's
 *    "Discuss in chat & modify" menu item, for a user who wants to revisit an
 *    already-made choice.
 */
export type DependencyResolutionIntent = "resolve" | "reconsider";

/** The batch flow: every open dependency, one at a time, ending back at Build. */
export const RESOLVE_ALL_DEPENDENCIES_COMMAND = "/resolve-dependencies";

/**
 * Build the seeded chat message for one dependency: names the component, the
 * dependency, and why the turn is being started. Nothing else — see the
 * file-header comment for why embedding more (the pre-Task-17 shape) is
 * unnecessary.
 */
export function buildDependencyResolutionMessage(
  componentName: string,
  dep: Dependency,
  intent: DependencyResolutionIntent,
): string {
  // Resolving runs the guided flow (the `resolve-dependency` skill), whose
  // first card asks which provider; a reconsider is a conversation about an
  // already-resolved choice, so it stays prose. The component is context for
  // the reconsider only — the dependency's definition is its own file, shared
  // by every consumer.
  return intent === "reconsider"
    ? `Let's reconsider the "${dep.name}" dependency on "${componentName}" — I'd like to look at other options.`
    : `/resolve-dependency ${dep.name}`;
}
