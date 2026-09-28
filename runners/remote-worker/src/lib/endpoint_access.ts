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
//     The fix is per-client, because no one mechanism reaches both: a `resolve`
//     entry per endpoint in curl's own config file, and the equivalent
//     `--host-resolver-rules` in the browser's launch args, so a plain
//     `curl <url>` and a bare `agent-browser open <url>` both work with
//     the real hostname, through the real gateway, carrying the real Host header
//     the HTTPRoute matches on. Rewriting the URL
//     was the alternative and is worse: an IP in the URL sends `Host: <ip>` and
//     matches no route, and a Service-DNS URL bypasses the gateway altogether —
//     dropping the api-configuration trait's auth, CORS and path rewrites, so an
//     auth-gated criterion could pass through a side door.
//
//  2. Whether the deployment answers at all is a PLATFORM fact. It used to be a
//     `curl` in the validation skill with prose telling the agent to stop if
//     it failed; the agent did not stop — it read RFC 6761's connection refused
//     as a broken deployment and went hunting through the pod's DNS
//     configuration. Same reasoning, and the same shape, as the context fetch in
//     validation_context.ts: an unanswerable platform question never reaches the
//     agent.
//
// On a cloud plane the endpoints are real DNS names, nothing special-cases them,
// and `curlResolveEntries` returns nothing — so no config is written and the
// whole local-plane concession costs the cloud path exactly nothing.

import { execFile } from "node:child_process";
import dns from "node:dns";
import fs from "node:fs";
import { promisify } from "node:util";
import os from "node:os";
import path from "node:path";

import type { ComponentEndpoint } from "./validation_context.js";

const execFileAsync = promisify(execFile);

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
 * The deployed name family, and the one address that serves all of it.
 *
 * A wildcard because the runner never learns every name it must reach: the
 * validation context carries the app's endpoints and nothing else, so the IdP —
 * which every signed-in scenario needs — has no entry to map. A pattern is the
 * only handle there is. `*.localhost` matches across labels, so it covers the
 * data plane's `…openchoreoapis.localhost` and the control plane's
 * `…openchoreo.localhost` alike.
 *
 * `host.k3d.internal` rather than the address DNS returns, because DNS is
 * measurably wrong here: the CoreDNS rewrite maps `(openchoreo|openchoreoapis)
 * .localhost` alike onto the DATA-plane gateway, while `*.openchoreo.localhost`
 * is served by the CONTROL-plane one. Following DNS gets a transport failure
 * (curl exit 7); the k3d bridge answers for both, because k3d publishes every
 * gateway port on the host — each name reaches its own service there on its own
 * port. Local-plane only — see the caller's gate.
 */
export const LOCAL_HOST_PATTERN = "*.localhost";
export const AUTH_BRIDGE_HOST = "host.k3d.internal";

/**
 * The Chromium flag that makes the deployed hostnames resolvable in a browser.
 *
 * `--host-resolver-rules` is the one override Chromium honours for RFC 6761:
 * it maps `localhost` and every `*.localhost` name to loopback itself, ahead of
 * DNS and `/etc/hosts`, so nothing else reaches it.
 *
 * ONE rule, and that is a constraint rather than a simplification. Chromium
 * separates this flag's own `MAP` rules with COMMAS, and `AGENT_BROWSER_ARGS` —
 * the only channel agent-browser offers — is documented comma-OR-newline
 * separated. A value carrying two MAPs is therefore split mid-flag by the
 * browser itself: the first mapping survives, and every later one is handed to
 * Chromium as an argument to nothing. Measured on the runner image: with
 * `MAP first.test 127.0.0.1,MAP second.test 127.0.0.1` in one value,
 * `first.test` is mapped (connection refused) and `second.test` comes back
 * ERR_NAME_NOT_RESOLVED. A second rule is not extra coverage — it is discarded,
 * silently. Emitting one is the only shape that survives the channel.
 *
 * That rule maps the whole `.localhost` family to the k3d bridge, which is the
 * one address serving all of it (see the pattern's own note). Per-endpoint
 * rules could not work here even without the splitting, because the IdP every
 * signed-in scenario needs is not among the endpoints the context names.
 *
 * `entries` no longer supplies addresses; it is the LOCAL-PLANE SIGNAL, since
 * `curlResolveEntries` yields `.localhost` hosts and nothing else. Without the
 * bridge there is no single address that serves both planes, so rather than
 * emit a rule that maps some names to the wrong gateway, emit none and let the
 * caller say so.
 *
 * No port in the rule: Chromium maps names, and each URL keeps its own port.
 */
export function hostResolverRules(
  entries: readonly CurlResolveEntry[],
  bridgeAddress?: string,
): string[] {
  if (entries.length === 0 || bridgeAddress === undefined) {
    return [];
  }
  return [`--host-resolver-rules=MAP ${LOCAL_HOST_PATTERN} ${bridgeAddress}`];
}

/**
 * Where the deployed system is reachable from this pod, or undefined if the
 * bridge does not resolve.
 *
 * Not fatal: a cloud plane has no k3d bridge, and there `curlResolveEntries`
 * yields nothing either, so the two absences agree and no wrapper is wanted.
 * On a local plane the absence is real — it costs the browser every mapping,
 * not just the IdP — so the caller reports it rather than letting the run
 * discover it as an unreachable app.
 */
