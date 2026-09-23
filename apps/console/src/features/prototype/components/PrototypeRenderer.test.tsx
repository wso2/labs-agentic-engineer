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
// @vitest-environment jsdom

import { useReducer } from "react";
import { act, cleanup, fireEvent, render, screen, waitForElementToBeRemoved, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import {
  parsePrototypeModel,
  type PrototypeModelV1,
  type PrototypeNode,
  type PrototypeNodeKind,
} from "@aep/prototype-model";
import { expenseApproval, integrationMonitor } from "../testing/fixtures";
import {
  initialPrototypeView,
  reducePrototypeView,
  type PrototypeViewEvent,
  type PrototypeViewRequest,
} from "../model/viewState";
import { PrototypeRenderer } from "./PrototypeRenderer";

afterEach(cleanup);

let dispatchRef: (e: PrototypeViewEvent) => void = () => {};

function Harness({ model, request }: { model: PrototypeModelV1; request: PrototypeViewRequest }) {
  const [view, dispatch] = useReducer(
    (s: ReturnType<typeof initialPrototypeView>, e: PrototypeViewEvent) => reducePrototypeView(model, s, e),
    request,
    (r) => initialPrototypeView(model, r),
  );
  dispatchRef = dispatch;
  return <PrototypeRenderer model={model} view={view} dispatch={dispatch} />;
}

function renderModel(model: PrototypeModelV1, request: PrototypeViewRequest = {}) {
  return render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <Harness model={model} request={request} />
    </OxygenUIThemeProvider>,
  );
}

const byId = (id: string) => document.querySelector(`[data-prototype-component-id="${id}"]`);

// One screen holding every v1 node kind, visible at once.
const go = { kind: "navigate", screenId: "screen.b" } as const;
const kitchenSinkNodes: PrototypeNode[] = [
  { kind: "breadcrumbs", id: "n.crumbs", items: [{ id: "n.crumb.home", label: "Home", action: go }, { id: "n.crumb.here", label: "Here" }] },
  { kind: "heading", id: "n.heading", text: "Everything", actions: [{ id: "n.heading.btn", label: "Open drawer", action: { kind: "show-drawer", drawerId: "n.drawer" } }] },
  { kind: "text", id: "n.text", text: "Some prose" },
  { kind: "badge", id: "n.badge", label: "Pending", tone: "warning" },
  { kind: "alert", id: "n.alert", tone: "error", title: "Broken", text: "It failed" },
  { kind: "stack", id: "n.stack", direction: "row", content: [{ kind: "stat", id: "n.stat", label: "Open", value: "7" }] },
  { kind: "grid", id: "n.grid", columns: 2, content: [{ kind: "empty-state", id: "n.empty", title: "Nothing", text: "Nothing here" }] },
  {
    kind: "split",
    id: "n.split",
    left: [{ kind: "detail", id: "n.detail", title: "Claim", fields: [{ label: "Amount", value: "$1" }] }],
    right: [{ kind: "timeline", id: "n.timeline", entries: [{ id: "n.tl.1", when: "Today", who: "Ann", text: "Submitted" }] }],
  },
  { kind: "tabs", id: "n.tabs", tabs: [
    { id: "n.tab.one", label: "One", content: [{ kind: "text", id: "n.tab.one.text", text: "First tab" }] },
    { id: "n.tab.two", label: "Two", content: [{ kind: "text", id: "n.tab.two.text", text: "Second tab" }] },
  ] },
  { kind: "stepper", id: "n.stepper", steps: [
    { id: "n.step.one", label: "Start", content: [{ kind: "text", id: "n.step.one.text", text: "Step one body" }] },
    { id: "n.step.two", label: "Finish", content: [{ kind: "text", id: "n.step.two.text", text: "Step two body" }] },
  ] },
  { kind: "validation-summary", id: "n.summary", issues: ["Amount is required"] },
  { kind: "form", id: "n.form", title: "Claim form", fields: [
    { id: "n.f.amount", label: "Amount", type: "number", value: "0", error: "Too small" },
    { id: "n.f.cat", label: "Category", type: "select", value: "Travel", options: ["Travel", "Meals"] },
    { id: "n.f.on", label: "Urgent", type: "switch", value: "on" },
  ], actions: [{ id: "n.form.btn", label: "Submit", emphasis: "primary", action: { kind: "show-dialog", dialogId: "n.dialog" } }] },
  { kind: "filters", id: "n.filters", fields: [{ id: "n.filter.q", label: "Search", value: "taxi" }] },
  { kind: "table", id: "n.table", title: "Claims", columns: ["Who", "Status"], rows: [
    { id: "n.row.1", values: { Who: "Ann", Status: "Open" }, tone: "warning" },
  ] },
  { kind: "task-queue", id: "n.queue", title: "Queue", columns: ["Who"], rows: [{ id: "n.qrow.1", values: { Who: "Bo" } }], onRow: go },
  { kind: "approval-panel", id: "n.approval", title: "Decide", summary: "Approve it?", actions: [
    { id: "n.approve", label: "Approve", emphasis: "primary", action: { kind: "show-dialog", dialogId: "n.dialog" } },
  ] },
  { kind: "button", id: "n.button", label: "Go to B", action: go },
  { kind: "link", id: "n.link", label: "Highlight Ann", action: { kind: "select-row", tableId: "n.table", rowId: "n.row.1" } },
];

