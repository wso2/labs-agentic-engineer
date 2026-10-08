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

/**
 * A preview smoke run under Oxygen: `prototype preview --theme` played in
 * Chromium, through the sandboxed frame, as a reviewer clicks it. It covers
 * what a theme draws and wires — navigation, rows, forms and validation, tabs,
 * dialogs, drawers, the stepper, Annotate selecting instead of acting — and
 * that the frame fetches nothing.
 */

import { chromium, type Browser, type FrameLocator, type Page } from "playwright";
import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { startPreview, type PreviewProcess } from "../harness.js";

let browser: Browser;

beforeAll(async () => {
  browser = await chromium.launch();
});

afterAll(async () => {
  await browser.close();
});

interface Session {
  preview: PreviewProcess;
  page: Page;
  app: FrameLocator;
  requests: string[];
  /** Uncaught errors in the host page or its sandboxed frame. */
  errors: string[];
}

async function open(fixture: string): Promise<Session> {
  const preview = await startPreview(`valid/${fixture}`);
  const page = await browser.newPage();
  const requests: string[] = [];
  const errors: string[] = [];
  page.on("request", (r) => requests.push(r.url()));
  // Playwright reports a frame's uncaught errors here too, the sandboxed one included.
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto(preview.url);
  return { preview, page, app: page.frameLocator('iframe[title$="prototype app"]'), requests, errors };
}

/**
 * The frame is sandboxed without `allow-same-origin`, so it has no storage: any
 * code of the theme or of Oxygen that reads `localStorage` unguarded throws
 * there. None may, whatever the run drew.
 */
async function close(s: Session): Promise<void> {
  const errors = s.errors;
  await s.page.close();
  await s.preview.stop();
  expect(errors).toEqual([]);
}

describe("contacts under Oxygen", () => {
  let s: Session;

  beforeAll(async () => {
    s = await open("contacts");
  });

  afterAll(async () => {
    await close(s);
  });

  beforeEach(async () => {
    // A reload starts from the seed and the entry screen (no --persist).
    await s.page.reload();
    await s.app.getByRole("heading", { name: "Acme contacts" }).waitFor();
  });

  it("navigates, validates a form and creates a record that shows on another screen, fetching nothing", async () => {
    await s.app.getByRole("row", { name: "Alan Turing" }).click();
    await s.app.getByRole("heading", { name: "Alan Turing" }).waitFor();
    await s.app.locator('[data-proto-key="nav.contacts"]').click();

    await s.app.getByRole("button", { name: "New contact" }).click();
    await s.app.getByRole("button", { name: "Save" }).click();
    await s.app.getByText("Fix 2 problems").waitFor();
    await s.app.getByRole("textbox", { name: "Name", exact: true }).fill("Grace Hopper");
    await s.app.getByRole("textbox", { name: "Email", exact: true }).fill("grace@example.com");
    await s.app.getByRole("button", { name: "Save" }).click();
    await s.app.getByRole("heading", { name: "Grace Hopper" }).waitFor();
    await s.app.locator('[data-proto-key="nav.contacts"]').click();
    await s.app.getByRole("row", { name: "Grace Hopper" }).waitFor();

    // Fonts and styles are inline: everything came from the preview server, which serves the host page and the runtime.
    expect(s.requests.filter((url) => !url.startsWith(s.preview.url))).toEqual([]);
  });

  it("runs in a frame that has no storage, and raises no uncaught error there", async () => {
    const noStorage = await s.page
      .frames()
      .find((f) => f !== s.page.mainFrame())!
      .evaluate(() => {
        try {
          void window.localStorage;
          return false;
        } catch {
          return true;
        }
      });
    expect(noStorage).toBe(true);
    expect(s.errors).toEqual([]);
  });

  it("fills fields and turns a switch on by their accessible names", async () => {
    await s.app.locator('[data-proto-key="nav.settings"]').click();
    await s.app.getByRole("textbox", { name: "Company", exact: true }).fill("Globex");
    await s.app.getByRole("button", { name: "Save settings" }).click();
    await s.app.getByText("Confirm company change is required").first().waitFor();
    await s.app.getByRole("switch", { name: "Confirm company change", exact: true }).click();
    await s.app.getByRole("button", { name: "Save settings" }).click();
    await s.app.getByRole("heading", { name: "Globex contacts" }).waitFor();
  });

  it("selects instead of acting in Annotate", async () => {
    await s.page.getByRole("button", { name: "Annotate" }).click();
    const button = s.app.locator('[data-proto-key="btn.new"]');
    await button.click();
    expect(await button.getAttribute("aria-pressed")).toBe("true");
    expect(await s.app.getByRole("heading", { name: "New contact" }).count()).toBe(0);
    await s.page.getByRole("button", { name: "Preview" }).click();
  });
});

