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

/** `prototype preview`, played: navigation with params, roles and states, and mock data created, edited and deleted across screens. */

import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { app, driver, host } from "./driver.js";
import type { Preview } from "./protocol.js";

describe("prototype preview — playing a prototype", () => {
  let preview: Preview;
  let page: string;

  beforeAll(async () => {
    preview = await driver.startPreview("contacts");
    page = await driver.openPage(preview.url);
  });

  afterAll(async () => {
    await driver.closePage(page);
    await driver.stopPreview(preview.id);
  });

  beforeEach(async () => {
    // A reload starts from the seed and the entry screen (no --persist).
    await driver.reloadPage(page);
    await driver.waitFor(page, app.heading("Acme contacts"));
  });

  it("covers the app while it starts and uncovers it once it draws", async () => {
    await driver.waitFor(page, host.text("Loading the prototype…"), "hidden");
    expect(await driver.read(page, { where: "host", role: "status" }, "count")).toBe(0);
  });

  it("navigates with params: a row opens that contact, and the host follows", async () => {
    await driver.click(page, app.row("Alan Turing"));
    await driver.waitFor(page, app.heading("Alan Turing"));
    await driver.waitFor(page, app.text("contacts-2"));
    expect(await driver.read(page, host.picker("Screen"), "value")).toBe("screen.contact");
  });

  it("shows the current screen, and a non-default state, in the browser window's address bar", async () => {
    await driver.waitFor(page, host.text("prototype://screen.contacts"));
    await driver.click(page, app.row("Alan Turing"));
    await driver.waitFor(page, host.text("prototype://screen.contact"));
    await driver.select(page, host.picker("State"), "Empty");
    await driver.waitFor(page, host.text("prototype://screen.contact?state=state.empty"));
  });

  it("renders differently per role", async () => {
    expect(await driver.count(page, app.button("New contact"))).toBe(1);
    await driver.select(page, host.picker("Role"), "Viewer");
    await driver.waitFor(page, app.button("New contact"), "hidden");
    expect(await driver.count(page, app.button("New contact"))).toBe(0);
  });

  it("renders differently per display state", async () => {
    await driver.select(page, host.picker("State"), "Empty");
    await driver.waitFor(page, app.text("No contacts yet"));
  });

  it("creates a contact on one screen that shows on another, with the next deterministic id", async () => {
    await driver.click(page, app.button("New contact"));
    await driver.fill(page, app.field("Name"), "Grace Hopper");
    await driver.fill(page, app.field("Email"), "grace@example.com");
    await driver.click(page, app.button("Save"));
    await driver.waitFor(page, app.heading("Grace Hopper"));
    await driver.waitFor(page, app.text("contacts-4"));
    await driver.waitFor(page, app.text("2026-01-15"));
    await driver.click(page, app.element("nav.contacts"));
    await driver.waitFor(page, app.row("Grace Hopper"));
  });

  it("validates a form before it submits", async () => {
    await driver.click(page, app.button("New contact"));
    await driver.fill(page, app.field("Email"), "not-an-email");
    await driver.click(page, app.button("Save"));
    await driver.waitFor(page, app.text("Fix 2 problems"));
    await driver.waitFor(page, app.heading("New contact"));
    await driver.fill(page, app.field("Name"), "Grace Hopper");
    await driver.waitFor(page, app.text("Fix 1 problem"));
  });

  it("stays put when the frame asks for a screen the role cannot reach", async () => {
    // The render check rejects a control that targets such a screen, so the request comes straight from the (untrusted) frame.
    await driver.click(page, app.row("Alan Turing"));
    await driver.waitFor(page, app.heading("Alan Turing"));
    await driver.select(page, host.picker("Role"), "Viewer");
    await driver.waitFor(page, app.button("Edit"), "hidden");
    await driver.evalInApp(page, `parent.postMessage({ type: "proto:navigate", screenId: "screen.edit" }, "*"); new Promise((r) => setTimeout(r, 500))`);
    expect(await driver.read(page, host.picker("Screen"), "value")).toBe("screen.contact");
    expect(await driver.count(page, app.heading("Edit Alan Turing"))).toBe(0);
    // A screen the role does reach still follows.
    await driver.evalInApp(page, `parent.postMessage({ type: "proto:navigate", screenId: "screen.settings" }, "*")`);
    await driver.waitFor(page, app.heading("Settings"));
  });

  it("edits and deletes a record", async () => {
    await driver.click(page, app.row("Ada Lovelace"));
    await driver.click(page, app.button("Edit"));
    await driver.fill(page, app.field("Email"), "ada@analytical.example");
    await driver.click(page, app.button("Save changes"));
    await driver.waitFor(page, app.text("ada@analytical.example"));
    await driver.click(page, app.button("Delete"));
    await driver.waitFor(page, app.heading("Acme contacts"));
    expect(await driver.count(page, app.row("Ada Lovelace"))).toBe(0);
    expect(await driver.count(page, app.row("Alan Turing"))).toBe(1);
  });

  it("carries a shared value across screens", async () => {
    await driver.click(page, app.element("nav.settings"));
    await driver.fill(page, app.field("Company"), "Globex");
    await driver.click(page, app.button("Save settings"));
    // A required switch must be on: off is a problem, on passes.
    await driver.waitFor(page, app.text("Confirm company change is required"));
    expect(await driver.count(page, app.heading("Globex contacts"))).toBe(0);
    await driver.click(page, app.field("Confirm company change"));
    await driver.click(page, app.button("Save settings"));
    await driver.waitFor(page, app.heading("Globex contacts"));
  });

  it("gives the app no network and no access to the host page", async () => {
    const before = (await driver.requests(page)).length;
    const fetched = await driver.evalInApp(page, `fetch(${JSON.stringify(`${preview.url}frame-runtime.js`)}).then(() => "reached", (e) => "blocked: " + e.name)`);
    expect(fetched).toBe("blocked: TypeError");
    const reachedHost = await driver.evalInApp(page, `(() => { try { return String(parent.document.title); } catch (e) { return "denied: " + e.name; } })()`);
    expect(reachedHost).toBe("denied: SecurityError");
    const storage = await driver.evalInApp(page, `(() => { try { return String(localStorage.length); } catch (e) { return "denied: " + e.name; } })()`);
    expect(storage).toBe("denied: SecurityError");
    expect((await driver.requests(page)).slice(before)).toEqual([]);
  });
});

