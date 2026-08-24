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
 * The flow layout engine: the DSL carries structure, the compiler computes
 * every pixel. These tests pin the geometry contract; the no-overlap /
 * in-frame invariant is asserted through validateWireframeLayout as oracle.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import {
  compileWireframes,
  dslToExcalidraw,
  validateWireframeLayout,
  validateWireframeSyntax,
} from "../src/index.js";

type El = {
  id: string;
  type: string;
  x: number;
  y: number;
  width: number;
  height: number;
  text?: string;
  backgroundColor?: string;
};

function compile(dsl: string): El[] {
  return JSON.parse(dslToExcalidraw("wireframes", dsl)).elements as El[];
}

/** The rendered box of the rect whose attached label text is `label`. */
function boxOf(els: El[], label: string): El {
  const t = els.find((e) => e.type === "text" && e.text === label);
  assert.ok(t, `text "${label}" not rendered`);
  // find the smallest rectangle containing that text (its container)
  const rects = els.filter(
    (e) =>
      (e.type === "rectangle" || e.type === "ellipse") &&
      t!.x >= e.x - 1 && t!.y >= e.y - 1 &&
      t!.x + 1 <= e.x + e.width && t!.y + 1 <= e.y + e.height,
  );
  assert.ok(rects.length > 0, `no container box around "${label}"`);
  return rects.sort((a, b) => a.width * a.height - b.width * b.height)[0]!;
}

test("stacked blocks land below each other with a gap, inside the content area", () => {
  const els = compile(`screen S
  navbar "Hub"
  sidebar "Home | Reports"
  heading "First"
  heading "Second"
`);
  const first = els.find((e) => e.type === "text" && e.text === "First")!;
  const second = els.find((e) => e.type === "text" && e.text === "Second")!;
  const navBar = els.find((e) => e.type === "rectangle" && e.height === 56)!;
  assert.ok(first.x >= 264, `content starts right of the sidebar, got x=${first.x}`);
  assert.ok(first.y >= navBar.y + 56, "content clears the navbar band");
  assert.ok(second.y > first.y + 20, "second block stacked below the first");
});

test("a row of three cards splits the width equally with gaps, same y", () => {
  const els = compile(`screen S
  navbar "Hub"
  sidebar "A | B"
  row
    card "Open | 128 | six audits"
    card "Overdue | 14 | follow up"
    card "Review | 32 | awaiting"
`);
  const a = boxOf(els, "Open");
  const b = boxOf(els, "Overdue");
  const c = boxOf(els, "Review");
  assert.equal(a.y, b.y);
  assert.equal(b.y, c.y);
  assert.ok(Math.abs(a.width - b.width) <= 1 && Math.abs(b.width - c.width) <= 1, "equal widths");
  assert.ok(b.x >= a.x + a.width + 8, "gap between 1st and 2nd");
  assert.ok(c.x + c.width <= 1240 + 1, `row ends inside the content area, got ${c.x + c.width}`);
});

test("`right` packs the rest of a row against the content's right edge", () => {
  const els = compile(`screen S
  navbar "Hub"
  row
    heading "Needs your attention"
    right
    button "New audit" primary
`);
  const btn = els.find((e) => e.type === "rectangle" && e.backgroundColor === "#fa7b3f")!;
  assert.ok(btn, "primary button missing");
  assert.ok(Math.abs(btn.x + btn.width - 1240) <= 1, `button right edge at 1240, got ${btn.x + btn.width}`);
});

test("split renders two independent columns with a vertical divider between", () => {
  const els = compile(`screen S
  navbar "Hub"
  sidebar "A | B"
  split 60/40
    left
      table "Version | By"
        row "v3 | J. Alvarez"
    right
      card "Discussion"
        text "K. Smith: please attach the sign-off"
`);
  const vline = els.find((e) => e.type === "line" && e.height > e.width && e.height > 60);
  assert.ok(vline, "vertical divider missing");
  const disc = els.find((e) => e.type === "text" && e.text === "Discussion")!;
  assert.ok(disc.x > vline!.x, "right column content sits right of the divider");
  const version = els.find((e) => e.type === "text" && e.text === "Version")!;
  assert.ok(version.x < vline!.x, "left column content sits left of the divider");
});

