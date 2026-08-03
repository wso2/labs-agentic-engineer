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
 * The composition root: pick a store (Postgres when DATABASE_URL is set, else
 * in-memory), wire the always-on M2M gate and the per-request model factory,
 * mount the SSE app, listen. The model is built per turn from the request's
 * `X-Anthropic-Key`, so there is NO boot-time key or model here.
 *
 *   pnpm --filter @aep/agents dev     # watch + reload
 *   pnpm --filter @aep/agents start   # run once
 */

import pg from "pg";
import { createApp } from "./server.js";
import { createModel, resolveModelId } from "./shared/model.js";
import { intEnv } from "./shared/env.js";
import { config } from "./shared/config.js";
import type { AgentsAuthConfig } from "./shared/auth.js";
import type { ConversationStore } from "./store/conversation-store.js";
import { InMemoryConversationStore } from "./store/memory-store.js";
import { PostgresConversationStore } from "./store/postgres-store.js";

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

async function buildStore(): Promise<ConversationStore> {
  if (!config.database.url) {
    process.stdout.write("@aep/agents: no Postgres URL — using the in-memory conversation store\n");
    return new InMemoryConversationStore();
  }
  const pool = new pg.Pool({ connectionString: config.database.url });
  const store = new PostgresConversationStore(pool);
  await store.init(); // idempotent CREATE TABLE IF NOT EXISTS

  // Periodic TTL sweep — threads embed inlined snapshots, so rows are heavy;
  // reclaim ones untouched past the retention window.
  const timer = setInterval(() => {
    void store.sweepExpired(config.database.conversationsTtlMs).catch((err: unknown) => {
      process.stderr.write(
        `conversation TTL sweep failed: ${err instanceof Error ? err.message : String(err)}\n`,
      );
    });
  }, config.database.conversationsSweepMs);
  timer.unref();

  process.stdout.write("@aep/agents: using the Postgres conversation store\n");
  return store;
}

async function main(): Promise<void> {
  const store = await buildStore();
  const app = createApp({
    store,
    buildModel: (apiKey) => createModel({ apiKey }), // built PER TURN from X-Anthropic-Key
    modelId: resolveModelId(), // the id createModel resolves above (usage attribution, #249)
    auth: buildAuthConfig(), // throws here if neither JWKS nor secret is set (gate is always on)
  });

  app.listen(port, () => {
    process.stdout.write(`@aep/agents SSE server listening on :${port}\n`);
  });
}

main().catch((err: unknown) => {
  process.stderr.write(
    `@aep/agents failed to start: ${err instanceof Error ? (err.stack ?? err.message) : String(err)}\n`,
  );
  process.exitCode = 1;
});
