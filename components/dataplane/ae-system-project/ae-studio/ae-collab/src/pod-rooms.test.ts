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
 * The Room in the pod (07 §11), end to end over real sockets: a real
 * HocuspocusProvider against both listeners, a loopback JWKS standing in for
 * the Platform IdP, and the fake Files socket. Token deadlines run on a test
 * clock, so expiry is stepped, never slept.
 *
 * Review Focus 3 (token expiry) and 4 (listener confusion) are pinned here.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, request, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { generateKeyPairSync, randomBytes, sign as rsaSign, type KeyObject } from "node:crypto";
import * as Y from "yjs";
import WebSocket from "ws";
import { HocuspocusProvider, HocuspocusProviderWebsocket } from "@hocuspocus/provider";
import { startFakeFilesSocket, type FakeFilesSocket } from "./fake-files-socket.js";
import { fragmentToMarkdown } from "@aep/collab-doc";
import { dropRoomState, roomState } from "./rooms.js";
import type { PodConfig } from "./pod/config.js";
import type { Clock } from "./pod/expiry.js";
import type { PodListeners } from "./pod/listeners.js";
import type { PodLogLine } from "./pod/log.js";
import { startDev, startPod } from "./pod/start.js";

const ISSUER = "http://thunder.test";
const USER_AUDIENCE = "aep-console-client";
const CONSOLE_ORIGIN = "http://console.ae.localhost:8080";
const PRD_PATH = "specs/requirements/prd.md";
const ROOM = "spec-acme-greeter";

// ---------------------------------------------------------------------------
// The Platform IdP stand-in

interface Idp {
  jwksUrl: string;
  userToken(o?: { ouHandle?: string; name?: string; email?: string; sub?: string; expiresInSec?: number }): string;
  agentToken(clientId: string, o?: { ouHandle?: string; expiresInSec?: number }): string;
  close(): Promise<void>;
}

function signJwt(key: KeyObject, claims: Record<string, unknown>): string {
  const b64 = (v: unknown) => Buffer.from(JSON.stringify(v)).toString("base64url");
  const input = `${b64({ alg: "RS256", kid: "k1", typ: "JWT" })}.${b64({ iss: ISSUER, ...claims })}`;
  return `${input}.${rsaSign("RSA-SHA256", Buffer.from(input), key).toString("base64url")}`;
}

const expIn = (sec: number) => Math.floor(Date.now() / 1000) + sec;

async function startIdp(): Promise<Idp> {
  const { publicKey, privateKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
  const jwks = JSON.stringify({ keys: [{ ...publicKey.export({ format: "jwk" }), kid: "k1", alg: "RS256", use: "sig" }] });
  const server = createServer((_req, res) => res.writeHead(200, { "content-type": "application/json" }).end(jwks));
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    jwksUrl: `http://127.0.0.1:${(server.address() as AddressInfo).port}/oauth2/jwks`,
    userToken: ({ ouHandle = "acme", name, email, sub = "u-ann", expiresInSec = 600 } = {}) =>
      signJwt(privateKey, {
        aud: USER_AUDIENCE,
        sub,
        ouId: `ou-${ouHandle}`,
        ouHandle,
        exp: expIn(expiresInSec),
        ...(name ? { name } : {}),
        ...(email ? { email } : {}),
      }),
    agentToken: (clientId, { ouHandle = "acme", expiresInSec = 600 } = {}) =>
      signJwt(privateKey, {
        aud: clientId,
        client_id: clientId,
        grant_type: "client_credentials",
        ouId: `ou-${ouHandle}`,
        ouHandle,
        exp: expIn(expiresInSec),
      }),
    close: () => closeServer(server),
  };
}

