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

import { useMemo } from "react";
import { useSession } from "./SessionContext";

// The AE permission vocabulary. The catalog that DEFINES it is Go —
// services/aep-api/aeperms, which also declares these keys to Thunder as the
// `ae` resource server's actions and gates on them in the BFF — and this list
// is the console's copy of it, because TypeScript cannot import Go.
//
// The two are reconciled by TestConsoleUnionMatchesCatalog
// (services/aep-api/internal/authz), which reads THIS FILE and compares it to
// the catalog in both directions. That is the same move the chart's roles and
// scopes already use, and it is why a copy is safe to keep: adding a key in Go
// without adding it here fails there, by name, with the file to edit.
//
// Order follows aeperms.Actions, the order an installer declares them to
// Thunder in. The union derives from the array rather than being written out
// twice, so a key can only be added in one place here.
//
// Gating always checks the key itself, never a role name — the console has no
// reliable role signal and no reason to want one.
export const ALL_PERMISSIONS = [
  "ae:build",
  "ae:build-view",
  "ae:design",
  "ae:design-view",
  "ae:github-config",
  "ae:model-config",
  "ae:requirement-update",
  "ae:requirement-view",
  "ae:skill-config",
  "ae:skill-view",
  "ae:usage-view",
  "ae:observability-view",
  "ae:resource-view",
  "ae:resource-config",
] as const;

/** An AE permission key, e.g. "ae:build". */
export type Permission = (typeof ALL_PERMISSIONS)[number];

// The prefix every AE permission key carries. Not decoration — it is the
// Thunder resource handle, which Thunder composes into the qualified action
// name (aeperms' Resource/ResourceHandle).
const PERMISSION_PREFIX = "ae:";

// The access token's space-delimited scope claim carries ae:* permission keys
// alongside unrelated scopes (openid, profile, email, ...) — this is the only
// place the console derives a caller's real permissions from. Mirrors the
// BFF's filterPermissions (services/aep-api/internal/platform/auth/jwt.go).
//
// The filter is by PREFIX, deliberately, and not by membership of
// ALL_PERMISSIONS. An allowlist would make a stale console silently withhold a
// real grant: a key the console did not yet know would be dropped from a token
// that legitimately carried it, closing every gate on it for everyone while
// the BFF went on allowing the call. Filtering by prefix cannot do that. An
// ae:* key the console does not recognize simply enters the set and is never
// asked about, since only keys in the union are ever checked — inert rather
// than damaging. The drift test keeps the two lists in agreement; this makes
// disagreeing harmless in the window before anyone notices.
export function permissionsFromScope(scope: unknown): Set<string> {
  const held = new Set<string>();
  if (typeof scope !== "string") return held;
  for (const token of scope.split(/\s+/)) {
    if (token.startsWith(PERMISSION_PREFIX)) held.add(token);
  }
  return held;
}

/** What a caller holds, as predicates rather than as a set to search. */
export interface Permissions {
  has: (permission: Permission) => boolean;
  /**
   * True when the caller holds ANY of the listed permissions.
   *
   * For surfaces genuinely reachable from two different features, each with
   * its own permission — Settings' Credentials panel, which either
   * ae:github-config or ae:model-config admits you to, since each card is
   * separately redacted by the BFF. It is NOT the way to pair a write
   * permission with its view-only sibling: entry to a page is gated on the
   * view permission exactly, so that a role holding only the write half is
   * not admitted to a surface nobody intended it to see. Mirrors the
   * backend's operationPermissions, which draws the same line.
   */
  hasAny: (permissions: Permission[]) => boolean;
}

// The predicates over a permission set, outside React. Exported so a pure
// function that gates on permissions — settings.index's landing rule is the
// one today — can be unit-tested against the real thing rather than against a
// hand-rolled stand-in, which is how a test ends up passing against logic the
// app does not run.
export function permissionsOf(held: ReadonlySet<string>): Permissions {
  return {
    has: (permission) => held.has(permission),
    hasAny: (permissions) => permissions.some((p) => held.has(p)),
  };
}

// The primitive the other two are written over, and the one to reach for when
// the surfaces being gated are DATA rather than JSX — a list of settings
// sections, a nav rail, a row of cards. A hook cannot be called from inside a
// .map(), so gating a list with useHasPermission forces every check up to the
// top of the component, away from the row it decides, and grows a second
// lookup table to carry the answers back down. Asking `can.has(…)` inside the
// map keeps the permission next to the thing it withholds.
export function usePermissions(): Permissions {
  const held = useSession().permissions;
  return useMemo(() => permissionsOf(held), [held]);
}

// Retained over usePermissions for the common case: a component gating ONE
// piece of its own JSX on ONE permission, where a predicate object reads as
// ceremony.
export function useHasPermission(permission: Permission): boolean {
  return usePermissions().has(permission);
}

/** See {@link Permissions.hasAny} for when this is the right check. */
export function useHasAnyPermission(permissions: Permission[]): boolean {
  return usePermissions().hasAny(permissions);
}
