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
import {
  initialPrototypeView,
  reducePrototypeView,
  viewRequestOf,
  type PrototypeViewEvent,
  type PrototypeViewState,
} from "./viewState";

const start = (over: Partial<PrototypeViewState> = {}): PrototypeViewState => ({
  ...initialPrototypeView(model, {}),
  ...over,
});
const run = (s: PrototypeViewState, ...events: PrototypeViewEvent[]) =>
  events.reduce((acc, e) => reducePrototypeView(model, acc, e), s);

const activate = (componentId: string, action: Extract<PrototypeViewEvent, { type: "ACTIVATE" }>["action"]) =>
  ({ type: "ACTIVATE", componentId, action }) as const;

describe("initialPrototypeView", () => {
  it("opens the default screen as the first role that reaches it, in the first display state, in Preview", () => {
    expect(initialPrototypeView(model, {})).toMatchObject({
      mode: "preview",
      screenId: "screen.queue",
      roleId: "approver",
      flowId: null,
      stateId: "state.default",
      openOverlayId: null,
      selectedComponentIds: [],
    });
  });

  it("restores a shared link's screen, flow, state and mode", () => {
    expect(
      initialPrototypeView(model, { screen: "screen.mine", flow: "flow.submit", state: "state.invalid", mode: "annotate" }),
    ).toMatchObject({ screenId: "screen.mine", flowId: "flow.submit", roleId: "employee", stateId: "state.invalid", mode: "annotate" });
  });

  it("repairs unknown IDs instead of rendering nothing", () => {
    expect(initialPrototypeView(model, { screen: "screen.gone", flow: "flow.gone", state: "state.gone" })).toMatchObject({
      screenId: "screen.queue",
      flowId: null,
      stateId: "state.default",
    });
  });

  it("starts a flow on its first screen when the requested screen is not its role's", () => {
    expect(initialPrototypeView(model, { screen: "screen.queue", flow: "flow.submit" })).toMatchObject({
      screenId: "screen.new",
      roleId: "employee",
    });
  });

  it("takes the role from the screen when no flow is named", () => {
    expect(initialPrototypeView(model, { screen: "screen.reports" }).roleId).toBe("finance");
  });

  it("restores a shared link's role on a screen that role reaches", () => {
    expect(initialPrototypeView(model, { screen: "screen.detail", role: "finance" })).toMatchObject({
      screenId: "screen.detail",
      roleId: "finance",
    });
  });

  it("opens the role's first screen when the requested screen is not the role's", () => {
    expect(initialPrototypeView(model, { role: "employee" })).toMatchObject({ screenId: "screen.new", roleId: "employee" });
    expect(initialPrototypeView(model, { screen: "screen.queue", role: "employee" })).toMatchObject({
      screenId: "screen.new",
      roleId: "employee",
    });
  });

  it("ignores an unknown role", () => {
    expect(initialPrototypeView(model, { screen: "screen.detail", role: "auditor" })).toMatchObject({
      screenId: "screen.detail",
      roleId: "approver",
    });
  });

  it("lets a flow's role win over the requested one", () => {
    expect(initialPrototypeView(model, { flow: "flow.submit", role: "finance" })).toMatchObject({
      flowId: "flow.submit",
      roleId: "employee",
      screenId: "screen.new",
    });
  });
});

describe("Preview acts", () => {
  it("navigates and closes any overlay", () => {
    const s = run(start({ openOverlayId: "drawer.export" }), activate("nav.reports", { kind: "navigate", screenId: "screen.reports" }));
    expect(s.screenId).toBe("screen.reports");
    expect(s.openOverlayId).toBeNull();
  });

  it("opens and closes dialogs and drawers", () => {
    const opened = run(start(), activate("btn.export", { kind: "show-drawer", drawerId: "drawer.export" }));
    expect(opened.openOverlayId).toBe("drawer.export");
    const dialog = run(opened, activate("x", { kind: "show-dialog", dialogId: "dialog.approve" }));
    expect(dialog.openOverlayId).toBe("dialog.approve");
    expect(run(dialog, activate("btn.close", { kind: "close-overlay" })).openOverlayId).toBeNull();
    expect(run(dialog, { type: "CLOSE_OVERLAY" }).openOverlayId).toBeNull();
  });

  it("switches tabs and steps per node", () => {
    const s = run(
      start(),
      activate("tab.b", { kind: "set-tab", tabsId: "tabs.x", tabId: "tab.b" }),
      activate("btn.next", { kind: "set-step", stepperId: "stepper.new", stepId: "step.receipt" }),
    );
    expect(s.activeTabIds).toEqual({ "tabs.x": "tab.b" });
    expect(s.activeStepIds).toEqual({ "stepper.new": "step.receipt" });
  });

  it("highlights a row on select-row", () => {
    const s = run(start(), activate("link.oldest", { kind: "select-row", tableId: "queue.expenses", rowId: "expense.1038" }));
    expect(s.selectedRowIds).toEqual({ "queue.expenses": "expense.1038" });
  });

  it("ignores a navigate to a screen the model does not have", () => {
    const s = start();
    expect(run(s, activate("x", { kind: "navigate", screenId: "screen.gone" }))).toEqual(s);
  });

  it("never selects", () => {
    const s = run(start(), { type: "TOGGLE_SELECTION", componentId: "btn.export" });
    expect(s.selectedComponentIds).toEqual([]);
  });
});

