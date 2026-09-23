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
import { expenseApproval as model } from "../testing/fixtures";
import { annotationFor, pinsOnScreen, type PrototypeAnnotation } from "./annotations";
import { labelOf } from "./labels";
import { initialPrototypeView, reducePrototypeView } from "./viewState";

describe("annotationFor", () => {
  it("names the screen, flow, state and the selection in the order it was made", () => {
    let view = initialPrototypeView(model, { flow: "flow.approve", state: "state.failed" });
    view = reducePrototypeView(model, view, { type: "ENTER_ANNOTATE" });
    view = reducePrototypeView(model, view, { type: "TOGGLE_SELECTION", componentId: "stat.overdue" });
    view = reducePrototypeView(model, view, { type: "TOGGLE_SELECTION", componentId: "btn.export" });
    expect(annotationFor(view, "Too busy")).toEqual({
      prototypeSchemaVersion: 1,
      screenId: "screen.queue",
      flowId: "flow.approve",
      stateId: "state.failed",
      componentIds: ["stat.overdue", "btn.export"],
      request: "Too busy",
    });
  });

  it("is a whole-screen request with nothing selected, and a null flow outside one", () => {
    const view = initialPrototypeView(model, {});
    expect(annotationFor(view, "x")).toMatchObject({ componentIds: [], flowId: null });
  });
});

describe("pinsOnScreen", () => {
  const a = (id: string, screenId: string, componentIds: string[]): PrototypeAnnotation => ({
    id,
    prototypeSchemaVersion: 1,
    screenId,
    flowId: null,
    stateId: "state.default",
    componentIds,
    request: "r",
  });

  it("numbers each component by its requests' queue positions, on this screen only", () => {
    const pins = pinsOnScreen(
      [a("1", "screen.queue", ["btn.export"]), a("2", "screen.detail", ["btn.approve"]), a("3", "screen.queue", ["btn.export", "stat.overdue"]), a("4", "screen.queue", [])],
      "screen.queue",
    );
    expect(Object.fromEntries(pins)).toEqual({ "btn.export": [1, 3], "stat.overdue": [3] });
  });
});

describe("labelOf", () => {
  it("reads what the component shows, wherever it sits on the screen", () => {
    expect(labelOf(model, "screen.queue", "btn.export")).toBe("Export");
    expect(labelOf(model, "screen.queue", "stat.overdue")).toBe("Overdue");
    expect(labelOf(model, "screen.queue", "queue.expenses")).toBe("Expenses");
    expect(labelOf(model, "screen.queue", "expense.1042")).toBe("Maya Fernando");
    expect(labelOf(model, "screen.queue", "heading.queue")).toBe("Approval queue");
  });

  it("falls back to the ID for anything the screen does not hold", () => {
    expect(labelOf(model, "screen.queue", "btn.approve")).toBe("btn.approve");
  });
});
