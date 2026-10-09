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
 * Comment mode (internally Annotate): a click selects (never acts) and opens a comment bubble at the
 * element; added comments leave pins that open back into the bubble; the
 * floating bar counts and lists them and saves them to
 * .prototype/feedback.json for the agent.
 */

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { MAX_FEEDBACK_REQUESTS, MAX_FEEDBACK_TEXT } from "@wso2/prototype-kit/feedback";
import { app, driver, host } from "./driver.js";
import type { Box, Preview, Target } from "./protocol.js";

const count = (n: number) => `${n} ${n === 1 ? "comment" : "comments"}`;
/** A queued comment's entry in the bar's list, by a part of its text. */
const entry = (text: string): Target => ({ where: "host", role: "button", name: text, partial: true });
const settle = (pg: string, ms = 300) => driver.evalInApp(pg, `new Promise((r) => setTimeout(r, ${ms}))`);
/** Waits until `read` changes from `before`. */
async function moved(read: () => Promise<Box>, before: Box): Promise<Box> {
  for (let i = 0; i < 50; i++) {
    const now = await read();
    if (now.x !== before.x || now.y !== before.y) return now;
    await new Promise((r) => setTimeout(r, 50));
  }
  return read();
}
/** The bubble is drawn just below or just above `el`, overlapping it across (it is kept inside the window). */
function besides(bubble: Box, el: Box) {
  const below = bubble.y >= el.y + el.height && bubble.y - (el.y + el.height) < 24;
  const above = bubble.y + bubble.height <= el.y && el.y - (bubble.y + bubble.height) < 24;
  expect(below || above, `bubble ${JSON.stringify(bubble)} beside ${JSON.stringify(el)}`).toBe(true);
  expect(bubble.x <= el.x + el.width && bubble.x + bubble.width >= el.x, `bubble ${JSON.stringify(bubble)} across ${JSON.stringify(el)}`).toBe(true);
}

async function open(fixture: string, heading: string) {
  const p = await driver.startPreview(fixture);
  const pg = await driver.openPage(p.url);
  await driver.waitFor(pg, app.heading(heading));
  return { p, pg };
}

async function annotate(pg: string) {
  await driver.click(pg, host.button("Comment"));
  await driver.frameMode(pg, "annotate");
}

/** A spot of empty space (nothing there takes a comment, in Comment mode), `dx`, `dy` in from the bottom-right of the app frame's viewport. */
async function emptySpot(pg: string, dx = 60, dy = 60): Promise<{ x: number; y: number }> {
  const [w, h] = (await driver.evalInApp(pg, `[innerWidth, innerHeight].join(",")`)).split(",").map(Number);
  const spot = { x: w! - dx, y: h! - dy };
  const hit = `String(document.elementFromPoint(${spot.x}, ${spot.y})?.closest("[data-proto-annotating], .proto-pin")?.outerHTML ?? "nothing")`;
  expect(await driver.evalInApp(pg, hit)).toBe("nothing");
  return spot;
}

/** Where the centre of the app's pin named `name` is, in the app frame's viewport. */
async function pinCentre(pg: string, name: string): Promise<{ x: number; y: number }> {
  await driver.waitFor(pg, app.button(name));
  const at = await driver.evalInApp(pg, `(() => { const r = document.querySelector('[aria-label="${name}"]').getBoundingClientRect(); return [Math.round(r.x + r.width / 2), Math.round(r.y + r.height / 2)].join(","); })()`);
  const [x, y] = at.split(",").map(Number);
  return { x: x!, y: y! };
}

/** Somewhere on the host page outside any bubble: the header's title. */
const outside: Target = { where: "host", role: "heading", name: "Contacts" };

let preview: Preview;
let page: string;

beforeAll(async () => {
  ({ p: preview, pg: page } = await open("contacts", "Acme contacts"));
});

afterAll(async () => {
  await driver.closePage(page);
  await driver.stopPreview(preview.id);
});

