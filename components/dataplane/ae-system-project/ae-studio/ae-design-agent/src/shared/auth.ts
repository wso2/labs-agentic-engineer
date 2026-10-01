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
 * The M2M auth gate (§12.3.2). A plain Bearer-JWT check on the conversation/turn
 * routes: the token's `aud` must match, verified against a configured JWKS
 * (RS256, the BFF's real S2S path) OR a shared secret (HS256, for local/eval).
 * There is NO org claim — nothing here calls back to the BFF, so all context is
 * pushed in per turn; `X-Org-Id` (read by the route) is load-bearing there: the
 * §12 fence asserts the conversation's org segment equals it.
 *
 * The gate is config-driven and injectable so tests exercise the shared-secret
 * path without a live IDP. It is always on: `resolveKey` throws at construction
 * if neither a JWKS URL nor a secret is configured, so the service cannot boot
 * unauthenticated.
 */

import type { RequestHandler, Response } from "express";
import { createRemoteJWKSet, jwtVerify, type JWTVerifyOptions } from "jose";

export interface AgentsAuthConfig {
  /** Required `aud` claim (e.g. `agents-service`). */
  audience: string;
  /** Optional `iss` claim to enforce. */
  issuer?: string;
  /** JWKS endpoint for RS256 verification (the BFF's S2S signing keys). */
  jwksUrl?: string;
  /** Shared secret for HS256 verification (local/eval). */
  secret?: string;
}

const REALM = "agents-service";

/** A token verifier bound to one key source. Resolves the payload or throws. */
type Verifier = (token: string) => Promise<unknown>;

/**
 * Bind a verifier to the configured key. RS256 via JWKS takes precedence when
 * both are set; otherwise HS256 via secret. Each branch calls `jwtVerify` with a
 * concrete key type, so the caller stays out of jose's overload union.
 */
function buildVerifier(cfg: AgentsAuthConfig): Verifier {
  const base: JWTVerifyOptions = {
    audience: cfg.audience,
    // Absorb small clock drift between this pod and the token issuer so a token
    // whose `nbf`/`iat` is a few seconds ahead of our clock (or `exp` a few
    // seconds behind) does not spuriously 401. jose's default is 0 (no skew).
    clockTolerance: "5s",
    ...(cfg.issuer ? { issuer: cfg.issuer } : {}),
  };
  if (cfg.jwksUrl) {
    const jwks = createRemoteJWKSet(new URL(cfg.jwksUrl));
    return (token) => jwtVerify(token, jwks, { ...base, algorithms: ["RS256"] });
  }
  if (cfg.secret) {
    const key = new TextEncoder().encode(cfg.secret);
    return (token) => jwtVerify(token, key, { ...base, algorithms: ["HS256"] });
  }
  throw new Error(
    "agents M2M auth is not configured: set AGENT_JWT_JWKS_URL or AGENT_JWT_SECRET (the gate is always on).",
  );
}

function setChallenge(res: Response, errorCode: string): void {
  const parts = [`realm="${REALM}"`];
  if (errorCode) parts.push(`error="${errorCode}"`);
  res.setHeader("WWW-Authenticate", `Bearer ${parts.join(", ")}`);
}

/**
 * Build the Bearer-JWT middleware. On success it calls `next()`; on any failure
 * it answers 401 with an RFC 6750 `WWW-Authenticate` challenge and does not fall
 * through, so a bad token never reaches a turn.
 */
export function createAuthMiddleware(cfg: AgentsAuthConfig): RequestHandler {
  const verify = buildVerifier(cfg);

  return async function m2mAuth(req, res, next): Promise<void> {
    const header = req.header("authorization");
    if (!header) {
      setChallenge(res, "");
      res.status(401).json({ error: "missing Authorization header" });
      return;
    }
    const match = /^Bearer\s+(.+)$/i.exec(header);
    if (!match) {
      setChallenge(res, "invalid_token");
      res.status(401).json({ error: "malformed Authorization header" });
      return;
    }
    try {
      await verify(match[1]!.trim());
      next();
    } catch {
      setChallenge(res, "invalid_token");
      res.status(401).json({ error: "invalid token" });
    }
  };
}
