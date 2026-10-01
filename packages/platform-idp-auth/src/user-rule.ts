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

/**
 * The org-only user rule (07 §8): a user token is admitted to the pod when
 * its `ouId` and `ouHandle` both equal the pod's org. The org claim is the
 * only input; this is the one TS place PR #778's scope branch lands. An empty
 * pod org throws: it is a wiring error that could match a token with empty
 * org claims.
 */
export function userRule(claims: UserClaims, pod: { orgId: string; orgHandle: string }): boolean {
  if (!pod.orgId) throw new Error("userRule: pod orgId is required");
  if (!pod.orgHandle) throw new Error("userRule: pod orgHandle is required");
  return claims.ouId === pod.orgId && claims.ouHandle === pod.orgHandle;
}
