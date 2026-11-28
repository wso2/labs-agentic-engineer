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

// ../app/scripts/gen-authz.mjs against the repo's design fixtures. The three
// generated files are the only route from security.json and the OpenAPI
// contracts into the bundle, and the failure modes that matter are not "it
// crashed": a non-deterministic emit churns every diff, a wrong branch on a
// missing specs tree breaks every image build, and an operations table that
// disagrees with mock/authz/contract.ts gates screens one way and refuses API
// calls another.
//
// HOW `yaml` RESOLVES HERE. It is NOT resolvable from the repo root — this is a
// pnpm workspace and only the packages that depend on it have it linked. Each
// temporary project therefore gets `node_modules/yaml` symlinked to the copy
// `packages/agent-stream` resolves, which is exactly the shape a real generated
// app has (`yaml` is its devDependency, for the mock plugin). `withYaml: false`
// drops the symlink, which is how the unresolvable-parser branch is walked.

import { test } from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import {
  copyFileSync,
  cpSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const GENERATOR = path.join(here, "..", "app", "scripts", "gen-authz.mjs");
const CONTRACT_TS = path.join(here, "..", "app", "mock", "authz", "contract.ts");

/** The repo's design fixtures, found by walking up rather than by a long ../. */
const REPO = (() => {
  let dir = here;
  for (;;) {
    if (existsSync(path.join(dir, "packages/agent-stream/test/fixtures/security"))) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
})();
const FIXTURES = REPO ? path.join(REPO, "packages/agent-stream/test/fixtures/security") : null;
const YAML_PACKAGE = REPO
  ? path.dirname(
      createRequire(path.join(REPO, "packages/agent-stream/package.json")).resolve(
        "yaml/package.json",
      ),
    )
  : null;

const temporaries = [];
function workspace() {
  const dir = mkdtempSync(path.join(tmpdir(), "gen-authz-"));
  temporaries.push(dir);
  return dir;
}

process.on("exit", () => {
  for (const dir of temporaries) rmSync(dir, { recursive: true, force: true });
});

const OUTPUTS = ["src/authz/roles.gen.ts", "src/authz/operations.gen.ts", "mock/authz/roles.gen.ts"];

/**
 * Lay out a project the way the platform does: the app is a component folder
 * beside the specs tree, the generator lives in the app's scripts/, and each
 * API component's contract sits at specs/design/components/<name>/openapi.yaml.
 *
 * `screens` is stripped from the fixture on the way in. Version 3 carries no
 * screen table; the fixtures lose theirs in the same change, and this keeps the
 * test honest whichever order the two land in.
 */
function project(fixture, { depth = 1, contracts = true, withYaml = true } = {}) {
  const root = workspace();
  const design = path.join(root, "specs/design");
  mkdirSync(design, { recursive: true });
  const doc = JSON.parse(readFileSync(path.join(FIXTURES, `${fixture}.json`), "utf8"));
  delete doc.screens;
  writeFileSync(path.join(design, "security.json"), `${JSON.stringify(doc, null, 2)}\n`);

  if (contracts) {
    for (const file of readdirSync(path.join(FIXTURES, fixture))) {
      if (!file.endsWith(".openapi.yaml")) continue;
      const component = file.slice(0, -".openapi.yaml".length);
      mkdirSync(path.join(design, "components", component), { recursive: true });
      copyFileSync(
        path.join(FIXTURES, fixture, file),
        path.join(design, "components", component, "openapi.yaml"),
      );
    }
  }

  const app = path.join(root, ...Array.from({ length: depth }, (_, i) => `level${i}`));
  mkdirSync(path.join(app, "scripts"), { recursive: true });
  cpSync(GENERATOR, path.join(app, "scripts/gen-authz.mjs"));
  if (withYaml) linkYaml(root);
  return { root, design, app };
}

/** `yaml` where a generated app has it: in the project's own node_modules. */
function linkYaml(root) {
  mkdirSync(path.join(root, "node_modules"), { recursive: true });
  symlinkSync(YAML_PACKAGE, path.join(root, "node_modules/yaml"), "dir");
}

function run(app, args = [], { env = {}, cwd = app } = {}) {
  const result = spawnSync(process.execPath, [path.join(app, "scripts/gen-authz.mjs"), ...args], {
    encoding: "utf8",
    cwd,
    env: { ...process.env, AEP_SECURITY_JSON: "", ...env },
  });
  return {
    status: result.status,
    out: `${result.stdout ?? ""}${result.stderr ?? ""}`,
    generated: (which = 0) => readFileSync(path.join(app, OUTPUTS[which]), "utf8"),
  };
}

/** Rewrite the project's security.json — the document under test. */
function rewriteSpec(root, mutate) {
  const spec = path.join(root, "specs/design/security.json");
  const doc = JSON.parse(readFileSync(spec, "utf8"));
  const next = mutate(doc);
  writeFileSync(spec, JSON.stringify(next === undefined ? doc : next));
  return spec;
}

/** Overwrite one component's contract. */
function writeContract(design, component, yaml) {
  mkdirSync(path.join(design, "components", component), { recursive: true });
  writeFileSync(path.join(design, "components", component, "openapi.yaml"), yaml);
}

/** Previously generated, COMMITTED outputs the generator must not clobber. */
function commit(app, only = OUTPUTS) {
  const committed = {};
  for (const file of only) {
    const contents = `// GENERATED earlier and COMMITTED: ${file}\n`;
    mkdirSync(path.dirname(path.join(app, file)), { recursive: true });
    writeFileSync(path.join(app, file), contents);
    committed[file] = contents;
  }
  return committed;
}

/** The OPERATIONS table read back out of the emitted TypeScript. */
function operationsOf(source) {
  const table = {};
  for (const line of source.split("\n")) {
    const row = /^ {2}"([^"]+)": \{ kind: "(\w+)"(?:, scope: "([^"]+)")? \},$/.exec(line);
    if (!row) continue;
    table[row[1]] = row[3] === undefined ? { kind: row[2] } : { kind: row[2], scope: row[3] };
  }
  return table;
}

