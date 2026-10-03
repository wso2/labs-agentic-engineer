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
import { connect, type AddressInfo } from "node:net";
import { generateKeyPairSync, randomBytes, sign as rsaSign, type KeyObject } from "node:crypto";
import * as Y from "yjs";
import WebSocket from "ws";
import { HocuspocusProvider, HocuspocusProviderWebsocket } from "@hocuspocus/provider";
import { startFakeFilesSocket, type FakeFilesSocket } from "./fake-files-socket.js";
import { fragmentToMarkdown, setDocFile } from "@aep/collab-doc";
import { dropRoomState, roomState } from "./rooms.js";
import { RESTARTING } from "./pod/commits.js";
import type { PodConfig } from "./pod/config.js";
import type { Clock } from "./pod/expiry.js";
import { startPodListeners, type PodListeners } from "./pod/listeners.js";
import type { PodLogLine } from "./pod/log.js";
import type { CommitCadence } from "./pod/room-server.js";
import { startDev, startPod } from "./pod/start.js";
import { Hocuspocus } from "@hocuspocus/server";

const ISSUER = "http://thunder.test";
const USER_AUDIENCE = "aep-console-client";
const CONSOLE_ORIGIN = "http://console.ae.localhost:8080";
const PRD_PATH = "specs/requirements/prd.md";
const ROOM = "spec-acme-greeter";

/**
 * A browser's socket: `ws` sends no Origin unless told, and the public
 * listener refuses an upgrade without a listed one.
 */
class ConsoleWebSocket extends WebSocket {
  constructor(address: string | URL, protocols?: string | string[]) {
    super(address, protocols, { origin: CONSOLE_ORIGIN });
  }
}

// ---------------------------------------------------------------------------
// The Platform IdP stand-in

