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
 * `FileConversationStore` — the playground's `ConversationStore` adapter
 * (docs/design/playground.md §3): one JSON file per conversation under
 * `<project>/.aep-playground/conversations/`. The `general` conversation is
 * `general.json` (ONE spec conversation per project — console parity); plan
 * turns are one-shot `task-plan-<uuid>.json` files.
 *
 * - Writes are atomic (tmp + rename) so a crash mid-save never corrupts the
 *   history.
 * - A conversation loaded back with `status: "active"` is a stale artifact of
 *   a killed process (the playground is a foreground CLI — nothing else can
 *   be mid-turn), so it resets to "done" on load.
 */

import { existsSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import type { Conversation, ConversationStore } from "@aep/agents/store/conversation-store";

interface StoredConversation {
  id: string;
  messages: Conversation["messages"];
  /** Turn journal (#463); absent in files written before it existed. */
  turns?: Conversation["turns"];
  status: Conversation["status"];
  createdAt: string;
  updatedAt: string;
}

/**
 * File name for a namespaced conversation id
 * (`org_<o>--proj_<p>--<useCase>--<uuid>`): the `general` conversation is a
 * per-project singleton; anything else keys on useCase + uuid (one-shots).
 */
export function conversationFileName(id: string): string {
  const segments = id.split("--");
  if (segments.length !== 4) return `${sanitize(id)}.json`;
  const [, , useCase, uuid] = segments as [string, string, string, string];
  return useCase === "general" ? "general.json" : `${sanitize(useCase)}-${sanitize(uuid)}.json`;
}

function sanitize(v: string): string {
  return v.replace(/[^A-Za-z0-9._-]/g, "_");
}

export class FileConversationStore implements ConversationStore {
  constructor(private readonly dir: string) {}

  private fileFor(id: string): string {
    return join(this.dir, conversationFileName(id));
  }

  async get(id: string): Promise<Conversation | null> {
    const file = this.fileFor(id);
    if (!existsSync(file)) return null;
    const raw = JSON.parse(readFileSync(file, "utf8")) as StoredConversation;
    return {
      id: raw.id,
      messages: raw.messages,
      // Nested dates need the same revival as the top-level ones — JSON hands
      // back ISO strings where the type says Date.
      turns: (raw.turns ?? []).map((t) => ({ ...t, createdAt: new Date(t.createdAt) })),
      // A persisted "active" is always stale here (see module doc).
      status: raw.status === "active" ? "done" : raw.status,
      createdAt: new Date(raw.createdAt),
      updatedAt: new Date(raw.updatedAt),
    };
  }

  async save(c: Conversation): Promise<void> {
    mkdirSync(this.dir, { recursive: true });
    const stored: StoredConversation = {
      id: c.id,
      messages: c.messages,
      turns: c.turns,
      status: c.status,
      createdAt: c.createdAt.toISOString(),
      updatedAt: c.updatedAt.toISOString(),
    };
    const file = this.fileFor(c.id);
    const tmp = `${file}.tmp`;
    writeFileSync(tmp, JSON.stringify(stored, null, 2), "utf8");
    renameSync(tmp, file);
  }

  /** `--fresh`: drop a conversation's history (next turn starts clean). */
  reset(id: string): void {
    rmSync(this.fileFor(id), { force: true });
  }
}
