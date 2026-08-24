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


import { test } from "node:test";
import assert from "node:assert/strict";
import {
  elementsOfScreens,
  firstScreenName,
  openingFocusElements,
  screenAtViewportCenter,
  screensToFollow,
} from "../src/screenFocus.js";

type El = { id: string; y: number; version: number; customData?: { screen: string } };
const el = (id: string, screen: string, y: number, version = 1): El => ({
  id,
  y,
  version,
  customData: { screen },
});

const SCENE: El[] = [
  el("h1", "Home", 0),
  el("h2", "Home", 40),
  el("l1", "Login", 900),
  el("l2", "Login", 940),
  el("d1", "Detail", 1800),
];

test("elementsOfScreens returns only the named screens' elements", () => {
  assert.deepEqual(
    elementsOfScreens(SCENE, ["Login"]).map((e) => e.id),
    ["l1", "l2"],
  );
  assert.deepEqual(
    elementsOfScreens(SCENE, ["Home", "Detail"]).map((e) => e.id),
    ["h1", "h2", "d1"],
  );
});

test("elementsOfScreens ignores elements with no screen tag", () => {
  const scene = [...SCENE, { id: "x", y: 5, version: 1 } as El];
  assert.deepEqual(elementsOfScreens(scene, ["Home"]).map((e) => e.id), ["h1", "h2"]);
});

test("firstScreenName is the screen whose topmost element is highest on the canvas", () => {
  assert.equal(firstScreenName(SCENE), "Home");
  assert.equal(firstScreenName([...SCENE].reverse()), "Home");
});

test("firstScreenName is null for an empty or untagged scene", () => {
  assert.equal(firstScreenName([]), null);
  assert.equal(firstScreenName([{ id: "x", y: 0, version: 1 } as El]), null);
});

test("the opening focus is the first screen plus a peek band of the second", () => {
  const scene = [
    { id: "a1", y: 0, height: 32, version: 1, customData: { screen: "Home" } },
    { id: "a2", y: 32, height: 800, version: 1, customData: { screen: "Home" } },
    // second screen: title at 952, frame 984..1784, a body element deep inside
    { id: "b1", y: 952, height: 32, version: 1, customData: { screen: "Login" } },
    { id: "b2", y: 984, height: 800, version: 1, customData: { screen: "Login" } },
    { id: "b3", y: 1500, height: 40, version: 1, customData: { screen: "Login" } },
    { id: "c1", y: 1904, height: 32, version: 1, customData: { screen: "Detail" } },
  ];
  const ids = openingFocusElements(scene).map((e) => e.id);
  assert.ok(ids.includes("a1") && ids.includes("a2"), "whole first screen");
  assert.ok(ids.includes("b1"), "second screen's title is in the peek band");
  assert.ok(!ids.includes("b2"), "second screen's full-height frame is NOT — it would fit both screens");
  assert.ok(!ids.includes("b3"), "deep body of the second screen is not");
  assert.ok(!ids.includes("c1"), "third screen is not");
  const marker = openingFocusElements(scene).find((e) => e.id === "__peek-marker")!;
  assert.ok(marker, "a bounds marker pins the peek depth");
  assert.ok(marker.y > 952 && marker.y < 1500, "marker sits inside the second screen's top band");
});

test("the opening focus falls back to the first screen alone when there is no second", () => {
  const scene = [
    { id: "a1", y: 0, height: 32, version: 1, customData: { screen: "Home" } },
    { id: "a2", y: 32, height: 800, version: 1, customData: { screen: "Home" } },
  ];
  assert.deepEqual(openingFocusElements(scene).map((e) => e.id), ["a1", "a2"]);
});

test("the opening focus is empty for an untagged scene", () => {
  assert.deepEqual(openingFocusElements([{ id: "x", y: 0, height: 10, version: 1 }]), []);
});

// ---------- which screen is the reader looking at, and should the camera move ----------

