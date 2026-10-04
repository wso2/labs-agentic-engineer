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
 * The pod's marketplace conversations (07 §6). A marketplace conversation has
 * no project: each belongs to the user who created it (the JWT `sub`), and
 * every call naming it must come from that user; to anyone else it does not
 * exist. Ids are plain uuids; messages stay behind the `ConversationStore`
 * port, as for project threads. Nothing outlives the process. The console's
 * "rotate on Start" is a new conversation.
 *
 * Memory is bounded (R2-I2): a conversation unused for `MARKETPLACE_IDLE_MS`
 * is evicted, and an owner keeps at most `MARKETPLACE_PER_OWNER` (a new one
 * evicts the owner's least recently used). Any call of the owner naming a
 * conversation is a use. Eviction drops the owner entry and the stored
 * messages, so the id answers 404 and the console's 404 recovery starts a new
 * conversation. A conversation whose turn runs is never evicted (its run
 * would save the messages back); it goes once the turn has ended, so the cap
 * is exceeded only while more of an owner's turns run than the cap.
 */

import { randomUUID } from "node:crypto";
import type { components } from "../generated/api.js";
import { projectDisplayHistory, type DisplayMessage } from "../conversation/display-history.js";
import type { ConversationStore } from "../store/conversation-store.js";

export type MarketplaceConversation = components["schemas"]["MarketplaceConversation"];

/** A marketplace conversation unused this long is evicted. */
export const MARKETPLACE_IDLE_MS = 2 * 60 * 60_000;
/** The conversations one owner keeps; a new one evicts the least recently used. */
export const MARKETPLACE_PER_OWNER = 5;

export interface MarketplaceBookOptions {
  now?: () => number;
  /** Whether a turn runs in the conversation (the desk's lock): never evicted while it does. */
  busy?: (conversationId: string) => boolean;
}

interface Entry {
  owner: string;
  usedAt: number;
}

export class MarketplaceBook {
  /** Conversation id → its owner's `sub` and last use; insertion order is creation order. */
  private readonly entries = new Map<string, Entry>();
  private readonly now: () => number;
  private readonly busy: (conversationId: string) => boolean;

  constructor(
    private readonly store: ConversationStore,
    opts: MarketplaceBookOptions = {},
  ) {
    this.now = opts.now ?? Date.now;
    this.busy = opts.busy ?? (() => false);
  }

  /** A fresh conversation owned by `owner`; the owner's least recently used goes past the cap. */
  async create(owner: string): Promise<MarketplaceConversation> {
    await this.evictIdle();
    let owned = [...this.entries.values()].filter((e) => e.owner === owner).length;
    const leastRecentlyUsed = [...this.entries]
      .filter(([id, e]) => e.owner === owner && !this.busy(id))
      .sort(([, a], [, b]) => a.usedAt - b.usedAt);
    for (const [id] of leastRecentlyUsed) {
      if (owned < MARKETPLACE_PER_OWNER) break;
      await this.evict(id);
      owned--;
    }
    const conversationId = randomUUID();
    this.entries.set(conversationId, { owner, usedAt: this.now() });
    return { conversationId };
  }

  /** Whether `conversationId` exists and belongs to `owner`; a yes is a use. */
  owns(conversationId: string, owner: string): boolean {
    const entry = this.entries.get(conversationId);
    if (!entry || entry.owner !== owner) return false;
    if (this.idle(conversationId, entry)) {
      // The id is unknown from here on; the stored messages go in the
      // background (a failed delete leaves them for the pod's life, never
      // served: nothing names the id any more).
      this.evict(conversationId).catch(() => {});
      return false;
    }
    entry.usedAt = this.now();
    return true;
  }

  /** The conversation's display history (`[]` before its first turn), or `null` when `owner` does not own it. */
  async history(conversationId: string, owner: string): Promise<DisplayMessage[] | null> {
    if (!this.owns(conversationId, owner)) return null;
    const conversation = await this.store.get(conversationId);
    return conversation ? projectDisplayHistory(conversation) : [];
  }

  private idle(conversationId: string, entry: Entry): boolean {
    return this.now() - entry.usedAt > MARKETPLACE_IDLE_MS && !this.busy(conversationId);
  }

  private async evictIdle(): Promise<void> {
    for (const [id, entry] of [...this.entries]) if (this.idle(id, entry)) await this.evict(id);
  }

  private async evict(conversationId: string): Promise<void> {
    this.entries.delete(conversationId);
    await this.store.delete(conversationId);
  }
}
