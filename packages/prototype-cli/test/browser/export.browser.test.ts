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

/** An exported prototype, opened as a file: it plays offline and makes no network request. */

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { app, driver, host } from "./driver.js";

let page: string;

beforeAll(async () => {
  page = await driver.openPage(await driver.exportFixture("contacts"));
  await driver.waitFor(page, app.heading("Acme contacts"));
});

afterAll(async () => {
  await driver.closePage(page);
});

describe("prototype export — the file, opened", () => {
  it("plays a flow and changes mock data", async () => {
    await driver.click(page, app.button("New contact"));
    await driver.fill(page, app.field("Name"), "Grace Hopper");
    await driver.fill(page, app.field("Email"), "grace@example.com");
    await driver.click(page, app.button("Save"));
    await driver.waitFor(page, app.heading("Grace Hopper"));
  });

  it("has the pickers and Reset data, but no Comment mode", async () => {
    await driver.waitFor(page, host.picker("Role"));
    await driver.waitFor(page, host.button("Reset data"));
    expect(await driver.count(page, host.button("Comment"))).toBe(0);
  });

  it("made no network request", async () => {
    const remote = (await driver.requests(page)).filter((url) => !/^(file|data|about):/.test(url));
    expect(remote).toEqual([]);
  });
});
