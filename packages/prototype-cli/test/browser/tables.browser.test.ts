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

/** `prototype preview` of a screen built from stat groups, sections and tables with a status column and row actions. */

import { afterAll, beforeAll, beforeEach, describe, expect, it } from "vitest";
import { app, driver, host } from "./driver.js";
import type { Preview } from "./protocol.js";

describe("prototype preview — stats, sections and row actions", () => {
  let preview: Preview;
  let page: string;

  beforeAll(async () => {
    preview = await driver.startPreview("team-leave");
    page = await driver.openPage(preview.url);
  });

  afterAll(async () => {
    await driver.closePage(page);
    await driver.stopPreview(preview.id);
  });

  beforeEach(async () => {
    // A reload starts from the seed, the entry screen, the first role and Preview.
    await driver.reloadPage(page);
    await driver.waitFor(page, app.heading("My Leave"));
  });

  async function asManager() {
    await driver.select(page, host.picker("Role"), "Manager");
    await driver.waitFor(page, app.heading("Pending Requests"));
  }

  it("draws each stat with its hint, and the section's own action", async () => {
    await driver.waitFor(page, app.text("of 20 left this year"));
    await driver.waitFor(page, app.heading("My Requests"));
    await driver.click(page, app.button("New request"));
    await driver.waitFor(page, app.heading("New Leave Request"));
  });

  it("draws the status column as badges, in the column the table declares", async () => {
    expect(await driver.read(page, app.row("Family trip"), "text")).toMatch(/Family trip\s+3\s+Pending\s+Cancel/);
  });

  it("acts on a row action in Preview, and the row does not also act", async () => {
    // The row leads nowhere: only the action opens the dialog.
    await driver.click(page, app.element("row.my-request.req-1001.cancel"));
    await driver.waitFor(page, app.dialog("Cancel this request?"));
    await driver.click(page, app.button("Cancel request"));
    await driver.waitFor(page, app.row("Family trip"), "hidden");

    // The row leads to the request: Approve changes the data and stays; Reject follows its own target with params.
    await asManager();
    await driver.click(page, app.element("row.team-queue.req-2001.approve"));
    await driver.waitFor(page, app.row("Alex Doe"), "hidden");
    expect(await driver.read(page, host.picker("Screen"), "value")).toBe("screen.team-queue");
    await driver.click(page, app.element("row.team-queue.req-2002.reject"));
    await driver.waitFor(page, app.heading("Request from Sam Lee"));
  });

  it("selects a row action in Annotate, not its row, and neither acts", async () => {
    await asManager();
    await driver.click(page, host.button("Annotate"));
    await driver.frameMode(page, "annotate");

    await driver.click(page, app.element("row.team-queue.req-2001.approve"));
    await driver.waitFor(page, host.text("Selected: Approve"));
    expect(await driver.read(page, app.element("row.team-queue.req-2001.approve"), "pressed")).toBe("true");
    expect(await driver.read(page, app.element("row.team-queue.req-2001"), "pressed")).toBe("false");
    expect(await driver.count(page, app.text("Alex Doe"))).toBe(1);
    expect(await driver.read(page, host.picker("Screen"), "value")).toBe("screen.team-queue");

    // A click elsewhere in the row is still the row's.
    await driver.click(page, app.text("Family trip"));
    expect(await driver.read(page, app.element("row.team-queue.req-2001"), "pressed")).toBe("true");

    await driver.click(page, host.button("Preview"));
    await driver.frameMode(page, "preview");
  });
});
