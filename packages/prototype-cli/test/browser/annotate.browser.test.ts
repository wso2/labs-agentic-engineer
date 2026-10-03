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

/** Annotate: a click selects and never acts; queued requests are saved to .prototype/feedback.json for the agent. */

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { MAX_FEEDBACK_REQUESTS, MAX_FEEDBACK_TEXT } from "@wso2/prototype-kit/feedback";
import { app, driver, host } from "./driver.js";
import type { Preview } from "./protocol.js";

let preview: Preview;
let page: string;

beforeAll(async () => {
  preview = await driver.startPreview("contacts");
  page = await driver.openPage(preview.url);
  await driver.waitFor(page, app.heading("Acme contacts"));
});

afterAll(async () => {
  await driver.closePage(page);
  await driver.stopPreview(preview.id);
});

describe("prototype preview — Annotate", () => {
  it("selects instead of acting, queues two requests, and saves them as feedback", async () => {
    await driver.click(page, host.button("Annotate"));
    expect(await driver.read(page, host.button("Annotate"), "pressed")).toBe("true");
    await driver.frameMode(page, "annotate");

    // A click on the button selects it; it does not navigate.
    await driver.click(page, app.element("btn.new"));
    expect(await driver.read(page, app.element("btn.new"), "pressed")).toBe("true");
    expect(await driver.read(page, host.picker("Screen"), "value")).toBe("screen.contacts");
    await driver.waitFor(page, host.text("Selected: New contact"));
    await driver.fill(page, host.field("Request"), "Make this button green");
    await driver.click(page, host.button("Add request"));

    await driver.click(page, app.element("heading.contacts"));
    await driver.fill(page, host.field("Request"), "Call this page People");
    await driver.click(page, host.button("Add request"));
    await driver.waitFor(page, { where: "app", elementId: "heading.contacts" });
    expect(await driver.count(page, { where: "app", text: "1" })).toBeGreaterThan(0);

    await driver.click(page, host.button("Save feedback"));
    await driver.waitFor(page, host.text("Saved 2 requests to .prototype/feedback.json"));

    const saved = JSON.parse((await driver.readFile(preview.id, ".prototype/feedback.json"))!) as Record<string, unknown>;
    expect(saved).toEqual({
      schemaVersion: 1,
      savedAt: expect.stringMatching(/^\d{4}-\d{2}-\d{2}T/),
      prototypeHash: await driver.revisionHash(preview.id),
      requests: [
        { screenId: "screen.contacts", roleId: "editor", stateId: "state.default", elementIds: ["btn.new"], text: "Make this button green" },
        { screenId: "screen.contacts", roleId: "editor", stateId: "state.default", elementIds: ["heading.contacts"], text: "Call this page People" },
      ],
    });
  });

  it("stays put in Annotate when the frame asks to navigate or a navigating control is clicked", async () => {
    expect(await driver.read(page, host.button("Annotate"), "pressed")).toBe("true");
    // A request straight from the (untrusted) frame to a screen the role does reach.
    await driver.evalInApp(page, `parent.postMessage({ type: "proto:navigate", screenId: "screen.settings" }, "*"); new Promise((r) => setTimeout(r, 500))`);
    expect(await driver.read(page, host.picker("Screen"), "value")).toBe("screen.contacts");
    expect(await driver.count(page, app.heading("Settings"))).toBe(0);
    // A click on a navigating control selects it and does not follow.
    await driver.click(page, app.element("nav.settings"));
    await driver.evalInApp(page, `new Promise((r) => setTimeout(r, 500))`);
    expect(await driver.read(page, app.element("nav.settings"), "pressed")).toBe("true");
    expect(await driver.read(page, host.picker("Screen"), "value")).toBe("screen.contacts");
    expect(await driver.count(page, app.heading("Settings"))).toBe(0);
  });

  it("acts again in Preview", async () => {
    await driver.click(page, host.button("Preview"));
    await driver.frameMode(page, "preview");
    await driver.click(page, app.button("New contact"));
    await driver.waitFor(page, app.heading("New contact"));
  });
});