test("children nested under a card render inside it and the card grows around them", () => {
  const els = compile(`screen S
  navbar "Hub"
  card "Discussion"
    text "First comment"
    textarea "Add a comment…"
    button "Post" primary
`);
  const title = els.find((e) => e.type === "text" && e.text === "Discussion")!;
  const card = els
    .filter((e) => e.type === "rectangle" && e.x <= title.x && e.y <= title.y)
    .sort((a, b) => b.width * b.height - a.width * a.height)[0]!;
  for (const label of ["First comment", "Add a comment…"]) {
    const t = els.find((e) => e.type === "text" && e.text === label)!;
    assert.ok(
      t.x >= card.x && t.y >= card.y && t.y <= card.y + card.height,
      `"${label}" inside the card (card ${card.y}..${card.y + card.height}, text y=${t.y})`,
    );
  }
});

test("a badge child docks to its card's top-right, inside the border", () => {
  const els = compile(`screen S
  navbar "Hub"
  row
    card "SOC2 Type II — 2024"
      badge "On track" success
    card "ISO 27001"
      badge "At risk" warning
`);
  const badge = boxOf(els, "On track");
  const card = boxOf(els, "SOC2 Type II — 2024"); // smallest container = the card
  assert.ok(badge.x + badge.width <= card.x + card.width, "badge inside right border");
  assert.ok(badge.x > card.x + card.width / 2, "badge in the right half");
});

test("the frame auto-grows below 800 when content stacks past it", () => {
  const many = Array.from({ length: 12 }, (_, i) => `  card "Panel ${i} | ${i} | of many"`).join("\n");
  const els = compile(`screen S\n  navbar "Hub"\n${many}\n`);
  const frame = els.find((e) => e.type === "rectangle" && e.width === 1280)!;
  assert.ok(frame.height > 800, `frame grew, got ${frame.height}`);
});

test("legacy coordinate dialect is rejected with a regenerate message", () => {
  assert.throws(
    () => compile(`screen S\n  heading "Overview" 280,84\n`),
    /coordinate dialect|regenerate/i,
  );
});

test("INVARIANT: flow output always passes the layout oracle (no overlap, in frame)", () => {
  const screens = [
    `screen Dash "Admin overview"
  navbar "Hub"
  sidebar "Home | Audits | Reports"
  text "OPERATIONS" muted
  row
    heading "Good morning"
    right
    search "Search everything"
    select "All frameworks"
  row
    card "Open | 128 | across audits"
    card "Overdue | 14 | escalate"
    card "Review | 32 | awaiting"
    card "Findings | 5 | 2 high"
  row
    heading "Needs attention"
    right
    button "New audit" primary
  table "A | B | C | D"
    row "1 | 2 | 3 | 4"
    row "5 | 6 | 7 | 8"
`,
    `screen Detail "One record"
  navbar "Hub"
  sidebar "Home | Audits"
  breadcrumb "Audits / SOC2 / CC6.1"
  row
    heading "CC6.1 — Access Reviews"
    badge "Needs Correction" danger
  split 60/40
    left
      table "Version | By | Status"
        row "v3 | J. Alvarez | Needs Correction"
      button "Upload New Evidence" primary
    right
      card "Discussion"
        text "K. Smith: attach the sign-off"
        textarea "Add a comment…"
        button "Post" primary
      heading "Activity"
      text "2d ago — correction requested"
`,
    `screen Form "Create a thing"
  navbar "Shop | Catalog | Cart"
  heading "New product"
  input "Name"
  row
    select "Category"
    select "Status"
  textarea "Description"
  row
    right
    button "Cancel"
    button "Create" primary -> Dash
`,
  ];
  for (const s of screens) {
    assert.deepEqual(validateWireframeLayout(s), [], `oracle clean for:\n${s.slice(0, 40)}…`);
  }
});

