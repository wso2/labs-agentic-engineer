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
  sessionUserId,
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
  it("thunder: mirrors the BFF's Thunder dialect, ouHandle > ouName > ouId", () => {
    expect(
      resolveOrgHandle({ ouHandle: "h", ouName: "n", ouId: "i" }, "thunder"),
    ).toBe("h");
    expect(resolveOrgHandle({ ouName: "n", ouId: "i" }, "thunder")).toBe("n");
    expect(resolveOrgHandle({ ouId: "i" }, "thunder")).toBe("i");
  });

  it("skips empty and non-string values", () => {
    expect(resolveOrgHandle({ ouHandle: "", ouName: "n" }, "thunder")).toBe("n");
    expect(resolveOrgHandle({ ouHandle: 42, ouName: "n" }, "thunder")).toBe("n");
    expect(resolveOrgHandle({}, "thunder")).toBeNull();
  });

  it("asgardeo: org_handle names the org, with no fallback", () => {
    expect(
      resolveOrgHandle({ org_handle: "h", org_name: "n", org_id: "i" }, "asgardeo"),
    ).toBe("h");
    expect(resolveOrgHandle({ org_name: "n", org_id: "i" }, "asgardeo")).toBeNull();
    // Thunder's claims name no org under asgardeo, nor asgardeo's under thunder.
    expect(resolveOrgHandle({ ouHandle: "h" }, "asgardeo")).toBeNull();
    expect(resolveOrgHandle({ org_handle: "h" }, "thunder")).toBeNull();
  });

  it("an IdP kind this build does not know names no org", () => {
    expect(resolveOrgHandle({ ouHandle: "h", org_handle: "h" }, "okta")).toBeNull();
  });
});

describe("identityFromClaims", () => {
  it("asgardeo: no name claim, so given and family name; the org from org_handle", () => {
    // An Asgardeo access token's identity claims (names as issued, values made up).
    const identity = identityFromClaims("asgardeo", {
      sub: "u1",
      aut: "APPLICATION_USER",
      org_handle: "acme",
      org_id: "org-1",
      org_name: "Acme Inc",
      given_name: "Ada",
      family_name: "Lovelace",
      email: "ada@acme.example",
      username: "ada@acme.example",
    });
    expect(identity).toEqual({ name: "Ada Lovelace", email: "ada@acme.example", orgHandle: "acme" });
  });

  it("prefers name, then given/family, then username, email, sub", () => {
    expect(identityFromClaims("thunder", { name: "N", given_name: "G" }).name).toBe("N");
    expect(
      identityFromClaims("thunder", { given_name: "G", family_name: "F", username: "u" })
        .name,
    ).toBe("G F");
    expect(identityFromClaims("thunder", { username: "u", email: "e@x" }).name).toBe("u");
    expect(identityFromClaims("thunder", { email: "e@x" }).name).toBe("e@x");
    expect(identityFromClaims("thunder", { sub: "s" }).name).toBe("s");
    expect(identityFromClaims("thunder", {}).name).toBe("User");
  });

  it("takes the first source that has a value (ID token before access token)", () => {
    const identity = identityFromClaims("thunder",
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
      identityFromClaims("thunder", { ouName: "n" }, { ouHandle: "h" }).orgHandle,
    ).toBe("n");
  });

  it("returns empty email when no source has one", () => {
    expect(identityFromClaims("thunder", { name: "N" }).email).toBe("");
  });
});

describe("sessionUserId", () => {
  // The pods name a turn's author by the `sub` of the token they verify — the
  // access token, the /v1 bearer — so the console's own id is that claim.
  it("is the access token's sub", () => {
    expect(sessionUserId({ sub: "u-access" }, { sub: "u-access", email: "ada@x.com" })).toBe("u-access");
  });

  it("prefers the access token's sub when the two tokens disagree", () => {
    expect(sessionUserId({ sub: "u-access" }, { sub: "u-id-token" })).toBe("u-access");
  });

  it("falls back to the ID token's sub when the access token has none", () => {
    expect(sessionUserId({}, { sub: "u-id-token" })).toBe("u-id-token");
  });

  it("is empty when neither carries one", () => {
    expect(sessionUserId({}, { email: "ada@x.com" })).toBe("");
  });
});
