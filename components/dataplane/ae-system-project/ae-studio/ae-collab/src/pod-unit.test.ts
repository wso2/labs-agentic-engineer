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
 * Unit tests of the pod pieces that the socket-level tests cannot reach:
 * `onTokenSync` while the IdP is down, the system clock past setTimeout's
 * 2^31-1 ms cap, and the last-leave flush's failure classes.
 */

import { mock, test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";
import type { Connection, Document, Hocuspocus } from "@hocuspocus/server";
import { setDocFile } from "@aep/collab-doc";
import { seedBaseline } from "./committer.js";
import { ApplyConflictError, FilesDeniedError, type FilesClient } from "./files-client.js";
import { commitHooks } from "./pod/commits.js";
import { dropRoomState, ensureRoomState } from "./rooms.js";
import { IdpUnavailableError, type VerifiedToken } from "@aep/platform-idp-auth";
import { onTokenSyncFor, type CollabContext, type Verify } from "./pod/auth.js";
import { systemClock, type ExpiryGuard } from "./pod/expiry.js";
import type { PodLogLine } from "./pod/log.js";

const cfg = { orgId: "ou-acme", orgHandle: "acme", userAudiences: ["aep-console-client"], agentClientId: "ae-studio-acme" };

function fakeConnection(context: CollabContext) {
  const closes: unknown[] = [];
  const connection = { context, close: (e: unknown) => closes.push(e), onClose: () => connection } as unknown as Connection<CollabContext>;
  return { connection, closes };
}

test("onTokenSync: an IdP that cannot be reached closes nothing and moves no deadline", async () => {
  const armed: number[] = [];
  const expiry: ExpiryGuard = { arm: (_c, exp) => armed.push(exp) };
  const lines: PodLogLine[] = [];
  const verify: Verify = () => Promise.reject(new IdpUnavailableError("down"));
  const sync = onTokenSyncFor(cfg, verify, expiry, (l) => lines.push(l));
  const context: CollabContext = {
    listener: "public",
    user: { name: "Ann", email: "", kind: "user" },
    projectName: "greeter",
    exp: 100,
    subject: "u-ann",
  };
  const { connection, closes } = fakeConnection(context);
  await sync({ token: "t", connection });
  assert.deepEqual(closes, []);
  assert.deepEqual(armed, []);
  assert.equal(context.exp, 100);
  assert.deepEqual(lines, [{ msg: "room_token_unverified", source: "ae-collab", listener: "public", cause: "idp_unavailable" }]);
});

test("onTokenSync: a verified token of the same subject moves the deadline, logging no subject change", async () => {
  const armed: number[] = [];
  const lines: PodLogLine[] = [];
  const verified: VerifiedToken = { kind: "user", claims: { sub: "u-ann", ouId: "ou-acme", ouHandle: "acme", exp: 500 } };
  const sync = onTokenSyncFor(cfg, () => Promise.resolve(verified), { arm: (_c, exp) => armed.push(exp) }, (l) => lines.push(l));
  const context: CollabContext = {
    listener: "public",
    user: { name: "Ann", email: "", kind: "user" },
    projectName: "greeter",
    exp: 100,
    subject: "u-ann",
  };
  await sync({ token: "t", connection: fakeConnection(context).connection });
  assert.deepEqual(armed, [500]);
  assert.equal(context.exp, 500);
  assert.deepEqual(lines.map((l) => l.msg), ["room_token_refreshed"]);
});

test("systemClock: a deadline past setTimeout's cap is reached in steps", () => {
  mock.timers.enable({ apis: ["setTimeout", "Date"], now: 0 });
  try {
    const cap = 2 ** 31 - 1;
    let fired = 0;
    systemClock.schedule(cap + 60_000, () => fired++);
    mock.timers.tick(cap);
    assert.equal(fired, 0, "the first step ends at the cap, short of the deadline");
    mock.timers.tick(59_999);
    assert.equal(fired, 0);
    mock.timers.tick(1);
    assert.equal(fired, 1);
    // A cancel stops a pending step.
    const cancel = systemClock.schedule(Date.now() + cap * 2, () => fired++);
    mock.timers.tick(cap);
    cancel();
    mock.timers.tick(cap);
    assert.equal(fired, 1);
  } finally {
    mock.timers.reset();
  }
});

test("systemClock: a deadline already past fires on the next turn, never synchronously", () => {
  mock.timers.enable({ apis: ["setTimeout", "Date"], now: 10_000 });
  try {
    let fired = 0;
    systemClock.schedule(0, () => fired++);
    assert.equal(fired, 0);
    mock.timers.tick(0);
    assert.equal(fired, 1);
  } finally {
    mock.timers.reset();
  }
});

/** A room holding one edited file, and a hook set whose every apply fails with `fail()`. */
function lastLeave(fail: () => Error) {
  const name = "spec-acme-shop";
  dropRoomState(name);
  const doc = new Y.Doc() as Document;
  setDocFile(doc, "specs/a.txt", "seeded");
  seedBaseline(ensureRoomState(name, "shop"), doc, [{ path: "specs/a.txt", content: "seeded", sha: "s1" }]);
  setDocFile(doc, "specs/a.txt", "edited");
  const files: FilesClient = {
    lookup: () => Promise.reject(new Error("unused")),
    bundle: () => Promise.resolve([{ path: "specs/a.txt", content: "outside", sha: "s2" }]),
    apply: () => Promise.reject(fail()),
  };
  const lines: PodLogLine[] = [];
  const commits = commitHooks({ files, log: (l) => lines.push(l), retry: { firstMs: 60_000, maxMs: 60_000 } });
  const instance = { documents: new Map([[name, doc]]), unloadDocument: () => Promise.resolve() } as unknown as Hocuspocus;
  const unload = () => commits.hooks.beforeUnloadDocument({ instance, document: doc, documentName: name });
  return { commits, unload, lines, done: () => dropRoomState(name) };
}

test("last leave: conflicts that keep coming defer the unload instead of dropping the edits", async () => {
  const t = lastLeave(() => new ApplyConflictError([{ path: "specs/a.txt", baseSha: "s1", currentSha: "s2" }]));
  try {
    await assert.rejects(t.unload(), (e: Error) => e.message === "");
    assert.ok(t.lines.some((l) => l.msg === "room_final_flush_deferred"));
  } finally {
    t.commits.stopRetries();
    t.done();
  }
});

test("last leave: an internal fault defers too; only a verdict lets the room unload", async () => {
  const fault = lastLeave(() => new TypeError("boom"));
  try {
    await assert.rejects(fault.unload());
  } finally {
    fault.commits.stopRetries();
    fault.done();
  }
  const verdict = lastLeave(() => new FilesDeniedError("path_invalid", 400));
  try {
    await verdict.unload();
    assert.ok(!verdict.lines.some((l) => l.msg === "room_final_flush_deferred"));
  } finally {
    verdict.commits.stopRetries();
    verdict.done();
  }
});