function closeServer(server: Server): Promise<void> {
  server.closeAllConnections();
  return new Promise((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
}

// ---------------------------------------------------------------------------
// A clock the test steps

interface TestClock extends Clock {
  advance(ms: number): void;
}

function testClock(): TestClock {
  let now = Date.now();
  let timers: { at: number; fn: () => void }[] = [];
  return {
    now: () => now,
    schedule(at, fn) {
      const timer = { at, fn };
      timers.push(timer);
      return () => {
        timers = timers.filter((t) => t !== timer);
      };
    },
    advance(ms) {
      now += ms;
      const due = timers.filter((t) => t.at <= now).sort((a, b) => a.at - b.at);
      timers = timers.filter((t) => t.at > now);
      for (const t of due) t.fn();
    },
  };
}

// ---------------------------------------------------------------------------
// The harness

const FAST_RETRY = { delay: 10, minDelay: 10, maxDelay: 50 };

interface Peer {
  provider: HocuspocusProvider;
  doc: Y.Doc;
  readonly closed: boolean;
}

interface TestCollab {
  idp: Idp;
  files: FakeFilesSocket;
  clock: TestClock;
  lines: PodLogLine[];
  pod: PodListeners;
  join(listener: "public" | "local", room: string, token: string, params?: Record<string, string>): Promise<Peer>;
  participants(room: string): { name: string; email: string }[];
  rawUpgrade(path: string, o?: { origin?: string }): Promise<{ status: number }>;
  close(): Promise<void>;
}

async function waitFor(cond: () => boolean, what: string, ms = 5_000): Promise<void> {
  const until = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > until) throw new Error(`timed out waiting for ${what}`);
    await new Promise((r) => setTimeout(r, 10));
  }
}

function wsUpgrade(url: string, opts: { origin?: string } = {}): Promise<{ status: number }> {
  return new Promise((resolve, reject) => {
    const req = request(url, {
      headers: {
        connection: "Upgrade",
        upgrade: "websocket",
        "sec-websocket-version": "13",
        "sec-websocket-key": randomBytes(16).toString("base64"),
        ...(opts.origin ? { origin: opts.origin } : {}),
      },
    });
    req.on("response", (res) => {
      res.resume();
      resolve({ status: res.statusCode ?? 0 });
    });
    req.on("upgrade", (res, socket) => {
      socket.destroy();
      resolve({ status: res.statusCode ?? 101 });
    });
    req.on("error", reject);
    req.end();
  });
}

async function startTestCollab(o: { unknownProjects?: string[]; allowedOrigins?: string[] } = {}): Promise<TestCollab> {
  const idp = await startIdp();
  const files = await startFakeFilesSocket({
    files: { [PRD_PATH]: "# PRD\n\nThe greeter says hello.\n" },
    ...(o.unknownProjects ? { unknownProjects: o.unknownProjects } : {}),
  });
  const clock = testClock();
  const lines: PodLogLine[] = [];
  const cfg: PodConfig = {
    orgId: "ou-acme",
    orgHandle: "acme",
    issuer: ISSUER,
    jwksUrl: idp.jwksUrl,
    userAudiences: [USER_AUDIENCE],
    agentClientId: "ae-studio-acme",
    allowedOrigins: o.allowedOrigins ?? [CONSOLE_ORIGIN],
    filesSocket: files.path,
    listenPort: 0,
    healthPort: 0,
    localPort: 0,
  };
  const pod = await startPod(cfg, { clock, log: (l) => lines.push(l) });
  const peers: { provider: HocuspocusProvider; socket: HocuspocusProviderWebsocket }[] = [];
  const rooms = new Set<string>();

  return {
    idp,
    files,
    clock,
    lines,
    pod,
    async join(listener, room, token, params = {}) {
      rooms.add(room);
      const base = listener === "public" ? `${pod.publicUrl}/v1/rooms` : pod.localUrl;
      // Connection parameters ride in the upgrade URL's query.
      const query = new URLSearchParams(params).toString();
      const socket = new HocuspocusProviderWebsocket({
        url: `${base.replace(/^http/, "ws")}${query ? `?${query}` : ""}`,
        WebSocketPolyfill: WebSocket,
        // Short reconnect sleeps: a pending one outlives destroy() and holds the process open.
        ...FAST_RETRY,
      });
      const doc = new Y.Doc();
      const provider = new HocuspocusProvider({ websocketProvider: socket, name: room, document: doc, token });
      peers.push({ provider, socket });
      let closed = false;
      provider.on("close", () => {
        closed = true;
      });
      await new Promise<void>((resolve, reject) => {
        const timer = setTimeout(() => reject(new Error("sync timeout")), 10_000);
        provider.on("synced", () => {
          clearTimeout(timer);
          resolve();
        });
        provider.on("authenticationFailed", ({ reason }: { reason: string }) => {
          clearTimeout(timer);
          reject(new Error(reason));
        });
        provider.attach();
      });
      return {
        provider,
        doc,
        get closed() {
          return closed;
        },
      };
    },
    participants: (room) => [...(roomState(room)?.participants.values() ?? [])],
    rawUpgrade: (path, opts = {}) => wsUpgrade(`${pod.publicUrl}${path}`, opts),
    async close() {
      for (const p of peers) {
        p.provider.destroy();
        p.socket.destroy();
      }
      await pod.close();
      await files.close();
      await idp.close();
      for (const room of rooms) dropRoomState(room);
    },
  };
}

