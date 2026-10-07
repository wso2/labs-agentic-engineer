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

import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { ThreadsMenu } from "./ThreadsMenu";

// The chat header's list of the project's threads: the main chat, and the
// Issues chat once it has something in it, with how many messages each has
// and whether the branch is open or was summed up in the main chat.

function renderMenu(props: Partial<Parameters<typeof ThreadsMenu>[0]> = {}) {
  const onMain = vi.fn();
  const onIssues = vi.fn();
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <ThreadsMenu
        projectLabel="Acme Expenses"
        mainCount={4}
        issues={{ count: 2, state: "open" }}
        current="main"
        onMain={onMain}
        onIssues={onIssues}
        {...props}
      />
    </OxygenUIThemeProvider>,
  );
  fireEvent.click(screen.getByRole("button", { name: "Threads" }));
  return { onMain, onIssues };
}

afterEach(cleanup);

describe("ThreadsMenu", () => {
  it("lists the main chat and the Issues branch, with their message counts and the branch's state", () => {
    renderMenu();
    const main = screen.getByRole("menuitem", { name: /main chat/ });
    expect(within(main).getByText("Acme Expenses · main chat")).toBeTruthy();
    expect(within(main).getByText("4")).toBeTruthy();
    const issues = screen.getByRole("menuitem", { name: /^Issues/ });
    expect(within(issues).getByText("Issues")).toBeTruthy();
    expect(within(issues).getByText("open")).toBeTruthy();
    expect(within(issues).getByText("2")).toBeTruthy();
  });

  it("says a branch that was summed up in the main chat is summarised", () => {
    renderMenu({ issues: { count: 6, state: "summarised" } });
    expect(within(screen.getByRole("menuitem", { name: /^Issues/ })).getByText("summarised")).toBeTruthy();
  });

  it("lists only the main chat while the Issues chat is empty", () => {
    renderMenu({ issues: null });
    expect(screen.getAllByRole("menuitem")).toHaveLength(1);
  });

  it("marks the thread in front", () => {
    renderMenu({ current: "issues" });
    expect(screen.getByRole("menuitem", { name: /^Issues/ }).classList).toContain("Mui-selected");
    expect(screen.getByRole("menuitem", { name: /main chat/ }).classList).not.toContain("Mui-selected");
  });

  it("Issues goes to the Issues chat, the main chat back to it, and either closes the menu", () => {
    const { onIssues } = renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: /^Issues/ }));
    expect(onIssues).toHaveBeenCalledTimes(1);
    cleanup();
    const { onMain } = renderMenu();
    fireEvent.click(screen.getByRole("menuitem", { name: /main chat/ }));
    expect(onMain).toHaveBeenCalledTimes(1);
  });
});
