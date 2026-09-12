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
 * The SECURITY half of a component's `openapi.yaml` write-gate.
 *
 * `openapi-spec.ts` judges a document on its own (3.x, has paths, has
 * operations). This module judges it against the two files that give its
 * security block meaning:
 *
 *  - the component's `design.json` — whether it depends on the sign-in client
 *    at all. A component is protected iff it declares the `thunder-app`
 *    platform resource; there is no `publicComponents` list any more.
 *  - the project's `specs/design/security.json` — the permission catalog.
 *    Operations REFERENCE handles; they never define them, and a handle whose
 *    resource another component owns is that component's to enforce.
 *
 * Why a write-gate and not a build-time check: everything here is a silent
 * failure downstream. A scope that is not in the catalog is a scope the
 * identity provider will never put on a token, so every call 401s at the
 * gateway and the SPA restarts sign-in — the observed infinite sign-in loop.
 * An OIDC scope emitted as an API scope (`openid`, `profile`, `email`,
 * `group`, `ou`) is worse: the operation looks guarded and admits everyone,
 * and nothing anywhere reports it. `X-User-Id` declared `required: true` makes
 * the generated server answer 400 from the parameter binder before the
 * authentication middleware ever runs. None of these are visible in the
 * document itself, which is exactly why the model needs them at write time.
 *
 * Message text lives in `./openapi-security-messages.ts`, one flat
 * key → template map published as JSON beside it, because the BFF's
 * `securityspec` applies the same rules to the same documents and the two
 * sides must not drift into two wordings of one rule.
 */

import type { DiagramBundleReader } from "./design-diagrams.js";
import { OIDC_RESERVED_SCOPES } from "./security-design-catalog.js";
import { openapiSecurityMessage as say } from "./openapi-security-messages.js";

/** A component's own spec: `specs/design/components/<name>/openapi.yaml`. */
const COMPONENT_SPEC_RE = /^specs\/design\/components\/([^/]+)\/openapi\.ya?ml$/;

/**
 * The platform resource type that IS sign-in. A component depending on it gets
 * an OAuth client, an issuer and a JWKS URL wired into its environment, and its
 * operations are rendered onto the gateway with a jwt-auth policy; a component
 * that does not is served with no policy at all.
 */
const SIGN_IN_RESOURCE_TYPE = "thunder-app";

/** The one authored permission catalog (`SECURITY_DESIGN_JSON_RE` matches it). */
const SECURITY_DESIGN_PATH = "specs/design/security.json";

/** The scheme name a generated component declares — the only one. */
const SCHEME = "oauth2";

/** Operation keys under a path item (OpenAPI 3.x). */
const HTTP_METHODS = new Set(["get", "put", "post", "delete", "options", "head", "patch", "trace"]);

const RESERVED = new Set(OIDC_RESERVED_SCOPES);

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

// -------------------------------------------------------------------------
// Inputs: the sign-in dependency, and the catalog
// -------------------------------------------------------------------------

/**
 * Whether `design.json` declares the sign-in client, or null when the file is
 * not in the bundle (or is not a JSON object). Null means "unknowable", and an
 * unknowable premise is not a rejection: the design lineup writes `design.json`
 * before `openapi.yaml`, so the only way here is an out-of-order write, and
 * refusing it would be refusing a file for a fact about ANOTHER file.
 */
function dependsOnSignIn(design: string | undefined): boolean | null {
  if (design === undefined) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(design);
  } catch {
    return null;
  }
  const root = asRecord(parsed);
  if (!root) return null;
  const deps = root["dependencies"];
  if (!Array.isArray(deps)) return false;
  return deps.some((d) => {
    const dep = asRecord(d);
    return (
      dep !== null &&
      dep["kind"] === "platform-resource" &&
      dep["resourceType"] === SIGN_IN_RESOURCE_TYPE
    );
  });
}

/**
 * `<resource>:<action>` → the component the catalog says owns that resource,
 * or null when `security.json` is not in the bundle yet (or does not parse).
 * Deliberately lenient: the document has its own schema gate, which ran when it
 * was written; re-judging it here would reject an `openapi.yaml` for a defect
 * in a different file.
 */
function catalogOwners(security: string | undefined): Map<string, string> | null {
  if (security === undefined) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(security);
  } catch {
    return null;
  }
  const root = asRecord(parsed);
  if (!root || !Array.isArray(root["permissions"])) return null;
  const owners = new Map<string, string>();
  for (const entry of root["permissions"]) {
    const permission = asRecord(entry);
    if (!permission) continue;
    const { resource, component, actions } = permission;
    if (typeof resource !== "string" || typeof component !== "string") continue;
    if (!Array.isArray(actions)) continue;
    for (const a of actions) {
      const action = asRecord(a);
      const handle = action?.["handle"];
      if (typeof handle === "string") owners.set(`${resource}:${handle}`, component);
    }
  }
  return owners;
}

