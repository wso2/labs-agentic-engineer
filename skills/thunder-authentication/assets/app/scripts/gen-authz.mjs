#!/usr/bin/env node
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

// Copied VERBATIM to <app-path>/scripts/gen-authz.mjs and wired into
// package.json:
//
//   "gen":   "node scripts/gen-authz.mjs"
//   "build": "npm run gen && tsc --noEmit && vite build"
//
// `gen` before `tsc` is NOT optional. The whole point of generating string
// literal unions is that a handle or an operation that no longer exists fails
// the type check — and a STALE roles.gen.ts type-checks perfectly green.
// Regenerate, then type check, then bundle.
//
// THREE outputs, every run, from the project's TWO sources of truth:
//
//   src/authz/roles.gen.ts        the permission catalog and the roles, from
//                                 specs/design/security.json
//   src/authz/operations.gen.ts   what each API operation requires, from the
//                                 project's openapi.yaml contracts
//   mock/authz/roles.gen.ts       the personas `?role=` switches between
//
// This is the ONLY route from the design into the bundle: gate routes, nav
// items and copy from these exports rather than retyping a handle, a role name
// or a scope anywhere else. There is no screen table — a screen's gate is the
// operation it LOADS, which src/authz/screens.ts names and openapi.yaml scopes.
//
// Zero dependencies for the catalog half, so it runs before `npm install` has
// resolved anything. The contract half needs a YAML parser and imports `yaml`
// DYNAMICALLY: it is a devDependency of every web app already (mock/plugin.ts
// reads the same contracts), and `npm i` runs before `npm run build` in the
// image. When it cannot be resolved the committed outputs are kept.
//
// Run `node scripts/gen-authz.mjs --help` for the usage; the exit codes are
// 0 written (or committed outputs deliberately kept), 1 the design or a
// contract could not be read, 2 the command line is wrong.

import { existsSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const APP_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const USAGE = `gen-authz — generate the app's authorization tables from specs/design

Usage:
  node scripts/gen-authz.mjs [--spec <path>] [--out-dir <app root>]
  node scripts/gen-authz.mjs --help

  --spec <path>      the security.json to read. Default: walk up from the app
                     folder looking for specs/design/security.json. The API
                     contracts are then read from the specs tree beside it.
  --out-dir <path>   the app root the three files are written under, resolved
                     against the CURRENT DIRECTORY. Default: the app folder
                     this script sits in.
  -h, --help         print this and exit without writing anything.

Writes:
  <app>/src/authz/roles.gen.ts        the catalog and the roles
  <app>/src/authz/operations.gen.ts   what each operation requires
  <app>/mock/authz/roles.gen.ts       the mock personas

Environment:
  AEP_SECURITY_JSON  same as --spec (skips the walk-up)

Exit: 0 written (or committed outputs deliberately kept), 1 the design or a
contract could not be read, 2 this command line is wrong.`;

/**
 * A wrong command line NEVER writes. A flag silently ignored is a file emitted
 * from the wrong document, exit 0 — a working build of the wrong app.
 */
function usageError(message) {
  console.error(`gen-authz: ${message}\n\n${USAGE}`);
  process.exit(2);
}

function fail(message) {
  console.error(`gen-authz: ${message}`);
  process.exit(1);
}

const VALUE_FLAGS = new Set(["--spec", "--out-dir"]);
const options = {};
const args = process.argv.slice(2);
for (let i = 0; i < args.length; i += 1) {
  const arg = args[i];
  if (arg === "--help" || arg === "-h") {
    console.log(USAGE);
    process.exit(0);
  }
  if (!VALUE_FLAGS.has(arg)) usageError(`unknown argument ${JSON.stringify(arg)}`);
  const value = args[i + 1];
  if (value === undefined || value.startsWith("-")) {
    usageError(`${arg} needs a value, e.g. ${arg} <${arg.slice(2)}>`);
  }
  options[arg.slice(2)] = value;
  i += 1;
}

// The default is the app this script sits in; an explicit --out-dir is a path
// the caller typed at a shell prompt, so it resolves the way every other tool
// resolves one.
// `||`, not `??`: a wrapper that exports these as EMPTY strings means "unset".
const OUT_ROOT = options["out-dir"] ? resolve(process.cwd(), options["out-dir"]) : APP_ROOT;

const OUTPUTS = {
  roles: join(OUT_ROOT, "src", "authz", "roles.gen.ts"),
  operations: join(OUT_ROOT, "src", "authz", "operations.gen.ts"),
  mockRoles: join(OUT_ROOT, "mock", "authz", "roles.gen.ts"),
};
const OUTPUT_NAMES = "src/authz/roles.gen.ts, src/authz/operations.gen.ts and mock/authz/roles.gen.ts";

/** Every committed output is there, so keeping them is a whole answer. */
function committedOutputsComplete() {
  return Object.values(OUTPUTS).every((file) => existsSync(file));
}

function missingOutputs() {
  return Object.entries(OUTPUTS)
    .filter(([, file]) => !existsSync(file))
    .map(([, file]) => relative(OUT_ROOT, file))
    .join(", ");
}

const SPEC_RELATIVE = join("specs", "design", "security.json");

/**
 * Walk UP from the app folder looking for the project's specs tree.
 *
 * The app is one component of a project; in the repository the spec tree sits
 * beside it (`<project>/specs/design/security.json`), but how many levels up
 * that is depends on the layout, so nothing may hardcode `../specs`.
 */
function findSpec(startDir) {
  let dir = startDir;
  for (;;) {
    const candidate = join(dir, SPEC_RELATIVE);
    if (existsSync(candidate)) return candidate;
    const parent = dirname(dir);
    if (parent === dir) return null;
    dir = parent;
  }
}

// `||`, not `??`, for the same reason as above.
const namedSpec = process.env.AEP_SECURITY_JSON || options.spec || "";
const specPath = namedSpec || findSpec(APP_ROOT);

// A spec the caller NAMED and that is not there is a typo, not a build context:
// keeping stale committed outputs because `--spec ../spec/security.json` was
// misspelled hides the design change the caller asked to regenerate from.
if (namedSpec && !existsSync(namedSpec)) {
  fail(
    `${namedSpec} was named explicitly (--spec / AEP_SECURITY_JSON) and does not ` +
      `exist. Fix the path, or drop the flag to walk up for ${SPEC_RELATIVE}.`,
  );
}

// A per-component image build's context is the APP FOLDER ALONE, so the
// project's specs/ tree is out of reach inside `docker build` — there is no
// ancestor that holds it. The three outputs are COMMITTED for exactly this
// reason: when the design cannot be read and every previous output exists, keep
// them and exit 0. Failing here would fail every image build the platform runs.
if (!specPath) {
  const where = `${SPEC_RELATIVE} (searched ${APP_ROOT} and every parent)`;
  if (committedOutputsComplete()) {
    console.log(`gen-authz: ${where} is out of reach (per-component build context); keeping the committed ${OUTPUT_NAMES}`);
    process.exit(0);
  }
  fail(
    `${where} not found, and ${missingOutputs()} is missing, so there is nothing ` +
      `to fall back on. Run this from the project checkout once and COMMIT all ` +
      `three generated files — the image build cannot see the specs tree.`,
  );
}

let doc;
try {
  doc = JSON.parse(readFileSync(specPath, "utf8"));
} catch (err) {
  fail(`cannot read ${specPath}: ${err.message}`);
}

// REFUSE anything that is not a version-3 document, rather than reading zero
// permissions out of it. `Scope = never` type-checks green and deploys an app
// in which every caller lands on NoAccess — a v1 file, a null, or a JSON array
// all produce exactly that, silently, over correct committed outputs.
const SHAPE = ["permissions", "roles"];
if (typeof doc !== "object" || doc === null || Array.isArray(doc)) {
  fail(
    `${specPath} is not a security.json document — expected a JSON object with ` +
      `"version": 3, read ${doc === null ? "null" : Array.isArray(doc) ? "an array" : typeof doc}.`,
  );
}
if (doc.version !== 3) {
  fail(
    `${specPath} declares version ${JSON.stringify(doc.version ?? null)}. ` +
      `security.json v${String(doc.version ?? null)} is not accepted; only version 3 is. ` +
      `Version 3 declares the permission catalog in permissions[] (resource, ` +
      `component, actions[{handle, description}]) and the roles that grant handles ` +
      `from it in roles[].grants. It carries no row axis and no screen table: ` +
      `which rows an operation reaches is its PATH in openapi.yaml, and which ` +
      `screens a role reaches follows from the operations those screens load. ` +
      `Re-author it against version 3.`,
  );
}
// A v3 document that still carries the screen table is the version-3 this
// project had for one release, and reading it tolerantly would leave the app
// gated on a table nothing generates any more.
if ("screens" in doc) {
  fail(
    `${specPath} is version 3 but still carries a \`screens\` key. Version 3 has ` +
      `no screen table: a screen's gate is the operation it LOADS, named once in ` +
      `src/authz/screens.ts, and that operation's scope is already in ` +
      `openapi.yaml. Delete screens[] from the document.`,
  );
}
for (const field of SHAPE) {
  if (!Array.isArray(doc[field])) {
    fail(
      `${specPath} has no ${field}[] array — a version-3 document declares both ` +
        `of ${SHAPE.join(", ")}.`,
    );
  }
}

// --- the catalog half --------------------------------------------------------

// Every catalog handle is `<resource>:<action.handle>`. EVERY one is emitted,
// including handles no operation requires — the union is the catalog, not the
// subset this app happens to use today.
const handles = [];
for (const permission of doc.permissions) {
  for (const action of permission.actions ?? []) {
    handles.push(`${permission.resource}:${action.handle}`);
  }
}
handles.sort();
const duplicates = handles.filter((h, i) => i > 0 && h === handles[i - 1]);
if (duplicates.length > 0) {
  fail(`duplicate handles in the catalog: ${[...new Set(duplicates)].join(", ")}`);
}
const catalog = new Set(handles);

// Only user-kind roles reach a browser: a service-kind role has no login, so it
// can never be a role the badge shows, a persona the mock walks, or a role
// NoAccess tells somebody to ask for.
const userRoles = doc.roles.filter((role) => (role.kind ?? "user") === "user");
// src/authz/roles.gen.ts is sorted — it is a lookup table, and a stable order
// keeps the diff quiet. mock/authz/roles.gen.ts is NOT (see below).
const roles = [...userRoles].sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));

