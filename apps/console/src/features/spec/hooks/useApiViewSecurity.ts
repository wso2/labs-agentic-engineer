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
 * What the API view needs that a contract cannot tell it: who may reach a row,
 * and which audience the scopes on it belong to.
 *
 * `OpenApiView` is a renderer of ONE document. The two facts it decorates rows
 * and its header with come from two other places — the design's permission
 * catalog (`security.json`, in the collab room or in git) and the platform's
 * own record of the project's resource server. Gathering both in one hook is
 * what keeps every place that renders a contract to a single line, so the API
 * view says the same thing in the Spec workspace, in the components list and on
 * a deployment's page instead of only where somebody remembered to wire it.
 *
 * Everything is DECORATION: each answer is independently optional, and a view
 * that gets neither renders exactly as it did before scopes existed. Nothing
 * here blocks or spins.
 */

import { useMemo } from "react";

import type { ScopeRoles } from "@aep/ui-openapi-view";

import { scopeRolesOf } from "../api/apiViewRoles";
import { SECURITY_JSON_PATH } from "../api/designTree";
import { useSpecFileContent, useSpecFiles } from "../api/queries";
import { resourceServerOf, useProjectRoles } from "../api/roles";
import type { CollabSpec } from "../collab/useCollabSpec";
import { useYTextString } from "../collab/useYTextString";

export interface ApiViewSecurity {
  /** Scope handle → the roles that grant it, or undefined with no catalog. */
  roles: ScopeRoles | undefined;
  /**
   * The audience a token for these scopes carries, as the platform recorded it
   * at the last Build. Undefined before there is a record — the console never
   * guesses a URL the gateway would then refuse.
   */
  resourceServer: string | undefined;
}

export function useApiViewSecurity({
  projectName,
  active,
  collab,
}: {
  projectName: string;
  /** False when no contract is on screen — every read below is then skipped. */
  active: boolean;
  /**
   * The spec room, when the caller has one. The room's copy of the catalog is
   * preferred over git's, so a grant moved on the Security page shows up on the
   * API view without a commit in between. A caller outside the Spec workspace
   * (a dialog on the components list) has no room and reads the committed copy.
   */
  collab?: CollabSpec | undefined;
}): ApiViewSecurity {
  const securityYText =
    active && collab ? collab.getFileText(SECURITY_JSON_PATH) : null;
  const liveText = useYTextString(securityYText);

  // The committed copy is the fallback only, and its file list is fetched only
  // when it is going to be used: a room that already holds the catalog costs
  // no request.
  const wantsCommitted = active && liveText === null;
  const files = useSpecFiles(projectName, wantsCommitted);
  const entry = wantsCommitted
    ? (files.data?.find((f) => f.path === SECURITY_JSON_PATH) ?? null)
    : null;
  const committed = useSpecFileContent(projectName, entry);

  const text = liveText ?? committed.data?.content ?? null;
  const roles = useMemo(() => scopeRolesOf(text), [text]);

  const live = useProjectRoles(projectName, active);

  return { roles, resourceServer: resourceServerOf(live.data) };
}
