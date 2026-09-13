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

// Copied VERBATIM to <app-path>/scripts/gen-scopes.mjs and wired into
// package.json:
//
//   "gen":   "node scripts/gen-scopes.mjs --component <this component's name>"
//   "build": "npm run gen && tsc --noEmit && vite build"
//
// `gen` before `tsc` is NOT optional. The whole point of generating a string
// literal union is that a handle that no longer exists fails the type check —
// and a STALE scopes.gen.ts type-checks perfectly green. Regenerate, then type
// check, then bundle.
//
// Reads the project's specs/design/security.json and writes src/scopes.gen.ts.
// This is the ONLY place the design document reaches the bundle: gate routes,
// nav items and copy from these exports rather than retyping a handle, a role
// name or a screen name anywhere else.
//
// Zero dependencies on purpose — it must run before `npm install` has resolved
// anything, and inside an image build that has no network.
//
// Run `node scripts/gen-scopes.mjs --help` for the usage; the exit codes are
// 0 written (or a committed output deliberately kept), 1 the design could not
// be read or is not a version-2 document, 2 the command line is wrong.

import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const APP_ROOT = resolve(dirname(fileURLToPath(import.meta.url)), "..");

const USAGE = `gen-scopes — generate src/scopes.gen.ts from specs/design/security.json

Usage:
  node scripts/gen-scopes.mjs [--component <name>] [--out <path>] [--spec <path>]
  node scripts/gen-scopes.mjs --help

  --component <name>  emit only this component's screens. Omitted, every
                      screen in the document is emitted.
  --out <path>        where to write, resolved against the CURRENT DIRECTORY.
                      Default: <app root>/src/scopes.gen.ts.
  --spec <path>       the security.json to read. Default: walk up from the app
                      folder looking for specs/design/security.json.
  -h, --help          print this and exit without writing anything.

Environment:
  AEP_APP_COMPONENT   same as --component
  AEP_SECURITY_JSON   same as --spec (skips the walk-up)

Exit: 0 written (or a committed output deliberately kept), 1 the design could
not be read or is not version 2, 2 this command line is wrong.`;

/**
 * A wrong command line NEVER writes. `--componenet <name>` silently ignored is
 * a file emitted with every component's screens in it, exit 0 — a working
 * build of the wrong app.
 */
function usageError(message) {
  console.error(`gen-scopes: ${message}\n\n${USAGE}`);
  process.exit(2);
}

const VALUE_FLAGS = new Set(["--component", "--out", "--spec"]);
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

// `||`, not `??`: a wrapper that exports these as EMPTY strings means "unset".
const component = options.component || process.env.AEP_APP_COMPONENT || "";
// The default is the app's own src/; an explicit --out is a path the caller
// typed at a shell prompt, so it resolves the way every other tool resolves one.
const outPath = options.out
  ? resolve(process.cwd(), options.out)
  : resolve(APP_ROOT, "src/scopes.gen.ts");

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
// keeping a stale committed output because `--spec ../spec/security.json` was
// misspelled hides the design change the caller asked to regenerate from.
if (namedSpec && !existsSync(namedSpec)) {
  console.error(
    `gen-scopes: ${namedSpec} was named explicitly (--spec / AEP_SECURITY_JSON) ` +
      `and does not exist. Fix the path, or drop the flag to walk up for ` +
      `${SPEC_RELATIVE}.`,
  );
  process.exit(1);
}

// A per-component image build's context is the APP FOLDER ALONE, so the
// project's specs/ tree is out of reach inside `docker build` — there is no
// ancestor that holds it. src/scopes.gen.ts is COMMITTED for exactly this
// reason: when the catalog cannot be read and a previous output exists, keep it
// and exit 0. Failing here would fail every image build the platform runs.
if (!specPath) {
  const where = `${SPEC_RELATIVE} (searched ${APP_ROOT} and every parent)`;
  if (existsSync(outPath)) {
    console.log(
      `gen-scopes: ${where} is out of reach (per-component build context); ` +
        `keeping the committed src/scopes.gen.ts`,
    );
    process.exit(0);
  }
  console.error(
    `gen-scopes: ${where} not found, and there is no committed src/scopes.gen.ts ` +
      `to fall back on. Run this from the project checkout once and COMMIT the ` +
      `generated file — the image build cannot see the specs tree.`,
  );
  process.exit(1);
}

let doc;
try {
  doc = JSON.parse(readFileSync(specPath, "utf8"));
} catch (err) {
  console.error(`gen-scopes: cannot read ${specPath}: ${err.message}`);
  process.exit(1);
}

