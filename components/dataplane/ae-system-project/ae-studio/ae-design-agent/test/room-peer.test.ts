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

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { Server } from "@hocuspocus/server";
import type { Document } from "@hocuspocus/server";
import { HocuspocusProvider, HocuspocusProviderWebsocket } from "@hocuspocus/provider";
import WebSocket from "ws";
import * as Y from "yjs";
import { readDocFile, setDocFile } from "@aep/collab-doc";
import { joinRoom, type RoomPeer } from "../src/collab/room-peer.js";
import { DocFileBundle } from "../src/collab/doc-bundle.js";

// A real Hocuspocus server (no auth hooks — auth is the collab service's
// oracle, not this client's concern) with a seeded room, so the peer path is
// exercised over an actual websocket: join, sync, read, mirror ops, leave.

let server: Server;
let url: string;
const serverDocs = new Map<string, Document>();

// Hocuspocus treats port 0 as unset (defaults to 80); pick a random high port.
const randomPort = (): number => 20000 + Math.floor(Math.random() * 20000);
const PORT = randomPort();

async function until(cond: () => boolean, what: string, ms = 10_000): Promise<void> {
  const deadline = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > deadline) assert.fail(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 20));
  }
}

before(async () => {
  server = new Server({
    onLoadDocument: ({ document, documentName }) => {
      serverDocs.set(documentName, document);
      setDocFile(document, "requirements/prd.md", "# PRD\n\nSeeded body.");
      setDocFile(document, "design/arch.excalidraw", '{"v":1}');
      return Promise.resolve(document);
    },
  });
  await server.listen(PORT);
  url = `ws://127.0.0.1:${PORT}`;
});

after(async () => {
  await server.destroy();
});

