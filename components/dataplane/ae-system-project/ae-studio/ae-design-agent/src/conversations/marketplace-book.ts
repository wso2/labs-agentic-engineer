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
 */

import { randomUUID } from "node:crypto";
import type { components } from "../generated/api.js";
import { projectDisplayHistory, type DisplayMessage } from "../conversation/display-history.js";
import type { ConversationStore } from "../store/conversation-store.js";

export type MarketplaceConversation = components["schemas"]["MarketplaceConversation"];

export class MarketplaceBook {
  /** Conversation id → its owner's `sub`. */
  private readonly owners = new Map<string, string>();

  constructor(private readonly store: ConversationStore) {}

  /** A fresh conversation owned by `owner`. */
  create(owner: string): MarketplaceConversation {
    const conversationId = randomUUID();
    this.owners.set(conversationId, owner);
    return { conversationId };
  }

  /** Whether `conversationId` exists and belongs to `owner`. */
  owns(conversationId: string, owner: string): boolean {
    return this.owners.get(conversationId) === owner;
  }

  /** The conversation's display history (`[]` before its first turn), or `null` when `owner` does not own it. */
  async history(conversationId: string, owner: string): Promise<DisplayMessage[] | null> {
    if (!this.owns(conversationId, owner)) return null;
    const conversation = await this.store.get(conversationId);
    return conversation ? projectDisplayHistory(conversation) : [];
  }
}
