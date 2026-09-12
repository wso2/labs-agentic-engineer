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

// Copied VERBATIM to <app-path>/src/api-client.ts. Your per-service client
// (src/api.ts, generated types and all) calls `apiFetch` — or, with a generated
// openapi-fetch client, `authorizationHeader()` and `classifyResponse()` from
// its middleware — and adds NOTHING of its own about authorization.
//
// Two jobs, and only two: attach the bearer, and decide what an unauthorized
// answer means.
//
// THE 401 RULE. `if (res.status === 401) signIn()` — what the previous sample
// taught — is DELETED, and deleting it is the point of this file. The gateway
// answers 401 for every failure with a byte-identical body and no
// `WWW-Authenticate`: no token, expired token, wrong issuer, wrong audience and
// MISSING SCOPE are indistinguishable on the wire. So a correctly provisioned
// user who touches one operation their role does not grant is thrown into an
// endless sign-in loop — sign in, succeed, call, 401, sign in — which is not a
// hypothetical: it was observed on real accounts, 160 ms per cycle.
//
// The decision comes from what the SPA already holds: its own `expires_at`.
// See ./authz-core#classifyApiFailure for the three cases. src/authz.tsx is
// what keeps them rare — it gates before the call, so a screen the token does
// not unlock is never rendered and its operations are never invoked — but a
// typed URL, a stale bundle or a race past a narrowed renew still get here.

import { accessToken, signIn, tokenIsValid } from "./auth";
import { createUnauthorizedHandler, type ApiFailure } from "./authz-core";

/** Where the app routes a refusal. Wired once, at start-up, from the router. */
export type ForbiddenNavigator = (context: { status: number }) => void;

let navigateToForbidden: ForbiddenNavigator = () => {
  // Until the router is up there is nowhere to go. Say so rather than swallow
  // it: a silent no-op here looks exactly like a screen that renders nothing.
  console.error(
    "api-client: a request was refused before the router was ready — " +
      "call setForbiddenNavigator() once from your router's root.",
  );
};

/**
 * Wire the /forbidden route in. Call ONCE from the app's root, e.g.
 *
 *   const navigate = useNavigate();
 *   useEffect(() => setForbiddenNavigator(() => navigate("/forbidden")), [navigate]);
 *
 * Injected rather than imported so this module stays free of the router — and
 * so the rule it carries is testable without one.
 */
export function setForbiddenNavigator(navigator: ForbiddenNavigator): void {
  navigateToForbidden = navigator;
}

const onUnauthorized = createUnauthorizedHandler({
  signIn,
  // Re-read the module variable on every call, so a navigator wired after this
  // handler was built is the one that runs.
  onForbidden: (context) => navigateToForbidden(context),
});

/** A call the caller's token may not make. NEVER a reason to sign in again. */
export class ForbiddenError extends Error {
  readonly status: number;
  constructor(status: number) {
    super("Your session does not carry the permission this action needs.");
    this.name = "ForbiddenError";
    this.status = status;
  }
}

export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

/**
 * The bearer header value, renewing silently first if it has to. null when
 * there is no session — send the request without one and let the gateway
 * answer; `classifyResponse` then starts the sign-in.
 */
export async function authorizationHeader(): Promise<string | null> {
  const token = await accessToken();
  return token ? `Bearer ${token}` : null;
}

/**
 * Apply the 401 rule to a response status. Returns what it decided and has
 * ALREADY acted on it: "forbidden" navigated to /forbidden, "signin" started
 * the redirect (once), "ok" did nothing. Call it on every response.
 */
export async function classifyResponse(status: number): Promise<ApiFailure> {
  if (status !== 401 && status !== 403) return "ok";
  return onUnauthorized(status, await tokenIsValid());
}

/**
 * fetch against the sibling service, with the bearer attached and the 401 rule
 * applied. Same-origin `/api`: the nginx in this pod reverse-proxies it to the
 * API gateway, which validates the token and injects the `X-User-*` headers the
 * service authorizes on. There is no browser-visible API host.
 *
 * Using a generated typed client (openapi-fetch) instead? Do NOT re-implement
 * the rule in its middleware — call the same two functions:
 *
 *   const authMiddleware: Middleware = {
 *     async onRequest({ request }) {
 *       const header = await authorizationHeader();
 *       if (header) request.headers.set("Authorization", header);
 *       return request;
 *     },
 *     async onResponse({ response }) {
 *       if ((await classifyResponse(response.status)) === "forbidden") {
 *         throw new ForbiddenError(response.status);
 *       }
 *       return response;
 *     },
 *   };
 */
export async function apiFetch(path: string, init: RequestInit = {}): Promise<Response> {
  const headers = new Headers(init.headers);
  const header = await authorizationHeader();
  if (header) headers.set("Authorization", header);

  const response = await fetch(`/api${path}`, { ...init, headers });

  const outcome = await classifyResponse(response.status);
  if (outcome === "forbidden") throw new ForbiddenError(response.status);
  // "signin": the redirect is under way; nothing downstream should render.
  if (outcome === "signin") throw new ApiError(response.status, "Signing in…");

  if (!response.ok) {
    throw new ApiError(
      response.status,
      `${response.status} ${response.statusText || "request failed"}`,
    );
  }
  return response;
}

/** The JSON body of a successful call. */
export async function apiJson<T>(path: string, init?: RequestInit): Promise<T> {
  return (await apiFetch(path, init)).json() as Promise<T>;
}
