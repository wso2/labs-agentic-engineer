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
import { tryDslToPrototype, dslToExcalidraw } from "../src/index.js";

type El = {
  type: string; x: number; y: number; width: number; height: number;
  text?: string; link?: string | null; strokeColor?: string; backgroundColor?: string;
};

const DSL = `screen Login "Sign-in for all roles"
  input "Email"
  button "Sign in" primary -> Dashboard
screen Dashboard
  navbar "App | Home"
  button "Log out" -> login
  button "Nowhere" -> Missing
flow
  Login -> Dashboard
`;

function model(dsl: string) {
  const res = tryDslToPrototype(dsl);
  assert.ok(res.ok, `expected ok, got ${!res.ok ? res.error : ""}`);
  return res.model;
}
function elements(sceneJson: string): El[] {
  return JSON.parse(sceneJson).elements as El[];
}

test("model lists every screen with metadata", () => {
  const m = model(DSL);
  assert.equal(m.screens.length, 2);
  assert.equal(m.screens[0]!.name, "Login");
  assert.equal(m.screens[0]!.description, "Sign-in for all roles");
  assert.equal(m.screens[0]!.width, 1280);
  assert.equal(m.screens[1]!.name, "Dashboard");
});

test("each scene is one screen with its frame at origin and no canvas decorations", () => {
  const m = model(DSL);
  const els = elements(m.screens[0]!.sceneJson);
  const frame = els.find((e) => e.type === "rectangle" && e.x === 0 && e.y === 0 && e.width === 1280);
  assert.ok(frame, "screen frame rect not at origin");
  const texts = els.filter((e) => e.type === "text").map((e) => e.text ?? "");
  assert.ok(!texts.includes("Login"), "screen title must be suppressed");
  assert.ok(!texts.some((t) => t.startsWith("Screen ")), "screen-number badge must be suppressed");
  assert.ok(!texts.some((t) => t.includes("→ Screen")), "nav markers must be suppressed");
  assert.ok(!texts.includes("Sign-in for all roles"), "description subtitle must be suppressed");
});

test("prototype scenes carry no Excalidraw links — navigation is overlay-driven, not link-driven", () => {
  const m = model(DSL);
  for (const s of m.screens) {
    const els = elements(s.sceneJson);
    assert.ok(
      els.every((e) => (e.link ?? null) === null),
      `screen ${s.name}: expected every element's link to be null`,
    );
  }
});

test("hotspot matches the navigable control's laid-out box and canonical target", () => {
  const m = model(DSL);
  const hs = m.screens[0]!.hotspots;
  assert.equal(hs.length, 1);
  assert.equal(hs[0]!.target, "Dashboard");
  const els = elements(m.screens[0]!.sceneJson);
  const btnRect = els.find((e) => e.type === "rectangle" && e.x === hs[0]!.x && e.y === hs[0]!.y)!;
  assert.equal(hs[0]!.width, btnRect.width);
  assert.equal(hs[0]!.height, btnRect.height);
});

test("target names resolve case-insensitively to the canonical screen name", () => {
  const m = model(DSL);
  const hs = m.screens[1]!.hotspots; // "Log out" -> login (lowercase)
  const toLogin = hs.find((h) => h.target === "Login");
  assert.ok(toLogin, "lowercase `-> login` should resolve to canonical Login");
});

test("a dead -> target compiles to no link and no hotspot", () => {
  const m = model(DSL);
  const els = elements(m.screens[1]!.sceneJson);
  const nowhere = els.find((e) => e.type === "text" && e.text === "Nowhere")!;
  assert.equal(nowhere.link ?? null, null);
  assert.ok(!m.screens[1]!.hotspots.some((h) => h.target === "Missing"));
});

// A `-> ThisScreen` says "go to where you already are". Agents reach for it to
// mean "this control acts in place" (an Add beside a search box that appends a
// row), so it renders a control that invites a click and then cannot change
// anything — indistinguishable from broken. The skill states the rule; the
// compiler refuses to advertise the affordance.
test("a self-targeting -> compiles to no hotspot", () => {
  const m = model(`screen LogSession
  search "Add exercise…"
  button "Add" -> LogSession
  button "Done" -> Summary
screen Summary
`);
  const logSession = m.screens[0]!;
  assert.ok(
    !logSession.hotspots.some((h) => h.target === "LogSession"),
    "a self-target must not produce a hotspot",
  );
  assert.equal(logSession.hotspots.length, 1, "the cross-screen target survives");
  assert.equal(logSession.hotspots[0]!.target, "Summary");
});

