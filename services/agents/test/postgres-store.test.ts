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
 * Postgres store logic over a faked pg client — no live database. The fake is a
 * Map-backed `Queryable` that mirrors the store's SQL (jsonb round-trip via
 * JSON, store-authoritative timestamps, updated_at-based sweep) so these assert
 * the same invariants as the in-memory store, plus the TTL sweep.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import type { Conversation } from "../src/store/conversation-store.js";
import { PostgresConversationStore, type Queryable } from "../src/store/postgres-store.js";

interface Row {
  id: string;
  messages: unknown;
  turns: unknown;
  status: string;
  created_at: Date;
  updated_at: Date;
}

/** A Map-backed pg double: dispatches on the SQL verb and reproduces its semantics. */
class FakePg implements Queryable {
  private readonly rows = new Map<string, Row>();
  /** Controllable clock standing in for Postgres `now()`. */
  nowMs = Date.parse("2026-01-01T00:00:00Z");

  private now(): Date {
    return new Date(this.nowMs);
  }

  private snapshot(row: Row): Record<string, unknown> {
    // jsonb comes back freshly parsed; timestamptz comes back as a Date.
    return {
      id: row.id,
      messages: JSON.parse(JSON.stringify(row.messages)),
      turns: JSON.parse(JSON.stringify(row.turns)),
      status: row.status,
      created_at: new Date(row.created_at),
      updated_at: new Date(row.updated_at),
    };
  }

  query(text: string, params: unknown[] = []): Promise<{ rows: Array<Record<string, unknown>> }> {
    const verb = text.trimStart().split(/\s+/)[0]?.toUpperCase();
    switch (verb) {
      case "CREATE":
      case "ALTER":
        return Promise.resolve({ rows: [] });
      case "SELECT": {
        const row = this.rows.get(String(params[0]));
        return Promise.resolve({ rows: row ? [this.snapshot(row)] : [] });
      }
      case "INSERT": {
        const id = String(params[0]);
        const existing = this.rows.get(id);
        this.rows.set(id, {
          id,
          messages: JSON.parse(String(params[1])),
          turns: JSON.parse(String(params[2])),
          status: String(params[3]),
          created_at: existing?.created_at ?? this.now(), // preserved on conflict
          updated_at: this.now(),
        });
        return Promise.resolve({ rows: [] });
      }
      case "DELETE": {
        const cutoff = this.nowMs - Number(params[0]);
        const deleted: Array<Record<string, unknown>> = [];
        for (const [id, row] of this.rows) {
          if (row.updated_at.getTime() < cutoff) {
            this.rows.delete(id);
            deleted.push({ id });
          }
        }
        return Promise.resolve({ rows: deleted });
      }
      default:
        throw new Error(`FakePg: unhandled SQL: ${text}`);
    }
  }

  /** Test helper: force a row's updated_at (to age it past the TTL). */
  ageRow(id: string, updatedAt: Date): void {
    const row = this.rows.get(id);
    if (row) row.updated_at = updatedAt;
  }
}

function fresh(id: string): Conversation {
  return {
    id,
    messages: [{ role: "user", content: "hi" }],
    turns: [],
    status: "active",
    createdAt: new Date("2020-01-01T00:00:00Z"), // ignored: the store owns timestamps
    updatedAt: new Date("2020-01-01T00:00:00Z"),
  };
}

test("init issues an idempotent CREATE TABLE", async () => {
  const store = new PostgresConversationStore(new FakePg());
  await store.init(); // must not throw
});

test("get returns null for an unknown id", async () => {
  const store = new PostgresConversationStore(new FakePg());
  assert.equal(await store.get("nope"), null);
});

test("save then get round-trips the aggregate through jsonb", async () => {
  const store = new PostgresConversationStore(new FakePg());
  await store.save(fresh("c1"));
  const got = await store.get("c1");
  assert.ok(got);
  assert.equal(got.id, "c1");
  assert.equal(got.status, "active");
  assert.equal(got.messages.length, 1);
  assert.ok(got.createdAt instanceof Date && got.updatedAt instanceof Date);
});

test("get returns a deep copy — mutating it does not leak into the store", async () => {
  const store = new PostgresConversationStore(new FakePg());
  await store.save(fresh("c1"));

  const a = await store.get("c1");
  assert.ok(a);
  a.messages.push({ role: "assistant", content: "leaked?" });

  const b = await store.get("c1");
  assert.equal(b?.messages.length, 1, "the jsonb round-trip must hand back a fresh copy");
});

test("save upserts; createdAt is preserved, updatedAt advances", async () => {
  const db = new FakePg();
  const store = new PostgresConversationStore(db);
  await store.save(fresh("c1"));
  const first = await store.get("c1");
  assert.ok(first);

  db.nowMs += 60_000; // advance the store clock
  const next = fresh("c1");
  next.messages = [
    { role: "user", content: "hi" },
    { role: "assistant", content: "hello" },
  ];
  next.status = "done";
  await store.save(next);

  const second = await store.get("c1");
  assert.ok(second);
  assert.equal(second.messages.length, 2);
  assert.equal(second.status, "done");
  assert.equal(second.createdAt.getTime(), first.createdAt.getTime(), "createdAt is first-seen");
  assert.ok(second.updatedAt.getTime() > first.updatedAt.getTime(), "updatedAt advances on upsert");
});

test("sweepExpired deletes rows past the TTL and keeps fresh ones", async () => {
  const db = new FakePg();
  const store = new PostgresConversationStore(db);
  await store.save(fresh("stale"));
  await store.save(fresh("fresh"));

  const DAY = 24 * 60 * 60 * 1000;
  db.ageRow("stale", new Date(db.nowMs - 10 * DAY)); // 10 days old

  const purged = await store.sweepExpired(DAY); // TTL = 1 day
  assert.equal(purged, 1);
  assert.equal(await store.get("stale"), null);
  assert.ok(await store.get("fresh"));
});
