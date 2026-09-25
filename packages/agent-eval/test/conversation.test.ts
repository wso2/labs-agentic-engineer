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

import { describe, expect, it } from "vitest";
import { runConversation } from "../src/conversation.js";

const SCENARIO = {
  id: "SC-001",
  criteria: [],
  brief: { goal: "Book a hotel in London", facts: { dates: "25th December" }, withholds: ["dates"] },
  rubric: { mustCover: [{ id: "MC-1", must: "asks for dates", weight: 1 }], mustNot: [] },
};

describe("runConversation", () => {
  it("runs turns until the user closes, and returns a readable transcript", async () => {
    const ask = async (msgs: { content: string }[]) =>
      msgs.length === 1 ? "Which dates are you travelling?" : "Here are two options.";

    const t = await runConversation({ scenario: SCENARIO as never, ask });

    expect(t.text).toContain("User: Book a hotel in London");
    expect(t.text).toContain("Agent: Which dates are you travelling?");
    expect(t.text).toContain("User: 25th December");
    expect(t.turns).toBeGreaterThanOrEqual(2);
  });

  // An agent that never stops asking must not run forever on the org's key.
  it("stops at maxTurns even if the agent keeps asking", async () => {
    const ask = async () => "And what dates?";
    const t = await runConversation({ scenario: SCENARIO as never, ask, maxTurns: 3 });
    expect(t.turns).toBe(3);
  });

  it("passes the WHOLE history to the agent each turn, not just the last message", async () => {
    const seen: number[] = [];
    const ask = async (msgs: unknown[]) => {
      seen.push(msgs.length);
      return "Which dates?";
    };
    await runConversation({ scenario: SCENARIO as never, ask, maxTurns: 3 });
    expect(seen).toEqual([1, 3, 5]);
  });
});
