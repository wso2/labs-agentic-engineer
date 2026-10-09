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
  /** The elements that hold a comment the reviewer started but did not add (a draft), drawn as a hollow pin; none when absent. */
  drafts?: string[] | undefined;
  /** Whole-screen comments' pins, each at the spot on the screen it was made; none when absent. */
  screenPins?: FrameScreenPin[] | undefined;
  colorScheme?: FrameColorScheme | undefined;
}

/**
 * A spot in the frame, in CSS pixels: of its viewport (`clientX`/`clientY`),
 * or of its document, scrolled with it (`pageX`/`pageY`), as each use says.
 */
export interface FramePoint {
  x: number;
  y: number;
}

/**
 * A whole-screen comment's pin, at the spot of the frame's document where the
 * reviewer clicked to make it: numbered by its queued comment, or (no number)
 * hollow, for the screen's draft or the comment being written there.
 */
export interface FrameScreenPin {
  at: FramePoint;
  number?: number | undefined;
}

/** What the host sends the frame. */
export type ToFrameMessage =
  /**
   * Run this prototype and draw `view`; start the mock data from `data` when
   * given, else from the seed. `version` names the prototype (the host's
   * `PrototypeFrame` version), echoed on every `proto:rendered` of it; a host
   * on the older protocol leaves it out.
   */
  | { type: "proto:load"; source: string; manifest: PrototypeManifest; view: FrameView; data?: DataSnapshot | undefined; version?: string | undefined }
  /** Draw another view of the loaded prototype. */
  | { type: "proto:view"; view: FrameView }
  /** Start the mock data from the seed again. */
  | { type: "proto:reset" }
  /**
   * Put keyboard focus back on the element `key` (a comment bubble on it
   * closed): on its pin for `requests` when given (`[]`: its draft pin), as
   * `proto:pin` named it, else on the element.
   */
  | { type: "proto:focus"; key: string; requests?: number[] | undefined }
  /** Put keyboard focus on the whole-screen comment's pin numbered `requests[0]` (`[]`: the hollow one), as `proto:screen-pin` named it. */
  | { type: "proto:focus-screen-pin"; requests: number[] };

/** An element the current screen draws, as the reviewer sees it. */
export interface FrameElement {
  key: string;
  label: string;
}

/**
 * Where an element is drawn, in CSS pixels of the frame's own viewport (its
 * `getBoundingClientRect()`). The host adds where the frame element sits in
 * its page to place its own UI by the element (see `useFrameAnchors`).
 */
export interface FrameBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** What the frame sends the host. */
export type FromFrameMessage =
  /** The runtime is up and waits for `load`. */
  | { type: "proto:ready" }
  /**
   * The screen drew these elements (after every change to them), for the
   * prototype `version` loaded (none when its load named none, or from a
   * frame on the older protocol): a report of an earlier version can arrive
   * after the host loaded the next one.
   */
  | { type: "proto:rendered"; version?: string | undefined; screenId: string; elements: FrameElement[] }
  /** A press in Preview asked for another screen. */
  | { type: "proto:navigate"; screenId: string }
  /**
   * A click in Annotate selected or deselected an element: where it is, and
   * whether the click held Shift (add to the selection rather than start a
   * new one). A frame on the older protocol sends the key alone.
   */
  | { type: "proto:toggle"; elementKey: string; box?: FrameBox | undefined; additive?: boolean | undefined }
  /**
   * A pin was clicked (in either mode): the element it is on, the queued
   * comments' numbers it shows (none: the element's draft pin), and where
   * the element is.
   */
  | { type: "proto:pin"; key: string; requests: number[]; box: FrameBox }
  /**
   * Annotate: a click on empty space, on no element that takes a comment (a
   * whole-screen comment there): where, in the frame's viewport (`point`) and
   * in its document (`at`, where its pin is drawn).
   */
  | { type: "proto:screen-click"; point: FramePoint; at: FramePoint }
  /** A whole-screen comment's pin was clicked (in either mode): the queued comment's number it shows (none: the hollow pin), and where it is, as for a screen click. */
  | { type: "proto:screen-pin"; requests: number[]; point: FramePoint; at: FramePoint }
  /**
   * Where the selected and pinned elements are now, and how far the frame's
   * document is scrolled (`scroll`; a frame on the older protocol leaves it
   * out), re-sent after every scroll, resize and redraw that moves them.
   */
  | { type: "proto:geometry"; boxes: Record<string, FrameBox>; scroll?: FramePoint | undefined }
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
const isFiniteNumber = (v: unknown): v is number => typeof v === "number" && Number.isFinite(v);
/** A queued comment's number: 1-based, as the pins show it. */
const isCommentNumbers = (v: unknown): v is number[] => Array.isArray(v) && v.every((n) => Number.isInteger(n) && n > 0);

