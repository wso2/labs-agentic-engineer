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

// seed.mjs turns a wireframe's example data into fixtures for mock mode.
// Pinned here against a DSL excerpt that exercises what the grammar allows
// around a table: a layout `row` above it, quoted `row`s under it, a `right`
// inside a layout row, nested `card` children, comments, a `-> Screen` on the
// table, and a `flow` block that must contribute nothing.

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import path from "node:path";
import { seedFromDsl } from "./seed.mjs";

const SCRIPT = path.join(import.meta.dirname, "seed.mjs");

// The risk register from wireframes/references/authoring.md, trimmed to two screens.
const DSL = `// Risk register — excerpt
screen RiskQueue "Manager monitors open risk across registers"
  navbar "RiskHub"
  sidebar "Overview | Review Queue -> RiskQueue | All Registers | Audits | Settings"
  row
    heading "Risk Queue"
    right
    select "Register: All"
  row
    heading "Needs review"
    right
    button "Review next" primary -> QueueRiskDetail
  table "Risk | Owner | Severity | Status | Updated" -> QueueRiskDetail
    row "Unpatched edge servers | Platform team | High | Open | 2h ago"
    row "Stale access keys | Security | Medium | In review | 1d ago"   // trailing comment
    row "Vendor SOC2 lapse | Compliance | High | Overdue | 3d ago"

screen QueueRiskDetail "Manager reviews progress and escalates risks that stall"
  navbar "RiskHub"
  sidebar "Overview | Review Queue -> RiskQueue | All Registers | Audits | Settings"
  row
    heading "Unpatched edge servers"
    right
    badge "High" danger
  text "Owner: Platform team — Updated 2h ago"
  row
    card "Open risks | 24 | across 6 registers"
    card "High severity | 3 | review this week"
    card "Overdue actions | 6"
  card "Review notes"
    text "You · 3d: second week overdue — needs a date."
    row
      textarea "Add a review note…"
      button "Post note"
  table "Action | Assignee | Due | Status"
    row "Patch kernel CVE-2026-1 | A. Chen | Fri | Done"
    row "Rotate edge certs | M. Diaz | Mon | In progress"
  list "Overview | Audits | Settings"
  row
    right
    select "Mine only"

flow "Approval queue"
  role "Manager"
  description "A manager triages queued risks and reviews each in detail"
  RiskQueue
  QueueRiskDetail
`;

test("each table's quoted rows become records keyed by its columns, and the arrow is kept", () => {
  const { screens } = seedFromDsl(DSL);
  assert.deepEqual(Object.keys(screens), ["RiskQueue", "QueueRiskDetail"]);
  const [table] = screens.RiskQueue.tables;
  assert.deepEqual(table.columns, ["Risk", "Owner", "Severity", "Status", "Updated"]);
  assert.equal(table.target, "QueueRiskDetail");
  assert.deepEqual(table.rows, [
    { Risk: "Unpatched edge servers", Owner: "Platform team", Severity: "High", Status: "Open", Updated: "2h ago" },
    { Risk: "Stale access keys", Owner: "Security", Severity: "Medium", Status: "In review", Updated: "1d ago" },
    { Risk: "Vendor SOC2 lapse", Owner: "Compliance", Severity: "High", Status: "Overdue", Updated: "3d ago" },
  ]);
});

test("a layout `row` is not a table row, and a table's rows stop at the next element", () => {
  const { screens } = seedFromDsl(DSL);
  assert.equal(screens.RiskQueue.tables.length, 1);
  const [table] = screens.QueueRiskDetail.tables;
  assert.equal(table.rows.length, 2);
  assert.equal(table.target, null);
  // The `row` under `card "Review notes"` and the `row` after the list are layout.
  assert.deepEqual(screens.QueueRiskDetail.lists, [{ items: ["Overview", "Audits", "Settings"] }]);
});