// --- the contract half -------------------------------------------------------

/** Methods the gateway is measured for. Anything else cannot be an operation. */
const RENDERABLE = new Set(["GET", "POST", "PUT", "PATCH", "DELETE"]);
/** Declared in contracts, unreachable through the gateway, skipped in silence. */
const SKIPPED = new Set(["HEAD", "TRACE", "OPTIONS"]);
/**
 * The OIDC scopes every access token carries. One of them as an operation's
 * handle would admit every signed-in account in the org while looking guarded,
 * so it is a refusal here exactly as it is at deploy.
 */
const RESERVED_OIDC = new Set(["openid", "profile", "email", "group", "ou"]);
const SCHEME = "oauth2";

function isRecord(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/**
 * Reads ONE operation's `security` block. This is mock/authz/contract.ts's
 * projection restated — the generator cannot import a .ts module, and the two
 * are tested against the same fixture so they cannot drift apart in silence.
 */
function requirementOf(operation, where) {
  if (!("security" in operation)) {
    // Inherits the document default, which is checked to be the signed-in one.
    return { kind: "signedIn" };
  }
  const security = operation.security;
  if (!Array.isArray(security)) fail(`${where}: \`security\` must be a list`);
  if (security.length === 0) return { kind: "public" };
  if (security.length > 1) {
    fail(`${where}: \`security\` names more than one requirement; the gateway serves exactly one`);
  }
  const requirement = security[0];
  if (!isRecord(requirement)) fail(`${where}: the \`security\` entry must be a mapping`);
  const names = Object.keys(requirement);
  if (names.length !== 1) {
    fail(`${where}: the \`security\` entry names ${names.length} schemes; the gateway serves exactly one`);
  }
  if (names[0] !== SCHEME) {
    fail(`${where}: unknown security scheme \`${names[0]}\`, expected \`${SCHEME}\``);
  }
  const scopes = requirement[SCHEME];
  if (!Array.isArray(scopes)) fail(`${where}: \`${SCHEME}\` must carry a list of scopes`);
  if (scopes.length > 1) {
    fail(`${where}: names ${scopes.length} scopes; an operation declares exactly one`);
  }
  // The document default, spelled out on the operation.
  if (scopes.length === 0) return { kind: "signedIn" };
  const scope = scopes[0];
  if (typeof scope !== "string") fail(`${where}: the scope must be a string`);
  if (RESERVED_OIDC.has(scope)) {
    fail(
      `${where}: \`${scope}\` is an OIDC scope that rides every token, so requiring ` +
        `it admits every signed-in account while looking guarded`,
    );
  }
  if (!catalog.has(scope)) {
    fail(
      `${where}: \`${scope}\` is not a handle ${specPath} declares. The contract and ` +
        `the catalog are one design: add the action to permissions[], or spell the ` +
        `operation's scope the way the catalog spells it.`,
    );
  }
  return { kind: "scope", scope };
}

/**
 * One contract's operations, as `{ key, requirement }` rows.
 *
 * A document that declares NO `oauth2` scheme is an API with no sign-in: it has
 * no gateway policy in front of it, so every operation on it is reachable by
 * anyone, which is `public` — not a refusal for lacking a scheme.
 */
function projectContract(document, source) {
  if (!isRecord(document)) fail(`${source}: the document is not a YAML mapping`);
  const schemes = isRecord(document.components) ? document.components.securitySchemes : undefined;
  const scheme = isRecord(schemes) ? schemes[SCHEME] : undefined;
  const unguarded = scheme === undefined;
  if (!unguarded) {
    if (!isRecord(scheme) || scheme.type !== SCHEME) {
      fail(`${source}: components.securitySchemes.${SCHEME} is not of type \`${SCHEME}\``);
    }
    // The document default is what makes an operation whose `security` block was
    // FORGOTTEN fail closed. Without it, absent would mean public.
    const docSecurity = document.security;
    const hasSignedInDefault =
      Array.isArray(docSecurity) &&
      docSecurity.length === 1 &&
      isRecord(docSecurity[0]) &&
      Object.keys(docSecurity[0]).length === 1 &&
      Array.isArray(docSecurity[0][SCHEME]) &&
      docSecurity[0][SCHEME].length === 0;
    if (!hasSignedInDefault) {
      fail(
        `${source}: the document declares no \`security: [{${SCHEME}: []}]\` default ` +
          `with an EMPTY scope list, so an operation that declares none of its own ` +
          `cannot be read as signed-in`,
      );
    }
  }

  const paths = isRecord(document.paths) ? document.paths : {};
  const rows = [];
  for (const [template, item] of Object.entries(paths)) {
    if (!isRecord(item)) continue;
    for (const [rawMethod, operation] of Object.entries(item)) {
      const method = rawMethod.toUpperCase();
      // OPTIONS is a preflight the real gateway short-circuits before its auth
      // policy; the SPA never gates a screen on one.
      if (SKIPPED.has(method)) continue;
      if (!RENDERABLE.has(method)) continue;
      if (!isRecord(operation)) continue;
      const where = `${source}: ${method} ${template}`;
      const requirement = unguarded ? { kind: "public" } : requirementOf(operation, where);
      rows.push({ key: `${method} ${template}`, requirement });
    }
  }
  return rows;
}

/** Where a project keeps the contracts this app might call, relative to specs/design. */
const CONTRACT_ROOTS = [
  { dir: ["components"], depth: 1, suffix: "openapi.yaml" },
  { dir: ["components"], depth: 2, suffix: ".openapi.yaml" },
];

/** Every file under `dir` at exactly `depth` levels down whose name ends in `suffix`. */
function findContracts(dir, depth, suffix) {
  let entries;
  try {
    entries = readdirSync(dir, { withFileTypes: true });
  } catch {
    return [];
  }
  const found = [];
  for (const entry of entries) {
    const full = join(dir, entry.name);
    if (depth > 1) {
      if (entry.isDirectory()) found.push(...findContracts(full, depth - 1, suffix));
    } else if (entry.isFile() && entry.name.endsWith(suffix)) {
      found.push(full);
    }
  }
  return found.sort();
}

const DESIGN_DIR = dirname(specPath);
const contractFiles = CONTRACT_ROOTS.flatMap(({ dir, depth, suffix }) =>
  findContracts(join(DESIGN_DIR, ...dir), depth + 1, suffix),
);

// Imported here and NOWHERE at module scope, so the catalog half runs with no
// dependency at all. An app that has not run `npm install` yet, or an image
// build whose install failed, keeps its committed outputs rather than failing.
let parse;
try {
  ({ parse } = await import("yaml"));
} catch {
  if (committedOutputsComplete()) {
    console.log(
      `gen-authz: the \`yaml\` package cannot be resolved from ${OUT_ROOT}; keeping ` +
        `the committed ${OUTPUT_NAMES}`,
    );
    process.exit(0);
  }
  fail(
    `the \`yaml\` package cannot be resolved from ${OUT_ROOT}, and ${missingOutputs()} ` +
      `is missing. Add \`yaml\` to the app's devDependencies and run \`npm install\` ` +
      `before \`npm run gen\`.`,
  );
}

const operations = new Map();
const declaredIn = new Map();
for (const file of contractFiles) {
  const source = relative(DESIGN_DIR, file);
  let document;
  try {
    document = parse(readFileSync(file, "utf8"));
  } catch (err) {
    fail(`cannot read ${file}: ${err.message}`);
  }
  for (const { key, requirement } of projectContract(document, source)) {
    const first = declaredIn.get(key);
    if (first !== undefined) {
      // Two contracts claiming one route: the app proxies /api to ONE of them,
      // so the second is noise at best and the wrong scope at worst.
      console.error(`gen-authz: ${key} is declared in both ${first} and ${source}; keeping ${first}`);
      continue;
    }
    declaredIn.set(key, source);
    operations.set(key, requirement);
  }
}

// --- the emit ----------------------------------------------------------------

const q = (value) => JSON.stringify(value);
const sorted = (values) => [...new Set(values ?? [])].sort();
const union = (values) => (values.length ? values.map(q).join("\n  | ") : "never");
const list = (values) => `[${values.map(q).join(", ")}]`;

const roleNames = roles.map((role) => role.name);
const roleRow = (role, key) => `  ${q(role.name)}: ${list(sorted(role[key]))},`;

const rolesOut = `// GENERATED by scripts/gen-authz.mjs from specs/design/security.json.
// Do not edit by hand, and do not retype any of these values anywhere else.
// Re-run \`npm run gen\` after the design changes. COMMIT this file: the
// per-component image build cannot see the specs tree.

export type Scope =
  | ${union(handles)};

export type Role =
  | ${union(roleNames)};

/** Every handle the catalog declares, sorted. */
export const SCOPES: readonly Scope[] = ${list(handles)};

/** Every role a person can hold, sorted. Service-kind roles are not here. */
export const ROLES: readonly Role[] = ${list(roleNames)};

/** What each role grants. heldRoles() projects the caller's scopes through this. */
export const ROLE_GRANTS: Record<Role, readonly Scope[]> = {
${roles.map((role) => roleRow(role, "grants")).join("\n")}
};

/** The directory groups a role is assigned to — what NoAccess tells a user to ask for. */
export const ROLE_ASSIGN_TO: Record<Role, readonly string[]> = {
${roles.map((role) => roleRow(role, "assignTo")).join("\n")}
};

/** The roles that can hand this one out — who NoAccess tells a user to ask. */
export const ROLE_ASSIGNABLE_BY: Record<Role, readonly string[]> = {
${roles.map((role) => roleRow(role, "assignableBy")).join("\n")}
};

export function isScope(value: string): value is Scope {
  return (SCOPES as readonly string[]).includes(value);
}
`;

const operationKeys = [...operations.keys()].sort();
const requirementLiteral = (requirement) =>
  requirement.kind === "scope"
    ? `{ kind: "scope", scope: ${q(requirement.scope)} }`
    : `{ kind: ${q(requirement.kind)} }`;
const operationRows = operationKeys.map(
  (key) => `  ${q(key)}: ${requirementLiteral(operations.get(key))},`,
);
// `{}` rather than a `{` and `}` with a blank line between them: an empty table
// is a real outcome (an app whose project declares no contract) and it should
// read like one.
const operationTable = operationRows.length ? `{\n${operationRows.join("\n")}\n}` : "{}";

const operationsOut = `// GENERATED by scripts/gen-authz.mjs from the project's OpenAPI contracts
// (specs/design/components/*/openapi.yaml and
// specs/design/components/*/dependencies/*.openapi.yaml).
// Do not edit by hand, and do not retype any of these values anywhere else.
// Re-run \`npm run gen\` after a contract changes. COMMIT this file: the
// per-component image build cannot see the specs tree.
//
// This table is what a screen is gated on. src/authz/screens.ts names the
// operation each screen LOADS; holding that operation's scope is what makes the
// screen reachable. Nothing else in the app decides that.

import type { Scope } from "./roles.gen";

/** What the gateway demands of a caller before it will pass an operation on. */
export type OperationRequirement =
  | { readonly kind: "public" }
  | { readonly kind: "signedIn" }
  | { readonly kind: "scope"; readonly scope: Scope };

/** "<METHOD> <path template>" as the contract spells it, e.g. "GET /me/claims". Sorted. */
export type OperationKey =
  | ${union(operationKeys)};

export const OPERATIONS: Record<OperationKey, OperationRequirement> = ${operationTable};

export function isOperationKey(value: string): value is OperationKey {
  return Object.prototype.hasOwnProperty.call(OPERATIONS, value);
}
`;

// DECLARED order, and this one alone is not sorted: the first role is the
// persona `?role=` falls back to, so sorting it would silently change who the
// mock signs in as.
const mockRows = userRoles.map(
  (role) => `  { name: ${q(role.name)}, grants: ${list(sorted(role.grants))} },`,
);
const mockTable = mockRows.length ? `[\n${mockRows.join("\n")}\n]` : "[]";

const mockRolesOut = `// GENERATED by scripts/gen-authz.mjs from specs/design/security.json roles[], in
// DECLARED order — the first is the default persona \`?role=\` falls back to.
// Do not edit by hand. Re-run \`npm run gen\` after the design changes.
//
// THE MOCK NEVER WIDENS: each role's grants are exactly what security.json
// declares. A role that cannot reach its own screen is a DESIGN DEFECT to
// report, never a mock to loosen.

export const mockRoles: readonly { readonly name: string; readonly grants: readonly string[] }[] = ${mockTable};
`;

for (const [file, contents] of [
  [OUTPUTS.roles, rolesOut],
  [OUTPUTS.operations, operationsOut],
  [OUTPUTS.mockRoles, mockRolesOut],
]) {
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, contents);
}

console.log(
  `gen-authz: ${handles.length} scopes, ${roleNames.length} roles, ` +
    `${operationKeys.length} operations from ${contractFiles.length} contract(s) -> ${OUT_ROOT}`,
);
