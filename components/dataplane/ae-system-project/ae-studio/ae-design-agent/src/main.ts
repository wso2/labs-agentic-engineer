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
 * The composition root. Two modes, chosen by env (`modes.ts`): the AE Studio
 * pod's listeners (`AE_*`: the `/v1` user gate and the health port), and the
 * legacy SSE server of the chart Deployment (`AGENT_JWT_*`). Each starts only
 * with its own config; boot fails with neither. The legacy server keeps
 * conversations in memory, wires the always-on M2M gate and the per-request model factory, mounts the SSE app
 * and listens. The model is built per turn from the request's key and
 * connection, so there is NO boot-time key or model here.
 *
 *   pnpm --filter @aep/ae-design-agent dev     # watch + reload
 *   pnpm --filter @aep/ae-design-agent start   # run once
 */

import type { Server } from "node:http";
import { registerTelemetry } from "ai";
import { createApp } from "./server.js";
import { createModel } from "./shared/model.js";
import { captureTelemetry } from "./shared/telemetry.js";
import { pruneDevtoolsFile } from "./shared/devtools-retention.js";
import { intEnv } from "./shared/env.js";
import { config } from "./shared/config.js";
import { selectModes } from "./modes.js";
import { startPodListeners } from "./pod/listeners.js";
import type { AgentsAuthConfig } from "./shared/auth.js";
import { InMemoryConversationStore } from "./store/memory-store.js";

const port = intEnv(process.env.PORT, 4000);

/** Only include the optional fields that are actually set (exactOptionalPropertyTypes). */
function buildAuthConfig(): AgentsAuthConfig {
  const { audience, issuer, jwksUrl, secret } = config.auth;
  return {
    audience,
    ...(issuer ? { issuer } : {}),
    ...(jwksUrl ? { jwksUrl } : {}),
    ...(secret ? { secret } : {}),
  };
}

async function main(): Promise<void> {
  // First: a process with neither config, or a partial pod env, must not boot.
  // `config` has loaded .env by now, so a legacy key set only there counts.
  const modes = selectModes(process.env);

  // DevTools retention, BEFORE anything can capture. The library lazily loads
  // the whole capture into a process-memory cache on its first write and
  // flushes it back whole on every step, so this is the only moment a prune
  // sticks — and doing it here is also what keeps it off the turn path: the
  // server is not listening yet, so no turn (`/start` included) ever waits on
  // it. Best-effort; a null result means nothing to do or something unreadable.
  if (config.devtools && config.devtoolsRetentionDays > 0) {
    const summary = pruneDevtoolsFile(config.devtoolsRetentionDays);
    if (summary) process.stdout.write(summary);
  }

  // Trace capture registers ONCE per process — it is a property of the
  // deployment, not of a turn. It used to be a middleware wrapped around the
  // per-turn model, which is what made every turn its own unrelated run: the
  // capture minted a run id when that short-lived object was constructed.
  // Registering here leaves model lifetimes alone; each turn stamps its own
  // functionId instead (shared/telemetry.ts).
  const telemetry = captureTelemetry();
  if (telemetry) registerTelemetry(telemetry);

  const closers: Array<() => Promise<void>> = [];
  const closeAll = () => Promise.allSettled(closers.map((close) => close()));
  try {
    if (modes.pod) closers.push((await startPodListeners(modes.pod)).close);
    if (modes.legacy) {
      const server = startLegacyServer();
      closers.push(() => closeLegacyServer(server));
    }
  } catch (err) {
    // A half-started process would keep a listener (and a ready probe) alive.
    await closeAll();
    throw err;
  }

  process.once("SIGTERM", () => {
    void closeAll().then((results) => {
      process.exit(results.some((r) => r.status === "rejected") ? 1 : 0);
    });
  });
}

/** Today's SSE server: in-memory store, M2M gate, per-turn model factory. */
function startLegacyServer(): Server {
  const app = createApp({
    store: new InMemoryConversationStore(),
    // Built PER TURN from the turn's connection (key, format, URL, model).
    buildModel: (conn, ctx) => createModel(conn, ctx),
    auth: buildAuthConfig(), // throws here if neither JWKS nor secret is set (gate is always on)
  });

  return app.listen(port, () => {
    process.stdout.write(`@aep/ae-design-agent SSE server listening on :${port}\n`);
  });
}

/** Stops accepting, then ends open streams: a SIGTERM used to end them too. */
function closeLegacyServer(server: Server): Promise<void> {
  const closed = new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
  server.closeAllConnections();
  return closed;
}

main().catch((err: unknown) => {
  process.stderr.write(
    `@aep/ae-design-agent failed to start: ${err instanceof Error ? (err.stack ?? err.message) : String(err)}\n`,
  );
  process.exitCode = 1;
});
