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

// Makes the validation run's deployed endpoints usable, and proves they answer,
// before the agent starts.
//
// Two different problems, both of which used to be the agent's:
//
//  1. `*.localhost` is unreachable from the two clients that matter. curl (>=7.77)
//     and Chromium both implement RFC 6761: they resolve `localhost` and every
//     `*.localhost` name to loopback THEMSELVES, consulting neither DNS nor
//     `/etc/hosts`. On a local plane every deployed endpoint is
//     `*.openchoreoapis.localhost`, so inside the runner pod both dial 127.0.0.1
//     — where nothing listens — however healthy the deployment is. The cluster's
//     CoreDNS rewrite is correct and simply never gets asked. Node is NOT
//     affected (`dns.lookup` returns the real address), which is why the probe
//     below needs no override of its own.
//
//     The fix is per-client, because no one mechanism reaches all three: a
//     `resolve` entry per endpoint in curl's own config file, and the equivalent
//     `--host-resolver-rules` in a config playwright-cli is pointed at, so a
//     plain `curl <url>` and a bare `playwright-cli open <url>` both work with
//     the real hostname, through the real gateway, carrying the real Host header
//     the HTTPRoute matches on. (The third client, the browser `playwright test`
//     launches, is configured by `playwright.config.template.ts` in the project's
//     own repo — it is the one the specs run in, and it is not ours to set from
//     here.) Rewriting the URL
//     was the alternative and is worse: an IP in the URL sends `Host: <ip>` and
//     matches no route, and a Service-DNS URL bypasses the gateway altogether —
//     dropping the api-configuration trait's auth, CORS and path rewrites, so an
//     auth-gated criterion could pass through a side door.
//
//  2. Whether the deployment answers at all is a PLATFORM fact. It used to be a
//     `curl` in the aep-validation skill with prose telling the agent to stop if
//     it failed; the agent did not stop — it read RFC 6761's connection refused
//     as a broken deployment and went hunting through the pod's DNS
//     configuration. Same reasoning, and the same shape, as the context fetch in
//     validation_context.ts: an unanswerable platform question never reaches the
//     agent.
//
// On a cloud plane the endpoints are real DNS names, nothing special-cases them,
// and `curlResolveEntries` returns nothing — so no config is written and the
// whole local-plane concession costs the cloud path exactly nothing.

import dns from "node:dns";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

import type { ComponentEndpoint } from "./validation_context.js";

/** curl's per-user config file, read from `$CURL_HOME` then `$HOME`. */
export const CURL_CONFIG_FILE = ".curlrc";

/**
 * Where the config goes, and what `CURL_HOME` is set to in the agent's env.
 *
 * One function so the writer and the env record cannot drift — a config written
 * somewhere curl is not told to look is indistinguishable from no config at all.
 *
 * The home directory, deliberately: it is outside the git work tree, so unlike
 * `.aep/` there is no path by which this file could be committed, and it is not
 * world-writable, so the staged-rename below is guarding against far less than
 * it would under /tmp.
 */
export function curlConfigHome(): string {
  return os.homedir();
}

/** One `resolve = host:port:address` override. */
export interface CurlResolveEntry {
  host: string;
  port: number;
  address: string;
}

/** An endpoint that did not answer, and what stopped it. */
export interface UnreachableEndpoint {
  component: string;
  url: string;
  reason: string;
}

/** The lookup shape used here — `dns.promises.lookup`'s options overload. */
type LookupFn = (host: string, options: { family: 4 }) => Promise<{ address: string }>;

/**
 * How long a single probe may take. Generous on purpose: this runs once per
 * endpoint on a cold gateway, and a false "unreachable" costs a whole validation
 * cycle, while a slow one costs seconds.
 */
const PROBE_TIMEOUT_MS = 10_000;

/** The port a URL implies when it does not name one. */
function defaultPort(protocol: string): number {
  return protocol === "https:" ? 443 : 80;
}

