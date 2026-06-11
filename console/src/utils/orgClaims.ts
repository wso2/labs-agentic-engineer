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

import type { UserClaims } from '../auth/useUserClaims';

/**
 * Resolve the canonical OC org handle from a verified JWT, preferring
 * `ouHandle` over `ouName` over `ouId`. Returns `undefined` when the
 * token has none of those claims — callers MUST surface this as a
 * fail-loud error rather than silently substitute an org.
 *
 * The BFF mirrors this precedence verbatim
 * (asdlc-service/middleware/jwt.ResolveOuHandle). Any change here MUST
 * land on both sides simultaneously.
 */
export function resolveOuHandle(claims: UserClaims | null | undefined): string | undefined {
  if (!claims) return undefined;
  return claims.ouHandle || claims.ouName || claims.ouId || undefined;
}
