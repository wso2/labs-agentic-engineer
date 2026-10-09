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

/** The browser tests' typed view of the node-side commands, plus target shorthands. */

import { commands } from "vitest/browser";
import type { Action, Box, Modifier, Preview, Reading, Target } from "./protocol.js";

type Call = (...args: unknown[]) => Promise<unknown>;
const call = (name: string): Call => (commands as unknown as Record<string, Call>)[name]!;

export const driver = {
  startPreview: (fixture: string, flags: string[] = []) => call("startPreview")(fixture, flags) as Promise<Preview>,
  startPreviewOnFrameRuntime: (fixture: string, frameRuntime: string) => call("startPreviewOnFrameRuntime")(fixture, frameRuntime) as Promise<Preview>,
  stopPreview: (id: string) => call("stopPreview")(id) as Promise<void>,
  openPage: (url: string) => call("openPage")(url) as Promise<string>,
  reloadPage: (id: string) => call("reloadPage")(id) as Promise<void>,
  closePage: (id: string) => call("closePage")(id) as Promise<void>,
  act: (page: string, target: Target, action: Action) => call("act")(page, target, action) as Promise<void>,
  click: (page: string, target: Target) => call("act")(page, target, { type: "click" }) as Promise<void>,
  /** A click holding modifiers (Shift adds to the comment's elements). */
  clickWith: (page: string, target: Target, ...modifiers: Modifier[]) => call("act")(page, target, { type: "click", modifiers }) as Promise<void>,
  fill: (page: string, target: Target, value: string) => call("act")(page, target, { type: "fill", value }) as Promise<void>,
  press: (page: string, target: Target, key: string) => call("act")(page, target, { type: "press", key }) as Promise<void>,
  select: (page: string, target: Target, label: string) => call("act")(page, target, { type: "select", label }) as Promise<void>,
  read: (page: string, target: Target, reading: Reading) => call("read")(page, target, reading) as Promise<string | number | null>,
  box: (page: string, target: Target) => call("box")(page, target) as Promise<Box>,
  /** A pointer click at (`x`, `y`) of the app frame's viewport (empty space, in Comment mode); resolves to that spot in the page's viewport. */
  clickAppAt: (page: string, x: number, y: number) => call("clickAppAt")(page, x, y) as Promise<{ x: number; y: number }>,
  /** A key on the page, to whatever has focus. */
  pressKey: (page: string, key: string) => call("pressKey")(page, key) as Promise<void>,
  resize: (page: string, width: number, height: number) => call("resizePage")(page, width, height) as Promise<void>,
  count: (page: string, target: Target) => call("read")(page, target, "count") as Promise<number>,
  waitFor: (page: string, target: Target, state: "visible" | "hidden" = "visible") => call("waitFor")(page, target, state) as Promise<void>,
  evalInApp: (page: string, expression: string) => call("evalInApp")(page, expression) as Promise<string>,
  /** The cursor the app frame shows over the middle of `target`, or (null) over empty space at the bottom of the screen. */
  cursorAt: (page: string, target: Target | null) => call("cursorAt")(page, target) as Promise<string>,
  /** With the pointer resting on `target`, the outline colour of each app element in `keys`. */
  outlinesOnHover: (page: string, target: Target, keys: string[]) => call("outlinesOnHover")(page, target, keys) as Promise<string[]>,
  /** Waits until the app frame draws in `mode`: a host's mode switch reaches the frame by message, after the click. */
  frameMode: (page: string, mode: "preview" | "annotate") =>
    call("evalInApp")(
      page,
      `new Promise((resolve) => { const check = () => (document.querySelector('.proto-scene[data-proto-mode="${mode}"]') ? resolve(true) : setTimeout(check, 20)); check(); })`,
    ) as Promise<string>,
  /** The message types a fresh frame posts when it is sent a view before any app. */
  viewBeforeLoad: (page: string) => call("viewBeforeLoad")(page) as Promise<string>,
  requests: (page: string) => call("requests")(page) as Promise<string[]>,
  setStorage: (page: string, key: string, value: string) => call("setStorage")(page, key, value) as Promise<void>,
  readFile: (preview: string, path: string) => call("readFile")(preview, path) as Promise<string | null>,
  writeFile: (preview: string, path: string, content: string) => call("writeFile")(preview, path, content) as Promise<void>,
  removeFile: (preview: string, path: string) => call("removeFile")(preview, path) as Promise<void>,
  replaceWithDirectory: (preview: string, path: string) => call("replaceWithDirectory")(preview, path) as Promise<void>,
  revisionHash: (preview: string) => call("revisionHash")(preview) as Promise<string>,
  exportFixture: (fixture: string) => call("exportFixture")(fixture) as Promise<string>,
};

export const app = {
  heading: (name: string): Target => ({ where: "app", role: "heading", name }),
  button: (name: string): Target => ({ where: "app", role: "button", name }),
  tab: (name: string): Target => ({ where: "app", role: "tab", name }),
  dialog: (name: string): Target => ({ where: "app", role: "dialog", name }),
  row: (name: string): Target => ({ where: "app", role: "row", name, partial: true }),
  field: (label: string): Target => ({ where: "app", label }),
  text: (text: string): Target => ({ where: "app", text }),
  element: (elementId: string): Target => ({ where: "app", elementId }),
};

export const host = {
  picker: (label: string): Target => ({ where: "host", role: "combobox", name: label }),
  button: (name: string): Target => ({ where: "host", role: "button", name }),
  region: (name: string): Target => ({ where: "host", role: "region", name }),
  /** The floating dock at the bottom of the review, which holds every review control. */
  dock: (): Target => ({ where: "host", role: "region", name: "Review controls" }),
  /** The browser window's address bar: the screen the review shows (`prototype://<screen>`). */
  address: (): Target => ({ where: "host", label: "Address" }),
  /** A dialog by its name (a comment bubble: `Comment on <elements>`, `Comment <n>`); `partial` matches a substring. */
  dialog: (name: string, partial = false): Target => ({ where: "host", role: "dialog", name, partial }),
  status: (): Target => ({ where: "host", role: "status" }),
  alert: (): Target => ({ where: "host", role: "alert" }),
  field: (label: string): Target => ({ where: "host", label }),
  text: (text: string, partial = false): Target => ({ where: "host", text, partial }),
};
