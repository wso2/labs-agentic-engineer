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
 * The shared sample tokens (`test/fixtures/idp-claims`): for each IdP AE ships
 * claim values for, those values, sample token payloads and how every reader
 * of the platform IdP's tokens must read them. aep-api's dialect, the pod's
 * Go twin and the console run the same files.
 */

import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { IdpClaims } from "../../src/index.js";

export interface ClaimsFixture {
  idp: string;
  claims: {
    org: { handle: string; id: string; name?: string };
    callerType: { claim: string; machine: string; user?: string };
  };
  tokens: {
    name: string;
    payload: Record<string, unknown>;
    org: { handle: string; id: string; name: string };
    caller: "user" | "machine" | "refused";
  }[];
}

export function loadFixture(idp: "thunder" | "asgardeo"): ClaimsFixture {
  return JSON.parse(readFileSync(join(import.meta.dirname, "../fixtures/idp-claims", `${idp}.json`), "utf8")) as ClaimsFixture;
}

/** A fixture's claim values in the shape the verifier takes. */
export function claimsOf(fixture: ClaimsFixture): IdpClaims {
  const { org, callerType } = fixture.claims;
  return {
    orgHandle: org.handle,
    orgId: org.id,
    orgName: org.name ?? "",
    callerTypeClaim: callerType.claim,
    callerTypeMachine: callerType.machine,
    callerTypeUser: callerType.user ?? "",
  };
}

/** How a ThunderID token names the org and the caller type. */
export const THUNDER: IdpClaims = claimsOf(loadFixture("thunder"));
/** How an Asgardeo token names the org and the caller type. */
export const ASGARDEO: IdpClaims = claimsOf(loadFixture("asgardeo"));