/** The seeded PRD as markdown. */
const markdown = (doc: Y.Doc) => fragmentToMarkdown(doc.getXmlFragment(PRD_PATH));

/** The log lines of one event. */
const events = (s: TestCollab, msg: PodLogLine["msg"]) => s.lines.filter((l) => l.msg === msg);

// ---------------------------------------------------------------------------
// Review Focus 4: listener confusion, all four combinations

test("public listener: user JWT ok, agent token refused, credit param ignored", async () => {
  const s = await startTestCollab();
  try {
    const u = s.idp.userToken({ name: "Ann", email: "ann@x" });
    const peer = await s.join("public", ROOM, u, { credit: JSON.stringify({ name: "Mallory", email: "m@x" }) });
    assert.equal(peer.provider.synced, true);
    // The room was seeded from the Files socket's bundle.
    assert.match(markdown(peer.doc), /The greeter says hello/);
    assert.deepEqual(s.participants(ROOM), [{ name: "Ann", email: "ann@x" }]);

    await assert.rejects(s.join("public", ROOM, s.idp.agentToken("ae-studio-acme")), /permission-denied/);
    assert.deepEqual(s.participants(ROOM), [{ name: "Ann", email: "ann@x" }]);
    assert.deepEqual(events(s, "room_auth_refused").at(-1), {
      msg: "room_auth_refused",
      source: "ae-collab",
      listener: "public",
      cause: "token",
    });
  } finally {
    await s.close();
  }
});

test("local listener: agent token ok and credits the named user, user JWT refused", async () => {
  const s = await startTestCollab();
  try {
    await s.join("local", ROOM, s.idp.agentToken("ae-studio-acme"), { credit: JSON.stringify({ name: "Ann", email: "ann@x" }) });
    assert.deepEqual(s.participants(ROOM), [{ name: "Ann", email: "ann@x" }]);

    await assert.rejects(s.join("local", ROOM, s.idp.userToken()), /permission-denied/);
    // Another org's agent client, and this pod's client minted in another org.
    await assert.rejects(
      s.join("local", ROOM, s.idp.agentToken("ae-studio-evil"), { credit: JSON.stringify({ name: "Ann" }) }),
      /permission-denied/,
    );
    await assert.rejects(
      s.join("local", ROOM, s.idp.agentToken("ae-studio-acme", { ouHandle: "evil" }), { credit: JSON.stringify({ name: "Ann" }) }),
      /permission-denied/,
    );
    assert.deepEqual(s.participants(ROOM), [{ name: "Ann", email: "ann@x" }]);
  } finally {
    await s.close();
  }
});

test("local listener: the agent must name the credited user", async () => {
  const s = await startTestCollab();
  try {
    const agent = s.idp.agentToken("ae-studio-acme");
    await assert.rejects(s.join("local", ROOM, agent), /permission-denied/);
    await assert.rejects(s.join("local", ROOM, agent, { credit: "not json" }), /permission-denied/);
    await assert.rejects(s.join("local", ROOM, agent, { credit: JSON.stringify({ email: "a@x" }) }), /permission-denied/);
    assert.deepEqual(
      events(s, "room_auth_refused").map((l) => l.cause),
      ["credit", "credit", "credit"],
    );
    // No email: the noreply address, as for a user token without one.
    await s.join("local", ROOM, agent, { credit: JSON.stringify({ name: "Ann" }) });
    assert.deepEqual(s.participants(ROOM), [{ name: "Ann", email: "Ann@users.noreply.aep.dev" }]);
  } finally {
    await s.close();
  }
});

test("public listener: the participant comes from the verified claims", async () => {
  const s = await startTestCollab();
  try {
    await s.join("public", ROOM, s.idp.userToken({ sub: "u-1", email: "ann@x", name: "Ann" }));
    await s.join("public", ROOM, s.idp.userToken({ sub: "u-2" }));
    assert.deepEqual(s.participants(ROOM), [
      { name: "Ann", email: "ann@x" },
      { name: "u-2", email: "u-2@users.noreply.aep.dev" },
    ]);
  } finally {
    await s.close();
  }
});

// ---------------------------------------------------------------------------
// The room rule and the project lookup

