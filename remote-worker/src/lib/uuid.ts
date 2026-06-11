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

// RFC 4122 UUID, any version. Strict — case insensitive but no surrounding
// braces, no truncation, no other characters allowed.
const UUID_RE =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function isUUID(s: unknown): s is string {
  return typeof s === "string" && UUID_RE.test(s);
}

// DNS-label-shaped slug (lowercase). Matches what git-service uses for
// ocOrgID validation and what the BFF generates for project names.
// Rejects path traversal (`..`, `/`, leading dots), shell metacharacters,
// uppercase, and overlong values. ≤ 63 chars per DNS label.
const SLUG_RE = /^[a-z0-9][a-z0-9-]{0,62}$/;

export function isSlug(s: unknown): s is string {
  return typeof s === "string" && SLUG_RE.test(s);
}
