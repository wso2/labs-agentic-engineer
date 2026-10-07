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
import { interviewCommand } from "@aep/contracts/commands";
import { acmeExpensesSpec } from "./spec";
import { scriptTurn } from "./interview";

describe("the mock agent's interview", () => {
  const f4 = acmeExpensesSpec.features.find((f) => f.id === "F4")!;
  const turn = (instruction: string, featureId = "F4") =>
    scriptTurn({
      instruction,
      scope: { kind: "feature", featureId },
      model: acmeExpensesSpec,
      lines: new Map(),
      progress: undefined,
      prompt: undefined,
      turnKey: "t1",
    });

  it("starts on the console's /interview command, scoped to that feature", () => {
    expect(turn(interviewCommand(f4.id)).effect).toEqual({ featureId: "F4", stage: "Interviewing" });
  });

  it("does not start for a feature other than the one in scope", () => {
    expect(turn(interviewCommand("F4"), "F2").effect).toBeUndefined();
  });
});

describe("the mock main agent's hand-off", () => {
  const product = (instruction: string) =>
    scriptTurn({
      instruction,
      scope: { kind: "product" },
      model: acmeExpensesSpec,
      lines: new Map(),
      progress: undefined,
      prompt: undefined,
      turnKey: "t1",
    });
  const handOffs = (instruction: string) =>
    product(instruction).frames.filter((f) => f.part.type === "tool-call" && f.part.toolName === "hand_off_to_issues");

  it("hands a report of something broken to Issues, in the user's words, and ends there", () => {
    const report = "The Save button on the expense form does nothing";
    const turn = product(report);
    expect(handOffs(report).map((f) => f.part.input)).toEqual([{ request: report }]);
    expect(turn.frames.find((f) => f.part.type === "tool-result")?.part.output).toEqual({
      status: "awaiting_handoff",
      view: "issues",
    });
    expect(turn.frames.at(-1)?.part.type).toBe("turn-committed");
    expect(turn.frames.filter((f) => f.part.type === "text-delta").length).toBeGreaterThan(0);
    expect(turn.reply[0]?.content).toContainEqual({
      type: "tool-call",
      toolCallId: "t1-handoff",
      toolName: "hand_off_to_issues",
      input: { request: report },
    });
  });

  it("hands off a crash or an error too, but not a question about the product", () => {
    expect(handOffs("The app crashes when I upload a receipt")).toHaveLength(1);
    expect(handOffs("I get an error on the reports page")).toHaveLength(1);
    expect(handOffs("Export is not working")).toHaveLength(1);
    expect(handOffs("What is left to do?")).toHaveLength(0);
  });
});
