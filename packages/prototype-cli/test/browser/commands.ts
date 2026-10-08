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
 * Node-side commands for the browser lane. They own the processes and pages:
 * a spawned `prototype preview` over a copy of a fixture, and Playwright pages
 * (in the test browser's own context) that open it or an exported file. A
 * target in the app is found through the sandboxed frame, exactly as a person
 * clicks it.
 */

import { createHash } from "node:crypto";
import { copyFileSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import type { FrameLocator, Locator, Page } from "playwright";
import type { BrowserCommand } from "vitest/node";
import { PACKAGE_ROOT, copyFixture, runCli, startPreview as spawnPreview, tempDir, type PreviewProcess } from "../harness.js";
import type { Action, Preview, Reading, Target } from "./protocol.js";

const previews = new Map<string, PreviewProcess>();
const pages = new Map<string, { page: Page; requests: string[] }>();
let next = 1;

const APP_FRAME = 'iframe[title$="prototype app"]';

function preview(id: string): PreviewProcess {
  const p = previews.get(id);
  if (!p) throw new Error(`no preview ${id}`);
  return p;
}

function page(id: string): Page {
  const p = pages.get(id);
  if (!p) throw new Error(`no page ${id}`);
  return p.page;
}

function locate(pageId: string, t: Target): Locator {
  const scope: Page | FrameLocator = t.where === "host" ? page(pageId) : page(pageId).frameLocator(APP_FRAME);
  const exact = !t.partial;
  if (t.elementId !== undefined) return scope.locator(`[data-proto-key="${t.elementId}"]`);
  if (t.role !== undefined) return scope.getByRole(t.role as Parameters<Page["getByRole"]>[0], t.name === undefined ? {} : { name: t.name, exact });
  if (t.label !== undefined) return scope.getByLabel(t.label, { exact: true });
  if (t.text !== undefined) return scope.getByText(t.text, { exact });
  throw new Error(`target has nothing to find it by: ${JSON.stringify(t)}`);
}

const startPreview: BrowserCommand<[fixture: string, flags?: string[]]> = async (_ctx, fixture, flags = []) => {
  const p = await spawnPreview(copyFixture(fixture.includes("/") ? fixture : `valid/${fixture}`), flags);
  const id = String(next++);
  previews.set(id, p);
  return { id, url: p.url } satisfies Preview;
};

/**
 * Previews a fixture on a theme whose frame runtime is `frameRuntime` (and
 * whose check runtime is the default theme's): a runtime that fails as it
 * loads, as a broken theme build would.
 */
const startPreviewOnFrameRuntime: BrowserCommand<[fixture: string, frameRuntime: string]> = async (_ctx, fixture, frameRuntime) => {
  const dir = copyFixture(`valid/${fixture}`);
  const themeDir = join(dir, "node_modules", "broken-theme");
  mkdirSync(themeDir, { recursive: true });
  const exports = { "./frame-runtime.js": "./frame-runtime.js", "./check-runtime.js": "./check-runtime.js" };
  writeFileSync(join(themeDir, "package.json"), JSON.stringify({ name: "broken-theme", version: "0.0.0", exports }));
  writeFileSync(join(themeDir, "frame-runtime.js"), frameRuntime);
  copyFileSync(createRequire(join(PACKAGE_ROOT, "x.js")).resolve("@wso2/prototype-theme-default/check-runtime.js"), join(themeDir, "check-runtime.js"));
  const p = await spawnPreview(dir, ["--theme", "broken-theme"]);
  const id = String(next++);
  previews.set(id, p);
  return { id, url: p.url } satisfies Preview;
};

const stopPreview: BrowserCommand<[id: string]> = async (_ctx, id) => {
  await preview(id).stop();
  rmSync(preview(id).dir, { recursive: true, force: true });
  previews.delete(id);
};

const openPage: BrowserCommand<[url: string]> = async (ctx, url) => {
  const p = await ctx.context.newPage();
  const requests: string[] = [];
  p.on("request", (r) => requests.push(r.url()));
  await p.goto(url);
  const id = String(next++);
  pages.set(id, { page: p, requests });
  return id;
};

const reloadPage: BrowserCommand<[id: string]> = async (_ctx, id) => {
  await page(id).reload();
};

const closePage: BrowserCommand<[id: string]> = async (_ctx, id) => {
  await page(id).close();
  pages.delete(id);
};

const act: BrowserCommand<[pageId: string, target: Target, action: Action]> = async (_ctx, pageId, target, action) => {
  const l = locate(pageId, target);
  if (action.type === "click") await l.click();
  else if (action.type === "fill") await l.fill(action.value);
  else if (action.type === "press") await l.press(action.key);
  else await l.selectOption({ label: action.label });
};

const read: BrowserCommand<[pageId: string, target: Target, reading: Reading]> = async (_ctx, pageId, target, reading) => {
  const l = locate(pageId, target);
  if (reading === "count") return l.count();
  if (reading === "text") return l.innerText();
  if (reading === "value") return l.inputValue();
  if (reading === "maxlength") return l.getAttribute("maxlength");
  if (reading === "disabled") return String(await l.isDisabled());
  return l.getAttribute("aria-pressed");
};

const waitFor: BrowserCommand<[pageId: string, target: Target, state?: "visible" | "hidden"]> = async (_ctx, pageId, target, state = "visible") => {
  await locate(pageId, target).first().waitFor({ state, timeout: 15_000 });
};

/** Evaluates an expression inside the sandboxed app frame and returns its result as a string. */
const evalInApp: BrowserCommand<[pageId: string, expression: string]> = async (_ctx, pageId, expression) => {
  const handle = await page(pageId).locator(APP_FRAME).elementHandle();
  const frame = await handle?.contentFrame();
  if (!frame) throw new Error("the app frame is not there");
  return String(await frame.evaluate(expression));
};

/**
 * Mounts a second frame from the host's own document and sends it a view before any app: returns the
 * message types it posted back, so a test can tell a frame that stayed quiet from one that reported a draw.
 */
const viewBeforeLoad: BrowserCommand<[pageId: string]> = async (_ctx, pageId) => {
  const types = await page(pageId).evaluate(async (selector) => {
    const original = document.querySelector<HTMLIFrameElement>(selector);
    if (!original) throw new Error("the app frame is not there");
    const probe = document.createElement("iframe");
    probe.setAttribute("sandbox", "allow-scripts");
    probe.srcdoc = original.srcdoc;
    // Off-screen, not display:none: a hidden frame never runs animation frames, which is when a draw is reported.
    probe.style.cssText = "position:fixed;left:-9999px;width:320px;height:240px;border:0";
    const seen: string[] = [];
    const ready = new Promise<void>((resolve) => {
      window.addEventListener("message", (e) => {
        if (e.source !== probe.contentWindow) return;
        const type = (e.data as { type?: string } | null)?.type ?? "";
        seen.push(type);
        if (type === "proto:ready") {
          const view = { mode: "preview", roleId: "role.none", stateId: "state.none", screenId: "screen.none", selectedKeys: [], pins: {} };
          probe.contentWindow?.postMessage({ type: "proto:view", view }, "*");
          resolve();
        }
      });
    });
    document.body.append(probe);
    await ready;
    await new Promise((r) => setTimeout(r, 500));
    probe.remove();
    return seen;
  }, APP_FRAME);
  return types.join(",");
};

const requests: BrowserCommand<[pageId: string]> = (_ctx, pageId) => {
  const p = pages.get(pageId);
  if (!p) throw new Error(`no page ${pageId}`);
  return [...p.requests];
};

const setStorage: BrowserCommand<[pageId: string, key: string, value: string]> = async (_ctx, pageId, key, value) => {
  await page(pageId).evaluate(([k, v]) => localStorage.setItem(k, v), [key, value] as const);
};

const readFile: BrowserCommand<[previewId: string, path: string]> = (_ctx, previewId, path) => {
  try {
    return readFileSync(join(preview(previewId).dir, path), "utf8");
  } catch {
    return null;
  }
};

const writeFile: BrowserCommand<[previewId: string, path: string, content: string]> = (_ctx, previewId, path, content) => {
  writeFileSync(join(preview(previewId).dir, path), content, "utf8");
};

const removeFile: BrowserCommand<[previewId: string, path: string]> = (_ctx, previewId, path) => {
  rmSync(join(preview(previewId).dir, path), { force: true, recursive: true });
};

const replaceWithDirectory: BrowserCommand<[previewId: string, path: string]> = (_ctx, previewId, path) => {
  const target = join(preview(previewId).dir, path);
  rmSync(target, { force: true, recursive: true });
  mkdirSync(target);
};

/** The revision hash the preview keys persisted data and feedback by: SHA-256 of prototype.json, NUL, prototype.tsx. */
const revisionHash: BrowserCommand<[previewId: string]> = (_ctx, previewId) => {
  const dir = preview(previewId).dir;
  return createHash("sha256").update(readFileSync(join(dir, "prototype.json"))).update("\u0000").update(readFileSync(join(dir, "prototype.tsx"))).digest("hex");
};

/** Exports a fixture with the CLI and returns the file's URL. */
const exportFixture: BrowserCommand<[fixture: string]> = (_ctx, fixture) => {
  const out = join(tempDir(), `${fixture}.html`);
  const run = runCli(["export", copyFixture(fixture.includes("/") ? fixture : `valid/${fixture}`), "-o", out]);
  if (run.status !== 0) throw new Error(`export failed: ${run.stderr}`);
  return pathToFileURL(out).href;
};

export const commands = {
  startPreview,
  startPreviewOnFrameRuntime,
  stopPreview,
  openPage,
  reloadPage,
  closePage,
  act,
  read,
  waitFor,
  evalInApp,
  viewBeforeLoad,
  requests,
  setStorage,
  readFile,
  writeFile,
  removeFile,
  replaceWithDirectory,
  revisionHash,
  exportFixture,
};
