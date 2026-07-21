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
 * The conversation repository seam. The whole `Conversation` aggregate is
 * stored/overwritten as ONE unit (load-modify-save), NOT normalized per-message
 * rows — the natural fit for the AI-SDK "persist the array" practice, and
 * history is append-only (§10) so the saved array only ever grows. A Postgres
 * implementation is a single table with a JSONB `messages` column behind this
 * same interface. FILES are never persisted (the repo + the commit service own
 * file truth).
 */

import type { ModelMessage } from "ai";

export interface Conversation {
  /** Caller-supplied id (the BFF owns its id namespace). */
  id: string;
  /**
   * The verbatim `ModelMessage[]` `runTurn` carries — zero-conversion on resume
   * (the wire is raw StreamPart, runTurn is ModelMessage-native), append-only.
   */
  messages: ModelMessage[];
  /** `awaiting-human`: a HITL (`files` toolset) turn ended on an `ask_question` call (ADR-0012). */
  status: "active" | "awaiting-human" | "done";
  /** Store-owned timestamps. */
  createdAt: Date;
  updatedAt: Date;
}

export interface ConversationStore {
  /**
   * Load the aggregate, or `null` if unknown. Implementations MUST return a deep
   * copy so `save()` is the sole commit point (matching Postgres
   * deserialize-on-read; see the in-memory store).
   */
  get(id: string): Promise<Conversation | null>;
  /** Upsert the WHOLE aggregate (last-write-wins for v1). */
  save(c: Conversation): Promise<void>;
}
