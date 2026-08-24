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
import type { PrototypeModel } from "@aep/excalidraw-dsl";
import { resolveFlow, flowEntryScreen, pickerScreens } from "../src/flowState.js";

function screen(name: string) {
  return { name, width: 1280, height: 800, sceneJson: "{}", hotspots: [] };
}

const MODEL: PrototypeModel = {
  screens: ["Login", "AdminQueue", "AuditDetail", "Orders"].map(screen),
  flows: [
    { name: "Admin path", screens: ["Login", "AdminQueue", "AuditDetail"] },
    { name: "Customer path", screens: ["Login", "Orders"] },
  ],
};

const NO_FLOWS: PrototypeModel = { screens: [screen("Login"), screen("Orders")], flows: [] };

test("an absent request resolves to the first declared flow", () => {
  assert.equal(resolveFlow(MODEL, undefined), "Admin path");
});

test("a known request resolves to itself", () => {
  assert.equal(resolveFlow(MODEL, "Customer path"), "Customer path");
});

test("an unknown request falls back to the first declared flow", () => {
  assert.equal(resolveFlow(MODEL, "Ghost path"), "Admin path");
});

test("a model with no flows resolves to null — the picker is hidden", () => {
  assert.equal(resolveFlow(NO_FLOWS, "Admin path"), null);
});

test("the entry screen is the flow's first listed screen", () => {
  assert.equal(flowEntryScreen(MODEL, "Customer path"), "Login");
  assert.equal(flowEntryScreen(MODEL, "Admin path"), "Login");
});

test("with no flow selected the entry screen is the model's first screen", () => {
  assert.equal(flowEntryScreen(NO_FLOWS, null), "Login");
});

test("the picker lists the selected flow's screens in flow order", () => {
  assert.deepEqual(pickerScreens(MODEL, "Admin path", "AdminQueue"), [
    "Login",
    "AdminQueue",
    "AuditDetail",
  ]);
});

test("a screen reached across flows is appended so the picker never holds a value it lacks", () => {
  assert.deepEqual(pickerScreens(MODEL, "Admin path", "Orders"), [
    "Login",
    "AdminQueue",
    "AuditDetail",
    "Orders",
  ]);
});

test("with no flow selected the picker lists every screen", () => {
  assert.deepEqual(pickerScreens(NO_FLOWS, null, "Login"), ["Login", "Orders"]);
});