// Three screens stacked: Home 0..800, Login 1000..1800, Detail 2000..2800.
const STACK = [
  { id: "h", x: 0, y: 0, width: 1280, height: 800, version: 1, customData: { screen: "Home" } },
  { id: "l", x: 0, y: 1000, width: 1280, height: 800, version: 1, customData: { screen: "Login" } },
  { id: "d", x: 0, y: 2000, width: 1280, height: 800, version: 1, customData: { screen: "Detail" } },
];

test("screenAtViewportCenter names the screen under the middle of the viewport", () => {
  // zoom 1, no scroll, 1280x800 viewport → centre at scene (640, 400) → Home
  assert.equal(
    screenAtViewportCenter(STACK, { scrollX: 0, scrollY: 0, zoom: { value: 1 } }, 1280, 800),
    "Home",
  );
  // scrolled so that scene y=1400 is the centre → Login
  assert.equal(
    screenAtViewportCenter(STACK, { scrollX: 0, scrollY: -1000, zoom: { value: 1 } }, 1280, 800),
    "Login",
  );
});

test("screenAtViewportCenter accounts for zoom", () => {
  // zoom 0.5: viewport 1280x800 spans 2560x1600 scene units; centre is scene (1280, 800)
  // with scroll 0 — between Home's bottom (800) and Login's top (1000) → nearest is Home.
  assert.equal(
    screenAtViewportCenter(STACK, { scrollX: 0, scrollY: 0, zoom: { value: 0.5 } }, 1280, 800),
    "Home",
  );
});

test("screenAtViewportCenter falls back to the nearest screen when the centre is in a gap", () => {
  // centre at y=1950: gap between Login (ends 1800) and Detail (starts 2000); Detail is nearer
  assert.equal(
    screenAtViewportCenter(STACK, { scrollX: 0, scrollY: -1550, zoom: { value: 1 } }, 1280, 800),
    "Detail",
  );
});

test("screenAtViewportCenter is null for an untagged scene", () => {
  assert.equal(screenAtViewportCenter([], { scrollX: 0, scrollY: 0, zoom: { value: 1 } }, 1280, 800), null);
});

test("screensToFollow holds still when the reader's screen is among the changed", () => {
  // The edit is under the reader's nose — moving would only twitch the camera.
  assert.deepEqual(screensToFollow(["Login"], "Login", null), []);
  assert.deepEqual(screensToFollow(["Home", "Login"], "Login", null), []);
});

test("screensToFollow moves to a single edit made entirely elsewhere", () => {
  assert.deepEqual(screensToFollow(["Home"], "Login", null), ["Home"]);
  assert.deepEqual(screensToFollow(["Home", "Detail"], "Login", null), ["Home", "Detail"]);
});

test("screensToFollow recognises a sweep and stops touring", () => {
  // A sweep edit (the same header on every screen) lands one flush per
  // screen: Home, then Login, then Detail. At the FIRST flush it is
  // indistinguishable from a single edit to Home, so the camera goes there.
  // But the SECOND flush changing a DIFFERENT single screen is the sweep's
  // signature — from then on the camera holds, rather than touring every
  // screen and stranding the reader on the last one.
  assert.deepEqual(screensToFollow(["Home"], "Detail", null), ["Home"]); // flush 1: looks like an edit
  assert.deepEqual(screensToFollow(["Login"], "Detail", ["Home"]), []); // flush 2: a sweep — hold
  assert.deepEqual(screensToFollow(["Detail"], "Detail", ["Login"]), []); // flush 3: on it anyway
});

test("screensToFollow does not mistake a repeated edit to one screen for a sweep", () => {
  // Several flushes all changing Home is one edit being written — keep following it.
  assert.deepEqual(screensToFollow(["Home"], "Detail", ["Home"]), ["Home"]);
});

test("screensToFollow follows everything when the reader's screen is unknown", () => {
  assert.deepEqual(screensToFollow(["Home"], null, null), ["Home"]);
});
