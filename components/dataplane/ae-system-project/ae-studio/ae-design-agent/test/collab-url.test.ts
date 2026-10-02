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
 * TEMPORARY (phase 3 deletes): old agents joins the pod Room. A room-scoped
 * turn names the Room's ws URL in its `collab` block (aep-api reads it from
 * the org's AE Studio); the service joins exactly that URL and has no collab
 * server of its own to fall back to.
 */

import { test, before, after } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { tmpdir } from "node:os";
import { Server } from "@hocuspocus/server";
import { setDocFile } from "@aep/collab-doc";
import { SignJWT } from "jose";
import { createApp } from "../src/server.js";
import { listen0 } from "../src/shared/listen.js";
import { InMemoryConversationStore } from "../src/store/memory-store.js";
import { mockModel } from "../src/shared/mock-model.js";

const AUD = "agents-service";
const SECRET = "test-secret";
const ORG = "acme";
const PROJ = "shop";
const SLUG = "spec-repo";
const REF = "1".repeat(40);
const SKILLS_REF = "2".repeat(40);
const CONV = `org_${ORG}--proj_${PROJ}--general--conv1`;
const ROOM = `spec-${ORG}-${PROJ}`;

// A real Hocuspocus server standing in for the pod's ae-collab; it records
// the path and room every join asks for.
let room: Server;
let roomUrl: string;
const joins: { path: string; room: string }[] = [];
const PORT = 20000 + Math.floor(Math.random() * 20000);

before(async () => {
  room = new Server({
    onConnect: ({ request, documentName }) => {
      joins.push({ path: new URL(request.url ?? "", "http://unused").pathname, room: documentName });
      return Promise.resolve();
    },
    onLoadDocument: ({ document }) => {
      setDocFile(document, "requirements/prd.md", "# PRD\n");
      return Promise.resolve(document);
    },
  });
  await room.listen(PORT);
  roomUrl = `ws://127.0.0.1:${PORT}/v1/rooms`;
});

after(async () => {
  await room.destroy();
});

function mountRoot(): string {
  const root = mkdtempSync(join(tmpdir(), "aep-collab-url-"));
  const snap = join(root, "repos", ORG, PROJ, SLUG, "snapshots", REF, "specs/requirements/prd.md");
  mkdirSync(dirname(snap), { recursive: true });
  writeFileSync(snap, "# PRD\n");
  mkdirSync(join(root, "repos", ORG, "_skills", "org-skills", "snapshots", SKILLS_REF), { recursive: true });
  return root;
}

async function postTurn(collab: unknown): Promise<Response> {
  const root = mountRoot();
  const app = createApp({
    store: new InMemoryConversationStore(),
    buildModel: () => mockModel([{ kind: "text", text: "ok" }]),
    auth: { audience: AUD, secret: SECRET },
    workspaceMountRoot: root,
  });
  const { baseUrl, close } = await listen0(app.listen(0));
  try {
    const token = await new SignJWT({})
      .setProtectedHeader({ alg: "HS256" })
      .setAudience(AUD)
      .setIssuedAt()
      .setExpirationTime("1h")
      .sign(new TextEncoder().encode(SECRET));
    const res = await fetch(`${baseUrl}/conversations/${CONV}/turns`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        Authorization: `Bearer ${token}`,
        "X-Model-Key": "sk-ant-test-key-0000000000",
        "X-Org-Id": ORG,
      },
      body: JSON.stringify({
        turn: { kind: "chat", text: "edit the doc" },
        workspace: { conversationId: CONV, turnId: "t-1", repoSlug: SLUG, ref: REF, skillsRef: SKILLS_REF },
        collab,
      }),
    });
    // Drain the stream so the turn (and its room peer) finishes before close.
    await res.text();
    return res;
  } finally {
    await close();
    rmSync(root, { recursive: true, force: true });
  }
}

test("a collab turn joins the URL from the body", async () => {
  joins.length = 0;
  const res = await postTurn({ roomId: ROOM, token: "user-jwt", url: roomUrl });
  assert.equal(res.status, 200);
  assert.deepEqual(joins, [{ path: "/v1/rooms", room: ROOM }]);
});

test("a collab block without a url is a pre-stream 400; nothing joins", async () => {
  joins.length = 0;
  const res = await postTurn({ roomId: ROOM, token: "user-jwt" });
  assert.equal(res.status, 400);
  assert.deepEqual(joins, []);
});
