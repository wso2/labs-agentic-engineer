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
 * In-memory `ConversationStore` (a `Map`). `get()` returns a `structuredClone`
 * so `save()` is the SOLE commit point — matching Postgres deserialize-on-read.
 * Without the clone the Map hands back the live object and `runTurn`'s in-place
 * `messages` mutation self-commits before `save()` runs, so the eval (the only
 * local driver) could never catch a dropped `save()` or a partial-turn rollback.
 * Whole-aggregate save, LAST-WRITE-WINS; the store owns the timestamps.
 */

import type { Conversation, ConversationStore } from "./conversation-store.js";

export class InMemoryConversationStore implements ConversationStore {
  private readonly conversations = new Map<string, Conversation>();

  async get(id: string): Promise<Conversation | null> {
    const c = this.conversations.get(id);
    return c ? structuredClone(c) : null;
  }

  async save(c: Conversation): Promise<void> {
    const existing = this.conversations.get(c.id);
    const now = new Date();
    // Store-authoritative timestamps: preserve the first createdAt, bump updatedAt.
    const stored: Conversation = structuredClone({
      ...c,
      createdAt: existing?.createdAt ?? c.createdAt,
      updatedAt: now,
    });
    this.conversations.set(c.id, stored);
  }
}
