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
 * The referential rules of `specs/design/security.json` v2 — everything a
 * standalone JSON Schema cannot express, because it is a statement ABOUT two
 * places in the document, or about the document and a sibling spec file.
 *
 * Three kinds of rule live here, in the order they are checked:
 *
 *  1. **Within the document.** A grant names a catalog handle; a test user
 *     names a declared, admin-enrolment user role; a role name is not a group
 *     name. These need nothing but the parsed document, so they run on every
 *     write of security.json.
 *  2. **Against a sibling the bundle may hold.** A permission's component and a
 *     screen's component are nodes of `design.cell`; a screen name exists in
 *     that component's `wireframes.dsl`. The design lineup writes security.json
 *     before some of those files exist, so **a rule whose input is missing is
 *     skipped in silence** — the build gate re-runs the whole list with the
 *     full tag, where every file is present by construction.
 *  3. **The reachability cross-check** (plan §1.2, Δ P6 §5): for every role,
 *     every operation behind every screen that role can reach must be granted
 *     by that role. This is the rule the design's own Expense Tracker example
 *     fails — `Approvals` is gated on `claims:approve`, the list it renders is
 *     `GET /claims` (`claims:read`), and `Approver` granted only
 *     `claims:read-all`. On the pinned gateway that is a bare 401 with no
 *     `WWW-Authenticate`, which the SPA cannot tell from an expired session:
 *     the observed symptom was an infinite sign-in loop. Caught here it is one
 *     sentence at authoring time.
 *
 * **Nothing implies anything.** The gateway compares scopes whole-string
 * (P2-live-2 b2), so `claims:read-all` does not admit a caller to an operation
 * guarded on `claims:read`. Rather than widen grants silently — which would
 * make the gate disagree with the runtime — a role holding `X:read-all`
 * without `X:read` is an error naming both handles (decision B1).
 *
 * Findings carry a severity. Errors refuse the write; warnings are the
 * Security page's "declared, used nowhere" / "unreachable by any role" lines;
 * the one info finding records that an `assignTo` group is resolved against the
 * org directory rather than this document (a reused org group is deliberately
 * not redeclared in `groups[]`).
 */

import { compileWireframes } from "@aep/excalidraw-dsl";
import { parse as parseYaml } from "yaml";

import type { SecurityDesign } from "./contracts/security-design.js";
import { cellNodeIds, DESIGN_CELL_PATH } from "./design-diagrams.js";
import { catalogHandles } from "./security-design-catalog.js";
import {
  securityMessage,
  type MessageParams,
  type SecurityMessageKey,
} from "./security-design-messages.js";

/** Matches `TEST_USERNAME_RE` in `./security-design-schema.ts`; duplicated here to keep that module's exports the schema's. */
const TEST_USERNAME = /^[a-z0-9][a-z0-9._-]*$/;

/**
 * The characters a role name may be made of: letters, digits, spaces, "-", "_"
 * and ".".
 *
 * It is a denylist expressed as an allowlist, and what it keeps out is what
 * cannot be escaped downstream: "/" is the separator in the directory name
 * `<project>/<Role>` that makes a role ownable, and "|", backticks and line
 * breaks would break the markdown table cell the build ticket publishes the
 * role in and that the validation agent parses. The Go copy is
 * `validRoleName` in `internal/platform/securityspec/references.go`.
 */
const ROLE_NAME = /^[\p{L}\p{N} ._-]+$/u;

/** `screens[].requires` for a screen shown before sign-in. */
const PUBLIC = "public";

/** How loudly a finding speaks. Only `error` refuses a write. */
export type SecurityFindingSeverity = "error" | "warning" | "info";

/** One thing the referential gate has to say about a document. */
export interface SecurityReferenceFinding {
  severity: SecurityFindingSeverity;
  /** The message-catalog key — the stable cross-language name of this rule. */
  key: SecurityMessageKey;
  /** The slots the template was rendered with, for a caller that re-renders. */
  params: Readonly<Record<string, string>>;
  /** The rendered sentence. */
  message: string;
}

/**
 * The sibling files the rules read. `FileBundle` satisfies it, and so does any
 * map of the tag's spec tree — which is how the build gate will call the same
 * rules.
 */
export interface SecurityReferenceContext {
  read(path: string): string | undefined;
}

// -------------------------------------------------------------------------
// Sibling spec files
// -------------------------------------------------------------------------

