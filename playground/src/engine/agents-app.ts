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
 * Boot the REAL design agent in-process (docs/design/playground.md §2): the
 * same `/v1` edge (`createApp`) and Turn socket the AE Studio pod serves, the
 * same TurnStarter, TurnDesk and ThreadBook, with the playground's adapters
 * around them (07 §9):
 *
 * - `authenticate`: the dev verifier (`kit/auth.ts`), not the Platform IdP;
 * - `tools`: the in-process tools socket over the project folder
 *   (`engine/tools-fake.ts`), not ae-studio-tools;
 * - `store`: a `FileConversationStore` under the project's dot-dir, not memory;
 * - the Turn socket on a Unix socket in the session's private temp dir.
 *
 * There is no Room: a turn's edits stream back to the caller, which folds
 * them to disk (`engine/turn.ts`). The `/v1` edge listens on loopback only.
 */

import { join } from "node:path";
import type { AddressInfo } from "node:net";
import type { Server } from "node:http";
import type { LanguageModel } from "ai";
import { createApp } from "@aep/ae-design-agent/server";
import { createTurnSocketApp } from "@aep/ae-design-agent/edge/turn-socket";
import { MarketplaceBook } from "@aep/ae-design-agent/conversations/marketplace-book";
import { ThreadBook } from "@aep/ae-design-agent/conversations/thread-book";
import { createModel, type ModelConnection } from "@aep/ae-design-agent/shared/model";
import type { ConversationStore } from "@aep/ae-design-agent/store/conversation-store";
import type { ToolsSocket } from "@aep/ae-design-agent/tools-socket/client";
import { finishedTurnSink, TurnStarter } from "@aep/ae-design-agent/turns/start-turn";
import { TurnDesk } from "@aep/ae-design-agent/turns/turn-desk";
import { devAuth } from "../kit/auth.js";

export interface AgentsApp {
  /** The `/v1` edge's origin. */
  baseUrl: string;
  /** The dev credential every `/v1` request carries. */
  headers: Record<string, string>;
  /** The Turn socket's path (server-started turns: Plan). */
  turnSocket: string;
  close: () => Promise<void>;
}

export interface BootOptions {
  store: ConversationStore;
  tools: ToolsSocket;
  /** Where the tools socket materializes snapshots (`FsSpecWorkspace.mountRoot`). */
  snapshotsDir: string;
  /** A private dir for the Turn socket file. */
  socketDir: string;
  connection: ModelConnection;
  /** Test seam: a scripted model instead of `createModel`. */
  model?: LanguageModel;
  /** No one answers questions in this session (the one-shot phase verbs). */
  headless?: boolean;
  /** The project's kept thread, resumed as its current thread. */
  thread?: { project: string; conversationId: string };
}

async function listening(server: Server): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    server.once("listening", resolve);
    server.once("error", reject);
  });
}

function closing(server: Server): Promise<void> {
  server.closeAllConnections();
  return new Promise<void>((resolve) => server.close(() => resolve()));
}

export async function bootAgentsApp(opts: BootOptions): Promise<AgentsApp> {
  const { store } = opts;
  const threads = new ThreadBook({ store });
  if (opts.thread) threads.resume(opts.thread.project, opts.thread.conversationId);
  // No usage ledger in a local run; the sink still feeds auto-rotation.
  const desk = new TurnDesk({ onFinished: finishedTurnSink(threads, { push: () => {} }) });
  const turns = new TurnStarter({
    desk,
    threads,
    store,
    tools: opts.tools,
    snapshotsDir: opts.snapshotsDir,
    connection: opts.connection,
    buildModel: opts.model ? () => opts.model! : (conn, ctx) => createModel(conn, ctx),
    ...(opts.headless ? { headless: true } : {}),
  });
  const auth = devAuth();

  const edge = createApp({ authenticate: auth.authenticate, turns, desk, threads, marketplace: new MarketplaceBook(store) }).listen(
    0,
    "127.0.0.1",
  );
  await listening(edge);
  const turnSocket = join(opts.socketDir, "turn.sock");
  const socket = createTurnSocketApp({ turns, desk }).listen(turnSocket);
  await listening(socket);

  const { port } = edge.address() as AddressInfo;
  return {
    baseUrl: `http://127.0.0.1:${port}`,
    headers: auth.headers,
    turnSocket,
    close: async () => {
      turns.refuse();
      await desk.abortAll("shutdown");
      await Promise.all([closing(edge), closing(socket)]);
    },
  };
}
