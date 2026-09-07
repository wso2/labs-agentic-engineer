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

// Every run writes its OWN results file, and the report is the merge of them.
//
// One run used to have to cover the whole suite, because there was one file and
// the last writer won. That made a run which could not finish in a single
// command a run that could produce no report at all — 24 specs outgrew the Bash
// timeout, the call was severed, and the criteria that had passed were lost with
// it (issue #701). Nothing here caps how long a suite may take any more: it is
// covered by however many calls it takes.
//
// The stamp is the merge order. ISO-8601 sorts lexicographically in time order,
// so `readdir().sort()` is chronological and the newest result for a criterion
// is simply the last one seen — which is what makes a heal's re-run supersede
// the failure that prompted it.
//
// Computed once at module scope, deliberately. Playwright loads this config
// twice — the CLI process, which constructs the reporters, and each worker,
// which does not — so a per-evaluation stamp would differ between them
// harmlessly, but a per-CALL one would not be stable within the run that uses it.
const STAMP = new Date().toISOString().replace(/[:.]/g, "-");

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
  // Written per run, never shared. See STAMP above: the report generator merges
  // this directory, so a narrowed run — one spec being verified while authored,
  // a spec re-run while healed, a shard — contributes its results instead of
  // overwriting everyone else's or being thrown away.
  reporter: [["list"], ["json", { outputFile: `test-results/runs/${STAMP}.json` }]],
  outputDir: "test-results/artifacts",
  use: {
    baseURL,
    trace: "retain-on-failure",
    launchOptions: {
      args: hostResolverArgs(baseURL),
    },
  },
});
