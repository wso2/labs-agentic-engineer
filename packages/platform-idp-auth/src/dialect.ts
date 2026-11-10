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
 * The platform IdP's token dialect: the claim names and rules one IdP kind
 * uses to say who a caller is. The verifier reads a token's org and whether
 * it is a machine's through the dialect; nothing else in the pod reads a
 * claim by its IdP-specific name. The TypeScript twin of ae-studio-tools'
 * `internal/auth/dialect` (and of aep-api's), kept in step by hand.
 */

import type { JWTPayload } from "jose";

/** The platform IdP kinds this build reads tokens of. */
export type DialectKind = "thunder";

/**
 * Reads the configured kind (`AE_IDP_KIND`). Empty is thunder, the default a
 * Thunder install never has to set; an unknown value throws, so a pod naming
 * a kind this build does not know fails at boot rather than running on
 * another kind's rules.
 */
export function parseDialectKind(value: string | undefined): DialectKind {
  const kind = (value ?? "").trim().toLowerCase();
  if (kind === "" || kind === "thunder") return "thunder";
  throw new Error(`unknown platform IdP kind "${value}" (supported: thunder)`);
}

/**
 * The caller's organization as the dialect reads it. `handle` is the name
 * the platform knows the org by, `id` the IdP's own id for it, `name` a
 * display name; each is `""` when the token carries no such claim.
 * `handleDeclared` is true when `handle` came from the handle claim itself
 * (Thunder: `ouHandle`), false when it was filled from a fallback (`ouName`,
 * `ouId`) or is empty. The pod's rules require the declared handle.
 */
export interface OrgIdentity {
  handle: string;
  id: string;
  name: string;
  handleDeclared: boolean;
}

/** The dialect's two questions about a verified payload. */
export interface Dialect {
  readonly kind: DialectKind;
  org(payload: JWTPayload): OrgIdentity;
  /** `true` for a machine's token, `false` for a user's, `undefined` when the token says nothing. */
  machine(payload: JWTPayload): boolean | undefined;
}

const str = (value: unknown): string => (typeof value === "string" ? value : "");

/**
 * ThunderID: the org is an organization unit carried as ouHandle / ouName /
 * ouId (the handle falls back to the name, then the id, as on aep-api's user
 * edge), and every token says its grant in grant_type; client_credentials is
 * a machine's.
 */
const thunder: Dialect = {
  kind: "thunder",
  org(payload) {
    const ouHandle = str(payload["ouHandle"]);
    const ouName = str(payload["ouName"]);
    const ouId = str(payload["ouId"]);
    return { handle: ouHandle || ouName || ouId, id: ouId, name: ouName, handleDeclared: ouHandle !== "" };
  },
  machine(payload) {
    const grant = str(payload["grant_type"]);
    if (grant === "") return undefined;
    return grant === "client_credentials";
  },
};

const DIALECTS: Record<DialectKind, Dialect> = { thunder };

/** The dialect of a parsed kind. */
export function dialectFor(kind: DialectKind): Dialect {
  return DIALECTS[kind];
}
