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
 * Comment pins, as the frame draws them under the default theme: each a
 * button named by its comment number, in both modes, on an element the kit
 * wraps and over one it cannot (a table row); pressing a pin opens its comment
 * in the host, so it neither selects nor acts in the prototype.
 */

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { app, driver, host } from "./driver.js";
import type { Preview } from "./protocol.js";

let preview: Preview;
let page: string;
let row: string;

beforeAll(async () => {
  preview = await driver.startPreview("contacts");
  page = await driver.openPage(preview.url);
  await driver.waitFor(page, app.heading("Acme contacts"));
  row = await driver.evalInApp(page, `document.querySelector('[data-proto-root][data-proto-key^="row."]').dataset.protoKey`);
});

afterAll(async () => {
  await driver.closePage(page);
  await driver.stopPreview(preview.id);
});

/** Whether the pin named `name` sits on the top-right corner of the element `key`. */
const onCorner = (name: string, key: string) =>
  driver.evalInApp(
    page,
    `(() => {
      const pin = document.querySelector('[aria-label="${name}"]').getBoundingClientRect();
      const el = document.querySelector('[data-proto-key="${key}"]').getBoundingClientRect();
      return String(Math.abs(pin.right - el.right) < 24 && pin.top < el.top + 24 && pin.bottom > el.top - 24);
    })()`,
  );

describe("comment pins", () => {
  it("draws a queued comment's pin as a button named by its number, on a wrapped element and over a table row", async () => {
    await driver.click(page, host.button("Comment"));
    await driver.frameMode(page, "annotate");
    await driver.click(page, app.element("btn.new"));
    await driver.fill(page, host.field("Comment"), "Make this button green");
    await driver.click(page, host.button("Add"));
    await driver.click(page, app.element(row));
    await driver.fill(page, host.field("Comment"), "Show the phone number too");
    await driver.click(page, host.button("Add"));

    await driver.waitFor(page, app.button("Comment 1"));
    await driver.waitFor(page, app.button("Comment 2"));
    expect(await onCorner("Comment 2", row)).toBe("true");
    expect(await driver.evalInApp(page, `(() => { const pin = document.querySelector('[aria-label="Comment 1"]'); pin.focus(); return String(document.activeElement === pin); })()`)).toBe("true");
  });

  it("keeps a pin's click its own in Comment mode: nothing is selected", async () => {
    await driver.click(page, app.button("Comment 2"));
    expect(await driver.read(page, app.element(row), "pressed")).toBe("false");
    expect(await driver.read(page, app.element("btn.new"), "pressed")).toBe("false");
  });

  it("draws the pins in Preview too, where a pin's click does not act", async () => {
    await driver.click(page, host.button("Preview"));
    await driver.frameMode(page, "preview");
    await driver.waitFor(page, app.button("Comment 1"));
    await driver.click(page, app.button("Comment 2"));
    await driver.click(page, app.button("Comment 1"));
    expect(await driver.read(page, host.address(), "text")).toBe("prototype://screen.contacts");
  });

  it("draws a draft's pin hollow, and focuses the pin a host names when its bubble closes", async () => {
    // As the host would send them (the frame takes a message whose source is its parent).
    const fromHost = (message: object) =>
      driver.evalInApp(page, `window.dispatchEvent(new MessageEvent("message", { data: ${JSON.stringify(message)}, source: window.parent })); new Promise((r) => setTimeout(r, 300))`);
    const view = { mode: "preview", roleId: "editor", stateId: "state.default", screenId: "screen.contacts", selectedKeys: [], pins: { "btn.new": [1] }, drafts: ["btn.new", row] };
    await fromHost({ type: "proto:view", view });
    expect(await driver.count(page, app.button("Draft comment"))).toBe(2);
    const style = (selector: string) => driver.evalInApp(page, `getComputedStyle(document.querySelector('${selector}')).borderTopStyle`);
    expect(await style('[aria-label="Draft comment"]')).toBe("dashed");
    expect(await style('[aria-label="Comment 1"]')).toBe("solid");

    await fromHost({ type: "proto:focus", key: row, requests: [] });
    expect(await driver.evalInApp(page, `document.activeElement.getAttribute("aria-label") + " " + document.activeElement.dataset.protoPinFor`)).toBe(`Draft comment ${row}`);
    await fromHost({ type: "proto:focus", key: "btn.new", requests: [1] });
    expect(await driver.evalInApp(page, `document.activeElement.getAttribute("aria-label")`)).toBe("Comment 1");
  });

  it("draws whole-screen comments' pins at their spots of the document, scrolling with it, hollow for a draft, and focuses one a host names", async () => {
    const fromHost = (message: object) =>
      driver.evalInApp(page, `window.dispatchEvent(new MessageEvent("message", { data: ${JSON.stringify(message)}, source: window.parent })); new Promise((r) => setTimeout(r, 300))`);
    const screenPins = [{ at: { x: 300, y: 260 }, number: 3 }, { at: { x: 200, y: 900 } }];
    const view = { mode: "preview", roleId: "editor", stateId: "state.default", screenId: "screen.contacts", selectedKeys: [], pins: {}, screenPins };
    await driver.evalInApp(page, `document.body.style.paddingBottom = "2000px"`);
    await fromHost({ type: "proto:view", view });
    const centre = (name: string) =>
      driver.evalInApp(page, `(() => { const r = document.querySelector('[aria-label="${name}"]').getBoundingClientRect(); return [Math.round(r.x + r.width / 2), Math.round(r.y + r.height / 2 + scrollY)].join(","); })()`);
    expect(await centre("Comment 3")).toBe("300,260");
    expect(await centre("Draft comment on the screen")).toBe("200,900");
    await driver.evalInApp(page, `window.scrollTo(0, 100)`);
    expect(await centre("Comment 3")).toBe("300,260");
    expect(await driver.evalInApp(page, `getComputedStyle(document.querySelector('[aria-label="Draft comment on the screen"]')).borderTopStyle`)).toBe("dashed");

    await fromHost({ type: "proto:focus-screen-pin", requests: [3] });
    expect(await driver.evalInApp(page, `document.activeElement.getAttribute("aria-label")`)).toBe("Comment 3");
    await fromHost({ type: "proto:focus-screen-pin", requests: [] });
    expect(await driver.evalInApp(page, `document.activeElement.getAttribute("aria-label")`)).toBe("Draft comment on the screen");
    await driver.evalInApp(page, `window.scrollTo(0, 0); document.body.style.paddingBottom = ""`);
  });
});
