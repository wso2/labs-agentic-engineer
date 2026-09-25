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
 * Boot the REAL agents service in-process (docs/design/playground.md §2):
 * the same `createApp` the deployed service runs — real HTTP contract, real
 * auth middleware (shared-secret HS256, minted per session like any caller),
 * real TurnGuard — with the playground's adapters around it: a
 * `FileConversationStore` under the project's dot-dir and the temp fixture
 * mount `FsSpecWorkspace` materializes snapshots into.
 */

import type { LanguageModel } from "ai";
import { createApp } from "@aep/agents/server";
import { createModel } from "@aep/agents/shared/model";
import { listen0 } from "@aep/agents/shared/listen";
import type { ConversationStore } from "@aep/agents/store/conversation-store";
import { EVAL_AUTH, evalTurnHeaders } from "../kit/auth.js";
import { PLAY_ORG } from "../ports/spec-workspace.js";

export interface AgentsApp {
  baseUrl: string;
  /** The M2M token + X-Anthropic-Key + X-Org-Id every turn POST carries. */
  headers: Record<string, string>;
  close: () => Promise<void>;
}

export interface BootOptions {
  store: ConversationStore;
  /** The fixture mount snapshots materialize into (FsSpecWorkspace.mountRoot). */
  workspaceMountRoot: string;
  /** ANTHROPIC_API_KEY for real runs; tests inject a mock via `model`. */
  apiKey: string;
  /** Test seam: bypass `createModel` with a scripted model. */
  model?: LanguageModel;
}

export async function bootAgentsApp(opts: BootOptions): Promise<AgentsApp> {
  const app = createApp({
    store: opts.store,
    buildModel: opts.model ? () => opts.model! : (key, model) => createModel({ apiKey: key, model }),
    auth: { audience: EVAL_AUTH.audience, secret: EVAL_AUTH.secret },
    workspaceMountRoot: opts.workspaceMountRoot,
  });
  const { baseUrl, close } = await listen0(app.listen(0));
  const headers = await evalTurnHeaders(opts.apiKey, PLAY_ORG);
  return { baseUrl, headers, close };
}
