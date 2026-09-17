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

import type { ProjectTestUserState } from "../../spec/api/roles";

/**
 * One login the dialog can put on screen: who it is, what it holds, and what
 * that adds up to.
 *
 * `roles` is plural because security.json v2 lets one account hold several —
 * the role it was created for, plus any other role of this project whose group
 * it is a member of. `scopes` is the union of those roles' grants, and it is
 * NOT recomputed here: the platform reads the grants from the identity
 * provider and returns the union already deduplicated, so deriving it a second
 * time on the client would only let the two answers disagree. Empty scopes
 * mean the directory could not be asked — "unknown", not "grants nothing".
 *
 * There is no cold-start field: version 1 served one account to a caller who
 * asked for credentials without naming a role, and version 2 has no such
 * thing.
 */
export interface PublishedTestUser {
  username: string;
  roles: string[];
  scopes: string[];
}

/** First-seen order, no repeats. The platform already deduplicates both
 *  lists; doing it again here is what lets the table key its cells on the
 *  value itself rather than on a position that means nothing. */
function unique(values: readonly string[]): string[] {
  return [...new Set(values)];
}

/** Accounts the sealed store can reveal. `owned` is ADR-0022: the platform holds the password. */
export function publishedTestUsers(
  users: readonly ProjectTestUserState[],
): PublishedTestUser[] {
  return users
    .filter((u) => u.owned)
    .map((u) => ({
      username: u.username,
      roles: unique(u.roles ?? []),
      scopes: unique(u.scopes ?? []),
    }));
}