/**
 * Which endpoints need a curl override, and what address to pin them to.
 *
 * Only `.localhost` hosts qualify — that is the exact set RFC 6761 captures, and
 * pinning anything else would freeze a name curl already resolves correctly (and
 * would go stale the moment the address changed). DNS is the discovery channel:
 * the CoreDNS rewrite answers any `*.openchoreoapis.localhost` with the
 * data-plane gateway's address, so the answer is resolved per run rather than
 * configured — a baked-in IP passes once and then silently points at nothing.
 *
 * Forgiving by design. An unparseable URL or a name that will not resolve is
 * warned about and skipped, never thrown: `probeEndpoints` is what decides
 * whether the run may proceed, and it reports the endpoint that actually failed
 * rather than the lookup that preceded it.
 */
export async function curlResolveEntries(
  endpoints: readonly ComponentEndpoint[],
  lookup: LookupFn = dns.promises.lookup as LookupFn,
  log: (line: string) => void = () => {},
): Promise<CurlResolveEntry[]> {
  const entries: CurlResolveEntry[] = [];
  // Components can share a gateway host:port, and a duplicate `resolve` line is
  // noise in a file a human may well have to read.
  const seen = new Set<string>();

  for (const ep of endpoints) {
    let parsed: URL;
    try {
      parsed = new URL(ep.url);
    } catch {
      log(`[endpoints] ⚠️  ${ep.component}: not a URL, no curl override written: ${ep.url}`);
      continue;
    }
    if (!parsed.hostname.endsWith(".localhost")) {
      continue;
    }
    const port = parsed.port === "" ? defaultPort(parsed.protocol) : Number(parsed.port);
    const key = `${parsed.hostname}:${port}`;
    if (seen.has(key)) {
      continue;
    }
    try {
      const { address } = await lookup(parsed.hostname, { family: 4 });
      seen.add(key);
      entries.push({ host: parsed.hostname, port, address });
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      log(`[endpoints] ⚠️  ${ep.component}: cannot resolve ${parsed.hostname}: ${msg}`);
    }
  }
  return entries;
}

/**
 * Write the overrides to `<dir>/.curlrc`, or nothing when there are none.
 *
 * Staged in a private directory and renamed into place, for the same reasons
 * validation_context.ts stages its write: `mode` is honoured only on create, so
 * writing over an existing path would keep whatever permissions it already had,
 * and rename is atomic and does not follow a symlink at the destination.
 *
 * Returns the path written, or undefined when there was nothing to write — an
 * empty file would be indistinguishable from a run whose endpoints needed no
 * override, and callers log the difference.
 */
export async function writeCurlResolveConfig(
  dir: string,
  entries: readonly CurlResolveEntry[],
): Promise<string | undefined> {
  if (entries.length === 0) {
    return undefined;
  }
  const file = path.join(dir, CURL_CONFIG_FILE);
  const body =
    `# Written by the AEP validation runner. Deployed endpoints are\n` +
    `# *.openchoreoapis.localhost, which curl resolves to loopback itself\n` +
    `# (RFC 6761) — these pin them to the gateway DNS actually answers with.\n` +
    entries.map((e) => `resolve = ${e.host}:${e.port}:${e.address}`).join("\n") +
    `\n`;

  await fs.promises.mkdir(dir, { recursive: true });
  const staging = await fs.promises.mkdtemp(path.join(dir, ".aep-curlrc-"));
  try {
    const staged = path.join(staging, CURL_CONFIG_FILE);
    await fs.promises.writeFile(staged, body, { mode: 0o600 });
    await fs.promises.rename(staged, file);
  } finally {
    await fs.promises.rm(staging, { recursive: true, force: true });
  }
  return file;
}

/**
 * playwright-cli's config file, pointed at by `$PLAYWRIGHT_MCP_CONFIG`.
 *
 * Deliberately NOT `.playwright/cli.config.json`, the name the CLI looks for by
 * default: that default is resolved against the CWD, and an exploring agent
 * moves between the repo root and `tests/e2e`, so a CWD-relative config is
 * present for some of its commands and absent for the rest. An absolute path in
 * the env holds for every invocation from every directory.
 */
export const PLAYWRIGHT_CLI_CONFIG_FILE = ".aep-playwright-cli.json";

