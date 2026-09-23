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
import { dslToExcalidraw, tryDslToExcalidraw } from "../src/index.js";

type El = {
  type: string;
  x: number;
  y: number;
  width: number;
  height: number;
  text?: string;
  strokeColor?: string;
  backgroundColor?: string;
};

// Semantic *status* colors (danger/success/warning/info). These must only
// appear when an element opts in via a variant — never leak into plain chrome.
// (Brand orange from the Oxygen theme is expected on chrome and is NOT here.)
const STATUS_COLORS = /^#(d92d20|2e7d32|ed6c02|0288d1)$/i;

function compile(dsl: string): El[] {
  return JSON.parse(dslToExcalidraw("wireframes", dsl)).elements as El[];
}

const DESKTOP = `screen Dashboard
  navbar "RiskHub | Dashboard | Risks | Reports"
  sidebar "Overview | My Risks | Audits | Settings"
  heading "Risk Overview"
  table "Risk | Owner | Severity | Status"
  button "New risk"
screen Detail
  heading "Risk Detail"
flow
  Dashboard -> Detail
`;

test("screens default to desktop 1280x800", () => {
  const els = compile(DESKTOP);
  const outline = els.find((e) => e.type === "rectangle" && e.width === 1280 && e.height === 800);
  assert.ok(outline, "no 1280x800 screen outline found");
});

test("an explicit screen size overrides the default", () => {
  const els = compile("screen Modal 480x320\n  text \"hi\"\n");
  assert.ok(els.some((e) => e.type === "rectangle" && e.width === 480 && e.height === 320));
});

test("navbar spans the screen width without coordinates and renders its items", () => {
  const els = compile(DESKTOP);
  const bar = els.find((e) => e.type === "rectangle" && e.width === 1280 && e.height === 56);
  assert.ok(bar, "navbar bar missing");
  for (const item of ["RiskHub", "Dashboard", "Risks", "Reports"]) {
    assert.ok(els.some((e) => e.type === "text" && e.text === item), `navbar item ${item} missing`);
  }
});

test("sidebar renders as a left rail with stacked items", () => {
  const els = compile(DESKTOP);
  const rail = els.find((e) => e.type === "rectangle" && e.width === 240 && e.height > 600);
  assert.ok(rail, "sidebar rail missing");
  const overview = els.find((e) => e.type === "text" && e.text === "Overview");
  const settings = els.find((e) => e.type === "text" && e.text === "Settings");
  assert.ok(overview && settings && settings.y > overview.y, "sidebar items not stacked");
});

test("table renders a header row, column headers, and row lines", () => {
  const els = compile(DESKTOP);
  for (const col of ["Risk", "Owner", "Severity", "Status"]) {
    assert.ok(els.some((e) => e.type === "text" && e.text === col), `column ${col} missing`);
  }
  assert.ok(els.some((e) => e.type === "line"), "table grid lines missing");
});

test("image placeholder renders a crossed box", () => {
  const els = compile('screen S\n  image "logo" 200x120\n');
  assert.equal(els.filter((e) => e.type === "line").length, 2);
});

test("primary actions and active navigation use the Oxygen brand color", () => {
  const els = compile(`screen S
  navbar "BrandApp | Home | Reports"
  button "Create" primary`);
  assert.ok(
    els.some((e) => e.type === "rectangle" && e.backgroundColor === "#fa7b3f"),
    "primary button not brand-filled",
  );
  assert.ok(
    els.some((e) => e.type === "text" && e.text === "BrandApp" && e.strokeColor === "#fa7b3f"),
    "active navbar item not branded",
  );
});

test("legacy kinds (rect/ellipse/button/text) still parse and flows still mark", () => {
  const els = compile(DESKTOP + "  rect \"x\"\n");
  const texts = els.filter((e) => e.type === "text");
  assert.ok(texts.some((t) => /^Screen \d+$/.test(t.text ?? "")), "screen number marker missing");
});

test("a screen description renders as a subtitle above the frame", () => {
  const els = compile(`screen Dashboard "Where managers monitor open risk"
  heading "Risk"`);
  assert.ok(
    els.some((e) => e.type === "text" && e.text === "Where managers monitor open risk"),
    "screen description subtitle missing",
  );
  assert.ok(els.some((e) => e.type === "text" && e.text === "Dashboard"), "screen name missing");
});

