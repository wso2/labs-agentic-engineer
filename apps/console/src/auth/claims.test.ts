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
import asgardeoFixture from "../../../../packages/platform-idp-auth/test/fixtures/idp-claims/asgardeo.json?raw";
import thunderFixture from "../../../../packages/platform-idp-auth/test/fixtures/idp-claims/thunder.json?raw";
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

// The sample tokens every reader of the platform IdP's tokens is tested
// against (aep-api's dialect, the pod's two, and this file): for each IdP AE
// ships claim values for, token payloads and the org each must read as.
interface ClaimsFixture {
  idp: string;
  claims: { org: { handle: string } };
  tokens: { name: string; payload: Record<string, unknown>; org: { handle: string } }[];
}

const fixtures = [thunderFixture, asgardeoFixture].map((raw) => JSON.parse(raw) as ClaimsFixture);

describe("resolveOrgHandle", () => {
  for (const fixture of fixtures) {
    for (const token of fixture.tokens) {
      it(`${fixture.idp}: ${token.name}`, () => {
        // The console's "no org" is null where the server's is an empty handle.
        expect(resolveOrgHandle(token.payload, fixture.claims.org.handle)).toBe(
          token.org.handle === "" ? null : token.org.handle,
        );
      });
    }
  }

  it("reads the named claim only: nothing stands in for the handle", () => {
    expect(resolveOrgHandle({ ouHandle: "h", ouName: "n", ouId: "i" }, "ouHandle")).toBe("h");
    expect(resolveOrgHandle({ ouName: "n", ouId: "i" }, "ouHandle")).toBeNull();
    expect(resolveOrgHandle({ org_handle: "h" }, "ouHandle")).toBeNull();
  });

  it("skips empty and non-string values", () => {
    expect(resolveOrgHandle({ ouHandle: "" }, "ouHandle")).toBeNull();
    expect(resolveOrgHandle({ ouHandle: 42 }, "ouHandle")).toBeNull();
    expect(resolveOrgHandle({}, "ouHandle")).toBeNull();
  });
});

describe("identityFromClaims", () => {
  it("a token with no name claim: given and family name; the org from the configured claim", () => {
    // An Asgardeo access token's identity claims (names as issued, values made up).
    const identity = identityFromClaims("org_handle", {
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
    expect(identityFromClaims("ouHandle", { name: "N", given_name: "G" }).name).toBe("N");
    expect(
      identityFromClaims("ouHandle", { given_name: "G", family_name: "F", username: "u" })
        .name,
    ).toBe("G F");
    expect(identityFromClaims("ouHandle", { username: "u", email: "e@x" }).name).toBe("u");
    expect(identityFromClaims("ouHandle", { email: "e@x" }).name).toBe("e@x");
    expect(identityFromClaims("ouHandle", { sub: "s" }).name).toBe("s");
    expect(identityFromClaims("ouHandle", {}).name).toBe("User");
  });

  it("takes the first source that has a value (ID token before access token)", () => {
    const identity = identityFromClaims("ouHandle",
      { name: "From ID" },
      { name: "From access", ouHandle: "acme" },
    );
    expect(identity.name).toBe("From ID");
    // ou* lives only on the access token — still found.
    expect(identity.orgHandle).toBe("acme");
  });

  it("takes the org from the first source that carries the handle claim", () => {
    // The ID token has the org's name only, which is not the handle; the
    // access token has the handle.
    expect(
      identityFromClaims("ouHandle", { ouName: "n" }, { ouHandle: "h" }).orgHandle,
    ).toBe("h");
    expect(
      identityFromClaims("ouHandle", { ouHandle: "id-token" }, { ouHandle: "access" }).orgHandle,
    ).toBe("id-token");
  });

  it("returns empty email when no source has one", () => {
    expect(identityFromClaims("ouHandle", { name: "N" }).email).toBe("");
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
