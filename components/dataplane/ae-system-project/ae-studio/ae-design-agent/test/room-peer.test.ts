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
const PORT = 20000 + Math.floor(Math.random() * 20000);

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
    token: "any",
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
  const peer = await joinRoom({ url, roomId: "spec-acme-shop2", token: "any" });
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