test("a screen description does not break the optional size", () => {
  const els = compile(`screen Modal "Confirm deletion" 480x320
  text "hi"`);
  assert.ok(els.some((e) => e.type === "rectangle" && e.width === 480 && e.height === 320), "sized frame missing");
  assert.ok(els.some((e) => e.type === "text" && e.text === "Confirm deletion"), "description missing");
});

test("a one-part card stays a simple panel title (back-compat)", () => {
  const els = compile(`screen S\n  card "Remediation progress" 300x120`);
  assert.ok(els.some((e) => e.type === "text" && e.text === "Remediation progress"), "panel title missing");
});

test("a multi-part card renders as a stat tile (label, big value, caption)", () => {
  const els = compile(`screen S\n  card "Open items | 47 | across 5 active audits" 300x120`);
  for (const t of ["Open items", "47", "across 5 active audits"]) {
    assert.ok(els.some((e) => e.type === "text" && e.text === t), `tile part "${t}" missing`);
  }
  const value = els.find((e) => e.type === "text" && e.text === "47") as (El & { fontSize?: number }) | undefined;
  const metric = els.find((e) => e.type === "text" && e.text === "Open items") as (El & { fontSize?: number }) | undefined;
  assert.ok((value?.fontSize ?? 0) > (metric?.fontSize ?? 0), "value not larger than its label");
});

test("the navbar groups links right and ends in a bell + account avatar", () => {
  const els = compile(`screen S\n  navbar "AuditHub | Dashboard | Reports"`);
  const brand = els.find((e) => e.type === "text" && e.text === "AuditHub");
  assert.ok(brand && brand.strokeColor === "#fa7b3f", "brand not left/branded");
  const link = els.find((e) => e.type === "text" && e.text === "Dashboard");
  assert.ok(link && link.x > 1280 / 2, "nav links not grouped right");
  const rightEllipses = els.filter((e) => e.type === "ellipse" && e.x > 1280 - 120);
  assert.ok(rightEllipses.length >= 2, "bell/avatar missing from navbar");
});

test("a heading renders an underline rule beneath it", () => {
  const before = compile(`screen S\n  text "x"`).filter((e) => e.type === "line").length;
  const after = compile(`screen S\n  heading "Recent activity"`).filter((e) => e.type === "line").length;
  assert.ok(after > before, "heading underline rule missing");
});

test("an element `-> Screen` renders a navigation marker beside that element", () => {
  const els = compile(`screen Catalog
  button "View product" -> ProductDetail
screen ProductDetail
  heading "Details"`);
  assert.ok(
    els.some((e) => e.type === "text" && /^→ Screen 2 · ProductDetail$/.test(e.text ?? "")),
    "element nav marker missing/mis-formatted",
  );
  assert.ok(els.some((e) => e.type === "text" && e.text === "View product"), "button label lost");
});

test("a `-> Screen` target that is a variant word is not mistaken for a variant", () => {
  const els = compile(`screen A\n  button "Go" -> Info\nscreen Info\n  heading "I"`);
  assert.ok(els.some((e) => e.type === "text" && /→ Screen 2 · Info/.test(e.text ?? "")), "nav to Info missing");
});

test("a `\\n` in a label becomes a real line break (card title + subtitle)", () => {
  const els = compile(`screen S\n  card "Speckled Mug\\n$28 · In stock" 260x160`);
  const label = els.find((e) => e.type === "text" && /^Speckled Mug/.test(e.text ?? ""));
  assert.ok(label, "card label missing");
  assert.ok(label!.text!.includes("\n") && !label!.text!.includes("\\n"), "backslash-n not converted to newline");
});

test("a taller-than-wide divider draws a vertical rule (column separator)", () => {
  const vert = compile(`screen S\n  divider "" 1x400`);
  const horiz = compile(`screen S\n  divider ""`);
  const vLine = vert.find((e) => e.type === "line") as (El & { points?: [number, number][] }) | undefined;
  const hLine = horiz.find((e) => e.type === "line") as (El & { points?: [number, number][] }) | undefined;
  assert.ok(vLine && vLine.points![1]![1] > vLine.points![1]![0], "divider not vertical when tall");
  assert.ok(hLine && hLine.points![1]![0] > hLine.points![1]![1], "divider not horizontal when wide");
});

