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
 * The M2M gate is always on (§12.3.2). The evals and playground boot the real
 * app in-process, so — like any caller — they must present a Bearer token and
 * the per-request model key. This is the CLIENT half: a shared-secret HS256
 * token (fine for a local in-process server) plus the header pair `streamTurn`
 * sends. The service half (verification) lives in `src/shared/auth.ts`.
 */

import { SignJWT } from "jose";

/** The shared-secret auth config the in-process eval/playground server uses. */
export const EVAL_AUTH = {
  audience: "agents-service",
  secret: "local-eval-secret",
} as const;

/** Mint a short-lived HS256 M2M token for `audience`, signed with `secret`. */
export async function signM2mToken(secret: string, audience: string): Promise<string> {
  return new SignJWT({})
    .setProtectedHeader({ alg: "HS256" })
    .setAudience(audience)
    .setIssuedAt()
    .setExpirationTime("1h")
    .sign(new TextEncoder().encode(secret));
}

/**
 * The headers a turn POST needs: the M2M token + the per-request model key
 * (`X-Model-Key`, for whichever connection the turn names), plus `X-Org-Id`
 * (load-bearing for the §12 fence on every turn).
 */
export async function evalTurnHeaders(apiKey: string, orgId?: string): Promise<Record<string, string>> {
  const token = await signM2mToken(EVAL_AUTH.secret, EVAL_AUTH.audience);
  return {
    Authorization: `Bearer ${token}`,
    "X-Model-Key": apiKey,
    ...(orgId !== undefined ? { "X-Org-Id": orgId } : {}),
  };
}
