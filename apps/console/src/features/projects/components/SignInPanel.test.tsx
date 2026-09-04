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
import { fireEvent, render, screen, within } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";
import type { PublishedTestUser } from "../lib/publishedTestUsers";
import { SignInPanel } from "./SignInPanel";
import { MASK } from "./TestUsersDialog";

const THUNDER_URL = "http://localhost:8097";
const THUNDER_CONSOLE_USERS = "http://localhost:8097/console/users";
const MOCK_PASSWORD = "mocknotreal";

function renderPanel(
  over: {
    logins?: readonly PublishedTestUser[];
    revealPassword?: (username: string) => Promise<string>;
    loadState?: "ready" | "pending" | "error";
  } = {},
): {
  revealPassword: ReturnType<typeof vi.fn<(username: string) => Promise<string>>>;
} {
  const revealPassword =
    over.revealPassword ??
    vi.fn(async () => MOCK_PASSWORD);
  const ui: ReactElement = (
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <SignInPanel
        logins={over.logins ?? []}
        thunderUrl={THUNDER_URL}
        revealPassword={revealPassword}
        {...(over.loadState !== undefined ? { loadState: over.loadState } : {})}
      />
    </OxygenUIThemeProvider>
  );
  render(ui);
  return { revealPassword: revealPassword as ReturnType<typeof vi.fn<(username: string) => Promise<string>>> };
}

describe("SignInPanel", () => {
  it("empty logins shows only the Thunder sentence", () => {
    renderPanel({ logins: [] });

    expect(screen.getByText(/Manage user accounts in/)).toBeInTheDocument();
    expect(
      screen.queryByText("Test users for agents on this environment"),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/test-/i)).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "View test users" }),
    ).not.toBeInTheDocument();

    const link = screen.getByRole("link", {
      name: "Open Thunder Console to add or remove real accounts",
    });
    expect(link).toHaveAttribute("href", THUNDER_CONSOLE_USERS);
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("says how many accounts there are and offers the dialog, listing none itself", () => {
    renderPanel({
      logins: [
        { username: "test-viewer", role: "Viewer", coldStart: true },
        { username: "test-admin", role: "Admin", coldStart: false },
      ],
    });

    expect(
      screen.getByText("Test users for agents on this environment"),
    ).toBeInTheDocument();
    expect(screen.getByText("2 accounts, one per role")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "View test users" }),
    ).toBeInTheDocument();
    // The card carries the COUNT and nothing else: the accounts themselves
    // are what made it grow with the design.
    expect(screen.queryByText("test-viewer")).not.toBeInTheDocument();
    expect(screen.queryByText(MASK)).not.toBeInTheDocument();
    expect(screen.getByText(/Manage user accounts in/)).toBeInTheDocument();
  });

  it("counts one account without pluralising it", () => {
    renderPanel({
      logins: [{ username: "test-viewer", role: "Viewer", coldStart: true }],
    });

    expect(screen.getByText("1 account, one per role")).toBeInTheDocument();
  });

  it("opens the accounts in a dialog", () => {
    renderPanel({
      logins: [
        { username: "test-viewer", role: "Viewer", coldStart: true },
        { username: "test-admin", role: "Admin", coldStart: false },
      ],
    });

    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "View test users" }));

    const dialog = screen.getByRole("dialog");
    expect(within(dialog).getByText("test-viewer")).toBeInTheDocument();
    expect(within(dialog).getByText("test-admin")).toBeInTheDocument();
  });

  it("does not render Add, Rotate, Delete, or Roles-gate copy", () => {
    renderPanel({
      logins: [{ username: "test-viewer", role: "Viewer", coldStart: true }],
    });

    expect(screen.queryByText(/^Add$/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Rotate/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Delete/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/Roles gate/i)).not.toBeInTheDocument();
  });

  it("pending load shows a caption and still links Thunder Console", () => {
    renderPanel({ loadState: "pending" });
    expect(screen.getByText("Loading test users\u2026")).toBeInTheDocument();
    expect(
      screen.getByRole("link", {
        name: "Open Thunder Console to add or remove real accounts",
      }),
    ).toBeInTheDocument();
  });

  it("error load shows a caption and still links Thunder Console", () => {
    renderPanel({ loadState: "error" });
    expect(screen.getByText("Couldn't load test users.")).toBeInTheDocument();
    expect(
      screen.getByRole("link", {
        name: "Open Thunder Console to add or remove real accounts",
      }),
    ).toBeInTheDocument();
  });
});
