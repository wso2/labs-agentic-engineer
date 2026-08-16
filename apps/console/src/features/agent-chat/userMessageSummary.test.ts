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

// Reading the machinery instructions as one human line (#485 live-testing
// round 2). The instructions themselves are built by @aep/agent-stream and are
// NOT changed by any of this — these tests feed the builders' real output in.

import { describe, expect, it } from "vitest";
import {
  buildAnswerInstruction,
  buildAnswersInstruction,
} from "@aep/agent-stream";
import { summarizeUserMessage } from "./userMessageSummary";

// The finish valve's real output (#486's buildFinishInstruction), pasted
// rather than imported: that builder ships on the branch this one doesn't
// carry yet, and the shape is exactly what the renderer must survive.
const FINISH_INSTRUCTION = [
  "Finish — use recommendations.",
  'Answers already given — treat each as the user\'s decision:\n- "Who signs in?": Anyone\n- "Photo uploads?": Yes',
  'Unanswered — apply your recommended answer to each and tag the decision *assumed* where it lands:\n- "Retention period?"\n- "Who can delete?"\n- "Payment provider?"',
  "Stop asking. Apply your recommended answer to every remaining undecided area the same way, each tagged *assumed*.",
].join("\n\n");

describe("summarizeUserMessage", () => {
  it("leaves the user's own words alone", () => {
    expect(summarizeUserMessage("please add a returns policy")).toBeNull();
    expect(summarizeUserMessage("/start")).toBeNull();
  });

  it("counts a batch of answers and keeps the instruction as the detail", () => {
    const instruction = buildAnswersInstruction([
      { question: "Who signs in?", selected: ["Anyone"] },
      { question: "Photo uploads?", selected: ["Yes"], freeText: "up to 5" },
      { question: "Payments?", selected: ["Stripe"] },
    ]);

    const summary = summarizeUserMessage(instruction);

    expect(summary?.summary).toBe("Answered 3 questions");
    expect(summary?.detail).toBe(instruction);
  });

  it("summarizes a single answer", () => {
    const instruction = buildAnswerInstruction("Who signs in?", ["Anyone"]);

    expect(summarizeUserMessage(instruction)?.summary).toBe("Answered 1 question");
  });

  it("summarizes the finish valve by what it decided", () => {
    const summary = summarizeUserMessage(FINISH_INSTRUCTION);

    expect(summary?.summary).toBe(
      "Finished — answered 2 questions, applied recommendations to 3 more",
    );
    // The directives the agent needs are preserved verbatim for the detail.
    expect(summary?.detail).toBe(FINISH_INSTRUCTION);
  });

  it("summarizes a finish with nothing answered", () => {
    const instruction = [
      "Finish — use recommendations.",
      'Unanswered — apply your recommended answer to each and tag the decision *assumed* where it lands:\n- "Retention period?"',
      "Stop asking. Apply your recommended answer to every remaining undecided area the same way, each tagged *assumed*.",
    ].join("\n\n");

    expect(summarizeUserMessage(instruction)?.summary).toBe(
      "Finished — applied recommendations to 1 remaining question",
    );
  });

  it("summarizes a bare finish — no lists to count", () => {
    expect(summarizeUserMessage("Finish — use recommendations.")?.summary).toBe(
      "Finished — applied the agent's recommendations",
    );
  });

  // Resilience: the render layer must never swallow a message it cannot read.
  it("renders verbatim when the answers prefix carries no list", () => {
    expect(summarizeUserMessage("Answers: yes to both")).toBeNull();
  });

  it("renders verbatim when the marker is absent", () => {
    expect(
      summarizeUserMessage("Wrap it up and use your recommendations"),
    ).toBeNull();
  });
});
