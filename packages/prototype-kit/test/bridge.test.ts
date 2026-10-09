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

/** The bridge's parsers: a data snapshot from the (untrusted) frame is kept only when it is JSON data; a view to the frame names a scheme it can draw or none. */

import { describe, expect, it } from "vitest";
import { parseFromFrameMessage, parseToFrameMessage } from "../src/host/bridge.js";

const data = (snapshot: unknown) => parseFromFrameMessage({ type: "proto:data", data: snapshot });

describe("parseFromFrameMessage — proto:data", () => {
  it("accepts collections and plain JSON values", () => {
    const snapshot = { company: "Acme", count: 3, flags: { a: true, b: null }, contacts: [{ id: "c-1", name: "Ada", tags: ["x"] }], empty: [] };
    expect(data(snapshot)).toEqual({ type: "proto:data", data: snapshot });
  });

  it.each([
    ["not an object", "text"],
    ["an array", []],
    ["null", null],
    ["a non-finite number", { n: Number.NaN }],
    ["a function-free but non-plain object", { d: new Date(0) }],
    ["undefined inside a record", { contacts: [{ id: "c-1", x: undefined }] }],
    ["a bigint", { n: 1n }],
  ])("ignores a snapshot that is %s", (_name, snapshot) => {
    expect(data(snapshot)).toBeNull();
  });

  it("ignores a cyclic snapshot", () => {
    const cyclic: Record<string, unknown> = {};
    cyclic["self"] = cyclic;
    expect(data(cyclic)).toBeNull();
  });

  it("ignores a branching cycle quickly", () => {
    const a: Record<string, unknown> = {};
    a["x"] = a;
    a["y"] = a;
    const started = Date.now();
    expect(data(a)).toBeNull();
    expect(Date.now() - started).toBeLessThan(500);
  });

  it("ignores a deep shared-reference DAG quickly (2^40 paths)", () => {
    let node: Record<string, unknown> = { leaf: 1 };
    for (let i = 0; i < 40; i++) node = { l: node, r: node };
    const started = Date.now();
    expect(data({ dag: node })).toBeNull();
    expect(Date.now() - started).toBeLessThan(500);
  });

  it("accepts a value that is shared but not cyclic and small", () => {
    const shared = { id: "s" };
    expect(data({ a: [shared], b: [shared] })).not.toBeNull();
  });
});

describe("parseToFrameMessage — the view's colour scheme", () => {
  const view = (extra: object) => ({ mode: "preview", roleId: "r", stateId: "s", screenId: "x", selectedKeys: [], pins: {}, ...extra });
  const parsed = (extra: object) => parseToFrameMessage({ type: "proto:view", view: view(extra) });

  it.each([["light"], ["dark"]])("carries the host's %s scheme to the frame", (scheme) => {
    expect(parsed({ colorScheme: scheme })).toEqual({ type: "proto:view", view: view({ colorScheme: scheme }) });
  });

  it("leaves the scheme to the frame when the host names none", () => {
    expect(parsed({})).toEqual({ type: "proto:view", view: view({}) });
  });

  it.each([["system"], ["sepia"], [1]])("ignores a view whose scheme is %s", (scheme) => {
    expect(parsed({ colorScheme: scheme })).toBeNull();
  });
});

describe("parseFromFrameMessage — where elements are (frame viewport coordinates)", () => {
  const box = { x: 12, y: 40.5, width: 120, height: 32 };

  it("reads a toggle from a frame on the older protocol: the key alone", () => {
    expect(parseFromFrameMessage({ type: "proto:toggle", elementKey: "btn.new" })).toEqual({ type: "proto:toggle", elementKey: "btn.new" });
  });

  it("carries the toggled element's box, and whether the click held Shift to add to the selection", () => {
    expect(parseFromFrameMessage({ type: "proto:toggle", elementKey: "btn.new", box, additive: true })).toEqual({
      type: "proto:toggle",
      elementKey: "btn.new",
      box,
      additive: true,
    });
  });

  it("carries the boxes of the selected and pinned elements", () => {
    const boxes = { "btn.new": box, "heading.contacts": { x: 0, y: 0, width: 0, height: 0 } };
    expect(parseFromFrameMessage({ type: "proto:geometry", boxes })).toEqual({ type: "proto:geometry", boxes });
  });

  it.each([
    ["not an object", "12,40"],
    ["missing a side", { x: 1, y: 2, width: 3 }],
    ["a string coordinate", { ...box, x: "12" }],
    ["a non-finite coordinate", { ...box, y: Number.POSITIVE_INFINITY }],
    ["NaN", { ...box, width: Number.NaN }],
    ["a negative size", { ...box, height: -1 }],
  ])("ignores a toggle whose box is %s", (_name, bad) => {
    expect(parseFromFrameMessage({ type: "proto:toggle", elementKey: "btn.new", box: bad })).toBeNull();
  });

  it("ignores a toggle whose additive flag is not a boolean", () => {
    expect(parseFromFrameMessage({ type: "proto:toggle", elementKey: "btn.new", additive: "yes" })).toBeNull();
  });

  it.each([
    ["not an object", [box]],
    ["holding a malformed box", { "btn.new": box, "btn.old": { ...box, x: Number.NaN } }],
  ])("ignores geometry whose boxes are %s", (_name, boxes) => {
    expect(parseFromFrameMessage({ type: "proto:geometry", boxes })).toBeNull();
  });
});

