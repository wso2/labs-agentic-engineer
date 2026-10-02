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
import { startPodListeners, type PodLogLine } from "../src/pod/listeners.js";
import { loadPodConfig } from "../src/pod/config.js";
import { selectModes } from "../src/modes.js";
import { testKeys } from "./helpers/test-keys.js";

/** The full pod env the RT renders, minus anything a test overrides. */
function podEnv(over: Record<string, string> = {}): Record<string, string> {
  return {
    AE_ORG_ID: "ou-1",
    AE_ORG_HANDLE: "default",
    AE_IDP_ISSUER: "http://thunder.test",
    AE_IDP_JWKS_URL: "http://unused",
    AE_USER_AUDIENCES: "aep-console-client",
    ...over,
  };
}

test("pod mode: /v1 is gated before routing, health is separate", async () => {
  const keys = await testKeys();
  const cfg = loadPodConfig(podEnv({ AE_IDP_ISSUER: keys.issuer, AE_LISTEN_PORT: "0", AE_HEALTH_PORT: "0" }));
  assert.ok(cfg);
  const lines: PodLogLine[] = [];
  const pod = await startPodListeners(cfg, { jwks: keys.jwks, log: (l) => lines.push(l) });
  try {
    const v1 = (p: string, token?: string) =>
      fetch(`${pod.publicUrl}${p}`, { headers: token ? { authorization: `Bearer ${token}` } : {} });
    const own = await keys.user("default", "ou-1");

    const missing = await v1("/v1/projects/p/turns/active");
    assert.equal(missing.status, 401);
    assert.equal(missing.headers.get("content-type"), "application/problem+json");
    assert.equal(((await missing.json()) as { code: string }).code, "unauthenticated");
    assert.equal((await v1("/v1/projects/p/turns/active", "not-a-jwt")).status, 401);

    const foreign = await v1("/v1/projects/p/turns/active", await keys.user("e2e-other", "ou-2"));
    assert.equal(foreign.status, 403);
    assert.equal(((await foreign.json()) as { code: string }).code, "org_mismatch");
    // Same OU id, another handle: both claims must match.
    assert.equal((await v1("/v1/x", await keys.user("other", "ou-1"))).status, 403);

    const r = await v1("/v1/projects/p/turns/active", own);
    assert.equal(r.status, 404);
    assert.equal(r.headers.get("content-type"), "application/problem+json");
    assert.deepEqual(await r.json(), {
      type: "about:blank",
      title: "Not Found",
      status: 404,
      detail: "no such route",
      code: "not_found",
    });

    // M2M never passes /v1: the publisher, the AE-only client, and a client
    // token that names the user audience with the pod's org.
    assert.equal((await v1("/v1/x", await keys.m2m())).status, 401);
    assert.equal((await v1("/v1/x", await keys.m2m("ae-internal"))).status, 401);
    assert.equal((await v1("/v1/x", await keys.m2mOnUserAudience("default", "ou-1"))).status, 401);

    // The gate covers /v1 itself and any casing of the prefix.
    assert.equal((await v1("/v1")).status, 401);
    assert.equal((await v1("/V1/x")).status, 401);

    // Outside /v1 nothing is served, and health is not on the public port.
    const other = await fetch(`${pod.publicUrl}/conversations/c1`);
    assert.equal(other.status, 404);
    assert.equal(other.headers.get("content-type"), "application/problem+json");
    assert.equal((await fetch(`${pod.publicUrl}/healthz`)).status, 404);
    assert.equal((await fetch(`${pod.publicUrl}/readyz`)).status, 404);

    assert.equal((await fetch(`${pod.healthUrl}/healthz`)).status, 200);
    assert.equal((await fetch(`${pod.healthUrl}/readyz`)).status, 200);
    assert.equal((await fetch(`${pod.healthUrl}/v1/x`)).status, 404);
  } finally {
    await pod.close();
  }
  assert.deepEqual(
    lines.map((l) => l.msg),
    ["pod_health_listening", "pod_public_listening", "pod_listeners_stopped"],
  );
});

test("pod mode: an IdP whose keys cannot be fetched is a 503 retry, not a 401", async () => {
  const keys = await testKeys();
  const cfg = loadPodConfig(podEnv({ AE_IDP_ISSUER: keys.issuer, AE_LISTEN_PORT: "0", AE_HEALTH_PORT: "0" }));
  assert.ok(cfg);
  const unreachable = () => Promise.reject(new Error("connect ECONNREFUSED"));
  const pod = await startPodListeners(cfg, { jwks: unreachable, log: () => {} });
  try {
    const r = await fetch(`${pod.publicUrl}/v1/x`, { headers: { authorization: `Bearer ${await keys.user("default", "ou-1")}` } });
    assert.equal(r.status, 503);
    assert.equal(r.headers.get("retry-after"), "5");
    assert.equal(((await r.json()) as { code: string }).code, "idp_unavailable");
  } finally {
    await pod.close();
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
    expectedSecretRev: "",
    secretRev: "r1",
  });
});

test("pod config: AE_ORG_ID without the rest of the pod env fails, naming every missing key", () => {
  assert.throws(
    () => loadPodConfig({ AE_ORG_ID: "ou-1" }),
    /missing AE_ORG_HANDLE, missing AE_IDP_ISSUER, missing AE_IDP_JWKS_URL, missing AE_USER_AUDIENCES/,
  );
  assert.throws(() => loadPodConfig(podEnv({ AE_USER_AUDIENCES: " , " })), /missing AE_USER_AUDIENCES/);
  assert.throws(() => loadPodConfig(podEnv({ AE_LISTEN_PORT: "http" })), /invalid AE_LISTEN_PORT/);
  assert.throws(() => loadPodConfig(podEnv({ AE_HEALTH_PORT: "70000" })), /invalid AE_HEALTH_PORT/);
});

test("pod mode refuses to start on a stale secret rev", async () => {
  const cfg = loadPodConfig(podEnv({ AE_EXPECTED_SECRET_REV: "r2", AE_SECRET_REV: "r1" }));
  assert.ok(cfg);
  await assert.rejects(startPodListeners(cfg), /secret revision mismatch/);
});

test("modes: no legacy server without legacy config", () => {
  const modes = selectModes(podEnv());
  assert.equal(modes.legacy, false);
  assert.equal(modes.pod?.orgId, "ou-1");
});

test("modes: no pod listeners without AE_ORG_ID (the chart Deployment)", () => {
  assert.deepEqual(selectModes({ AGENT_JWT_SECRET: "s" }), { pod: null, legacy: true });
  assert.deepEqual(selectModes({ AGENT_JWT_JWKS_URL: "http://jwks" }), { pod: null, legacy: true });
});

test("modes: both configs start both servers", () => {
  const modes = selectModes({ ...podEnv(), AGENT_JWT_SECRET: "s" });
  assert.equal(modes.legacy, true);
  assert.ok(modes.pod);
});

test("modes: boot fails with neither config, and with a partial pod config", () => {
  assert.throws(() => selectModes({}), /neither pod \(AE_\*\) nor legacy \(AGENT_JWT_\*\) config is set/);
  assert.throws(() => selectModes({ AGENT_JWT_SECRET: "" }), /neither pod/);
  assert.throws(() => selectModes({ AE_ORG_ID: "ou-1" }), /missing AE_ORG_HANDLE/);
});