/**
 * Where that config goes. Same home-directory reasoning as `curlConfigHome`,
 * and the same one-function rule so the writer and the env record cannot drift.
 */
export function playwrightCliConfigHome(): string {
  return os.homedir();
}

/** The absolute path the env variable carries — writer and reader share it. */
export function playwrightCliConfigPath(): string {
  return path.join(playwrightCliConfigHome(), PLAYWRIGHT_CLI_CONFIG_FILE);
}

/**
 * The IdP's name family, and where it is actually reachable from inside a pod.
 *
 * A wildcard, unlike every endpoint rule below it, because the runner never
 * learns the IdP's hostname: the validation context carries the app's endpoints
 * and nothing else, so a pattern is the only handle there is. The same pattern
 * `playwright.config.template.ts` uses, for the same reason.
 *
 * `host.k3d.internal` rather than the address DNS returns, because DNS is
 * measurably wrong here: the CoreDNS rewrite maps `(openchoreo|openchoreoapis)
 * .localhost` alike onto the DATA-plane gateway, while `*.openchoreo.localhost`
 * is served by the CONTROL-plane one. Following DNS gets a transport failure
 * (curl exit 7); the k3d bridge, which publishes the control-plane gateway,
 * answers. Local-plane only — see the caller's gate.
 */
export const AUTH_HOST_PATTERN = "*.openchoreo.localhost";
export const AUTH_BRIDGE_HOST = "host.k3d.internal";

/**
 * The Chromium flag that makes the deployed hostnames resolvable in a browser.
 *
 * `--host-resolver-rules` is the one override Chromium honours for RFC 6761:
 * it maps `localhost` and every `*.localhost` name to loopback itself, ahead of
 * DNS and `/etc/hosts`, so nothing else reaches it. One `MAP <host> <address>`
 * per endpoint rather than a pattern — these are the hosts this run actually
 * resolved, and a wildcard would also capture names nobody probed. The IdP is
 * the one exception, appended last and explained above.
 *
 * No port in any rule: Chromium maps names, and each URL keeps its own port.
 */
export function hostResolverRules(
  entries: readonly CurlResolveEntry[],
  authAddress?: string,
): string[] {
  const seen = new Set<string>();
  const rules: string[] = [];
  for (const e of entries) {
    if (seen.has(e.host)) continue;
    seen.add(e.host);
    rules.push(`MAP ${e.host} ${e.address}`);
  }
  if (rules.length === 0) {
    return [];
  }
  // Specific first, pattern last. The suffixes cannot overlap
  // (`…openchoreoapis.localhost` never matches `*.openchoreo.localhost`), so
  // this is for a reader rather than for Chromium.
  if (authAddress !== undefined) {
    rules.push(`MAP ${AUTH_HOST_PATTERN} ${authAddress}`);
  }
  return [`--host-resolver-rules=${rules.join(",")}`];
}

/**
 * Where the IdP is reachable from this pod, or undefined if it is not.
 *
 * Forgiving on purpose: a cloud plane has no k3d bridge to resolve, and an
 * exploration hop the agent may never take is not worth failing a run over. The
 * app endpoints — which the preflight DOES prove — are unaffected either way.
 */
export async function resolveAuthGatewayAddress(
  lookup: LookupFn = dns.promises.lookup as LookupFn,
): Promise<string | undefined> {
  try {
    const { address } = await lookup(AUTH_BRIDGE_HOST, { family: 4 });
    return address;
  } catch {
    return undefined;
  }
}

