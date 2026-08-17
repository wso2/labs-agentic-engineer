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

// The empty pane's ladder (#485): every rung is a statement about the agent,
// and the bottom rung always answers — a waiting state gated on a resolved
// stage is a blank pane at exactly the moment the user is most lost.

import { describe, expect, it } from "vitest";
import { specWaitingLine } from "./waitingLine";

describe("specWaitingLine", () => {
  it("says the agent is writing while an agent peer is live in the room", () => {
    expect(specWaitingLine({ stage: "reading", writing: true })).toBe(
      "The agent is drafting the PRD…",
    );
  });

  it("says the agent is reading through every stage of the interview", () => {
    for (const stage of ["starting", "reading", "questions"] as const) {
      expect(specWaitingLine({ stage, writing: false })).toBe(
        "The agent is looking at your idea…",
      );
    }
  });

  // The rung that makes this a ladder rather than a gate: with no stage
  // resolved and nothing writing, the pane still says something true.
  it("always resolves to something, with no stage at all", () => {
    for (const stage of ["none", "failed"] as const) {
      expect(specWaitingLine({ stage, writing: false })).toBe(
        "The agent is working on your spec. This view fills in as it writes.",
      );
    }
  });

  it("never asks the user to select a file they do not have", () => {
    for (const stage of ["none", "starting", "reading", "questions", "failed"] as const) {
      for (const writing of [true, false]) {
        expect(specWaitingLine({ stage, writing })).not.toMatch(/Select a file/);
      }
    }
  });
});