function isBox(v: unknown): v is FrameBox {
  if (!isObject(v)) return false;
  const { x, y, width, height } = v;
  return isFiniteNumber(x) && isFiniteNumber(y) && isFiniteNumber(width) && isFiniteNumber(height) && width >= 0 && height >= 0;
}

/** A checked box, copied without whatever else the frame put on it. */
const boxOf = ({ x, y, width, height }: FrameBox): FrameBox => ({ x, y, width, height });

const isPoint = (v: unknown): v is FramePoint => isObject(v) && isFiniteNumber(v["x"]) && isFiniteNumber(v["y"]);

/** A checked point, copied without whatever else the frame put on it. */
const pointOf = ({ x, y }: FramePoint): FramePoint => ({ x, y });

const isScreenPin = (v: unknown): v is FrameScreenPin =>
  isObject(v) && isPoint(v["at"]) && (v["number"] === undefined || isCommentNumbers([v["number"]]));

function isView(v: unknown): v is FrameView {
  if (!isObject(v)) return false;
  const pins = v["pins"];
  const scheme = v["colorScheme"];
  const drafts = v["drafts"];
  const screenPins = v["screenPins"];
  return (
    (scheme === undefined || scheme === "light" || scheme === "dark") &&
    (drafts === undefined || isStringArray(drafts)) &&
    (screenPins === undefined || (Array.isArray(screenPins) && screenPins.every(isScreenPin))) &&
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
      const version = data["version"];
      if (!isString(data["source"]) || !isObject(data["manifest"]) || !isView(data["view"])) return null;
      if ((snapshot !== undefined && !isObject(snapshot)) || (version !== undefined && !isString(version))) return null;
      return {
        type: "proto:load",
        source: data["source"],
        manifest: data["manifest"] as unknown as PrototypeManifest,
        view: data["view"],
        data: snapshot,
        ...(version !== undefined ? { version } : {}),
      };
    }
    case "proto:view":
      return isView(data["view"]) ? { type: "proto:view", view: data["view"] } : null;
    case "proto:reset":
      return { type: "proto:reset" };
    case "proto:focus": {
      const { key, requests } = data;
      if (!isString(key) || (requests !== undefined && !isCommentNumbers(requests))) return null;
      return { type: "proto:focus", key, ...(requests !== undefined ? { requests: [...requests] } : {}) };
    }
    case "proto:focus-screen-pin":
      return isCommentNumbers(data["requests"]) ? { type: "proto:focus-screen-pin", requests: [...data["requests"]] } : null;
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
      const { elements, version } = data;
      if (!isString(data["screenId"]) || !Array.isArray(elements) || (version !== undefined && !isString(version))) return null;
      if (!elements.every((e) => isObject(e) && isString(e["key"]) && isString(e["label"]))) return null;
      return {
        type: "proto:rendered",
        ...(version !== undefined ? { version } : {}),
        screenId: data["screenId"],
        elements: (elements as Json[]).map((e) => ({ key: e["key"] as string, label: e["label"] as string })),
      };
    }
    case "proto:navigate":
      return isString(data["screenId"]) ? { type: "proto:navigate", screenId: data["screenId"] } : null;
    case "proto:toggle": {
      const { elementKey, box, additive } = data;
      if (!isString(elementKey)) return null;
      if ((box !== undefined && !isBox(box)) || (additive !== undefined && typeof additive !== "boolean")) return null;
      return {
        type: "proto:toggle",
        elementKey,
        ...(box !== undefined ? { box: boxOf(box) } : {}),
        ...(additive !== undefined ? { additive } : {}),
      };
    }
    case "proto:pin": {
      const { key, requests, box } = data;
      if (!isString(key) || !isCommentNumbers(requests) || !isBox(box)) return null;
      return { type: "proto:pin", key, requests: [...requests], box: boxOf(box) };
    }
    case "proto:screen-click": {
      const { point, at } = data;
      return isPoint(point) && isPoint(at) ? { type: "proto:screen-click", point: pointOf(point), at: pointOf(at) } : null;
    }
    case "proto:screen-pin": {
      const { requests, point, at } = data;
      if (!isCommentNumbers(requests) || !isPoint(point) || !isPoint(at)) return null;
      return { type: "proto:screen-pin", requests: [...requests], point: pointOf(point), at: pointOf(at) };
    }
    case "proto:geometry": {
      const { boxes, scroll } = data;
      if (!isObject(boxes) || !Object.values(boxes).every(isBox) || (scroll !== undefined && !isPoint(scroll))) return null;
      return {
        type: "proto:geometry",
        boxes: Object.fromEntries(Object.entries(boxes as Record<string, FrameBox>).map(([key, b]) => [key, boxOf(b)])),
        ...(scroll !== undefined ? { scroll: pointOf(scroll) } : {}),
      };
    }
    case "proto:error":
      return isString(data["message"]) ? { type: "proto:error", message: data["message"] } : null;
    case "proto:data":
      return isDataSnapshot(data["data"]) ? { type: "proto:data", data: data["data"] } : null;
    default:
      return null;
  }
}
