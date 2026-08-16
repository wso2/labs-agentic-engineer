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

// Reading a machinery instruction as a human line (#485 live-testing round 2).
//
// Interview answers and the finish valve are sent as PLAIN TEXT instructions
// (@aep/agent-stream's builders) — that is the contract with the agent and it
// does not change here. What changed is the reading: the feed used to print
// the whole instruction as the user's chat message, so answering five
// questions showed a raw `Answers:` dump and the finish valve showed three
// paragraphs of directives. This module turns those two shapes into one line,
// keeping the verbatim text for the expandable detail.
//
// PURE and render-only: nothing here touches what is sent. Anything it does
// not recognize returns null and renders verbatim, so a builder that changes
// wording degrades to today's behavior instead of hiding a message.

import { ANSWER_PREFIX, ANSWERS_PREFIX } from "@aep/agent-stream";

/**
 * The finish valve's marker (`buildFinishInstruction`, issue #486). Written
 * out rather than imported because that builder ships on the #486 branch,
 * which this one does not carry yet — swap in `FINISH_PREFIX` from
 * @aep/agent-stream once the two meet. A marker that never matches costs
 * nothing: the message simply renders verbatim.
 */
const FINISH_PREFIX = "Finish — use recommendations.";

/** The finish instruction's two list headings, matched on their first word(s)
 *  so a reworded tail doesn't lose the count. */
const ANSWERED_HEADING = "Answers already given";
const UNANSWERED_HEADING = "Unanswered";

export interface UserMessageSummary {
  /** The one human line shown in place of the instruction. */
  summary: string;
  /** The instruction, verbatim — the expandable detail. */
  detail: string;
}

function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`;
}

/** The `- "…": …` entries of one block (the builders' list shape). */
function bulletCount(block: string): number {
  let n = 0;
  for (const line of block.split("\n")) {
    if (/^\s*-\s+\S/.test(line)) n += 1;
  }
  return n;
}

/** The paragraph opening with `heading`, or "" — the finish instruction is
 *  assembled from blank-line-separated parts. */
function section(text: string, heading: string): string {
  return text.split("\n\n").find((part) => part.startsWith(heading)) ?? "";
}

function finishSummary(text: string): string {
  const answered = bulletCount(section(text, ANSWERED_HEADING));
  const assumed = bulletCount(section(text, UNANSWERED_HEADING));
  if (answered > 0 && assumed > 0) {
    return `Finished — answered ${plural(answered, "question")}, applied recommendations to ${assumed} more`;
  }
  if (assumed > 0) {
    return `Finished — applied recommendations to ${plural(assumed, "remaining question")}`;
  }
  if (answered > 0) {
    return `Finished — answered ${plural(answered, "question")}, recommendations for the rest`;
  }
  return "Finished — applied the agent's recommendations";
}

/**
 * A human line for a user message that is really machinery, or null when the
 * message is the user's own words (the overwhelmingly common case).
 */
export function summarizeUserMessage(content: string): UserMessageSummary | null {
  const text = content.trim();
  if (text.startsWith(FINISH_PREFIX)) {
    return { summary: finishSummary(text), detail: content };
  }
  if (text.startsWith(ANSWERS_PREFIX)) {
    const answered = bulletCount(text.slice(ANSWERS_PREFIX.length));
    // A prefix with no list under it is not a batch — leave it verbatim rather
    // than claim a count of zero.
    return answered > 0
      ? { summary: `Answered ${plural(answered, "question")}`, detail: content }
      : null;
  }
  if (text.startsWith(ANSWER_PREFIX)) {
    return { summary: "Answered 1 question", detail: content };
  }
  return null;
}
