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

// Mock mode, WIRED — copied verbatim to <app-path>/mock/wired.ts, never edited.
//
// THE API GATEWAY, stood in for by the dev server, in front of the REAL service.
//
// Mock mode answers every `/api` call from `mock/handlers.ts` in the browser.
// Wired mode (the playground's `play <dir> wire`) does not: the generated
// service is running in a container with its own database, and this file is
// what lets the app talk to it as a role.
//
// It is the same two-layer request path mock mode already models —
//
//   the gateway   may this caller call this operation at all?  -> 401 if not
//   the service   which of these rows are theirs?              -> the real pod
//
// — with the gateway's half moved out of the browser and into Node, because it
// has to do the one thing a request interceptor structurally cannot: MINT the
// signed `x-jwt-assertion` the service verifies. The service checks that
// signature against one certificate and nothing else (see the `ballerina`
// skill's gateway_assertion.bal), so whoever holds the matching private key IS
// the gateway as far as it can tell. `wire` generates that keypair per project
// and passes the service the certificate and this file the key.
//
// NOTHING ABOUT SCOPES IS TRANSCRIBED HERE. The operation table is the one
// `mock/plugin.ts` already reads out of the project's `openapi.yaml`, and the
// bearer is the one `mock/authz/session.ts` already mints, so the wired gateway
// and the browser gateway cannot disagree about who may call what.
//
// Active only when AEP_WIRED_API is set; a bare `npm run dev:mock` is plain mock
// mode and this file does nothing.

import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import type { Connect, ProxyOptions } from "vite";
import type { MockOperation, MockOperationTable } from "./authz/gateway";

/** Everything `wire` passes in through the environment. */
export interface WiredOptions {
  /** Where the real service answers — `http://localhost:<compose's mapped port>`. */
  target: string;
  /** PEM private key (PKCS8) the assertion is signed with. Never inside the app tree. */
  keyPath: string;
  /** `specs/design/security.json`, read for each role's `testUsers` row. */
  securityPath: string;
  /** The `iss` claim; must equal the service's `GATEWAY_ASSERTION_ISSUER`. */
  issuer: string;
  /** The header the assertion travels in; the service's `GATEWAY_ASSERTION_HEADER`. */
  header: string;
}

/** The caller a request was admitted as, carried from the middleware to the proxy. */
interface Caller {
  role: string;
  username: string;
  scopes: string[];
}

// A symbol rather than a header or a property name: the middleware and the
// proxy hook are two steps of ONE request, and nothing the browser sends may
// impersonate what the first step decided.
const CALLER = Symbol("aep-wired-caller");
type WithCaller = IncomingMessage & { [CALLER]?: Caller | null };

/** The base scopes the platform requests beside the project handles, as `mock/authz/session.ts` mints them. */
const BASE_SCOPES = ["openid", "profile", "email", "group", "ou"];

/**
 * Where the project's `security.json` is, for whoever needs it: the roles it
 * declares (the badge) and the login name each one signs in as (the assertion).
 * `AEP_WIRED_SECURITY` overrides it, so a project laid out differently moves
 * both readers at once.
 */
export function securityPath(root: string): string {
  return process.env.AEP_WIRED_SECURITY ?? path.resolve(root, "..", "specs", "design", "security.json");
}

/**
 * Wired mode's settings, or null for plain mock mode.
 *
 * `AEP_WIRED_KEY` is REQUIRED once `AEP_WIRED_API` is set rather than defaulted:
 * without a key this file would forward every call with no assertion, and the
 * service would answer every `/me/` operation 401 — a wiring fault that reads
 * as an application bug.
 */
export function wiredFromEnv(root: string): WiredOptions | null {
  const target = process.env.AEP_WIRED_API;
  if (!target) return null;
  const keyPath = process.env.AEP_WIRED_KEY;
  if (!keyPath) throw new Error("AEP_WIRED_API is set but AEP_WIRED_KEY is not — see the playground's `wire` verb");
  return {
    target,
    keyPath,
    securityPath: securityPath(root),
    issuer: process.env.AEP_WIRED_ISSUER ?? "aep-playground-wire",
    header: process.env.AEP_WIRED_HEADER ?? "x-jwt-assertion",
  };
}

