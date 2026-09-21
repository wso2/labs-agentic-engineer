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
 * The `wire` gate, and the one thing a gate must never do: throw.
 *
 * A gate runs while the phase menu is being DRAWN. Anything it throws takes
 * down the surface a reader would have used to run the phase that fixes the
 * problem — so the menu dies over a broken file, and the only way out is a
 * phase the menu can no longer offer.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { wireGate } from "../src/engine/gates.js";

/** A project tree with one component whose `design.json` is whatever is passed. */
function project(designJson: string, built: boolean): string {
  const dir = mkdtempSync(join(tmpdir(), "wire-gate-"));
  mkdirSync(join(dir, "specs/design/components/api"), { recursive: true });
  writeFileSync(join(dir, "specs/design/components/api/design.json"), designJson);
  if (built) {
    mkdirSync(join(dir, "api"), { recursive: true });
    writeFileSync(join(dir, "api", "Dockerfile"), "FROM scratch\n");
  }
  return dir;
}

test("a built component opens the gate", () => {
  assert.deepEqual(wireGate(project(JSON.stringify({ appPath: "api" }), true)), { ok: true });
});

test("a component with no Dockerfile has not been built", () => {
  const result = wireGate(project(JSON.stringify({ appPath: "api" }), false));
  assert.equal(result.ok, false);
  assert.match(result.reason ?? "", /nothing built yet/);
});

test("a design.json that is not JSON blocks the gate rather than throwing through it", () => {
  // The whole point. `JSON.parse` on a half-written file throws, and this used
  // to let it — so a truncated design took the menu with it.
  const result = wireGate(project("{ not json at all", true));
  assert.equal(result.ok, false);
  assert.match(result.reason ?? "", /not readable JSON/);
  assert.match(result.reason ?? "", /design\.json/, "and it names the file, so it can be fixed");
});

test("one unreadable component does not hide another that is built", () => {
  const dir = project("{ not json at all", false);
  mkdirSync(join(dir, "specs/design/components/web"), { recursive: true });
  writeFileSync(join(dir, "specs/design/components/web/design.json"), JSON.stringify({ appPath: "web" }));
  mkdirSync(join(dir, "web"), { recursive: true });
  writeFileSync(join(dir, "web", "Dockerfile"), "FROM scratch\n");
  assert.deepEqual(wireGate(dir), { ok: true }, "the gate asks whether ANYTHING is built");
});

test("a project with no components says so, and says nothing about JSON", () => {
  const dir = mkdtempSync(join(tmpdir(), "wire-gate-"));
  const result = wireGate(dir);
  assert.equal(result.ok, false);
  assert.match(result.reason ?? "", /no components/);
});
