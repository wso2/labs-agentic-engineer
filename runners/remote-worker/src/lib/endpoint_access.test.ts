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
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import {
  AUTH_BRIDGE_HOST,
  CURL_CONFIG_FILE,
  PLAYWRIGHT_CLI_CONFIG_FILE,
  curlResolveEntries,
  hostResolverRules,
  probeEndpoints,
  resolveAuthGatewayAddress,
  writeCurlResolveConfig,
  writePlaywrightCliConfig,
} from "./endpoint_access.js";

const GATEWAY = "10.43.246.32";

/** A lookup stub that answers every name with one address. */
function stubLookup(address = GATEWAY) {
  const asked: string[] = [];
  return {
    asked,
    fn: async (host: string) => {
      asked.push(host);
      return { address };
    },
  };
}

async function tmpDir(): Promise<string> {
  return fs.promises.mkdtemp(path.join(os.tmpdir(), "aep-curlrc-test-"));
}

// The `.localhost` filter is the whole point: RFC 6761 is what makes those names
// unreachable, and pinning anything else would freeze an address curl already
// resolves correctly — and freeze it wrong the moment the cluster changed.
test("curlResolveEntries pins only .localhost hosts", async () => {
  const lookup = stubLookup();
  const entries = await curlResolveEntries(
    [
      { component: "webapp", url: "http://app.openchoreoapis.localhost:19080/" },
      { component: "api", url: "https://api.example.com/v1" },
    ],
    lookup.fn,
  );
  assert.deepEqual(entries, [{ host: "app.openchoreoapis.localhost", port: 19080, address: GATEWAY }]);
  assert.deepEqual(lookup.asked, ["app.openchoreoapis.localhost"]);
});

// A cloud plane names no `.localhost` endpoint, so the whole mechanism has to be
// a no-op there rather than something that merely happens to write nothing
// useful.
test("curlResolveEntries returns nothing when no endpoint is .localhost", async () => {
  const entries = await curlResolveEntries(
    [{ component: "api", url: "https://api.example.com/" }],
    stubLookup().fn,
  );
  assert.deepEqual(entries, []);
});

// curl's `resolve` is keyed by host:port, so the port the URL implies has to be
// the port written — an entry on the wrong port silently does nothing.
test("curlResolveEntries defaults the port from the scheme", async () => {
  const entries = await curlResolveEntries(
    [
      { component: "plain", url: "http://a.localhost/" },
      { component: "tls", url: "https://b.localhost/" },
    ],
    stubLookup().fn,
  );
  assert.deepEqual(
    entries.map((e) => [e.host, e.port]),
    [
      ["a.localhost", 80],
      ["b.localhost", 443],
    ],
  );
});

test("curlResolveEntries emits one entry per host:port", async () => {
  const lookup = stubLookup();
  const entries = await curlResolveEntries(
    [
      { component: "webapp", url: "http://gw.localhost:19080/app" },
      { component: "api", url: "http://gw.localhost:19080/api" },
    ],
    lookup.fn,
  );
  assert.equal(entries.length, 1);
  assert.equal(lookup.asked.length, 1);
});

// Skipped, never thrown: probeEndpoints is what decides whether the run may
// proceed, and it names the endpoint that actually failed rather than the lookup
// in front of it.
test("curlResolveEntries skips a name that will not resolve", async () => {
  const lines: string[] = [];
  const entries = await curlResolveEntries(
    [
      { component: "gone", url: "http://gone.localhost:19080/" },
      { component: "ok", url: "http://ok.localhost:19080/" },
    ],
    async (host) => {
      if (host === "gone.localhost") throw new Error("ENOTFOUND");
      return { address: GATEWAY };
    },
    (l) => lines.push(l),
  );
  assert.deepEqual(entries.map((e) => e.host), ["ok.localhost"]);
  assert.equal(lines.length, 1);
  assert.match(lines[0], /gone\.localhost/);
});

test("curlResolveEntries skips a malformed URL without throwing", async () => {
  const lines: string[] = [];
  const entries = await curlResolveEntries(
    [{ component: "bad", url: "not-a-url" }],
    stubLookup().fn,
    (l) => lines.push(l),
  );
  assert.deepEqual(entries, []);
  assert.equal(lines.length, 1);
});