interface Idp {
  jwksUrl: string;
  /** While down, the JWKS endpoint answers 503. */
  setDown(down: boolean): void;
  userToken(o?: { ouHandle?: string; name?: string; email?: string; sub?: string; expiresInSec?: number }): string;
  /** `extra` adds (or overrides) claims, to shape the token like a live one. */
  agentToken(clientId: string, o?: { ouHandle?: string; expiresInSec?: number; extra?: Record<string, unknown> }): string;
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
  let down = false;
  const server = createServer((_req, res) =>
    down ? res.writeHead(503).end() : res.writeHead(200, { "content-type": "application/json" }).end(jwks),
  );
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return {
    jwksUrl: `http://127.0.0.1:${(server.address() as AddressInfo).port}/oauth2/jwks`,
    setDown: (d) => {
      down = d;
    },
    userToken: ({ ouHandle = "acme", name, email, sub = "u-ann", expiresInSec = 600 } = {}) =>
      signJwt(
        privateKey,
        {
          aud: USER_AUDIENCE,
          sub,
          ouId: `ou-${ouHandle}`,
          ouHandle,
          exp: expIn(expiresInSec),
          ...(name ? { name } : {}),
          ...(email ? { email } : {}),
        },
      ),
    agentToken: (clientId, { ouHandle = "acme", expiresInSec = 600, extra = {} } = {}) =>
      signJwt(privateKey, {
        aud: clientId,
        client_id: clientId,
        grant_type: "client_credentials",
        ouId: `ou-${ouHandle}`,
        ouHandle,
        exp: expIn(expiresInSec),
        ...extra,
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
  /** Steps to `offsetMs` around the token's `exp`: the deadline is test time, whatever real time it is. */
  advanceToExp(token: string, offsetMs?: number): void;
}

/** A JWT's `exp`, read without verifying (the test minted it). */
function expOf(token: string): number {
  return (JSON.parse(Buffer.from(token.split(".")[1] ?? "", "base64url").toString()) as { exp: number }).exp;
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
    advanceToExp(token, offsetMs = 0) {
      this.advance(Math.max(0, expOf(token) * 1000 + offsetMs - now));
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
  /** Every stateless message the server sent this peer, parsed. */
  stateless: { type: string; id?: string; message?: string; warnings?: unknown }[];
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

async function startTestCollab(
  o: { unknownProjects?: string[]; allowedOrigins?: string[]; cadence?: Partial<CommitCadence>; warnings?: { path: string; message: string }[] } = {},
): Promise<TestCollab> {
  const idp = await startIdp();
  const files = await startFakeFilesSocket({
    files: { [PRD_PATH]: "# PRD\n\nThe greeter says hello.\n" },
    ...(o.unknownProjects ? { unknownProjects: o.unknownProjects } : {}),
    ...(o.warnings ? { warnings: o.warnings } : {}),
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
  const pod = await startPod(cfg, { clock, log: (l) => lines.push(l), ...(o.cadence ? { cadence: o.cadence } : {}) });
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
        WebSocketPolyfill: listener === "public" ? ConsoleWebSocket : WebSocket,
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
      const stateless: Peer["stateless"] = [];
      provider.on("stateless", ({ payload }: { payload: string }) => stateless.push(JSON.parse(payload) as Peer["stateless"][number]));
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
        stateless,
        get closed() {
          return closed;
        },
      };
    },
    participants: (room) => [...(roomState(room)?.participants.values() ?? [])],
    rawUpgrade: (path, opts = {}) => wsUpgrade(`${pod.publicUrl}${path}`, opts),
    async close() {
      for (const p of peers) {
        // A socket the server closed (pod.close() ends the room sockets
        // before it flushes) schedules a reconnect `delay` ms after the
        // close, and that timer survives destroy() and turns the socket back
        // on (provider 4.3), so a reconnect loop would outlive the test.
        p.socket.connect = () => Promise.resolve();
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

/** Appends a paragraph to the seeded PRD, as an editor does. */
function typeInto(doc: Y.Doc, text: string): void {
  const para = new Y.XmlElement("paragraph");
  doc.getXmlFragment(PRD_PATH).push([para]);
  para.push([new Y.XmlText(text)]);
}

/** Sends a `flush` and resolves with the server's answer to it. */
async function flush(peer: Peer, id: string): Promise<Peer["stateless"][number]> {
  peer.provider.sendStateless(JSON.stringify({ type: "flush", id }));
  let answer: Peer["stateless"][number] | undefined;
  await waitFor(() => (answer = peer.stateless.find((m) => m.id === id)) !== undefined, `the answer to ${id}`);
  return answer!;
}

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

test("local listener: a token shaped like Thunder's live client_credentials token is accepted (D-7)", async () => {
  const s = await startTestCollab();
  try {
    // The claim set of a Thunder client_credentials access token for the
    // ae-studio-<org> client (research/03 §3; claim names of the phase-1
    // mints, no values): `sub` is the application's entity id, not the client
    // id; `aud` defaults to the client id; iat/nbf/jti/scope and the OU's name
    // ride along. Only aud, client_id, grant_type, ouId and ouHandle decide.
    const now = Math.floor(Date.now() / 1000);
    const live = s.idp.agentToken("ae-studio-acme", {
      extra: { sub: "0f9c6a52-3c1e-4d8e-9a4b-2b7f1d0c5e11", iat: now, nbf: now, jti: "7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f", scope: "", ouName: "Acme" },
    });
    // The design agent's credit when the turn's credit has no name: the user id (local-room.ts).
    await s.join("local", ROOM, live, { credit: JSON.stringify({ name: "u-ann", email: "" }) });
    assert.deepEqual(s.participants(ROOM), [{ name: "u-ann", email: "u-ann@users.noreply.aep.dev" }]);
    assert.deepEqual(events(s, "room_auth_refused"), []);
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
    const aToken = s.idp.userToken({ expiresInSec: 2 });
    const a = await s.join("public", ROOM, aToken);
    s.clock.advanceToExp(aToken, -100);
    assert.equal(events(s, "room_token_expired").length, 0, "still inside the token's lifetime");
    s.clock.advanceToExp(aToken);
    await waitFor(() => a.closed, "a to close at exp");
    assert.equal(events(s, "room_token_expired").length, 1);

    const bShort = s.idp.userToken({ expiresInSec: 2 });
    const b = await s.join("public", ROOM, bShort);
    // The provider's token getter now returns the fresh one.
    const bFresh = s.idp.userToken({ expiresInSec: 600 });
    b.provider.configuration.token = bFresh;
    await b.provider.sendToken();
    await waitFor(() => events(s, "room_token_refreshed").length === 1, "b's token sync");
    s.clock.advanceToExp(bShort, 1_000);
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
    s.clock.advanceToExp(bFresh, -100);
    assert.equal(events(s, "room_token_expired").length, 1);
    s.clock.advanceToExp(bFresh);
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
    const agentShort = s.idp.agentToken("ae-studio-acme", { expiresInSec: 2 });
    const agent2 = await s.join("local", ROOM, agentShort, credit);
    agent2.provider.configuration.token = s.idp.agentToken("ae-studio-acme");
    await agent2.provider.sendToken();
    await waitFor(() => events(s, "room_token_refreshed").length === 1, "the agent's sync");
    s.clock.advanceToExp(agentShort, 1_000);
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

test("an IdP whose keys cannot be fetched means retry, not denied", async () => {
  const s = await startTestCollab();
  try {
    // Nothing fetched the keys yet, and the IdP is down.
    s.idp.setDown(true);
    const u = s.idp.userToken();
    await assert.rejects(s.join("public", ROOM, u), /upstream-unavailable/);
    assert.equal(events(s, "room_auth_refused").at(-1)?.cause, "idp_unavailable");
    const http = await fetch(`${s.pod.publicUrl}/v1/rooms`, { headers: { authorization: `Bearer ${u}` } });
    assert.equal(http.status, 503);
    assert.equal(http.headers.get("retry-after"), "5");
    assert.equal(((await http.json()) as { code: string }).code, "idp_unavailable");
    // Back up: the same token joins.
    s.idp.setDown(false);
    await s.join("public", ROOM, u);
  } finally {
    await s.close();
  }
});

test("a synced token for another user of the org is logged, value-free", async () => {
  const s = await startTestCollab();
  try {
    const peer = await s.join("public", ROOM, s.idp.userToken({ sub: "u-ann" }));
    peer.provider.configuration.token = s.idp.userToken({ sub: "u-bob" });
    await peer.provider.sendToken();
    await waitFor(() => events(s, "room_token_refreshed").length === 1, "the sync");
    assert.deepEqual(events(s, "room_token_subject_changed"), [
      { msg: "room_token_subject_changed", source: "ae-collab", listener: "public" },
    ]);
    assert.doesNotMatch(JSON.stringify(s.lines), /u-ann|u-bob/);
  } finally {
    await s.close();
  }
});

test("a refused room load leaves no participant behind", async () => {
  const s = await startTestCollab();
  try {
    s.files.failNext(503, "aep_api_unavailable", "bundle");
    await assert.rejects(s.join("public", ROOM, s.idp.userToken({ sub: "u-mallory", name: "Mallory" })), /upstream-unavailable/);
    assert.deepEqual(s.participants(ROOM), []);
    await s.join("public", ROOM, s.idp.userToken({ name: "Ann", email: "ann@x" }));
    assert.deepEqual(s.participants(ROOM), [{ name: "Ann", email: "ann@x" }]);
  } finally {
    await s.close();
  }
});

// ---------------------------------------------------------------------------
// The public listener's upgrade rules

test("Origin: listed origin ok; unlisted or absent Origin refused on the public listener; local listener has no Origin check", async () => {
  const s = await startTestCollab({ allowedOrigins: [CONSOLE_ORIGIN] });
  try {
    assert.equal((await s.rawUpgrade("/v1/rooms", { origin: CONSOLE_ORIGIN })).status, 101);
    assert.equal((await s.rawUpgrade("/v1/rooms", { origin: "https://evil.example" })).status, 403);
    assert.equal((await s.rawUpgrade("/v1/rooms", {})).status, 403);
    assert.equal((await s.rawUpgrade("/collab", { origin: CONSOLE_ORIGIN })).status, 404);
    // The local listener checks no Origin and takes any path: the in-pod
    // agent joins there without one.
    assert.equal((await wsUpgrade(`${s.pod.localUrl}/`)).status, 101);
    assert.equal((await wsUpgrade(`${s.pod.localUrl}/anything`, { origin: "https://evil.example" })).status, 101);
  } finally {
    await s.close();
  }
});

test("a frame over 32 MiB closes the socket (1009) before any auth", async () => {
  const s = await startTestCollab();
  try {
    for (const url of [`${s.pod.publicUrl}/v1/rooms`, s.pod.localUrl]) {
      const ws = new ConsoleWebSocket(url.replace(/^http/, "ws"));
      await new Promise((resolve, reject) => {
        ws.once("open", resolve);
        ws.once("error", reject);
      });
      const closed = new Promise<number>((resolve) => ws.once("close", (code: number) => resolve(code)));
      // Only the frame header, declaring 32 MiB + 1 (a masked binary frame
      // with a 64-bit length): the server refuses on the header, so its close
      // frame is not lost to a reset mid-upload.
      const header = Buffer.alloc(14);
      header[0] = 0x82;
      header[1] = 0x80 | 127;
      header.writeBigUInt64BE(BigInt((32 << 20) + 1), 2);
      (ws as unknown as { _socket: NodeJS.WritableStream })._socket.write(header);
      assert.equal(await closed, 1009);
    }
  } finally {
    await s.close();
  }
});

test("a malformed absolute-form upgrade target is refused, not a crash", async () => {
  const s = await startTestCollab();
  try {
    const { port } = new URL(s.pod.localUrl);
    const reply = await new Promise<string>((resolve, reject) => {
      const sock = connect(Number(port), "127.0.0.1", () => {
        sock.write(
          "GET http://[bad/ HTTP/1.1\r\nHost: x\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n" +
            `Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: ${randomBytes(16).toString("base64")}\r\n\r\n`,
        );
      });
      let data = "";
      sock.on("data", (d) => (data += d.toString()));
      sock.on("close", () => resolve(data));
      sock.on("error", reject);
      setTimeout(() => sock.destroy(), 1_000);
    });
    // Either refused by the HTTP parser or upgraded and then held to auth;
    // the process survives and the listener still serves.
    void reply;
    assert.equal((await wsUpgrade(`${s.pod.localUrl}/`)).status, 101);
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
        const ws = new ConsoleWebSocket(url.replace(/^http/, "ws"));
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

// ---------------------------------------------------------------------------
// Commits through the Files socket (no token)

test("a quiet period commits the room and every peer hears the commit's warnings", async () => {
  const s = await startTestCollab({
    cadence: { debounceMs: 50, maxDebounceMs: 500 },
    warnings: [{ path: PRD_PATH, message: "soft" }],
  });
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken({ name: "Ann", email: "ann@x" }));
    const agent = await s.join("local", ROOM, s.idp.agentToken("ae-studio-acme"), { credit: JSON.stringify({ name: "Bob", email: "bob@x" }) });
    typeInto(ann.doc, "Typed by Ann.");
    await waitFor(() => s.files.commits().length === 1, "the debounced commit");
    assert.match(s.files.file(PRD_PATH)!, /Typed by Ann\./);
    // One commit for the room, crediting everyone in it; the socket call carried no token.
    assert.match(s.files.commits()[0]!.message, /Co-authored-by: Ann <ann@x>\nCo-authored-by: Bob <bob@x>$/);
    for (const peer of [ann, agent]) {
      await waitFor(() => peer.stateless.some((m) => m.type === "flush-warnings"), "flush-warnings");
      assert.deepEqual(
        peer.stateless.find((m) => m.type === "flush-warnings"),
        { type: "flush-warnings", warnings: [{ path: PRD_PATH, message: "soft" }] },
      );
    }
  } finally {
    await s.close();
  }
});

test("a debounced flush that meets an outage tells the room and keeps the edit for the next one", async () => {
  const s = await startTestCollab({ cadence: { debounceMs: 50, maxDebounceMs: 500 } });
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    s.files.failNext(503, "disk_full", "apply");
    typeInto(ann.doc, "Kept through the outage.");
    await waitFor(() => ann.stateless.some((m) => m.type === "flush-error"), "flush-error");
    assert.deepEqual(ann.stateless.find((m) => m.type === "flush-error"), { type: "flush-error", message: RESTARTING });
    assert.equal(s.files.commits().length, 0);
    assert.equal(ann.closed, false, "a failed flush never closes a connection");
    // The next debounce retries the same changes.
    typeInto(ann.doc, "And one more.");
    await waitFor(() => s.files.commits().length === 1, "the retried commit");
    assert.match(s.files.file(PRD_PATH)!, /Kept through the outage\.[\s\S]*And one more\./);
  } finally {
    await s.close();
  }
});

test("flush: acked flushed after the commit; an outage is acked flush-error and the next flush lands", async () => {
  const s = await startTestCollab();
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    // A clean room acks at once: HEAD is already the truth.
    assert.deepEqual(await flush(ann, "f0"), { type: "flushed", id: "f0" });
    assert.equal(s.files.commits().length, 0);

    typeInto(ann.doc, "Before the build.");
    await new Promise((r) => setTimeout(r, 100)); // the update reaches the server
    s.files.failNext(503, "aep_api_unavailable", "apply");
    assert.deepEqual(await flush(ann, "f1"), { type: "flush-error", id: "f1", message: RESTARTING });
    assert.equal(s.files.commits().length, 0);

    assert.deepEqual(await flush(ann, "f2"), { type: "flushed", id: "f2" });
    assert.equal(s.files.commits().length, 1);
    assert.match(s.files.file(PRD_PATH)!, /Before the build\./);
  } finally {
    await s.close();
  }
});

test("a last leave whose final flush meets an outage keeps the room loaded; the edit survives to a rejoin", async () => {
  const s = await startTestCollab();
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    typeInto(ann.doc, "Typed before leaving.");
    await flush(ann, "sync"); // the edit reached the server, and landed
    typeInto(ann.doc, "Typed last.");
    await new Promise((r) => setTimeout(r, 100)); // the update reaches the server
    // Both the pending debounced store and the final flush after it meet the outage.
    s.files.failNext(503, "disk_full", "apply", 2);
    ann.provider.destroy();
    await waitFor(() => events(s, "room_final_flush_deferred").length === 1, "the deferred final flush");
    assert.equal(s.files.commits().length, 1);

    // The room was not unloaded, so it was not reseeded from git: the edit is still there.
    const bob = await s.join("public", ROOM, s.idp.userToken({ sub: "u-bob" }));
    assert.match(markdown(bob.doc), /Typed last\./);
    assert.deepEqual(await flush(bob, "f"), { type: "flushed", id: "f" });
    assert.match(s.files.file(PRD_PATH)!, /Typed last\./);
  } finally {
    await s.close();
  }
});

test("a last leave with one refused path commits the room's other edits, then unloads", async () => {
  const s = await startTestCollab();
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    typeInto(ann.doc, "Saved at the last leave.");
    setDocFile(ann.doc, "notes/scratch.json", '{"not":"a spec"}');
    await new Promise((r) => setTimeout(r, 100)); // the updates reach the server
    ann.provider.destroy();
    await waitFor(() => roomState(ROOM) === undefined, "the room to unload");
    assert.match(s.files.file(PRD_PATH)!, /Saved at the last leave\./);
    assert.equal(s.files.file("notes/scratch.json"), undefined);
    assert.equal(events(s, "room_final_flush_deferred").length, 0);
  } finally {
    await s.close();
  }
});

test("a room that unloads at the last leave with refused paths logs how many, value-free", async () => {
  const s = await startTestCollab();
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    typeInto(ann.doc, "Saved at the last leave.");
    setDocFile(ann.doc, "notes/scratch.json", '{"not":"a spec"}');
    await new Promise((r) => setTimeout(r, 100)); // the updates reach the server
    ann.provider.destroy();
    await waitFor(() => roomState(ROOM) === undefined, "the room to unload");
    assert.deepEqual(events(s, "room_unloaded_with_refused"), [
      { msg: "room_unloaded_with_refused", source: "ae-collab", count: 1 },
    ]);
    // No line names the refused path, its content or the room.
    const logged = JSON.stringify(s.lines);
    for (const value of ["notes/scratch.json", "scratch", "a spec", '\\"not\\"', ROOM]) {
      assert.equal(logged.includes(value), false, value);
    }
  } finally {
    await s.close();
  }
});

test("a room that unloads with nothing refused logs no refused count", async () => {
  const s = await startTestCollab();
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    typeInto(ann.doc, "Saved at the last leave.");
    await new Promise((r) => setTimeout(r, 100)); // the update reaches the server
    ann.provider.destroy();
    await waitFor(() => roomState(ROOM) === undefined, "the room to unload");
    assert.equal(events(s, "room_unloaded_with_refused").length, 0);
  } finally {
    await s.close();
  }
});

test("a deferred final flush is retried on a backoff with nobody in the room, lands, and the room unloads", async () => {
  const s = await startTestCollab({ cadence: { retryFirstMs: 50, retryMaxMs: 200 } });
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    typeInto(ann.doc, "Typed before an outage.");
    await new Promise((r) => setTimeout(r, 100)); // the update reaches the server
    // The pending store, the final flush and the first two retries all meet the outage.
    s.files.failNext(503, "disk_full", "apply", 4);
    ann.provider.destroy();
    await waitFor(() => s.files.commits().length === 1, "the retried commit, with no rejoin");
    assert.match(s.files.file(PRD_PATH)!, /Typed before an outage\./);
    assert.ok(events(s, "room_final_flush_deferred").length >= 1);
    await waitFor(() => roomState(ROOM) === undefined, "the room to unload once its edits landed");
  } finally {
    await s.close();
  }
});

test("a deferred room is flushed by shutdown, and no retry runs after it", async () => {
  const s = await startTestCollab({ cadence: { retryFirstMs: 60_000 } });
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    typeInto(ann.doc, "Saved by the shutdown flush.");
    await new Promise((r) => setTimeout(r, 100)); // the update reaches the server
    s.files.failNext(503, "disk_full", "apply", 2);
    ann.provider.destroy();
    await waitFor(() => events(s, "room_final_flush_deferred").length === 1, "the deferral");
    await s.pod.close();
    assert.equal(s.files.commits().length, 1);
    assert.match(s.files.file(PRD_PATH)!, /Saved by the shutdown flush\./);
  } finally {
    await s.close();
  }
});

test("close: listeners stop accepting, the room sockets end, then the rooms are flushed", async () => {
  const s = await startTestCollab();
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    typeInto(ann.doc, "Saved on SIGTERM.");
    await new Promise((r) => setTimeout(r, 100)); // the update reaches the server
    await s.pod.close();
    assert.equal(s.files.commits().length, 1, "the shutdown flush committed the room");
    assert.match(s.files.file(PRD_PATH)!, /Saved on SIGTERM\./);
    assert.equal(events(s, "room_flush_committed").length, 1, "one commit: the unload and the shutdown flush share it");
  } finally {
    await s.close();
  }
});

test("close: no edit reaches a room after its shutdown flush read it, and close waits for the commit", async () => {
  const s = await startTestCollab();
  try {
    const ann = await s.join("public", ROOM, s.idp.userToken());
    const bob = await s.join("public", ROOM, s.idp.userToken({ sub: "u-bob" }));
    typeInto(ann.doc, "Typed before SIGTERM.");
    await waitFor(() => /Typed before SIGTERM\./.test(markdown(bob.doc)), "the edit to reach the room");
    const apply = s.files.holdNext("apply");
    let closed = false;
    const closing = s.pod.close().then(() => {
      closed = true;
    });
    await apply.arrived; // the shutdown's commit is on its way
    typeInto(ann.doc, "Typed during the shutdown flush.");
    await new Promise((r) => setTimeout(r, 150)); // an open socket would deliver it now
    assert.equal(closed, false, "close waits for the commit");
    apply.release();
    await closing;

    const committed = s.files.file(PRD_PATH)!;
    assert.match(committed, /Typed before SIGTERM\./);
    // Whatever the room held is committed: an edit the room never took stays
    // with its author, whose next room takes it.
    if (/Typed during the shutdown flush\./.test(markdown(bob.doc))) {
      assert.match(committed, /Typed during the shutdown flush\./, "the room took the edit but never committed it");
    }
    assert.match(markdown(ann.doc), /Typed during the shutdown flush\./);
    assert.equal(s.files.commits().length, 1);
  } finally {
    await s.close();
  }
});

test("close: drain runs after the listeners stop accepting, and ends the open sockets before it flushes", async () => {
  let atDrain: { socketOpen: boolean; upgradeRefused: boolean; ready: number; endedBeforeFlush: boolean } | undefined;
  let localUrl = "";
  let healthUrl = "";
  const open: WebSocket[] = [];
  const pod = await startPodListeners(
    { allowedOrigins: [], listenPort: 0, healthPort: 0, localPort: 0 },
    {
      rooms: new Hocuspocus(),
      gate: () => Promise.resolve(null),
      log: () => {},
      drain: async (endSockets) => {
        const upgradeRefused = await wsUpgrade(`${localUrl}/`).then(
          () => false,
          () => true,
        );
        const socketOpen = open[0]?.readyState === WebSocket.OPEN;
        const ready = (await fetch(`${healthUrl}/readyz`)).status;
        const ended = new Promise<void>((resolve) => open[0]!.once("close", () => resolve()));
        endSockets();
        await ended;
        atDrain = { socketOpen, upgradeRefused, ready, endedBeforeFlush: true };
      },
    },
  );
  localUrl = pod.localUrl;
  healthUrl = pod.healthUrl;
  const ws = new WebSocket(localUrl.replace(/^http/, "ws"));
  open.push(ws);
  await new Promise((resolve, reject) => {
    ws.once("open", resolve);
    ws.once("error", reject);
  });
  await pod.close();
  assert.deepEqual(atDrain, { socketOpen: true, upgradeRefused: true, ready: 503, endedBeforeFlush: true });
});

test("dev mode: a flush is acked through the fake Files socket", async () => {
  const dev = await startDev({ allowedOrigins: [], listenPort: 0, healthPort: 0, localPort: 0 }, { log: () => {} });
  const socket = new HocuspocusProviderWebsocket({ url: dev.localUrl.replace(/^http/, "ws"), WebSocketPolyfill: WebSocket, ...FAST_RETRY });
  const doc = new Y.Doc();
  const provider = new HocuspocusProvider({ websocketProvider: socket, name: "spec-default-demo-shop", document: doc, token: "" });
  const stateless: Peer["stateless"] = [];
  provider.on("stateless", ({ payload }: { payload: string }) => stateless.push(JSON.parse(payload) as Peer["stateless"][number]));
  try {
    await new Promise<void>((resolve, reject) => {
      provider.on("synced", () => resolve());
      provider.on("authenticationFailed", ({ reason }: { reason: string }) => reject(new Error(reason)));
      provider.attach();
    });
    provider.sendStateless(JSON.stringify({ type: "flush", id: "dev-1" }));
    await waitFor(() => stateless.some((m) => m.id === "dev-1"), "the dev flush ack");
    assert.deepEqual(stateless.find((m) => m.id === "dev-1"), { type: "flushed", id: "dev-1" });
  } finally {
    provider.destroy();
    socket.destroy();
    await dev.close();
    dropRoomState("spec-default-demo-shop");
  }
});

test("reference documents are never seeded into a room or its baseline", async () => {
  const s = await startTestCollab();
  try {
    const ref = "specs/requirements/references/rfp.pdf";
    s.files.pushExternal(ref, "JVBERi0xLjQK");
    const ann = await s.join("public", ROOM, s.idp.userToken());
    assert.equal(ann.doc.getMap("files").has(ref), false);
    assert.equal(roomState(ROOM)?.baseline.has(ref), false);
    assert.equal(roomState(ROOM)?.baseline.has(PRD_PATH), true);
  } finally {
    await s.close();
  }
});