/**
 * `Bearer mock:<role>;<scopes csv>` -> the role and the handles it holds.
 *
 * The role is NAMED by the token, never inferred from the scopes: two roles in
 * one design can hold identical grants (an IT and a Facilities staffer who each
 * only complete their own tasks), and the assertion's `username` — which is what
 * the service keys `/me/` reach on — differs between them. Inferring would make
 * one of the two invisible.
 *
 * A token with no `;` is the pre-wired shape and yields an empty role, which is
 * exactly the `?role=` (signed in, no grants) caller.
 */
export function parseMockBearer(header: string | undefined): { role: string; scopes: string[] } | null {
  const token = header?.replace(/^Bearer\s+/i, "").trim() ?? "";
  if (!token.startsWith("mock:")) return null;
  const body = token.slice("mock:".length);
  const semi = body.indexOf(";");
  // `decodeURIComponent` THROWS on malformed percent-encoding (`%zz`), and a
  // throw here escapes into the middleware as a 500. Every other unusable token
  // returns null and is answered as a refused session, which is what a caller
  // holding a broken token should see; one bad escape must not be the single
  // shape that reads as the mock being broken instead.
  try {
    const role = semi >= 0 ? decodeURIComponent(body.slice(0, semi)) : "";
    const csv = semi >= 0 ? body.slice(semi + 1) : body;
    const scopes = csv
      .split(",")
      .map((value) => decodeURIComponent(value).trim())
      .filter(Boolean);
    return { role, scopes };
  } catch {
    return null;
  }
}

/**
 * The login name the assertion carries for a role — `security.json`'s own
 * `testUsers` row, because that is the name the deployed environment's test
 * accounts sign in with and therefore the name the seeded rows are owned by.
 *
 * Falls back to the role name so a design with no `testUsers` still runs; what
 * it loses is `/me/` reach, which is a finding about the design rather than
 * something for this file to paper over.
 */
export function usernameFor(role: string, securityPath: string): string {
  try {
    const security = JSON.parse(fs.readFileSync(securityPath, "utf8")) as {
      testUsers?: { username: string; roles: string[] }[];
    };
    return security.testUsers?.find((u) => u.roles.includes(role))?.username ?? role;
  } catch {
    return role;
  }
}

const b64url = (input: Buffer | string): string =>
  Buffer.from(input).toString("base64").replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_");

/**
 * A directory id for a role, derived from its name so it is the same id on every
 * run — a row created as HRCoordinator yesterday still belongs to them today.
 * Shaped as a UUID because that is what a real `sub` is.
 */
function subjectFor(role: string): string {
  const digest = crypto.createHash("sha1").update(`aep-wire:${role}`).digest("hex");
  return `${digest.slice(0, 8)}-${digest.slice(8, 12)}-${digest.slice(12, 16)}-${digest.slice(16, 20)}-${digest.slice(20, 32)}`;
}

/**
 * The assertion, minted per request and valid for a minute.
 *
 * Short on purpose: the only consumer is the next hop on localhost, and a token
 * that outlives the request cannot leak from a terminal's scrollback into
 * anything that still works.
 */
export function mintAssertion(caller: Caller, opts: WiredOptions, now = Date.now()): string {
  const issuedAt = Math.floor(now / 1000);
  const header = b64url(JSON.stringify({ alg: "RS256", typ: "JWT" }));
  const payload = b64url(
    JSON.stringify({
      iss: opts.issuer,
      sub: subjectFor(caller.role),
      username: caller.username,
      scope: caller.scopes.join(" "),
      ouHandle: "local",
      iat: issuedAt,
      exp: issuedAt + 60,
    }),
  );
  const signature = crypto.sign("sha256", Buffer.from(`${header}.${payload}`), fs.readFileSync(opts.keyPath, "utf8"));
  return `${header}.${payload}.${b64url(signature)}`;
}

