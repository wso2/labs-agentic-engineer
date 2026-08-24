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

import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:net";
import * as Y from "yjs";
import type { CollabConfig } from "./env.js";
import type { BffClient } from "./bff.js";
import { BffAccessDeniedError } from "./bff.js";
import {
  buildAuthenticateHook,
  buildLoadDocumentHook,
  buildStatelessHook,
  createCollabServer,
  requestFreshToken,
  type CollabContext,
} from "./server.js";
import { filesMap } from "./seed.js";
import { devSeedFiles } from "./fixtures.js";
import type { Document } from "@hocuspocus/server";
import {
  dropRoomState,
  ensureRoomState,
  roomState,
} from "./rooms.js";

/** Ephemeral port — Hocuspocus `listen(0)` is a no-op (falsy → default 80). */
async function freePort(): Promise<number> {
  return await new Promise((resolve, reject) => {
    const s = createServer();
    s.listen(0, "127.0.0.1", () => {
      const addr = s.address();
      if (!addr || typeof addr === "string") {
        s.close();
        reject(new Error("expected TCP address"));
        return;
      }
      const { port } = addr;
      s.close((err) => (err ? reject(err) : resolve(port)));
    });
    s.on("error", reject);
  });
}

const ROOM = "spec-acme-shop";

const prodConfig: CollabConfig = {
  port: 0,
  aepApiBase: "http://bff.test/api/v1",
  devMode: false,
  mockBff: false,
  mockBffPort: 0,
  commitDebounceMs: 1000,
  commitMaxDebounceMs: 5000,
};
const devConfig: CollabConfig = {
  port: 0,
  aepApiBase: null,
  devMode: true,
  mockBff: false,
  mockBffPort: 0,
  commitDebounceMs: 1000,
  commitMaxDebounceMs: 5000,
};

function fakeBff(overrides: Partial<BffClient> = {}): BffClient {
  return {
    validateAccess: async () => ({
      name: "Jo",
      email: "jo@example.com",
      projectName: "shop",
    }),
    fetchSpecFiles: async () => [
      { path: "requirements/prd.md", content: "# P\n", sha: "s1" },
    ],
    applyFiles: async () => ({ commitSha: "c", files: [] }),
    ...overrides,
  };
}

beforeEach(() => dropRoomState(ROOM));


test("auth rejects unknown rooms before hitting the oracle", async () => {
  let oracleCalled = false;
  const auth = buildAuthenticateHook(prodConfig, {
    bff: fakeBff({
      validateAccess: async () => {
        oracleCalled = true;
        return { name: "x", email: "x", projectName: "x" };
      },
    }),
  });
  await assert.rejects(
    auth({ token: "t", documentName: "not-a-spec-room" }),
    /unknown room/,
  );
  assert.equal(oracleCalled, false);
});

test("auth passes token + room to the oracle; identity and project come back", async () => {
  const seen: string[] = [];
  const auth = buildAuthenticateHook(prodConfig, {
    bff: fakeBff({
      validateAccess: async (token, room) => {
        seen.push(token, room);
        return { name: "Jo", email: "jo@example.com", projectName: "shop" };
      },
    }),
  });
  const ctx = await auth({ token: "jwt-abc", documentName: "spec-acme-shop" });
  assert.deepEqual(seen, ["jwt-abc", "spec-acme-shop"]);
  assert.equal(ctx.user.name, "Jo");
  assert.equal(ctx.user.kind, "user");
  assert.equal(ctx.token, "jwt-abc");
  assert.equal(ctx.projectName, "shop");
});

test("auth propagates oracle denial", async () => {
  const auth = buildAuthenticateHook(prodConfig, {
    bff: fakeBff({
      validateAccess: async () => {
        throw new BffAccessDeniedError(403);
      },
    }),
  });
  await assert.rejects(
    auth({ token: "t", documentName: "spec-acme-shop" }),
    BffAccessDeniedError,
  );
});

test("auth rejects a missing token outside dev mode", async () => {
  const auth = buildAuthenticateHook(prodConfig, { bff: fakeBff() });
  await assert.rejects(
    auth({ token: "", documentName: "spec-acme-shop" }),
    /missing token/,
  );
});

test("dev mode skips the oracle entirely", async () => {
  const auth = buildAuthenticateHook(devConfig, { bff: null });
  const ctx = await auth({ token: "", documentName: "spec-acme-shop" });
  assert.equal(ctx.user.kind, "dev");
});

test("dev mode still rejects unknown rooms", async () => {
  const auth = buildAuthenticateHook(devConfig, { bff: null });
  await assert.rejects(
    auth({ token: "", documentName: "lobby" }),
    /unknown room/,
  );
});

test("load seeds from fixtures in dev mode (md files become fragments)", async () => {
  const load = buildLoadDocumentHook(devConfig, { bff: null });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: { user: { name: "d", email: "d", kind: "dev" }, token: null, projectName: null },
  });
  for (const file of devSeedFiles) {
    if (file.path.endsWith(".md")) {
      assert.ok(
        doc.getXmlFragment(file.path).length > 0,
        `fragment for ${file.path}`,
      );
    } else {
      assert.ok(filesMap(doc).has(file.path), `map entry for ${file.path}`);
    }
  }
});

test("load seeds from the BFF's Files API with the joiner's token", async () => {
  const calls: string[] = [];
  const load = buildLoadDocumentHook(prodConfig, {
    bff: fakeBff({
      fetchSpecFiles: async (token, project) => {
        calls.push(token, project);
        return [{ path: "requirements/prd.md", content: "hi", sha: "s2" }];
      },
    }),
  });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: {
      user: { name: "Jo", email: "j", kind: "user" },
      token: "jwt-abc",
      projectName: "shop",
    },
  });
  assert.deepEqual(calls, ["jwt-abc", "shop"]);
  assert.match(doc.getXmlFragment("requirements/prd.md").toString(), /hi/);
});

