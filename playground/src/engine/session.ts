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
 * Session assembly: wire the playground adapters around the real agents app
 * for one project directory. Everything phase commands need — the booted
 * in-process service, the workspace materializer, project state, and the
 * working-tree skills dir — behind one open/close pair.
 */

import { join } from "node:path";
import type { LanguageModel } from "ai";
import { bootAgentsApp } from "./agents-app.js";
import { playgroundModel, type PlaygroundModel } from "../kit/model-connection.js";
import { REPO_ROOT } from "../paths.js";
import { FsSpecWorkspace, playConversationId } from "../ports/spec-workspace.js";
import { FileConversationStore } from "../ports/conversation-store.js";
import { conversationsDir, loadProjectState, rememberProject, saveProjectState } from "../state/project.js";
import type { TurnSession } from "./turn.js";

/** The working-tree skill library (edits apply next turn — no rebuild). */
export const SKILLS_DIR = join(REPO_ROOT, "skills");

export interface PlaygroundSession extends TurnSession {
  store: FileConversationStore;
  close: () => Promise<void>;
}

export interface OpenOptions {
  /** `--fresh`: rotate the project's `general` conversation before the first turn. */
  fresh?: boolean;
  /** Test seam: scripted model instead of the `AEP_MODEL_*` / `ANTHROPIC_API_KEY` connection. */
  model?: LanguageModel;
  /** Override the skills library dir (tests). */
  skillsDir?: string;
}

/**
 * Open a session on `projectDir`. Throws when the connection has no key
 * (`AEP_MODEL_API_KEY`, or `ANTHROPIC_API_KEY` when no `AEP_MODEL_*` names a
 * connection) unless a model is injected.
 */
export async function openSession(projectDir: string, opts: OpenOptions = {}): Promise<PlaygroundSession> {
  const turnModel: PlaygroundModel = opts.model ? { apiKey: "playground-mock" } : playgroundModel();

  const ws = new FsSpecWorkspace(projectDir);
  const state = loadProjectState(projectDir, ws.slug);
  saveProjectState(projectDir, state); // persist the minted conversation uuid on first open
  rememberProject(projectDir);

  const store = new FileConversationStore(conversationsDir(projectDir));
  if (opts.fresh) {
    store.reset(playConversationId(state.slug, "general", state.conversationUuid));
  }

  const app = await bootAgentsApp({
    store,
    workspaceMountRoot: ws.mountRoot,
    apiKey: turnModel.apiKey,
    ...(opts.model ? { model: opts.model } : {}),
  });

  return {
    projectDir,
    ws,
    baseUrl: app.baseUrl,
    headers: app.headers,
    ...(turnModel.connection ? { connection: turnModel.connection } : {}),
    ...(turnModel.model ? { model: turnModel.model } : {}),
    state,
    skillsDir: opts.skillsDir ?? SKILLS_DIR,
    store,
    close: async () => {
      await app.close();
      ws.cleanup();
    },
  };
}
