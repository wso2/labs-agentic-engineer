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

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { BranchSheet } from "./BranchSheet";

// The Issues chat as a sheet over the main chat: a strip back to the main
// chat on top, the path from the project to Issues, then the branch's own
// thread and composer.

function renderSheet(props: Partial<Parameters<typeof BranchSheet>[0]> = {}) {
  const onMinimise = vi.fn();
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <BranchSheet projectLabel="Acme Expenses" peek="Sure, I'll look." hidden={false} onMinimise={onMinimise} {...props}>
        <p>the branch's thread</p>
      </BranchSheet>
    </OxygenUIThemeProvider>,
  );
  return { onMinimise };
}

afterEach(cleanup);

describe("BranchSheet", () => {
  it("is the Issues chat: a strip back to the main chat, the path, and its contents", () => {
    renderSheet();
    const sheet = screen.getByRole("region", { name: "Issues chat" });
    expect(sheet.textContent).toContain("↑ Main chat");
    expect(screen.getByTestId("branch-path").textContent).toBe("Acme Expenses └ Issues");
    expect(screen.getByText("the branch's thread")).toBeTruthy();
  });

  it("shows the main chat's last line on the strip", () => {
    renderSheet();
    expect(screen.getByRole("button", { name: /Main chat/ }).textContent).toContain("Sure, I'll look.");
  });

  it("the strip minimises the sheet", () => {
    const { onMinimise } = renderSheet();
    fireEvent.click(screen.getByRole("button", { name: /Main chat/ }));
    expect(onMinimise).toHaveBeenCalledTimes(1);
  });

  it("hidden while minimised, keeping what it holds", () => {
    renderSheet({ hidden: true });
    expect(screen.queryByRole("region", { name: "Issues chat" })).toBeNull();
    expect(screen.getByText("the branch's thread")).toBeTruthy();
  });
});