// -------------------------------------------------------------------------
// Reading the document
// -------------------------------------------------------------------------

interface Operation {
  /** Uppercased, as the message and the gateway spell it. */
  method: string;
  path: string;
  /** The operation object. */
  op: Record<string, unknown>;
  /** Path-item parameters plus the operation's own, `$ref`s resolved. */
  parameters: Record<string, unknown>[];
}

/** Resolve a local `#/components/parameters/<name>` reference; anything else stays unresolved. */
function resolveParameter(
  value: unknown,
  root: Record<string, unknown>,
): Record<string, unknown> | null {
  const param = asRecord(value);
  if (!param) return null;
  const ref = param["$ref"];
  if (typeof ref !== "string") return param;
  const prefix = "#/components/parameters/";
  if (!ref.startsWith(prefix)) return null;
  const components = asRecord(root["components"]);
  const declared = asRecord(components?.["parameters"]);
  return asRecord(declared?.[ref.slice(prefix.length)]);
}

function parameterList(value: unknown, root: Record<string, unknown>): Record<string, unknown>[] {
  if (!Array.isArray(value)) return [];
  const out: Record<string, unknown>[] = [];
  for (const entry of value) {
    const param = resolveParameter(entry, root);
    if (param) out.push(param);
  }
  return out;
}

/** Every operation the document declares, in document order. */
function operations(root: Record<string, unknown>): Operation[] {
  const paths = asRecord(root["paths"]);
  if (!paths) return [];
  const out: Operation[] = [];
  for (const [path, item] of Object.entries(paths)) {
    const pathItem = asRecord(item);
    if (!pathItem) continue;
    const shared = parameterList(pathItem["parameters"], root);
    for (const [key, value] of Object.entries(pathItem)) {
      if (!HTTP_METHODS.has(key.toLowerCase())) continue;
      const op = asRecord(value);
      if (!op) continue;
      out.push({
        method: key.toUpperCase(),
        path,
        op,
        parameters: [...shared, ...parameterList(op["parameters"], root)],
      });
    }
  }
  return out;
}

/** The `X-User-*` header parameters an operation carries, by the name it declares. */
function identityHeaders(op: Operation): Record<string, unknown>[] {
  return op.parameters.filter((p) => {
    const name = p["name"];
    return (
      p["in"] === "header" && typeof name === "string" && name.toLowerCase().startsWith("x-user-")
    );
  });
}

/** The scopes of `securitySchemes.oauth2`'s flows, each with the flow that advertises it. */
function flowScopes(root: Record<string, unknown>): { scope: string; where: string }[] {
  const components = asRecord(root["components"]);
  const scheme = asRecord(asRecord(components?.["securitySchemes"])?.[SCHEME]);
  const flows = asRecord(scheme?.["flows"]);
  if (!flows) return [];
  const out: { scope: string; where: string }[] = [];
  for (const [flow, value] of Object.entries(flows)) {
    const scopes = asRecord(asRecord(value)?.["scopes"]);
    if (!scopes) continue;
    for (const scope of Object.keys(scopes)) {
      out.push({ scope, where: `components.securitySchemes.oauth2.flows.${flow}.scopes` });
    }
  }
  return out;
}

/**
 * True for the document default the design fixes: exactly
 * `security: [ { oauth2: [] } ]`.
 *
 * The scope list must be EMPTY, not merely a list. A default naming a scope
 * reads as "everything needs this permission", but nothing enforces it: an
 * operation that declares no security of its own is projected onto the gateway
 * as plain signed-in, so the named scope is silently dropped and the operation
 * ships open to any signed-in caller.
 */
function isDocumentDefault(value: unknown): boolean {
  if (!Array.isArray(value) || value.length !== 1) return false;
  const requirement = asRecord(value[0]);
  if (!requirement) return false;
  const keys = Object.keys(requirement);
  if (keys.length !== 1 || keys[0] !== SCHEME) return false;
  const scopes = requirement[SCHEME];
  return Array.isArray(scopes) && scopes.length === 0;
}

// -------------------------------------------------------------------------
// The gate
// -------------------------------------------------------------------------

/**
 * Judge the security of a parsed component `openapi.yaml`. Returns the first
 * violation as a finished, punctuated message (the caller adds the path and the
 * remedy), or null when the document is sound — or when the premise cannot be
 * read (`design.json` absent).
 *
 * `security.json` absent narrows the check rather than skipping it: the
 * structural rules (one requirement object, one scope, the scheme, the document
 * default, the identity headers) all still run, and only the catalog membership
 * and ownership of a handle wait for the file that defines them.
 */
export function checkOpenapiSecurity(
  path: string,
  root: Record<string, unknown>,
  bundle: DiagramBundleReader,
): string | null {
  const match = COMPONENT_SPEC_RE.exec(path);
  if (!match) return null;
  const component = match[1] as string;

  const protectedComponent = dependsOnSignIn(
    bundle.read(`specs/design/components/${component}/design.json`),
  );
  if (protectedComponent === null) return null;

  const owners = catalogOwners(bundle.read(SECURITY_DESIGN_PATH));
  const ops = operations(root);

  return protectedComponent
    ? checkProtected(component, root, ops, owners)
    : checkUnprotected(component, root, ops);
}