describe("prototype preview — keyboard submit", () => {
  let preview: Preview;
  let page: string;

  beforeAll(async () => {
    preview = await driver.startPreview("contacts");
    page = await driver.openPage(preview.url);
  });

  afterAll(async () => {
    await driver.closePage(page);
    await driver.stopPreview(preview.id);
  });

  it("submits a form on Enter in a single-line field: validation first, then onSubmit", async () => {
    await driver.waitFor(page, app.heading("Acme contacts"));
    await driver.click(page, app.button("New contact"));
    await driver.press(page, app.field("Name"), "Enter");
    await driver.waitFor(page, app.text("Fix 2 problems"));
    await driver.fill(page, app.field("Name"), "Grace Hopper");
    await driver.fill(page, app.field("Email"), "grace@example.com");
    await driver.press(page, app.field("Email"), "Enter");
    await driver.waitFor(page, app.heading("Grace Hopper"));
  });
});

describe("prototype preview — tabs, dialogs and drawers", () => {
  let preview: Preview;
  let page: string;

  beforeAll(async () => {
    preview = await driver.startPreview("integration-monitor");
    page = await driver.openPage(preview.url);
  });

  afterAll(async () => {
    await driver.closePage(page);
    await driver.stopPreview(preview.id);
  });

  beforeEach(async () => {
    await driver.reloadPage(page);
    await driver.click(page, app.row("#8812"));
    await driver.waitFor(page, app.heading("Run #8812 · Salesforce → ERP"));
  });

  it("switches the tab panel", async () => {
    await driver.waitFor(page, app.text("Open the run log"));
    await driver.click(page, app.tab("Log"));
    await driver.waitFor(page, app.text("Run started"));
    await driver.waitFor(page, app.text("Open the run log"), "hidden");
    await driver.click(page, app.tab("Summary"));
    await driver.waitFor(page, app.text("Open the run log"));
    expect(await driver.count(page, app.text("Run started"))).toBe(0);
  });

  it("opens a dialog from a button and closes it with its action and with Escape", async () => {
    await driver.click(page, app.button("Replay run"));
    await driver.waitFor(page, app.dialog("Replay run #8812?"));
    await driver.click(page, app.button("Cancel"));
    await driver.waitFor(page, app.dialog("Replay run #8812?"), "hidden");
    await driver.click(page, app.button("Replay run"));
    await driver.waitFor(page, app.dialog("Replay run #8812?"));
    // The theme closes a dialog on Escape pressed inside it (focus on its control).
    await driver.press(page, app.button("Cancel"), "Escape");
    await driver.waitFor(page, app.dialog("Replay run #8812?"), "hidden");
  });

  it("opens a drawer from a button and closes it with its close button", async () => {
    await driver.click(page, app.button("View payload"));
    await driver.waitFor(page, app.dialog("First failed message"));
    await driver.click(page, app.button("Close"));
    await driver.waitFor(page, app.dialog("First failed message"), "hidden");
  });
});

