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

// ../gen-scopes.mjs against the three design fixtures. The generated file is
// the only route from security.json into the bundle, and the failure modes that
// matter are not "it crashed": a non-deterministic emit churns every diff, a
// wrong branch on a missing specs tree breaks every image build, and a SCREENS
// table that leaks another component's screens routes a webapp to a screen it
// has no page for.

import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const GENERATOR = path.join(here, "..", "gen-scopes.mjs");
const SNAPSHOT = path.join(here, "expense-tracker.scopes.gen.txt");

/** The repo's design fixtures, found by walking up rather than by a long ../. */
const FIXTURES = (() => {
  let dir = here;
  for (;;) {
    const candidate = path.join(dir, "packages/agent-stream/test/fixtures/security");
    if (existsSync(candidate)) return candidate;
    const parent = path.dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
})();

const temporaries = [];
function workspace() {
  const dir = mkdtempSync(path.join(tmpdir(), "gen-scopes-"));
  temporaries.push(dir);
  return dir;
}

process.on("exit", () => {
  for (const dir of temporaries) rmSync(dir, { recursive: true, force: true });
});

/**
 * Lay out a project the way the platform does: the app is a component folder
 * beside the specs tree, and the generator lives in the app's scripts/.
 */
function project(fixture, { depth = 1 } = {}) {
  const root = workspace();
  mkdirSync(path.join(root, "specs/design"), { recursive: true });
  cpSync(path.join(FIXTURES, fixture), path.join(root, "specs/design/security.json"));
  const app = path.join(root, ...Array.from({ length: depth }, (_, i) => `level${i}`));
  mkdirSync(path.join(app, "scripts"), { recursive: true });
  cpSync(GENERATOR, path.join(app, "scripts/gen-scopes.mjs"));
  return { root, app };
}

function run(app, args = []) {
  const result = spawnSync(process.execPath, [path.join(app, "scripts/gen-scopes.mjs"), ...args], {
    encoding: "utf8",
    cwd: app,
    env: { ...process.env, AEP_SECURITY_JSON: "", AEP_APP_COMPONENT: "" },
  });
  return {
    status: result.status,
    out: `${result.stdout ?? ""}${result.stderr ?? ""}`,
    generated: () => readFileSync(path.join(app, "src/scopes.gen.ts"), "utf8"),
  };
}

test("gen-scopes", { skip: FIXTURES ? false : "design fixtures not in this checkout" }, async (t) => {
  await t.test("emits the expense-tracker snapshot byte for byte", () => {
    const { app } = project("expense-tracker.json");
    const result = run(app, ["--component", "expense-webapp"]);
    assert.equal(result.status, 0, result.out);
    assert.equal(result.generated(), readFileSync(SNAPSHOT, "utf8"));
  });

  await t.test("is deterministic — two runs, identical bytes", () => {
    const { app } = project("expense-tracker.json");
    assert.equal(run(app, ["--component", "expense-webapp"]).status, 0);
    const first = run(app, ["--component", "expense-webapp"]).generated();
    const second = run(app, ["--component", "expense-webapp"]).generated();
    assert.equal(first, second);
  });

  await t.test("walks UP for the specs tree — ../specs is never assumed", () => {
    // Four levels between the app and the project root: nothing may hardcode
    // how deep a component sits.
    const { app } = project("expense-tracker.json", { depth: 4 });
    const result = run(app, ["--component", "expense-webapp"]);
    assert.equal(result.status, 0, result.out);
    assert.match(result.generated(), /export type Scope =/);
  });

  await t.test("service-kind roles are NOT in the browser's tables", () => {
    // vendor.json declares `reconciliation-job` with kind: "service" — it has
    // no login, so it can never be a role the badge shows or NoAccess names.
    const { app } = project("vendor.json");
    const result = run(app, ["--component", "vendor-webapp"]);
    assert.equal(result.status, 0, result.out);
    const generated = result.generated();
    assert.doesNotMatch(generated, /reconciliation-job/);
    assert.match(generated, /export const ROLES: readonly Role\[\] = \["Buyer", "Finance", "Supplier"\]/);
    // The catalog itself keeps every handle, granted to a person or not.
    assert.match(generated, /"payments:release"/);
  });

  await t.test("SCREENS carries only this component's rows, in declared order", () => {
    // clinic.json has two web apps. A staff-webapp that emitted booking-site's
    // rows would route to a screen it has no page for — and src/screens.ts
    // throws on exactly that, loudly, at module load.
    const { app } = project("clinic.json");
    assert.equal(run(app, ["--component", "staff-webapp"]).status, 0);
    const staff = run(app, ["--component", "staff-webapp"]).generated();
    assert.doesNotMatch(staff, /booking-site/);
    assert.match(
      staff,
      /SCREENS: readonly ScreenGate\[\] = \[\n {2}\{ component: "staff-webapp", screen: "Front desk", requires: "appointments:manage" \},\n {2}\{ component: "staff-webapp", screen: "Schedule", requires: "schedule:read" \},\n {2}\{ component: "staff-webapp", screen: "Patient records", requires: "records:read" \},\n\]/,
    );

    const booking = run(app, ["--component", "booking-site"]).generated();
    assert.doesNotMatch(booking, /staff-webapp/);
    // "public" survives as the literal the screen gate reads.
    assert.match(booking, /screen: "Find a slot", requires: "public"/);
  });

  await t.test("a self-service role emits empty assignment tables, not prose", () => {
    // clinic.json's Patient has no assignTo and no assignableBy. NoAccess reads
    // these; it must find empty arrays rather than a hardcoded sentence.
    const { app } = project("clinic.json");
    const generated = run(app, ["--component", "booking-site"]).generated();
    assert.match(generated, /ROLE_ASSIGN_TO[\s\S]*?"Patient": \[\],/);
    assert.match(generated, /ROLE_ASSIGNABLE_BY[\s\S]*?"Patient": \[\],/);
    assert.match(generated, /ROLE_ASSIGN_TO[\s\S]*?"Receptionist": \["Clinic Reception"\],/);
  });

  await t.test("no specs tree + a committed output: keeps it, says why, exits 0", () => {
    // This is EVERY per-component image build: the context is the app folder
    // alone, so there is no ancestor holding specs/. Exiting non-zero here
    // would fail the build of every web application the platform ships.
    const app = path.join(workspace(), "app");
    mkdirSync(path.join(app, "scripts"), { recursive: true });
    mkdirSync(path.join(app, "src"), { recursive: true });
    cpSync(GENERATOR, path.join(app, "scripts/gen-scopes.mjs"));
    const committed = '// GENERATED earlier and COMMITTED\nexport type Scope = "claims:read";\n';
    writeFileSync(path.join(app, "src/scopes.gen.ts"), committed);

    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.match(result.out, /out of reach/);
    assert.match(result.out, /keeping the committed src\/scopes\.gen\.ts/);
    assert.equal(result.generated(), committed);
  });

  await t.test("no specs tree and no committed output: exits 1 naming why", () => {
    const app = path.join(workspace(), "app");
    mkdirSync(path.join(app, "scripts"), { recursive: true });
    cpSync(GENERATOR, path.join(app, "scripts/gen-scopes.mjs"));

    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /not found/);
    assert.match(result.out, /COMMIT the/);
    assert.equal(existsSync(path.join(app, "src/scopes.gen.ts")), false);
  });

  await t.test("a duplicate catalog handle is refused, not silently deduped", () => {
    const { root, app } = project("expense-tracker.json");
    const spec = path.join(root, "specs/design/security.json");
    const doc = JSON.parse(readFileSync(spec, "utf8"));
    doc.permissions.push({
      resource: "claims",
      component: "expense-api",
      actions: [{ handle: "read", ownership: "own" }],
    });
    writeFileSync(spec, JSON.stringify(doc));

    const result = run(app, ["--component", "expense-webapp"]);
    assert.equal(result.status, 1);
    assert.match(result.out, /duplicate handles in the catalog: claims:read/);
  });
});
