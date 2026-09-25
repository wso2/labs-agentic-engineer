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
import {
  fireEvent,
  render,
  screen,
  waitForElementToBeRemoved,
  within,
} from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { describe, expect, it } from "vitest";
import {
  displayScopes,
  scopesButtonLabel,
  scopesButtonText,
} from "../lib/testUserScopes";
import { TestUserScopesCell, UNKNOWN_SCOPES } from "./TestUserScopesDialog";

const SIX = [
  "progress:read",
  "food-entries:write",
  "coach-access:manage",
  "daily-goal:read",
  "food-entries:read",
  "daily-goal:write",
];

function renderCell(scopes: readonly string[], username = "test-client") {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <TestUserScopesCell username={username} scopes={scopes} />
    </OxygenUIThemeProvider>,
  );
}

function openScopes(name: string) {
  fireEvent.click(screen.getByRole("button", { name }));
  return within(screen.getByRole("dialog"));
}

describe("the scopes helpers", () => {
  it("orders the handles for reading, not in the wire's order", () => {
    expect(displayScopes(SIX)).toEqual([
      "coach-access:manage",
      "daily-goal:read",
      "daily-goal:write",
      "food-entries:read",
      "food-entries:write",
      "progress:read",
    ]);
    // The caller's array is not reordered under it.
    expect(SIX[0]).toBe("progress:read");
  });

  it("names the control by what it does, how many, and whose", () => {
    expect(scopesButtonText(6)).toBe("Scopes · 6");
    expect(scopesButtonLabel(6, "test-client")).toBe(
      "Show 6 scopes for test-client",
    );
    // One is not "1 scopes".
    expect(scopesButtonLabel(1, "test-coach")).toBe(
      "Show 1 scope for test-coach",
    );
  });
});

describe("the scopes cell", () => {
  it("holds a count, not the list — the row stays a row", () => {
    renderCell(SIX);

    const button = screen.getByRole("button", {
      name: "Show 6 scopes for test-client",
    });
    expect(button).toHaveTextContent("Scopes · 6");
    // Not a single handle is in the row until it is asked for.
    expect(screen.queryByText("coach-access:manage")).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("opens every scope in a dialog titled for the account", () => {
    renderCell(SIX);

    const dialog = openScopes("Show 6 scopes for test-client");
    for (const scope of SIX) {
      expect(dialog.getByText(scope)).toBeInTheDocument();
    }
    expect(
      screen.getByRole("dialog", { name: /test-client/ }),
    ).toBeInTheDocument();
  });

  it("closes again, and the row is unchanged", async () => {
    renderCell(SIX);

    const dialog = openScopes("Show 6 scopes for test-client");
    fireEvent.click(dialog.getByRole("button", { name: "Close" }));
    // The dialog fades out, so it leaves the tree a beat after the click.
    await waitForElementToBeRemoved(() => screen.queryByRole("dialog"));
    expect(
      screen.getByRole("button", { name: "Show 6 scopes for test-client" }),
    ).toBeInTheDocument();
  });

  // The wire's empty array means the identity provider could not be asked.
  // A button opening an empty dialog would claim the account grants nothing.
  it("offers no control when there is nothing to list", () => {
    renderCell([], "test-ghost");

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.getByText(UNKNOWN_SCOPES)).toBeInTheDocument();
    // The em dash is decoration; the word is what is announced.
    expect(screen.getByText("Scopes unknown")).toBeInTheDocument();
  });
});
