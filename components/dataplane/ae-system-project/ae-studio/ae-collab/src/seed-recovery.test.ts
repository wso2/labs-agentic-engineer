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
 * git, after an `aep-api` restart while the collab server stayed up. The seed
 * threw, the failure was swallowed, and the room opened empty — permanently,
 * because a room stays loaded while any client is connected. The console could
 * not tell that room from an empty project, so it suppressed the committed-git
 * fallback; an agent turn joined it, synced perfectly, and was told the project
 * had no files; and every flush 409ed, because a baseline that was never
 * populated writes with `baseSha: ""`, which the Files socket reads as "must
 * not exist".
 *
 * These run the REAL pod Room against a REAL provider and the fake Files
 * socket: the claim is about what a client experiences when a load fails,
 * which neither side can demonstrate on its own. The IdP is stood in by a
 * verifier that admits one user of the pod's org.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import * as Y from "yjs";
import { HocuspocusProvider, HocuspocusProviderWebsocket } from "@hocuspocus/provider";
import WebSocket from "ws";
import { fragmentToMarkdown } from "@aep/collab-doc";
import { startFakeFilesSocket, type FakeFilesSocket } from "./fake-files-socket.js";
import type { Verify } from "./pod/auth.js";
import type { PodConfig } from "./pod/config.js";
import type { PodListeners } from "./pod/listeners.js";
import { startPod } from "./pod/start.js";
import { dropRoomState, roomState } from "./rooms.js";

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
const PRD = "# PRD\n\nA paragraph of body text.\n";

/** Admits one user of the pod's org: the token check is not what these tests are about. */
const verify: Verify = () =>
  Promise.resolve({
    kind: "user",
    claims: { sub: "u-jo", ouId: "ou-acme", ouHandle: "acme", name: "Jo", email: "jo@example.com", exp: Math.floor(Date.now() / 1000) + 3600 },
  });

async function startRoom(files: FakeFilesSocket): Promise<PodListeners> {
  const cfg: PodConfig = {
    orgId: "ou-acme",
    orgHandle: "acme",
    issuer: "http://thunder.test",
    jwksUrl: "http://thunder.test/oauth2/jwks",
    userAudiences: ["aep-console-client"],
    agentClientId: "ae-studio-acme",
    allowedOrigins: [CONSOLE_ORIGIN],
    filesSocket: files.path,
    listenPort: 0,
    healthPort: 0,
    localPort: 0,
  };
  return startPod(cfg, { verify, log: () => {} });
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
async function attemptJoin(pod: PodListeners): Promise<Attempt> {
  const doc = new Y.Doc();
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
  const files = await startFakeFilesSocket({ files: { [PRD_PATH]: PRD } });
  const pod = await startRoom(files);

  try {
    // aep-api is down: the project lookup fails first, which is the likelier
    // of the two windows during a restart.
    files.failNext(503, "aep_api_unavailable", "lookup");
    const refusedAtAuth = await attemptJoin(pod);
    assert.equal(refusedAtAuth.synced, false, "an unseedable room must not sync");
    assert.equal(
      refusedAtAuth.refusedWith,
      "upstream-unavailable",
      "an outage must not read to the client as a rejected bearer",
    );
    await leave(refusedAtAuth);

    // The narrower window the bug was reported from: the lookup answers, the
    // spec read does not. Same refusal, same tag — a room that cannot be
    // seeded is refused however the seed failed.
    files.failNext(503, "aep_api_unavailable", "bundle");
    const refusedAtSeed = await attemptJoin(pod);
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

    // Recovery: the socket answers again, and a fresh attempt seeds from git.
    const recovered = await attemptJoin(pod);
    assert.equal(recovered.synced, true, "the room must open once the read works");
    assert.equal(
      fragmentToMarkdown(recovered.doc.getXmlFragment(PRD_PATH)).trim(),
      PRD.trim(),
      "the recovered room holds the committed document",
    );

    // The wedge itself: a baseline that was never populated writes with
    // baseSha "" — "must not exist" — and 409s against the real file forever.
    const para = new Y.XmlElement("paragraph");
    recovered.doc.getXmlFragment(PRD_PATH).push([para]);
    para.push([new Y.XmlText("edited")]);
    await leave(recovered);
    assert.equal(files.commits().length, 1, "the recovered room must be able to commit");
    assert.match(files.file(PRD_PATH) ?? "", /edited/);
    assert.equal(
      files.requests.filter((r) => r.method === "POST").length,
      1,
      "a recovered room commits against the sha it was seeded from: no conflict, no retry",
    );
  } finally {
    await pod.close();
    await files.close();
    dropRoomState(ROOM);
  }
});

test("refusing a room leaves no stale baseline behind", async () => {
  // Hocuspocus registers a document only AFTER its load resolves, so the unload
  // path — and the afterUnloadDocument hook that normally drops this — never
  // runs for a load that threw. A baseline left here would hand the NEXT load
  // entries it did not seed, and the flush would commit against those shas.
  dropRoomState(ROOM);
  const files = await startFakeFilesSocket({ files: { [PRD_PATH]: PRD } });
  const pod = await startRoom(files);
  try {
    files.failNext(503, "aep_api_unavailable", "bundle");
    const refused = await attemptJoin(pod);
    assert.equal(refused.synced, false);
    await leave(refused);
    assert.equal(
      roomState(ROOM)?.baseline.size ?? 0,
      0,
      "a refused room kept a baseline the next load would commit against",
    );
  } finally {
    await pod.close();
    await files.close();
    dropRoomState(ROOM);
  }
});
