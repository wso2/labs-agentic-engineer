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

import type { UserClaims } from "./verify.js";

/** Who a verified user token names, for credit and presence. */
export interface DisplayIdentity {
  /** Never empty: `name`, else `given_name` + `family_name`, else `sub`. */
  name: string;
  /** The `email` claim, `""` when the token carries none. */
  email: string;
}

/**
 * The display rule of aep-api's `parseDisplayIdentity`
 * (`A/spec/collab_identity.go`), over verified claims: the `name` claim;
 * else `given_name` + `family_name`, dropping the IdP's placeholder surname
 * "User"; else the subject. The one TS place agent credit (and later collab
 * presence) reads a user's name from.
 */
export function displayIdentity(claims: UserClaims): DisplayIdentity {
  let name = claims.name ?? "";
  if (name === "") {
    const given = (claims.given_name ?? "").trim();
    let family = (claims.family_name ?? "").trim();
    if (family.toLowerCase() === "user") family = "";
    name = `${given} ${family}`.trim();
  }
  if (name === "") name = claims.sub;
  return { name, email: claims.email ?? "" };
}