describe("parseFromFrameMessage — a pin was clicked", () => {
  const box = { x: 12, y: 40, width: 120, height: 32 };

  it("names the element, the queued comments the pin numbers, and where the element is", () => {
    expect(parseFromFrameMessage({ type: "proto:pin", key: "btn.new", requests: [2], box })).toEqual({ type: "proto:pin", key: "btn.new", requests: [2], box });
  });

  it("names no comment for an element's draft pin", () => {
    expect(parseFromFrameMessage({ type: "proto:pin", key: "btn.new", requests: [], box })).toEqual({ type: "proto:pin", key: "btn.new", requests: [], box });
  });

  it.each([
    ["no key", { requests: [1], box }],
    ["no comment numbers", { key: "btn.new", box }],
    ["a comment number that is not a positive whole number", { key: "btn.new", requests: [0], box }],
    ["a fractional comment number", { key: "btn.new", requests: [1.5], box }],
    ["no box", { key: "btn.new", requests: [1] }],
    ["a malformed box", { key: "btn.new", requests: [1], box: { ...box, x: Number.NaN } }],
  ])("ignores a pin message with %s", (_name, fields) => {
    expect(parseFromFrameMessage({ type: "proto:pin", ...fields })).toBeNull();
  });
});

describe("parseToFrameMessage — draft pins and focus", () => {
  const view = (extra: object) => ({ mode: "annotate", roleId: "r", stateId: "s", screenId: "x", selectedKeys: [], pins: { a: [1] }, ...extra });

  it("carries the elements that hold a draft, drawn as hollow pins", () => {
    expect(parseToFrameMessage({ type: "proto:view", view: view({ drafts: ["b"] }) })).toEqual({ type: "proto:view", view: view({ drafts: ["b"] }) });
  });

  it("reads a view from a host on the older protocol: no drafts", () => {
    expect(parseToFrameMessage({ type: "proto:view", view: view({}) })).toEqual({ type: "proto:view", view: view({}) });
  });

  it("ignores a view whose drafts are not element ids", () => {
    expect(parseToFrameMessage({ type: "proto:view", view: view({ drafts: "b" }) })).toBeNull();
    expect(parseToFrameMessage({ type: "proto:view", view: view({ drafts: [1] }) })).toBeNull();
  });

  it("asks for focus back on an element, or on the pin that opened its comment", () => {
    expect(parseToFrameMessage({ type: "proto:focus", key: "a" })).toEqual({ type: "proto:focus", key: "a" });
    expect(parseToFrameMessage({ type: "proto:focus", key: "a", requests: [1] })).toEqual({ type: "proto:focus", key: "a", requests: [1] });
    expect(parseToFrameMessage({ type: "proto:focus", key: "a", requests: [] })).toEqual({ type: "proto:focus", key: "a", requests: [] });
  });

  it.each([
    ["no key", {}],
    ["malformed comment numbers", { key: "a", requests: ["1"] }],
  ])("ignores a focus message with %s", (_name, fields) => {
    expect(parseToFrameMessage({ type: "proto:focus", ...fields })).toBeNull();
  });
});

describe("parseFromFrameMessage — a click on empty space in Annotate (a whole-screen comment there)", () => {
  const point = { x: 40, y: 120.5 };
  const at = { x: 40, y: 620.5 };

  it("names where the click was, in the frame's viewport and in its scrolled document", () => {
    expect(parseFromFrameMessage({ type: "proto:screen-click", point, at })).toEqual({ type: "proto:screen-click", point, at });
  });

  it.each([
    ["no point", { at }],
    ["no document point", { point }],
    ["a point that is not an object", { point: "40,120", at }],
    ["a string coordinate", { point: { x: "40", y: 1 }, at }],
    ["a non-finite coordinate", { point, at: { x: 1, y: Number.POSITIVE_INFINITY } }],
  ])("ignores a screen click with %s", (_name, fields) => {
    expect(parseFromFrameMessage({ type: "proto:screen-click", ...fields })).toBeNull();
  });

  it("copies the points without whatever else the frame put on them", () => {
    expect(parseFromFrameMessage({ type: "proto:screen-click", point: { ...point, extra: 1 }, at })).toEqual({ type: "proto:screen-click", point, at });
  });
});

