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
 * The reference half of the manifest validator — the rules a JSON Schema
 * cannot express — run only on a manifest that already has the v3 shape.
 * In this order, and findings come out in it:
 *
 *  1. Ids: roles, states, screens and flows share one namespace. A repeat is
 *     `DUPLICATE_ID` and ends validation (a reference into an ambiguous
 *     namespace has no single target).
 *  2. The entry screen is a screen.
 *  3. Each screen's roles are declared.
 *  4. Each flow's role is declared, and each step is a screen it reaches.
 */

import { MANIFEST_FILE, type Finding } from "../findings.js";
import { formatJsonPath } from "./json-path.js";
import type { PrototypeManifest } from "./types.js";

export function referenceFindings(manifest: PrototypeManifest): Finding[] {
  const duplicates = duplicateIdFindings(manifest);
  if (duplicates.length > 0) return duplicates;

  const findings: Finding[] = [];
  const report = (path: (string | number)[], message: string) =>
    findings.push({ code: "UNKNOWN_REFERENCE", file: MANIFEST_FILE, location: formatJsonPath(path), message });

  const roles = new Set(manifest.roles.map((r) => r.id));
  const screens = new Map(manifest.screens.map((s) => [s.id, s]));

  if (!screens.has(manifest.entryScreen)) {
    report(["entryScreen"], `entryScreen ${q(manifest.entryScreen)} is not one of the screens`);
  }
  manifest.screens.forEach((s, i) => {
    s.roleIds.forEach((r, j) => {
      if (!roles.has(r)) report(["screens", i, "roleIds", j], `role ${q(r)} is not declared in roles`);
    });
  });
  manifest.flows.forEach((f, i) => {
    const roleKnown = roles.has(f.roleId);
    if (!roleKnown) report(["flows", i, "roleId"], `role ${q(f.roleId)} is not declared in roles`);
    f.screenIds.forEach((id, j) => {
      const screen = screens.get(id);
      if (!screen) {
        report(["flows", i, "screenIds", j], `screen ${q(id)} is not one of the screens`);
      } else if (roleKnown && !screen.roleIds.includes(f.roleId)) {
        report(["flows", i, "screenIds", j], `screen ${q(id)} is not one role ${q(f.roleId)} reaches; add the role to the screen's roleIds or take the step out of the flow`);
      }
    });
  });
  return findings;
}

function duplicateIdFindings(manifest: PrototypeManifest): Finding[] {
  const first = new Map<string, string>();
  const findings: Finding[] = [];
  const lists = [
    ["roles", manifest.roles],
    ["states", manifest.states],
    ["screens", manifest.screens],
    ["flows", manifest.flows],
  ] as const;
  for (const [name, list] of lists) {
    list.forEach((entry, i) => {
      const location = formatJsonPath([name, i, "id"]);
      const seen = first.get(entry.id);
      if (seen === undefined) {
        first.set(entry.id, location);
        return;
      }
      findings.push({
        code: "DUPLICATE_ID",
        file: MANIFEST_FILE,
        location,
        message: `id ${q(entry.id)} is already used at ${seen}; role, state, screen and flow ids are unique together`,
      });
    });
  }
  return findings;
}

function q(s: string): string {
  return JSON.stringify(s);
}
