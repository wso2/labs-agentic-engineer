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

// Template for tests/e2e/playwright.config.ts in the project repo.
// Copied by the aep-validation skill; adjust nothing except where a
// comment says so. Retries stay 0 — flake repair is the healer's job,
// and retries would mask the brittleness signal it needs.

import { execSync } from "node:child_process";
import { defineConfig } from "@playwright/test";
import { primaryTarget } from "./lib/targets";

// Chromium implements RFC 6761: it maps `localhost` and every `*.localhost`
// name to loopback ITSELF, without consulting DNS or /etc/hosts. On a local
// plane the deployed endpoints are `*.openchoreoapis.localhost`, so the
// cluster's CoreDNS rewrite never gets asked, and inside the runner pod
// loopback is the pod — every request dies with ERR_CONNECTION_REFUSED.
// `--host-resolver-rules` is the one override Chromium honours.
//
// Resolve the addresses at load time rather than hard-coding them: they are
// per-cluster and change on every cluster rebuild, so a baked-in IP passes once
// and then silently points at nothing.
const HOSTNAME_PATTERN = /^[A-Za-z0-9.-]+$/;

function resolveIPv4(host: string): string | undefined {
  // DNS is the discovery channel: the CoreDNS rewrite answers any
  // `*.openchoreoapis.localhost` with the data-plane gateway's ClusterIP.
  if (!HOSTNAME_PATTERN.test(host)) {
    return undefined;
  }
  try {
    const first = execSync(`getent ahostsv4 ${host}`, {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).split(/\s+/)[0];
    return first || undefined;
  } catch {
    return undefined;
  }
}

// Only a `.localhost` target needs this. A real DNS name resolves normally, so
// on a cloud plane the browser is left completely alone.
function hostResolverArgs(baseURL: string): string[] {
  const targetHost = new URL(baseURL).hostname;
  if (!targetHost.endsWith(".localhost")) {
    return [];
  }

  // The app endpoints (webapp + API) both sit behind the data-plane gateway.
  const ingressIP = process.env.AEP_E2E_INGRESS_IP ?? resolveIPv4(targetHost);

  // The IdP does NOT: `thunder.openchoreo.localhost` is served by the
  // CONTROL-plane gateway, while the CoreDNS rewrite points every
  // `*.openchoreo.localhost` name at the data-plane one — so DNS is the wrong
  // answer here and the login redirect has to be mapped separately. The k3d
  // host bridge publishes it on 8080.
  const authIP = process.env.AEP_E2E_AUTH_IP ?? resolveIPv4("host.k3d.internal");

  const rules = [
    ingressIP ? `MAP *.openchoreoapis.localhost ${ingressIP}` : "",
    authIP ? `MAP *.openchoreo.localhost ${authIP}` : "",
  ].filter(Boolean);

  return rules.length ? [`--host-resolver-rules=${rules.join(",")}`] : [];
}

const baseURL = primaryTarget();

// Playwright's own ways of narrowing a run, beyond naming spec files. `--shard`
// is the one that bites: it looks like the way to fit a big suite inside the
// command window, and without it here a `--shard=1/3` would have been treated as
// the authoritative pass and written a THIRD of the suite's results as if they
// were all of it — every unrun criterion reported as never checked.
const NARROWING_FLAGS = /^(?:--shard|--grep|--grep-invert|-g|--last-failed|--only-changed)(?:=|$)/;

/**
 * Whether this run covers less than the whole suite.
 *
 * A partial run is a PROBE — one spec being checked on its own, a shard, a
 * grep — and no probe may write `test-results/results.json`, which is the
 * report generator's only input. Only a complete, unfiltered run may.
 *
 * Read off the command rather than switched by a flag or an env var: naming a
 * spec is not optional, it is how you run one spec, so there is nothing for a
 * caller to remember and nothing to forget.
 */
function isPartialRun(): boolean {
  return process.argv
    .slice(2)
    .some((a) => a.includes(".spec.") || NARROWING_FLAGS.test(a));
}

export default defineConfig({
  testDir: "./specs",
  // Serial: specs share one deployed environment; parallel runs would
  // race on server-side state and make failures non-deterministic.
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  // A PARTIAL run is a probe — one spec being checked on its own (how it is
  // verified while authored and re-checked while healed), a shard, or a grep. A
  // run that narrows nothing is the authoritative pass over the whole suite, and
  // its `results.json` is the only thing the report generator reads. See
  // isPartialRun above.
  //
  // Only the authoritative pass may write that file. Probes used to overwrite it
  // with a single spec's results, which is invisible until the report claims one
  // criterion was checked and the rest never ran.
  //
  // Inferred from the command rather than switched by a flag or an env var on
  // purpose: naming a spec is not optional — it is how you run one spec — so
  // there is nothing here for a caller to remember, and nothing to forget. The
  // cost is that a SHARDED authoritative run reads as a probe and writes no
  // report input at all; the generator then exits 2 naming the missing path,
  // which is why the workflow runs the authoritative pass as one call.
  //
  // Playwright loads this config twice — once in the CLI process, which is where
  // argv carries the filter and where reporters are constructed, and once per
  // worker, where argv is empty. Only the first decides. Do not "simplify" this
  // to a module-level constant computed somewhere the CLI process cannot see.
  reporter: isPartialRun()
    ? [["line"]]
    : [["list"], ["json", { outputFile: "test-results/results.json" }]],
  outputDir: "test-results/artifacts",
  use: {
    baseURL,
    trace: "retain-on-failure",
    launchOptions: {
      args: hostResolverArgs(baseURL),
    },
  },
});
