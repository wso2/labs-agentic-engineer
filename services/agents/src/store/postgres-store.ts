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
 * Postgres-backed `ConversationStore` (§12.3.4): one table, a JSONB `messages`
 * column, whole-aggregate load-modify-save behind the same interface as the
 * in-memory store. Timestamps are store-authoritative (first `created_at`
 * preserved, `updated_at` bumped) — matching the in-memory store's contract.
 *
 * It depends only on a minimal `Queryable` (which `pg.Pool` satisfies), never on
 * `pg` directly, so the SQL and (de)serialization are unit-tested with a faked
 * client and `npm test` needs no live Postgres. The composition root owns the
 * real pool, `init()`, and the TTL sweep.
 */

import type { ModelMessage } from "ai";
import type { Conversation, ConversationStore, TurnJournalEntry } from "./conversation-store.js";

/** The subset of a `pg.Pool`/`pg.Client` this store uses. */
export interface Queryable {
  query(text: string, params?: unknown[]): Promise<{ rows: Array<Record<string, unknown>> }>;
}

const CREATE_TABLE = `CREATE TABLE IF NOT EXISTS conversations (
  id text PRIMARY KEY,
  messages jsonb NOT NULL,
  turns jsonb NOT NULL DEFAULT '[]',
  status text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
)`;

// Pre-journal deployments created the table without `turns` (#463); CREATE
// TABLE IF NOT EXISTS never adds a column, so bootstrap alters idempotently.
// The ALTER runs only when the probe says the column is missing: Postgres
// takes the table's ACCESS EXCLUSIVE lock BEFORE evaluating IF NOT EXISTS, so
// an every-boot ALTER queued behind a long transaction would stall every
// reader on the table; the probe costs one catalog read instead.
const TURNS_COLUMN_EXISTS = `SELECT 1 FROM information_schema.columns
  WHERE table_schema = current_schema() AND table_name = 'conversations' AND column_name = 'turns'`;

const ADD_TURNS_COLUMN = `ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS turns jsonb NOT NULL DEFAULT '[]'`;

const SELECT_ONE = `SELECT id, messages, turns, status, created_at, updated_at
  FROM conversations WHERE id = $1`;

// created_at is untouched on conflict (store-authoritative first-seen); only the
// messages/turns/status/updated_at advance.
const UPSERT = `INSERT INTO conversations (id, messages, turns, status, created_at, updated_at)
  VALUES ($1, $2::jsonb, $3::jsonb, $4, now(), now())
  ON CONFLICT (id) DO UPDATE
    SET messages = EXCLUDED.messages,
        turns = EXCLUDED.turns,
        status = EXCLUDED.status,
        updated_at = now()`;

const SWEEP = `DELETE FROM conversations
  WHERE updated_at < now() - (interval '1 millisecond' * $1::double precision)
  RETURNING id`;

function asDate(value: unknown): Date {
  return value instanceof Date ? value : new Date(String(value));
}

/** jsonb comes back already parsed by node-postgres; timestamptz comes back as a Date. */
function rowToConversation(row: Record<string, unknown>): Conversation {
  const turns = ((row.turns ?? []) as Array<Record<string, unknown>>).map((t): TurnJournalEntry => {
    const author = t.author as { id?: unknown; displayName?: unknown } | undefined;
    return {
      turnId: String(t.turnId ?? ""),
      text: String(t.text ?? ""),
      ...(author && typeof author.id === "string" && typeof author.displayName === "string"
        ? { author: { id: author.id, displayName: author.displayName } }
        : {}),
      messageIndex: Number(t.messageIndex ?? -1),
      createdAt: asDate(t.createdAt),
    };
  });
  return {
    id: String(row.id),
    messages: (row.messages ?? []) as ModelMessage[],
    turns,
    status: row.status as Conversation["status"],
    createdAt: asDate(row.created_at),
    updatedAt: asDate(row.updated_at),
  };
}

export class PostgresConversationStore implements ConversationStore {
  constructor(private readonly db: Queryable) {}

  /** Idempotent schema bootstrap; safe to call on every startup. */
  async init(): Promise<void> {
    await this.db.query(CREATE_TABLE);
    const { rows } = await this.db.query(TURNS_COLUMN_EXISTS);
    if (rows.length === 0) await this.db.query(ADD_TURNS_COLUMN);
  }

  async get(id: string): Promise<Conversation | null> {
    const { rows } = await this.db.query(SELECT_ONE, [id]);
    const row = rows[0];
    return row ? rowToConversation(row) : null;
  }

  async save(c: Conversation): Promise<void> {
    await this.db.query(UPSERT, [c.id, JSON.stringify(c.messages), JSON.stringify(c.turns), c.status]);
  }

  /** Delete rows whose `updated_at` is older than `ttlMs`. Returns the count purged. */
  async sweepExpired(ttlMs: number): Promise<number> {
    const { rows } = await this.db.query(SWEEP, [ttlMs]);
    return rows.length;
  }
}
