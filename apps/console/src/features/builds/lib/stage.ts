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

import type { StatusTone } from "../../../components/StatusChip";

// The vocabulary every stage on the run's rail shares. There is ONE rail per run
// — provisioning, then each build session's stages, numbered straight through —
// so there is one vocabulary, in one file.

/**
 * How far a stage has got.
 *
 * There is deliberately no "not read" member. Every stage on the rail is on
 * screen, so every stage's facts are fetched; a state meaning "nobody looked"
 * existed only for the collapsed sessions this rail replaced.
 */
export type StageState =
  | "waiting"
  | "active"
  | "done"
  | "attention"
  | "failed";

export interface SpineStage {
  id: string;
  /** The stage's name, as the console says it. */
  name: string;
  /** WHO is acting. Half of a build session is the platform's own work, and
   *  naming the actor is what stops a platform wait reading as a hung agent. */
  actor: string;
  state: StageState;
  /** One sentence: what this stage waits for, or what it did. */
  note: string;
  /** A learned fact worth showing beside the name — a PR number, a merge SHA. */
  fact?: string;
  /**
   * Where the fact LIVES, when it is a reference to something outside the
   * console — a pull request's page on the host.
   *
   * It is always a URL the platform recorded, never one the console assembled:
   * a link built here from a repo URL and a number would encode the host's URL
   * grammar and the repo row's clone-URL spelling into this file, and be wrong
   * everywhere at once when either changes. No recorded URL, no link — the fact
   * still renders, as text.
   */
  factHref?: string;
}

const STATE_TONE: Record<StageState, StatusTone> = {
  waiting: "neutral",
  active: "info",
  done: "success",
  attention: "warning",
  failed: "error",
};

export function stageTone(state: StageState): StatusTone {
  return STATE_TONE[state];
}

/**
 * Where a rail has got to: the index of the FIRST stage that is not done, or
 * null when every stage is.
 *
 * The rail is a sequence, so the first unfinished stage IS the current one —
 * nothing after it has run. Loudness does not reorder it: a failed stage is
 * already the first non-done one.
 *
 * It lives here, in the vocabulary every rail shares, because three surfaces
 * ask it — the glance's NOW panel, its "then …" tail, and the build page's
 * header pill — and three spellings of "which stage is now" is three chances
 * for one page to contradict itself.
 */
export function frontierIndex(stages: SpineStage[]): number | null {
  const at = stages.findIndex((stage) => stage.state !== "done");
  return at === -1 ? null : at;
}

// Loudest first: a stage that failed is the story even when three others are
// healthy, and something a human has to act on outranks the platform working.
const STATE_RANK: StageState[] = ["failed", "attention", "active", "waiting", "done"];

/** The loudest state in a set — how a section summarises its own rows. */
export function loudestState(states: StageState[]): StageState {
  for (const state of STATE_RANK) {
    if (states.includes(state)) return state;
  }
  return "waiting";
}
