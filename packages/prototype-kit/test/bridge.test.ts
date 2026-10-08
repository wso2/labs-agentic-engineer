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
