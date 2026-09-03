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
import type { TurnAnchor } from "@aep/agent-stream";

/**
 * One turn's display record (#463): what the CLIENT sent, verbatim, plus who
 * sent it — the source the get-conversation read serves for user rows, so the
 * composed model prompt (skills, file dumps, framing) never reaches a browser.
 * Journaled beside the transcript, never inside it: `messages` is the model's
 * memory and the prompt-cache prefix, and its bytes must not change.
 */
export interface TurnJournalEntry {
  /** The BFF-minted turn id (WorkspaceRef.turnId). */
  turnId: string;
  /** The raw client-sent instruction — exactly what the sender's UI rendered. */
  text: string;
  /**
   * The acting user, in the console's live author shape ({id: email,
   * displayName}); absent for M2M callers with no human identity.
   */
  author?: { id: string; displayName: string };
  /**
   * File NAMES that rode this message (#428) — never bytes. The display read
   * replaces a user row's content with `text`, so without these a reload would
   * show the agent discussing a document that appears nowhere in the thread.
   */
  attachments?: string[];
  /**
   * What this message was aimed at (#666) — the passage of a spec document the
   * user selected before typing it. Journaled for the same reason as attachment
   * names: the console renders it as a tag above the message, and a record of
   * the words but not the target is not a record of what happened.
   */
  anchor?: TurnAnchor;
  /**
   * Index into `messages` of the user message this turn appended — stamped at
   * write time (the append site knows it exactly), so the display read pairs
   * entry↔message by position stated as fact, never inferred. Journal-less
   * turns simply have no entry claiming their index.
   */
  messageIndex: number;
  createdAt: Date;
}

export interface Conversation {
  /** Caller-supplied id (the BFF owns its id namespace). */
  id: string;
  /**
   * The verbatim `ModelMessage[]` `runTurn` carries — zero-conversion on resume
   * (the wire is raw StreamPart, runTurn is ModelMessage-native), append-only.
   */
  messages: ModelMessage[];
  /**
   * The turn journal (#463), one entry per completed turn, in turn order. A
   * turn appends exactly one user message to `messages`, so the nth user
   * message pairs with the nth entry; pre-journal turns simply have no entry
   * (the read path falls back to the raw message for those).
   */
  turns: TurnJournalEntry[];
  /** `awaiting-human` = the turn ended on a HITL question call (ask_question / ask_questions). */
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