describe("integration-monitor under Oxygen", () => {
  let s: Session;

  beforeAll(async () => {
    s = await open("integration-monitor");
  });

  afterAll(async () => {
    await close(s);
  });

  it("switches tabs and opens and closes a dialog and a drawer", async () => {
    await s.app.getByRole("row", { name: "#8812" }).click();
    await s.app.getByRole("heading", { name: "Run #8812 · Salesforce → ERP" }).waitFor();
    await s.app.getByRole("tab", { name: "Log" }).click();
    await s.app.getByText("Run started").waitFor();

    // Escape the dialog handles is the prototype's own: it must not reach the host (which would close a console review).
    await s.page.evaluate(() => {
      const w = window as unknown as { __escapes: number };
      w.__escapes = 0;
      window.addEventListener("message", (e) => {
        if ((e.data as { type?: string } | null)?.type === "proto:escape") w.__escapes++;
      });
    });
    await s.app.getByRole("button", { name: "Replay run" }).click();
    const dialog = s.app.getByRole("dialog", { name: "Replay run #8812?" });
    await dialog.waitFor();
    await s.app.getByRole("button", { name: "Cancel" }).press("Escape");
    await dialog.waitFor({ state: "hidden" });
    await s.page.waitForTimeout(300);
    expect(await s.page.evaluate(() => (window as unknown as { __escapes: number }).__escapes)).toBe(0);

    await s.app.getByRole("tab", { name: "Summary" }).click();
    await s.app.getByRole("button", { name: "View payload" }).click();
    const drawer = s.app.getByRole("dialog", { name: "First failed message" });
    await drawer.waitFor();
    await s.app.getByRole("button", { name: "Close" }).click();
    await drawer.waitFor({ state: "hidden" });
  });
});

describe("expense-approval under Oxygen", () => {
  let s: Session;

  beforeAll(async () => {
    s = await open("expense-approval");
  });

  afterAll(async () => {
    await close(s);
  });

  it("moves between steps", async () => {
    await s.app.getByRole("heading", { name: "Approval queue" }).waitFor();
    await s.page.getByRole("combobox", { name: "Role" }).selectOption({ label: "Employee" });
    await s.app.locator('[data-proto-key="nav.new"]').click();
    await s.app.getByRole("heading", { name: "New expense" }).waitFor();
    await s.app.getByRole("spinbutton", { name: "Amount", exact: true }).waitFor();
    await s.app.getByRole("button", { name: "Next" }).click();
    await s.app.getByText("Attach the receipt. Photos and PDFs are accepted.").waitFor();
    await s.app.getByRole("button", { name: "Back" }).click();
    await s.app.getByRole("spinbutton", { name: "Amount", exact: true }).waitFor();
  });
});

describe("the app shell under Oxygen", () => {
  let s: Session;

  beforeAll(async () => {
    s = await open("app-shell");
  });

  afterAll(async () => {
    await close(s);
  });

  it("draws the header, user menu and side navigation, follows the role, and signs out and back in, fetching nothing", async () => {
    await s.app.getByRole("heading", { name: "My requests" }).waitFor();
    // The host covered the frame while the runtime started, and uncovers it once the app draws.
    await s.page.getByText("Loading the prototype…").waitFor({ state: "hidden" });
    await s.app.getByText("Leave requests", { exact: true }).waitFor();

    await s.page.evaluate(() => {
      const w = window as unknown as { __escapes: number };
      w.__escapes = 0;
      window.addEventListener("message", (e) => {
        if ((e.data as { type?: string } | null)?.type === "proto:escape") w.__escapes++;
      });
    });
    await s.app.getByRole("button", { name: "Dana Lee, Employee" }).click();
    const account = s.app.getByRole("menuitem", { name: "Account" });
    await account.waitFor();
    // Escape closing the menu is the prototype's own: it must not reach the host.
    await account.press("Escape");
    await account.waitFor({ state: "hidden" });
    await s.page.waitForTimeout(300);
    expect(await s.page.evaluate(() => (window as unknown as { __escapes: number }).__escapes)).toBe(0);

    await s.app.getByRole("button", { name: "Dana Lee, Employee" }).click();
    await account.click();
    await s.app.getByRole("heading", { name: "Account" }).waitFor();
    await s.app.getByRole("menuitem", { name: "Settings" }).waitFor({ state: "hidden" });
    expect(await s.app.locator('[data-proto-key="menu.team"]').count()).toBe(0);
    expect(await s.app.locator('[data-proto-key="nav.team"]').count()).toBe(0);

    await s.page.getByRole("combobox", { name: "Role" }).selectOption({ label: "Manager" });
    await s.app.getByRole("button", { name: "Priya Shah, Manager" }).click();
    // The prototype's own entry shows for the role that reaches its screen.
    await s.app.getByRole("menuitem", { name: "My team" }).waitFor();
    await s.app.getByRole("menuitem", { name: "Sign out" }).click();
    await s.app.getByRole("heading", { name: "You are signed out" }).waitFor();
    await s.app.getByRole("button", { name: "Sign in" }).click();
    await s.app.locator('[data-proto-key="nav.team"]').click();
    await s.app.getByRole("heading", { name: "Team requests" }).waitFor();

    expect(s.requests.filter((url) => !url.startsWith(s.preview.url))).toEqual([]);
  });

  it("draws in the scheme the host names, and in the system's when it names none", async () => {
    const frame = s.page.frames().find((f) => f !== s.page.mainFrame())!;
    const scheme = () => frame.evaluate(() => document.documentElement.getAttribute("data-color-scheme"));
    expect(await scheme()).toBe("light");
    // As the console does: the view the host sends carries its resolved scheme.
    const send = (colorScheme?: string) =>
      s.page.evaluate((colorScheme) => {
        const frameWindow = document.querySelector<HTMLIFrameElement>('iframe[title$="prototype app"]')!.contentWindow!;
        const view = { mode: "preview", roleId: "manager", stateId: "state.default", screenId: "screen.team", selectedKeys: [], pins: {}, ...(colorScheme ? { colorScheme } : {}) };
        frameWindow.postMessage({ type: "proto:view", view }, "*");
      }, colorScheme);
    await send("dark");
    await expect.poll(scheme).toBe("dark");
    const background = await frame.evaluate(() => getComputedStyle(document.body).backgroundColor);
    expect(background).toBe("rgb(15, 18, 22)");
    await send();
    await expect.poll(scheme).toBe("light");
  });

  it("selects the user menu in Annotate instead of opening it", async () => {
    await s.page.getByRole("button", { name: "Annotate" }).click();
    const user = s.app.locator('[data-proto-key="shell.user"]');
    await user.click();
    expect(await user.getAttribute("aria-pressed")).toBe("true");
    expect(await s.app.getByRole("menuitem", { name: "Account" }).count()).toBe(0);
    await s.page.getByRole("button", { name: "Preview" }).click();
  });
});

