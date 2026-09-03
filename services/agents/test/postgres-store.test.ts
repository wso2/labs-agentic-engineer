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
import { PostgresConversationStore, sanitizeForJsonb, type Queryable } from "../src/store/postgres-store.js";

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

// --- NUL sanitization at the persistence boundary (#384) ---------------------
//
// PostgreSQL's jsonb column rejects any string containing the U+0000 escape
// sequence ("unsupported Unicode escape sequence") — real turns hit this when
// a tool result contained embedded NUL bytes (e.g. a binary file read as
// "text"), which killed the INSERT and, with it, the whole turn.

/** A pg double that records the exact params of the last INSERT, unparsed. */
class RecordingPg implements Queryable {
  lastInsertParams: unknown[] | undefined;
  query(text: string, params: unknown[] = []): Promise<{ rows: Array<Record<string, unknown>> }> {
    if (text.trimStart().toUpperCase().startsWith("INSERT")) {
      this.lastInsertParams = params;
    }
    return Promise.resolve({ rows: [] });
  }
}

test("sanitizeForJsonb recursively replaces U+0000 with U+FFFD; everything else is byte-identical", () => {
  const input = {
    a: "keep me",
    b: ["x\u0000y", 42, null, true],
    c: { nested: "z\u0000\u0000z", untouched: "fine" },
  };
  const out = sanitizeForJsonb(input);
  assert.deepEqual(out, {
    a: "keep me",
    b: ["x�y", 42, null, true],
    c: { nested: "z��z", untouched: "fine" },
  });
  // The original value is untouched (no in-place mutation).
  assert.equal(input.b[0], "x\u0000y");
});

// Postgres refuses the codepoint anywhere in a jsonb document, keys included —
// sanitizing only the leaves would still lose the turn to a NUL-bearing key.
// A Date has no enumerable own properties, so the plain-object rebuild used to
// replace it with {} — and the journal then read back an Invalid Date on every
// save. Non-plain objects pass through so JSON.stringify can apply toJSON.
test("sanitizeForJsonb preserves a Date through the JSON round trip", () => {
  const createdAt = new Date("2026-08-17T10:00:00.000Z");

  const out = sanitizeForJsonb({ turns: [{ turnId: "t1", createdAt }] });
  const round = JSON.parse(JSON.stringify(out)) as {
    turns: Array<{ createdAt: string }>;
  };
  const back = new Date(round.turns[0]!.createdAt);

  assert.equal(Number.isNaN(back.getTime()), false, "createdAt round-tripped as an Invalid Date");
  assert.equal(back.toISOString(), createdAt.toISOString());
});

// The pass-through must not cost the NUL scrub anywhere it still applies.
test("sanitizeForJsonb still scrubs NULs in strings beside a Date", () => {
  const out = sanitizeForJsonb({
    createdAt: new Date("2026-08-17T10:00:00.000Z"),
    text: "before\u0000after",
  }) as { createdAt: Date; text: string };

  assert.equal(out.text, "before\ufffdafter");
  assert.equal(out.createdAt instanceof Date, true);
});

test("sanitizeForJsonb replaces U+0000 in object keys, at any depth", () => {
  const NUL = String.fromCharCode(0);
  const out = sanitizeForJsonb({
    [`top${NUL}key`]: "v",
    nested: { [`deep${NUL}key`]: [`a${NUL}b`] },
  });
  assert.deepEqual(out, {
    "top�key": "v",
    nested: { "deep�key": ["a�b"] },
  });
  assert.equal(JSON.stringify(out).includes(NUL), false);
});

test("save strips the NUL escape sequence from the jsonb payload sent to Postgres", async () => {
  const db = new RecordingPg();
  const store = new PostgresConversationStore(db);
  const withNul: Conversation = {
    id: "nul1",
    messages: [
      { role: "user", content: "hi" },
      {
        role: "tool",
        content: [
          {
            type: "tool-result",
            toolCallId: "t1",
            toolName: "loadSkillReference",
            output: { type: "text", value: "binary junk: \u0000\u0000 more text" },
          },
        ],
      },
    ],
    turns: [],
    status: "done",
    createdAt: new Date(),
    updatedAt: new Date(),
  };

  await store.save(withNul);

  const payload = String(db.lastInsertParams?.[1]);
  assert.equal(payload.includes("\u0000"), false, "no literal NUL byte in the jsonb payload");
  assert.equal(payload.includes("\\u0000"), false, "no NUL unicode escape — this is what Postgres rejects");
  assert.match(payload, /binary junk: �� more text/, "the NUL was replaced with U+FFFD, not dropped");
});