test("strict syntax: unknown keywords and misplaced groups are reported with line numbers", () => {
  const errs = validateWireframeSyntax(`screen S
  navbar "Hub"
  bogus "what is this"
  left
`);
  assert.ok(errs.some((e) => /line 3/.test(e) && /bogus/.test(e)), `unknown kind flagged: ${errs}`);
  assert.ok(errs.some((e) => /line 4/.test(e) && /left/.test(e)), `stray left flagged: ${errs}`);
});

test("strict syntax: a clean flow file has no errors", () => {
  assert.deepEqual(
    validateWireframeSyntax(`screen S
  navbar "Hub"
  row
    heading "Hi"
    right
    button "Go" primary
  table "A | B"
    row "1 | 2"
`),
    [],
  );
});

test("a `row` nests inside a card: children share the card's inner width", () => {
  // The exact shape agents reach for (and the gate used to reject): an entity
  // card with side-by-side content inside it.
  const els = compile(`screen S
  navbar "Hub"
  card "This week"
    row
      text "Workouts: 4"
      text "Volume: 12,400 kg"
    progress "4/5"
`);
  const title = els.find((e) => e.type === "text" && e.text === "This week")!;
  const card = boxOf(els, "This week");
  const a = els.find((e) => e.type === "text" && e.text === "Workouts: 4")!;
  const b = els.find((e) => e.type === "text" && e.text === "Volume: 12,400 kg")!;
  assert.equal(a.y, b.y, "row children share a baseline");
  assert.ok(b.x > a.x + 20, "side by side");
  assert.ok(a.x >= card.x && b.x < card.x + card.width, "inside the card");
  assert.ok(a.y > title.y, "below the card title");
  const prog = els.find((e) => e.type === "rectangle" && e.height === 10)!;
  assert.ok(prog.y > a.y, "next block stacks below the row");
  assert.ok(card.y + card.height >= prog.y + 10, "card grew around everything");
});

test("row-in-card passes the syntax gate and the layout oracle", () => {
  const dsl = `screen S
  navbar "Hub"
  row
    card "SOC2"
      row
        text "68/92"
        badge "On track" success
    card "ISO"
      text "plain child"
`;
  assert.deepEqual(validateWireframeSyntax(dsl), []);
  assert.deepEqual(validateWireframeLayout(dsl), []);
});


test("screens stack vertically: every screen frame shares x=0 and each starts below the previous", () => {
  const els = compile(`screen One
  heading "First"
screen Two
  heading "Second"
screen Three
  heading "Third"
`);
  const frames = els
    .filter((e) => e.type === "rectangle" && e.width === 1280)
    .sort((a, b) => a.y - b.y);
  assert.equal(frames.length, 3);
  assert.ok(frames.every((f) => f.x === 0), "all frames at x=0");
  assert.ok(frames[1]!.y >= frames[0]!.y + frames[0]!.height, "second below first");
  assert.ok(frames[2]!.y >= frames[1]!.y + frames[1]!.height, "third below second");
});

test("a screen's element ids survive an earlier screen growing taller", () => {
  // Screens stack in one column, so growing an earlier screen moves every
  // later one down the canvas. Identity is screen-relative precisely so that
  // move does not re-id untouched elements — a viewer diffing two scenes
  // would otherwise read every screen below the edit as wholly replaced.
  const filler = Array.from({ length: 20 }, (_, i) => `  text "line ${i}"`).join("\n");
  const idsOfTwo = (dsl: string) =>
    compile(dsl)
      .filter((e) => (e as { customData?: { screen?: string } }).customData?.screen === "Two")
      .map((e) => e.id)
      .sort();

  const before = idsOfTwo(`screen One\n  heading "A"\nscreen Two\n  heading "B"\n`);
  const after = idsOfTwo(`screen One\n  heading "A"\n${filler}\nscreen Two\n  heading "B"\n`);
  assert.ok(before.length > 0);
  assert.deepEqual(after, before, "screen Two's ids must not change when screen One grows");
});

