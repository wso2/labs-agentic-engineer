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
 * The composition root of the AE Studio pod's design agent (07 §9): the pod
 * env (`pod/config.ts`), the model connection (`shared/connection-env.ts`),
 * the tools socket, the usage outbox, the conversation books, the TurnDesk
 * and the turn start path with its Room join (`collab/local-room.ts`), then
 * the pod listeners with the `/v1` edge and the Turn socket. SIGTERM runs
 * `pod/shutdown.ts`.
 * Without `AE_ORG_ID` there is nothing to run: a local run goes through the
 * playground, which drives the same edge in process with its dev verifier.
 * The model is built per turn from the connection.
 *
 *   pnpm --filter @aep/ae-design-agent dev     # watch + reload (pod env)
 *   pnpm --filter @aep/ae-design-agent start   # run once
 */

import { registerTelemetry } from "ai";
import { createModel } from "./shared/model.js";
import { captureTelemetry } from "./shared/telemetry.js";
import { pruneDevtoolsFile } from "./shared/devtools-retention.js";
import { config } from "./shared/config.js";
import { connectionFromEnv } from "./shared/connection-env.js";
import { loadPodConfig, type PodConfig } from "./pod/config.js";
import { startPodListeners, type PodListeners } from "./pod/listeners.js";
import { InMemoryConversationStore } from "./store/memory-store.js";
import { ThreadBook } from "./conversations/thread-book.js";
import { MarketplaceBook } from "./conversations/marketplace-book.js";
import { createToolsSocket } from "./tools-socket/client.js";
import { localRoomJoiner } from "./collab/local-room.js";
import { shutdown } from "./pod/shutdown.js";
import { UsageOutbox } from "./usage/outbox.js";
import { TurnDesk } from "./turns/turn-desk.js";
import { finishedTurnSink, TurnStarter } from "./turns/start-turn.js";

async function main(): Promise<void> {
  // First: a process without the pod env, or with a partial one, must not boot.
  const cfg = loadPodConfig(process.env);
  if (!cfg) throw new Error("ae-design-agent: the pod env (AE_ORG_ID, ...) is not set; local runs go through the playground");
  // A malformed connection is a misrendered pod: fail the boot, value-free.
  // No key is not a fault: the pod serves and a turn answers no_default_key.
  const connection = connectionFromEnv(process.env);

  // DevTools retention, BEFORE anything can capture. The library lazily loads
  // the whole capture into a process-memory cache on its first write and
  // flushes it back whole on every step, so this is the only moment a prune
  // sticks — and doing it here is also what keeps it off the turn path: the
  // server is not listening yet, so no turn ever waits on it. Best-effort; a
  // null result means nothing to do or something unreadable.
  if (config.devtools && config.devtoolsRetentionDays > 0) {
    const summary = pruneDevtoolsFile(config.devtoolsRetentionDays);
    if (summary) process.stdout.write(summary);
  }

  // Trace capture registers ONCE per process — it is a property of the
  // deployment, not of a turn. Each turn stamps its own functionId
  // (shared/telemetry.ts).
  const telemetry = captureTelemetry();
  if (telemetry) registerTelemetry(telemetry);

  const pod = await startPod(cfg, connection);
  process.once("SIGTERM", () => {
    void shutdown(pod).then(
      () => process.exit(0),
      () => process.exit(1),
    );
  });
}

/** The running pod: what its shutdown stops. */
interface Pod {
  turns: TurnStarter;
  desk: TurnDesk;
  outbox: UsageOutbox;
  listeners: PodListeners;
}

/** Wire the pod's services and start its listeners. */
async function startPod(cfg: PodConfig, connection: ReturnType<typeof connectionFromEnv>): Promise<Pod> {
  const store = new InMemoryConversationStore();
  const threads = new ThreadBook({ store });
  const tools = createToolsSocket(cfg.mcpSocket);
  const outbox = new UsageOutbox(tools);
  // Delivering from boot: a record reaches ae-studio-tools as its turn ends.
  outbox.run();
  const desk = new TurnDesk({ onFinished: finishedTurnSink(threads, outbox) });
  const turns = new TurnStarter({
    desk,
    threads,
    store,
    tools,
    snapshotsDir: cfg.snapshotsDir,
    connection,
    // Built PER TURN from the org's connection.
    buildModel: (conn, ctx) => createModel(conn, ctx),
    room: localRoomJoiner({ url: cfg.collabLocalUrl, orgHandle: cfg.orgHandle, tools }),
    surface: "console",
    orgId: cfg.orgId,
  });
  const listeners = await startPodListeners(cfg, {
    edge: { turns, desk, threads, marketplace: new MarketplaceBook(store, { busy: (conversationId) => desk.active({ kind: "marketplace", conversationId }) !== null }), keepAliveMs: config.keepAliveMs },
  });
  return { turns, desk, outbox, listeners };
}

main().catch((err: unknown) => {
  process.stderr.write(
    `@aep/ae-design-agent failed to start: ${err instanceof Error ? (err.stack ?? err.message) : String(err)}\n`,
  );
  process.exitCode = 1;
});
