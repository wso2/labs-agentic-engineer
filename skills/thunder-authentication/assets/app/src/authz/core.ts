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

// Copied VERBATIM, with the rest of app/, to <app-path>/src/authz/core.ts.
//
// The pure half of authorization: scope parsing, the operation gate, the role
// projection, token expiry and the one rule that decides what an unauthorized
// answer means. No React, no `window`, no session, no import of ANY other
// module in the app — not even ./roles.gen, which is why every function that
// needs the generated tables takes them as an argument.
//
// WHY THIS FILE EXISTS AT ALL. src/env.ts throws at module load when
// `window._env_` is absent and src/authz/session.ts constructs a UserManager at
// module load. Anything that imports session.ts therefore drags a browser into
// the import graph and cannot be loaded by a unit test in a plain node
// environment. Keeping the rules here — and only the wiring in gates.tsx,
// session.ts and client.ts — is what makes them testable with no DOM, no jsdom
// and no stub of the IdP. src/authz/gates.tsx re-exports what callers need, so
// the module surface the platform's skills target is unchanged.
//
// Scope comparison is a WHOLE-STRING match, everywhere, deliberately. A token
// that carries `claims:read-all` does NOT satisfy `claims:read`: the gateway
// does not treat one as a superset of the other, and neither does the service
// middleware, so neither may the SPA. A role that needs both holds both.

/**
 * Split an access token's `scope` claim into the handles it carries.
 *
 * The claim is one space-separated string and it carries the IdP's own scopes
 * (`openid profile email group ou`) beside the project's. They stay in the set:
 * nothing here can tell them apart, and `granted()` only ever asks about a
 * catalog handle, so `openid` can never satisfy one. Filter against the
 * generated `SCOPES` list if you need the project subset for display.
 */
export function parseScopes(scopeClaim: string | undefined): Set<string> {
  return new Set((scopeClaim ?? "").split(/\s+/).filter(Boolean));
}

/**
 * Does this scope set carry `scope` — one catalog handle, spelled exactly as
 * the catalog spells it?
 */
export function granted(scopes: ReadonlySet<string>, scope: string): boolean {
  return scopes.has(scope);
}

/**
 * What an API operation demands of its caller, as
 * `src/authz/operations.gen.ts` projects it out of `openapi.yaml`. Restated
 * here structurally, with `scope` widened to `string`, so this module keeps
 * importing nothing (see the header).
 */
export type OperationRequirement =
  | { kind: "public" }
  | { kind: "signedIn" }
  | { kind: "scope"; scope: string };

/**
 * May a caller holding `scopes` (signed in or not) call an operation with this
 * requirement?
 *
 *   public    served to anyone, session or not
 *   signedIn  any caller with a session, whatever they hold
 *   scope     exactly that handle, whole-string
 *
 * This is also the SCREEN gate: a screen is reachable when its load operation
 * is callable. There is no second rule and no screen table anywhere.
 */
export function canCall(
  requirement: OperationRequirement,
  scopes: ReadonlySet<string>,
  signedIn: boolean,
): boolean {
  if (requirement.kind === "public") return true;
  if (requirement.kind === "signedIn") return signedIn;
  return granted(scopes, requirement.scope);
}

/**
 * The roles whose EVERY grant is held. A partial grant set yields no role:
 * holding two of an Approver's five handles does not make anybody an Approver.
 *
 * `roleGrants` is the generated `ROLE_GRANTS` table. Passed in rather than
 * imported so this module stays free of ./roles.gen (see the header).
 */
export function heldRoles<R extends string, S extends string>(
  scopes: ReadonlySet<string>,
  roleGrants: Readonly<Record<R, readonly S[]>>,
): R[] {
  return (Object.keys(roleGrants) as R[]).filter((role) => {
    const grants = roleGrants[role];
    return grants.length > 0 && grants.every((grant) => scopes.has(grant));
  });
}

/**
 * The roles that grant `scope` — what the Forbidden page names to the user.
 * Deterministic: declaration order of `ROLE_GRANTS`, which is the sorted
 * generator output.
 */
export function rolesGranting<R extends string, S extends string>(
  scope: string,
  roleGrants: Readonly<Record<R, readonly S[]>>,
): R[] {
  return (Object.keys(roleGrants) as R[]).filter((role) =>
    (roleGrants[role] as readonly string[]).includes(scope),
  );
}

