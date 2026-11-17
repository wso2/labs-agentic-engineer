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
 * The platform IdP's token dialect: the claim names and values one IdP uses
 * to say who a caller is. The verifier reads a token's org and whether it is
 * a machine's through the dialect; nothing else in the pod reads one of those
 * claims by its IdP-specific name. The TypeScript twin of ae-studio-tools'
 * `internal/auth/dialect` (and of aep-api's), pinned to both by the sample
 * tokens in `test/fixtures/idp-claims`, which all of them are tested against.
 *
 * The names and values are settings (`AE_IDP_CLAIMS_*`, the ones aep-api
 * reads its own tokens by). This module knows no IdP. Claims a standard names
 * (`sub`, `aud`, `iss`, `exp`, `client_id`, `name`, `email`) are not the
 * dialect's: the verifier reads them by their standard names.
 */

import type { JWTPayload } from "jose";

/**
 * What the platform IdP calls the two things no standard defines: the
 * caller's org, and whether a token was issued to a machine or to a user.
 * Each claim is a top-level text claim of the token. `orgName` and
 * `callerTypeUser` are optional, `""` when not configured.
 */
export interface IdpClaims {
  /** The names of the claims that carry the caller's org: its handle, the IdP's id for it, a display name. */
  orgHandle: string;
  orgId: string;
  orgName: string;
  /** The name of the claim that says who a token was issued to. */
  callerTypeClaim: string;
  /** The value of that claim that means a machine. */
  callerTypeMachine: string;
  /**
   * The value that means a user. When set, only those two values are
   * accepted; when `""`, any value other than the machine's is a user's.
   */
  callerTypeUser: string;
}

/** The mandatory settings, by env key: a dialect cannot work without them. */
const REQUIRED: readonly (readonly [keyof IdpClaims, string])[] = [
  ["orgHandle", "AE_IDP_CLAIMS_ORG_HANDLE"],
  ["orgId", "AE_IDP_CLAIMS_ORG_ID"],
  ["callerTypeClaim", "AE_IDP_CLAIMS_CALLER_TYPE_CLAIM"],
  ["callerTypeMachine", "AE_IDP_CLAIMS_CALLER_TYPE_MACHINE"],
];

/**
 * Reads the pod's claim settings (`AE_IDP_CLAIMS_*`), trimmed. `problems`
 * names each mandatory key that is missing, in the pod config's own wording
 * (`missing KEY`); there are no defaults, since the pod knows no IdP.
 */
export function readIdpClaims(env: Readonly<Record<string, string | undefined>>): { claims: IdpClaims; problems: string[] } {
  const get = (key: string): string => env[key]?.trim() ?? "";
  const claims: IdpClaims = {
    orgHandle: get("AE_IDP_CLAIMS_ORG_HANDLE"),
    orgId: get("AE_IDP_CLAIMS_ORG_ID"),
    orgName: get("AE_IDP_CLAIMS_ORG_NAME"),
    callerTypeClaim: get("AE_IDP_CLAIMS_CALLER_TYPE_CLAIM"),
    callerTypeMachine: get("AE_IDP_CLAIMS_CALLER_TYPE_MACHINE"),
    callerTypeUser: get("AE_IDP_CLAIMS_CALLER_TYPE_USER"),
  };
  const problems = REQUIRED.filter(([field]) => claims[field] === "").map(([, key]) => `missing ${key}`);
  return { claims, problems };
}

/**
 * The caller's organization as the dialect reads it. `handle` is the handle
 * the IdP knows the org by, `id` the IdP's own id for it, `name` a display
 * name; each is `""` when the token carries no such claim. The handle comes
 * from the one handle claim: nothing stands in for it.
 */
export interface OrgIdentity {
  handle: string;
  id: string;
  name: string;
}

/** The dialect's two questions about a verified payload. */
export interface Dialect {
  org(payload: JWTPayload): OrgIdentity;
  /** `true` for a machine's token, `false` for a user's, `undefined` when the token does not say in a way the dialect knows. */
  machine(payload: JWTPayload): boolean | undefined;
}

/** A claim's value when it is text, else `""`: the dialect's claims are text claims. */
const str = (value: unknown): string => (typeof value === "string" ? value : "");

/**
 * The dialect of `claims`. A mandatory name or value left empty throws,
 * naming it: a dialect that could not find the org or tell a machine from a
 * user must not run.
 */
export function createDialect(claims: IdpClaims): Dialect {
  const missing = REQUIRED.filter(([field]) => claims[field].trim() === "").map(([field]) => field);
  if (missing.length > 0) throw new Error(`the platform IdP's claims are incomplete: ${missing.join(", ")} not set`);
  return {
    org(payload) {
      return {
        handle: str(payload[claims.orgHandle]),
        id: str(payload[claims.orgId]),
        name: claims.orgName === "" ? "" : str(payload[claims.orgName]),
      };
    },
    machine(payload) {
      const value = str(payload[claims.callerTypeClaim]);
      if (value === "") return undefined;
      if (value === claims.callerTypeMachine) return true;
      // With a user value configured, a third value is refused rather than
      // taken for a user's; with none, any other value is a user's.
      if (claims.callerTypeUser === "" || value === claims.callerTypeUser) return false;
      return undefined;
    },
  };
}
