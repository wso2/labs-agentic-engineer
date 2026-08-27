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
 * A room the server could not seed must not open (#586).
 *
 * Observed as a blank PRD on a project whose requirements are 4993 bytes in
 * git, after an `aep-api` restart while `aep-collab` stayed up. The seed threw,
 * the failure was swallowed, and the room opened empty — permanently, because a
 * room stays loaded while any client is connected. The console could not tell
 * that room from an empty project, so it suppressed the committed-git fallback;
 * an agent turn joined it, synced perfectly, and was told the project had no
 * files; and every flush 409ed, because a baseline that was never populated
 * writes with `baseSha: ""`, which the Files API reads as "must not exist".
 *
 * These run the REAL server against a REAL provider: the claim is about what a
 * client experiences when a load fails, which neither side can demonstrate on
 * its own.
 *
 * WHY THIS FILE ALONE RUNS WITH `--test-force-exit`: a connection Hocuspocus
 * REFUSES is never registered against a document, so `Server.destroy()` —
 * which closes documents' connections — does not reach it, and the process
 * stops draining its loop even though no active referenced handle is left.
 * That is a teardown gap in the library, reachable only from a test that
 * makes the server refuse; nothing in the service's own lifetime depends on
 * it. The flag is scoped to this file by `package.json` rather than applied
 * package-wide, so every OTHER suite keeps its leak detection — that is what
 * would catch a real `Server.destroy()` or stray-timer regression in the
 * service's own code.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import * as Y from "yjs";
import { HocuspocusProvider, HocuspocusProviderWebsocket } from "@hocuspocus/provider";
import WebSocket from "ws";
import { fragmentToMarkdown } from "@aep/collab-doc";
import { createCollabServer } from "./server.js";
import { dropRoomState, roomState } from "./rooms.js";
import type { ApplyWrite, BffClient } from "./bff.js";
import type { CollabConfig } from "./env.js";

const ROOM = "spec-acme-shop";
const PRD_PATH = "specs/requirements/prd.md";
const PRD = "# PRD\n\nA paragraph of body text.\n";
const PRD_SHA = "s1";

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.once("error", reject);
    srv.listen(0, () => {
      const addr = srv.address();
      const port = typeof addr === "object" && addr ? addr.port : 0;
      srv.close(() => resolve(port));
    });
  });
}

function config(port: number): CollabConfig {
  return {
    port,
    aepApiBase: "http://bff.test/api/v1",
    devMode: false,
    mockBff: false,
    mockBffPort: 0,
    commitDebounceMs: 60_000,
    commitMaxDebounceMs: 300_000,
  };
}

/**
 * An `aep-api` with two independently failing halves — the two windows a
 * restart opens. `oracleDown` is the likelier one (the validate call is first
 * and cheap); `readDown` is the narrower one the bug was reported from, where
 * the oracle answers and the spec bundle read does not.
 */
interface Upstream {
  oracleDown: boolean;
  readDown: boolean;
}

function flakyBff(up: Upstream, applied: ApplyWrite[]): BffClient {
  return {
    validateAccess: async () => {
      if (up.oracleDown) throw new Error("oracle unavailable (503)");
      return { name: "Jo", email: "jo@example.com", projectName: "shop" };
    },
    fetchSpecFiles: async () => {
      if (up.readDown) throw new Error("Failed to read spec files for shop (500)");
      return [{ path: PRD_PATH, content: PRD, sha: PRD_SHA }];
    },
    applyFiles: async (_t, _p, batch) => {
      applied.push(...batch.writes);
      return { commitSha: "c1", files: [] };
    },
  };
}

interface Attempt {
  provider: HocuspocusProvider;
  socket: HocuspocusProviderWebsocket;
  doc: Y.Doc;
  synced: boolean;
  refusedWith: string | null;
}

/**
 * One join attempt with a FRESH doc, resolving on whichever comes first: a
 * sync, or the server's refusal. A fresh doc per attempt is what the console
 * does when it rebuilds — the refused doc never synced, so it carries nothing.
 */