test("a self-target is dropped case-insensitively", () => {
  const m = model(`screen LogSession
  button "Add" -> logsession
screen Summary
`);
  assert.equal(m.screens[0]!.hotspots.length, 0);
});

test("prototype compile is deterministic", () => {
  const a = model(DSL).screens[0]!.sceneJson;
  const b = model(DSL).screens[0]!.sceneJson;
  assert.equal(a, b);
});

test("empty and screenless sources fail softly", () => {
  assert.equal(tryDslToPrototype("").ok, false);
  assert.equal(tryDslToPrototype("// just a comment\n").ok, false);
});

test("canvas compile is unchanged: decorations present, links null", () => {
  const els = JSON.parse(dslToExcalidraw("wireframes", DSL)).elements as El[];
  const texts = els.filter((e) => e.type === "text").map((e) => e.text ?? "");
  assert.ok(texts.some((t) => t.includes("→ Screen")), "canvas keeps nav markers");
  assert.ok(texts.includes("Login"), "canvas keeps screen titles");
  assert.ok(els.every((e) => (e.link ?? null) === null), "canvas emits no links");
});

// Chrome (navbar/sidebar) is how a real webapp reaches its top-level views, so
// each item may carry its own target. Without this the sidebar is a drawing and
// whole sections of a generated app have no way in.
const CHROME_DSL = `screen Dashboard
  sidebar "Dashboard -> Dashboard | Templates -> Templates | Progress -> Progress | Help"
  navbar "WorkoutTracker | History -> History | Missing -> Nowhere"
  heading "Today"
screen Templates
screen Progress
screen History
`;

test("a sidebar item's target becomes a full-row hotspot", () => {
  const dash = model(CHROME_DSL).screens[0]!;
  const templates = dash.hotspots.find((h) => h.target === "Templates");
  assert.ok(templates, "annotated sidebar item produced no hotspot");
  // Item index 1 → the active-pill box: x 8, y 64 + 1*40, 224x32.
  assert.deepEqual(templates, { x: 8, y: 104, width: 224, height: 32, target: "Templates" });
});

test("chrome items render their text without the arrow clause", () => {
  const els = elements(model(CHROME_DSL).screens[0]!.sceneJson);
  const texts = els.filter((e) => e.type === "text").map((e) => e.text ?? "");
  assert.ok(texts.includes("Templates"), "sidebar item should draw its label alone");
  assert.ok(texts.includes("History"), "navbar item should draw its label alone");
  assert.ok(
    !texts.some((t) => t.includes("->")),
    `no drawn text may contain a raw arrow: ${JSON.stringify(texts)}`,
  );
});

test("an unannotated chrome item claims no hotspot", () => {
  const dash = model(CHROME_DSL).screens[0]!;
  assert.ok(!dash.hotspots.some((h) => h.target === "Help"));
});

test("a chrome item pointing at its own screen claims no hotspot", () => {
  // The sidebar's "Dashboard" entry, seen from Dashboard, goes nowhere.
  const dash = model(CHROME_DSL).screens[0]!;
  assert.ok(!dash.hotspots.some((h) => h.target === "Dashboard"));
  // …but the same sidebar seen from another screen does navigate.
  const templates = model(`screen Dashboard
screen Templates
  sidebar "Dashboard -> Dashboard | Templates -> Templates"
`).screens[1]!;
  assert.ok(templates.hotspots.some((h) => h.target === "Dashboard"));
  assert.ok(!templates.hotspots.some((h) => h.target === "Templates"));
});

test("a chrome item with a dead target claims no hotspot", () => {
  const dash = model(CHROME_DSL).screens[0]!;
  assert.ok(!dash.hotspots.some((h) => h.target === "Nowhere"));
});