test("tryDslToExcalidraw reports a parse error instead of throwing", () => {
  const res = tryDslToExcalidraw("wireframes", "not a wireframe\n");
  assert.equal(res.ok, false);
  if (!res.ok) assert.ok(res.error.length > 0);
});

test("tryDslToExcalidraw reports the retired coordinate dialect", () => {
  const res = tryDslToExcalidraw("wireframes", 'screen S\n  heading "Hi" 280,84\n');
  assert.equal(res.ok, false);
  if (!res.ok) assert.match(res.error, /coordinate dialect|regenerate/i);
});

// ---------- Richer vocabulary ----------

test("new primitives parse and render (tabs, list, badge, avatar, toggle, chart)", () => {
  const els = compile(`screen S
  tabs "Overview | Activity | Settings"
  list "Alpha | Beta | Gamma"
  row
    badge "Overdue" danger
    avatar "Jane Doe"
    toggle "" active
  progress "60%" info
  chart "Spend by month"
  row
    select "Team"
    search "Search risks"
  textarea "Notes"
  row
    checkbox "Email me" active
    radio "Weekly" active
  divider ""
  breadcrumb "Home / Risks / Detail"
  link "View all"
`);
  for (const t of ["Overview", "Activity", "Settings"]) {
    assert.ok(els.some((e) => e.type === "text" && e.text === t), `tab ${t} missing`);
  }
  for (const t of ["Alpha", "Beta", "Gamma"]) {
    assert.ok(els.some((e) => e.type === "text" && e.text === t), `list item ${t} missing`);
  }
  assert.ok(els.some((e) => e.type === "text" && e.text === "JD"), "avatar initials missing");
  assert.ok(els.some((e) => e.type === "ellipse"), "no ellipse rendered");
});

test("a trailing variant paints an element with semantic color", () => {
  const els = compile(`screen S
  row
    button "Delete" danger
    button "Save" primary
    badge "Live" success
`);
  assert.ok(
    els.some((e) => e.type === "rectangle" && e.strokeColor === "#d92d20"),
    "danger accent not applied",
  );
  assert.ok(
    els.some((e) => e.type === "rectangle" && e.backgroundColor === "#fa7b3f"),
    "primary button not brand-filled",
  );
  assert.ok(
    els.some((e) => e.type === "text" && e.text === "Save" && e.strokeColor === "#ffffff"),
    "primary button text not white",
  );
  assert.ok(
    els.some((e) => e.type === "rectangle" && e.backgroundColor === "#e8f5e9"),
    "success badge tint missing",
  );
});

test("status color (danger/success/warning/info) appears ONLY via variants", () => {
  const els = compile(`screen S
  navbar "App | Home | Reports"
  sidebar "Overview | Settings"
  tabs "A | B | C"
  list "One | Two"
  progress "40%"
  avatar "Sam Lee"
  chart "Trend"
  select "Pick"
`);
  const leaked = els.filter(
    (e) =>
      (e.backgroundColor && STATUS_COLORS.test(e.backgroundColor)) ||
      (e.strokeColor && STATUS_COLORS.test(e.strokeColor)),
  );
  assert.deepEqual(leaked, [], `status color leaked without a variant: ${JSON.stringify(leaked)}`);
});

test("table `row` lines render real cell content", () => {
  const els = compile(`screen S
  table "Risk | Owner | Status"
    row "Edge servers | Platform | Open"
    row "Stale creds | Security | Overdue"
`);
  for (const cell of ["Edge servers", "Platform", "Open", "Stale creds", "Security", "Overdue"]) {
    assert.ok(els.some((e) => e.type === "text" && e.text === cell), `cell ${cell} missing`);
  }
});

test("long table cells are clipped to their column (no overflow into the next)", () => {
  const els = compile(`screen S
  table "Claim | When" 400x120
    row "Client dinner — Acme pitch and follow-up | Jul 8"
`);
  const cell = els.find((e) => e.type === "text" && /^Client dinner/.test(e.text ?? ""));
  assert.ok(cell, "claim cell missing");
  assert.ok(cell!.text!.endsWith("…"), `expected truncation, got "${cell!.text}"`);
  assert.ok(els.some((e) => e.type === "text" && e.text === "Jul 8"), "neighbour cell missing");
});