/**
 * Write the browser's half of the same override, for the EXPLORATION browser.
 *
 * `.curlrc` is a curl mechanism and never reaches a browser, and
 * `playwright.config.template.ts` covers only the browser `playwright test`
 * launches — so playwright-cli, which reads neither, was the one client left
 * dialling loopback. The agent then rediscovered RFC 6761 from scratch each
 * authoring run: 180s of DNS spelunking, a throwaway probe spec and two edits
 * to a config that was never in the path, on the run that measured it (#570).
 *
 * `launchOptions.args` ONLY. Naming `browser.browserName` here would leave
 * `channel` undefined and re-enable the Chromium sandbox, which cannot start as
 * the pod's non-root user — the failure ADR-0007 exists to keep out of this
 * file. The browser is chosen by `PLAYWRIGHT_MCP_BROWSER` in the image; this
 * only says how to resolve a name.
 *
 * The IdP is mapped alongside them, so an exploration that follows a login
 * redirect does not meet an unresolvable host — the gap that used to leave a
 * dead hop for the agent to misread as a broken deployment, which is the exact
 * fault ADR-0006 exists to remove. Gated on there being an endpoint to map at
 * all: `curlResolveEntries` yields only `.localhost` hosts, so a non-empty list
 * IS the local-plane signal, and a cloud run writes no file and gets no rule.
 *
 * Returns the path written, or undefined when there is nothing to map — and in
 * that case REMOVES any file a previous run left behind. `$PLAYWRIGHT_MCP_CONFIG`
 * is fatal when it points at a missing file (the daemon exits on ENOENT), so the
 * env is set from this file's existence; a stale file would silently pin a
 * cluster that no longer exists.
 */
export async function writePlaywrightCliConfig(
  dir: string,
  entries: readonly CurlResolveEntry[],
  lookup: LookupFn = dns.promises.lookup as LookupFn,
): Promise<string | undefined> {
  const file = path.join(dir, PLAYWRIGHT_CLI_CONFIG_FILE);
  if (entries.length === 0) {
    await fs.promises.rm(file, { force: true });
    return undefined;
  }
  const args = hostResolverRules(entries, await resolveAuthGatewayAddress(lookup));
  if (args.length === 0) {
    await fs.promises.rm(file, { force: true });
    return undefined;
  }
  const body = `${JSON.stringify({ browser: { launchOptions: { args } } }, null, 2)}\n`;

  await fs.promises.mkdir(dir, { recursive: true });
  const staging = await fs.promises.mkdtemp(path.join(dir, ".aep-pwcli-"));
  try {
    const staged = path.join(staging, PLAYWRIGHT_CLI_CONFIG_FILE);
    await fs.promises.writeFile(staged, body, { mode: 0o600 });
    await fs.promises.rename(staged, file);
  } finally {
    await fs.promises.rm(staging, { recursive: true, force: true });
  }
  return file;
}

/**
 * Probe every endpoint and report the ones that did not answer.
 *
 * ANY HTTP response counts as reachable — status is deliberately not evidence.
 * An endpoint behind the api-configuration trait legitimately answers 401, an
 * API root legitimately answers 404, and a gateway holding no matching
 * HTTPRoute also answers 404, indistinguishably from the app's own. Reading a
 * status as a verdict would therefore manufacture failures against healthy
 * deployments, which is the exact fault this preflight exists to remove. Only a
 * transport failure — refused, unroutable, timed out, unresolvable — means the
 * platform cannot show the agent the system it is meant to validate.
 *
 * Redirects are NOT followed. A login redirect points at the IdP on the control
 * plane, which is a different hop with its own resolution story; chasing it here
 * would turn an answered endpoint into a false negative. A 302 is an answer.
 */
export async function probeEndpoints(
  endpoints: readonly ComponentEndpoint[],
  opts: { timeoutMs?: number; fetchImpl?: typeof fetch } = {},
): Promise<UnreachableEndpoint[]> {
  const doFetch = opts.fetchImpl ?? fetch;
  const timeoutMs = opts.timeoutMs ?? PROBE_TIMEOUT_MS;
  const unreachable: UnreachableEndpoint[] = [];

  for (const ep of endpoints) {
    try {
      const res = await doFetch(ep.url, {
        redirect: "manual",
        signal: AbortSignal.timeout(timeoutMs),
      });
      // Nothing here reads the body; leaving it unconsumed holds the socket.
      await res.body?.cancel().catch(() => {});
    } catch (err) {
      const cause = (err as { cause?: { code?: string } }).cause;
      const reason = cause?.code ?? (err instanceof Error ? err.message : String(err));
      unreachable.push({ component: ep.component, url: ep.url, reason });
    }
  }
  return unreachable;
}
