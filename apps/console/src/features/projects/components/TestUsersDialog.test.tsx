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
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import { describe, expect, it, vi } from "vitest";
import type { PublishedTestUser } from "../lib/publishedTestUsers";
import { MASK, TestUsersDialog } from "./TestUsersDialog";

const MOCK_PASSWORD = "mocknotreal";

const TWO: PublishedTestUser[] = [
  { username: "test-viewer", role: "Viewer", coldStart: true },
  { username: "test-compliance-admin", role: "Compliance Admin", coldStart: false },
];

function renderDialog(
  over: {
    logins?: readonly PublishedTestUser[];
    revealPassword?: (username: string) => Promise<string>;
    onClose?: () => void;
  } = {},
) {
  const revealPassword = over.revealPassword ?? vi.fn(async () => MOCK_PASSWORD);
  const onClose = over.onClose ?? vi.fn();
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <TestUsersDialog
        open
        onClose={onClose}
        logins={over.logins ?? TWO}
        revealPassword={revealPassword}
      />
    </OxygenUIThemeProvider>,
  );
  return { revealPassword, onClose };
}

/** The row a username sits in — every assertion about one account is scoped
 *  to its row, so a two-account table cannot pass on the other one's cells. */
function rowOf(username: string): HTMLElement {
  const cell = screen.getByText(username);
  const row = cell.closest("tr");
  if (row === null) throw new Error(`no row for ${username}`);
  return row;
}

describe("TestUsersDialog", () => {
  it("gives every account its role, and marks the cold-start one", () => {
    renderDialog();

    // The role used to live in a tooltip on the username. It is a column now,
    // which is the whole reason the table earns a dialog.
    expect(within(rowOf("test-viewer")).getByText("Viewer")).toBeInTheDocument();
    expect(within(rowOf("test-viewer")).getByText("Cold start")).toBeInTheDocument();
    expect(
      within(rowOf("test-compliance-admin")).getByText("Compliance Admin"),
    ).toBeInTheDocument();
    expect(
      within(rowOf("test-compliance-admin")).queryByText("Cold start"),
    ).not.toBeInTheDocument();
  });

  it("masks every password, with both controls, before any reveal", () => {
    renderDialog();

    expect(screen.getAllByText(MASK)).toHaveLength(2);
    expect(
      screen.getByRole("button", { name: "Reveal the password for test-viewer" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Copy the password for test-viewer" }),
    ).toBeInTheDocument();
  });

  it("the eye toggles visibility, and reads the secret only once", async () => {
    const { revealPassword } = renderDialog();

    fireEvent.click(
      screen.getByRole("button", { name: "Reveal the password for test-viewer" }),
    );
    expect(revealPassword).toHaveBeenCalledWith("test-viewer");
    await waitFor(() => {
      expect(screen.getByText(MOCK_PASSWORD)).toBeInTheDocument();
    });
    expect(screen.getByText(MOCK_PASSWORD)).toHaveAttribute("aria-live", "polite");

    fireEvent.click(
      screen.getByRole("button", { name: "Hide the password for test-viewer" }),
    );
    expect(screen.queryByText(MOCK_PASSWORD)).not.toBeInTheDocument();

    // Showing it again is a decision about this screen, not a reason to ask
    // the sealed store for the secret a second time.
    fireEvent.click(
      screen.getByRole("button", { name: "Reveal the password for test-viewer" }),
    );
    await waitFor(() => {
      expect(screen.getByText(MOCK_PASSWORD)).toBeInTheDocument();
    });
    expect(revealPassword).toHaveBeenCalledTimes(1);
  });

  it("revealing one account does not reveal another", async () => {
    renderDialog();

    fireEvent.click(
      screen.getByRole("button", { name: "Reveal the password for test-viewer" }),
    );
    await waitFor(() => {
      expect(screen.getByText(MOCK_PASSWORD)).toBeInTheDocument();
    });

    expect(screen.getAllByText(MOCK_PASSWORD)).toHaveLength(1);
    expect(screen.getAllByText(MASK)).toHaveLength(1);
    expect(
      screen.getByRole("button", {
        name: "Reveal the password for test-compliance-admin",
      }),
    ).toBeInTheDocument();
  });

  it("copies a password that was never put on screen", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    renderDialog();

    fireEvent.click(
      screen.getByRole("button", { name: "Copy the password for test-viewer" }),
    );

    await waitFor(() => {
      expect(writeText).toHaveBeenCalledWith(MOCK_PASSWORD);
    });
    expect(screen.queryByText(MOCK_PASSWORD)).not.toBeInTheDocument();
  });

  it("shows an error in the row when the reveal fails", async () => {
    renderDialog({
      revealPassword: vi.fn(async () => {
        throw new Error("sealed store unreachable");
      }),
    });

    fireEvent.click(
      screen.getByRole("button", { name: "Reveal the password for test-viewer" }),
    );

    await waitFor(() => {
      expect(screen.getByText("sealed store unreachable")).toBeInTheDocument();
    });
    expect(screen.queryByText(MOCK_PASSWORD)).not.toBeInTheDocument();
    // The password stays masked; a failed read reveals nothing.
    expect(screen.getAllByText(MASK)).toHaveLength(2);
  });

  it("shows an error in the row when the clipboard write fails", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("clipboard denied"));
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    renderDialog();

    fireEvent.click(
      screen.getByRole("button", { name: "Copy the password for test-viewer" }),
    );

    await waitFor(() => {
      expect(screen.getByText("clipboard denied")).toBeInTheDocument();
    });
  });

  it("closes on the close button", () => {
    const { onClose } = renderDialog();

    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalled();
  });
});
