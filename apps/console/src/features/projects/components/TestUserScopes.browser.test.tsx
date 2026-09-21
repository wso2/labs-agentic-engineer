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

// The scopes cell's one PROMISE is a measurement: a row stays a row however
// many scopes the account carries. Listed inline, a six-scope row ran roughly
// 350px; the cell now holds a button and the list moved into a dialog.
//
// jsdom cannot tell the difference -- every box is 0px tall there -- so the
// claim that made the change worth doing has never actually been checked.
// This lane has a real layout engine, so it can be. It also drives the dialog
// with real keyboard input (Escape, focus restoration), which MUI implements
// against the real focus model rather than the one jsdom approximates.
//
// The local cluster's only project has test users with NO scopes, so the
// populated case had never been on a screen at all. The screenshots this file
// writes to /tmp/scopes-shots are the first look at it.

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { page, userEvent } from "vitest/browser";
import { OxygenTheme, OxygenUIThemeProvider } from "@wso2/oxygen-ui";
import {
  projectRolesView,
  projectRolesViewOffline,
} from "../../../mocks/fixtures/roles";
import type { PublishedTestUser } from "../lib/publishedTestUsers";
import { TestUsersInline } from "./TryItOut";

const SHOTS = "/tmp/scopes-shots";

/** The column width the environment page gives this panel on a laptop, with
 *  the agent chat rail closed. Wide enough that the Scopes column is the
 *  narrow one on the right, which is the shape the cell was designed for. */
const PANEL_WIDTH = 1100;

/** The lane's default iframe is far narrower than the panel, so a shot taken
 *  in it would crop the Scopes column straight off the right edge -- which is
 *  the one thing these pictures exist to show. */
async function widenViewport() {
  await page.viewport(PANEL_WIDTH + 80, 640);
}

/**
 * The accounts the mock directory serves, in the panel's own shape.
 *
 * Taken from the roles fixture rather than invented, so the handles are the
 * `resource:verb` shape the platform actually returns and the counts are the
 * ones the product produces: 4 for the compliance admin, 3 for the viewer,
 * and 6 for the account holding two roles. `publishedTestUsers` is not used
 * to map them -- it filters on `owned`, which would drop two of the three and
 * leave one row to measure instead of three.
 */
const WITH_SCOPES: PublishedTestUser[] = (projectRolesView.testUsers ?? []).map(
  (u) => ({
    username: u.username,
    roles: [...(u.roles ?? [])],
    scopes: [...(u.scopes ?? [])],
  }),
);

/** The degraded read, from the fixture that models it: the identity provider
 *  could not be asked, so every scope list comes back empty. */
const WITHOUT_SCOPES: PublishedTestUser = (() => {
  const u = (projectRolesViewOffline.testUsers ?? [])[0]!;
  return {
    username: `${u.username}-offline`,
    roles: [...(u.roles ?? [])],
    scopes: [],
  };
})();

const LOGINS: PublishedTestUser[] = [...WITH_SCOPES, WITHOUT_SCOPES];

/** The account with the most scopes -- the row the height claim is about. */
const CROWDED = WITH_SCOPES.find((u) => u.username === "jsmith")!;

/**
 * The panel as the environment page mounts it, at a realistic column width.
 * `TestUsersInline` needs no router and no query client, so this is the whole
 * product surface: the head row with the Scopes column, and one `TestUserRow`
 * per account.
 */
function renderPanel(logins: readonly PublishedTestUser[] = LOGINS) {
  render(
    <OxygenUIThemeProvider theme={OxygenTheme}>
      <div
        data-testid="panel"
        style={{ width: PANEL_WIDTH, padding: 16, background: "#fff" }}
      >
        <TestUsersInline
          logins={logins}
          loadState="ready"
          thunderUrl="https://thunder.openchoreo.localhost:8090"
          revealPassword={vi.fn(async () => "hunter2")}
        />
      </div>
    </OxygenUIThemeProvider>,
  );
}

/** MUI grows the dialog in over ~225ms. A shot taken the moment it mounts
 *  catches it half-faded and half-scaled, which is a picture of the
 *  transition rather than of the thing -- so wait for it to settle. */
async function settled(dialog: HTMLElement) {
  await vi.waitFor(() => {
    const style = getComputedStyle(dialog);
    expect(style.opacity).toBe("1");
    expect(["none", "matrix(1, 0, 0, 1, 0, 0)"]).toContain(style.transform);
  });
}

/** The `<tr>` an account's cells sit in. */
function rowFor(username: string): HTMLElement {
  return screen.getByText(username).closest("tr")!;
}

function scopesButton(user: PublishedTestUser): HTMLElement {
  return screen.getByRole("button", {
    name: `Show ${user.scopes.length} scopes for ${user.username}`,
  });
}

/** Every row's height, keyed by account. */
function rowHeights(users: readonly PublishedTestUser[]): Map<string, number> {
  return new Map(
    users.map((u) => [
      u.username,
      rowFor(u.username).getBoundingClientRect().height,
    ]),
  );
}

/**
 * The same accounts with their scopes taken away -- the CONTROL.
 *
 * The other three columns are not uniform across these accounts: `jsmith`
 * holds two roles, and two role names wrap to two lines in a 160px Role
 * column, which makes its row taller than the rest for a reason that has
 * nothing to do with scopes. Comparing each account against ITSELF without
 * scopes is what isolates the cell under test from that.
 */
