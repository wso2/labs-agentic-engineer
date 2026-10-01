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
 * The Platform IdP token check (07 §8): a signature over the IdP's JWKS, the
 * exact issuer, a required exp, and an aud that names one of the token kinds
 * the caller accepts. A token matching no kind is refused. The TypeScript
 * twin of ae-studio-tools' `internal/auth/verify.go`.
 */

import { createRemoteJWKSet, jwtVerify, type JWTPayload, type JWTVerifyGetKey } from "jose";

/** A token kind and the audiences that name it. Kinds are wiring, never request input. */
export interface TokenKind {
  name: "user" | "ae-studio";
  audiences: string[];
}

/**
 * The claims of a verified Platform IdP access token that the pod reads.
 * `sub` is optional here: a client_credentials token need not carry one.
 */
export interface PlatformClaims {
  sub?: string;
  ouId?: string;
  ouHandle?: string;
  client_id?: string;
  grant_type?: string;
  name?: string;
  given_name?: string;
  family_name?: string;
  email?: string;
  exp: number;
}

/** The claims of a verified user token: a user always has a subject. */
export type UserClaims = PlatformClaims & { sub: string };

export type VerifiedToken =
  | { kind: "user"; claims: UserClaims }
  | { kind: "ae-studio"; claims: PlatformClaims };

/**
 * The token is missing, malformed, not signed by the IdP, expired, from
 * another issuer, or not for any accepted kind. Always a 401.
 */
export class UnauthenticatedError extends Error {
  override readonly name = "UnauthenticatedError";
}

const ALGORITHMS = ["RS256", "ES256"];
const CLOCK_TOLERANCE = "5s";
const JWKS_CACHE_MAX_AGE_MS = 10 * 60_000;
const JWKS_COOLDOWN_MS = 30_000;
const CLIENT_CREDENTIALS = "client_credentials";

const OPTIONAL_STRING_CLAIMS = [
  "sub",
  "ouId",
  "ouHandle",
  "client_id",
  "grant_type",
  "name",
  "given_name",
  "family_name",
  "email",
] as const;

/**
 * Returns `verify(token, kinds)` for tokens issued by exactly `issuer`. The
 * token must name its key (`kid`). A `user` token needs a non-empty `sub` and
 * is never a client_credentials token; an `ae-studio` token is a
 * client_credentials token whose `client_id` is one of that kind's audiences.
 * The key set is fetched from `jwksUrl` (cached, refetched on an unknown kid);
 * `jwks` replaces it in tests. An empty issuer or JWKS URL throws: each is a
 * wiring error that would weaken the check.
 */
export function createVerifier(opts: {
  issuer: string;
  jwksUrl: string;
  jwks?: JWTVerifyGetKey;
}): (token: string, kinds: TokenKind[]) => Promise<VerifiedToken> {
  if (!opts.issuer) throw new Error("createVerifier: issuer is required");
  if (!opts.jwksUrl) throw new Error("createVerifier: jwksUrl is required");
  const keys =
    opts.jwks ??
    createRemoteJWKSet(new URL(opts.jwksUrl), {
      cacheMaxAge: JWKS_CACHE_MAX_AGE_MS,
      cooldownDuration: JWKS_COOLDOWN_MS,
    });

  return async function verify(token: string, kinds: TokenKind[]): Promise<VerifiedToken> {
    assertKinds(kinds);
    let payload: JWTPayload;
    let kid: unknown;
    try {
      ({ payload, protectedHeader: { kid } } = await jwtVerify(token, keys, {
        issuer: opts.issuer,
        algorithms: ALGORITHMS,
        requiredClaims: ["exp"],
        clockTolerance: CLOCK_TOLERANCE,
      }));
    } catch {
      throw new UnauthenticatedError("invalid token");
    }
    if (typeof kid !== "string" || kid === "") throw new UnauthenticatedError("token names no key");
    const aud = Array.isArray(payload.aud) ? payload.aud : payload.aud ? [payload.aud] : [];
    const kind = kinds.find((k) => aud.some((a) => k.audiences.includes(a)));
    if (!kind) throw new UnauthenticatedError("token kind not accepted here");
    const claims = toPlatformClaims(payload);
    if (kind.name === "user") {
      if (claims.grant_type === CLIENT_CREDENTIALS) throw new UnauthenticatedError("client token on a user route");
      const { sub } = claims;
      if (!sub) throw new UnauthenticatedError("user token without a subject");
      return { kind: "user", claims: { ...claims, sub } };
    }
    if (claims.grant_type !== CLIENT_CREDENTIALS || !claims.client_id || !kind.audiences.includes(claims.client_id)) {
      throw new UnauthenticatedError("not this kind's client");
    }
    return { kind: "ae-studio", claims };
  };
}

/** No kind, a kind without audiences, or an empty audience would widen or void the check. */
function assertKinds(kinds: TokenKind[]): void {
  if (kinds.length === 0 || kinds.some((k) => k.audiences.length === 0 || k.audiences.includes(""))) {
    throw new Error("verify: every token kind needs at least one non-empty audience");
  }
}

/** jwtVerify has checked exp; only string-valued optional claims are carried. */
function toPlatformClaims(payload: JWTPayload): PlatformClaims {
  if (typeof payload.exp !== "number") throw new UnauthenticatedError("invalid token");
  const claims: PlatformClaims = { exp: payload.exp };
  for (const key of OPTIONAL_STRING_CLAIMS) {
    const value = payload[key];
    if (typeof value === "string") claims[key] = value;
  }
  return claims;
}
