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
 * Write-gate behavior for wireframes `.dsl` syntax: the flow dialect computes
 * geometry from structure, so the gate polices STRUCTURE — invalid lines would
 * be silently dropped by the tolerant compile, which is how content quietly
 * goes missing. Same seam as the design.json schema gate.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { checkWireframeLayout } from "../src/wireframe-layout.ts";
import { FileBundle } from "../src/bundle.ts";

const PATH = "specs/design/components/shop-webapp/wireframes.dsl";

const CLEAN = `screen Dashboard "Admin overview"
  navbar "Hub"
  sidebar "Home | Reports"
  row
    heading "Good morning"
    right
    button "New audit" primary
  row
    card "Open items | 47 | across 5 projects"
    card "Overdue | 12 | needs escalation"
  table "A | B"
    row "1 | 2"
`;

const LEGACY = `screen Dashboard "Admin overview"
  navbar "Hub"
  heading "Overview" 280,84
  card "Open | 47 | stuff" 280,160 280x160
`;

const TYPO = `screen Dashboard
  navbar "Hub"
  crd "Open items | 47 | across 5 projects"
`;

test("accepts a clean flow-dialect wireframe", () => {
  assert.equal(checkWireframeLayout(PATH, CLEAN), null);
});

test("rejects the retired coordinate dialect with line numbers", () => {
  const problem = checkWireframeLayout(PATH, LEGACY);
  assert.equal(problem?.code, "INVALID_DSL");
  assert.match(problem!.message, /line 3/);
  assert.match(problem!.message, /coordinates/);
});

test("rejects an unknown element kind (a typo would silently vanish)", () => {
  const problem = checkWireframeLayout(PATH, TYPO);
  assert.equal(problem?.code, "INVALID_DSL");
  assert.match(problem!.message, /line 3/);
  assert.match(problem!.message, /crd/);
});

test("ignores non-wireframe paths and non-dsl files", () => {
  assert.equal(checkWireframeLayout("specs/requirements/prd.md", LEGACY), null);
  assert.equal(checkWireframeLayout("specs/design/components/api/erd.dsl", LEGACY), null);
});

test("FileBundle.addFile rejects a bad wireframe and stays unchanged", () => {
  const bundle = new FileBundle({});
  const res = bundle.addFile(PATH, TYPO);
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.code, "INVALID_DSL");
  assert.equal(bundle.has(PATH), false);
});

test("FileBundle.addFile applies a clean wireframe", () => {
  const bundle = new FileBundle({});
  const res = bundle.addFile(PATH, CLEAN);
  assert.equal(res.ok, true);
  assert.equal(bundle.has(PATH), true);
});

test("FileBundle.editFile that introduces bad syntax is rejected, file unchanged", () => {
  const bundle = new FileBundle({ [PATH]: CLEAN });
  const res = bundle.editFile(
    PATH,
    '    card "Overdue | 12 | needs escalation"',
    '    crd "Overdue | 12 | needs escalation"',
  );
  assert.equal(res.ok, false);
  if (!res.ok) assert.equal(res.code, "INVALID_DSL");
  assert.match(bundle.read(PATH) ?? "", /card "Overdue/);
});
