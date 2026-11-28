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

// The cases for ../screens.example.ts — the screen-reachability half of the
// pattern, and in particular WHICH QUESTION DECIDES NoAccess.
//
// That file is a template: it imports `./core` and `./operations.gen`, which
// only resolve once it has been copied into a generated app. So this assembles
// the smallest app that makes it importable — the real `core.ts` and the
// committed Expense Tracker `.gen` fixtures beside it, in a temp dir — rather
// than re-implementing the rule and testing the re-implementation.
//
// NOT named *.test.mjs: importing .ts needs `--experimental-strip-types` on the
// pinned Node, and the repo-wide runner passes no flags. ./screens.test.mjs is
// the wrapper. Run directly with
//
//   node --experimental-strip-types --test screens.cases.mjs

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { fileURLToPath, pathToFileURL } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const assets = path.dirname(here);

/**
 * The app's own imports are extensionless (`from "./core"`), which is what a
 * bundler resolves and what every generated app therefore ships. Node's ESM
 * resolver is not a bundler and needs the extension, so the harness adds it on
 * the way into the temp dir. It rewrites how the modules FIND each other, never
 * what they do — the rule under test is copied byte for byte.
 */
// `path.extname` is the wrong test here: "./operations.gen" has an "extension"
// of ".gen" and would be left alone. Only a real module extension counts.
const addExtensions = (src) =>
  src.replace(/(from\s+")(\.\/[A-Za-z0-9_.-]+)(")/g, (m, a, spec, b) =>
    /\.(ts|tsx|js|mjs|cjs|json)$/.test(spec) ? m : `${a}${spec}.ts${b}`,
  );

/** The smallest generated app that makes screens.example.ts importable. */
function materialize() {
  const dir = mkdtempSync(path.join(tmpdir(), "authz-screens-"));
  const authz = path.join(dir, "src", "authz");
  mkdirSync(authz, { recursive: true });
  const put = (name, src) => writeFileSync(path.join(authz, name), addExtensions(src));

  put("core.ts", readFileSync(path.join(assets, "app/src/authz/core.ts"), "utf8"));
  // The fixtures are committed as .txt so the repo's own tsc never compiles a
  // file that is an EXAMPLE of generated output rather than generated output.
  put("roles.gen.ts", readFileSync(path.join(here, "expense-tracker.roles.gen.txt"), "utf8"));
  put("operations.gen.ts", readFileSync(path.join(here, "expense-tracker.operations.gen.txt"), "utf8"));
  put("screens.ts", readFileSync(path.join(assets, "screens.example.ts"), "utf8"));
  return pathToFileURL(path.join(authz, "screens.ts")).href;
}

const mod = await import(materialize());
const { reachableScreens, hasScopedReach, SCREEN_ROUTES } = mod;

const held = (...scopes) => new Set(scopes);

// The Expense Tracker table is the fixture. `submitclaim` is a form with no
// load call, and it names the operation its SUBMIT makes — which is the rule
// this file exists to pin.
test("a form with no load call still names an operation, never null", () => {
  const form = SCREEN_ROUTES.find((s) => s.key === "submitclaim");
  assert.ok(form, "expected the example table to keep a write-only form");
  assert.equal(
    form.loads,
    "POST /me/claims",
    "a form names the operation its submit makes, so the rail, the route and the button agree",
  );
});

test("a caller who cannot submit does not reach the form", () => {
  const reachable = reachableScreens(held(), true).map((s) => s.key);
  assert.ok(
    !reachable.includes("submitclaim"),
    `a zero-scope caller must not reach a form they can never submit, got ${reachable}`,
  );
});

test("a caller who CAN submit reaches the form", () => {
  const reachable = reachableScreens(held("claims:submit"), true).map((s) => s.key);
  assert.ok(reachable.includes("submitclaim"), `expected the form to be reachable, got ${reachable}`);
});

test("NoAccess: a signed-in caller holding no scope has no scoped reach", () => {
  // Naming the form's submit operation removed the common way this went wrong:
  // the form used to count as reach for a caller who could not submit it, so an
  // App gating NoAccess on `reachable.length === 0` dropped that caller onto a
  // form with an empty rail. Two independently generated apps did exactly that
  // (2026-09-17) and each walk patched it somewhere different. The question is
  // still asked separately, because a public or operation-free screen can keep
  // the list non-empty on its own.
  assert.equal(hasScopedReach(held(), true), false);
});

test("NoAccess: one scope-gated screen is enough to have somewhere to go", () => {
  assert.equal(hasScopedReach(held("claims:read"), true), true);
});

test("a visitor who is not signed in has no scoped reach", () => {
  assert.equal(hasScopedReach(held(), false), false);
});

test("scoped reach never counts a public screen — it is reachable by everyone", () => {
  const publicOnly = SCREEN_ROUTES.filter((s) => s.public);
  // The fixture has to CONTAIN one, or this test passes by describing nothing:
  // a loop over an empty list asserts nothing at all, and the regression it is
  // named for — a public screen counted as reach, so NoAccess never renders —
  // would go straight through it.
  assert.ok(publicOnly.length > 0, "the example table must keep a public screen for this to be a test");
  for (const screen of publicOnly) {
    // Reachable by a caller holding nothing: that is what public means.
    assert.ok(
      reachableScreens(held(), true).includes(screen),
      `a public screen is reachable by everyone, ${screen.key} was not`,
    );
    // And reachable before sign-in, which is the other half of it.
    assert.ok(
      reachableScreens(held(), false).includes(screen),
      `a public screen is reachable by a visitor, ${screen.key} was not`,
    );
  }
  // Yet holding nothing, with those public screens present, is still NoAccess —
  // the rail is not empty and the caller has still earned nowhere to go.
  assert.equal(hasScopedReach(held(), true), false);
  assert.ok(reachableScreens(held(), true).length > 0, "the public screen keeps the rail non-empty");
});