test("writeCurlResolveConfig writes one resolve line per entry, 0600", async () => {
  const dir = await tmpDir();
  const written = await writeCurlResolveConfig(dir, [
    { host: "a.localhost", port: 19080, address: GATEWAY },
    { host: "b.localhost", port: 443, address: "10.0.0.9" },
  ]);
  assert.equal(written, path.join(dir, CURL_CONFIG_FILE));

  const body = await fs.promises.readFile(written as string, "utf8");
  assert.match(body, /^resolve = a\.localhost:19080:10\.43\.246\.32$/m);
  assert.match(body, /^resolve = b\.localhost:443:10\.0\.0\.9$/m);

  const stat = await fs.promises.stat(written as string);
  assert.equal(stat.mode & 0o777, 0o600);
});

// An empty file and "this run needed no overrides" are different facts, and the
// caller logs them differently.
test("writeCurlResolveConfig writes nothing when there are no entries", async () => {
  const dir = await tmpDir();
  const written = await writeCurlResolveConfig(dir, []);
  assert.equal(written, undefined);
  assert.deepEqual(await fs.promises.readdir(dir), []);
});

// Overwriting must land the new content at the same path with the same mode —
// `mode` is honoured only on create, which is why the write is staged+renamed.
test("writeCurlResolveConfig replaces an existing config", async () => {
  const dir = await tmpDir();
  await fs.promises.writeFile(path.join(dir, CURL_CONFIG_FILE), "stale\n", { mode: 0o644 });
  const written = await writeCurlResolveConfig(dir, [
    { host: "a.localhost", port: 80, address: GATEWAY },
  ]);
  const body = await fs.promises.readFile(written as string, "utf8");
  assert.doesNotMatch(body, /stale/);
  assert.equal((await fs.promises.stat(written as string)).mode & 0o777, 0o600);
  // The staging directory must not survive the write.
  assert.deepEqual(await fs.promises.readdir(dir), [CURL_CONFIG_FILE]);
});

// One MAP per host and no port: Chromium's rule maps names, and the URL keeps
// its own port. Per-host rather than the wildcard the test config uses, so a
// name nobody probed is never captured.
test("hostResolverRules maps each resolved host once, without a port", () => {
  const args = hostResolverRules([
    { host: "a.localhost", port: 19080, address: GATEWAY },
    { host: "b.localhost", port: 443, address: "10.0.0.9" },
    // Same host on a second port — one MAP, not two.
    { host: "a.localhost", port: 8443, address: GATEWAY },
  ]);
  assert.deepEqual(args, [
    `--host-resolver-rules=MAP a.localhost ${GATEWAY},MAP b.localhost 10.0.0.9`,
  ]);
});

test("hostResolverRules returns nothing when there is nothing to map", () => {
  assert.deepEqual(hostResolverRules([]), []);
  // …and an auth address alone is not something to map: with no endpoint there
  // is no local plane to be on.
  assert.deepEqual(hostResolverRules([], "172.18.0.1"), []);
});

// The IdP is the one wildcard, because the validation context names the app's
// endpoints and never the IdP — a pattern is the only handle. Appended last so a
// reader sees the specific rules first.
test("hostResolverRules appends the IdP pattern when an auth address is known", () => {
  const args = hostResolverRules([{ host: "a.localhost", port: 19080, address: GATEWAY }], "172.18.0.1");
  assert.deepEqual(args, [
    `--host-resolver-rules=MAP a.localhost ${GATEWAY},MAP *.openchoreo.localhost 172.18.0.1`,
  ]);
});

// DNS is measurably wrong for the IdP (the rewrite sends *.openchoreo.localhost
// to the DATA plane, which does not serve it), so the bridge is looked up by
// name — and an unresolvable bridge must not cost the app endpoints their rules.
test("resolveAuthGatewayAddress resolves the bridge, and swallows a failure", async () => {
  const ok = await resolveAuthGatewayAddress(async (host) => {
    assert.equal(host, AUTH_BRIDGE_HOST);
    return { address: "172.18.0.1" };
  });
  assert.equal(ok, "172.18.0.1");

  const missing = await resolveAuthGatewayAddress(async () => {
    throw new Error("ENOTFOUND");
  });
  assert.equal(missing, undefined);
});

// launchOptions.args ONLY. Naming browser.browserName here would leave `channel`
// undefined, which re-enables the Chromium sandbox — and that cannot start as
// the pod's non-root user (ADR-0007). This assertion is the guard on that.
test("writePlaywrightCliConfig writes only launch args, 0600", async () => {
  const dir = await tmpDir();
  const written = await writePlaywrightCliConfig(
    dir,
    [{ host: "a.localhost", port: 19080, address: GATEWAY }],
    async () => ({ address: "172.18.0.1" }),
  );
  assert.equal(written, path.join(dir, PLAYWRIGHT_CLI_CONFIG_FILE));

  const body = await fs.promises.readFile(written as string, "utf8");
  assert.doesNotMatch(body, /browserName/);
  assert.deepEqual(JSON.parse(body), {
    browser: {
      launchOptions: {
        args: [
          `--host-resolver-rules=MAP a.localhost ${GATEWAY},MAP *.openchoreo.localhost 172.18.0.1`,
        ],
      },
    },
  });
  assert.equal((await fs.promises.stat(written as string)).mode & 0o777, 0o600);
  assert.deepEqual(await fs.promises.readdir(dir), [PLAYWRIGHT_CLI_CONFIG_FILE]);
});

