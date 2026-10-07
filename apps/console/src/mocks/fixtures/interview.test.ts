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