/** A component with no sign-in dependency declares no scheme and no security, anywhere. */
function checkUnprotected(
  component: string,
  root: Record<string, unknown>,
  ops: Operation[],
): string | null {
  const components = asRecord(root["components"]);
  const schemes = asRecord(components?.["securitySchemes"]);
  if (schemes && Object.keys(schemes).length > 0) {
    return say("scheme_without_dependency", { component });
  }
  if ("security" in root) {
    return say("document_security_without_dependency", { component });
  }
  for (const op of ops) {
    if ("security" in op.op) {
      return say("security_without_dependency", { component, method: op.method, path: op.path });
    }
  }
  return identityHeaderProblem(ops);
}

/** A component behind sign-in: the scheme, the default, the flows, then every operation. */
function checkProtected(
  component: string,
  root: Record<string, unknown>,
  ops: Operation[],
  owners: Map<string, string> | null,
): string | null {
  const components = asRecord(root["components"]);
  const scheme = asRecord(asRecord(components?.["securitySchemes"])?.[SCHEME]);
  if (!scheme) {
    return say("missing_oauth2_scheme", { component });
  }
  const type = scheme["type"];
  if (type !== SCHEME) {
    return say("scheme_wrong_type", {
      component,
      type: typeof type === "string" ? type : type === undefined ? "absent" : JSON.stringify(type),
    });
  }
  if (!isDocumentDefault(root["security"])) {
    return say("missing_document_security", { component });
  }

  for (const { scope, where } of flowScopes(root)) {
    if (RESERVED.has(scope)) return say("reserved_oidc_scope", { scope, where });
    if (!owners) continue;
    const owner = owners.get(scope);
    if (owner === undefined) return say("flow_scope_not_in_catalog", { scope });
    if (owner !== component) return say("flow_scope_not_owned", { scope, owner });
  }

  for (const op of ops) {
    const problem = checkOperationSecurity(component, op, owners);
    if (problem) return problem;
  }
  return identityHeaderProblem(ops);
}

/** Absent, `[]`, or ONE requirement object naming `oauth2` with at most one scope. */
function checkOperationSecurity(
  component: string,
  op: Operation,
  owners: Map<string, string> | null,
): string | null {
  if (!("security" in op.op)) return null;
  const { method, path } = op;
  const security = op.op["security"];
  if (!Array.isArray(security)) return say("operation_security_not_a_list", { method, path });
  if (security.length === 0) return null; // public
  if (security.length > 1) return say("operation_multiple_requirements", { method, path });

  const requirement = asRecord(security[0]);
  if (!requirement) return say("operation_security_not_a_list", { method, path });
  const names = Object.keys(requirement);
  // Two schemes in ONE object mean "both", which the gateway cannot express and
  // the generated server does not read — the same disagreement as two objects.
  if (names.length !== 1) return say("operation_multiple_requirements", { method, path });
  const name = names[0] as string;
  if (name !== SCHEME) return say("operation_unknown_scheme", { method, path, scheme: name });

  const scopes = requirement[name];
  if (!Array.isArray(scopes)) return say("operation_security_not_a_list", { method, path });
  if (scopes.length > 1) return say("operation_multiple_scopes", { method, path });
  if (scopes.length === 0) return null; // the document default, spelled out

  const scope = scopes[0];
  if (typeof scope !== "string") return say("operation_security_not_a_list", { method, path });
  if (RESERVED.has(scope)) {
    return say("reserved_oidc_scope", { scope, where: `the security of ${method} ${path}` });
  }
  if (!owners) return null;
  const owner = owners.get(scope);
  if (owner === undefined) return say("scope_not_in_catalog", { method, path, scope });
  if (owner !== component) return say("scope_not_owned", { method, path, scope, owner });
  return null;
}

/**
 * The two identity-header rules. Both are about what happens OUTSIDE the
 * document — the parameter binder runs before the middleware, and the gateway
 * does not strip inbound headers on an operation it applies no policy to — so
 * neither shows up in the spec as anything but a plausible line.
 */
function identityHeaderProblem(ops: Operation[]): string | null {
  for (const op of ops) {
    const headers = identityHeaders(op);
    if (headers.length === 0) continue;
    const isPublic = Array.isArray(op.op["security"]) && op.op["security"].length === 0;
    for (const header of headers) {
      const name = header["name"] as string;
      if (isPublic) {
        return say("public_operation_declares_identity_header", {
          method: op.method,
          path: op.path,
          header: name,
        });
      }
      if (header["required"] === true) {
        return say("identity_header_required", {
          method: op.method,
          path: op.path,
          header: name,
        });
      }
    }
  }
  return null;
}
