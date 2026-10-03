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
import type { TurnBody } from "../../features/agent-chat/turnScope";
import { prototypeFeedbackProblem } from "./conversation";

// The mock refuses a turn's prototypeFeedback where aep-api does (400).

const feedback = {
  prototypeHash: "a".repeat(64),
  component: "expense-web",
  requests: [{ screenId: "screen.claim", roleId: "manager", stateId: "state.default", elementIds: [], text: "Bigger total" }],
};

describe("prototypeFeedbackProblem", () => {
  it.each<[string, TurnBody]>([
    ["a turn without feedback", { instruction: "Hello", collab: true }],
    ["/prototype for the batch's component", { instruction: "/prototype expense-web", collab: true, prototypeFeedback: feedback }],
    ["a bare /prototype", { instruction: "/prototype", collab: true, prototypeFeedback: feedback }],
  ])("takes %s", (_, body) => {
    expect(prototypeFeedbackProblem(body)).toBeNull();
  });

  it.each<[string, TurnBody]>([
    ["another command", { instruction: "/design F1", collab: true, prototypeFeedback: feedback }],
    ["another component", { instruction: "/prototype admin-web", collab: true, prototypeFeedback: feedback }],
    ["a turn outside the room", { instruction: "/prototype", prototypeFeedback: feedback }],
    ["a malformed batch", { instruction: "/prototype", collab: true, prototypeFeedback: { ...feedback, requests: [] } }],
  ])("refuses %s", (_, body) => {
    expect(prototypeFeedbackProblem(body)).not.toBeNull();
  });
});
