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
 * These run the REAL pod Room against a REAL provider (and the fake Files
 * socket), because the bug lives in the interaction between them, not in
 * either alone.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join as joinPath } from "node:path";
import * as Y from "yjs";
import { HocuspocusProvider, HocuspocusProviderWebsocket } from "@hocuspocus/provider";
import WebSocket from "ws";
import { fragmentToMarkdown } from "@aep/collab-doc";
import { startFakeFilesSocket, type FakeFilesSocket } from "./fake-files-socket.js";
import type { Verify } from "./pod/auth.js";
import type { PodListeners } from "./pod/listeners.js";
import { startPod } from "./pod/start.js";
import { dropRoomState } from "./rooms.js";

const ROOM = "spec-acme-shop";
const CONSOLE_ORIGIN = "http://console.ae.localhost:8080";

/**
 * A browser's socket: `ws` sends no Origin unless told, and the public
 * listener refuses an upgrade without a listed one.
 */
class ConsoleWebSocket extends WebSocket {
  constructor(address: string | URL, protocols?: string | string[]) {
    super(address, protocols, { origin: CONSOLE_ORIGIN });
  }
}
const PRD_PATH = "specs/requirements/prd.md";
// Without a trailing newline: the room's markdown serializer drops one, so a
// file ending in "\n" would be rewritten by the first session's final flush,
// and the rejoins below would reseed different bytes from the ones the client
// holds (a different seed identity, which no dedupe can collapse).
const PRD = "# PRD\n\nA paragraph of body text.\n\n## Section\n\nMore text.";

/** Admits one user of the pod's org: the token check is not what these tests are about. */
const verify: Verify = () =>
  Promise.resolve({
    kind: "user",
    claims: { sub: "u-jo", ouId: "ou-acme", ouHandle: "acme", name: "Jo", email: "jo@example.com", exp: Math.floor(Date.now() / 1000) + 3600 },
  });

/** The pod Room over a Files socket whose spec bundle is one markdown file. */
async function startRoom(): Promise<{ pod: PodListeners; files: FakeFilesSocket; close(): Promise<void> }> {
  dropRoomState(ROOM);
  const files = await startFakeFilesSocket({ files: { [PRD_PATH]: PRD } });
  const pod = await startPod(
    {
      orgId: "ou-acme",
      orgHandle: "acme",
      issuer: "http://thunder.test",
      jwksUrl: "http://thunder.test/oauth2/jwks",
      userAudiences: ["aep-console-client"],
      allowedOrigins: [CONSOLE_ORIGIN],
      filesSocket: files.path,
      // Unused here (no agent joins), but bound like the pod's.
      roomSocket: joinPath(mkdtempSync(joinPath(tmpdir(), "aec-")), "room.sock"),
      listenPort: 0,
      healthPort: 0,
    },
    // The default 60 s debounce: no store fires mid-test; the assertions read the doc.
    { verify, log: () => {} },
  );
  return {
    pod,
    files,
    async close() {
      await pod.close();
      await files.close();
      dropRoomState(ROOM);
    },
  };
}

/** Attach `doc` to the room and resolve once synced. */
async function join(pod: PodListeners, doc: Y.Doc) {
  const socket = new HocuspocusProviderWebsocket({
    url: `${pod.publicUrl.replace(/^http/, "ws")}/v1/rooms`,
    WebSocketPolyfill: ConsoleWebSocket,
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
  const room = await startRoom();

  // The browser's doc: created once when the spec view mounts, and kept for as
  // long as it stays mounted — across every reconnect underneath it.
  const browserDoc = new Y.Doc();
  try {
    const first = await join(room.pod, browserDoc);
    assert.equal(copies(browserDoc), 1, "first join seeds the room once");
    // The socket drops (redeploy / sleep / blip). The server unloads the room;
    // this client keeps its doc (the console's specRoom.ts throws its doc
    // away and joins fresh; this test covers a client that keeps it).
    await first.leave();

    const second = await join(room.pod, browserDoc);
    assert.equal(
      copies(browserDoc),
      1,
      "rejoining must not append a second copy of the document",
    );
    await second.leave();
  } finally {
    await room.close();
  }
});

test("repeated rejoins do not compound — the production failure was exponential", async () => {
  // The observed timeline was 374 → 750 → 1502 → 3006 → 6014 → 12030 → 24062 →
  // 48126 lines: each session doubled what the last one wrote, so a single
  // surviving rejoin path is not a small leak, it is a doubling per session.
  // Four rounds would have been 16 copies.
  const room = await startRoom();

  const browserDoc = new Y.Doc();
  try {
    for (let round = 1; round <= 4; round++) {
      const session = await join(room.pod, browserDoc);
      assert.equal(copies(browserDoc), 1, `still one copy after rejoin ${round}`);
      await session.leave();
    }
  } finally {
    await room.close();
  }
});

test("a client's own edits survive a rejoin", async () => {
  // The dedupe must key on the SEED's identity, not on "drop anything that
  // looks like a duplicate" — a client reconnecting with unsynced work has to
  // keep it, or the fix would trade doubling for data loss. The work is typed
  // while the socket is down, so git (and the reseed) still holds the bytes
  // the client was seeded with.
  const room = await startRoom();

  const browserDoc = new Y.Doc();
  try {
    const first = await join(room.pod, browserDoc);
    await first.leave();
    const fragment = browserDoc.getXmlFragment(PRD_PATH);
    const para = new Y.XmlElement("paragraph");
    fragment.push([para]);
    para.push([new Y.XmlText("a sentence typed while disconnected")]);

    const second = await join(room.pod, browserDoc);
    const md = fragmentToMarkdown(browserDoc.getXmlFragment(PRD_PATH));
    assert.match(md, /a sentence typed while disconnected/, "the edit must not be dropped");
    assert.equal(copies(browserDoc), 1, "and the document must still appear once");
    await second.leave();
  } finally {
    await room.close();
  }
});

test("a genuinely new client still gets the room seeded from git", async () => {
  // The guard above must not be so eager that it stops seeding altogether: a
  // first joiner with an empty doc has to receive the committed content.
  const room = await startRoom();

  const doc = new Y.Doc();
  try {
    const session = await join(room.pod, doc);
    assert.equal(copies(doc), 1);
    assert.match(fragmentToMarkdown(doc.getXmlFragment(PRD_PATH)), /A paragraph of body text/);
    await session.leave();
  } finally {
    await room.close();
  }
});

test("a second, empty client joining a live room sees the room's content once", async () => {
  const room = await startRoom();

  const first = new Y.Doc();
  const second = new Y.Doc();
  try {
    const a = await join(room.pod, first);
    const b = await join(room.pod, second); // joins while A is still connected
    assert.equal(copies(first), 1, "the established peer keeps one copy");
    assert.equal(copies(second), 1, "the joining peer receives one copy");
    await b.leave();
    await a.leave();
  } finally {
    await room.close();
  }
});

// Pre-existing and not fixed here: the seed identity is a hash of the bytes in
// git, so once a session's edit is committed the reseed carries new bytes, a
// new identity, and nothing collapses it with the copy the client kept.
test.todo(
  "client keeps its Y.Doc, its edit is committed, room unloads, client reconnects → document doubles",
);