test("joins, snapshots the doc, mirrors bundle ops live, leaves", async () => {
  const peer: RoomPeer = await joinRoom({
    url,
    roomId: "spec-acme-shop",
    token: async () => "any",
  });
  try {
    const files = peer.files();
    assert.match(files["requirements/prd.md"] ?? "", /# PRD/);
    assert.equal(files["design/arch.excalidraw"], '{"v":1}');

    const bundle = new DocFileBundle(peer, files);

    // edit an md file → fragment reparse lands on the server doc
    const edit = bundle.editFile("requirements/prd.md", "Seeded body.", "Agent-edited body.");
    assert.equal(edit.ok, true);

    // add a new text file → files-map entry lands on the server doc
    const add = bundle.addFile("validation/plan.txt", "check things\n");
    assert.equal(add.ok, true);

    // let updates flush over the socket
    await new Promise((r) => setTimeout(r, 300));

    const serverDoc = serverDocs.get("spec-acme-shop");
    assert.ok(serverDoc, "server holds the room doc");
    assert.match(
      readDocFile(serverDoc, "requirements/prd.md") ?? "",
      /Agent-edited body\./,
    );
    assert.equal(readDocFile(serverDoc, "validation/plan.txt"), "check things\n");
  } finally {
    peer.leave();
  }
});

test("a failed-op does not touch the doc", async () => {
  const peer = await joinRoom({ url, roomId: "spec-acme-shop2", token: async () => "any" });
  try {
    const bundle = new DocFileBundle(peer, peer.files());
    const res = bundle.editFile("requirements/prd.md", "not-present-text", "x");
    assert.equal(res.ok, false);
    await new Promise((r) => setTimeout(r, 200));
    const serverDoc = serverDocs.get("spec-acme-shop2");
    assert.match(readDocFile(serverDoc!, "requirements/prd.md") ?? "", /Seeded body\./);
  } finally {
    peer.leave();
  }
});

test("the token is asked at every connect and the parameters ride the upgrade", async () => {
  const port = randomPort();
  const seen: Array<{ token: string; room: string; credit: string | null }> = [];
  const srv = new Server({
    quiet: true,
    onAuthenticate: ({ token, documentName, requestParameters }) => {
      seen.push({ token, room: documentName, credit: requestParameters.get("credit") });
      return Promise.resolve();
    },
  });
  await srv.listen(port);
  let asked = 0;
  const credit = JSON.stringify({ name: "Ann", email: "ann@x" });
  const peer = await joinRoom({
    url: `ws://127.0.0.1:${port}`,
    roomId: "spec-acme-greeter",
    token: async () => `token-${++asked}`,
    parameters: { credit },
  });
  try {
    assert.deepEqual(seen, [{ token: "token-1", room: "spec-acme-greeter", credit }]);
  } finally {
    peer.leave();
    await srv.destroy();
  }
});

test("a dropped Room is rejoined with a fresh doc: a new token, no doubled seed, the agent's writes land again (C13)", async () => {
  const port = randomPort();
  const tokens: string[] = [];
  let serverDoc: Document | undefined;
  let loads = 0;
  // Each start of the collab server seeds its doc anew (new Yjs items), as
  // ae-collab does from git after a restart.
  const start = async (): Promise<Server> => {
    const srv = new Server({
      quiet: true,
      onAuthenticate: ({ token }) => {
        tokens.push(token);
        return Promise.resolve();
      },
      onLoadDocument: ({ document }) => {
        loads++;
        serverDoc = document;
        setDocFile(document, "specs/requirements/prd.md", "# PRD\n\nSeeded body.");
        return Promise.resolve(document);
      },
    });
    await srv.listen(port);
    return srv;
  };
  let srv = await start();
  let n = 0;
  const peer = await joinRoom({ url: `ws://127.0.0.1:${port}`, roomId: "spec-acme-rejoin", token: async () => `t${++n}` });
  try {
    peer.set("specs/design/notes.txt", "agent wrote this\n", false);
    await until(() => serverDoc !== undefined && readDocFile(serverDoc, "specs/design/notes.txt") !== undefined, "the first write");

    // The collab server restarts: its doc is gone, the next load re-seeds.
    await srv.destroy();
    serverDoc = undefined;
    srv = await start();
    await until(() => serverDoc !== undefined && readDocFile(serverDoc, "specs/design/notes.txt") === "agent wrote this\n", "the write re-applied after the rejoin");

    assert.equal(loads, 2, "the doc was seeded twice: a kept client doc would merge the first seed in");
    assert.equal(readDocFile(serverDoc!, "specs/requirements/prd.md"), "# PRD\n\nSeeded body.", "the seed is not doubled");
    assert.deepEqual(tokens, ["t1", "t2"], "the rejoin asked for a new token");
    assert.equal(peer.files()["specs/requirements/prd.md"], "# PRD\n\nSeeded body.");

    // Writes after the rejoin go to the new connection.
    peer.set("specs/design/more.txt", "later\n", false);
    await until(() => readDocFile(serverDoc!, "specs/design/more.txt") === "later\n", "a write after the rejoin");
  } finally {
    peer.leave();
    await srv.destroy();
  }
});

test("leave clears the agent's presence for the other peers at once (C21)", async () => {
  const peer = await joinRoom({ url, roomId: "spec-acme-presence", token: async () => "any" });
  const socket = new HocuspocusProviderWebsocket({ url, WebSocketPolyfill: WebSocket });
  const observer = new HocuspocusProvider({ websocketProvider: socket, name: "spec-acme-presence", document: new Y.Doc(), token: "any" });
  observer.attach();
  const agents = (): number =>
    [...(observer.awareness?.getStates().values() ?? [])].filter((s) => (s.user as { kind?: string } | undefined)?.kind === "agent").length;
  try {
    await until(() => agents() === 1, "the agent's presence");
    peer.leave();
    // Well inside the 30 s awareness timeout: the removal is sent on leave.
    await until(() => agents() === 0, "the agent's presence cleared", 2_000);
  } finally {
    peer.leave();
    observer.destroy();
    socket.destroy();
  }
});
