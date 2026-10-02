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

// The helpers live in this file, not a shared module: collab keeps its tests
// in src/, where a test-only module would read as dead code to knip
// --production.

import { test } from "node:test";
import assert from "node:assert/strict";
import { createServer, request, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { generateKeyPairSync, randomBytes, sign as rsaSign, type KeyObject } from "node:crypto";
import { selectModes } from "./modes.js";
import { loadDevConfig, loadPodConfig, type PodConfig } from "./pod/config.js";
import type { PodLogLine } from "./pod/log.js";
import { startPod } from "./pod/start.js";

const ISSUER = "http://thunder.test";
const USER_AUDIENCE = "aep-console-client";

/**
 * A local stand-in for the Platform IdP: one RS256 key served as a JWKS over
 * a loopback HTTP server (the pod's real remote-JWKS path), and signers for
 * the token shapes the pod sees.
 */
interface TestKeys {
  jwksUrl: string;
  /** Any claims over the IdP's key; `iss` and `exp` default to the IdP's. */
  sign(claims: Record<string, unknown>): string;
  user(ouHandle: string, ouId: string): string;
  m2m(client: "publisher" | "ae-internal"): string;
  close(): Promise<void>;
}

function signJwt(key: KeyObject, claims: Record<string, unknown>): string {
  const b64 = (v: unknown) => Buffer.from(JSON.stringify(v)).toString("base64url");
  const input = `${b64({ alg: "RS256", kid: "k1", typ: "JWT" })}.${b64({
    iss: ISSUER,
    exp: Math.floor(Date.now() / 1000) + 3600,
    ...claims,
  })}`;
  return `${input}.${rsaSign("RSA-SHA256", Buffer.from(input), key).toString("base64url")}`;
}

async function testKeys(): Promise<TestKeys> {
  const { publicKey, privateKey } = generateKeyPairSync("rsa", { modulusLength: 2048 });
  const jwks = JSON.stringify({ keys: [{ ...publicKey.export({ format: "jwk" }), kid: "k1", alg: "RS256", use: "sig" }] });
  const server = createServer((_req, res) => {
    res.writeHead(200, { "content-type": "application/json" }).end(jwks);
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const cc = { grant_type: "client_credentials" };
  return {
    jwksUrl: `http://127.0.0.1:${(server.address() as AddressInfo).port}/oauth2/jwks`,
    sign: (claims) => signJwt(privateKey, claims),
    user: (ouHandle, ouId) => signJwt(privateKey, { aud: USER_AUDIENCE, sub: `user-${ouHandle}`, ouId, ouHandle }),
    m2m: (client) =>
      client === "publisher"
        ? signJwt(privateKey, { ...cc, aud: "aep-publisher-default", client_id: "aep-publisher-default", ouId: "ou-1", ouHandle: "default" })
        : signJwt(privateKey, { ...cc, aud: "ae-studio-default", client_id: "ae-studio-default", ouId: "ou-1", ouHandle: "default" }),
    close: () => closeServer(server),
  };
}

function closeServer(server: Server): Promise<void> {
  server.closeAllConnections();
  return new Promise((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
}

/** The full pod env the ResourceType renders, minus anything a test overrides. */
function podEnv(over: Record<string, string> = {}): Record<string, string> {
  return {
    AE_ORG_ID: "ou-1",
    AE_ORG_HANDLE: "default",
    AE_IDP_ISSUER: ISSUER,
    AE_IDP_JWKS_URL: "http://unused",
    AE_USER_AUDIENCES: USER_AUDIENCE,
    AE_AGENT_CLIENT_ID: "ae-studio-default",
    AE_ALLOWED_ORIGINS: "http://console.ae.localhost:8080,http://localhost:8090",
    AE_FILES_SOCKET: "/run/ae/files/files.sock",
    ...over,
  };
}

/** A port free right now: bound on 0, read, released. The pod config refuses 0. */
async function freePort(): Promise<string> {
  const probe = createServer();
  await new Promise<void>((resolve) => probe.listen(0, resolve));
  const { port } = probe.address() as AddressInfo;
  await closeServer(probe);
  return String(port);
}

/** A pod config on free ports, verifying against the test keys. */
async function podCfg(keys: TestKeys, over: Record<string, string> = {}): Promise<PodConfig> {
  const ports = { AE_LISTEN_PORT: await freePort(), AE_HEALTH_PORT: await freePort() };
  const cfg = loadPodConfig(podEnv({ AE_IDP_JWKS_URL: keys.jwksUrl, ...ports, ...over }));
  assert.ok(cfg);
  // The local port is fixed (8091) in a pod; a test takes any free one.
  return { ...cfg, localPort: 0 };
}

/** A WebSocket opening handshake; resolves with the status the server answered. */
function wsUpgrade(url: string, opts: { origin?: string } = {}): Promise<{ status: number; contentType?: string | undefined }> {
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
      resolve({ status: res.statusCode ?? 0, contentType: res.headers["content-type"] });
    });
    req.on("upgrade", (res, socket) => {
      socket.destroy();
      resolve({ status: res.statusCode ?? 101 });
    });
    req.on("error", reject);
    req.end();
  });
}

test("pod: HTTP under /v1 is gated; room upgrades pass the origin rule and the path", async () => {
  const keys = await testKeys();
  const lines: PodLogLine[] = [];
  const pod = await startPod(await podCfg(keys), { log: (l) => lines.push(l) });
  try {
    const v1 = (p: string, token?: string) =>
      fetch(`${pod.publicUrl}${p}`, { headers: token ? { authorization: `Bearer ${token}` } : {} });

    const r = await v1("/v1/rooms");
    assert.equal(r.status, 401);
    assert.equal(r.headers.get("content-type"), "application/problem+json");
    assert.equal(r.headers.get("www-authenticate"), "Bearer");
    assert.equal(((await r.json()) as { code: string }).code, "unauthenticated");
    assert.equal((await v1("/v1/rooms", "not-a-jwt")).status, 401);

    const foreign = await v1("/v1/rooms", keys.user("x", "ou-2"));
    assert.equal(foreign.status, 403);
    assert.equal(((await foreign.json()) as { code: string }).code, "org_mismatch");
    // Same OU id, another handle: both claims must match.
    assert.equal((await v1("/v1/rooms", keys.user("other", "ou-1"))).status, 403);

    // M2M never passes /v1, even the pod's own agent client, nor a client
    // token that names the console user audience with the pod's org.
    assert.equal((await v1("/v1/rooms", keys.m2m("publisher"))).status, 401);
    assert.equal((await v1("/v1/rooms", keys.m2m("ae-internal"))).status, 401);
    const clientOnUserAudience = keys.sign({
      grant_type: "client_credentials",
      aud: USER_AUDIENCE,
      client_id: USER_AUDIENCE,
      sub: USER_AUDIENCE,
      ouId: "ou-1",
      ouHandle: "default",
    });
    assert.equal((await v1("/v1/rooms", clientOnUserAudience)).status, 401);

    // The pod's issuer, exactly: a user token of the pod's org signed by the
    // same key under another issuer is refused.
    const foreignIssuer = keys.sign({ iss: "http://other-idp.test", aud: USER_AUDIENCE, sub: "u", ouId: "ou-1", ouHandle: "default" });
    const foreign401 = await v1("/v1/rooms", foreignIssuer);
    assert.equal(foreign401.status, 401);
    assert.equal(foreign401.headers.get("www-authenticate"), 'Bearer error="invalid_token"');

    // The gate sits before route matching: unknown /v1 paths, /v1 itself and
    // any casing of the prefix are 401 before they are 404.
    assert.equal((await v1("/v1/nope")).status, 401);
    assert.equal((await v1("/v1")).status, 401);
    assert.equal((await v1("/V1/rooms")).status, 401);

    // An admitted user reaches no operation yet.
    const own = await v1("/v1/rooms", keys.user("default", "ou-1"));
    assert.equal(own.status, 404);
    assert.deepEqual(await own.json(), {
      type: "about:blank",
      title: "Not Found",
      status: 404,
      detail: "no such route",
      code: "not_found",
    });

    // Outside /v1 nothing is served, and health is not on the public port.
    const other = await fetch(`${pod.publicUrl}/healthz`);
    assert.equal(other.status, 404);
    assert.equal(other.headers.get("content-type"), "application/problem+json");
    assert.equal((await fetch(`${pod.publicUrl}/readyz`)).status, 404);

    // Upgrades: Origin first, then the path; /v1/rooms goes to the Room,
    // which authenticates in-protocol. An absent Origin is accepted while the
    // phase-2 agents bridge exists (U2; phase 3 Task 3.22 makes it 403).
    const evil = await wsUpgrade(`${pod.publicUrl}/v1/rooms`, { origin: "https://evil.example" });
    assert.equal(evil.status, 403);
    assert.equal(evil.contentType, "application/problem+json");
    assert.equal((await wsUpgrade(`${pod.publicUrl}/v1/rooms`)).status, 101);
    assert.equal((await wsUpgrade(`${pod.publicUrl}/other`, { origin: "https://evil.example" })).status, 403);
    assert.equal((await wsUpgrade(`${pod.publicUrl}/other`, { origin: "http://localhost:8090" })).status, 404);
    assert.equal((await wsUpgrade(`${pod.publicUrl}/other`)).status, 404);
    const ok = await wsUpgrade(`${pod.publicUrl}/v1/rooms`, { origin: "http://localhost:8090" });
    assert.equal(ok.status, 101);
    assert.equal((await wsUpgrade(`${pod.publicUrl}/v1/rooms?x=1`, { origin: "http://console.ae.localhost:8080" })).status, 101);
    // The path is matched as sent: no casing or trailing-slash variants.
    assert.equal((await wsUpgrade(`${pod.publicUrl}/V1/rooms`, { origin: "http://localhost:8090" })).status, 404);
    assert.equal((await wsUpgrade(`${pod.publicUrl}/v1/rooms/`, { origin: "http://localhost:8090" })).status, 404);

    // The local listener serves upgrades only.
    assert.equal((await fetch(`${pod.localUrl}/v1/rooms`)).status, 404);

    assert.equal((await fetch(`${pod.healthUrl}/healthz`)).status, 200);
    assert.equal((await fetch(`${pod.healthUrl}/readyz`)).status, 200);
    assert.equal((await fetch(`${pod.healthUrl}/v1/rooms`)).status, 404);
  } finally {
    await pod.close();
    await keys.close();
  }
  assert.deepEqual(
    lines.map((l) => l.msg),
    ["pod_health_listening", "pod_public_listening", "pod_local_listening", "pod_listeners_stopped"],
  );
});

test("pod: close ends open connections and stops both listeners", async () => {
  const keys = await testKeys();
  const pod = await startPod(await podCfg(keys), { log: () => {} });
  // A keep-alive connection left open must not hold close() up.
  await fetch(`${pod.publicUrl}/v1/rooms`, { headers: { connection: "keep-alive" } });
  await pod.close();
  await keys.close();
  await assert.rejects(fetch(`${pod.publicUrl}/v1/rooms`));
  await assert.rejects(fetch(`${pod.healthUrl}/readyz`));
});

test("pod: a busy public port fails the start and frees the health port", async () => {
  const keys = await testKeys();
  const busy = createServer();
  await new Promise<void>((resolve) => busy.listen(0, resolve));
  const port = String((busy.address() as AddressInfo).port);
  const lines: PodLogLine[] = [];
  try {
    await assert.rejects(
      startPod(await podCfg(keys, { AE_LISTEN_PORT: port }), { log: (l) => lines.push(l) }),
      /EADDRINUSE/,
    );
    assert.deepEqual(lines.map((l) => l.msg), ["pod_health_listening"]);
    // No half-started process: the health port (and its ready probe) is gone.
    await assert.rejects(fetch(`http://127.0.0.1:${lines[0]?.port}/healthz`));
  } finally {
    await closeServer(busy);
    await keys.close();
  }
});

test("pod config: none without AE_ORG_ID", () => {
  assert.equal(loadPodConfig({}), null);
  assert.equal(loadPodConfig({ AE_ORG_ID: " " }), null);
});

test("pod config: defaults and lists", () => {
  const cfg = loadPodConfig(
    podEnv({ AE_USER_AUDIENCES: " aep-console-client , other ,", AE_ALLOWED_ORIGINS: " http://localhost:8090 ," }),
  );
  assert.deepEqual(cfg, {
    orgId: "ou-1",
    orgHandle: "default",
    issuer: ISSUER,
    jwksUrl: "http://unused",
    userAudiences: ["aep-console-client", "other"],
    agentClientId: "ae-studio-default",
    allowedOrigins: ["http://localhost:8090"],
    filesSocket: "/run/ae/files/files.sock",
    listenPort: 8081,
    healthPort: 9081,
    localPort: 8091,
  });
});

test("pod config: AE_ORG_ID without the rest fails, naming every missing key and no value", () => {
  assert.throws(
    () => loadPodConfig({ AE_ORG_ID: "ou-1" }),
    new Error(
      "ae-collab pod env: missing AE_ORG_HANDLE, missing AE_IDP_ISSUER, missing AE_IDP_JWKS_URL, " +
        "missing AE_USER_AUDIENCES, missing AE_AGENT_CLIENT_ID, missing AE_ALLOWED_ORIGINS, missing AE_FILES_SOCKET",
    ),
  );
  assert.throws(() => loadPodConfig(podEnv({ AE_ALLOWED_ORIGINS: " , " })), /missing AE_ALLOWED_ORIGINS/);
  // An entry that is not a bare origin would never match a browser's Origin.
  assert.throws(
    () => loadPodConfig(podEnv({ AE_ALLOWED_ORIGINS: "http://localhost:8090/,secret-looking-value" })),
    (err: Error) => err.message === "ae-collab pod env: invalid AE_ALLOWED_ORIGINS",
  );
  assert.throws(() => loadPodConfig(podEnv({ AE_LISTEN_PORT: "http" })), /invalid AE_LISTEN_PORT/);
  assert.throws(() => loadPodConfig(podEnv({ AE_HEALTH_PORT: "70000" })), /invalid AE_HEALTH_PORT/);
  // 0 means "any free port": never a pod's port.
  assert.throws(
    () => loadPodConfig(podEnv({ AE_LISTEN_PORT: "0", AE_HEALTH_PORT: "0" })),
    /invalid AE_LISTEN_PORT, invalid AE_HEALTH_PORT/,
  );
});

test("modes: a full pod env is pod mode", () => {
  assert.deepEqual(selectModes(podEnv()), { mode: "pod", config: loadPodConfig(podEnv()) });
});

test("modes: no dev mode in pod mode: a legacy key in a pod env fails the boot", () => {
  for (const [legacy, keys] of [
    [{ COLLAB_DEV: "1" }, "COLLAB_DEV"],
    [{ COLLAB_DEV: "0" }, "COLLAB_DEV"],
    [{ COLLAB_MOCK_BFF: "1" }, "COLLAB_MOCK_BFF"],
    [{ AEP_API_BASE: "http://aep-api/api/v1" }, "AEP_API_BASE"],
    [{ COLLAB_DEV: "1", COLLAB_MOCK_BFF: "1", AEP_API_BASE: "" }, "COLLAB_DEV, COLLAB_MOCK_BFF, AEP_API_BASE"],
  ] as const) {
    // The message names the keys, never their values.
    assert.throws(
      () => selectModes({ ...podEnv(), ...legacy }),
      (err: Error) => err.message === `ae-collab pod env: legacy keys set: ${keys}`,
    );
  }
});

test("modes: COLLAB_DEV alone is dev mode: the pod's listeners, fixed local port", () => {
  assert.deepEqual(selectModes({ COLLAB_DEV: "1" }), {
    mode: "dev",
    config: { allowedOrigins: [], listenPort: 8081, healthPort: 9081, localPort: 8091 },
  });
  assert.deepEqual(loadDevConfig({ AE_ALLOWED_ORIGINS: "http://localhost:5173", AE_LISTEN_PORT: "18081" }), {
    allowedOrigins: ["http://localhost:5173"],
    listenPort: 18081,
    healthPort: 9081,
    localPort: 8091,
  });
  assert.throws(
    () => loadDevConfig({ AE_ALLOWED_ORIGINS: "http://localhost:5173/", AE_HEALTH_PORT: "0" }),
    (err: Error) => err.message === "ae-collab dev env: invalid AE_ALLOWED_ORIGINS, invalid AE_HEALTH_PORT",
  );
});

test("modes: the removed legacy server's keys configure nothing", () => {
  // The chart Deployment's env (until Task 2.16 deletes it) boots nothing.
  assert.throws(() => selectModes({ AEP_API_BASE: "http://aep-api:9090/api/v1/" }), /ae-collab: no config/);
  assert.throws(() => selectModes({ COLLAB_MOCK_BFF: "1" }), /ae-collab: no config/);
});

test("modes: boot fails with no config, and with a partial pod config", () => {
  assert.throws(() => selectModes({}), /ae-collab: no config/);
  assert.throws(() => selectModes({ COLLAB_DEV: "0" }), /ae-collab: no config/);
  assert.throws(() => selectModes({ AE_ORG_ID: "ou-1", COLLAB_DEV: "1" }), /missing AE_ORG_HANDLE/);
});