// An unresolvable bridge is not a reason to lose the endpoint rules: those are
// what the preflight already proved reachable, and the IdP hop may never be
// taken. It degrades to exactly the coverage this file had before the IdP rule.
test("writePlaywrightCliConfig still maps the endpoints when the bridge will not resolve", async () => {
  const dir = await tmpDir();
  const written = await writePlaywrightCliConfig(
    dir,
    [{ host: "a.localhost", port: 19080, address: GATEWAY }],
    async () => {
      throw new Error("ENOTFOUND");
    },
  );
  const cfg = JSON.parse(await fs.promises.readFile(written as string, "utf8"));
  assert.deepEqual(cfg.browser.launchOptions.args, [
    `--host-resolver-rules=MAP a.localhost ${GATEWAY}`,
  ]);
});

// $PLAYWRIGHT_MCP_CONFIG is fatal when it points at a missing file, so the env
// is derived from this file's existence. A file left by an earlier run would
// therefore pin a cluster that no longer exists — it has to be removed, not just
// left unwritten.
test("writePlaywrightCliConfig removes a stale config when nothing needs mapping", async () => {
  const dir = await tmpDir();
  const stale = path.join(dir, PLAYWRIGHT_CLI_CONFIG_FILE);
  await fs.promises.writeFile(stale, "stale\n");
  // A resolvable bridge must not be enough on its own — no endpoint means no
  // local plane to map, and the file has to go.
  const written = await writePlaywrightCliConfig(dir, [], async () => ({ address: "172.18.0.1" }));
  assert.equal(written, undefined);
  assert.equal(fs.existsSync(stale), false);
  assert.deepEqual(await fs.promises.readdir(dir), []);
});

/** A fetch stub that answers each call with the next queued status, or throws. */
function stubFetch(outcomes: (number | Error)[]) {
  const seen: { url: string; redirect?: string }[] = [];
  const impl = (async (url: string | URL | Request, init?: RequestInit) => {
    seen.push({ url: String(url), redirect: init?.redirect });
    const next = outcomes.shift();
    if (next instanceof Error) throw next;
    return new Response("body", { status: next ?? 200 });
  }) as unknown as typeof fetch;
  return { impl, seen };
}

// Status is deliberately not evidence: an api-configuration-gated endpoint
// answers 401 and an API root answers 404, both from a perfectly healthy
// deployment. Only a transport failure means the platform cannot show the agent
// the system it is meant to validate.
test("probeEndpoints treats any HTTP answer as reachable", async () => {
  const { impl } = stubFetch([401, 404, 500, 302]);
  const unreachable = await probeEndpoints(
    [
      { component: "a", url: "http://a.localhost/" },
      { component: "b", url: "http://b.localhost/" },
      { component: "c", url: "http://c.localhost/" },
      { component: "d", url: "http://d.localhost/" },
    ],
    { fetchImpl: impl },
  );
  assert.deepEqual(unreachable, []);
});

test("probeEndpoints reports a transport failure, with its code", async () => {
  const refused = Object.assign(new Error("fetch failed"), {
    cause: { code: "ECONNREFUSED" },
  });
  const { impl } = stubFetch([200, refused]);
  const unreachable = await probeEndpoints(
    [
      { component: "up", url: "http://up.localhost/" },
      { component: "down", url: "http://down.localhost/" },
    ],
    { fetchImpl: impl },
  );
  assert.equal(unreachable.length, 1);
  assert.equal(unreachable[0].component, "down");
  assert.equal(unreachable[0].reason, "ECONNREFUSED");
});

// A login redirect points at the IdP on the CONTROL plane — a different hop with
// its own resolution story. Following it here would turn an endpoint that
// answered into a false negative.
test("probeEndpoints does not follow redirects", async () => {
  const { impl, seen } = stubFetch([302]);
  await probeEndpoints([{ component: "a", url: "http://a.localhost/" }], { fetchImpl: impl });
  assert.equal(seen[0].redirect, "manual");
});
