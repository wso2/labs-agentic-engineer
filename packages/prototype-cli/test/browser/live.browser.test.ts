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
 * `prototype preview`'s live loop: a rewrite on disk shows without a reload,
 * findings overlay the last good render, Reset data reseeds, and --persist
 * keeps changes across a reload until the source changes.
 */

import { afterEach, describe, expect, it } from "vitest";
import { app, driver, host } from "./driver.js";
import type { Preview } from "./protocol.js";

let preview: Preview | undefined;
let page: string | undefined;

afterEach(async () => {
  if (page) await driver.closePage(page);
  if (preview) await driver.stopPreview(preview.id);
  page = undefined;
  preview = undefined;
});

async function open(flags: string[] = []): Promise<{ preview: Preview; page: string; source: string }> {
  preview = await driver.startPreview("contacts", flags);
  page = await driver.openPage(preview.url);
  await driver.waitFor(page, app.heading("Acme contacts"));
  return { preview, page, source: (await driver.readFile(preview.id, "prototype.tsx"))! };
}

async function addGrace(p: string) {
  await driver.click(p, app.button("New contact"));
  await driver.fill(p, app.field("Name"), "Grace Hopper");
  await driver.fill(p, app.field("Email"), "grace@example.com");
  await driver.click(p, app.button("Save"));
  await driver.waitFor(p, app.heading("Grace Hopper"));
  await driver.click(p, app.element("nav.contacts"));
  await driver.waitFor(p, app.row("Grace Hopper"));
}

