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
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import {
  AUTH_BRIDGE_HOST,
  CURL_CONFIG_FILE,
  curlResolveEntries,
  hostResolverRules,
  probeEndpoints,
  resolveBridgeAddress,
  writeCurlResolveConfig,
  agentBrowserWrapperScript,
  writeAgentBrowserWrapper,
} from "./endpoint_access.js";


const execFileAsync = promisify(execFile);
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

// One rule for the whole family, mapped to the bridge — no port, because
// Chromium maps names and each URL keeps its own. Endpoint addresses are NOT
// used: they name the data-plane gateway, which does not serve the IdP.
test("hostResolverRules maps the whole .localhost family to the bridge", () => {
  const args = hostResolverRules(
    [
      { host: "a.localhost", port: 19080, address: GATEWAY },
      { host: "b.localhost", port: 443, address: "10.0.0.9" },
    ],
    "172.18.0.1",
  );
  assert.deepEqual(args, ["--host-resolver-rules=MAP *.localhost 172.18.0.1"]);
});

// THE regression guard. Chromium separates this flag's own MAP rules with
// commas, and agent-browser splits AGENT_BROWSER_ARGS on commas as readily as on
// newlines — so a comma anywhere in the value is a split, including one inside a
// single flag. Measured on the runner image: with two MAPs in one value, the
// first host resolves and the second returns ERR_NAME_NOT_RESOLVED. That cost a
// whole validation run, reported as 15 of 15 scenarios blocked on an
// unreachable IdP, which reads as a verdict about the app.
test("the emitted rule carries no comma, so nothing can be split off it", () => {
  const args = hostResolverRules(
    [
      { host: "a.localhost", port: 19080, address: GATEWAY },
      { host: "b.localhost", port: 443, address: "10.0.0.9" },
      { host: "c.localhost", port: 8443, address: "10.0.0.7" },
    ],
    "172.18.0.1",
  );
  assert.equal(args.length, 1, "more than one arg is more than one thing to lose");
  assert.ok(!(args[0] as string).includes(","), `a comma is a silent split: ${args[0]}`);
});

test("hostResolverRules returns nothing when there is nothing to map", () => {
  // No endpoint is no local plane to be on, whether or not a bridge resolved.
  assert.deepEqual(hostResolverRules([]), []);
  assert.deepEqual(hostResolverRules([], "172.18.0.1"), []);
  // A local plane whose bridge did not resolve: no single address serves both
  // planes, so emit none rather than one that maps the IdP to the wrong gateway.
  assert.deepEqual(hostResolverRules([{ host: "a.localhost", port: 19080, address: GATEWAY }]), []);
});

// DNS is measurably wrong here (the rewrite sends *.openchoreo.localhost to the
// DATA plane, which does not serve it), so the bridge is looked up by name. A
// failure is answered, not thrown: a cloud plane has no bridge and no
// `.localhost` endpoints either, so the two absences agree.
test("resolveBridgeAddress resolves the bridge, and answers undefined on failure", async () => {
  const ok = await resolveBridgeAddress(async (host) => {
    assert.equal(host, AUTH_BRIDGE_HOST);
    return { address: "172.18.0.1" };
  });
  assert.equal(ok, "172.18.0.1");

  const missing = await resolveBridgeAddress(async () => {
    throw new Error("ENOTFOUND");
  });
  assert.equal(missing, undefined);
});

// The wrapper is BASH, and every bug it can have is a quoting bug — so these
// run it rather than reading it. The stand-in "real binary" prints the
// AGENT_BROWSER_ARGS it was handed, which is the whole contract.
async function runWrapper(callerArgs: string | undefined, rule: string): Promise<string> {
  const dir = await tmpDir();
  const stub = path.join(dir, "stub");
  await fs.promises.writeFile(stub, '#!/usr/bin/env bash\nprintf %s "$AGENT_BROWSER_ARGS"\n', { mode: 0o755 });
  const wrapper = path.join(dir, "agent-browser");
  await fs.promises.writeFile(wrapper, agentBrowserWrapperScript(stub, rule), { mode: 0o755 });
  const env = { ...process.env };
  if (callerArgs === undefined) delete env.AGENT_BROWSER_ARGS;
  else env.AGENT_BROWSER_ARGS = callerArgs;
  const { stdout } = await execFileAsync(wrapper, [], { env });
  return stdout;
}

// The rule the platform actually emits: ONE MAP, no comma. A wrapper test
// cannot prove the browser's own parse — this shell only hands the variable on —
// which is precisely why the no-comma invariant is pinned upstream, on
// hostResolverRules, where it is the thing that can be got wrong.
const RULE = `--host-resolver-rules=MAP *.localhost 172.18.0.1`;

test("the wrapper passes the platform rule through as one argument", async () => {
  const out = await runWrapper("--no-sandbox", RULE);
  const args = out.split("\n").filter(Boolean);
  assert.deepEqual(args, ["--no-sandbox", RULE]);
});

// The reason this is a wrapper at all: the agent-browser skill tells an agent to
// export AGENT_BROWSER_ARGS when the browser will not launch. One that does must
// not be able to un-map the deployed hosts — measured as 7 of 8 scenarios
// `blocked` on an unreachable auth issuer.
test("a caller's own resolver rule is discarded, not merged", async () => {
  const out = await runWrapper(
    `--no-sandbox --host-resolver-rules=MAP only-this.localhost 1.2.3.4`,
    RULE,
  );
  assert.ok(!out.includes("only-this.localhost"), `caller rule survived:\n${out}`);
  assert.ok(out.includes("MAP *.localhost"), `platform rule missing:\n${out}`);
});

// A caller that comma-joined its own rule leaves bare `MAP …` fragments once the
// value is split on commas. They are arguments to nothing and Chromium would
// refuse them.
test("bare MAP fragments from a caller's comma join are dropped", async () => {
  const out = await runWrapper(
    `--no-sandbox,--host-resolver-rules=MAP x.localhost 9.9.9.9,MAP y.localhost 9.9.9.9`,
    RULE,
  );
  const args = out.split("\n").filter(Boolean);
  assert.deepEqual(args, ["--no-sandbox", RULE]);
});

// --no-sandbox is not decoration: Chromium cannot start as the pod's non-root
// user without it (ADR-0007), and the image sets it.
test("everything the caller set that is not a resolver rule passes through", async () => {
  const out = await runWrapper("--no-sandbox\n--disable-gpu", RULE);
  const args = out.split("\n").filter(Boolean);
  assert.deepEqual(args, ["--no-sandbox", "--disable-gpu", RULE]);
});

test("an unset AGENT_BROWSER_ARGS yields the rule alone, with no empty argument", async () => {
  const out = await runWrapper(undefined, RULE);
  assert.deepEqual(out.split("\n").filter(Boolean), [RULE]);
  assert.ok(!out.startsWith("\n"), "leading newline parses as an empty first argument");
});

// A cloud plane resolves its hostnames normally: nothing to map, so no wrapper,
// so the image's own agent-browser stays on PATH unshadowed.
test("no endpoints to map writes no wrapper", async () => {
  const dir = await tmpDir();
  assert.equal(await writeAgentBrowserWrapper(dir, [], async () => ({ address: "172.18.0.1" })), undefined);
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