const kitchenSinkResult = parsePrototypeModel({
  schemaVersion: 1,
  component: "sink",
  name: "Sink",
  defaultScreenId: "screen.a",
  roles: [{ id: "user", name: "User" }],
  states: [{ id: "state.default", name: "Default" }],
  flows: [],
  navigation: [{ id: "n.nav", kind: "side-nav", items: [
    { id: "n.nav.a", label: "Screen A", action: { kind: "navigate", screenId: "screen.a" } },
    { id: "n.nav.b", label: "Screen B", action: go },
  ] }],
  screens: [
    { id: "screen.a", name: "A", roleIds: ["user"], navigationId: "n.nav", content: kitchenSinkNodes, overlays: [
      { kind: "dialog", id: "n.dialog", title: "Sure?", content: [{ kind: "text", id: "n.dialog.text", text: "Dialog body" }], actions: [
        { id: "n.dialog.close", label: "Close", action: { kind: "close-overlay" } },
      ] },
      { kind: "drawer", id: "n.drawer", title: "Side panel", content: [{ kind: "text", id: "n.drawer.text", text: "Drawer body" }] },
    ] },
    { id: "screen.b", name: "B", roleIds: ["user"], navigationId: "n.nav", content: [{ kind: "heading", id: "n.b.heading", text: "Screen B" }] },
  ],
});
if (!kitchenSinkResult.ok) throw new Error(JSON.stringify(kitchenSinkResult.issues));
const sink = kitchenSinkResult.model;

