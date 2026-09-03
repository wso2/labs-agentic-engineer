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
 *
 * `save()` runs every message through `sanitizeForJsonb` before it is
 * stringified (#384): PostgreSQL's jsonb type rejects any string containing a
 * U+0000 codepoint ("unsupported Unicode escape sequence") — surfaced by a real
 * turn where a tool pulled a binary file's bytes in as "text" (an 868KB PDF
 * read through an MCP tool), which then killed the INSERT and, with it, the
 * whole turn (the BFF only ever saw "stream ended without a manifest"). This is
 * the ONE choke point every write passes through, so no call site upstream has
 * to know the rule or remember to apply it.
 */

import type { ModelMessage } from "ai";
import type { Conversation, ConversationStore, TurnJournalEntry } from "./conversation-store.js";
import type { TurnAnchor } from "@aep/agent-stream";

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

/**
 * Recursively replace every U+0000 codepoint in a JSON-shaped value's strings —
 * leaves AND object keys — with U+FFFD (the Unicode replacement character),
 * leaving everything else (other characters, array order, non-string values)
 * byte-identical. Never mutates its input. `unknown` in/out rather than typed
 * to `ModelMessage[]`: this is a structural JSON transform, not a message-shape
 * one, and it must apply uniformly regardless of which part type carries the
 * NUL (tool-result output, text, anything future).
 */
export function sanitizeForJsonb<T>(value: T): T {
  if (typeof value === "string") {
    return (value.includes("\u0000") ? value.replaceAll("\u0000", "�") : value) as T;
  }
  if (Array.isArray(value)) {
    return value.map((v) => sanitizeForJsonb(v)) as T;
  }
  // PLAIN objects only. A `Date` (or any class instance) has no enumerable own
  // properties, so `Object.entries` returns [] and the rebuild below would
  // replace it with `{}` — silently destroying it. That is not hypothetical
  // here: every TurnJournalEntry carries a `createdAt: Date`, so the whole
  // journal used to persist `{}` and read back `new Date("[object Object]")`,
  // an Invalid Date, on every save.
  //
  // Passing non-plain objects through is safe because the caller hands the
  // result straight to JSON.stringify, which applies `toJSON` itself — a Date
  // serializes to its ISO string exactly as it would have without this
  // function. The inputs are JSON-shaped values plus Dates; there is no
  // NUL-bearing exotic object for the pass-through to miss.
  if (value !== null && typeof value === "object" && isPlainObject(value)) {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(value as Record<string, unknown>)) {
      // The key goes through the same transform as the value: Postgres refuses
      // the codepoint anywhere in a jsonb document, so a NUL carried by a key
      // kills the write exactly as one in a value does. Two keys that differ
      // only by a NUL collapse into one, last write winning — persisting the
      // turn beats preserving a distinction Postgres would not store either way.
      out[sanitizeForJsonb(k)] = sanitizeForJsonb(v);
    }
    return out as T;
  }
  return value;
}

/** Own-enumerable-properties objects — `{}` literals and null-prototype maps. */
function isPlainObject(value: object): boolean {
  const proto = Object.getPrototypeOf(value) as unknown;
  return proto === Object.prototype || proto === null;
}

function asDate(value: unknown): Date {
  return value instanceof Date ? value : new Date(String(value));
}

/**
 * One journalled anchor off a jsonb column, or undefined.
 *
 * Defensive for the same reason the rest of `rowToConversation` is — this is a
 * read of data written by an older build as easily as by this one — and total:
 * a partially-read anchor would render a tag for a selection that is not the
 * one the user made.
 */
function anchorOf(raw: unknown): TurnAnchor | undefined {
  if (raw === null || typeof raw !== "object" || !isPlainObject(raw)) return undefined;
  const { file, nodes } = raw as { file?: unknown; nodes?: unknown };
  if (typeof file !== "string" || file === "" || !Array.isArray(nodes) || nodes.length === 0) {
    return undefined;
  }
  const mapped: TurnAnchor["nodes"] = [];
  for (const node of nodes) {
    if (node === null || typeof node !== "object" || !isPlainObject(node)) return undefined;
    const { name, kind, context } = node as { name?: unknown; kind?: unknown; context?: unknown };
    if (typeof name !== "string" || name === "" || typeof kind !== "string" || kind === "") {
      return undefined;
    }
    mapped.push({ name, kind, ...(typeof context === "string" && context ? { context } : {}) });
  }
  return { file, nodes: mapped };
}

/** jsonb comes back already parsed by node-postgres; timestamptz comes back as a Date. */
function rowToConversation(row: Record<string, unknown>): Conversation {
  const turns = ((row.turns ?? []) as Array<Record<string, unknown>>).map((t): TurnJournalEntry => {
    const author = t.author as { id?: unknown; displayName?: unknown } | undefined;
    // Attachment names (#428). Rebuilt field-by-field like the rest of this
    // entry — a defensive read of a jsonb column, not a spread — which is
    // exactly why a new field has to be added HERE too: the names were written
    // correctly and silently dropped on the way back out, so a chip showed while
    // the turn was live and vanished the moment the thread rehydrated.
    const attachments = Array.isArray(t.attachments)
      ? (t.attachments as unknown[]).filter((n): n is string => typeof n === "string" && n !== "")
      : [];
    // The anchor (#666) — read back with the same defensive rebuild, and
    // dropped WHOLE when any part of it is malformed: a tag naming fewer nodes
    // than the user selected is a quieter, worse failure than no tag.
    const anchor = anchorOf(t.anchor);
    return {
      turnId: String(t.turnId ?? ""),
      text: String(t.text ?? ""),
      ...(author && typeof author.id === "string" && typeof author.displayName === "string"
        ? { author: { id: author.id, displayName: author.displayName } }
        : {}),
      ...(attachments.length > 0 ? { attachments } : {}),
      ...(anchor ? { anchor } : {}),
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
    // Both jsonb columns go through the sanitizer, not just messages: the turn
    // journal carries the user's own text, and Postgres refuses U+0000 in any
    // jsonb document — one NUL in either column loses the whole write.
    await this.db.query(UPSERT, [
      c.id,
      JSON.stringify(sanitizeForJsonb(c.messages)),
      JSON.stringify(sanitizeForJsonb(c.turns)),
      c.status,
    ]);
  }

  /** Delete rows whose `updated_at` is older than `ttlMs`. Returns the count purged. */
  async sweepExpired(ttlMs: number): Promise<number> {
    const { rows } = await this.db.query(SWEEP, [ttlMs]);
    return rows.length;
  }
}