test("room prefix must be the pod org and the project must pass the lookup", async () => {
  const s = await startTestCollab({ unknownProjects: ["ghost"] });
  try {
    const u = s.idp.userToken();
    await assert.rejects(s.join("public", "spec-evil-greeter", u), /permission-denied/);
    await assert.rejects(s.join("public", "spec-acme-ghost", u), /permission-denied/);
    await assert.rejects(s.join("public", "spec-acme-", u), /permission-denied/);
    s.files.failNext(503, "aep_api_unavailable", "lookup");
    await assert.rejects(s.join("public", ROOM, u), /upstream-unavailable/);
    assert.deepEqual(
      events(s, "room_auth_refused").map((l) => l.cause),
      ["room", "project_unknown", "room", "files_unavailable"],
    );
    // A token of another org is refused before the room is even read.
    await assert.rejects(s.join("public", ROOM, s.idp.userToken({ ouHandle: "evil" })), /permission-denied/);
    assert.equal(events(s, "room_auth_refused").at(-1)?.cause, "org");
  } finally {
    await s.close();
  }
});

test("a room whose bundle cannot be read is refused: outage retries, verdict does not", async () => {
  const s = await startTestCollab();
  try {
    const u = s.idp.userToken();
    s.files.failNext(503, "aep_api_unavailable", "bundle");
    await assert.rejects(s.join("public", ROOM, u), /upstream-unavailable/);
    s.files.failNext(404, "path_not_found", "bundle");
    await assert.rejects(s.join("public", ROOM, u), /permission-denied/);
    assert.equal(events(s, "room_seed_failed").length, 2);
    // The next attempt seeds.
    const peer = await s.join("public", ROOM, u);
    assert.match(markdown(peer.doc), /The greeter says hello/);
  } finally {
    await s.close();
  }
});

// ---------------------------------------------------------------------------
// Review Focus 3: token expiry, onTokenSync re-verification

test("connection closes at exp without a synced token; a synced valid token keeps it; a wrong-org sync closes it", async () => {
  const s = await startTestCollab();
  try {
    const a = await s.join("public", ROOM, s.idp.userToken({ expiresInSec: 2 }));
    s.clock.advance(900);
    assert.equal(events(s, "room_token_expired").length, 0, "still inside the token's lifetime");
    s.clock.advance(1_600);
    await waitFor(() => a.closed, "a to close at exp");
    assert.equal(events(s, "room_token_expired").length, 1);

    const b = await s.join("public", ROOM, s.idp.userToken({ expiresInSec: 2 }));
    // The provider's token getter now returns the fresh one.
    b.provider.configuration.token = s.idp.userToken({ expiresInSec: 600 });
    await b.provider.sendToken();
    await waitFor(() => events(s, "room_token_refreshed").length === 1, "b's token sync");
    s.clock.advance(2_500);
    assert.equal(events(s, "room_token_expired").length, 1, "b's old deadline was replaced");

    const c = await s.join("public", ROOM, s.idp.userToken({ expiresInSec: 600 }));
    c.provider.configuration.token = s.idp.userToken({ ouHandle: "evil" });
    await c.provider.sendToken();
    await waitFor(() => c.closed, "c to close on a wrong-org sync");
    assert.deepEqual(events(s, "room_token_refused").at(-1), {
      msg: "room_token_refused",
      source: "ae-collab",
      listener: "public",
      cause: "org",
    });
    assert.equal(b.closed, false);
    // b lives until its synced token's exp.
    s.clock.advance(600_000);
    await waitFor(() => b.closed, "b to close at its synced exp");
  } finally {
    await s.close();
  }
});

test("a synced token must be valid and of the listener's kind", async () => {
  const s = await startTestCollab();
  try {
    const credit = { credit: JSON.stringify({ name: "Ann" }) };
    const garbage = await s.join("public", ROOM, s.idp.userToken());
    garbage.provider.configuration.token = "not-a-jwt";
    await garbage.provider.sendToken();
    await waitFor(() => garbage.closed, "a garbage sync to close");

    // A user JWT synced onto the agent's connection is listener confusion too.
    const agent = await s.join("local", ROOM, s.idp.agentToken("ae-studio-acme"), credit);
    agent.provider.configuration.token = s.idp.userToken();
    await agent.provider.sendToken();
    await waitFor(() => agent.closed, "a user JWT synced on the local listener to close");

    // The agent's own fresh token keeps its connection.
    const agent2 = await s.join("local", ROOM, s.idp.agentToken("ae-studio-acme", { expiresInSec: 2 }), credit);
    agent2.provider.configuration.token = s.idp.agentToken("ae-studio-acme");
    await agent2.provider.sendToken();
    await waitFor(() => events(s, "room_token_refreshed").length === 1, "the agent's sync");
    s.clock.advance(2_500);
    assert.deepEqual(
      events(s, "room_token_refused").map((l) => [l.listener, l.cause]),
      [
        ["public", "token"],
        ["local", "token"],
      ],
    );
    assert.equal(events(s, "room_token_expired").length, 0);
    assert.equal(agent2.closed, false);
  } finally {
    await s.close();
  }
});

