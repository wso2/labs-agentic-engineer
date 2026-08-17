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

import type { components } from "../../../generated/aep-api";
import type { InterviewState } from "../../agent-chat/interviewState";

type ProjectStatus = components["schemas"]["ProjectStatus"];

/**
 * A new project's FIRST RUN: the backend started the spec interview at create
 * (#485), and this is the one place the console decides what to say about it.
 *
 * Two sources, because neither is sufficient. The kickoff report knows the
 * seconds between the claim and the first turn — and knows a kickoff that
 * died, which no client-side fact can — while the live chat log knows what the
 * turn is actually doing right now, seconds before the 5s status poll would.
 *
 * The console starts NOTHING here. Every state below is a report, and the only
 * action any of them offers is Retry, which asks the backend to do its own job
 * again.
 */
export type SpecFirstRunStage =
  | "none"
  | "starting"
  | "reading"
  | "questions"
  | "failed";

export interface SpecFirstRunView {
  stage: SpecFirstRunStage;
  /**
   * An interview is open. The CTA reads "Continue spec" whenever this holds —
   * "Generate spec" is only honest when nothing is running.
   */
  open: boolean;
  /** Questions waiting on the user (`questions` stage only). */
  questions: number;
  /** The card's sentence, in the agent's voice; empty when there is nothing to say. */
  line: string;
  /** Why the kickoff failed; empty otherwise. */
  reason: string;
}

const NOT_RUNNING: SpecFirstRunView = {
  stage: "none",
  open: false,
  questions: 0,
  line: "",
  reason: "",
};

export function specFirstRunView(
  status: ProjectStatus | undefined,
  interview: InterviewState,
): SpecFirstRunView {
  // Optional chaining all the way down: this runs against a poll that may not
  // have landed, and an older backend serves a spec stage with no kickoff at
  // all — neither is a first run, and neither may throw on a rendering path.
  const kickoff = status?.spec?.kickoff;
  // A spec exists: whatever the kickoff did, the document is the answer now —
  // and the backend stops reporting on it for the same reason.
  if (!kickoff || status?.spec?.exists || kickoff.status === "none") return NOT_RUNNING;

  if (kickoff.status === "failed") {
    return {
      stage: "failed",
      open: false,
      questions: 0,
      line: "The spec interview could not be started.",
      reason: kickoff.reason,
    };
  }
  // Live facts first: the log is seconds ahead of the status poll, and a user
  // watching questions arrive must not be told the agent is "starting".
  if (interview.pendingQuestions > 0) {
    return {
      stage: "questions",
      open: true,
      questions: interview.pendingQuestions,
      line:
        interview.pendingQuestions === 1
          ? "Agent has 1 question about your idea"
          : `Agent has ${interview.pendingQuestions} questions about your idea`,
      reason: "",
    };
  }
  if (interview.turnRunning) {
    return {
      stage: "reading",
      open: true,
      questions: 0,
      line: "Agent is looking at your idea",
      reason: "",
    };
  }
  // Claimed, and nothing has reached this browser yet — the turn row may not
  // even exist. Saying so is the whole reason the kickoff is reported at all:
  // the alternative reads as an untouched project.
  return {
    stage: "starting",
    open: true,
    questions: 0,
    line: "Agent is starting the interview",
    reason: "",
  };
}
