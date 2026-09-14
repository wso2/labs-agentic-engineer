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

import { frontierIndex, type SpineStage } from "./stage";

// THE GLANCE — one run's flow as a single line, plus the one stage worth words.
//
// The rail this sits over renders every stage in full: each one's note, its
// issues, its log. That is the right surface for reading a finished run, and the
// wrong one for answering "what is happening right now", which is what a reader
// arrives with while a run is live. Six expanded stages make the reader do the
// scanning; the glance does it for them and keeps the rest one click away.
//
// Nothing here re-derives a stage. The states come from `sessionStages` and
// `provisioningStage` exactly as the rail gets them — this only decides which
// one is NOW and what "the rest" collapses to.

export interface GlanceStage {
  stage: SpineStage;
  /** Its number in the run's one flow, matching the rail's numbering. */
  step: number;
}

export interface RunGlance {
  stages: GlanceStage[];
  /**
   * Index into `stages` of the stage the NOW panel narrates, or null when the
   * flow has nothing left to say (every stage done).
   */
  nowIndex: number | null;
  /** The stages after `nowIndex` — the quiet "then …" tail. */
  ahead: GlanceStage[];
}

/**
 * Collapse a run's stages into the glance.
 *
 * NOW is the rail's frontier — the FIRST stage that is not done, whether it is
 * running, waiting on the platform, or stopped needing a human. `frontierIndex`
 * is where that rule lives, shared with the build page's header pill so the two
 * surfaces cannot name different stages as the current one.
 *
 * `stepFrom` keeps the numbers identical to the rail's, so the glance and the
 * rail never disagree about which stage is "3".
 */
export function buildGlance(stages: SpineStage[], stepFrom = 1): RunGlance {
  const numbered = stages.map(
    (stage, i): GlanceStage => ({ stage, step: stepFrom + i }),
  );

  const nowIndex = frontierIndex(stages);

  return {
    stages: numbered,
    nowIndex,
    ahead: nowIndex === null ? [] : numbered.slice(nowIndex + 1),
  };
}

/**
 * What a stage is doing while it RUNS, said as the platform would say it.
 *
 * A generic "Coding agent — running" makes the reader carry the meaning; the
 * whole point of a now-first surface is that the headline already is the
 * meaning. Keyed by stage id, and every id falls back to the generic form, so
 * a stage this map has not learned yet still reads correctly.
 */
const ACTIVE_HEADLINE: Record<string, string> = {
  // provisioningStage emits id "provision" — not "provisioning" — and an
  // unmatched id silently falls back to the generic form.
  provision: "Standing up this version's connections",
  agent: "Coding agent is writing code",
  pr: "Waiting on the agent's pull request",
  merge: "Merging the agent's work",
  builds: "Building the components the merge touched",
  deploy: "Rolling out to the cluster",
};

/**
 * The NOW headline — what is happening, as a sentence.
 *
 * The stage's own `note` says what the stage DOES; this says what its current
 * state MEANS, so the panel leads with the state and follows with the detail
 * rather than making the reader infer the first from the second.
 */
export function glanceHeadline(stage: SpineStage): string {
  switch (stage.state) {
    case "active":
      return ACTIVE_HEADLINE[stage.id] ?? `${stage.name} — running`;
    case "waiting":
      return `${stage.name} — waiting`;
    case "attention":
      return `${stage.name} — needs a human`;
    case "failed":
      return `${stage.name} — stopped`;
    default:
      return stage.name;
  }
}

/**
 * How a stage still ahead is named in the one-line tail.
 *
 * Bare stage names ("pull request → merge → builds") read as a list of nouns;
 * naming the ACTOR turns the tail into the sentence it is meant to be — who
 * does what next, and why the run ends where it does.
 */
const AHEAD_PHRASE: Record<string, string> = {
  agent: "the agent writes the code",
  pr: "the agent opens a pull request",
  merge: "the platform merges it",
  builds: "one build per component the diff touches",
  deploy: "a green build deploys itself",
};

/** The quiet "then …" tail: everything still ahead, in one sentence. */
export function aheadSentence(ahead: GlanceStage[]): string {
  if (ahead.length === 0) return "";
  return ahead
    .map((entry) => AHEAD_PHRASE[entry.stage.id] ?? entry.stage.name.toLowerCase())
    .join(" → ");
}
