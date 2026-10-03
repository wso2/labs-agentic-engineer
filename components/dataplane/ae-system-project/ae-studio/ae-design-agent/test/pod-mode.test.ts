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

import { test } from "node:test";
import assert from "node:assert/strict";
import { startPodListeners } from "../src/pod/listeners.js";
import { loadPodConfig } from "../src/pod/config.js";
import { startEdge } from "./helpers/edge.js";

/** The full pod env the RT renders, minus anything a test overrides. */
function podEnv(over: Record<string, string> = {}): Record<string, string> {
  return {
    AE_ORG_ID: "ou-1",
    AE_ORG_HANDLE: "default",
    AE_IDP_ISSUER: "http://thunder.test",
    AE_IDP_JWKS_URL: "http://unused",
    AE_USER_AUDIENCES: "aep-console-client",
    AE_MCP_SOCKET: "/run/ae/mcp/mcp.sock",
    AE_TURN_SOCKET: "/run/ae/mcp/turn.sock",
    AE_COLLAB_LOCAL_URL: "ws://127.0.0.1:8091",
    AE_SNAPSHOTS_DIR: "/snapshots",
    ...over,
  };
}

test("pod mode: /v1 is gated before routing, health is separate", async () => {
  const edge = await startEdge();
  try {
    const v1 = (p: string, token?: string) =>
      fetch(`${edge.base}${p}`, { headers: token ? { authorization: `Bearer ${token}` } : {} });
    const own = await edge.token();

    const missing = await v1("/v1/projects/p/turns/active");
    assert.equal(missing.status, 401);
    assert.equal(missing.headers.get("content-type"), "application/problem+json");
    assert.equal(missing.headers.get("www-authenticate"), "Bearer");
    assert.equal(((await missing.json()) as { code: string }).code, "unauthenticated");
    assert.equal((await v1("/v1/projects/p/turns/active", "not-a-jwt")).status, 401);

    const foreign = await v1("/v1/projects/p/turns/active", await edge.token({ ouHandle: "e2e-other" }));
    assert.equal(foreign.status, 403);
    assert.equal(((await foreign.json()) as { code: string }).code, "org_mismatch");

    // The gate admits the pod's user: the /v1 routes answer (D-1).
    assert.equal((await v1("/v1/projects/p/turns/active", own)).status, 204);
    const r = await v1("/v1/x", own);
    assert.equal(r.status, 404);
    assert.deepEqual(await r.json(), {
      type: "about:blank",
      title: "Not Found",
      status: 404,
      detail: "no such route",
      code: "not_found",
    });

    // M2M never passes /v1: the publisher, the AE-only client, and a client
    // token that names the user audience with the pod's org.
    assert.equal((await v1("/v1/x", await edge.m2m("publisher"))).status, 401);
    assert.equal((await v1("/v1/x", await edge.m2m("ae-internal"))).status, 401);
    assert.equal((await v1("/v1/x", await edge.m2mOnUserAudience("acme", "ou-acme"))).status, 401);

    // The gate covers /v1 itself and any casing of the prefix.
    assert.equal((await v1("/v1")).status, 401);
    assert.equal((await v1("/V1/x")).status, 401);

    // Outside /v1 nothing is served, and health is not on the public port.
    const other = await fetch(`${edge.base}/conversations/c1`);
    assert.equal(other.status, 404);
    assert.equal(other.headers.get("content-type"), "application/problem+json");
    assert.equal((await fetch(`${edge.base}/healthz`)).status, 404);
    assert.equal((await fetch(`${edge.base}/readyz`)).status, 404);

    assert.equal((await fetch(`${edge.healthUrl}/healthz`)).status, 200);
    assert.equal((await fetch(`${edge.healthUrl}/readyz`)).status, 200);
    assert.equal((await fetch(`${edge.healthUrl}/v1/x`)).status, 404);
  } finally {
    await edge.close();
  }
  assert.deepEqual(
    edge.logs.map((l) => l.msg),
    ["pod_health_listening", "pod_public_listening", "pod_turn_socket_listening", "pod_listeners_stopped"],
  );
});

test("pod mode: an IdP whose keys cannot be fetched is a 503 retry, not a 401", async () => {
  const edge = await startEdge({ jwks: () => Promise.reject(new Error("connect ECONNREFUSED")) });
  try {
    const r = await fetch(`${edge.base}/v1/x`, { headers: { authorization: `Bearer ${await edge.token()}` } });
    assert.equal(r.status, 503);
    assert.equal(r.headers.get("retry-after"), "5");
    assert.equal(((await r.json()) as { code: string }).code, "idp_unavailable");
  } finally {
    await edge.close();
  }
});

test("no pod config without AE_ORG_ID", () => {
  assert.equal(loadPodConfig({}), null);
  assert.equal(loadPodConfig({ AE_ORG_ID: "" }), null);
});

test("pod config: defaults, lists, and the secret revisions", () => {
  const cfg = loadPodConfig(podEnv({ AE_USER_AUDIENCES: " aep-console-client , other ,", AE_SECRET_REV: "r1" }));
  assert.deepEqual(cfg, {
    orgId: "ou-1",
    orgHandle: "default",
    issuer: "http://thunder.test",
    jwksUrl: "http://unused",
    userAudiences: ["aep-console-client", "other"],
    listenPort: 8080,
    healthPort: 9080,
    mcpSocket: "/run/ae/mcp/mcp.sock",
    turnSocket: "/run/ae/mcp/turn.sock",
    collabLocalUrl: "ws://127.0.0.1:8091",
    snapshotsDir: "/snapshots",
    expectedSecretRev: "",
    secretRev: "r1",
  });
});

test("pod config: AE_ORG_ID without the rest of the pod env fails, naming every missing key", () => {
  assert.throws(
    () => loadPodConfig({ AE_ORG_ID: "ou-1" }),
    /missing AE_ORG_HANDLE, missing AE_IDP_ISSUER, missing AE_IDP_JWKS_URL, missing AE_USER_AUDIENCES, missing AE_MCP_SOCKET, missing AE_TURN_SOCKET, missing AE_COLLAB_LOCAL_URL, missing AE_SNAPSHOTS_DIR/,
  );
  assert.throws(() => loadPodConfig(podEnv({ AE_USER_AUDIENCES: " , " })), /missing AE_USER_AUDIENCES/);
  assert.throws(() => loadPodConfig(podEnv({ AE_LISTEN_PORT: "http" })), /invalid AE_LISTEN_PORT/);
  assert.throws(() => loadPodConfig(podEnv({ AE_HEALTH_PORT: "70000" })), /invalid AE_HEALTH_PORT/);
  assert.throws(() => loadPodConfig(podEnv({ AE_COLLAB_LOCAL_URL: "http://127.0.0.1:8091" })), /invalid AE_COLLAB_LOCAL_URL/);
});

test("pod mode refuses to start on a stale secret rev", async () => {
  const cfg = loadPodConfig(podEnv({ AE_EXPECTED_SECRET_REV: "r2", AE_SECRET_REV: "r1" }));
  assert.ok(cfg);
  await assert.rejects(startPodListeners(cfg, { edge: {} as never }), /secret revision mismatch/);
});