test("a navbar item's target becomes a hotspot on its text box", () => {
  const dash = model(CHROME_DSL).screens[0]!;
  const history = dash.hotspots.find((h) => h.target === "History");
  assert.ok(history, "annotated navbar item produced no hotspot");
  assert.equal(history.y, 19, "navbar hotspot sits on the item's text row");
  assert.equal(history.height, 18);
  assert.ok(history.width >= 40 && history.x > 0);
});

test("the navbar brand never navigates", () => {
  const m = model(`screen Dashboard
  navbar "WorkoutTracker -> Templates | History -> History"
screen Templates
screen History
`);
  assert.ok(!m.screens[0]!.hotspots.some((h) => h.target === "Templates"));
});

// Colors mirror BRAND_DARK / BRAND_TINT in src/index.ts — not exported, so
// asserted here by value.
const BRAND_DARK = "#e74420";
const BRAND_TINT = "#fff0e8";

test("sidebar active pill follows the rendered screen, not always index 0", () => {
  const dsl = `screen Dashboard
  sidebar "Dashboard -> Dashboard | Templates -> Templates | Progress -> Progress"
screen Templates
  sidebar "Dashboard -> Dashboard | Templates -> Templates | Progress -> Progress"
screen Progress
  sidebar "Dashboard -> Dashboard | Templates -> Templates | Progress -> Progress"
`;
  const m = model(dsl);
  // Order in the annotated sidebar matches screen declaration order, so the
  // active row index equals the screen's own index (0, 1, 2).
  m.screens.forEach((s, expectedActiveIndex) => {
    const els = elements(s.sceneJson);
    const pillY = 68 + expectedActiveIndex * 40 - 4; // frameY(0) + NAVBAR_H(56) + 12 + i*40 - 4
    const pill = els.find(
      (e) => e.type === "rectangle" && e.x === 8 && e.y === pillY && e.backgroundColor === BRAND_TINT,
    );
    assert.ok(pill, `screen ${s.name}: expected active pill at row ${expectedActiveIndex}`);

    const texts = els.filter((e) => e.type === "text");
    texts.forEach((t) => {
      const rowIndex = ["Dashboard", "Templates", "Progress"].indexOf(t.text ?? "");
      if (rowIndex === -1) return; // not a sidebar item text
      const expectedColor = rowIndex === expectedActiveIndex ? BRAND_DARK : "#1e1e1e";
      assert.equal(
        t.strokeColor,
        expectedColor,
        `screen ${s.name}: row "${t.text}" should be ${expectedColor}`,
      );
    });
  });
});

test("an unannotated sidebar still highlights index 0 on every screen", () => {
  const dsl = `screen Dashboard
  sidebar "Dashboard | Templates | Progress"
screen Templates
  sidebar "Dashboard | Templates | Progress"
`;
  const m = model(dsl);
  m.screens.forEach((s) => {
    const els = elements(s.sceneJson);
    const pill = els.find(
      (e) => e.type === "rectangle" && e.x === 8 && e.y === 64 && e.backgroundColor === BRAND_TINT,
    );
    assert.ok(pill, `screen ${s.name}: unannotated sidebar should default to index 0`);
    const dashboard = els.find((e) => e.type === "text" && e.text === "Dashboard")!;
    assert.equal(dashboard.strokeColor, BRAND_DARK);
  });
});

test("an item that is only an arrow draws its raw text and claims no target", () => {
  const m = model(`screen Dashboard
  sidebar "-> Templates | Home"
screen Templates
`);
  const dash = m.screens[0]!;
  assert.equal(dash.hotspots.length, 0, "an arrow-only item must not claim a hotspot");
  const els = elements(dash.sceneJson);
  const texts = els.filter((e) => e.type === "text").map((e) => e.text ?? "");
  assert.ok(texts.includes("-> Templates"), "an arrow-only item draws its raw text literally");
});

test("body hotspots still work alongside chrome ones", () => {
  const m = model(`screen Dashboard
  sidebar "Dashboard -> Dashboard | Templates -> Templates"
  button "Start" primary -> Templates
screen Templates
`);
  // One from the sidebar item, one from the button — same target, two hotspots.
  assert.equal(m.screens[0]!.hotspots.filter((h) => h.target === "Templates").length, 2);
});