describe("Annotate only selects", () => {
  const annotating = () => run(start(), { type: "ENTER_ANNOTATE" });

  it("selects instead of acting", () => {
    const s = run(annotating(), activate("btn.export", { kind: "show-drawer", drawerId: "drawer.export" }));
    expect(s.openOverlayId).toBeNull();
    expect(s.screenId).toBe("screen.queue");
    expect(s.selectedComponentIds).toEqual(["btn.export"]);
  });

  it("multi-selects in insertion order and toggles off on a second click", () => {
    const s = run(
      annotating(),
      activate("stat.overdue", { kind: "close-overlay" }),
      activate("btn.export", { kind: "close-overlay" }),
      activate("nav.queue", { kind: "navigate", screenId: "screen.detail" }),
    );
    expect(s.selectedComponentIds).toEqual(["stat.overdue", "btn.export", "nav.queue"]);
    expect(run(s, activate("btn.export", { kind: "close-overlay" })).selectedComponentIds).toEqual([
      "stat.overdue",
      "nav.queue",
    ]);
  });

  const selected = () => run(annotating(), { type: "TOGGLE_SELECTION", componentId: "btn.export" });

  it.each<[string, PrototypeViewEvent]>([
    ["Escape", { type: "CLEAR_SELECTION" }],
    ["a screen change", { type: "NAVIGATE", screenId: "screen.detail" }],
    ["a flow change", { type: "SET_FLOW", flowId: "flow.approve" }],
    ["a display-state change", { type: "SET_STATE", stateId: "state.failed" }],
    ["a role change", { type: "SET_ROLE", roleId: "finance" }],
    ["a replaced model", { type: "MODEL_REPLACED", model }],
    ["leaving Annotate", { type: "EXIT_ANNOTATE" }],
  ])("clears the selection on %s", (_, event) => {
    expect(run(selected(), event).selectedComponentIds).toEqual([]);
  });

  it("never selects in Preview, even when asked to directly", () => {
    expect(run(start(), { type: "TOGGLE_SELECTION", componentId: "btn.export" }).selectedComponentIds).toEqual([]);
  });

  it("stays in Annotate when the model is replaced (a landed feedback turn)", () => {
    expect(run(selected(), { type: "MODEL_REPLACED", model }).mode).toBe("annotate");
  });

  it("returns to Preview on exit, where clicks act again", () => {
    const s = run(selected(), { type: "EXIT_ANNOTATE" }, activate("btn.export", { kind: "show-drawer", drawerId: "drawer.export" }));
    expect(s.mode).toBe("preview");
    expect(s.openOverlayId).toBe("drawer.export");
  });
});

describe("role, flow and display state", () => {
  it("a role change keeps a screen the role reaches, else moves to its first screen", () => {
    expect(run(start(), { type: "SET_ROLE", roleId: "finance" }).screenId).toBe("screen.queue");
    expect(run(start(), { type: "SET_ROLE", roleId: "employee" }).screenId).toBe("screen.new");
  });

  it("a role change drops a flow that belongs to another role", () => {
    const inFlow = run(start(), { type: "SET_FLOW", flowId: "flow.approve" });
    expect(run(inFlow, { type: "SET_ROLE", roleId: "employee" }).flowId).toBeNull();
    expect(run(inFlow, { type: "SET_ROLE", roleId: "approver" }).flowId).toBe("flow.approve");
  });

  it("a flow takes its role and starts on its first screen; clearing it keeps the screen", () => {
    const s = run(start(), { type: "SET_FLOW", flowId: "flow.month-end" });
    expect(s).toMatchObject({ flowId: "flow.month-end", roleId: "finance", screenId: "screen.reports" });
    expect(run(s, { type: "SET_FLOW", flowId: null })).toMatchObject({ flowId: null, screenId: "screen.reports" });
  });

  it("ignores unknown roles, flows and states", () => {
    const s = start();
    expect(run(s, { type: "SET_ROLE", roleId: "nobody" })).toEqual(s);
    expect(run(s, { type: "SET_FLOW", flowId: "flow.gone" })).toEqual(s);
    expect(run(s, { type: "SET_STATE", stateId: "state.gone" })).toEqual(s);
  });

  it("switches display state", () => {
    expect(run(start(), { type: "SET_STATE", stateId: "state.empty" }).stateId).toBe("state.empty");
  });
});

describe("MODEL_REPLACED", () => {
  it("keeps the view where the new model still has it", () => {
    const s = run(start(), { type: "SET_ROLE", roleId: "finance" }, { type: "NAVIGATE", screenId: "screen.reports" });
    expect(run(s, { type: "MODEL_REPLACED", model })).toMatchObject({ screenId: "screen.reports", roleId: "finance" });
  });

  it("falls back when the screen is gone from the new model", () => {
    const s = run(start(), { type: "NAVIGATE", screenId: "screen.detail" });
    const without = { ...model, screens: model.screens.filter((x) => x.id !== "screen.detail") };
    expect(run(s, { type: "MODEL_REPLACED", model: without }).screenId).toBe("screen.queue");
  });
});

describe("viewRequestOf", () => {
  it("carries screen, role and state always, flow when set, mode only when annotating", () => {
    expect(viewRequestOf(start())).toEqual({ screen: "screen.queue", role: "approver", state: "state.default" });
    const s = run(start(), { type: "SET_FLOW", flowId: "flow.approve" }, { type: "ENTER_ANNOTATE" });
    expect(viewRequestOf(s)).toEqual({
      screen: "screen.queue",
      role: "approver",
      state: "state.default",
      flow: "flow.approve",
      mode: "annotate",
    });
  });

  it("round-trips a chosen role through the URL", () => {
    const s = run(start(), { type: "SET_ROLE", roleId: "finance" }, { type: "NAVIGATE", screenId: "screen.detail" });
    expect(initialPrototypeView(model, viewRequestOf(s))).toMatchObject({ screenId: "screen.detail", roleId: "finance" });
  });
});
