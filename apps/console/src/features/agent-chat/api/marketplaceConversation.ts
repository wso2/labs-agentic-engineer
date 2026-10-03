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

// The marketplace register chat's conversation. Unlike a project's thread it
// has no server-held "current" id: the console starts a conversation
// (POST /v1/marketplace/conversations) and remembers the id for the browser
// session, beside the register draft it feeds, so a reload picks the same
// conversation back up. The pod keeps conversations in memory, so a roll
// forgets them — a stored id the pod no longer knows (404) starts fresh.

import { designAgent } from "../../../api/aeStudio";
import { apiErrorMessage } from "../../../api/errors";
import { MARKETPLACE_SCOPE } from "../chatScope";
import { readConversationMessages } from "./turns";

const STORAGE_PREFIX = "aep.chat.conv.";

function storageKey(chatKey: string): string {
  return STORAGE_PREFIX + chatKey;
}

export function storedMarketplaceConversation(chatKey: string): string | null {
  try {
    return sessionStorage.getItem(storageKey(chatKey)) || null;
  } catch {
    return null;
  }
}

function remember(chatKey: string, conversationId: string): void {
  try {
    sessionStorage.setItem(storageKey(chatKey), conversationId);
  } catch {
    // best-effort: without storage a reload simply starts a fresh conversation
  }
}

function forget(chatKey: string): void {
  try {
    sessionStorage.removeItem(storageKey(chatKey));
  } catch {
    // best-effort
  }
}

// One start in flight per chat. The chat panel resolving its conversation and
// the register page starting a fresh one run side by side on a new register;
// a conversation just started is already fresh, so they share it rather than
// racing two (the later answer would orphan the one the other remembered).
const starting = new Map<string, Promise<string>>();

/** Start a conversation on the pod and remember it for this session. */
export function startMarketplaceConversation(chatKey: string): Promise<string> {
  const inFlight = starting.get(chatKey);
  if (inFlight) return inFlight;
  const start = (async () => {
    const { data, error } = await designAgent().POST("/marketplace/conversations");
    if (error !== undefined || data === undefined) {
      throw new Error(apiErrorMessage(error, "Failed to start a new conversation"));
    }
    remember(chatKey, data.conversationId);
    return data.conversationId;
  })().finally(() => starting.delete(chatKey));
  starting.set(chatKey, start);
  return start;
}

/**
 * The conversation this browser session was using, or a fresh one. A stored id
 * the pod answers 404 for (a pod roll) is dropped and `onRestarted` is called
 * so the caller can tell the user their earlier conversation is gone. Any
 * other answer — including no answer — keeps the stored id: the rehydrate
 * that follows has its own recovery.
 */
export async function resolveMarketplaceConversation(
  chatKey: string,
  onRestarted: () => void,
): Promise<string> {
  const stored = storedMarketplaceConversation(chatKey);
  if (stored === null) return startMarketplaceConversation(chatKey);
  const read = await readConversationMessages(MARKETPLACE_SCOPE, stored);
  if (read.kind !== "gone") return stored;
  forget(chatKey);
  onRestarted();
  return startMarketplaceConversation(chatKey);
}