/**
 * Is the session's access token still usable?
 *
 *   expiresAt    the token's `expires_at`, SECONDS since the epoch, exactly as
 *                oidc-client-ts surfaces it. `undefined` when there is no user
 *                and no token — which is not a valid token.
 *   now          milliseconds since the epoch (`Date.now()`).
 *   skewSeconds  the library's clock skew, as a GRACE period.
 *
 * The skew EXTENDS validity rather than shortening it, and the direction is the
 * whole point. Getting this wrong in the other direction says "expired" about a
 * live session, and a 401 on a live session then restarts sign-in — the
 * infinite sign-in loop a correctly provisioned user hit on the real cluster.
 * Getting it wrong this way shows Forbidden once on a genuinely dead session,
 * which the next reload corrects.
 */
export function tokenIsValid(
  expiresAt: number | undefined,
  now: number,
  skewSeconds: number,
): boolean {
  if (expiresAt === undefined) return false;
  return (expiresAt + skewSeconds) * 1000 > now;
}

/** What an API response means for the caller's session. */
export type ApiFailure = "forbidden" | "signin" | "ok";

/**
 * THE 401 RULE. The one place that decides what an unauthorized answer means,
 * because getting it wrong turns every forbidden click into a login loop.
 *
 *   403                    the service, reached past the gateway, saying
 *                          `insufficient_scope`. Forbidden. ALWAYS. Signing in
 *                          again cannot add a scope the caller's roles do not
 *                          grant, so a retry loops for ever.
 *   401 + a live token     the gateway refusing the operation's scope. It
 *                          answers 401 for EVERY failure — no token, bad token,
 *                          wrong issuer, wrong audience, missing scope — with a
 *                          byte-identical body and no `WWW-Authenticate`, so
 *                          nothing on the wire distinguishes them. The SPA's own
 *                          `expires_at` is the only local, authoritative signal:
 *                          if the session is alive, the refusal is about scope.
 *                          Forbidden.
 *   401 + no/expired token the ordinary case. Sign in.
 *
 * Do NOT add a `WWW-Authenticate` read. The gateway never sends one; this was
 * measured three ways, on the pinned chart, and the header column came back
 * empty in every cell.
 */
export function classifyApiFailure(status: number, tokenValid: boolean): ApiFailure {
  if (status === 403) return "forbidden";
  if (status === 401) return tokenValid ? "forbidden" : "signin";
  return "ok";
}

/** What an unauthorized handler needs from the app it is wired into. */
export interface UnauthorizedHandlerDeps {
  /** Starts the redirect to the IdP. Called at most once per page load. */
  signIn: () => void | Promise<void>;
  /** Routes to the app's /forbidden screen, INSIDE the shell. */
  onForbidden: (context: { status: number }) => void;
}

/**
 * Builds the response hook `src/authz/client.ts` runs on every answer.
 *
 * `signIn` is guarded to ONE call per page load. A burst of parallel requests
 * from one screen answers 401 several times over; without the guard each answer
 * starts its own redirect and the address bar thrashes. The redirect ends this
 * document anyway, so a second call can only be noise.
 *
 * A sign-in that REJECTS disarms the guard again. `signIn()` reaches the
 * network — `oidc-client-ts` fetches the issuer's discovery document before it
 * can build the authorize URL — so a flaky moment leaves the document exactly
 * where it was, with no redirect under way. Holding the guard armed on that
 * would wedge the page for the rest of its life: every later 401 returns
 * "signin" and nothing ever signs in. The rejection is logged rather than
 * swallowed, because it is the only trace of a failed redirect.
 */
export function createUnauthorizedHandler(deps: UnauthorizedHandlerDeps): (
  status: number,
  tokenValid: boolean,
) => ApiFailure {
  let signInStarted = false;
  return (status, tokenValid) => {
    const outcome = classifyApiFailure(status, tokenValid);
    if (outcome === "forbidden") {
      deps.onForbidden({ status });
    } else if (outcome === "signin" && !signInStarted) {
      signInStarted = true;
      void Promise.resolve(deps.signIn()).catch((err) => {
        signInStarted = false;
        console.error("authz/client: sign-in failed", err);
      });
    }
    return outcome;
  };
}