const MINIMAL = `openapi: 3.0.3
info: { title: T, version: "1.0.0" }
components:
  securitySchemes:
    oauth2:
      type: oauth2
      flows: {}
security:
  - oauth2: []
paths:
`;

test("gen-authz", { skip: FIXTURES ? false : "design fixtures not in this checkout" }, async (t) => {
  // --- the three outputs -----------------------------------------------------

  await t.test("emits the expense-tracker snapshots byte for byte", () => {
    const { app } = project("expense-tracker");
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    const snapshots = ["roles", "operations", "mock-roles"];
    for (let i = 0; i < OUTPUTS.length; i += 1) {
      assert.equal(
        result.generated(i),
        readFileSync(path.join(here, `expense-tracker.${snapshots[i]}.gen.txt`), "utf8"),
        `${OUTPUTS[i]} does not match expense-tracker.${snapshots[i]}.gen.txt`,
      );
    }
  });

  await t.test("is deterministic — two runs, identical bytes", () => {
    const { app } = project("expense-tracker");
    assert.equal(run(app).status, 0);
    const first = OUTPUTS.map((_, i) => run(app).generated(i));
    const second = OUTPUTS.map((_, i) => run(app).generated(i));
    assert.deepEqual(first, second);
  });

  await t.test("walks UP for the specs tree — ../specs is never assumed", () => {
    // Four levels between the app and the project root: nothing may hardcode
    // how deep a component sits.
    const { app } = project("expense-tracker", { depth: 4 });
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.match(result.generated(0), /export type Scope =/);
    assert.match(result.generated(1), /"GET \/me\/claims": \{ kind: "scope", scope: "claims:read" \}/);
  });

  await t.test("service-kind roles are NOT in the browser's tables", () => {
    // vendor.json declares `reconciliation-job` with kind: "service" — it has
    // no login, so it can never be a role the badge shows, a persona the mock
    // walks, or a role NoAccess names.
    const { app } = project("vendor");
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.doesNotMatch(result.generated(0), /reconciliation-job/);
    assert.doesNotMatch(result.generated(2), /reconciliation-job/);
    assert.match(
      result.generated(0),
      /export const ROLES: readonly Role\[\] = \["Buyer", "Finance", "Supplier"\]/,
    );
    // The catalog itself keeps every handle, granted to a person or not.
    assert.match(result.generated(0), /"payments:release"/);
  });

  await t.test("mock roles keep the DECLARED order; the catalog is sorted", () => {
    // security.json declares Employee first, and `?role=` with no value signs
    // in as the first row. Sorting it would silently change the default
    // persona — Approver — which is the one thing the mock must not do.
    const { app } = project("expense-tracker");
    assert.equal(run(app).status, 0);
    const mock = run(app).generated(2);
    assert.ok(
      mock.indexOf('name: "Employee"') < mock.indexOf('name: "Approver"'),
      "mock/authz/roles.gen.ts must keep security.json's order",
    );
    // Sorted in the app's own table, where it is a lookup.
    const roles = run(app).generated(0);
    assert.ok(roles.indexOf('"Approver"') < roles.indexOf('"Employee"'));
  });

  await t.test("a dependency's contract is read too", () => {
    // A web app usually holds the API it calls as a dependency stub rather than
    // owning the component; both layouts are one discovery.
    const { design, app } = project("expense-tracker", { contracts: false });
    const contract = readFileSync(
      path.join(FIXTURES, "expense-tracker/expense-api.openapi.yaml"),
      "utf8",
    );
    mkdirSync(path.join(design, "components/expense-webapp/dependencies"), { recursive: true });
    writeFileSync(
      path.join(design, "components/expense-webapp/dependencies/expense-api.openapi.yaml"),
      contract,
    );
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.match(result.generated(1), /"GET \/claims": \{ kind: "scope", scope: "claims:read-all" \}/);
  });

  await t.test("no contract at all: an empty operations table that reads like one", () => {
    const { app } = project("expense-tracker", { contracts: false });
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.match(result.generated(1), /export type OperationKey =\n {2}\| never;/);
    assert.match(
      result.generated(1),
      /export const OPERATIONS: Record<OperationKey, OperationRequirement> = \{\};\n/,
    );
  });

  // --- how a contract is read ------------------------------------------------

  await t.test("absent security is signedIn, `security: []` is public", () => {
    const { app } = project("expense-tracker");
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    const table = operationsOf(result.generated(1));
    assert.deepEqual(table["GET /me"], { kind: "signedIn" });
    assert.deepEqual(table["GET /health"], { kind: "public" });
    assert.deepEqual(table["POST /me/claims"], { kind: "scope", scope: "claims:submit" });
  });

  await t.test("a contract that declares no oauth2 scheme contributes public operations", () => {
    // An API with no sign-in has no gateway policy in front of it. Refusing it
    // for lacking a scheme would stop an app that legitimately has none.
    const { design, app } = project("expense-tracker", { contracts: false });
    writeContract(
      design,
      "open-api",
      `openapi: 3.0.3\ninfo: { title: T, version: "1.0.0" }\npaths:\n  /public:\n    get: { responses: { "200": { description: ok } } }\n`,
    );
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.deepEqual(operationsOf(result.generated(1)), { "GET /public": { kind: "public" } });
  });

  await t.test("HEAD, TRACE and OPTIONS are not operations a screen gates on", () => {
    const { design, app } = project("expense-tracker", { contracts: false });
    writeContract(
      design,
      "expense-api",
      `${MINIMAL}  /claims:\n    get: { security: [{ oauth2: [claims:read] }], responses: { "200": { description: ok } } }\n    head: { responses: { "200": { description: ok } } }\n    trace: { responses: { "200": { description: ok } } }\n    options: { security: [], responses: { "200": { description: ok } } }\n`,
    );
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.deepEqual(operationsOf(result.generated(1)), {
      "GET /claims": { kind: "scope", scope: "claims:read" },
    });
  });

  await t.test("two scopes on one operation is refused, naming method and path", () => {
    const { design, app } = project("expense-tracker", { contracts: false });
    writeContract(
      design,
      "expense-api",
      `${MINIMAL}  /claims:\n    get: { security: [{ oauth2: [claims:read, claims:read-all] }], responses: { "200": { description: ok } } }\n`,
    );
    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /expense-api\/openapi\.yaml: GET \/claims/);
    assert.match(result.out, /names 2 scopes; an operation declares exactly one/);
  });

  await t.test("a scope the catalog does not declare is refused", () => {
    // The contract and the catalog are one design. A handle in one and not the
    // other is a screen gated on a scope no role can ever grant.
    const { design, app } = project("expense-tracker", { contracts: false });
    writeContract(
      design,
      "expense-api",
      `${MINIMAL}  /claims:\n    get: { security: [{ oauth2: [claims:audit] }], responses: { "200": { description: ok } } }\n`,
    );
    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /`claims:audit` is not a handle/);
    assert.match(result.out, /permissions\[\]/);
  });

  await t.test("an OIDC scope as an operation's handle is refused", () => {
    const { design, app } = project("expense-tracker", { contracts: false });
    writeContract(
      design,
      "expense-api",
      `${MINIMAL}  /claims:\n    get: { security: [{ oauth2: [profile] }], responses: { "200": { description: ok } } }\n`,
    );
    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /rides every token/);
  });

  await t.test("a contract with no signed-in default is refused", () => {
    const { design, app } = project("expense-tracker", { contracts: false });
    writeContract(
      design,
      "expense-api",
      `openapi: 3.0.3\ninfo: { title: T, version: "1.0.0" }\ncomponents:\n  securitySchemes:\n    oauth2: { type: oauth2, flows: {} }\npaths:\n  /claims:\n    get: { responses: { "200": { description: ok } } }\n`,
    );
    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /declares no `security: \[\{oauth2: \[\]\}\]` default/);
  });

  await t.test("one route in two contracts: the first wins, and it says so", () => {
    const { design, app } = project("expense-tracker", { contracts: false });
    const body = (scope) =>
      `${MINIMAL}  /claims:\n    get: { security: [{ oauth2: [${scope}] }], responses: { "200": { description: ok } } }\n`;
    // Discovery is sorted by path, so a-api is read before z-api.
    writeContract(design, "a-api", body("claims:read"));
    writeContract(design, "z-api", body("claims:read-all"));
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.match(
      result.out,
      /GET \/claims is declared in both components\/a-api\/openapi\.yaml and components\/z-api\/openapi\.yaml/,
    );
    assert.match(result.out, /keeping components\/a-api\/openapi\.yaml/);
    assert.deepEqual(operationsOf(result.generated(1)), {
      "GET /claims": { kind: "scope", scope: "claims:read" },
    });
  });

  await t.test("the generator and mock/authz/contract.ts read one contract the same way", () => {
    // Two implementations of one projection — the generator cannot import a .ts
    // module — so this is the only thing that keeps the screen gates and the
    // mock gateway from reading a contract differently.
    const { root, design, app } = project("expense-tracker");
    assert.equal(run(app).status, 0);
    const mine = operationsOf(run(app).generated(1));

    const probe = path.join(root, "parity.mjs");
    writeFileSync(
      probe,
      `import { readFileSync } from "node:fs";\n` +
        `import { parse } from "yaml";\n` +
        `import { projectOperations } from ${JSON.stringify(CONTRACT_TS)};\n` +
        `const doc = parse(readFileSync(process.argv[2], "utf8"));\n` +
        `console.log(JSON.stringify(projectOperations(doc, "expense-api/openapi.yaml")));\n`,
    );
    const child = spawnSync(
      process.execPath,
      [
        "--experimental-strip-types",
        "--no-warnings",
        probe,
        path.join(design, "components/expense-api/openapi.yaml"),
      ],
      { encoding: "utf8" },
    );
    assert.equal(child.status, 0, `${child.stdout}${child.stderr}`);

    const theirs = {};
    for (const op of JSON.parse(child.stdout)) {
      // OPTIONS is the one deliberate difference: the mock models the preflight
      // the real gateway short-circuits; the SPA never gates a screen on one.
      if (op.method === "OPTIONS") continue;
      theirs[`${op.method} ${op.path}`] = op.isPublic
        ? { kind: "public" }
        : op.scope === null
          ? { kind: "signedIn" }
          : { kind: "scope", scope: op.scope };
    }
    assert.deepEqual(mine, theirs);
  });

  // --- the fallbacks the image build depends on ------------------------------

  await t.test("no specs tree + all three committed outputs: keeps them, exits 0", () => {
    // This is EVERY per-component image build: the context is the app folder
    // alone, so there is no ancestor holding specs/. Exiting non-zero here
    // would fail the build of every web application the platform ships.
    const app = path.join(workspace(), "app");
    mkdirSync(path.join(app, "scripts"), { recursive: true });
    cpSync(GENERATOR, path.join(app, "scripts/gen-authz.mjs"));
    const committed = commit(app);

    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.match(result.out, /out of reach/);
    assert.match(result.out, /keeping the committed/);
    for (const [file, contents] of Object.entries(committed)) {
      assert.equal(readFileSync(path.join(app, file), "utf8"), contents);
    }
  });

  await t.test("no specs tree and a MISSING output: exits 1 naming the gap", () => {
    // Two of three is not a fallback: the app would build against a stale pair
    // and a third file that is simply not there.
    const app = path.join(workspace(), "app");
    mkdirSync(path.join(app, "scripts"), { recursive: true });
    cpSync(GENERATOR, path.join(app, "scripts/gen-authz.mjs"));
    commit(app, OUTPUTS.slice(0, 2));

    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /not found/);
    assert.match(result.out, /mock\/authz\/roles\.gen\.ts is missing/);
    assert.match(result.out, /COMMIT all/);
  });

  await t.test("`yaml` unresolvable + committed outputs: keeps them, exits 0", () => {
    const { app } = project("expense-tracker", { withYaml: false });
    const committed = commit(app);
    const result = run(app);
    assert.equal(result.status, 0, result.out);
    assert.match(result.out, /`yaml` package cannot be resolved/);
    assert.match(result.out, /keeping the committed/);
    for (const [file, contents] of Object.entries(committed)) {
      assert.equal(readFileSync(path.join(app, file), "utf8"), contents);
    }
  });

  await t.test("`yaml` unresolvable with nothing committed: exits 1 naming the dependency", () => {
    const { app } = project("expense-tracker", { withYaml: false });
    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /`yaml` package cannot be resolved/);
    assert.match(result.out, /devDependencies/);
  });

  // --- the document has to BE a version-3 security.json ----------------------

  await t.test("a v1 document is refused — it is not read as an empty catalog", () => {
    // The failure this catches is silent: v1 has no permissions[] in the shape
    // read here, so a tolerant generator emits `Scope = never` over correct
    // committed files, type-checks green, and deploys an app where every caller
    // lands on NoAccess.
    const { root, app } = project("expense-tracker");
    const committed = commit(app);
    rewriteSpec(root, () => ({
      version: 1,
      resources: [{ name: "claims", scopes: ["read"] }],
      roles: [{ name: "Employee", scopes: ["claims:read"] }],
    }));

    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /security\.json v1 is not accepted/);
    assert.match(result.out, /declares version 1/);
    assert.equal(result.generated(0), committed[OUTPUTS[0]]);
  });

  await t.test("a v2 document is refused, and the message says what changed", () => {
    const { root, app } = project("expense-tracker");
    rewriteSpec(root, (doc) => ({ ...doc, version: 2 }));
    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /security\.json v2 is not accepted/);
    assert.match(result.out, /PATH in openapi\.yaml/);
  });

  await t.test("a null document is refused with a message, not a stack", () => {
    const { root, app } = project("expense-tracker");
    const committed = commit(app);
    writeFileSync(path.join(root, "specs/design/security.json"), "null");

    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /is not a security\.json document/);
    assert.match(result.out, /read null/);
    // A TypeError with a stack means the generator dereferenced it first.
    assert.doesNotMatch(result.out, /TypeError|\bat .*gen-authz\.mjs/);
    assert.equal(result.generated(0), committed[OUTPUTS[0]]);
  });

  await t.test("a version-3 document that still carries screens is refused by name", () => {
    // v3-with-screens is the version this project had for one release. Reading
    // it tolerantly would leave the app gated on a table nothing generates.
    const { root, app } = project("expense-tracker");
    rewriteSpec(root, (doc) => ({
      ...doc,
      screens: [{ component: "expense-webapp", screen: "My Claims", requires: "claims:read" }],
    }));
    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /still carries a `screens` key/);
    assert.match(result.out, /src\/authz\/screens\.ts/);
  });

  await t.test("a version-3 document missing permissions[] is refused", () => {
    const { root, app } = project("expense-tracker");
    rewriteSpec(root, (doc) => {
      delete doc.permissions;
      return doc;
    });
    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /has no permissions\[\] array/);
  });

  await t.test("a duplicate catalog handle is refused, not silently deduped", () => {
    const { root, app } = project("expense-tracker");
    rewriteSpec(root, (doc) => {
      doc.permissions.push({
        resource: "claims",
        component: "expense-api",
        actions: [{ handle: "read" }],
      });
      return doc;
    });

    const result = run(app);
    assert.equal(result.status, 1);
    assert.match(result.out, /duplicate handles in the catalog: claims:read/);
  });

  // --- the command line ------------------------------------------------------

  await t.test("an unknown flag is a usage error, and writes nothing", () => {
    // `--component` is GONE: the operations table is the project's, not one
    // component's. A flag silently ignored is a build of the wrong app.
    const { app } = project("expense-tracker");
    for (const args of [
      ["--componenet", "expense-webapp"],
      ["--component", "expense-webapp"],
      ["--out", "src/x.ts"],
    ]) {
      const result = run(app, args);
      assert.equal(result.status, 2, result.out);
      assert.match(result.out, /unknown argument/);
      assert.match(result.out, /Usage:/);
      assert.equal(existsSync(path.join(app, OUTPUTS[0])), false);
    }
  });

  await t.test("a flag with no value is a usage error, and writes nothing", () => {
    const { app } = project("expense-tracker");
    const result = run(app, ["--spec"]);
    assert.equal(result.status, 2);
    assert.match(result.out, /--spec needs a value/);
    assert.match(result.out, /Usage:/);
    assert.equal(existsSync(path.join(app, OUTPUTS[0])), false);
  });

  await t.test("--help prints the usage, exits 0 and writes nothing", () => {
    const { app } = project("expense-tracker");
    for (const flag of ["--help", "-h"]) {
      const result = run(app, [flag]);
      assert.equal(result.status, 0, result.out);
      assert.match(result.out, /Usage:/);
      assert.match(result.out, /--out-dir <path>/);
      assert.equal(existsSync(path.join(app, OUTPUTS[0])), false);
    }
  });

  await t.test("--out-dir is an app root, resolved against the CURRENT directory", () => {
    // Not against the app root: a path typed at a shell prompt means what every
    // other tool means by it.
    const { root, app } = project("expense-tracker");
    const result = run(app, ["--out-dir", "elsewhere"], { cwd: root });
    assert.equal(result.status, 0, result.out);
    for (const file of OUTPUTS) {
      assert.equal(existsSync(path.join(root, "elsewhere", file)), true, file);
      assert.equal(existsSync(path.join(app, file)), false, file);
    }
  });

  await t.test("a --spec that does not exist is an error, not a kept output", () => {
    // The out-of-reach branch is for the image build, which names no spec. A
    // path the caller typed and misspelled must not silently keep stale files.
    const { app } = project("expense-tracker");
    const committed = commit(app);
    const missing = path.join(app, "nope/security.json");

    const flagged = run(app, ["--spec", missing]);
    assert.equal(flagged.status, 1);
    assert.match(flagged.out, /was named explicitly/);
    assert.doesNotMatch(flagged.out, /keeping the committed/);
    assert.equal(flagged.generated(0), committed[OUTPUTS[0]]);

    const envd = run(app, [], { env: { AEP_SECURITY_JSON: missing } });
    assert.equal(envd.status, 1);
    assert.match(envd.out, /was named explicitly/);
    assert.equal(envd.generated(0), committed[OUTPUTS[0]]);
  });

  await t.test("AEP_SECURITY_JSON names the document, and the contracts beside it", () => {
    const { root, design, app } = project("expense-tracker");
    // An app folder with no specs ancestor at all, pointed at the project's.
    const detached = path.join(workspace(), "app");
    mkdirSync(path.join(detached, "scripts"), { recursive: true });
    cpSync(GENERATOR, path.join(detached, "scripts/gen-authz.mjs"));
    linkYaml(path.dirname(detached));

    const result = run(detached, [], {
      env: { AEP_SECURITY_JSON: path.join(design, "security.json") },
    });
    assert.equal(result.status, 0, result.out);
    assert.match(result.generated(1), /"GET \/reports": \{ kind: "scope", scope: "reports:read" \}/);
    assert.equal(existsSync(path.join(root, "specs/design/security.json")), true);
  });
});
