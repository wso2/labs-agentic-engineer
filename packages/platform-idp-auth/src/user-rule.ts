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

import type { PlatformClaims, UserClaims } from "./verify.js";

/** The pod's org: both values are required. */
export interface PodOrg {
  orgId: string;
  orgHandle: string;
}

/**
 * The org rule (07 §8): a verified token belongs to the pod when its `ouId`
 * and `ouHandle` both equal the pod's org. It reads no other claim, so it
 * serves every token kind, the `ae-studio-<org>` client token included. The
 * org claim is the only input; this is the one TS place PR #778's scope
 * branch lands. An empty pod org throws: it is a wiring error that could
 * match a token with empty org claims.
 */
export function orgRule(claims: PlatformClaims, pod: PodOrg): boolean {
  if (!pod.orgId) throw new Error("orgRule: pod orgId is required");
  if (!pod.orgHandle) throw new Error("orgRule: pod orgHandle is required");
  return claims.ouId === pod.orgId && claims.ouHandle === pod.orgHandle;
}

/**
 * The org-only user rule: the org rule plus a subject. A user token is
 * admitted to the pod when it names a user of the pod's org.
 */
export function userRule(claims: UserClaims, pod: PodOrg): boolean {
  return orgRule(claims, pod) && claims.sub !== "";
}