describe("prototype preview — Annotate across a revision", () => {
  it("keeps the queue, says it was made against an earlier version, and saves the original hash", async () => {
    const p = await driver.startPreview("contacts");
    const pg = await driver.openPage(p.url);
    try {
      await driver.waitFor(pg, app.heading("Acme contacts"));
      const originalHash = await driver.revisionHash(p.id);
      await driver.click(pg, host.button("Annotate"));
      await driver.frameMode(pg, "annotate");
      await driver.click(pg, app.element("btn.new"));
      await driver.fill(pg, host.field("Request"), "Make this button green");
      await driver.click(pg, host.button("Add request"));
      const source = (await driver.readFile(p.id, "prototype.tsx"))!;
      await driver.writeFile(p.id, "prototype.tsx", source.replace("`${company} contacts`", "`${company} people`"));
      await driver.waitFor(pg, app.heading("Acme people"));
      await driver.waitFor(pg, host.text("Queued against an earlier version of the prototype."));
      await driver.click(pg, host.button("Save feedback"));
      await driver.waitFor(pg, host.text("Saved 1 request to .prototype/feedback.json"));
      const saved = JSON.parse((await driver.readFile(p.id, ".prototype/feedback.json"))!) as { prototypeHash: string };
      expect(saved.prototypeHash).toBe(originalHash);
      expect(await driver.revisionHash(p.id)).not.toBe(originalHash);
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});

describe("prototype preview — Annotate limits", () => {
  it("limits the request text to what the server accepts and stops adding at the queue cap, saying why", async () => {
    const p = await driver.startPreview("contacts");
    const pg = await driver.openPage(p.url);
    try {
      await driver.waitFor(pg, app.heading("Acme contacts"));
      await driver.click(pg, host.button("Annotate"));
      await driver.frameMode(pg, "annotate");
      
      expect(await driver.read(pg, host.field("Request"), "maxlength")).toBe(String(MAX_FEEDBACK_TEXT));
      await driver.fill(pg, host.field("Request"), "Fine");
      for (let i = 0; i < MAX_FEEDBACK_REQUESTS; i++) {
        await driver.click(pg, host.button("Add request"));
        await driver.fill(pg, host.field("Request"), "Fine");
      }
      await driver.waitFor(pg, host.text(`The queue is full (${MAX_FEEDBACK_REQUESTS} requests)`, true));
      expect(await driver.read(pg, host.button("Add request"), "disabled")).toBe("true");
      await driver.click(pg, host.button("Save feedback"));
      await driver.waitFor(pg, host.text(`Saved ${MAX_FEEDBACK_REQUESTS} requests to .prototype/feedback.json`));
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});

describe("prototype preview — Escape", () => {
  it("clears the selection on Escape in the page, but not on Escape in a focused form control", async () => {
    const p = await driver.startPreview("expense-approval");
    const pg = await driver.openPage(p.url);
    try {
      await driver.waitFor(pg, app.heading("Approval queue"));
      await driver.click(pg, host.button("Annotate"));
      await driver.frameMode(pg, "annotate");
      await driver.click(pg, app.element("heading.queue"));
      await driver.waitFor(pg, host.text("Selected: Approval queue"));
      expect(await driver.read(pg, app.element("heading.queue"), "pressed")).toBe("true");

      // Annotate makes the controls read-only, so enable one: Escape closing its native picker is the prototype's own,
      // and the host (whose only reaction to Escape is clearing the selection) must leave the selection alone.
      await driver.evalInApp(pg, `document.querySelectorAll("select").forEach((s) => (s.disabled = false))`);
      await driver.press(pg, { where: "app", role: "combobox", name: "Team" }, "Escape");
      await driver.evalInApp(pg, `new Promise((r) => setTimeout(r, 500))`);
      expect(await driver.read(pg, app.element("heading.queue"), "pressed")).toBe("true");

      // Escape on the page itself is the host's.
      await driver.evalInApp(pg, `document.activeElement.blur(); document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }))`);
      await driver.waitFor(pg, host.text("Selected: Approval queue"), "hidden");
      expect(await driver.read(pg, app.element("heading.queue"), "pressed")).toBe("false");
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});
