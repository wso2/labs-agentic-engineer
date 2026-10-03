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
 * The frame bridge: the only channel between a host page and a running
 * prototype. The prototype runs in an `<iframe sandbox="allow-scripts">`
 * without `allow-same-origin` — an opaque origin with no access to the host's
 * cookies, storage or DOM, under a CSP that allows no network — and the two
 * sides exchange these messages and nothing else.
 *
 * Every message is parsed on receipt: a side acts only on a message of this
 * shape, from the window it expects (the receiver checks `event.source`).
 * React-free.
 */

import { isDataSnapshot, type DataSnapshot } from "../data.js";
import type { PrototypeManifest } from "../manifest/types.js";

export type FrameMode = "preview" | "annotate";

/** The host's resolved colour scheme, which the theme draws the prototype in; absent, the frame follows the system. */
export type FrameColorScheme = "light" | "dark";

/** The review's view as the frame draws it. */
export interface FrameView {
  mode: FrameMode;
  roleId: string;
  stateId: string;
  screenId: string;
  selectedKeys: string[];
  pins: Record<string, number[]>;
  colorScheme?: FrameColorScheme | undefined;
}

/** What the host sends the frame. */
export type ToFrameMessage =
  /** Run this prototype and draw `view`; start the mock data from `data` when given, else from the seed. */
  | { type: "proto:load"; source: string; manifest: PrototypeManifest; view: FrameView; data?: DataSnapshot | undefined }
  /** Draw another view of the loaded prototype. */
  | { type: "proto:view"; view: FrameView }
  /** Start the mock data from the seed again. */
  | { type: "proto:reset" };

/** An element the current screen draws, as the reviewer sees it. */
export interface FrameElement {
  key: string;
  label: string;
}

/** What the frame sends the host. */
export type FromFrameMessage =
  /** The runtime is up and waits for `load`. */
  | { type: "proto:ready" }
  /** The screen drew these elements (after every change to them). */
  | { type: "proto:rendered"; screenId: string; elements: FrameElement[] }
  /** A press in Preview asked for another screen. */
  | { type: "proto:navigate"; screenId: string }
  /** A click in Annotate selected or deselected an element. */
  | { type: "proto:toggle"; elementKey: string }
  /** Escape was pressed inside the frame. */
  | { type: "proto:escape" }
  /** The prototype failed to load or a screen failed to render. */
  | { type: "proto:error"; message: string }
  /** The mock data changed; the whole snapshot. */
  | { type: "proto:data"; data: DataSnapshot };

type Json = Record<string, unknown>;

const isObject = (v: unknown): v is Json => typeof v === "object" && v !== null && !Array.isArray(v);
const isString = (v: unknown): v is string => typeof v === "string";
const isStringArray = (v: unknown): v is string[] => Array.isArray(v) && v.every(isString);

function isView(v: unknown): v is FrameView {
  if (!isObject(v)) return false;
  const pins = v["pins"];
  const scheme = v["colorScheme"];
  return (
    (scheme === undefined || scheme === "light" || scheme === "dark") &&
    (v["mode"] === "preview" || v["mode"] === "annotate") &&
    isString(v["roleId"]) &&
    isString(v["stateId"]) &&
    isString(v["screenId"]) &&
    isStringArray(v["selectedKeys"]) &&
    isObject(pins) &&
    Object.values(pins).every((n) => Array.isArray(n) && n.every((x) => typeof x === "number"))
  );
}

/** A host message, or null for anything else. The manifest is the host's parsed one (`PrototypeFrame` takes a `PrototypeManifest`), so only its being an object is checked here. */
export function parseToFrameMessage(data: unknown): ToFrameMessage | null {
  if (!isObject(data)) return null;
  switch (data["type"]) {
    case "proto:load": {
      const snapshot = data["data"];
      if (!isString(data["source"]) || !isObject(data["manifest"]) || !isView(data["view"])) return null;
      if (snapshot !== undefined && !isObject(snapshot)) return null;
      return { type: "proto:load", source: data["source"], manifest: data["manifest"] as unknown as PrototypeManifest, view: data["view"], data: snapshot };
    }
    case "proto:view":
      return isView(data["view"]) ? { type: "proto:view", view: data["view"] } : null;
    case "proto:reset":
      return { type: "proto:reset" };
    default:
      return null;
  }
}

/** A frame message, or null for anything else. */
export function parseFromFrameMessage(data: unknown): FromFrameMessage | null {
  if (!isObject(data)) return null;
  switch (data["type"]) {
    case "proto:ready":
    case "proto:escape":
      return { type: data["type"] };
    case "proto:rendered": {
      const elements = data["elements"];
      if (!isString(data["screenId"]) || !Array.isArray(elements)) return null;
      if (!elements.every((e) => isObject(e) && isString(e["key"]) && isString(e["label"]))) return null;
      return {
        type: "proto:rendered",
        screenId: data["screenId"],
        elements: (elements as Json[]).map((e) => ({ key: e["key"] as string, label: e["label"] as string })),
      };
    }
    case "proto:navigate":
      return isString(data["screenId"]) ? { type: "proto:navigate", screenId: data["screenId"] } : null;
    case "proto:toggle":
      return isString(data["elementKey"]) ? { type: "proto:toggle", elementKey: data["elementKey"] } : null;
    case "proto:error":
      return isString(data["message"]) ? { type: "proto:error", message: data["message"] } : null;
    case "proto:data":
      return isDataSnapshot(data["data"]) ? { type: "proto:data", data: data["data"] } : null;
    default:
      return null;
  }
}
