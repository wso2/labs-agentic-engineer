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

// Mock mode — copied to <app-path>/mock/auth.ts.
//
// This is the one mock file that is NOT verbatim. It is a module substitution,
// so its export list has to be your src/auth.ts's export list: add a mock of
// anything your auth module adds, drop what it does not have. Everything below
// is the standard surface — signIn, signOut, handleCallback, currentUser,
// accessToken, tokenIsValid — plus scopesFromToken for the mock handlers.
//
// mock/plugin.ts resolves every import of src/auth.ts to this file under
// `--mode mock`, so it exports the same names and nothing under src/ changes.
// src/authz.tsx and src/authz-core.ts are NOT substituted: they read
// `user.scope` from whatever auth module is in play, so the REAL authorization
// code runs in mock mode too. That is the whole value of the walk.
//
// There is no IDP, no redirect and no real token: the caller is always signed
// in, and `?role=` on the URL says as whom.
//
//   /claims                 the first role in ./roles.ts
//   /claims?role=Approver   that role, holding exactly that role's grants
//   /claims?role=           signed in holding NO project scope at all — this is
//                           the NoAccess case, and it must be walkable
//   /claims?auth=out        NOBODY is signed in, so the app's own sign-in guard
//                           runs; signIn() then drops the parameter, which is
//                           the mock's stand-in for coming back from the IDP.
//                           Without this every sign-in story is unwalkable.
//
// Switching roles is a navigation, so a reviewer compares two roles against one
// running server. A role absent from ./roles.ts is honoured too — it simply
// grants nothing, which is how a screen is checked against a role that must NOT
// reach it.
//
// THE MOCK NEVER WIDENS. `mockRoles` carries each role's grants exactly as
// security.json declares them, and mock/handlers.ts enforces the exact handle
// the contract declares. A mock that accepts `claims:read-all` where the
// contract says `claims:read` lets a role pass a walk the real service answers
// 403 to — which hides the precise defect the walk exists to find. A role that
// cannot reach its own screen is a DESIGN DEFECT to report, never a mock to
// loosen.

import { mockRoles } from "./roles";

// The OIDC scopes the platform always requests beside the project handles. They
// are in the real token's `scope` claim, so they are in the mock's too — and
// holding them is never holding a project handle.
const BASE_SCOPES = ["openid", "profile", "email", "group", "ou"];

export interface MockUser {
  profile: { sub: string; name: string; email: string };
  access_token: string;
  /** The granted scope string, exactly as a token response carries it. */
  scope: string;
  /** Seconds since the epoch, as oidc-client-ts surfaces it. */
  expires_at: number;
  expired: boolean;
}

function signedOut(): boolean {
  return new URLSearchParams(window.location.search).get("auth") === "out";
}

// The role rides the URL only for the navigation that set it: the app's own
// internal links and redirects carry bare paths, exactly as they would against
// a real session that needs no query string. Production reads an OIDC session
// out of storage, so it survives that navigation untouched; a mock that re-reads
// the URL on every mount forgets who is signed in the moment the app takes one
// internal route change — every role except the URL-less default. Persisting
// the last explicit `?role=` (including the deliberately empty one, which is
// the NoAccess case) is what makes this mock behave like the session it stands
// in for.
const ROLE_STORAGE_KEY = "aep-mock-role";

function activeRoleNames(): string[] {
  const param = new URLSearchParams(window.location.search).get("role");
  let asked: string;
  if (param !== null) {
    try {
      sessionStorage.setItem(ROLE_STORAGE_KEY, param);
    } catch {
      /* private mode: fall through, the URL still wins for this mount */
    }
    asked = param;
  } else {
    let stored: string | null = null;
    try {
      stored = sessionStorage.getItem(ROLE_STORAGE_KEY);
    } catch {
      /* private mode */
    }
    if (stored === null) return mockRoles.slice(0, 1).map((role) => role.name);
    asked = stored;
  }
  return asked
    .split(",")
    .map((name) => name.trim())
    .filter(Boolean);
}

/** The union of the grants of every role named on the URL. Sorted, so the
 *  token string is stable across renders and screenshots. */
function activeScopes(): string[] {
  const names = activeRoleNames().map((name) => name.toLowerCase());
  const held = new Set<string>();
  for (const role of mockRoles) {
    if (names.includes(role.name.toLowerCase())) {
      for (const grant of role.grants) held.add(grant);
    }
  }
  return [...held].sort();
}

function user(): MockUser {
  const names = activeRoleNames();
  const who = names[0] ?? "visitor";
  const slug = who.toLowerCase().replace(/\s+/g, "-");
  const scopes = activeScopes();
  return {
    profile: {
      sub: `mock-${slug}`,
      name: `Mock ${who}`,
      email: `${slug}@example.test`,
    },
    access_token: `mock:${scopes.map(encodeURIComponent).join(",")}`,
    scope: [...BASE_SCOPES, ...scopes].join(" "),
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    expired: false,
  };
}

/**
 * The scopes carried by an `Authorization: Bearer mock:claims:read,…` header.
 * mock/handlers.ts enforces with this, so a scope-gated operation is scope-gated
 * in mock mode too — the 403 + `insufficient_scope` included.
 */
export function scopesFromToken(header: string | null): string[] {
  const token = header?.replace(/^Bearer\s+/i, "") ?? "";
  if (!token.startsWith("mock:")) return [];
  return token
    .slice("mock:".length)
    .split(",")
    .map((value) => decodeURIComponent(value).trim())
    .filter(Boolean);
}

// Signed in already, unless ?auth=out says otherwise — in which case this is
// the redirect coming back: drop the parameter and reload, and the caller finds
// a session where a moment ago there was none.
export async function signIn(): Promise<void> {
  if (!signedOut()) return;
  const url = new URL(window.location.href);
  url.searchParams.delete("auth");
  window.location.assign(url.toString());
}

export async function handleCallback(): Promise<MockUser> {
  return user();
}

export async function signOut(): Promise<void> {
  window.location.assign("/");
}

export async function currentUser(): Promise<MockUser | null> {
  return signedOut() ? null : user();
}

export async function accessToken(): Promise<string | null> {
  return signedOut() ? null : user().access_token;
}

/**
 * The mock session never expires, so this answers "is somebody signed in?".
 * src/api-client.ts asks it on every 401 and 403: with a session, a refusal is
 * about SCOPE and routes to Forbidden; without one it signs in. Returning true
 * unconditionally here would make `?auth=out` unwalkable.
 */
export async function tokenIsValid(): Promise<boolean> {
  return !signedOut();
}
