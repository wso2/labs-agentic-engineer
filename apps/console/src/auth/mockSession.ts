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

import { ALL_PERMISSIONS } from "./permissions";

// The mock-auth identity for default dev (VITE_API_MODE=mock): a fixed
// user and org, no Thunder required. The org matches the collab mock
// BFF's default so spec rooms resolve the same way in both places.
//
// Multi-user dev (`aep:mock:user`): set a display name and reload to act as
// a DIFFERENT teammate — `sessionStorage.setItem('aep:mock:user','Alice')`
// scopes the identity to ONE tab (sessionStorage is per-tab), so two tabs of
// the same browser can exercise owner-vs-teammate flows (e.g. the question
// form's asker-only submit). A localStorage value works too (whole browser).
// Read once at module load: identity must stay stable for the session, same
// as a real login. The email derives from the name.

function mockUserOverride(): { name: string; email: string } | null {
  try {
    const name = (
      sessionStorage.getItem("aep:mock:user") ?? localStorage.getItem("aep:mock:user")
    )?.trim();
    if (!name) return null;
    const slug = name.toLowerCase().replaceAll(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
    return { name, email: `${slug || "user"}@example.com` };
  } catch {
    return null;
  }
}

export const MOCK_USER = {
  role: "Developer",
  ...(mockUserOverride() ?? { name: "Developer", email: "developer@example.com" }),
} as const;

export const MOCK_ORG = "acme";

// Independent of `aep:mock:user` above — permissions are data, not identity,
// so switching teammates never changes what the mock session can do. Absent
// key => every permission; present-but-empty => an explicit "no permissions"
// override, and any comma-separated subset works in between, which is how a
// restricted surface is demoed without a real Thunder role:
//
//   sessionStorage.setItem('aep:mock:permissions', '')                 // denied
//   sessionStorage.setItem('aep:mock:permissions', 'ae:design-view')   // read-only spec
function mockPermissionsOverride(): Set<string> | null {
  try {
    const raw =
      sessionStorage.getItem("aep:mock:permissions") ??
      localStorage.getItem("aep:mock:permissions");
    if (raw === null) return null;
    return new Set(
      raw
        .split(",")
        .map((key) => key.trim())
        .filter(Boolean),
    );
  } catch {
    return null;
  }
}

// Every permission by default. Mock mode exists to develop the console without
// a BFF, so it must not hide surfaces: a partial grant reads as a broken build
// rather than as a permission demo, and view permissions are exact-matched —
// holding ae:build does not admit you to a page gated on ae:build-view — so
// any subset silently locks pages that have nothing to do with what is being
// worked on. Use the override above to see a restricted state on purpose.
export function mockPermissions(): Set<string> {
  return mockPermissionsOverride() ?? new Set<string>(ALL_PERMISSIONS);
}

// JWT-shaped but unsigned — enough for the collab mock BFF, which decodes
// name/email claims without verifying. Never sent to a real BFF: real API
// runs use thunder auth mode.
export function mockAccessToken(): string {
  const b64 = (v: unknown) =>
    btoa(JSON.stringify(v))
      .replaceAll("+", "-")
      .replaceAll("/", "_")
      .replace(/=+$/, "");
  return `${b64({ alg: "none" })}.${b64({
    name: MOCK_USER.name,
    email: MOCK_USER.email,
    ouHandle: MOCK_ORG,
  })}.dev`;
}