const STRIPPED: PublishedTestUser[] = WITH_SCOPES.map((u) => ({
  ...u,
  scopes: [],
}));

/** Measured in this lane: a one-role row is 43.25-43.75px with scopes and
 *  36.5-37px without; `jsmith`, whose two role names wrap, is 54.16px. The
 *  bound sits above all of them and is still a fifth of the ~350px a
 *  six-scope stacked list cost -- the number the cell was changed to fix.
 *  One extra LINE of handles would blow straight through it. */
const ONE_LINE_MAX = 80;

/** What the text button costs over the em dash it replaces, on the same
 *  account: 6.75px measured. Bounded rather than demanded to be zero -- a
 *  control has a control's height. What matters is that it is a constant and
 *  not something the scope COUNT feeds. */
const BUTTON_COST_MAX = 12;

afterEach(cleanup);

describe("the test-users table, in a real browser", () => {
  it("heads the column Scopes", () => {
    renderPanel();
    const head = screen.getAllByRole("row")[0]!;
    expect(within(head).getByText("Scopes")).toBeInTheDocument();
    // The v1 leftover the header used to read.
    expect(within(head).queryByText("Cold start")).not.toBeInTheDocument();
  });

  it("keeps a row one line tall however many scopes the account carries", async () => {
    renderPanel();
    const withScopes = rowHeights(WITH_SCOPES);

    for (const user of WITH_SCOPES) {
      const height = withScopes.get(user.username)!;
      expect(
        height,
        `${user.username} (${user.scopes.length} scopes) measured ${height}px`,
      ).toBeLessThan(ONE_LINE_MAX);
      // The button says how many, so the count reads without opening it.
      expect(scopesButton(user)).toHaveTextContent(
        `Scopes · ${user.scopes.length}`,
      );
    }

    await widenViewport();
    await page.screenshot({
      path: `${SHOTS}/01-table.png`,
      element: screen.getByTestId("panel"),
    });

    // The same accounts, scopes removed: what each row costs BECAUSE of its
    // scopes is the difference, and it is the button's height rather than
    // anything that tracks 3, 4 or 6 handles.
    cleanup();
    renderPanel(STRIPPED);
    const stripped = rowHeights(STRIPPED);

    const costs = WITH_SCOPES.map(
      (u) => withScopes.get(u.username)! - stripped.get(u.username)!,
    );
    for (const [i, user] of WITH_SCOPES.entries()) {
      expect(
        costs[i]!,
        `${user.username}'s ${user.scopes.length} scopes added ${costs[i]!.toFixed(2)}px ` +
          `to a row that is ${stripped.get(user.username)!}px without them`,
      ).toBeLessThan(BUTTON_COST_MAX);
    }
    // And the cost does not RISE with the count, which is the whole claim.
    // Measured: 6.75, 6.75, 0.50 -- the six-scope account pays LEAST, because
    // its two wrapped role names already make its row tall enough to hold the
    // button for free. Asserted as "the fullest account costs no more than
    // the emptiest" rather than as three equal numbers, which they are not.
    //
    // The bound is the MINIMUM, deliberately. `most` is itself a member of
    // `costs`, so bounding it by the maximum asserts nothing -- it holds for
    // every possible input, including the scope-count-dependent growth this
    // line exists to catch. Bounded by the minimum it fails the moment the
    // crowded account pays more than the emptiest one does.
    const most = costs[WITH_SCOPES.indexOf(CROWDED)]!;
    expect(
      most,
      `per-account cost: ${WITH_SCOPES.map(
        (u, i) => `${u.username} (${u.scopes.length}) ${costs[i]!.toFixed(2)}px`,
      ).join(", ")}`,
    ).toBeLessThanOrEqual(Math.min(...costs) + 0.01);
  });

  it("opens every one of the account's scopes in the dialog", async () => {
    await widenViewport();
    renderPanel();

    await userEvent.click(scopesButton(CROWDED));
    const dialog = await screen.findByRole("dialog", {
      name: new RegExp(CROWDED.username),
    });
    for (const scope of CROWDED.scopes) {
      expect(
        within(dialog).getByText(scope),
        `${scope} is missing from the dialog`,
      ).toBeInTheDocument();
    }
    // Every handle-shaped label in the dialog is one of this account's
    // scopes -- nothing extra crept in, and nothing was folded away.
    const chips = within(dialog).getAllByText(/^[a-z][a-z-]*:[a-z][a-z-]*$/);
    expect(chips.length).toBe(CROWDED.scopes.length);

    await settled(dialog);
    await page.screenshot({ path: `${SHOTS}/02-dialog.png` });
  });

  it("closes on Escape and hands focus back to the button that opened it", async () => {
    renderPanel();

    const button = scopesButton(CROWDED);
    await userEvent.click(button);
    await screen.findByRole("dialog");

    await userEvent.keyboard("{Escape}");
    await vi.waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
    // MUI restores focus to the trigger, so the reader lands back where they
    // were rather than at the top of the document.
    await vi.waitFor(() => {
      expect(document.activeElement).toBe(button);
    });
  });

  it("offers no button when the directory could not be asked", () => {
    renderPanel();

    const row = rowFor(WITHOUT_SCOPES.username);
    const cell = row.querySelectorAll("td")[3]!;
    expect(within(cell).queryByRole("button")).toBeNull();
    expect(cell.textContent).toContain("Scopes unknown");
  });
});