describe("parseFromFrameMessage — a whole-screen comment's pin was clicked", () => {
  const point = { x: 40, y: 20 };
  const at = { x: 40, y: 520 };

  it("names the queued comment the pin numbers (none: the screen's draft), and where it is", () => {
    expect(parseFromFrameMessage({ type: "proto:screen-pin", requests: [3], point, at })).toEqual({ type: "proto:screen-pin", requests: [3], point, at });
    expect(parseFromFrameMessage({ type: "proto:screen-pin", requests: [], point, at })).toEqual({ type: "proto:screen-pin", requests: [], point, at });
  });

  it.each([
    ["no comment numbers", { point, at }],
    ["a comment number that is not a positive whole number", { requests: [0], point, at }],
    ["no point", { requests: [1], at }],
    ["a malformed document point", { requests: [1], point, at: { x: Number.NaN, y: 0 } }],
  ])("ignores a screen pin message with %s", (_name, fields) => {
    expect(parseFromFrameMessage({ type: "proto:screen-pin", ...fields })).toBeNull();
  });
});

describe("parseFromFrameMessage — how far the frame's document is scrolled", () => {
  const box = { x: 1, y: 2, width: 3, height: 4 };

  it("carries the scroll with the boxes, and reads geometry from a frame on the older protocol without it", () => {
    expect(parseFromFrameMessage({ type: "proto:geometry", boxes: { a: box }, scroll: { x: 0, y: 500 } })).toEqual({ type: "proto:geometry", boxes: { a: box }, scroll: { x: 0, y: 500 } });
    expect(parseFromFrameMessage({ type: "proto:geometry", boxes: { a: box } })).toEqual({ type: "proto:geometry", boxes: { a: box } });
  });

  it("ignores geometry whose scroll is malformed", () => {
    expect(parseFromFrameMessage({ type: "proto:geometry", boxes: {}, scroll: { x: 0 } })).toBeNull();
  });
});

describe("parseToFrameMessage — whole-screen comments' pins", () => {
  const view = (extra: object) => ({ mode: "annotate", roleId: "r", stateId: "s", screenId: "x", selectedKeys: [], pins: {}, ...extra });

  it("carries the pins at the spots on the screen, numbered or hollow (a draft, or the comment being written)", () => {
    const screenPins = [{ at: { x: 10, y: 900 }, number: 2 }, { at: { x: 300, y: 40 } }];
    expect(parseToFrameMessage({ type: "proto:view", view: view({ screenPins }) })).toEqual({ type: "proto:view", view: view({ screenPins }) });
  });

  it.each([
    ["not a list", { at: { x: 1, y: 1 } }],
    ["a pin without a spot", [{ number: 1 }]],
    ["a malformed spot", [{ at: { x: "1", y: 1 } }]],
    ["a number that is not a positive whole number", [{ at: { x: 1, y: 1 }, number: 0 }]],
  ])("ignores a view whose screen pins are %s", (_name, screenPins) => {
    expect(parseToFrameMessage({ type: "proto:view", view: view({ screenPins }) })).toBeNull();
  });

  it("asks for focus back on a whole-screen comment's pin, or on the screen's hollow one", () => {
    expect(parseToFrameMessage({ type: "proto:focus-screen-pin", requests: [2] })).toEqual({ type: "proto:focus-screen-pin", requests: [2] });
    expect(parseToFrameMessage({ type: "proto:focus-screen-pin", requests: [] })).toEqual({ type: "proto:focus-screen-pin", requests: [] });
    expect(parseToFrameMessage({ type: "proto:focus-screen-pin" })).toBeNull();
  });
});

describe("the prototype's version, from load to the frame's report of what it drew", () => {
  const view = { mode: "preview", roleId: "r", stateId: "s", screenId: "x", selectedKeys: [], pins: {} };
  const load = { type: "proto:load", source: "export default 1", manifest: {}, view };
  const elements = [{ key: "btn.ok", label: "OK" }];

  it("carries the version the host loads, and reads a load from a host on the older protocol without it", () => {
    expect(parseToFrameMessage({ ...load, version: "v2" })).toMatchObject({ type: "proto:load", version: "v2" });
    expect(parseToFrameMessage(load)).not.toHaveProperty("version");
  });

  it("ignores a load whose version is not a string", () => {
    expect(parseToFrameMessage({ ...load, version: 2 })).toBeNull();
  });

  it("carries the version a report was drawn for, and reads a report from a frame on the older protocol without it", () => {
    expect(parseFromFrameMessage({ type: "proto:rendered", version: "v2", screenId: "x", elements })).toEqual({ type: "proto:rendered", version: "v2", screenId: "x", elements });
    expect(parseFromFrameMessage({ type: "proto:rendered", screenId: "x", elements })).toEqual({ type: "proto:rendered", screenId: "x", elements });
  });

  it("ignores a report whose version is not a string", () => {
    expect(parseFromFrameMessage({ type: "proto:rendered", version: 2, screenId: "x", elements })).toBeNull();
  });
});
