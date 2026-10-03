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

export type TokenClaims = Record<string, unknown>;

// Decodes a JWT payload without verifying it — display/routing only. The
// BFF re-verifies every token; nothing security-relevant hangs off this.
export function decodeJwtClaims(token: string): TokenClaims | null {
  const payload = token.split(".")[1];
  if (!payload) return null;
  try {
    const b64 = payload.replaceAll("-", "+").replaceAll("_", "/");
    // atob yields latin-1 code points; decode the underlying UTF-8 bytes so
    // non-ASCII names survive.
    const bytes = Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
    const parsed: unknown = JSON.parse(new TextDecoder().decode(bytes));
    return typeof parsed === "object" && parsed !== null
      ? (parsed as TokenClaims)
      : null;
  } catch {
    return null;
  }
}

function str(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}

// Mirrors the BFF's auth.ResolveOuHandle precedence exactly (jwt.go):
// ouHandle > ouName > ouId. The console must agree with the server on
// which org a token addresses.
export function resolveOrgHandle(claims: TokenClaims): string | null {
  return (
    str(claims["ouHandle"]) ?? str(claims["ouName"]) ?? str(claims["ouId"]) ?? null
  );
}

export interface SessionIdentity {
  /** The token's `sub` (`""` when no source carries one). */
  id: string;
  name: string;
  email: string;
  orgHandle: string | null;
}

// Builds the display identity from token claim sets in precedence order
// (ID token first, access token as fallback — Thunder puts the ou* claims
// on the access token).
export function identityFromClaims(...sources: TokenClaims[]): SessionIdentity {
  const first = (pick: (c: TokenClaims) => string | undefined) => {
    for (const claims of sources) {
      const value = pick(claims);
      if (value) return value;
    }
    return undefined;
  };

  const givenFamily = (c: TokenClaims) => {
    const parts = [str(c["given_name"]), str(c["family_name"])].filter(Boolean);
    return parts.length > 0 ? parts.join(" ") : undefined;
  };

  const email = first((c) => str(c["email"])) ?? "";
  const name =
    first((c) => str(c["name"])) ??
    first(givenFamily) ??
    first((c) => str(c["username"])) ??
    (email || undefined) ??
    first((c) => str(c["sub"])) ??
    "User";

  let orgHandle: string | null = null;
  for (const claims of sources) {
    orgHandle = resolveOrgHandle(claims);
    if (orgHandle) break;
  }

  const id = first((c) => str(c["sub"])) ?? "";

  return { id, name, email, orgHandle };
}
