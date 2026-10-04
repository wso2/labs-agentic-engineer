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

// What a chat is about. A project chat is the project's shared thread (the
// server resolves its current conversation); a marketplace chat is the
// org-level register conversation, which has no project and no "current" id —
// the console starts one and keeps its id for the browser session
// (marketplaceConversation.ts).
//
// ae-design-agent serves the two on parallel route families (/projects/{p}/…
// and /marketplace/…); this type is how a caller picks one.

import { chatKeyFor, dropChatLogsOfScope } from "./chatStore.js";

export type ChatScope = { kind: "project"; project: string } | { kind: "marketplace" };

export const MARKETPLACE_SCOPE: ChatScope = { kind: "marketplace" };

export function projectScope(project: string): ChatScope {
  return { kind: "project", project };
}

// The segment that tells scopes apart in store and cache keys. A project name
// is a DNS label, so the marketplace segment cannot collide with one.
export function scopeName(scope: ChatScope): string {
  return scope.kind === "project" ? scope.project : "~marketplace";
}

/**
 * The chat log's key for the scope, as `userId` (the access token's `sub`)
 * sees it. A project's log is the project's shared thread. A marketplace
 * conversation belongs to the user who started it, so its log, and the
 * session's conversation id stored under this key, are per user: a second
 * user on the same browser neither paints nor resumes the first user's.
 */
export function chatKeyForScope(org: string, scope: ChatScope, userId: string): string {
  return chatKeyFor(org, scope.kind === "project" ? scopeName(scope) : `${scopeName(scope)}.${userId}`);
}

/**
 * Before the per-user key, every user of a browser shared one marketplace log
 * per org (`aep.chat.v1.<org>.~marketplace`). The console removes those on
 * load, so a later user on the same browser never reads an earlier one's
 * register conversation. The per-user keys (`~marketplace.<sub>`) stay.
 */
export function dropSharedMarketplaceLogs(): void {
  dropChatLogsOfScope(scopeName(MARKETPLACE_SCOPE));
}