test("stat cards carry label, value and caption; a two-part card has no caption; a panel card is not a stat", () => {
  const { screens } = seedFromDsl(DSL);
  assert.deepEqual(screens.QueueRiskDetail.stats, [
    { label: "Open risks", value: "24", caption: "across 6 registers" },
    { label: "High severity", value: "3", caption: "review this week" },
    { label: "Overdue actions", value: "6", caption: null },
  ]);
});

test("selects split `Label: Value`; a bare label has no preselected value; badges keep their variant", () => {
  const { screens } = seedFromDsl(DSL);
  assert.deepEqual(screens.RiskQueue.selects, [{ label: "Register", value: "All" }]);
  assert.deepEqual(screens.QueueRiskDetail.selects, [{ label: "Mine only", value: null }]);
  assert.deepEqual(screens.QueueRiskDetail.badges, [{ label: "High", variant: "danger" }]);
});

test("flow blocks and chrome contribute nothing", () => {
  const { screens } = seedFromDsl(DSL);
  assert.equal(screens["Approval queue"], undefined);
  assert.equal(screens.RiskQueue.stats.length, 0);
  assert.equal(screens.RiskQueue.badges.length, 0);
});

test("a `//` inside a quoted label is content, not a comment", () => {
  const { screens } = seedFromDsl(`screen S "x"\n  table "Link | Note"\n    row "https://a.example | see // notes"\n`);
  assert.deepEqual(screens.S.tables[0].rows, [{ Link: "https://a.example", Note: "see // notes" }]);
});

test("the CLI prints JSON for a file, or writes it with -o", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "wireframes-seed-"));
  const dsl = path.join(dir, "wireframes.dsl");
  writeFileSync(dsl, DSL);
  const printed = spawnSync(process.execPath, [SCRIPT, dsl], { encoding: "utf8" });
  assert.equal(printed.status, 0, printed.stderr);
  assert.equal(JSON.parse(printed.stdout).screens.RiskQueue.tables[0].rows.length, 3);

  const out = path.join(dir, "fixtures.json");
  const written = spawnSync(process.execPath, [SCRIPT, "-o", out, dsl], { encoding: "utf8" });
  assert.equal(written.status, 0, written.stderr);
  assert.match(written.stdout, /wrote .*fixtures\.json/);
  assert.equal(JSON.parse(readFileSync(out, "utf8")).screens.QueueRiskDetail.stats.length, 3);
});

test("an output path that cannot be written is a named failure, not a stack trace", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "wireframes-seed-"));
  const dsl = path.join(dir, "wireframes.dsl");
  writeFileSync(dsl, DSL);
  const r = spawnSync(process.execPath, [SCRIPT, "-o", dir, dsl], { encoding: "utf8" });
  assert.equal(r.status, 1);
  assert.match(r.stdout, /FAIL  cannot write .*wireframes-seed-/);
  assert.doesNotMatch(r.stdout + r.stderr, /at \w+ \(/);
});

test("-o without a path is a usage error, not a failed write of 'undefined'", () => {
  const dir = mkdtempSync(path.join(tmpdir(), "wireframes-seed-"));
  const dsl = path.join(dir, "wireframes.dsl");
  writeFileSync(dsl, DSL);
  const r = spawnSync(process.execPath, [SCRIPT, dsl, "-o"], { encoding: "utf8" });
  assert.equal(r.status, 2);
  assert.match(r.stdout, /usage: node seed\.mjs/);
  assert.doesNotMatch(r.stdout, /undefined/);
});

test("a missing file is a named failure; no file prints the usage", () => {
  const missing = spawnSync(process.execPath, [SCRIPT, "/nowhere/wireframes.dsl"], { encoding: "utf8" });
  assert.equal(missing.status, 1);
  assert.match(missing.stdout, /FAIL  cannot read \/nowhere\/wireframes\.dsl/);
  const usage = spawnSync(process.execPath, [SCRIPT], { encoding: "utf8" });
  assert.equal(usage.status, 2);
  assert.match(usage.stdout, /usage: node seed\.mjs/);
});