async function attemptJoin(port: number): Promise<Attempt> {
  const doc = new Y.Doc();
  const socket = new HocuspocusProviderWebsocket({
    url: `ws://127.0.0.1:${port}`,
    WebSocketPolyfill: WebSocket,
  });
  const provider = new HocuspocusProvider({
    websocketProvider: socket,
    name: ROOM,
    document: doc,
    token: "jwt",
  });
  const attempt: Attempt = { provider, socket, doc, synced: false, refusedWith: null };
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("neither synced nor refused")), 10_000);
    provider.on("synced", () => {
      clearTimeout(timer);
      attempt.synced = true;
      resolve();
    });
    provider.on("authenticationFailed", ({ reason }: { reason: string }) => {
      clearTimeout(timer);
      attempt.refusedWith = reason;
      resolve();
    });
    provider.attach();
  });
  return attempt;
}

async function leave(attempt: Attempt) {
  attempt.provider.destroy();
  attempt.socket.destroy();
  // Let the server observe the close and run its unload (and final flush).
  await new Promise((r) => setTimeout(r, 400));
}

test("a room whose spec read failed is refused, and the next attempt recovers it", async () => {
  dropRoomState(ROOM);
  const port = await freePort();
  const up: Upstream = { oracleDown: true, readDown: true };
  const applied: ApplyWrite[] = [];
  const server = createCollabServer(config(port), { bff: flakyBff(up, applied) });
  await server.listen(port);

  try {
    // aep-api is down: the oracle itself fails first, which is the likelier of
    // the two windows during a restart.
    const refusedAtAuth = await attemptJoin(port);
    assert.equal(refusedAtAuth.synced, false, "an unseedable room must not sync");
    assert.equal(
      refusedAtAuth.refusedWith,
      "upstream-unavailable",
      "an outage must not read to the client as a rejected bearer",
    );
    await leave(refusedAtAuth);

    // The narrower window the bug was reported from: the oracle answers, the
    // spec read does not. Same refusal, same tag — a room that cannot be
    // seeded is refused however the seed failed.
    up.oracleDown = false;
    const refusedAtSeed = await attemptJoin(port);
    assert.equal(refusedAtSeed.synced, false, "an unseeded room must not sync");
    assert.equal(refusedAtSeed.refusedWith, "upstream-unavailable");
    // Ask the share map, not `getXmlFragment` — that getter CREATES the key it
    // is asked about (ADR-0020), so inspecting the doc that way would plant the
    // very node this line is claiming is absent.
    assert.equal(
      refusedAtSeed.doc.share.has(PRD_PATH),
      false,
      "the refused client holds nothing to render",
    );
    await leave(refusedAtSeed);

    // Recovery: aep-api is back, and a fresh attempt seeds from git.
    up.readDown = false;
    const recovered = await attemptJoin(port);
    assert.equal(recovered.synced, true, "the room must open once the read works");
    assert.equal(
      fragmentToMarkdown(recovered.doc.getXmlFragment(PRD_PATH)).trim(),
      PRD.trim(),
      "the recovered room holds the committed document",
    );

    // The wedge itself: a baseline that was never populated writes with
    // baseSha "" — "must not exist" — and 409s against the real file forever.
    recovered.doc.getXmlFragment(PRD_PATH).insert(0, [new Y.XmlText("edited ")]);
    await leave(recovered);
    const write = applied.find((w) => w.path === PRD_PATH);
    assert.ok(write, "the recovered room must be able to commit");
    assert.equal(
      write.baseSha,
      PRD_SHA,
      "a recovered room commits against the sha it was seeded from",
    );
  } finally {
    await server.destroy();
  }
});

test("refusing a room leaves no stale baseline behind", async () => {
  // Hocuspocus registers a document only AFTER its load resolves, so the unload
  // path — and the afterUnloadDocument hook that normally drops this — never
  // runs for a load that threw. A baseline left here would hand the NEXT load
  // entries it did not seed, and the flush would commit against those shas.
  dropRoomState(ROOM);
  const port = await freePort();
  const server = createCollabServer(config(port), {
    bff: flakyBff({ oracleDown: false, readDown: true }, []),
  });
  await server.listen(port);
  try {
    const refused = await attemptJoin(port);
    assert.equal(refused.synced, false);
    await leave(refused);
    assert.equal(
      roomState(ROOM)?.baseline.size ?? 0,
      0,
      "a refused room kept a baseline the next load would commit against",
    );
  } finally {
    await server.destroy();
  }
});