describe("stats, sections and row actions under Oxygen", () => {
  let s: Session;

  beforeAll(async () => {
    s = await open("team-leave");
  });

  afterAll(async () => {
    await close(s);
  });

  beforeEach(async () => {
    await s.page.reload();
    await s.app.getByRole("heading", { name: "My Leave" }).waitFor();
  });

  it("draws the stats at one width, and the section's heading with its own action", async () => {
    const widths = await s.app.locator('[data-proto-key^="stat.balance."]').evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().width)));
    expect(widths).toHaveLength(3);
    expect(new Set(widths).size).toBe(1);
    await s.app.getByText("of 20 left this year").waitFor();
    await s.app.getByRole("heading", { name: "My Requests", level: 2 }).waitFor();
    await s.app.getByRole("button", { name: "New request" }).first().click();
    await s.app.getByRole("heading", { name: "New Leave Request" }).waitFor();
  });

  it("acts on a row's action in Preview without pressing the row, and selects it in Annotate without acting", async () => {
    await s.page.getByRole("combobox", { name: "Role" }).selectOption({ label: "Manager" });
    await s.app.getByRole("heading", { name: "Pending Requests" }).waitFor();
    await s.app.locator('[data-proto-key="row.team-queue.req-2001.approve"]').click();
    await s.app.getByText("Alex Doe").waitFor({ state: "hidden" });
    await s.app.getByRole("heading", { name: "Pending Requests" }).waitFor();

    await s.page.getByRole("button", { name: "Annotate" }).click();
    const reject = s.app.locator('[data-proto-key="row.team-queue.req-2002.reject"]');
    await expect.poll(() => reject.getAttribute("data-proto-annotating")).toBe("");
    await reject.click();
    expect(await reject.getAttribute("aria-pressed")).toBe("true");
    expect(await s.app.locator('[data-proto-key="row.team-queue.req-2002"]').getAttribute("aria-pressed")).toBe("false");
    await s.page.getByText("Selected: Reject").waitFor();
    expect(await s.app.getByRole("heading", { name: "Request from Sam Lee" }).count()).toBe(0);
    await s.page.getByRole("button", { name: "Preview" }).click();
  });
});

describe("a row's overflow actions under Oxygen", () => {
  let s: Session;

  beforeAll(async () => {
    s = await open("row-actions");
  });

  afterAll(async () => {
    await close(s);
  });

  it("puts more than two actions behind one menu, whose entries act without pressing the row", async () => {
    await s.app.getByRole("heading", { name: "Documents", exact: true }).waitFor();
    // One action stays a button; three go behind the menu.
    await s.app.locator('[data-proto-key="row.doc-1.open"]').and(s.app.getByRole("button", { name: "Open" })).waitFor();
    expect(await s.app.getByRole("button", { name: "More actions" }).count()).toBe(1);
    await s.app.getByRole("button", { name: "More actions" }).click();
    await s.app.getByRole("menuitem", { name: "Delete" }).click();
    await s.app.getByText("Travel guide").waitFor({ state: "hidden" });
    await s.app.getByRole("heading", { name: "Documents", exact: true }).waitFor();
  });
});