test("every element carries customData.screen naming the screen it belongs to", () => {
  const els = compile(`screen Login "Sign in"
  button "Go" primary
screen Home "Landing"
  heading "Welcome"
`);
  const byScreen = new Map<string, number>();
  for (const e of els) {
    const s = (e as { customData?: { screen?: string } }).customData?.screen;
    assert.ok(s, `element ${e.id} lacks customData.screen`);
    byScreen.set(s!, (byScreen.get(s!) ?? 0) + 1);
  }
  assert.ok((byScreen.get("Login") ?? 0) > 0);
  assert.ok((byScreen.get("Home") ?? 0) > 0);
  assert.equal(byScreen.size, 2);
});


// ---------- compileWireframes: the compiler reports which screens changed ----------

const SHOP = `screen One
  heading "A"
screen Two
  button "Go"
screen Three
  heading "C"
`;

test("compileWireframes with no previous result reports no changed screens", () => {
  const r = compileWireframes(SHOP, null);
  assert.ok(r.ok);
  assert.deepEqual(r.changedScreens, []);
  assert.ok(JSON.parse(r.json).elements.length > 0);
});

test("compileWireframes reports only the screen whose content changed", () => {
  const first = compileWireframes(SHOP, null);
  assert.ok(first.ok);
  const second = compileWireframes(SHOP.replace('button "Go"', 'button "Go" primary'), first);
  assert.ok(second.ok);
  assert.deepEqual(second.changedScreens, ["Two"]);
});

test("compileWireframes does not report screens that merely moved down", () => {
  const first = compileWireframes(SHOP, null);
  assert.ok(first.ok);
  const filler = Array.from({ length: 20 }, (_, i) => `  text "line ${i}"`).join("\n");
  const grown = SHOP.replace('  heading "A"', `  heading "A"\n${filler}`);
  const second = compileWireframes(grown, first);
  assert.ok(second.ok);
  assert.deepEqual(second.changedScreens, ["One"], "Two and Three shifted but did not change");
});

test("compileWireframes reports an added screen and a removed screen", () => {
  const first = compileWireframes(SHOP, null);
  assert.ok(first.ok);
  const added = compileWireframes(SHOP + 'screen Four\n  heading "D"\n', first);
  assert.ok(added.ok);
  assert.deepEqual(added.changedScreens, ["Four"]);
  const removed = compileWireframes(SHOP.replace('screen Three\n  heading "C"\n', ""), first);
  assert.ok(removed.ok);
  assert.deepEqual(removed.changedScreens, ["Three"]);
});

test("compileWireframes ranks a removed screen by where it USED to sit", () => {
  // Remove Two and edit Three in one step. Two is gone from the new compile, so
  // its position is only knowable from the previous one — without that it sorts
  // last and the report claims Three changed before Two, contradicting the
  // canvas the reader was looking at.
  const first = compileWireframes(SHOP, null);
  assert.ok(first.ok);
  const next = compileWireframes(
    SHOP.replace('screen Two\n  button "Go"\n', "").replace('heading "C"', 'heading "C2"'),
    first,
  );
  assert.ok(next.ok);
  assert.deepEqual(next.changedScreens, ["Two", "Three"]);
});

test("compileWireframes reports nothing when the source is unchanged", () => {
  const first = compileWireframes(SHOP, null);
  assert.ok(first.ok);
  const again = compileWireframes(SHOP, first);
  assert.ok(again.ok);
  assert.deepEqual(again.changedScreens, []);
});

test("compileWireframes reports changed screens in canvas order", () => {
  const first = compileWireframes(SHOP, null);
  assert.ok(first.ok);
  const both = compileWireframes(
    SHOP.replace('heading "C"', 'heading "C2"').replace('heading "A"', 'heading "A2"'),
    first,
  );
  assert.ok(both.ok);
  assert.deepEqual(both.changedScreens, ["One", "Three"]);
});

test("compileWireframes fails like tryDslToExcalidraw on an empty source", () => {
  const r = compileWireframes("", null);
  assert.equal(r.ok, false);
});
