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
 * Pure projections of a parsed `SecurityDesign` v2 — the permission catalog as
 * a lookup, and a role's grants as a lookup.
 *
 * Every gate that has to answer "is this handle declared?" or "does this role
 * hold it?" (the referential checks, the openapi.yaml security gate, the console
 * Security page) folds the document the same way, so the fold lives once. No
 * runtime imports: types only, no validation, no I/O — a caller that already
 * holds a parsed document can use these without pulling Zod in.
 */

import type { SecurityDesign } from "./contracts/security-design.js";

/**
 * The OIDC scopes the identity provider puts on every access token. They are
 * NOT catalog handles and must never be emitted as one: an operation guarded on
 * `openid` admits every signed-in account in the org, and the gateway reports
 * nothing — the projection bug fails wide open, silently.
 */
export const OIDC_RESERVED_SCOPES: readonly string[] = ["openid", "profile", "email", "group", "ou"];

/** Every `<resource>:<action>` handle the document declares. */
export function catalogHandles(doc: SecurityDesign): Set<string> {
  const handles = new Set<string>();
  for (const permission of doc.permissions) {
    for (const action of permission.actions) {
      handles.add(`${permission.resource}:${action.handle}`);
    }
  }
  return handles;
}

/**
 * Role name (verbatim, as authored) → the handles that role grants. Built from
 * `grants` alone: the `X:read-all` ⇒ `X:read` widening is a gate RULE, checked
 * and reported against the authored list, not silently applied here.
 */
export function roleGrants(doc: SecurityDesign): Map<string, Set<string>> {
  const byRole = new Map<string, Set<string>>();
  for (const role of doc.roles) {
    byRole.set(role.name, new Set(role.grants));
  }
  return byRole;
}
