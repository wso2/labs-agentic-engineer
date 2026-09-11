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

import { useSession } from "./SessionContext";

// Mirrors services/aep-api's role_permissions_catalog.go by hand until the
// backend exposes permission keys as a generated contract type. Gating always
// checks the key itself, never a role name — the console has no reliable
// role signal and no reason to want one.
export type Permission =
  | "ae:skill-config"
  | "ae:model-config"
  | "ae:github-config"
  | "ae:requirement-update"
  | "ae:requirement-view"
  | "ae:design-view"
  | "ae:build"
  | "ae:build-view";

export function useHasPermission(permission: Permission): boolean {
  return useSession().permissions.has(permission);
}

// For a "-view" permission that's a strictly weaker sibling of an editing one
// (ae:build-view alongside ae:build): holding the editing permission always
// satisfies the view check too, so callers never need to grant both.
export function useHasAnyPermission(permissions: Permission[]): boolean {
  const held = useSession().permissions;
  return permissions.some((permission) => held.has(permission));
}