function findOperation(table: MockOperationTable, method: string, pathname: string): MockOperation | null {
  for (const op of table.operations) {
    if (op.method === method && new RegExp(op.pattern).test(pathname)) return op;
  }
  return null;
}

/**
 * Refused before anything reaches the service.
 *
 * The body is empty, as the deployed gateway's is. The reason rides an extra
 * response header instead, because unlike the deployed gateway this one is a
 * DEBUGGING surface: the browser's network tab is where a wired walk finds out
 * that a screen 401s because its role lacks a handle. Header values must be
 * ASCII — a single non-ASCII character makes Node throw while writing the
 * response, which surfaces as a 500 on every call and hides the real answer.
 */
function refuse(res: ServerResponse, status: number, why: string): void {
  console.info(`[wired gateway] ${status} ${why}`);
  res.statusCode = status;
  res.setHeader("x-aep-wired-reason", why.replace(/[^\x20-\x7e]/g, "-"));
  res.end();
}

/**
 * The gateway's "may this be called" decision, as a Connect middleware.
 *
 * Registered in `configureServer`, which Vite runs BEFORE its own proxy
 * middleware — so every forwarded request has passed this, and there is no
 * ordering trick that reaches the service around it.
 */
export function wiredMiddleware(table: MockOperationTable, opts: WiredOptions): Connect.NextHandleFunction {
  const prefix = table.prefix;
  return (req, res, next) => {
    const url = req.url ?? "";
    if (url !== prefix && !url.startsWith(`${prefix}/`)) return next();
    const pathname = new URL(url, "http://wired.local").pathname.slice(prefix.length) || "/";
    const method = (req.method ?? "GET").toUpperCase();

    const op = findOperation(table, method, pathname);
    // 404 rather than a pass-through: no contract declares this path, so the
    // deployed gateway has no route for it either. Mock mode hid this behind
    // MSW's 501 catch-all; here it is the same answer the platform gives.
    if (!op) return refuse(res, 404, `${method} ${pathname} — no operation in any openapi.yaml declares this path`);

    if (op.isPublic) {
      (req as WithCaller)[CALLER] = null;
      return next();
    }

    const bearer = parseMockBearer(req.headers.authorization);
    if (!bearer) return refuse(res, 401, `${method} ${op.path} — no session`);
    if (op.scope && !bearer.scopes.includes(op.scope)) {
      return refuse(res, 401, `${method} ${op.path} — role ${bearer.role || "(none)"} lacks ${op.scope}`);
    }

    (req as WithCaller)[CALLER] = {
      role: bearer.role,
      username: usernameFor(bearer.role, opts.securityPath),
      scopes: [...BASE_SCOPES, ...bearer.scopes],
    };
    return next();
  };
}

/**
 * The forward, as a Vite `server.proxy` entry: the prefix is stripped exactly as
 * the production nginx strips it, the browser's mock bearer is DROPPED (the
 * service must never see a caller-supplied identity), and the assertion the
 * middleware's decision earned is set in its place.
 */
export function wiredProxy(table: MockOperationTable, opts: WiredOptions): Record<string, ProxyOptions> {
  const prefix = table.prefix;
  return {
    [prefix]: {
      target: opts.target,
      changeOrigin: true,
      rewrite: (requestPath) => requestPath.slice(prefix.length) || "/",
      configure(proxy) {
        proxy.on("proxyReq", (proxyReq, req) => {
          const caller = (req as WithCaller)[CALLER];
          proxyReq.removeHeader("authorization");
          if (caller) proxyReq.setHeader(opts.header, mintAssertion(caller, opts));
          else proxyReq.removeHeader(opts.header);
        });
      },
    },
  };
}
