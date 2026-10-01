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
 * Seeding against a client that already holds the room's content — the case
 * that silently doubled every markdown spec file in git, once per session.
 *
 * A browser keeps ONE Y.Doc for as long as the spec view is mounted, while the
 * server (unloadImmediately) discards its document the moment the last peer
 * leaves. So a reconnect after any blip — a redeploy, a laptop sleep, a dropped
 * socket — puts a client holding a full copy of the room in front of a server
 * that believes the room is new and seeds it from git. Yjs merges; it does not
 * deduplicate, because two independent insertions of identical text are two
 * distinct sets of items. The room ends up holding the document twice, the
 * committer flushes exactly what it sees, and git gets a file twice as long.
 * Repeat per session: 374 → 750 → 1502 → 3006 → … lines.
 *
 * These run the REAL server against a REAL provider, because the bug lives in
 * the interaction between them, not in either alone.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import net from "node:net";
import * as Y from "yjs";
import { HocuspocusProvider, HocuspocusProviderWebsocket } from "@hocuspocus/provider";
import WebSocket from "ws";
import { fragmentToMarkdown } from "@aep/collab-doc";
import { createCollabServer } from "./server.js";
import { dropRoomState } from "./rooms.js";
import type { BffClient } from "./bff.js";
import type { CollabConfig } from "./env.js";

const ROOM = "spec-acme-shop";
const PRD_PATH = "specs/requirements/prd.md";
const PRD = "# PRD\n\nA paragraph of body text.\n\n## Section\n\nMore text.\n";

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
    // Long enough that no debounced store fires mid-test; the assertions read
    // the doc directly rather than waiting on a commit.
    commitDebounceMs: 60_000,
    commitMaxDebounceMs: 300_000,
  };
}

/** A BFF whose spec bundle is one markdown file, and which records applies. */
function fakeBff(applied: { paths: string[] }[]): BffClient {
  return {
    validateAccess: async () => ({
      name: "Jo",
      email: "jo@example.com",
      projectName: "shop",
    }),
    fetchSpecFiles: async () => [{ path: PRD_PATH, content: PRD, sha: "s1" }],
    applyFiles: async (_t, _p, batch) => {
      applied.push({ paths: batch.writes.map((w) => w.path) });
      return { commitSha: "c1", files: [] };
    },
  };
}

/** Attach `doc` to the room and resolve once synced. */
async function join(port: number, doc: Y.Doc) {
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
  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error("sync timeout")), 10_000);
    provider.on("synced", () => {
      clearTimeout(timer);
      resolve();
    });
    provider.on("authenticationFailed", () => {
      clearTimeout(timer);
      reject(new Error("auth rejected"));
    });
    provider.attach();
  });
  return {
    leave: async () => {
      provider.destroy();
      socket.destroy();
      // Let the server observe the close and run its unload path.
      await new Promise((r) => setTimeout(r, 250));
    },
  };
}

/** How many times the seeded document appears in the room's markdown. */
function copies(doc: Y.Doc): number {
  const md = fragmentToMarkdown(doc.getXmlFragment(PRD_PATH));
  return md.split("# PRD").length - 1;
}

test("a client that rejoins with its existing doc does not get a second copy seeded in", async () => {
  dropRoomState(ROOM);
  const port = await freePort();
  const applied: { paths: string[] }[] = [];
  const server = createCollabServer(config(port), { bff: fakeBff(applied) });
  await server.listen(port);

  // The browser's doc: created once when the spec view mounts, and kept for as
  // long as it stays mounted — across every reconnect underneath it.
  const browserDoc = new Y.Doc();
  try {
    const first = await join(port, browserDoc);
    assert.equal(copies(browserDoc), 1, "first join seeds the room once");
    // The socket drops (redeploy / sleep / blip). The server unloads the room;
    // the browser keeps its doc, exactly as useCollabSpec does.
    await first.leave();

    const second = await join(port, browserDoc);
    assert.equal(
      copies(browserDoc),
      1,
      "rejoining must not append a second copy of the document",
    );
    await second.leave();
  } finally {
    await server.destroy();
  }
});

test("repeated rejoins do not compound — the production failure was exponential", async () => {
  // The observed timeline was 374 → 750 → 1502 → 3006 → 6014 → 12030 → 24062 →
  // 48126 lines: each session doubled what the last one wrote, so a single
  // surviving rejoin path is not a small leak, it is a doubling per session.
  // Four rounds would have been 16 copies.
  dropRoomState(ROOM);
  const port = await freePort();
  const server = createCollabServer(config(port), { bff: fakeBff([]) });
  await server.listen(port);

  const browserDoc = new Y.Doc();
  try {
    for (let round = 1; round <= 4; round++) {
      const session = await join(port, browserDoc);
      assert.equal(copies(browserDoc), 1, `still one copy after rejoin ${round}`);
      await session.leave();
    }
  } finally {
    await server.destroy();
  }
});

test("a client's own edits survive a rejoin", async () => {
  // The dedupe must key on the SEED's identity, not on "drop anything that
  // looks like a duplicate" — a client reconnecting with unsynced work has to
  // keep it, or the fix would trade doubling for data loss.
  dropRoomState(ROOM);
  const port = await freePort();
  const server = createCollabServer(config(port), { bff: fakeBff([]) });
  await server.listen(port);

  const browserDoc = new Y.Doc();
  try {
    const first = await join(port, browserDoc);
    const fragment = browserDoc.getXmlFragment(PRD_PATH);
    const para = new Y.XmlElement("paragraph");
    fragment.push([para]);
    para.push([new Y.XmlText("a sentence typed while connected")]);
    await first.leave();

    const second = await join(port, browserDoc);
    const md = fragmentToMarkdown(browserDoc.getXmlFragment(PRD_PATH));
    assert.match(md, /a sentence typed while connected/, "the edit must not be dropped");
    assert.equal(copies(browserDoc), 1, "and the document must still appear once");
    await second.leave();
  } finally {
    await server.destroy();
  }
});

test("a genuinely new client still gets the room seeded from git", async () => {
  // The guard above must not be so eager that it stops seeding altogether: a
  // first joiner with an empty doc has to receive the committed content.
  dropRoomState(ROOM);
  const port = await freePort();
  const server = createCollabServer(config(port), { bff: fakeBff([]) });
  await server.listen(port);

  const doc = new Y.Doc();
  try {
    const session = await join(port, doc);
    assert.equal(copies(doc), 1);
    assert.match(fragmentToMarkdown(doc.getXmlFragment(PRD_PATH)), /A paragraph of body text/);
    await session.leave();
  } finally {
    await server.destroy();
  }
});

test("a second, empty client joining a live room sees the room's content once", async () => {
  dropRoomState(ROOM);
  const port = await freePort();
  const server = createCollabServer(config(port), { bff: fakeBff([]) });
  await server.listen(port);

  const first = new Y.Doc();
  const second = new Y.Doc();
  try {
    const a = await join(port, first);
    const b = await join(port, second); // joins while A is still connected
    assert.equal(copies(first), 1, "the established peer keeps one copy");
    assert.equal(copies(second), 1, "the joining peer receives one copy");
    await b.leave();
    await a.leave();
  } finally {
    await server.destroy();
  }
});
