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
 * project/componentName now come straight from the caller's ae_create_issue
 * arguments — there is no separate identity channel to resolve them against.
 * See docs/design/draft/2026-09-17-sre-agent-extensions-handoff.md §2/§3.
 */

import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { fileURLToPath } from "node:url";

import { HANDOFF_LABELS, resolveHandoff } from "./handoffContext.js";

const ARGS = { project: "argproj", componentName: "argcomp", labels: ["needs-triage"], actionStatuses: ["revised"] };

test("project and componentName are taken straight from the arguments", () => {
  const resolved = resolveHandoff(ARGS);

  assert.equal(resolved.project, "argproj");
  assert.equal(resolved.componentName, "argcomp");
});

test("no component leaves componentName absent", () => {
  const resolved = resolveHandoff({ project: "argproj", actionStatuses: [] });

  assert.equal(resolved.componentName, undefined);
});

test("the handoff labels are added once, on top of the model's own", () => {
  const resolved = resolveHandoff({ project: "p", labels: ["bug", "mine"], actionStatuses: [] });

  assert.deepEqual(resolved.labels, ["bug", "mine", "incident"]);
  for (const label of HANDOFF_LABELS) assert.ok(resolved.labels.includes(label));
});

test("actionStatuses is carried through exactly, including nulls", () => {
  const resolved = resolveHandoff({ project: "p", actionStatuses: ["suggested", null, "revised"] });
  assert.deepEqual(resolved.actionStatuses, ["suggested", null, "revised"]);
});

// aep-api's SRE issue authority stamps these labels on every SRE filing.
// Resolved relative to this file so the assertion holds regardless of where
// the suite is invoked from.
const SRE_ISSUE_SOURCE = fileURLToPath(
  new URL("../../aep-api/internal/sourcecontrol/issue_incident.go", import.meta.url),
);
const KIND_BUG_SOURCE = fileURLToPath(new URL("../../aep-api/internal/delivery/labels.go", import.meta.url));

function readGoConstant(path: string, name: string): string {
  const source = readFileSync(path, "utf8");
  const match = new RegExp(`${name}\\s*=\\s*"([^"]+)"`).exec(source);
  assert.ok(match, `could not find the ${name} declaration in ${path}`);
  const value = match?.[1];
  assert.ok(value, `the ${name} pattern matched but captured no value`);
  return value as string;
}

test("HANDOFF_LABELS is pinned, element for element and in order, to aep-api's label constants", () => {
  const kindBug = readGoConstant(KIND_BUG_SOURCE, "KindBug");
  const sreSource = readFileSync(SRE_ISSUE_SOURCE, "utf8");
  assert.match(sreSource, /labels\s*:=\s*\[\]string\{"bug",\s*incidentTrackingLabel\}/);

  assert.deepEqual(HANDOFF_LABELS, [kindBug, "incident"]);
});