test("progress fill width tracks the fraction in the label", () => {
  const half = compile(`screen S\n  progress "50%" 200x10\n`);
  const full = compile(`screen S\n  progress "100%" 200x10\n`);
  const fillW = (els: El[]) =>
    Math.max(
      ...els
        .filter((e) => e.type === "rectangle" && e.height === 10 && e.backgroundColor === "#6c757d")
        .map((e) => e.width),
    );
  assert.ok(fillW(half) < fillW(full), `progress fill did not grow: ${fillW(half)} vs ${fillW(full)}`);
});

test("a long text line is clipped to the screen's right edge with an ellipsis", () => {
  const long = Array.from({ length: 6 }, () => "Please attach the sign-off email for the Q3 access review").join(" — ");
  const els = compile(`screen S\n  text "${long}"\n`);
  const t = els.find((e) => e.type === "text" && /^Please attach/.test(e.text ?? ""))!;
  assert.ok(t, "text element missing");
  assert.ok(t.text!.endsWith("…"), "long text should be truncated with an ellipsis");
  const approxRight = t.x + t.text!.length * 14 * 0.52;
  assert.ok(approxRight <= 1280, `clipped text should stay within the screen, got ~${Math.round(approxRight)}`);
});

test("a short text line is left untouched (no ellipsis)", () => {
  const els = compile(`screen S\n  text "Owner: Platform team"\n`);
  const t = els.find((e) => e.type === "text" && /Owner/.test(e.text ?? ""))!;
  assert.equal(t.text, "Owner: Platform team");
});

// ---------- navigation markers must never overprint ----------

/** Axis-aligned overlap between two boxes, with a small tolerance. */
function overlaps(a: El, b: El): boolean {
  return (
    a.x < b.x + b.width - 1 &&
    a.x + a.width > b.x + 1 &&
    a.y < b.y + b.height - 1 &&
    a.y + a.height > b.y + 1
  );
}

test("a navigation marker never overprints a neighbouring control in the same row", () => {
  // Two buttons side by side, the LEFT one navigating. Its marker must not be
  // drawn across the right one — that made both unreadable.
  const els = compile(`screen Confirm
  row
    right
    button "Go back" -> Chat
    button "Confirm booking" primary -> Booked
screen Chat
  heading "c"
screen Booked
  heading "b"`);
  const marker = els.find((e) => e.type === "text" && /Screen 2 · Chat/.test(e.text ?? ""))!;
  assert.ok(marker, "marker missing");
  const confirmBtn = els.find((e) => e.type === "rectangle" && e.backgroundColor === "#fa7b3f")!;
  assert.ok(confirmBtn, "primary button missing");
  assert.ok(!overlaps(marker, confirmBtn), "marker drawn over the neighbouring button");
});

test("a navigation marker never runs past the screen's right edge", () => {
  const els = compile(`screen Confirm
  row
    right
    button "Confirm booking" primary -> Booked
screen Booked
  heading "b"`);
  const marker = els.find((e) => e.type === "text" && /Screen 2 · Booked/.test(e.text ?? ""))!;
  assert.ok(marker, "marker missing");
  assert.ok(marker.x + marker.width <= 1280 + 1, `marker ends at ${marker.x + marker.width}, past 1280`);
});

test("a navigation marker with room to its right still sits beside its control", () => {
  const els = compile(`screen Catalog
  button "View product" -> ProductDetail
screen ProductDetail
  heading "Details"`);
  const btn = els.find((e) => e.type === "rectangle" && e.width < 400 && e.height < 60)!;
  const marker = els.find((e) => e.type === "text" && /Screen 2 · ProductDetail/.test(e.text ?? ""))!;
  assert.ok(marker.x >= btn.x + btn.width, "marker should be to the right when there is room");
  assert.ok(
    Math.abs(marker.y + marker.height / 2 - (btn.y + btn.height / 2)) < 12,
    "vertically centred on the control",
  );
});
