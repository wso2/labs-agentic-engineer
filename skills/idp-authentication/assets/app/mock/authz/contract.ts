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

// Mock mode — copied verbatim, with the rest of app/, to
// <app-path>/mock/authz/contract.ts, never edited.
//
// Projects an OpenAPI document's `security` blocks into the table
// `mock/authz/gateway.ts` refuses from. This is the same reading the platform does
// when it renders the real gateway (aep-api's OpenAPIOperations), so the mock
// and the deployed API agree by construction rather than by review.
//
// It takes a PARSED document, not a file: the YAML parse lives in
// mock/plugin.ts, which is the only half that needs a dependency, and keeping
// the rules here means they can be exercised against plain objects.
//
// scripts/gen-authz.mjs RESTATES these rules in JavaScript — a .mjs script
// cannot import a .ts module — and the two are tested against one fixture so
// the screen gates and the mock gateway cannot read a contract differently.
//
// Every refusal below THROWS rather than degrading. A contract this cannot read
// is one the real gateway would also refuse to serve, and a mock that guessed
// would be green where the cell is 404 — which is the failure the whole layer
// exists to prevent.

import type { MockOperation } from "./gateway";

/** Methods the gateway is measured for. Anything else cannot be a row. */
const RENDERABLE = new Set(["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"]);
/** Declared in contracts, unreachable through the gateway, skipped in silence. */
const SKIPPED = new Set(["HEAD", "TRACE"]);

/**
 * The OIDC scopes every access token carries. One of them as an operation's
 * handle would admit every signed-in account in the org while looking guarded,
 * so it is a refusal here exactly as it is at deploy.
 */
const RESERVED_OIDC = new Set(["openid", "profile", "email", "group", "ou"]);

const SCHEME = "oauth2";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** `/claims/{claimId}/approve` -> `^/claims/[^/]+/approve$` */
function patternFor(template: string): string {
  const escaped = template.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  // The escape above turned `{id}` into `\{id\}`; swap each back for one segment.
  return `^${escaped.replace(/\\\{[^/]*?\\\}/g, "[^/]+")}$`;
}

/** How many path parameters a template carries — the sort key, below. */
function parameterCount(template: string): number {
  return (template.match(/\{/g) ?? []).length;
}

/**
 * Reads ONE operation's `security` block. Mirrors the deploy-time projection
 * rule for rule, including which shapes are refused outright.
 */
function requirementOf(
  operation: Record<string, unknown>,
  where: string,
): { scope: string | null; isPublic: boolean } {
  if (!("security" in operation)) {
    // Inherits the document default, which is checked to be the signed-in one.
    return { scope: null, isPublic: false };
  }
  const security = operation.security;
  if (!Array.isArray(security)) {
    throw new Error(`${where}: \`security\` must be a list`);
  }
  if (security.length === 0) {
    return { scope: null, isPublic: true };
  }
  if (security.length > 1) {
    throw new Error(
      `${where}: \`security\` names more than one requirement; the gateway serves exactly one`,
    );
  }
  const requirement = security[0];
  if (!isRecord(requirement)) {
    throw new Error(`${where}: the \`security\` entry must be a mapping`);
  }
  const names = Object.keys(requirement);
  if (names.length !== 1) {
    throw new Error(
      `${where}: the \`security\` entry names ${names.length} schemes; the gateway serves exactly one`,
    );
  }
  if (names[0] !== SCHEME) {
    throw new Error(`${where}: unknown security scheme \`${names[0]}\`, expected \`${SCHEME}\``);
  }
  const scopes = requirement[SCHEME];
  if (!Array.isArray(scopes)) {
    throw new Error(`${where}: \`${SCHEME}\` must carry a list of scopes`);
  }
  if (scopes.length > 1) {
    throw new Error(`${where}: names ${scopes.length} scopes; an operation declares exactly one`);
  }
  if (scopes.length === 0) {
    // The document default, spelled out on the operation.
    return { scope: null, isPublic: false };
  }
  const scope = scopes[0];
  if (typeof scope !== "string") {
    throw new Error(`${where}: the scope must be a string`);
  }
  if (RESERVED_OIDC.has(scope)) {
    throw new Error(
      `${where}: \`${scope}\` is an OIDC scope that rides every token, so requiring it ` +
        "admits every signed-in account while looking guarded",
    );
  }
  return { scope, isPublic: false };
}

/**
 * The operation table for one contract, or NULL when the document declares no
 * `oauth2` scheme — an API with no sign-in, which has no gateway policy to
 * model and must not be refused for lacking one.
 *
 * @param document the parsed openapi.yaml
 * @param source   the file it came from, for every message
 */
export function projectOperations(document: unknown, source: string): MockOperation[] | null {
  if (!isRecord(document)) {
    throw new Error(`${source}: the document is not a YAML mapping`);
  }
  const schemes = isRecord(document.components) ? document.components.securitySchemes : undefined;
  const scheme = isRecord(schemes) ? schemes[SCHEME] : undefined;
  if (scheme === undefined) {
    return null;
  }
  if (!isRecord(scheme) || scheme.type !== SCHEME) {
    throw new Error(`${source}: components.securitySchemes.${SCHEME} is not of type \`${SCHEME}\``);
  }
  // The document default is what makes an operation whose `security` block was
  // FORGOTTEN fail closed. Without it, absent would mean public.
  const docSecurity = document.security;
  const hasSignedInDefault =
    Array.isArray(docSecurity) &&
    docSecurity.length === 1 &&
    isRecord(docSecurity[0]) &&
    Object.keys(docSecurity[0]).length === 1 &&
    Array.isArray((docSecurity[0] as Record<string, unknown>)[SCHEME]) &&
    ((docSecurity[0] as Record<string, unknown>)[SCHEME] as unknown[]).length === 0;
  if (!hasSignedInDefault) {
    throw new Error(
      `${source}: the document declares no \`security: [{${SCHEME}: []}]\` default with an EMPTY ` +
        "scope list, so an operation that declares none of its own cannot be read as signed-in",
    );
  }

  const paths = isRecord(document.paths) ? document.paths : {};
  const operations: MockOperation[] = [];
  for (const [template, item] of Object.entries(paths)) {
    if (!isRecord(item)) continue;
    for (const [rawMethod, operation] of Object.entries(item)) {
      const method = rawMethod.toUpperCase();
      if (SKIPPED.has(method)) continue;
      if (!RENDERABLE.has(method)) continue;
      if (!isRecord(operation)) continue;
      const { scope, isPublic } = requirementOf(operation, `${source}: ${method} ${template}`);
      operations.push({ method, path: template, pattern: patternFor(template), scope, isPublic });
    }
  }

  // Literal paths before templated ones, and longer before shorter. The gateway
  // resolves a route; this list is scanned in order, so without the sort
  // `/todos/{id}` would swallow `/todos/archived`.
  operations.sort(
    (a, b) => parameterCount(a.path) - parameterCount(b.path) || b.path.length - a.path.length,
  );
  return operations;
}
