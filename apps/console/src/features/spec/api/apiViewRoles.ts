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
 * The permission catalog as the API view wants it: scope handle → who grants it.
 *
 * `OpenApiView` holds the CONTRACT and nothing else — it reads which handle an
 * operation asks for, and it resolves nothing. Who holds that handle is in
 * `security.json`, a file the view has never heard of, so the join is made
 * here and handed in as a prop. That is what lets the same view render a
 * project's own contract with roles beside every row and a third party's
 * without.
 *
 * A pure function over the document text, so the projection is testable without
 * a room, a query client or a render.
 */

import type { ScopeRoles } from "@aep/ui-openapi-view";

import {
  parseSecurityDesign,
  rolesGranting,
  type SecurityDesign,
} from "./securityDesign";

/**
 * Every handle the catalog declares, with the roles that grant it.
 *
 * Only DECLARED handles are keys. A contract asking for a handle the catalog
 * does not declare gets no roles rather than an empty list — the view shows
 * nothing beside such a row, and the Security page's own "used nowhere" /
 * "not declared" findings are where that disagreement is reported.
 *
 * An `own`-ownership action carries the qualifier the matrix draws as a chip,
 * so the row reads "Employee, Approver · own rows" — the same sentence in both
 * places. `any` gets none: it is the unremarkable case, and a note on every row
 * would be noise rather than information.
 */
export function scopeRoles(doc: SecurityDesign): ScopeRoles {
  const out: ScopeRoles = {};
  for (const permission of doc.permissions) {
    for (const action of permission.actions) {
      const handle = `${permission.resource}:${action.handle}`;
      const roles = rolesGranting(doc, handle);
      out[handle] =
        action.ownership === "own" ? { roles, note: "own rows" } : roles;
    }
  }
  return out;
}

/**
 * The same projection over the document TEXT, or `undefined` when there is no
 * readable document.
 *
 * `undefined` and `{}` are different answers and the view treats them so: with
 * no map it renders exactly as it did before scopes existed, and with an empty
 * one it renders the fixed public / signed-in copy on every row. A project
 * whose design has not reached `security.json` yet must get the first.
 */
export function scopeRolesOf(text: string | null): ScopeRoles | undefined {
  const parsed = parseSecurityDesign(text);
  return parsed.kind === "ok" ? scopeRoles(parsed.doc) : undefined;
}
