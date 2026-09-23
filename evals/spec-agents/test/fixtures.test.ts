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

// Every web-application in a design fixture carries what the platform now
// builds it from: a prototype.json the shared model accepts for its directory,
// whose roles are security.json role names when that file exists (the build
// gate's rule), and the skills the architecture skill pins on a web app. A
// fixture that drifted from this would feed the evals a design the platform
// itself would refuse to build.

import { strict as assert } from "node:assert";
import { test } from "node:test";
import { existsSync, readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { parsePrototypeModel } from "@aep/prototype-model";
import { FIXTURES_DIR } from "../src/config.js";

function webApplications(): { fixture: string; design: string; component: string }[] {
  const out: { fixture: string; design: string; component: string }[] = [];
  for (const fixture of readdirSync(FIXTURES_DIR, { withFileTypes: true })) {
    if (!fixture.isDirectory()) continue;
    const design = join(FIXTURES_DIR, fixture.name, "specs/design");
    const components = join(design, "components");
    if (!existsSync(components)) continue;
    for (const component of readdirSync(components)) {
      const doc = JSON.parse(readFileSync(join(components, component, "design.json"), "utf8")) as { type?: string };
      if (doc.type === "web-application") out.push({ fixture: fixture.name, design, component });
    }
  }
  return out;
}

test("the design fixtures hold at least one web-application", () => {
  assert.ok(webApplications().length > 0);
});

for (const { fixture, design, component } of webApplications()) {
  const dir = join(design, "components", component);

  test(`${fixture}/${component}: prototype.json is a valid prototype of its component`, () => {
    const parsed = parsePrototypeModel(JSON.parse(readFileSync(join(dir, "prototype.json"), "utf8")), { component });
    assert.ok(parsed.ok, JSON.stringify(parsed.ok ? [] : parsed.issues));
    const security = join(design, "security.json");
    if (!existsSync(security)) return;
    const declared = new Set(
      (JSON.parse(readFileSync(security, "utf8")) as { roles: { name: string }[] }).roles.map((r) => r.name),
    );
    for (const role of parsed.model.roles) assert.ok(declared.has(role.id), `role ${role.id} is not a security.json role`);
  });

  test(`${fixture}/${component}: pins the prototype and web-app stack skills, and no wireframes`, () => {
    const { skillsPinned } = JSON.parse(readFileSync(join(dir, "design.json"), "utf8")) as { skillsPinned: string[] };
    for (const skill of ["prototype", "react-webapp"]) assert.ok(skillsPinned.includes(skill), `missing ${skill}`);
    assert.ok(!skillsPinned.includes("excalidraw-wireframes"));
    assert.ok(!existsSync(join(dir, "wireframes.dsl")));
  });
}