function componentDir(component: string): string {
  return `specs/design/components/${component}`;
}

/** A component's spec, `.yaml` or `.yml`, or undefined when the bundle has neither. */
function readOpenapi(ctx: SecurityReferenceContext, component: string): string | undefined {
  return (
    ctx.read(`${componentDir(component)}/openapi.yaml`) ??
    ctx.read(`${componentDir(component)}/openapi.yml`)
  );
}

function readWireframes(ctx: SecurityReferenceContext, component: string): string | undefined {
  return ctx.read(`${componentDir(component)}/wireframes.dsl`);
}

/**
 * The component ids a `design.cell` declares, or null when the cell is not in
 * the bundle.
 *
 * The ids come from `cellNodeIds` — the cell grammar's own reader — rather than
 * a regex over the lines: it skips the platform's `---` frontmatter and keeps a
 * quoted run one token, so `component "My API" service` is `My API` here and in
 * every other gate that resolves a cell node.
 */
function cellComponents(ctx: SecurityReferenceContext): Set<string> | null {
  const source = ctx.read(DESIGN_CELL_PATH);
  if (source === undefined || source.trim() === "") return null;
  return new Set(cellNodeIds(source).components);
}

/**
 * A screen name as both sides may spell it. `security.json` writes the label a
 * person reads ("My Claims"); the wireframe grammar is `/^screen\s+([\w-]+)/`,
 * so the same screen is `MyClaims` there. Neither file format changes: the
 * comparison drops everything that is not a letter or a digit and lowercases
 * the rest (plan §1.2, Δ P6 §7.34).
 */
export function normalizeScreenName(name: string): string {
  return name.replace(/[^a-zA-Z0-9]/g, "").toLowerCase();
}

/** The screen names a wireframes.dsl declares, normalized. Null when it does not compile. */
function wireframeScreens(dsl: string): Set<string> | null {
  const compiled = compileWireframes(dsl, null);
  if (!compiled.ok) return null;
  return new Set(compiled.screenOrder.map(normalizeScreenName));
}

// -------------------------------------------------------------------------
// openapi.yaml → operations
// -------------------------------------------------------------------------

const HTTP_METHODS = ["get", "put", "post", "delete", "options", "head", "patch", "trace"] as const;

/** The methods that only READ. A screen cannot render without them. */
const SAFE_METHODS = new Set<string>(["get", "head"]);

/** One operation of one component spec, reduced to what authorization cares about. */
interface SpecOperation {
  method: string;
  path: string;
  /** The single scope handle the operation requires, or null for "any signed-in caller". */
  scope: string | null;
  /** True when `security: []` — no token at all. */
  isPublic: boolean;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

/**
 * The one scope an OpenAPI `security` value names, in the three shapes the
 * design allows: absent (inherit), `[]` (public), or one requirement object
 * with one scope. Anything else is `openapi.yaml`'s own gate's business
 * (`checkOpenapiSpec`), and is read here as leniently as possible so that a
 * malformed spec produces ONE message from ONE gate.
 */
function securityOf(value: unknown): { scope: string | null; isPublic: boolean } | null {
  if (!Array.isArray(value)) return null;
  if (value.length === 0) return { scope: null, isPublic: true };
  const first = asRecord(value[0]);
  if (!first) return null;
  const scopes = Object.values(first)[0];
  if (!Array.isArray(scopes) || scopes.length === 0) return { scope: null, isPublic: false };
  const scope = scopes[0];
  return { scope: typeof scope === "string" ? scope : null, isPublic: false };
}

/** Every operation a component spec declares, with its effective security. */
function specOperations(source: string): SpecOperation[] | null {
  let doc: unknown;
  try {
    doc = parseYaml(source);
  } catch {
    return null;
  }
  const root = asRecord(doc);
  if (!root) return null;
  const paths = asRecord(root["paths"]);
  if (!paths) return null;
  const documentDefault = securityOf(root["security"]) ?? { scope: null, isPublic: false };

  const out: SpecOperation[] = [];
  for (const [path, item] of Object.entries(paths)) {
    const pathItem = asRecord(item);
    if (!pathItem) continue;
    for (const method of HTTP_METHODS) {
      const operation = asRecord(pathItem[method]);
      if (!operation) continue;
      const own = "security" in operation ? securityOf(operation["security"]) : null;
      const effective = own ?? documentDefault;
      out.push({ method, path, scope: effective.scope, isPublic: effective.isPublic });
    }
  }
  return out;
}

// -------------------------------------------------------------------------
// The rules
// -------------------------------------------------------------------------

class Findings {
  readonly all: SecurityReferenceFinding[] = [];