export async function resolveBridgeAddress(
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
 * Write the `agent-browser` PATH wrapper that makes the deployed hostnames
 * resolvable in the browser, and return its path (undefined when nothing needs
 * mapping, or when there is no real binary to wrap).
 *
 * **Why a wrapper and not an environment variable.** The rules used to ride
 * `AGENT_BROWSER_ARGS`, and that was wrong: `skills/agent-browser` tells an
 * agent to `export AGENT_BROWSER_ARGS=--no-sandbox` whenever the browser will
 * not launch, so the one variable carrying the mappings is the one a skill
 * instructs agents to overwrite. Measured on a real run — the agent re-exported
 * it per command, dropped the IdP rule, and reported 7 of 8 scenarios `blocked`
 * on an unreachable auth issuer. A config file is no better: the CLI documents
 * `AGENT_BROWSER_*` as overriding config-file values. Only something the caller
 * cannot address survives, and `layout.aepDir` is already first on the agent's
 * PATH and already holds the `gh` wrapper.
 *
 * The wrapper OWNS `--host-resolver-rules`: it drops any the caller supplied
 * (and any bare `MAP …` fragment left by a caller who comma-joined its own)
 * before appending this run's. Everything else the caller set — `--no-sandbox`
 * above all, without which Chromium cannot start as the pod's non-root user
 * (ADR-0007) — is passed through untouched.
 *
 * It joins with NEWLINES. `AGENT_BROWSER_ARGS` is comma-or-newline separated and
 * `--host-resolver-rules` separates its own `MAP` rules with commas, so a comma
 * join splits the value mid-flag: the first mapping stays attached to the flag
 * and every later one becomes a bogus Chromium argument.
 *
 * Gated on there being an endpoint to map at all: `curlResolveEntries` yields
 * only `.localhost` hosts, so a non-empty list IS the local-plane signal and a
 * cloud run writes no wrapper.
 */
export async function writeAgentBrowserWrapper(
  dir: string,
  entries: readonly CurlResolveEntry[],
  lookup: LookupFn = dns.promises.lookup as LookupFn,
): Promise<string | undefined> {
  if (entries.length === 0) return undefined;
  const rules = hostResolverRules(entries, await resolveBridgeAddress(lookup));
  if (rules.length === 0) return undefined;
  const real = await resolveRealAgentBrowserPath();
  if (real === undefined) return undefined;

  const file = path.join(dir, "agent-browser");
  await fs.promises.mkdir(dir, { recursive: true });
  await fs.promises.writeFile(file, agentBrowserWrapperScript(real, rules[0] as string), { mode: 0o755 });
  return file;
}

/** The real binary, resolved off PATH before this wrapper shadows it. */
async function resolveRealAgentBrowserPath(): Promise<string | undefined> {
  try {
    const { stdout } = await execFileAsync("which", ["agent-browser"]);
    const p = stdout.trim().split("\n")[0]?.trim();
    return p !== undefined && p.startsWith("/") ? p : undefined;
  } catch {
    return undefined;
  }
}

/** The wrapper's text. Exported for the test that pins the separator. */
export function agentBrowserWrapperScript(realPath: string, rule: string): string {
  return `#!/usr/bin/env bash
# agent-browser wrapper — written by the runner's endpoint preflight.
#
# The deployed hostnames are *.localhost, which RFC 6761 pins to loopback ahead
# of DNS, so the browser needs --host-resolver-rules to reach them. This file
# owns that flag: a caller that sets AGENT_BROWSER_ARGS (the agent-browser skill
# tells agents to, when the browser will not launch) would otherwise silently
# un-map every deployed host.
set -e

RULE=${JSON.stringify(rule)}

# AGENT_BROWSER_ARGS is comma OR newline separated, and a caller may also have
# space-joined it (which never worked, but says what it meant). Split on all
# three, then KEEP ONLY FLAGS: every Chromium switch starts with "-", so any
# other fragment is the tail of a split value - a MAP keyword, a hostname, an
# address - and passing it on would hand Chromium an argument to nothing.
keep=""
while IFS= read -r a; do
  [ -z "$a" ] && continue
  case "$a" in
    --host-resolver-rules=*) continue ;;  # this file owns that flag
    -*) ;;                                # a real switch: keep it
    *) continue ;;                        # a fragment of a split value
  esac
  keep="\${keep}\${a}
"
done <<EOF
$(printf '%s' "\${AGENT_BROWSER_ARGS:-}" | tr ',\t ' '\n\n\n')
EOF

# Newline join, and RULE itself carries no comma — agent-browser splits this
# variable on commas as readily as on newlines, so a comma anywhere inside it is
# a split, including one INSIDE a single flag's value. That is why the rule is
# built as exactly one MAP (see hostResolverRules); a comma join here, or a
# second MAP there, silently drops everything after the first.
export AGENT_BROWSER_ARGS="\${keep}\${RULE}"
exec ${JSON.stringify(realPath)} "$@"
`;
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
