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
import { BffAccessDeniedError, BffReadError } from "./bff.js";
import {
  buildAuthenticateHook,
  buildLoadDocumentHook,
  buildStatelessHook,
  createCollabServer,
  requestFreshToken,
  UPSTREAM_UNAVAILABLE,
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

// A 403 is a verdict about this bearer, and the console is right to stop
// retrying it. A 503 is not (#586): `aep-api` restarts on every deploy, and
// every non-ok status used to arrive as the same BffAccessDeniedError — so a
// redeploy left the spec view offline until the page was reloaded. The reason
// is what the console reads to tell the two apart.
test("an unreachable oracle is tagged transient, not a denial", async () => {
  for (const failure of [
    new BffAccessDeniedError(503),
    new TypeError("fetch failed"),
  ]) {
    const auth = buildAuthenticateHook(prodConfig, {
      bff: fakeBff({
        validateAccess: async () => {
          throw failure;
        },
      }),
    });
    const err = await auth({ token: "t", documentName: ROOM }).then(
      () => null,
      (e: unknown) => e,
    );
    assert.equal(
      (err as { reason?: string }).reason,
      UPSTREAM_UNAVAILABLE,
      `${String(failure)} should not read as a verdict`,
    );
  }
});

// The retryable 4xx are the same outage wearing a different status. A BFF
// shedding load answers 429, and latching the console offline on that would
// reintroduce #586 through the one door the 5xx test does not cover.
test("a rate-limited or timing-out oracle is transient, not a verdict", async () => {
  for (const status of [408, 425, 429]) {
    const auth = buildAuthenticateHook(prodConfig, {
      bff: fakeBff({
        validateAccess: async () => {
          throw new BffAccessDeniedError(status);
        },
      }),
    });
    const err = await auth({ token: "t", documentName: ROOM }).then(
      () => null,
      (e: unknown) => e,
    );
    assert.equal(
      (err as { reason?: string }).reason,
      UPSTREAM_UNAVAILABLE,
      `${status} should keep the client retrying`,
    );
  }
});

// The seed read needs the same split the oracle got. A 404 for a project whose
// repo row is missing is permanent: tagging it transient would have every open
// tab reconnect forever against a room that can never be seeded.
test("a permanently-unreadable spec is a verdict, a failing one is not", async () => {
  for (const [failure, expected] of [
    [new BffReadError("shop", 404), undefined],
    [new BffReadError("shop", 502), UPSTREAM_UNAVAILABLE],
    [new BffReadError("shop", 429), UPSTREAM_UNAVAILABLE],
    [new TypeError("fetch failed"), UPSTREAM_UNAVAILABLE],
  ] as const) {
    const load = buildLoadDocumentHook(prodConfig, {
      bff: fakeBff({
        fetchSpecFiles: async () => {
          throw failure;
        },
      }),
    });
    ensureRoomState(ROOM, "shop");
    const err = await load({
      document: new Y.Doc() as Document,
      documentName: ROOM,
      context: {
        user: { name: "Jo", email: "j", kind: "user" },
        token: "t",
        projectName: "shop",
      },
    }).then(
      () => null,
      (e: unknown) => e,
    );
    assert.equal(
      (err as { reason?: string }).reason,
      expected,
      `${String(failure)} was classified wrongly`,
    );
  }
});

test("a denial carries no transient reason", async () => {
  const auth = buildAuthenticateHook(prodConfig, {
    bff: fakeBff({
      validateAccess: async () => {
        throw new BffAccessDeniedError(403);
      },
    }),
  });
  const err = await auth({ token: "t", documentName: ROOM }).then(
    () => null,
    (e: unknown) => e,
  );
  assert.equal((err as { reason?: string }).reason, undefined);
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

// A ROOM EXISTS ONLY IF IT WAS SEEDED (#586). This used to assert the
// opposite — that a failed read still opened a live room, on the reasoning that
// an unseeded doc beat a dead connection. It did not: the baseline stayed
// empty, so every write preconditioned on baseSha "" ("must not exist") and
// 409ed against the real file for as long as the room lived, while the console
// painted the empty room over a document that exists in git.
test("a failed files fetch rejects the load rather than opening an empty room", async () => {
  const load = buildLoadDocumentHook(prodConfig, {
    bff: fakeBff({
      fetchSpecFiles: async () => {
        throw new Error("files fetch exploded (500)");
      },
    }),
  });
  // Stand in for a second tab that authenticated into this room while this
  // load was in flight: its token and its stale baseline entry are both
  // already on the shared state.
  const state = ensureRoomState(ROOM, "shop");
  state.lastToken = "held-by-another-tab";
  state.baseline.set("specs/requirements/prd.md", { content: "stale", sha: "s0" });
  const doc = new Y.Doc() as Document;
  await assert.rejects(
    load({
      document: doc,
      documentName: ROOM,
      context: {
        user: { name: "Jo", email: "j", kind: "user" },
        token: "t",
        projectName: "shop",
      },
    }),
    /files fetch exploded \(500\)/,
  );
  // The BASELINE is what must not survive: a seed that threw partway leaves
  // entries the next successful seed would not overwrite, and a stale baseline
  // is what makes a flush commit against the wrong sha.
  assert.equal(
    roomState(ROOM)!.baseline.size,
    0,
    "a failed seed left a baseline behind for the next load to commit against",
  );
  // The state itself belongs to every connection that authenticated into the
  // room, not to this load. Evicting it would take another tab's token with
  // it, and a room with no token skips its flush entirely — that tab's edits
  // would be dropped in silence, which is worse than the wedge being fixed.
  assert.equal(
    roomState(ROOM)!.lastToken,
    "held-by-another-tab",
    "a failed load evicted state another connection owns",
  );
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

// The same wedge through a different door: a room with no project can never
// commit either, because the committer needs the project the oracle failed to
// resolve. It opened empty as a pre-phase-2 BFF allowance; nothing in the field
// is that old, and one branch left returning an unseeded room would keep the
// class alive while the fix claims it closed.
test("no project from the oracle rejects the load too", async () => {
  const load = buildLoadDocumentHook(prodConfig, { bff: fakeBff() });
  ensureRoomState(ROOM, "shop");
  const doc = new Y.Doc() as Document;
  await assert.rejects(
    load({
      document: doc,
      documentName: ROOM,
      context: { user: { name: "Jo", email: "j", kind: "user" }, token: "t", projectName: null },
    }),
    /missing bff\/token\/project/,
  );
});

// ...and it is a VERDICT, not an outage. A room the oracle resolved without a
// project will keep resolving without one, so tagging it transient would have
// every open tab reconnect every 30s for the life of the page against a room
// that can never open.
test("a room with no project is refused permanently, not tagged transient", async () => {
  const load = buildLoadDocumentHook(prodConfig, { bff: fakeBff() });
  ensureRoomState(ROOM, "shop");
  const doc = new Y.Doc() as Document;
  const err = await load({
    document: doc,
    documentName: ROOM,
    context: { user: { name: "Jo", email: "j", kind: "user" }, token: "t", projectName: null },
  }).then(
    () => null,
    (e: unknown) => e,
  );
  assert.equal(
    (err as { reason?: string }).reason,
    undefined,
    "a permanent condition must not invite an endless retry",
  );
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