// Reference documents (specs/requirements/references/, #383/#384) are user
// uploads, not collaboratively-edited spec — and a binary one seeded into a
// text room gets flushed back as text, destroying the committed file (a real
// PDF in git became its own base64). They never enter the room.
test("load never seeds reference documents into the room", async () => {
  const load = buildLoadDocumentHook(prodConfig, {
    bff: fakeBff({
      fetchSpecFiles: async () => [
        { path: "specs/requirements/prd.md", content: "# PRD\n", sha: "s1" },
        { path: "specs/requirements/references/rfp.pdf", content: "JVBERi0xLjQK", sha: "s2" },
        { path: "specs/requirements/references/notes.md", content: "# Notes\n", sha: "s3" },
      ],
    }),
  });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: {
      user: { name: "Jo", email: "j", kind: "user" },
      token: "jwt-abc",
      projectName: "shop",
    },
  });
  assert.match(doc.getXmlFragment("specs/requirements/prd.md").toString(), /PRD/);
  // Neither reference file may exist in the doc — not the binary one, and not
  // the markdown one either: the whole folder is out of collab's scope. Each
  // check has to match where the seed would have PUT that kind, or it proves
  // nothing: a non-markdown file becomes a Y.Text inside the files map, never a
  // top-level share key, so `share.has(pdf)` is false whether or not the seed
  // ran. And `share.has` for the markdown is asked BEFORE any getXmlFragment
  // call on that path, because that call would itself create the entry.
  assert.ok(
    !filesMap(doc).has("specs/requirements/references/rfp.pdf"),
    "pdf seeded into the room",
  );
  assert.ok(
    !doc.share.has("specs/requirements/references/notes.md"),
    "reference markdown seeded into the room",
  );
  assert.equal(
    doc.getXmlFragment("specs/requirements/references/notes.md").length,
    0,
    "reference markdown seeded into the room",
  );
});

test("load opens an unseeded doc when the files fetch fails (room must survive)", async () => {
  const load = buildLoadDocumentHook(prodConfig, {
    bff: fakeBff({
      fetchSpecFiles: async () => {
        throw new Error("files fetch exploded (404)");
      },
    }),
  });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: {
      user: { name: "Jo", email: "j", kind: "user" },
      token: "t",
      projectName: "shop",
    },
  });
  assert.equal(filesMap(doc).size, 0);
});

test("GET /healthz returns 200 ok", async () => {
  const port = await freePort();
  const server = createCollabServer({ ...devConfig, port }, { bff: null });
  await server.listen(port);
  const res = await fetch(`http://127.0.0.1:${port}/healthz`);
  assert.equal(res.status, 200);
  assert.equal(await res.text(), "ok");
  await server.destroy();
});

test("load opens an empty doc when the oracle gave no project (pre-phase-2 BFF)", async () => {
  const load = buildLoadDocumentHook(prodConfig, { bff: fakeBff() });
  const doc = new Y.Doc() as Document;
  await load({
    document: doc,
    documentName: "spec-acme-shop",
    context: { user: { name: "Jo", email: "j", kind: "user" }, token: "t", projectName: null },
  });
  assert.equal(filesMap(doc).size, 0);
});

test("stateless token updates connection context and lastToken", async () => {
  ensureRoomState(ROOM, "shop").lastToken = "old";
  const ctx: CollabContext = {
    user: { name: "Jo", email: "j", kind: "user" },
    token: "old",
    projectName: "shop",
  };
  const logs: string[] = [];
  const hook = buildStatelessHook(prodConfig, {
    bff: fakeBff(),
    log: (m) => logs.push(m),
  });
  await hook({
    connection: {
      context: ctx,
      sendStateless: () => {},
    } as never,
    documentName: ROOM,
    document: new Y.Doc() as Document,
    payload: JSON.stringify({ type: "token", value: "new-jwt" }),
  });
  assert.equal(ctx.token, "new-jwt");
  assert.equal(roomState(ROOM)!.lastToken, "new-jwt");
  assert.match(logs.join("\n"), /token refreshed for spec-acme-shop/);
});

test("requestFreshToken resolves when a matching token reply arrives", async () => {
  const sent: string[] = [];
  const fakeConn = {
    sendStateless: (payload: string) => sent.push(payload),
  };
  const doc = {
    getConnections: () => [fakeConn],
    broadcastStateless: () => {},
  };
  const pending = requestFreshToken(doc as never, {}, ROOM);
  assert.equal(sent.length, 1);
  const please = JSON.parse(sent[0]!) as { type: string; id: string };
  assert.equal(please.type, "token-please");
  assert.ok(please.id);

  const hook = buildStatelessHook(prodConfig, { bff: fakeBff() });
  ensureRoomState(ROOM, "shop");
  await hook({
    connection: {
      context: {
        user: { name: "Jo", email: "j", kind: "user" },
        token: "stale",
        projectName: "shop",
      } satisfies CollabContext,
      sendStateless: () => {},
    } as never,
    documentName: ROOM,
    document: new Y.Doc() as Document,
    payload: JSON.stringify({
      type: "token",
      value: "pulled-jwt",
      id: please.id,
    }),
  });
  assert.equal(await pending, "pulled-jwt");
});

test("requestFreshToken returns null when no connections", async () => {
  const doc = {
    getConnections: () => [],
    broadcastStateless: () => {},
  };
  assert.equal(await requestFreshToken(doc as never, {}, ROOM), null);
});
