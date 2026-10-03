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

import { describe, expect, it } from "vitest";
import {
  decodeJwtClaims,
  identityFromClaims,
  resolveOrgHandle,
} from "./claims";

function jwt(payload: Record<string, unknown>): string {
  const b64 = (v: unknown) => {
    const bytes = new TextEncoder().encode(JSON.stringify(v));
    const bin = Array.from(bytes, (b) => String.fromCharCode(b)).join("");
    return btoa(bin)
      .replaceAll("+", "-")
      .replaceAll("/", "_")
      .replace(/=+$/, "");
  };
  return `${b64({ alg: "RS256" })}.${b64(payload)}.signature`;
}

describe("decodeJwtClaims", () => {
  it("decodes a base64url payload", () => {
    expect(decodeJwtClaims(jwt({ sub: "u1", ouHandle: "acme" }))).toEqual({
      sub: "u1",
      ouHandle: "acme",
    });
  });

  it("handles base64url characters (- and _)", () => {
    const claims = { name: "ÿþ?>~", note: "a?b>c" };
    expect(decodeJwtClaims(jwt(claims))).toEqual(claims);
  });

  it("returns null for garbage", () => {
    expect(decodeJwtClaims("not-a-jwt")).toBeNull();
    expect(decodeJwtClaims("a.b.c")).toBeNull();
    expect(decodeJwtClaims("")).toBeNull();
  });
});

describe("resolveOrgHandle", () => {
  it("mirrors the BFF precedence: ouHandle > ouName > ouId", () => {
    expect(
      resolveOrgHandle({ ouHandle: "h", ouName: "n", ouId: "i" }),
    ).toBe("h");
    expect(resolveOrgHandle({ ouName: "n", ouId: "i" })).toBe("n");
    expect(resolveOrgHandle({ ouId: "i" })).toBe("i");
  });

  it("skips empty and non-string values", () => {
    expect(resolveOrgHandle({ ouHandle: "", ouName: "n" })).toBe("n");
    expect(resolveOrgHandle({ ouHandle: 42, ouName: "n" })).toBe("n");
    expect(resolveOrgHandle({})).toBeNull();
  });
});

describe("identityFromClaims", () => {
  it("prefers name, then given/family, then username, email, sub", () => {
    expect(identityFromClaims({ name: "N", given_name: "G" }).name).toBe("N");
    expect(
      identityFromClaims({ given_name: "G", family_name: "F", username: "u" })
        .name,
    ).toBe("G F");
    expect(identityFromClaims({ username: "u", email: "e@x" }).name).toBe("u");
    expect(identityFromClaims({ email: "e@x" }).name).toBe("e@x");
    expect(identityFromClaims({ sub: "s" }).name).toBe("s");
    expect(identityFromClaims({}).name).toBe("User");
  });

  it("takes the first source that has a value (ID token before access token)", () => {
    const identity = identityFromClaims(
      { name: "From ID" },
      { name: "From access", ouHandle: "acme" },
    );
    expect(identity.name).toBe("From ID");
    // ou* lives only on the access token — still found.
    expect(identity.orgHandle).toBe("acme");
  });

  it("resolves the org per-source, not per-claim across sources", () => {
    // ID token has ouName only; access token has ouHandle. The first source
    // with any org claim wins (ouName), matching a BFF that reads one token.
    expect(
      identityFromClaims({ ouName: "n" }, { ouHandle: "h" }).orgHandle,
    ).toBe("n");
  });

  it("returns empty email when no source has one", () => {
    expect(identityFromClaims({ name: "N" }).email).toBe("");
  });
});

describe("identityFromClaims id", () => {
  // The pods name a turn's author by the verified token's `sub`, so the
  // console's own id must be the same claim, not the email.
  it("is the token's sub", () => {
    expect(identityFromClaims({ sub: "u-123", email: "ada@x.com", name: "Ada" }).id).toBe("u-123");
  });

  it("takes the first source that carries a sub", () => {
    expect(identityFromClaims({ email: "ada@x.com" }, { sub: "u-123" }).id).toBe("u-123");
  });

  it("is empty when no source carries one", () => {
    expect(identityFromClaims({ email: "ada@x.com" }).id).toBe("");
  });
});