test("no log line carries a token", async () => {
  const s = await startTestCollab();
  try {
    const short = s.idp.userToken({ expiresInSec: 2 });
    const a = await s.join("public", ROOM, short);
    await assert.rejects(s.join("public", ROOM, s.idp.agentToken("ae-studio-acme")), /permission-denied/);
    a.provider.configuration.token = s.idp.userToken({ ouHandle: "evil" });
    await a.provider.sendToken();
    await waitFor(() => a.closed, "a to close");
    const logged = JSON.stringify(s.lines);
    assert.doesNotMatch(logged, /eyJ/);
    assert.doesNotMatch(logged, /acme-greeter/);
  } finally {
    await s.close();
  }
});

// ---------------------------------------------------------------------------
// The public listener's upgrade rules

// U2: the absent-Origin case is the phase-2 rule while the old agents bridge exists; phase 3 Task 3.22 flips it to 403.
test("Origin (phase 2 rule): listed origin ok, unlisted origin refused, absent Origin (non-browser client) allowed", async () => {
  const s = await startTestCollab({ allowedOrigins: [CONSOLE_ORIGIN] });
  try {
    assert.equal((await s.rawUpgrade("/v1/rooms", { origin: CONSOLE_ORIGIN })).status, 101);
    assert.equal((await s.rawUpgrade("/v1/rooms", { origin: "https://evil.example" })).status, 403);
    assert.equal((await s.rawUpgrade("/v1/rooms", {})).status, 101);
    assert.equal((await s.rawUpgrade("/collab", { origin: CONSOLE_ORIGIN })).status, 404);
    // The local listener checks no Origin and takes any path.
    assert.equal((await wsUpgrade(`${s.pod.localUrl}/anything`, { origin: "https://evil.example" })).status, 101);
  } finally {
    await s.close();
  }
});

test("close ends open room sockets on both listeners", async () => {
  const s = await startTestCollab();
  try {
    // Raw sockets, not providers: a provider would reconnect into the close.
    const open = (url: string) =>
      new Promise<WebSocket>((resolve, reject) => {
        const ws = new WebSocket(url.replace(/^http/, "ws"));
        ws.once("open", () => resolve(ws));
        ws.once("error", reject);
      });
    const sockets = [await open(`${s.pod.publicUrl}/v1/rooms`), await open(s.pod.localUrl)];
    const closed = sockets.map((ws) => new Promise<void>((resolve) => ws.once("close", () => resolve())));
    await s.pod.close();
    await Promise.all(closed);
    await assert.rejects(fetch(`${s.pod.healthUrl}/readyz`));
    await assert.rejects(wsUpgrade(`${s.pod.localUrl}/`));
  } finally {
    await s.close();
  }
});

// ---------------------------------------------------------------------------
// Dev mode

test("dev mode: no token, rooms seeded from the dev fixtures", async () => {
  const lines: PodLogLine[] = [];
  const dev = await startDev(
    { allowedOrigins: [], listenPort: 0, healthPort: 0, localPort: 0 },
    { log: (l) => lines.push(l) },
  );
  const socket = new HocuspocusProviderWebsocket({
    url: dev.localUrl.replace(/^http/, "ws"),
    WebSocketPolyfill: WebSocket,
    ...FAST_RETRY,
  });
  const doc = new Y.Doc();
  const provider = new HocuspocusProvider({ websocketProvider: socket, name: "spec-default-demo-shop", document: doc, token: "" });
  try {
    await new Promise<void>((resolve, reject) => {
      provider.on("synced", () => resolve());
      provider.on("authenticationFailed", ({ reason }: { reason: string }) => reject(new Error(reason)));
      provider.attach();
    });
    assert.match(markdown(doc), /Demo Shop/);
    assert.deepEqual(
      [...(roomState("spec-default-demo-shop")?.participants.values() ?? [])],
      [{ name: "Dev User", email: "dev@localhost" }],
    );
    assert.ok(lines.some((l) => l.msg === "pod_dev_mode"));
  } finally {
    provider.destroy();
    socket.destroy();
    await dev.close();
    dropRoomState("spec-default-demo-shop");
  }
});