describe("prototype preview — commenting in Comment mode", () => {
  it("opens a bubble at the clicked element, queues with Add and Cmd/Ctrl+Enter, and saves the feedback file", async () => {
    await annotate(page);
    expect(await driver.read(page, host.button("Comment"), "pressed")).toBe("true");

    // A click on the button selects it and opens the bubble beside it; it does not navigate.
    await driver.click(page, app.element("btn.new"));
    await driver.waitFor(page, host.dialog("Comment on New contact"));
    expect(await driver.read(page, app.element("btn.new"), "pressed")).toBe("true");
    expect(await driver.read(page, host.address(), "text")).toBe("prototype://screen.contacts");
    besides(await driver.box(page, host.dialog("Comment on New contact")), await driver.box(page, app.element("btn.new")));
    await driver.fill(page, host.field("Comment"), "Make this button green");
    await driver.click(page, host.button("Add"));
    await driver.waitFor(page, host.dialog("Comment on New contact"), "hidden");
    await driver.waitFor(page, app.button("Comment 1"));
    expect(await driver.read(page, app.element("btn.new"), "pressed")).toBe("false");

    await driver.click(page, app.element("heading.contacts"));
    await driver.fill(page, host.field("Comment"), "Call this page People");
    await driver.press(page, host.field("Comment"), "ControlOrMeta+Enter");
    await driver.waitFor(page, app.button("Comment 2"));
    await driver.waitFor(page, host.button("2 comments"));

    await driver.click(page, host.button("Save feedback"));
    await driver.waitFor(page, host.text("Saved 2 comments to .prototype/feedback.json"));
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

  it("points one comment at several elements with Shift-click, keeping what was typed", async () => {
    await driver.click(page, app.element("nav.settings"));
    await driver.fill(page, host.field("Comment"), "Group these");
    await driver.clickWith(page, app.element("nav.contacts"), "Shift");
    await driver.waitFor(page, host.dialog("Comment on Settings, Contacts"));
    expect(await driver.read(page, host.field("Comment"), "value")).toBe("Group these");
    await driver.click(page, host.button("Add"));
    await driver.waitFor(page, host.button("3 comments"));
    // Selecting does not follow a navigating control.
    expect(await driver.read(page, host.address(), "text")).toBe("prototype://screen.contacts");
    expect(await driver.count(page, app.heading("Settings"))).toBe(0);
  });

  it("keeps the bubble at its element as the prototype scrolls and the window resizes", async () => {
    await driver.evalInApp(page, `document.body.style.paddingBottom = "2000px"`);
    await driver.click(page, app.element("heading.contacts"));
    const bubble = host.dialog("Comment on Acme contacts");
    await driver.waitFor(page, bubble);
    const before = await driver.box(page, bubble);
    await driver.evalInApp(page, `window.scrollBy(0, 40)`);
    const scrolled = await moved(() => driver.box(page, bubble), before);
    expect(scrolled.y).toBeCloseTo(before.y - 40, 0);
    besides(scrolled, await driver.box(page, app.element("heading.contacts")));

    await driver.resize(page, 900, 700);
    await settle(page);
    besides(await driver.box(page, bubble), await driver.box(page, app.element("heading.contacts")));
    await driver.resize(page, 1280, 720);
    await driver.evalInApp(page, `window.scrollTo(0, 0); document.body.style.paddingBottom = ""`);
    await driver.pressKey(page, "Escape");
    await driver.pressKey(page, "Escape");
  });

  it("opens a pin to read, edit and remove its comment", async () => {
    await driver.click(page, app.button("Comment 1"));
    await driver.waitFor(page, host.dialog("Comment 1"));
    expect(await driver.read(page, host.dialog("Comment 1"), "text")).toContain("Make this button green");
    await driver.click(page, host.button("Edit"));
    await driver.fill(page, host.field("Comment"), "Make this button blue");
    await driver.click(page, host.button("Save"));
    await driver.waitFor(page, host.text("Make this button blue"));

    await driver.click(page, app.button("Comment 2"));
    await driver.click(page, host.button("Remove"));
    await driver.waitFor(page, host.dialog("Comment 2"), "hidden");
    await driver.waitFor(page, host.button("2 comments"));
    // The rest renumber: the Shift-click comment is 2 now.
    await driver.click(page, host.button("2 comments"));
    expect(await driver.read(page, { where: "host", role: "list", name: "Queued comments" }, "text")).not.toContain("Call this page People");
    await driver.click(page, host.button("2 comments"));
  });

  it("opens pins in Preview too, without acting", async () => {
    await driver.click(page, host.button("Preview"));
    await driver.frameMode(page, "preview");
    await driver.click(page, app.button("Comment 1"));
    await driver.waitFor(page, host.dialog("Comment 1"));
    expect(await driver.read(page, host.address(), "text")).toBe("prototype://screen.contacts");
    await driver.pressKey(page, "Escape");
    await driver.waitFor(page, host.dialog("Comment 1"), "hidden");
  });

  it("keeps typed text as a hollow draft pin on a click away, and restores it when reopened", async () => {
    await annotate(page);
    await driver.click(page, app.element("heading.contacts"));
    await driver.fill(page, host.field("Comment"), "Half a thought");
    await driver.click(page, outside); // the header's title, outside the bubble
    await driver.waitFor(page, host.dialog("Comment on Acme contacts"), "hidden");
    await driver.waitFor(page, app.button("Draft comment"));
    await driver.waitFor(page, host.button("2 comments")); // a draft is not counted

    await driver.click(page, app.button("Draft comment"));
    await driver.waitFor(page, host.dialog("Comment on Acme contacts"));
    expect(await driver.read(page, host.field("Comment"), "value")).toBe("Half a thought");
    await driver.pressKey(page, "Escape");
    await driver.pressKey(page, "Escape");
  });

  it("closes an empty bubble without a draft", async () => {
    await driver.click(page, app.element("btn.new"));
    await driver.waitFor(page, host.dialog("Comment on New contact"));
    await driver.click(page, outside);
    await driver.waitFor(page, host.dialog("Comment on New contact"), "hidden");
    expect(await driver.count(page, app.button("Draft comment"))).toBe(1);
    // The click (on a heading) took no focus: it goes back to the element the bubble was on.
    const focused = `new Promise((resolve) => { const t = Date.now(); const check = () => { const k = document.activeElement?.closest("[data-proto-key]")?.getAttribute("data-proto-key"); k === "btn.new" || Date.now() - t > 2000 ? resolve(String(k)) : setTimeout(check, 20); }; check(); })`;
    expect(await driver.evalInApp(page, focused)).toBe("btn.new");
  });

  it("comments on the whole screen by clicking empty space: the bubble opens at the spot, and the pin is left there", async () => {
    const spot = await emptySpot(page);
    const clicked = await driver.clickAppAt(page, spot.x, spot.y);
    const bubble = host.dialog("Comment on Contacts (whole screen)");
    await driver.waitFor(page, bubble);
    besides(await driver.box(page, bubble), { ...clicked, width: 0, height: 0 });
    // A hollow pin marks the spot while the comment is written.
    expect(await pinCentre(page, "Draft comment on the screen")).toEqual(spot);
    await driver.fill(page, host.field("Comment"), "Too much whitespace");

    // A click away keeps the text as the screen's draft, its hollow pin at the spot, which reopens it.
    await driver.click(page, outside);
    await driver.waitFor(page, bubble, "hidden");
    await driver.click(page, app.button("Draft comment on the screen"));
    await driver.waitFor(page, bubble);
    expect(await driver.read(page, host.field("Comment"), "value")).toBe("Too much whitespace");
    await driver.click(page, host.button("Add"));
    await driver.waitFor(page, host.button("3 comments"));
    expect(await pinCentre(page, "Comment 3")).toEqual(spot);
    expect(await driver.count(page, app.button("Draft comment on the screen"))).toBe(0);
    await driver.click(page, host.button("3 comments"));
    expect(await driver.read(page, entry("Too much whitespace"), "text")).toContain("Whole screen");
    await driver.click(page, host.button("3 comments"));
  });

  it("closes an open bubble on a click on empty space, keeping its text as a draft, before opening another", async () => {
    await driver.click(page, app.element("heading.contacts"));
    await driver.fill(page, host.field("Comment"), "Later");
    const spot = await emptySpot(page, 120, 120);
    await driver.clickAppAt(page, spot.x, spot.y);
    await driver.waitFor(page, host.dialog("Comment on Acme contacts"), "hidden");
    expect(await driver.count(page, host.dialog("Comment on Contacts (whole screen)"))).toBe(0);
    await driver.waitFor(page, app.button("Draft comment"));
    await driver.pressKey(page, "Escape");
    // The next click there comments on the screen.
    await driver.clickAppAt(page, spot.x, spot.y);
    await driver.waitFor(page, host.dialog("Comment on Contacts (whole screen)"));
    await driver.pressKey(page, "Escape");
    await driver.waitFor(page, host.dialog("Comment on Contacts (whole screen)"), "hidden");
    // Nothing typed: no draft, no hollow pin left (the frame is told a moment later).
    await driver.waitFor(page, app.button("Draft comment on the screen"), "hidden");
  });

  it("keeps a whole-screen comment's pin at its spot as the prototype scrolls, and opens it to read and edit", async () => {
    const before = await pinCentre(page, "Comment 3");
    await driver.evalInApp(page, `document.body.style.paddingBottom = "2000px"; window.scrollBy(0, 40)`);
    expect(await pinCentre(page, "Comment 3")).toEqual({ x: before.x, y: before.y - 40 });
    await driver.click(page, app.button("Comment 3"));
    const bubble = host.dialog("Comment 3");
    await driver.waitFor(page, bubble);
    // At the spot: the pin's centre.
    const pin = await driver.box(page, app.button("Comment 3"));
    besides(await driver.box(page, bubble), { x: pin.x + pin.width / 2, y: pin.y + pin.height / 2, width: 0, height: 0 });
    await driver.click(page, host.button("Edit"));
    await driver.fill(page, host.field("Comment"), "Far too much whitespace");
    await driver.click(page, host.button("Save"));
    await driver.waitFor(page, host.text("Far too much whitespace"));
    await driver.pressKey(page, "Escape");
    await driver.waitFor(page, bubble, "hidden");
    await driver.evalInApp(page, `window.scrollTo(0, 0); document.body.style.paddingBottom = ""`);
  });

  it("comments on the whole screen from the list too, for the keyboard, the bubble at the dock", async () => {
    await driver.click(page, host.button("3 comments"));
    await driver.click(page, host.button("Comment on this screen"));
    const bubble = host.dialog("Comment on Contacts (whole screen)");
    await driver.waitFor(page, bubble);
    const dock = await driver.box(page, host.dock());
    const placed = await driver.box(page, bubble);
    expect(placed.y + placed.height).toBeLessThanOrEqual(dock.y);
    expect(await driver.count(page, host.button("Comment on screen"))).toBe(0);
    await driver.pressKey(page, "Escape");
    await driver.waitFor(page, bubble, "hidden");
  });

  it("lists comments across screens and states; an entry goes there and opens it", async () => {
    await driver.select(page, host.picker("State"), "Empty");
    await driver.click(page, app.element("heading.contacts"));
    await driver.fill(page, host.field("Comment"), "Say why it is empty");
    await driver.click(page, host.button("Add"));
    await driver.select(page, host.picker("State"), "Default");
    // Away by using the prototype, in Preview.
    await driver.click(page, host.button("Preview"));
    await driver.frameMode(page, "preview");
    await driver.click(page, app.element("nav.settings"));
    await driver.waitFor(page, app.heading("Settings"));

    await driver.click(page, host.button("4 comments"));
    await driver.click(page, entry("Say why it is empty"));
    await driver.waitFor(page, host.dialog("Comment 4"));
    expect(await driver.read(page, host.address(), "text")).toBe("prototype://screen.contacts?state=state.empty");
    expect(await driver.read(page, host.picker("State"), "value")).toBe("state.empty");
    expect(await driver.read(page, host.button("Comment"), "pressed")).toBe("true");
    await driver.pressKey(page, "Escape");
  });

  it("toggles Comment mode with C, but not while typing", async () => {
    await driver.pressKey(page, "c");
    expect(await driver.read(page, host.button("Preview"), "pressed")).toBe("true");
    await driver.pressKey(page, "c");
    expect(await driver.read(page, host.button("Comment"), "pressed")).toBe("true");
    await driver.click(page, app.element("btn.new"));
    await driver.press(page, host.field("Comment"), "c");
    expect(await driver.read(page, host.field("Comment"), "value")).toBe("c");
    expect(await driver.read(page, host.button("Comment"), "pressed")).toBe("true");
  });

  it("undoes the nearest thing on Escape: the bubble, then the selection", async () => {
    // The bubble from the last test is open on btn.new.
    await driver.pressKey(page, "Escape");
    await driver.waitFor(page, host.dialog("Comment on New contact"), "hidden");
    expect(await driver.read(page, app.element("btn.new"), "pressed")).toBe("true");
    await driver.pressKey(page, "Escape");
    // Focus went back into the frame: this Escape reaches the host as the frame's message, so the selection clears a moment later.
    const cleared = `new Promise((resolve) => { const t = Date.now(); const check = () => { const p = document.querySelector('[data-proto-key="btn.new"]')?.getAttribute("aria-pressed"); p === "false" || Date.now() - t > 2000 ? resolve(String(p)) : setTimeout(check, 20); }; check(); })`;
    expect(await driver.evalInApp(page, cleared)).toBe("false");
  });

  it("acts again in Preview", async () => {
    await driver.click(page, host.button("Preview"));
    await driver.frameMode(page, "preview");
    await driver.click(page, app.button("New contact"));
    await driver.waitFor(page, app.heading("New contact"));
  });
});

describe("prototype preview — comments across a revision", () => {
  it("keeps the queue, marks the comment written on the earlier version, and saves the original hash", async () => {
    const { p, pg } = await open("contacts", "Acme contacts");
    try {
      const originalHash = await driver.revisionHash(p.id);
      await annotate(pg);
      await driver.click(pg, app.element("btn.new"));
      await driver.fill(pg, host.field("Comment"), "Make this button green");
      await driver.click(pg, host.button("Add"));
      const source = (await driver.readFile(p.id, "prototype.tsx"))!;
      await driver.writeFile(p.id, "prototype.tsx", source.replace("`${company} contacts`", "`${company} people`"));
      await driver.waitFor(pg, app.heading("Acme people"));
      await driver.waitFor(pg, host.text("1 comment was written on an earlier version of the prototype."));
      // A comment written on the revision showing is not marked.
      const spot = await emptySpot(pg);
      await driver.clickAppAt(pg, spot.x, spot.y);
      await driver.fill(pg, host.field("Comment"), "Say people, not contacts");
      await driver.click(pg, host.button("Add"));
      await driver.waitFor(pg, host.button("2 comments"));
      await driver.waitFor(pg, host.text("1 comment was written on an earlier version of the prototype."));
      await driver.click(pg, host.button("Save feedback"));
      await driver.waitFor(pg, host.text("Saved 2 comments to .prototype/feedback.json"));
      const saved = JSON.parse((await driver.readFile(p.id, ".prototype/feedback.json"))!) as { prototypeHash: string };
      expect(saved.prototypeHash).toBe(originalHash);
      expect(await driver.revisionHash(p.id)).not.toBe(originalHash);
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});

describe("prototype preview — comments across screens and roles, saved as one batch", () => {
  it("comments on elements and empty space on two screens and as another role, lists them all, saves one batch, and pins each screen's again there", async () => {
    const { p, pg } = await open("contacts", "Acme contacts");
    const comment = async (text: string) => {
      await driver.fill(pg, host.field("Comment"), text);
      await driver.click(pg, host.button("Add"));
    };
    try {
      await annotate(pg);
      // The contacts screen: an element, and a spot of empty space.
      await driver.click(pg, app.element("btn.new"));
      await comment("Make this button green");
      const onContacts = await emptySpot(pg);
      await driver.clickAppAt(pg, onContacts.x, onContacts.y);
      await comment("The list feels empty");

      // To the settings screen by clicking through the prototype, where the contacts screen's pins are not drawn.
      await driver.click(pg, host.button("Preview"));
      await driver.frameMode(pg, "preview");
      await driver.click(pg, app.element("nav.settings"));
      await driver.waitFor(pg, app.heading("Settings"));
      expect(await driver.count(pg, app.button("Comment 1"))).toBe(0);
      expect(await driver.count(pg, app.button("Comment 2"))).toBe(0);
      await annotate(pg);
      await driver.click(pg, app.element("heading.settings"));
      await comment("Call this Preferences");
      const onSettings = await emptySpot(pg, 100, 100);
      await driver.clickAppAt(pg, onSettings.x, onSettings.y);
      await comment("Say what saving does");

      // As another role, on the same screen.
      await driver.select(pg, host.picker("Role"), "Viewer");
      await driver.frameMode(pg, "annotate");
      await driver.click(pg, app.element("heading.settings"));
      await comment("Tell viewers why they cannot edit");

      await driver.waitFor(pg, host.button("5 comments"));
      await driver.click(pg, host.button("5 comments"));
      const list = await driver.read(pg, { where: "host", role: "list", name: "Queued comments" }, "text");
      for (const text of ["Make this button green", "The list feels empty", "Call this Preferences", "Say what saving does", "Tell viewers why they cannot edit"]) expect(list).toContain(text);
      await driver.click(pg, host.button("5 comments"));

      await driver.click(pg, host.button("Save feedback"));
      await driver.waitFor(pg, host.text("Saved 5 comments to .prototype/feedback.json"));
      const saved = JSON.parse((await driver.readFile(p.id, ".prototype/feedback.json"))!) as { requests: unknown[] };
      const on = (screenId: string, roleId: string, elementIds: string[], text: string) => ({ screenId, roleId, stateId: "state.default", elementIds, text });
      // One batch, and a whole-screen comment's spot is the review's own: never saved.
      expect(saved.requests).toEqual([
        on("screen.contacts", "editor", ["btn.new"], "Make this button green"),
        on("screen.contacts", "editor", [], "The list feels empty"),
        on("screen.settings", "editor", ["heading.settings"], "Call this Preferences"),
        on("screen.settings", "editor", [], "Say what saving does"),
        on("screen.settings", "viewer", ["heading.settings"], "Tell viewers why they cannot edit"),
      ]);

      // Each screen's pins are drawn again there.
      expect(await pinCentre(pg, "Comment 4")).toEqual(onSettings);
      await driver.select(pg, host.picker("Role"), "Editor");
      await driver.click(pg, host.button("Preview"));
      await driver.frameMode(pg, "preview");
      await driver.click(pg, app.element("nav.contacts"));
      await driver.waitFor(pg, app.heading("Acme contacts"));
      await driver.waitFor(pg, app.button("Comment 1"));
      expect(await pinCentre(pg, "Comment 2")).toEqual(onContacts);
      expect(await driver.count(pg, app.button("Comment 4"))).toBe(0);

      // A whole-screen comment's pin removes it.
      await driver.click(pg, app.button("Comment 2"));
      await driver.click(pg, host.button("Remove"));
      await driver.waitFor(pg, host.button("4 comments"));
      await driver.waitFor(pg, app.button("Comment 2"), "hidden");
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});

describe("prototype preview — comment limits", () => {
  it("limits the comment to what the server accepts and stops adding at the queue cap, saying why", async () => {
    const { p, pg } = await open("contacts", "Acme contacts");
    try {
      await annotate(pg);
      const spot = await emptySpot(pg);
      await driver.clickAppAt(pg, spot.x, spot.y);
      expect(await driver.read(pg, host.field("Comment"), "maxlength")).toBe(String(MAX_FEEDBACK_TEXT));
      await driver.fill(pg, host.field("Comment"), "x".repeat(MAX_FEEDBACK_TEXT - 10));
      await driver.waitFor(pg, host.text(`${MAX_FEEDBACK_TEXT - 10} / ${MAX_FEEDBACK_TEXT}`));
      await driver.fill(pg, host.field("Comment"), "Fine");
      await driver.click(pg, host.button("Add"));
      // The rest from the list's "Comment on this screen" (a click on the spot would open its pin).
      for (let i = 1; i < MAX_FEEDBACK_REQUESTS; i++) {
        await driver.click(pg, host.button(`${count(i)}`));
        await driver.click(pg, host.button("Comment on this screen"));
        await driver.fill(pg, host.field("Comment"), "Fine");
        await driver.click(pg, host.button("Add"));
      }
      await driver.waitFor(pg, host.text(`The queue is full (${MAX_FEEDBACK_REQUESTS} comments)`, true));
      await driver.click(pg, host.button(`${MAX_FEEDBACK_REQUESTS} comments`));
      expect(await driver.read(pg, host.button("Comment on this screen"), "disabled")).toBe("true");
      await driver.click(pg, host.button(`${MAX_FEEDBACK_REQUESTS} comments`));
      await driver.click(pg, app.element("btn.new"));
      await driver.fill(pg, host.field("Comment"), "One more");
      expect(await driver.read(pg, host.button("Add"), "disabled")).toBe("true");
      await driver.click(pg, host.button("Save feedback"));
      await driver.waitFor(pg, host.text(`Saved ${MAX_FEEDBACK_REQUESTS} comments to .prototype/feedback.json`));
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});

/** A comment cursor: an arrow with a bubble beside it, hotspot at the arrow's tip, and its fallback; null for any other cursor. */
function commentCursor(cursor: string): { svg: string; fallback: string } | null {
  const m = /^url\("data:image\/svg\+xml,(.+)"\) 3 2, ([a-z-]+)$/.exec(cursor);
  return m ? { svg: decodeURIComponent(m[1]!), fallback: m[2]! } : null;
}
const ORANGE = /fill="#ff7300"/i;

describe("prototype preview — the hover highlight", () => {
  it("outlines only the innermost element under the pointer, not the ones holding it", async () => {
    const { p, pg } = await open("contacts", "Acme contacts");
    try {
      await annotate(pg);
      const [heading, button] = await driver.outlinesOnHover(pg, app.element("btn.new"), ["heading.contacts", "btn.new"]);
      expect(button).not.toBe("rgba(0, 0, 0, 0)");
      expect(heading).toBe("rgba(0, 0, 0, 0)");
      // The holder still takes the highlight where the pointer is on it and on nothing inside.
      const [own] = await driver.outlinesOnHover(pg, app.element("heading.contacts"), ["heading.contacts"]);
      expect(own).not.toBe("rgba(0, 0, 0, 0)");
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});

describe("prototype preview — the Preview and Comment tools", () => {
  it("switches with the tools and V and C, signals Comment mode, and draws the comment cursor only there", async () => {
    const { p, pg } = await open("contacts", "Acme contacts");
    const signals = async () => [await driver.count(pg, host.text("Comment mode", true)), await driver.count(pg, host.text("Click anything to comment"))];
    try {
      // Preview: the default cursors, and no mode signals.
      expect(await driver.read(pg, host.button("Preview"), "pressed")).toBe("true");
      expect(await signals()).toEqual([0, 0]);
      expect(commentCursor(await driver.cursorAt(pg, app.element("heading.contacts")))).toBeNull();
      expect(commentCursor(await driver.cursorAt(pg, null))).toBeNull();

      await annotate(pg);
      expect(await driver.read(pg, host.button("Comment"), "pressed")).toBe("true");
      expect(await driver.read(pg, host.button("Preview"), "pressed")).toBe("false");
      expect(await signals()).toEqual([1, 1]);
      // Over an element that takes a comment: the solid orange bubble with a "+"; over empty space, the hollow one.
      const over = commentCursor(await driver.cursorAt(pg, app.element("heading.contacts")));
      expect(over?.svg).toMatch(ORANGE);
      expect(over?.fallback).toBe("crosshair");
      const empty = commentCursor(await driver.cursorAt(pg, null));
      expect(empty).not.toBeNull();
      expect(empty?.svg).not.toMatch(ORANGE);

      // V returns to Preview; C toggles Comment mode.
      await driver.pressKey(pg, "v");
      await driver.frameMode(pg, "preview");
      expect(await driver.read(pg, host.button("Preview"), "pressed")).toBe("true");
      expect(await signals()).toEqual([0, 0]);
      await driver.pressKey(pg, "v");
      expect(await driver.read(pg, host.button("Preview"), "pressed")).toBe("true");
      await driver.pressKey(pg, "c");
      await driver.frameMode(pg, "annotate");
      expect(await driver.read(pg, host.button("Comment"), "pressed")).toBe("true");
      await driver.click(pg, host.button("Preview"));
      await driver.frameMode(pg, "preview");
      expect(await signals()).toEqual([0, 0]);
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });

  it("says how to start in the empty list", async () => {
    const { p, pg } = await open("contacts", "Acme contacts");
    try {
      await driver.click(pg, host.button("0 comments"));
      await driver.waitFor(pg, host.text("Press C or choose Comment, then click anything", true));
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});

describe("prototype preview — Escape in the prototype", () => {
  it("clears the selection on Escape in the app, but not on Escape in a focused form control", async () => {
    const { p, pg } = await open("expense-approval", "Approval queue");
    try {
      await annotate(pg);
      await driver.click(pg, app.element("heading.queue"));
      await driver.waitFor(pg, host.dialog("Comment on Approval queue"));
      await driver.pressKey(pg, "Escape");
      await driver.waitFor(pg, host.dialog("Comment on Approval queue"), "hidden");
      expect(await driver.read(pg, app.element("heading.queue"), "pressed")).toBe("true");

      // Annotate makes the controls read-only, so enable one: Escape closing its native picker is the prototype's own,
      // and the host must leave the selection alone.
      await driver.evalInApp(pg, `document.querySelectorAll("select").forEach((s) => (s.disabled = false))`);
      await driver.press(pg, { where: "app", role: "combobox", name: "Team" }, "Escape");
      await settle(pg, 500);
      expect(await driver.read(pg, app.element("heading.queue"), "pressed")).toBe("true");

      // Escape on the app's page itself is the host's.
      await driver.evalInApp(pg, `document.activeElement.blur(); document.body.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }))`);
      await settle(pg);
      expect(await driver.read(pg, app.element("heading.queue"), "pressed")).toBe("false");
    } finally {
      await driver.closePage(pg);
      await driver.stopPreview(p.id);
    }
  });
});
