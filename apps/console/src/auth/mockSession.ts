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
// as a real login. The email and the id (the token's `sub`) derive from the name.

function mockUserOverride(): { id: string; name: string; email: string } | null {
  try {
    const name = (
      sessionStorage.getItem("aep:mock:user") ?? localStorage.getItem("aep:mock:user")
    )?.trim();
    if (!name) return null;
    const slug = name.toLowerCase().replaceAll(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");
    return { id: `mock-sub-${slug || "user"}`, name, email: `${slug || "user"}@example.com` };
  } catch {
    return null;
  }
}

export const MOCK_USER = {
  role: "Developer",
  ...(mockUserOverride() ?? { id: "mock-sub-developer", name: "Developer", email: "developer@example.com" }),
} as const;

export const MOCK_ORG = "acme";

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
    sub: MOCK_USER.id,
    name: MOCK_USER.name,
    email: MOCK_USER.email,
    ouHandle: MOCK_ORG,
  })}.dev`;
}