// REFUSE anything that is not a version-2 document, rather than reading zero
// permissions and zero screens out of it. `Scope = never` and `SCREENS = []`
// type-check green and deploy an app in which every caller lands on NoAccess —
// a v1 file, a null, or a JSON array all produce exactly that, silently, over
// a correct committed output.
const SHAPE = ["permissions", "roles", "screens"];
if (typeof doc !== "object" || doc === null || Array.isArray(doc)) {
  console.error(
    `gen-scopes: ${specPath} is not a security.json document — expected a JSON ` +
      `object with "version": 2, read ${doc === null ? "null" : Array.isArray(doc) ? "an array" : typeof doc}.`,
  );
  process.exit(1);
}
if (doc.version !== 2) {
  console.error(
    `gen-scopes: ${specPath} declares version ${JSON.stringify(doc.version ?? null)}. ` +
      `security.json v1 is not accepted: version 2 declares the permission ` +
      `catalog in permissions[] (resource, component, actions[{handle, ownership}]), ` +
      `roles grant handles from it in roles[].grants, and screens[] maps each ` +
      `wireframe screen to one handle. Re-author it against version 2.`,
  );
  process.exit(1);
}
for (const field of SHAPE) {
  if (!Array.isArray(doc[field])) {
    console.error(
      `gen-scopes: ${specPath} has no ${field}[] array — a version-2 document ` +
        `declares all of ${SHAPE.join(", ")}.`,
    );
    process.exit(1);
  }
}

// Every catalog handle is `<resource>:<action.handle>`. EVERY one is emitted,
// including handles no operation and no screen requires — the union is the
// catalog, not the subset this app happens to use today.
const handles = [];
for (const permission of doc.permissions) {
  for (const action of permission.actions ?? []) {
    handles.push(`${permission.resource}:${action.handle}`);
  }
}
handles.sort();
const duplicates = handles.filter((h, i) => i > 0 && h === handles[i - 1]);
if (duplicates.length > 0) {
  console.error(`gen-scopes: duplicate handles in the catalog: ${[...new Set(duplicates)].join(", ")}`);
  process.exit(1);
}

// Only user-kind roles reach a browser: a service-kind role has no login, so it
// can never be a role the badge shows or NoAccess tells somebody to ask for.
const roles = doc.roles
  .filter((role) => (role.kind ?? "user") === "user")
  .sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));

// Screens keep their DECLARED order, and only this one is not sorted. The order
// is meaningful: it is the nav rail's order and its first reachable row is the
// landing screen. Sorting it would silently rearrange the app.
const declaredScreens = doc.screens;
const screens = declaredScreens.filter(
  (screen) => component === "" || screen.component === component,
);

// A component name that matches NOTHING is a typo — and its output is an app
// with no screens at all, which renders NoAccess to everybody and type-checks
// green. The document knows which names are real; say them.
if (component !== "" && declaredScreens.length > 0 && screens.length === 0) {
  const declared = [...new Set(declaredScreens.map((screen) => screen.component))].sort();
  console.error(
    `gen-scopes: no screen in ${specPath} is declared for component ` +
      `"${component}". The document declares screens for: ${declared.join(", ")}.`,
  );
  process.exit(1);
}

const q = (value) => JSON.stringify(value);
const sorted = (values) => [...new Set(values ?? [])].sort();
const union = (values) => (values.length ? values.map(q).join("\n  | ") : "never");
const list = (values) => `[${values.map(q).join(", ")}]`;

const roleNames = roles.map((role) => role.name);
const roleRow = (role, key) => `  ${q(role.name)}: ${list(sorted(role[key]))},`;

const screenRows = screens.map(
  (screen) =>
    `  { component: ${q(screen.component)}, screen: ${q(screen.screen)}, requires: ${
      screen.requires === null || screen.requires === undefined ? "null" : q(screen.requires)
    } },`,
);
// `[]` rather than a `[` and `]` with a blank line between them: an empty table
// is a real outcome (a component the design gives no screen) and it should read
// like one.
const screenTable = screenRows.length ? `[\n${screenRows.join("\n")}\n]` : "[]";

const out = `// GENERATED by scripts/gen-scopes.mjs from specs/design/security.json.
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

export interface ScreenGate {
  readonly component: string;
  /** The screen name as security.json spells it, e.g. "My Claims". */
  readonly screen: string;
  /** A handle, null for any signed-in caller, "public" for before sign-in. */
  readonly requires: Scope | null | "public";
}

/**
 * The screen table, in DECLARED order — the nav order, whose first reachable
 * row is the landing screen. Gate routes from this, never from a handle typed
 * into JSX.
 */
export const SCREENS: readonly ScreenGate[] = ${screenTable};

export function isScope(value: string): value is Scope {
  return (SCOPES as readonly string[]).includes(value);
}
`;

mkdirSync(dirname(outPath), { recursive: true });
writeFileSync(outPath, out);
console.log(
  `gen-scopes: ${handles.length} scopes, ${roleNames.length} roles, ` +
    `${screens.length} screens${component ? ` for ${component}` : ""} -> ${outPath}`,
);