describe("prototype preview — the live loop", () => {
  it("shows a rewritten source without a manual reload", async () => {
    const { preview, page, source } = await open();
    await driver.writeFile(preview.id, "prototype.tsx", source.replace("`${company} contacts`", "`${company} people`"));
    await driver.waitFor(page, app.heading("Acme people"));
  });

  it("overlays findings and keeps the last good render, then recovers", async () => {
    const { preview, page, source } = await open();
    await driver.writeFile(preview.id, "prototype.tsx", source.replace("export default defineApp(", "const broken = <div />;\nexport default defineApp("));
    await driver.waitFor(page, host.region("Check findings"));
    expect(await driver.read(page, host.region("Check findings"), "text")).toContain("FORBIDDEN_ELEMENT");
    expect(await driver.read(page, host.region("Check findings"), "text")).toContain("showing the last good version");
    expect(await driver.count(page, app.heading("Acme contacts"))).toBe(1);
    await driver.writeFile(preview.id, "prototype.tsx", source);
    await driver.waitFor(page, host.region("Check findings"), "hidden");
  });

  it("shows findings without the last-good phrase when no good revision exists yet", async () => {
    preview = await driver.startPreview("invalid/source-date-now");
    page = await driver.openPage(preview.url);
    await driver.waitFor(page, host.region("Check findings"));
    const text = await driver.read(page, host.region("Check findings"), "text");
    expect(text).toContain("finding");
    expect(text).not.toContain("last good version");
  });

  // Review Focus: an agent writes the files in steps — an empty source, a deleted manifest.
  it("survives half-written files: an empty source and a missing manifest show findings, not a crash", async () => {
    const { preview, page, source } = await open();
    const manifest = (await driver.readFile(preview.id, "prototype.json"))!;
    await driver.writeFile(preview.id, "prototype.tsx", "");
    await driver.waitFor(page, host.text("NO_APP", true));
    await driver.removeFile(preview.id, "prototype.json");
    await driver.waitFor(page, host.text("MISSING_FILE", true));
    expect(await driver.count(page, app.heading("Acme contacts"))).toBe(1);
    await driver.writeFile(preview.id, "prototype.json", manifest);
    await driver.writeFile(preview.id, "prototype.tsx", source);
    await driver.waitFor(page, host.region("Check findings"), "hidden");
    await driver.waitFor(page, app.heading("Acme contacts"));
  });

  it("a view sent before any app is loaded does not report a draw", async () => {
    const { page } = await open();
    expect(await driver.viewBeforeLoad(page)).toBe("proto:ready");
  });

  it("Reset data returns to the seed", async () => {
    const { page } = await open();
    await addGrace(page);
    await driver.click(page, host.button("Reset data"));
    await driver.waitFor(page, app.row("Grace Hopper"), "hidden");
    expect(await driver.count(page, app.row("Ada Lovelace"))).toBe(1);
  });

  it("forgets changes on reload without --persist", async () => {
    const { page } = await open();
    await addGrace(page);
    await driver.reloadPage(page);
    await driver.waitFor(page, app.heading("Acme contacts"));
    expect(await driver.count(page, app.row("Grace Hopper"))).toBe(0);
  });

  it("with --persist, keeps changes across a reload, and starts from the seed when the source changes", async () => {
    const { preview, page, source } = await open(["--persist"]);
    await addGrace(page);
    await driver.reloadPage(page);
    await driver.waitFor(page, app.row("Grace Hopper"));
    await driver.writeFile(preview.id, "prototype.tsx", source.replace("`${company} contacts`", "`${company} people`"));
    await driver.waitFor(page, app.heading("Acme people"));
    expect(await driver.count(page, app.row("Grace Hopper"))).toBe(0);
  });

  // Review Focus: stored data the host cannot use (corrupt JSON, or the wrong kind for a key) falls back to the seed.
  it("with --persist, starts from the seed when the stored snapshot is unusable", async () => {
    const { preview, page } = await open(["--persist"]);
    await addGrace(page);
    const key = `proto:data:${await driver.revisionHash(preview.id)}`;
    await driver.setStorage(page, key, "{not json");
    await driver.reloadPage(page);
    await driver.waitFor(page, app.heading("Acme contacts"));
    expect(await driver.count(page, app.row("Grace Hopper"))).toBe(0);
    await driver.setStorage(page, key, JSON.stringify({ company: [], contacts: "not a collection" }));
    await driver.reloadPage(page);
    await driver.waitFor(page, app.heading("Acme contacts"));
    expect(await driver.count(page, app.row("Ada Lovelace"))).toBe(1);
  });

  // Carry-over: the frame is untrusted, so a snapshot that is not JSON data is dropped, not stored.
  it("with --persist, ignores a malformed data message from the frame", async () => {
    const { page } = await open(["--persist"]);
    await addGrace(page);
    await driver.evalInApp(page, 'parent.postMessage({ type: "proto:data", data: { company: "Evil", contacts: [{ id: "contacts-9" }], bad: NaN } }, "*")');
    await driver.evalInApp(page, 'parent.postMessage({ type: "proto:data", data: [] }, "*")');
    await driver.reloadPage(page);
    await driver.waitFor(page, app.row("Grace Hopper"));
    expect(await driver.count(page, app.heading("Acme contacts"))).toBe(1);
  });

  // Carry-over: a frame document that reloads announces itself again and is given its app again.
  it("recovers when the frame document reloads", async () => {
    const { page } = await open();
    await driver.evalInApp(page, "window.__beforeReload = true");
    await driver.evalInApp(page, "setTimeout(() => location.reload(), 0)");
    await driver.waitFor(page, app.heading("Acme contacts"));
    expect(await driver.evalInApp(page, "String(window.__beforeReload)")).toBe("undefined");
  });

  // Carry-over: a revision that removes the screen being viewed opens on the entry screen, with no error from the old view.
  it("moves off a screen the new revision removes, without an error alert", async () => {
    const { preview, page, source } = await open();
    const manifest = (await driver.readFile(preview.id, "prototype.json"))!;
    await driver.click(page, app.element("nav.settings"));
    await driver.waitFor(page, app.heading("Settings"));
    await driver.writeFile(preview.id, "prototype.json", manifest.replace('    { "id": "screen.settings", "name": "Settings", "roleIds": ["editor", "viewer"] }\n', "").replace('"screen.edit", "name": "Edit contact", "roleIds": ["editor"] },', '"screen.edit", "name": "Edit contact", "roleIds": ["editor"] }'));
    await driver.writeFile(
      preview.id,
      "prototype.tsx",
      source.replace('      { id: "nav.settings", label: "Settings", to: "screen.settings" },\n', "").replace('    "screen.settings": Settings,\n', ""),
    );
    await driver.waitFor(page, app.heading("Acme contacts"));
    expect(await driver.count(page, host.alert())).toBe(0);
    expect(await driver.read(page, host.address(), "text")).toBe("prototype://screen.contacts");
  });

  // Fix round: a file that cannot be read (here a directory) must not crash the preview; it shows findings, keeps the last good render and recovers.
  it("survives prototype.tsx becoming a directory, then recovers", async () => {
    const { preview, page, source } = await open();
    await driver.replaceWithDirectory(preview.id, "prototype.tsx");
    await driver.waitFor(page, host.region("Check findings"));
    expect(await driver.read(page, host.region("Check findings"), "text")).toContain("cannot be read");
    expect(await driver.count(page, app.heading("Acme contacts"))).toBe(1);
    await driver.removeFile(preview.id, "prototype.tsx");
    await driver.writeFile(preview.id, "prototype.tsx", source.replace("`${company} contacts`", "`${company} people`"));
    await driver.waitFor(page, host.region("Check findings"), "hidden");
    await driver.waitFor(page, app.heading("Acme people"));
  });
});
