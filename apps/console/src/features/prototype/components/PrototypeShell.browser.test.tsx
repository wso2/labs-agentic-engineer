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

// Annotate against a real layout engine and real input (#817): the selection
// rules the reducer promises, exercised with real pointer and keyboard events —
// the wrapper rule (content takes no pointer events, the wrapper does) is CSS,
// and only a real browser resolves which element a click lands on.
//
// Also writes the Annotate screenshots to /tmp/prototype-shots.

import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { page, userEvent } from "vitest/browser";
import { expenseApproval as model } from "../testing/fixtures";
import { ShellHarness } from "../testing/ShellHarness";

const SHOTS = "/tmp/prototype-shots";

const byId = (id: string) => document.querySelector<HTMLElement>(`[data-prototype-component-id="${id}"]`)!;
const selected = () =>
  [...document.querySelectorAll<HTMLElement>('[data-prototype-component-id][aria-pressed="true"]')].map(
    (el) => el.dataset.prototypeComponentId,
  );
const inspector = () => screen.queryByRole("complementary", { name: "Feedback" });

beforeEach(async () => {
  await page.viewport(1440, 900);
  render(<ShellHarness model={model} />);
  await userEvent.click(screen.getByRole("button", { name: "Annotate" }));
});
afterEach(cleanup);

describe("PrototypeShell in a browser", () => {
  it("toggles a component off when it is clicked a second time", async () => {
    await userEvent.click(byId("btn.export"));
    await userEvent.click(byId("stat.overdue"));
    expect(selected()).toEqual(["btn.export", "stat.overdue"]);
    await userEvent.click(byId("btn.export"));
    expect(selected()).toEqual(["stat.overdue"]);
    // Annotate never acts: the Export drawer did not open.
    expect(screen.queryByRole("heading", { name: "Export queue" })).toBeNull();
  });

  it("selects the innermost component a click lands on", async () => {
    await userEvent.click(byId("stat.pending"));
    expect(selected()).toEqual(["stat.pending"]);
  });

  it("clears the selection on Escape", async () => {
    await userEvent.click(byId("btn.export"));
    await userEvent.click(byId("stat.overdue"));
    await userEvent.keyboard("{Escape}");
    expect(selected()).toEqual([]);
    expect(inspector()).not.toBeNull();
  });

  it("clears the selection on leaving Annotate, where clicks act again", async () => {
    await userEvent.click(byId("btn.export"));
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));
    expect(inspector()).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Annotate" }));
    expect(selected()).toEqual([]);
    await userEvent.click(screen.getByRole("button", { name: "Preview" }));
    await userEvent.click(within(byId("btn.export")).getByRole("button", { name: "Export" }));
    expect(await screen.findByText("Export queue")).toBeInTheDocument();
  });

  it("clears the selection on a screen change", async () => {
    await userEvent.click(byId("btn.export"));
    await userEvent.click(screen.getByRole("combobox", { name: "Screen" }));
    await userEvent.click(await screen.findByRole("option", { name: "Expense detail" }));
    expect(selected()).toEqual([]);
    expect(within(inspector()!).getByText("On Expense detail")).toBeInTheDocument();
  });

  it("shows selections, pins and a queue (screenshot)", async () => {
    const request = () => within(inspector()!).getByRole("textbox", { name: "Request" });
    await userEvent.click(byId("btn.export"));
    await userEvent.fill(request(), "Call this Download CSV — Export reads as a data dump.");
    await userEvent.click(within(inspector()!).getByRole("button", { name: "Add request" }));
    await userEvent.click(byId("stat.overdue"));
    await userEvent.click(byId("stat.total"));
    await userEvent.fill(request(), "Overdue should link to the filtered queue, and the month total needs a currency.");
    await userEvent.click(within(inspector()!).getByRole("button", { name: "Add request" }));
    await userEvent.fill(request(), "The screen needs a filter by department.");
    await userEvent.click(within(inspector()!).getByRole("button", { name: "Add request" }));
    await userEvent.click(byId("queue.expenses"));
    await userEvent.click(byId("stat.pending"));
    expect(within(inspector()!).getAllByRole("listitem")).toHaveLength(3);
    await page.screenshot({ path: `${SHOTS}/annotate-selection-pins-queue.png` });
  });
});