describe("PrototypeRenderer — the v1 registry", () => {
  it("renders every node kind, each carrying its stable model ID", () => {
    renderModel(sink);
    const kinds = new Set<PrototypeNodeKind>();
    const walk = (nodes: PrototypeNode[]) =>
      nodes.forEach((n) => {
        kinds.add(n.kind);
        if (n.kind === "stack" || n.kind === "grid") walk(n.content);
        if (n.kind === "split") walk([...n.left, ...n.right]);
      });
    walk(kitchenSinkNodes);
    expect(kinds.size).toBe(22);
    for (const n of kitchenSinkNodes) expect(byId(n.id), n.id).not.toBeNull();

    expect(screen.getByRole("heading", { name: "Everything" })).toBeInTheDocument();
    expect(screen.getByText("Pending")).toBeInTheDocument();
    expect(screen.getByText("It failed")).toBeInTheDocument();
    expect(screen.getByText("7")).toBeInTheDocument();
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
    expect(screen.getByText("$1")).toBeInTheDocument();
    expect(screen.getByText("Submitted")).toBeInTheDocument();
    expect(screen.getByText("Amount is required")).toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Claims" })).toBeInTheDocument();
    expect(screen.getByRole("table", { name: "Queue" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Highlight Ann" })).toBeInTheDocument();
  });

  it("gives every interactive entry the stable model ID", () => {
    renderModel(sink);
    for (const id of [
      "n.crumb.home", "n.heading.btn", "n.tab.one", "n.tab.two", "n.step.one", "n.step.two",
      "n.f.amount", "n.f.cat", "n.f.on", "n.form.btn", "n.filter.q", "n.row.1", "n.qrow.1",
      "n.approve", "n.button", "n.link", "n.nav.a", "n.nav.b",
    ]) {
      expect(byId(id), id).not.toBeNull();
    }
  });

  it("renders forms read-only", () => {
    renderModel(sink);
    const amount = within(byId("n.f.amount") as HTMLElement).getByRole("textbox");
    expect(amount).toHaveAttribute("readonly");
    expect(amount).toHaveValue("0");
    expect(within(byId("n.f.on") as HTMLElement).getByRole("checkbox")).toBeChecked();
    expect(within(byId("n.f.on") as HTMLElement).getByRole("checkbox")).toHaveAttribute("readonly");
    fireEvent.mouseDown(within(byId("n.f.cat") as HTMLElement).getByRole("combobox"));
    expect(screen.queryByRole("listbox")).toBeNull();
  });

  it("shows a field's error in the states that declare it", () => {
    renderModel(sink);
    expect(screen.getByText("Too small")).toBeInTheDocument();
  });
});

describe("PrototypeRenderer — Preview acts", () => {
  it("opens and closes a dialog", async () => {
    renderModel(sink);
    fireEvent.click(screen.getByRole("button", { name: "Submit" }));
    const dialog = screen.getByRole("dialog", { name: "Sure?" });
    expect(within(dialog).getByText("Dialog body")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitForElementToBeRemoved(() => screen.queryByText("Dialog body"));
  });

  it("opens a drawer", () => {
    renderModel(sink);
    fireEvent.click(screen.getByRole("button", { name: "Open drawer" }));
    expect(screen.getByText("Drawer body")).toBeInTheDocument();
  });

  it("switches tabs and steps", () => {
    renderModel(sink);
    expect(screen.getByText("First tab")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Two" }));
    expect(screen.getByText("Second tab")).toBeInTheDocument();
    expect(screen.queryByText("First tab")).toBeNull();

    expect(screen.getByText("Step one body")).toBeInTheDocument();
    fireEvent.click(byId("n.step.two") as HTMLElement);
    expect(screen.getByText("Step two body")).toBeInTheDocument();
  });

  it("navigates from a button, a row, a breadcrumb and the side navigation", () => {
    renderModel(sink);
    fireEvent.click(screen.getByRole("button", { name: "Go to B" }));
    expect(screen.getByRole("heading", { name: "Screen B" })).toBeInTheDocument();
    fireEvent.click(screen.getByText("Screen A"));
    expect(screen.getByRole("heading", { name: "Everything" })).toBeInTheDocument();
    fireEvent.click(byId("n.qrow.1") as HTMLElement);
    expect(screen.getByRole("heading", { name: "Screen B" })).toBeInTheDocument();
    act(() => dispatchRef({ type: "NAVIGATE", screenId: "screen.a" }));
    fireEvent.click(screen.getByRole("button", { name: "Home" }));
    expect(screen.getByRole("heading", { name: "Screen B" })).toBeInTheDocument();
  });

  it("highlights a row from a link or a click on a table without an onRow action", () => {
    renderModel(sink);
    const row = byId("n.row.1") as HTMLElement;
    expect(row).not.toHaveClass("Mui-selected");
    fireEvent.click(screen.getByRole("button", { name: "Highlight Ann" }));
    expect(row).toHaveClass("Mui-selected");
  });
});

describe("PrototypeRenderer — Annotate selects", () => {
  it("selects the button instead of opening its dialog", () => {
    renderModel(sink, { mode: "annotate" });
    const frame = byId("n.form.btn") as HTMLElement;
    expect(frame).toHaveAttribute("aria-pressed", "false");
    fireEvent.click(frame);
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(frame).toHaveAttribute("aria-pressed", "true");
    // The enclosing form is not selected with it.
    expect(byId("n.form")).toHaveAttribute("aria-pressed", "false");
  });

  it("selects a row without navigating or selecting its table", () => {
    renderModel(sink, { mode: "annotate" });
    fireEvent.click(byId("n.qrow.1") as HTMLElement);
    expect(screen.getByRole("heading", { name: "Everything" })).toBeInTheDocument();
    expect(byId("n.qrow.1")).toHaveAttribute("aria-pressed", "true");
    expect(byId("n.queue")).toHaveAttribute("aria-pressed", "false");
  });
});

describe("PrototypeRenderer — the fixtures", () => {
  it.each([
    ["expense approval", expenseApproval],
    ["integration monitor", integrationMonitor],
  ])("renders every screen of %s in every display state", (_, model) => {
    for (const s of model.screens) {
      for (const st of model.states) {
        const { unmount } = renderModel(model, { screen: s.id, state: st.id });
        for (const n of s.content) {
          const shown = !n.showIn || n.showIn.includes(st.id);
          expect(Boolean(byId(n.id)), `${s.id} ${st.id} ${n.id}`).toBe(shown);
        }
        unmount();
      }
    }
  });

  it("filters navigation items by role", () => {
    const navItems = () =>
      within(screen.getByRole("navigation", { name: "Expense approvals navigation" })).getAllByRole("button").map((b) => b.textContent);
    renderModel(expenseApproval, { screen: "screen.mine" });
    expect(navItems()).toEqual(["My expenses", "New expense"]);
    cleanup();
    renderModel(expenseApproval, { screen: "screen.reports" });
    expect(navItems()).toEqual(["Approval queue", "Reports"]);
  });

  it("renders a top navigation", () => {
    renderModel(integrationMonitor);
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");
  });
});

describe("PrototypeRenderer — no escape hatch", () => {
  // The model has no markup, script or style field; this keeps the renderer
  // from growing a path that would turn a string into any of them.
  const sources = import.meta.glob<string>(["./**/*.tsx", "!./**/*.test.tsx"], {
    query: "?raw",
    import: "default",
    eager: true,
  });

  it.each(Object.entries(sources))("%s has no raw HTML, script or style-string path", (_, source) => {
    expect(source).not.toMatch(/dangerouslySetInnerHTML|innerHTML|outerHTML|insertAdjacentHTML|new Function|eval\(|<script|style=\{|style="/);
  });
});