describe("prototype preview — stepper", () => {
  let preview: Preview;
  let page: string;

  beforeAll(async () => {
    preview = await driver.startPreview("expense-approval");
    page = await driver.openPage(preview.url);
  });

  afterAll(async () => {
    await driver.closePage(page);
    await driver.stopPreview(preview.id);
  });

  it("moves between steps with the steps and with its own buttons", async () => {
    await driver.waitFor(page, app.heading("Approval queue"));
    await driver.select(page, host.picker("Role"), "Employee");
    await driver.waitFor(page, app.element("nav.new"));
    await driver.click(page, app.element("nav.new"));
    await driver.waitFor(page, app.heading("New expense"));
    await driver.waitFor(page, app.field("Amount"));
    await driver.click(page, app.button("Next"));
    await driver.waitFor(page, app.text("Attach the receipt. Photos and PDFs are accepted."));
    await driver.click(page, app.button("Back"));
    await driver.waitFor(page, app.field("Amount"));
    await driver.click(page, { where: "app", role: "button", name: "Review", partial: true });
    await driver.waitFor(page, app.button("Submit for approval"));
  });
});

describe("prototype preview — the app shell", () => {
  let preview: Preview;
  let page: string;
  const entry = (name: string) => ({ where: "app" as const, role: "menuitem", name });

  beforeAll(async () => {
    preview = await driver.startPreview("app-shell");
    page = await driver.openPage(preview.url);
  });

  afterAll(async () => {
    await driver.closePage(page);
    await driver.stopPreview(preview.id);
  });

  it("opens Account and Settings from the user menu, shows the viewing role's user, and signs out and back in", async () => {
    await driver.waitFor(page, app.heading("My requests"));
    await driver.click(page, app.button("Dana Lee"));
    await driver.waitFor(page, app.text("Employee"));
    await driver.click(page, entry("Account"));
    await driver.waitFor(page, app.heading("Account"));
    expect(await driver.read(page, host.picker("Screen"), "value")).toBe("screen.account");
    await driver.waitFor(page, entry("Settings"), "hidden");

    // A navigation entry shows only for the role that reaches its screen, and the header follows the role.
    expect(await driver.count(page, app.element("nav.team"))).toBe(0);
    expect(await driver.count(page, app.element("menu.team"))).toBe(0);
    await driver.select(page, host.picker("Role"), "Manager");
    await driver.waitFor(page, app.button("Priya Shah"));
    await driver.waitFor(page, app.element("nav.team"));

    await driver.click(page, app.button("Priya Shah"));
    await driver.waitFor(page, entry("My team"));
    await driver.click(page, entry("Sign out"));
    await driver.waitFor(page, app.heading("You are signed out"));
    expect(await driver.count(page, app.button("Priya Shah"))).toBe(0);
    await driver.click(page, app.button("Sign in"));
    await driver.waitFor(page, app.heading("My requests"));
  });

  it("selects the user menu in Annotate instead of opening it", async () => {
    await driver.click(page, host.button("Annotate"));
    await driver.frameMode(page, "annotate");
    await driver.click(page, app.element("shell.user"));
    await driver.waitFor(page, host.text("Selected: Priya Shah"));
    expect(await driver.count(page, entry("Account"))).toBe(0);
    await driver.click(page, host.button("Preview"));
  });
});

describe("prototype preview — a frame runtime that fails as it loads", () => {
  it("says why instead of leaving the loading cover up", async () => {
    const p = await driver.startPreviewOnFrameRuntime("baseline", 'throw new Error("the theme failed to load");');
    const pg = await driver.openPage(p.url);
    try {
      await driver.waitFor(pg, { where: "host", role: "alert" });
      expect(await driver.read(pg, { where: "host", role: "alert" }, "text")).toContain("the theme failed to load");
      expect(await driver.count(pg, { where: "host", role: "status" })).toBe(0);
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});
