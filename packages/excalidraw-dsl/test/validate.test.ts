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
 * validateWireframeLayout is the ORACLE for the flow layout engine: overlap
 * and out-of-frame are inexpressible in the dialect, so the oracle must come
 * back clean for anything the engine lays out (asserted here and in
 * layout.test.ts). validateWireframeSyntax is the write-gate's strict check.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { validateWireframeLayout, validateWireframeSyntax } from "../src/index.js";

test("the oracle is clean for a dense flow screen (rows, split, card nesting)", () => {
  const issues = validateWireframeLayout(`screen Dashboard "Admin overview"
  navbar "Hub"
  sidebar "Home | Audits | Reports"
  row
    heading "Good morning"
    right
    search "Search"
    select "All frameworks"
  row
    card "Open | 128 | across audits"
    card "Overdue | 14 | escalate"
    card "Review | 32 | awaiting"
  split 60/40
    left
      table "A | B"
        row "1 | 2"
    right
      card "Discussion"
        text "hello"
        badge "Open" info
`);
  assert.deepEqual(issues, []);
});

test("the oracle returns no issues for unparseable or legacy sources (syntax owns those)", () => {
  assert.deepEqual(validateWireframeLayout("garbage {{{"), []);
  assert.deepEqual(validateWireframeLayout('screen S\n  heading "Hi" 280,84\n'), []);
});

test("syntax: the retired coordinate dialect is reported with the line number", () => {
  const errs = validateWireframeSyntax('screen S\n  heading "Overview" 280,84\n');
  assert.ok(errs.some((e) => /line 2/.test(e) && /coordinates/.test(e)), `got: ${errs}`);
});

test("syntax: a quoted row outside a table is reported", () => {
  const errs = validateWireframeSyntax('screen S\n  row "a | b"\n');
  assert.ok(errs.some((e) => /line 2/.test(e) && /table/.test(e)), `got: ${errs}`);
});

test("syntax: split percentages parse and misplaced groups are reported", () => {
  const ok = validateWireframeSyntax(`screen S
  split 60/40
    left
      text "a"
    right
      text "b"
`);
  assert.deepEqual(ok, []);
  const bad = validateWireframeSyntax('screen S\n  right\n');
  assert.ok(bad.some((e) => /line 2/.test(e)), `got: ${bad}`);
});

test("syntax: an explicit WxH is still legal anywhere", () => {
  assert.deepEqual(
    validateWireframeSyntax('screen S\n  chart "Trend" 600x220\n'),
    [],
  );
});