test("save/get round-trips a NUL-bearing message through a real jsonb-shaped store", async () => {
  const db = new FakePg();
  const store = new PostgresConversationStore(db);
  await store.save({
    id: "nul2",
    messages: [
      { role: "user", content: "hi" },
      {
        role: "tool",
        content: [
          { type: "tool-result", toolCallId: "t1", toolName: "x", output: { type: "text", value: "a\u0000b" } },
        ],
      },
    ],
    turns: [],
    status: "done",
    createdAt: new Date(),
    updatedAt: new Date(),
  });

  const got = await store.get("nul2");
  assert.ok(got);
  const toolMsg = got.messages[1] as { content: Array<{ output: { value: string } }> };
  assert.equal(toolMsg.content[0]?.output.value, "a�b");
  // The rest of the aggregate is untouched.
  assert.equal((got.messages[0] as { content: string }).content, "hi");
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

// The bug this pins: attachment names were WRITTEN to the turns jsonb correctly
// and dropped on the way back OUT, because rowToConversation rebuilds each entry
// field by field. A chip therefore showed while the turn was live and vanished
// the moment the thread rehydrated — the write side looked perfect.
test("the journal's attachment names survive the jsonb round-trip", async () => {
  const store = new PostgresConversationStore(new FakePg());
  await store.save({
    ...fresh("c-attach"),
    turns: [
      {
        turnId: "t1",
        text: "add this form as well",
        messageIndex: 0,
        createdAt: new Date("2026-01-01T00:00:00Z"),
        attachments: ["2025-Motor Claim Form.pdf"],
      },
    ],
  });
  const got = await store.get("c-attach");
  assert.ok(got);
  assert.deepEqual(got.turns[0]?.attachments, ["2025-Motor Claim Form.pdf"]);
});

test("a turn with no attachments round-trips without the field", async () => {
  const store = new PostgresConversationStore(new FakePg());
  await store.save({
    ...fresh("c-plain"),
    turns: [{ turnId: "t1", text: "hello", messageIndex: 0, createdAt: new Date(0) }],
  });
  const got = await store.get("c-plain");
  assert.ok(got);
  assert.equal("attachments" in (got.turns[0] ?? {}), false);
});

test("a malformed attachments value reads back as absent, not as blank chips", async () => {
  // The column is jsonb written by an older or buggier build; a non-string entry
  // must not reach the UI as an empty chip.
  const store = new PostgresConversationStore(new FakePg());
  await store.save({
    ...fresh("c-bad"),
    turns: [
      {
        turnId: "t1",
        text: "hi",
        messageIndex: 0,
        createdAt: new Date(0),
        attachments: ["ok.pdf", "", 42, null] as unknown as string[],
      },
    ],
  });
  const got = await store.get("c-bad");
  assert.ok(got);
  assert.deepEqual(got.turns[0]?.attachments, ["ok.pdf"]);
});

// The SAME bug, one field later (#666). The comment on rowToConversation warns
// that a new journal field has to be added there too — and the anchor was
// written to the turns jsonb correctly and dropped on the way back out anyway,
// caught only by walking the real stack. A tag showed while the turn was live
// and vanished on rehydrate, which is precisely the failure the journal exists
// to prevent.
const ANCHOR = {
  file: "specs/requirements/prd.md",
  nodes: [
    {
      name: "Manager — reviews expense claims submitted by their direct reports",
      kind: "list item",
      context: "Employees Submit Expense — PRD › Actors",
    },
  ],
};

test("the journal's anchor survives the jsonb round-trip", async () => {
  const store = new PostgresConversationStore(new FakePg());
  await store.save({
    ...fresh("c-anchor"),
    turns: [
      {
        turnId: "t1",
        text: "say explicitly that a manager cannot approve their own claim",
        messageIndex: 0,
        createdAt: new Date("2026-08-30T00:00:00Z"),
        anchor: ANCHOR,
      },
    ],
  });
  const got = await store.get("c-anchor");
  assert.ok(got);
  assert.deepEqual(got.turns[0]?.anchor, ANCHOR);
});

test("a turn with no anchor round-trips without the field", async () => {
  const store = new PostgresConversationStore(new FakePg());
  await store.save({
    ...fresh("c-unaimed"),
    turns: [{ turnId: "t1", text: "hello", messageIndex: 0, createdAt: new Date(0) }],
  });
  const got = await store.get("c-unaimed");
  assert.ok(got);
  assert.equal("anchor" in (got.turns[0] ?? {}), false);
});

// Whole, not partial: a tag naming fewer nodes than the user selected points at
// a selection they never made.
test("a malformed anchor reads back as absent, not as a half-tag", async () => {
  const store = new PostgresConversationStore(new FakePg());
  await store.save({
    ...fresh("c-bad-anchor"),
    turns: [
      {
        turnId: "t1",
        text: "shorter",
        messageIndex: 0,
        createdAt: new Date(0),
        anchor: {
          file: "a.md",
          nodes: [{ name: "ok", kind: "paragraph" }, { name: "no kind" }],
        } as unknown as typeof ANCHOR,
      },
    ],
  });
  const got = await store.get("c-bad-anchor");
  assert.ok(got);
  assert.equal("anchor" in (got.turns[0] ?? {}), false);
});