  add(severity: SecurityFindingSeverity, key: SecurityMessageKey, params: MessageParams = {}): void {
    const flat: Record<string, string> = {};
    for (const [k, v] of Object.entries(params)) flat[k] = String(v);
    this.all.push({ severity, key, params: flat, message: securityMessage(key, params) });
  }
}

/** The resource half of a `<resource>:<action>` handle. */
function resourceOf(handle: string): string {
  return handle.split(":")[0] ?? handle;
}

/** True when `handle` is `<resource>:read-all`. */
function isReadAll(handle: string): boolean {
  return handle.endsWith(":read-all");
}

/** A role's enrolment and kind, with the schema's defaults applied. */
function enrolmentOf(role: SecurityDesign["roles"][number]): "admin" | "self-service" {
  return role.enrolment ?? "admin";
}
function kindOf(role: SecurityDesign["roles"][number]): "user" | "service" {
  return role.kind ?? "user";
}

/**
 * Every referential finding for `doc`, in rule order. `ctx` is optional: with
 * no bundle, only the rules that read the document alone run, and the rest are
 * skipped in silence.
 *
 * Exported so the console's Security page and the build gate can show the
 * warnings, which `checkSecurityReferences` (first error only) drops.
 */
export function securityReferenceFindings(
  doc: SecurityDesign,
  ctx?: SecurityReferenceContext,
): SecurityReferenceFinding[] {
  const found = new Findings();
  const catalog = catalogHandles(doc);

  // --- the catalog ---------------------------------------------------------
  const seenResources = new Set<string>();
  /** resource → the component that owns it. */
  const ownerOf = new Map<string, string>();
  for (const permission of doc.permissions) {
    if (seenResources.has(permission.resource)) {
      found.add("error", "duplicate_resource", { resource: permission.resource });
      continue;
    }
    seenResources.add(permission.resource);
    ownerOf.set(permission.resource, permission.component);
    const seenActions = new Set<string>();
    for (const action of permission.actions) {
      if (seenActions.has(action.handle)) {
        found.add("error", "duplicate_action", {
          resource: permission.resource,
          handle: action.handle,
        });
        continue;
      }
      seenActions.add(action.handle);
    }
  }

  const cellIds = ctx ? cellComponents(ctx) : null;
  if (cellIds) {
    for (const permission of doc.permissions) {
      if (!cellIds.has(permission.component)) {
        found.add("error", "resource_component_unknown", {
          resource: permission.resource,
          component: permission.component,
        });
      }
    }
  }

  // --- roles ---------------------------------------------------------------
  const groupNames = new Set(doc.groups.map((g) => g.name));
  const groupNamesLower = new Set(doc.groups.map((g) => g.name.toLowerCase()));
  const declaredRoles = new Set<string>();
  for (const role of doc.roles) {
    const key = role.name.toLowerCase();
    if (declaredRoles.has(key)) {
      found.add("error", "duplicate_role_name", { role: role.name });
      continue;
    }
    declaredRoles.add(key);
    if (role.name !== role.name.trim()) {
      found.add("error", "role_name_whitespace", { role: role.name });
    }
    // The charset. A role name travels into two places that cannot escape it:
    // the directory, where it becomes "<project>/<Role>" and the "/" is the
    // platform's own ownership separator, and the build ticket's markdown
    // table, where a "|" or a line break would break the row the validation
    // agent parses. Everything a PRD actor noun needs is still allowed.
    if (!ROLE_NAME.test(role.name.trim())) {
      found.add("error", "role_name_invalid", { role: role.name });
    }
    // A reused org group is NOT redeclared in groups[], so this rule can only
    // judge the names THIS document introduces — the directory check at the
    // build gate is what catches a collision with a group somebody else made.
    if (groupNamesLower.has(key)) {
      found.add("error", "role_name_is_group_name", { role: role.name });
    }
    for (const handle of role.grants) {
      if (!catalog.has(handle)) {
        found.add("error", "grant_unknown_handle", { role: role.name, handle });
      }
    }
    // Decision B1: the "all" handle widens the rows, it does not replace the
    // operation, and scope matching is a whole-string compare.
    const grants = new Set(role.grants);
    for (const handle of role.grants) {
      if (!isReadAll(handle)) continue;
      const readHandle = `${resourceOf(handle)}:read`;
      if (!catalog.has(readHandle) || grants.has(readHandle)) continue;
      found.add("error", "read_all_without_read", {
        role: role.name,
        allHandle: handle,
        readHandle,
      });
    }
  }

  for (const role of doc.roles) {
    for (const ref of role.assignableBy ?? []) {
      if (!declaredRoles.has(ref.toLowerCase())) {
        found.add("error", "assignable_by_unknown_role", { role: role.name, ref });
      }
    }
    const enrolment = enrolmentOf(role);
    const kind = kindOf(role);
    const assignTo = role.assignTo ?? [];
    if (kind === "service" && assignTo.length > 0) {
      found.add("error", "non_admin_role_has_assign_to", { role: role.name, kind: "service" });
    } else if (kind === "user" && enrolment === "self-service" && assignTo.length > 0) {
      found.add("error", "non_admin_role_has_assign_to", {
        role: role.name,
        kind: "self-service",
      });
    } else if (kind === "user" && enrolment === "admin" && assignTo.length === 0) {
      found.add("error", "admin_role_needs_assign_to", { role: role.name });
    }
    for (const group of assignTo) {
      if (!groupNames.has(group)) {
        found.add("info", "assign_to_directory_checked", { role: role.name, group });
      }
    }
  }

  // --- test users ----------------------------------------------------------
  const roleByLowerName = new Map(doc.roles.map((r) => [r.name.toLowerCase(), r] as const));
  const seenUsers = new Set<string>();
  for (const user of doc.testUsers) {
    if (!TEST_USERNAME.test(user.username)) {
      found.add("error", "invalid_test_username", { username: user.username });
      continue;
    }
    if (seenUsers.has(user.username)) {
      found.add("error", "duplicate_test_user", { username: user.username });
      continue;
    }
    seenUsers.add(user.username);
    for (const roleName of user.roles) {
      const role = roleByLowerName.get(roleName.toLowerCase());
      if (!role) {
        found.add("error", "test_user_unknown_role", { username: user.username, role: roleName });
        continue;
      }
      if (kindOf(role) !== "user" || enrolmentOf(role) !== "admin") {
        found.add("error", "test_user_role_not_user_kind", {
          username: user.username,
          role: roleName,
        });
      }
    }
  }

  // --- screens -------------------------------------------------------------
  for (const screen of doc.screens) {
    if (cellIds && !cellIds.has(screen.component)) {
      found.add("error", "screen_component_unknown", {
        component: screen.component,
        screen: screen.screen,
      });
      continue;
    }
    if (screen.requires !== null && screen.requires !== PUBLIC && !catalog.has(screen.requires)) {
      found.add("error", "screen_requires_unknown_handle", {
        component: screen.component,
        screen: screen.screen,
        handle: screen.requires,
      });
    }
    if (!ctx) continue;
    const dsl = readWireframes(ctx, screen.component);
    if (dsl === undefined) continue;
    const declared = wireframeScreens(dsl);
    if (declared === null) continue;
    if (!declared.has(normalizeScreenName(screen.screen))) {
      found.add("error", "screen_unknown", {
        component: screen.component,
        screen: screen.screen,
      });
    }
  }

  if (ctx) reachabilityRules(doc, ctx, catalog, ownerOf, found);
  return found.all;
}

/**
 * The rules that need the component specs: the screen→operation cross-check and
 * the two catalog-coverage warnings.
 *
 * **How a screen's operations are derived.** The wireframes DSL has no
 * screen→operation binding — its grammar is screens, elements and flows — so
 * the operation set is derived through the CATALOG instead, from two facts the
 * document does state:
 *
 *  - a screen gated on `<resource>:<action>` RENDERS that resource, and
 *  - the component that owns the resource declares the operations on it.
 *
 * So the operation a screen cannot avoid calling is the resource's own read:
 * the SAFE (`GET`/`HEAD`) operation guarded on `<resource>:read` in the owning
 * component's spec. If the role that reaches the screen cannot call that, the
 * page loads into a 401 — exactly the class the design's example broke on.
 *
 * The rule deliberately stops there rather than demanding every scoped
 * operation of the resource: a `GET /reports/export` guarded on
 * `reports:export` sits behind a button the page can hide, while the list GET
 * runs on load, so requiring the whole set would refuse a correct document (an
 * Approver who may read reports but not export them). A screen requiring `null`
 * (any signed-in caller) names no resource and so has no derivable operation
 * set; a `"public"` screen is outside authorization entirely. Both are skipped,
 * and a per-screen binding — if the DSL ever grows one — replaces this
 * derivation without moving the rule.
 */
function reachabilityRules(
  doc: SecurityDesign,
  ctx: SecurityReferenceContext,
  catalog: Set<string>,
  ownerOf: Map<string, string>,
  found: Findings,
): void {
  // Owner components only: a resource is declared by the component that owns
  // it, so those are the specs the catalog can be judged against.
  const operationsByComponent = new Map<string, SpecOperation[]>();
  for (const component of new Set(ownerOf.values())) {
    const source = readOpenapi(ctx, component);
    if (source === undefined) continue;
    const operations = specOperations(source);
    if (operations === null) continue;
    operationsByComponent.set(component, operations);
  }
  if (operationsByComponent.size === 0) return;

  // --- the reachability cross-check (plan §1.2, Δ P6 §5) -------------------
  for (const screen of doc.screens) {
    const requires = screen.requires;
    if (requires === null || requires === PUBLIC || !catalog.has(requires)) continue;
    const resource = resourceOf(requires);
    const owner = ownerOf.get(resource);
    if (!owner) continue;
    const operations = operationsByComponent.get(owner);
    if (!operations) continue;

    // The one operation the page cannot avoid: the resource's read, if some
    // safe operation of the owner is guarded on it.
    const readHandle = `${resource}:read`;
    const loads = operations.some(
      (o) => !o.isPublic && o.scope === readHandle && SAFE_METHODS.has(o.method),
    );
    if (!loads) continue;

    for (const role of doc.roles) {
      // A service role holds an app principal's token; it reaches no screen.
      if (kindOf(role) !== "user") continue;
      if (!role.grants.includes(requires)) continue;
      if (role.grants.includes(readHandle)) continue;
      found.add("error", "screen_operation_not_granted", {
        role: role.name,
        screen: screen.screen,
        handle: readHandle,
      });
    }
  }

  // --- the two coverage warnings ------------------------------------------
  const requiredByOperations = new Set<string>();
  for (const operations of operationsByComponent.values()) {
    for (const operation of operations) {
      if (operation.scope !== null) requiredByOperations.add(operation.scope);
    }
  }
  const requiredByScreens = new Set<string>();
  for (const screen of doc.screens) {
    if (screen.requires !== null && screen.requires !== PUBLIC) requiredByScreens.add(screen.requires);
  }
  const granted = new Set<string>();
  for (const role of doc.roles) for (const handle of role.grants) granted.add(handle);

  for (const handle of catalog) {
    // `X:read-all` is a ROW widener, not an operation guard: the design's own
    // example puts it on no operation and no screen, and the service reads it
    // off the token while serving `X:read`. Warning about it every time would
    // make this line noise, so it only fires when even `X:read` is unused.
    const widensAUsedRead =
      isReadAll(handle) &&
      (requiredByOperations.has(`${resourceOf(handle)}:read`) ||
        requiredByScreens.has(`${resourceOf(handle)}:read`));
    if (!requiredByOperations.has(handle) && !requiredByScreens.has(handle) && !widensAUsedRead) {
      found.add("warning", "handle_used_nowhere", { handle });
    }
    if (requiredByOperations.has(handle) && !granted.has(handle)) {
      found.add("warning", "handle_unreachable", { handle });
    }
  }
}

/**
 * The first blocking violation, phrased for the model's self-correction, or
 * null when the document has none.
 *
 * The write-gate wants one sentence: the model fixes one thing per round trip,
 * and a list of twenty would be spent re-reading. Callers that want the whole
 * picture (the console's Security page, the build gate's report) call
 * `securityReferenceFindings`.
 */
export function checkSecurityReferences(
  doc: SecurityDesign,
  ctx?: SecurityReferenceContext,
): string | null {
  for (const finding of securityReferenceFindings(doc, ctx)) {
    if (finding.severity === "error") return finding.message;
  }
  return null;
}
