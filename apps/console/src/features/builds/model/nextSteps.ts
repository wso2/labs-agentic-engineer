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

import type { NoteAction } from "../../agent-chat/chatLog";
import type { FeatureResults, ValidationOutcome } from "./validation";

// What a finished build offers next, on the Validation and Build cards and in
// the chat, so no finished state is a dead end: a failing scenario offers its
// fix (or the build already fixing it); a version that passes offers the next
// feature to interview, or the newer version when there is one. Fix lives on
// the version's Validation card, beside the scenarios it fixes, so the Build
// card points there instead. The chat keeps out of the way: one line when a
// build starts, one when it ends, each linking to the build.

export type NextStep =
  | { kind: "fix"; label: string; version: string; stories: string[] }
  | { kind: "story"; label: string; story: string }
  | { kind: "open"; label: string; version: string }
  | { kind: "validation"; label: string; version: string }
  | { kind: "interview"; label: string; featureId: string };

export interface NextInput {
  version: string;
  outcome: ValidationOutcome;
  /** The repair build started for this version, if any, and whether it still runs. */
  fixedBy: { version: string; building: boolean } | null;
  /** The newest version built. */
  latest: string;
  /** The next feature to interview, by Next up's rule; null when none waits. */
  nextInterview: { id: string; name: string } | null;
}

export interface NextSteps {
  /** A line before the actions: "Fixing in v1.1…". */
  note: string | null;
  steps: NextStep[];
}

function failingStories(outcome: ValidationOutcome): string[] {
  return [...new Set(outcome.failing.flatMap((f) => (f.story ? [f.story] : [])))];
}

export function nextSteps(input: NextInput): NextSteps {
  const { version, outcome, fixedBy, latest, nextInterview } = input;
  if (outcome.failing.length > 0) {
    if (fixedBy) {
      return {
        note: fixedBy.building ? `Fixing in ${fixedBy.version}…` : `Fixed in ${fixedBy.version}.`,
        steps: [{ kind: "open", label: `Open ${fixedBy.version}`, version: fixedBy.version }],
      };
    }
    const stories = failingStories(outcome);
    const first = stories[0];
    return {
      note: null,
      steps: [
        {
          kind: "fix",
          label: stories.length === 1 ? `Fix ${first}` : `Fix ${outcome.failing.length} failing scenarios`,
          version,
          stories,
        },
        ...(first && stories.length === 1 ? [{ kind: "story" as const, label: `Open ${first} in Spec`, story: first }] : []),
      ],
    };
  }
  if (latest !== version) {
    return { note: `${latest} is the latest build.`, steps: [{ kind: "open", label: `Open ${latest}`, version: latest }] };
  }
  return {
    note: null,
    steps: [
      ...(nextInterview
        ? [{ kind: "interview" as const, label: `Interview ${nextInterview.name}`, featureId: nextInterview.id }]
        : []),
    ],
  };
}

/**
 * The next steps as the Build card offers them: a fix, and the failing story,
 * are the Validation card's, so a failing version points to its validation.
 */
export function buildCardSteps(next: NextSteps, version: string): NextSteps {
  if (!next.steps.some((s) => s.kind === "fix")) return next;
  return { note: next.note, steps: [{ kind: "validation", label: `See what failed in ${version}`, version }] };
}

/** The chat's line once a build has started, linking to it on the card. */
export function startedNote(
  version: string,
  names: string[],
  repair: { stories: string[] } | null,
): { text: string; actions: NoteAction[] } {
  const what = repair ? `it fixes ${repair.stories.join(", ")}` : names.join(", ");
  return {
    text: `${version} is building: ${what}. Watch it here.`,
    actions: [{ kind: "open-build", label: `Open ${version}`, version }],
  };
}

function scenarios(n: number): string {
  return `${n} scenario${n === 1 ? "" : "s"}`;
}

/** The chat's line once a build has finished: its result, and what it offers next that the chat can do. */
export function finishedNote(
  version: string,
  groups: FeatureResults[],
  outcome: ValidationOutcome,
  next: NextSteps,
): { text: string; actions: NoteAction[] } {
  const open: NoteAction = { kind: "open-build", label: `Open ${version}`, version };
  if (outcome.failing.length > 0) {
    const failing = groups
      .filter((g) => g.passed < g.scenarios.length)
      .map((g) => `${g.name}: ${g.passed} of ${scenarios(g.scenarios.length)} pass`);
    const stories = outcome.failing.map((f) => f.story ?? f.name);
    return {
      text: `${version} is built. ${failing.join("; ")}; ${stories.join(", ")} ${stories.length === 1 ? "fails" : "fail"}.`,
      actions: [open],
    };
  }
  const interviews = next.steps.flatMap((s): NoteAction[] =>
    s.kind === "interview" ? [{ kind: "interview", label: s.label, featureId: s.featureId }] : [],
  );
  return {
    text: `${version} is built and all ${scenarios(outcome.total)} pass. It runs in development.`,
    actions: [open, ...interviews],
  };
}
