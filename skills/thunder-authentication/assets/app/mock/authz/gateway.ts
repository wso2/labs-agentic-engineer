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
// <app-path>/mock/authz/gateway.ts, never edited.
//
// THE API GATEWAY, in mock mode. Production has two layers in front of a
// screen and they answer different questions:
//
//   the gateway   may this caller call this operation at all?  -> 401 if not
//   the service   which of these rows are theirs?              -> 404 if not
//
// `mock/handlers.ts` is the SERVICE. This file is the GATEWAY, and it is
// registered ahead of the handlers so the two stay as separate in mock mode as
// they are in a cell. A handler that does its own scope check is keeping a
// second copy of the contract — the exact drift this layer exists to remove.
//
// WHERE THE TABLE COMES FROM. `mock/plugin.ts` reads the sibling's
// `openapi.yaml` at dev-server start and puts it on `/env-config.js`, the same
// script that already carries `window._env_` and for the same reason: it is the
// one channel that is in place BEFORE the bundle runs. Nothing is transcribed
// and nothing is committed, so the mock cannot disagree with the contract the
// real gateway is rendered from. An app whose contracts declare no `oauth2`
// scheme gets a null table and no gateway at all, which is right: it has no
// sign-in to enforce.
//
// NOT Vite's `define`, which is the obvious reach and is wrong here: Vite skips
// define replacement for client modules in dev (`consumer === "client" &&
// !isBuild` returns early in vite:define), so the table would land in a
// production build nobody runs and be MISSING from `dev:mock`, which is the
// only way mock mode ever runs. The failure is silent — an undefined identifier
// reads as "no table", and the gateway disables itself.
//
// WHY 401 AND NOT 403. The real gateway answers 401 for every
// authentication-policy failure — no token, expired token, wrong issuer, wrong
// audience, MISSING SCOPE — with a byte-identical body and no
// `WWW-Authenticate`. Nothing downstream can tell those apart, which is the
// whole reason `src/authz/client.ts` decides on its own `expires_at` instead.
// Answering 403 here would walk a branch of that rule the deployed app never
// takes, and leave the branch it does take — 401 while the token is still
// valid — untested. That branch is the one whose regression is an endless
// sign-in loop.

import { http, HttpResponse, type RequestHandler } from "msw";

/** One row of the operation table, as mock/plugin.ts projects it. */
export interface MockOperation {
  /** Upper-case HTTP method. */
  method: string;
  /** The contract's own path template, for the console line. */
  path: string;
  /** RegExp source matching the request path with the prefix stripped. */
  pattern: string;
  /** The handle the operation declares, or null for "signed in, no handle". */
  scope: string | null;
  /** `security: []` — served to anyone, and the handler reads no identity. */
  isPublic: boolean;
}

export interface MockOperationTable {
  /** The path prefix the app calls through, `/api` unless configured. */
  prefix: string;
  operations: MockOperation[];
}

// Read off the global /env-config.js sets. A cast rather than `declare global`
// so this file needs no ambient .d.ts and no `lib: ["DOM"]`.
const table: MockOperationTable | null =
  (globalThis as { __AEP_MOCK_GATEWAY__?: MockOperationTable | null }).__AEP_MOCK_GATEWAY__ ??
  null;

/**
 * The scopes carried by a mock bearer (`mock:HRCoordinator;claims:read,claims:submit`).
 *
 * This file owns the format because in production the GATEWAY is what reads a
 * token; `mock/authz/session.ts` mints one and re-exports this reader for the header
 * badge. A handler never decides on it: which rows an operation returns is its
 * path (`/api/me/…` the caller's, anything else every row — ADR-0031).
 *
 * THE ROLE SEGMENT. Everything before the first `;` names the role(s) the
 * session is signed in as, and it is NOT a scope. It exists for the one reader
 * that cannot work it out for itself: the wired gateway (`mock/wired.ts`), which
 * has to put a username in the assertion it mints and cannot infer the role from
 * the grants, because two roles in one design may hold exactly the same ones. A
 * token with no `;` is the older shape and is read as all-scopes, so a page
 * loaded before a restart keeps working.
 */
export function scopesFromToken(header: string | null): string[] {
  const token = header?.replace(/^Bearer\s+/i, "") ?? "";
  if (!token.startsWith("mock:")) return [];
  const body = token.slice("mock:".length);
  const semicolon = body.indexOf(";");
  return (semicolon >= 0 ? body.slice(semicolon + 1) : body)
    .split(",")
    .map((value) => decodeURIComponent(value).trim())
    .filter(Boolean);
}

/**
 * Whether the caller presented a session at all.
 *
 * Separate from holding any handle, and the distinction is load-bearing: a user
 * signed in with NO project grants (`?role=`) still has a valid token, and an
 * operation that declares no handle — "signed in, no particular permission" —
 * must admit them. Testing the scope list for empty would refuse them, which no
 * gateway does.
 */
function signedIn(header: string | null): boolean {
  return /^Bearer\s+mock:/i.test(header?.trim() ?? "");
}

const matchers = (table?.operations ?? []).map((op) => ({
  op,
  re: new RegExp(op.pattern),
}));

function findOperation(method: string, path: string): MockOperation | null {
  for (const { op, re } of matchers) {
    if (op.method === method && re.test(path)) return op;
  }
  return null;
}

// Bare, exactly as the deployed gateway answers: no body to read, no
// `WWW-Authenticate` to branch on. The reason lands in the CONSOLE instead —
// the wire stays honest and the walk stays diagnosable.
function refuse(method: string, op: MockOperation, why: string): HttpResponse<null> {
  console.info(
    `[mock gateway] 401 ${method} ${op.path} — ${why}. ` +
      "The deployed gateway answers this the same way, with no body and no WWW-Authenticate.",
  );
  return new HttpResponse(null, { status: 401 });
}

/**
 * Registered BEFORE the app's handlers. Returning `undefined` falls through to
 * them, which is how one MSW handler layers in front of another.
 *
 * Empty when no contract declares an `oauth2` scheme — an app with no sign-in
 * has no gateway policy to model, and an empty list adds no handler at all.
 */
export const gatewayHandlers: RequestHandler[] = table
  ? [
      http.all(`${table.prefix}/*`, ({ request }) => {
        const method = request.method.toUpperCase();
        const path = new URL(request.url).pathname.slice(table.prefix.length) || "/";
        const op = findOperation(method, path);

        // Not in the contract: the gateway has no route for it and answers 404.
        // Left to fall through so browser.ts's 501 catch-all names it as the
        // mock's own gap — a 404 here would be indistinguishable from one the
        // contract genuinely declares, which is the harder bug to find.
        if (!op) return undefined;

        // A preflight short-circuits before the auth policy on the real
        // gateway. Same-origin `/api` never issues one, so this is here to be
        // correct rather than because it fires.
        if (op.isPublic || method === "OPTIONS") return undefined;

        const auth = request.headers.get("authorization");
        if (!signedIn(auth)) return refuse(method, op, "no token");
        if (op.scope && !scopesFromToken(auth).includes(op.scope)) {
          return refuse(method, op, `the caller does not hold ${op.scope}`);
        }
        return undefined;
      }),
    ]
  : [];
