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
import { scopeOfBody, turnBody, turnScopeFor, type TurnScope } from "./turnScope";

const approvals = { id: "F2", name: "Approvals", path: "specs/requirements/features/F2-approvals.md" };

describe("turnScopeFor (where the user is)", () => {
  it("is the feature open in the spec card", () => {
    expect(turnScopeFor("spec", approvals)).toEqual({ kind: "feature", featureId: "F2" });
  });

  it("is the design review on the design card, and on the prototypes beside it", () => {
    expect(turnScopeFor("design", null)).toEqual({ kind: "design" });
    expect(turnScopeFor("prototype", null)).toEqual({ kind: "design" });
  });

  it("is the whole product anywhere else in the project", () => {
    expect(turnScopeFor("spec", null)).toEqual({ kind: "product" });
    expect(turnScopeFor(null, null)).toEqual({ kind: "product" });
    expect(turnScopeFor("build", approvals)).toEqual({ kind: "product" });
    expect(turnScopeFor("configure", null)).toEqual({ kind: "product" });
  });
});

describe("the scope on the wire", () => {
  it.each<[TurnScope, object]>([
    [turnScopeFor("spec", approvals), { scope: { kind: "feature", feature: "F2" } }],
    [{ kind: "product" }, {}],
    [{ kind: "design" }, { scope: { kind: "design-review" } }],
  ])("sends %o as %o, and reads it back", (scope, fields) => {
    const body = turnBody("Tighten this", scope);
    expect(body).toEqual({ instruction: "Tighten this", collab: true, ...fields });
    const read = scopeOfBody(body);
    expect(read.kind).toBe(scope.kind);
    if (scope.kind === "feature") expect(read).toEqual({ kind: "feature", featureId: "F2" });
  });
});

describe("a prototype turn on the wire", () => {
  const feedback = {
    prototypeHash: "a".repeat(64),
    component: "expense-web",
    requests: [{ screenId: "screen.claim", roleId: "manager", stateId: "state.default", elementIds: ["btn.approve"], text: "Move it right" }],
  };

  it("sends no scope: the command says what it is about", () => {
    expect(turnBody("/prototype expense-web", { kind: "prototype" })).toEqual({
      instruction: "/prototype expense-web",
      collab: true,
    });
  });

  it("carries a review's requests as prototypeFeedback, unchanged", () => {
    const body = turnBody("/prototype expense-web", { kind: "prototype", feedback });
    expect(body).toEqual({ instruction: "/prototype expense-web", collab: true, prototypeFeedback: feedback });
    expect(scopeOfBody(body)).toEqual({ kind: "product" });
  });
});
