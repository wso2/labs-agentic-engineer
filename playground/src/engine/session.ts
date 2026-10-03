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
 * Session assembly: wire the playground adapters around the real design agent
 * for one project directory. Everything phase commands need — the booted
 * in-process service, the workspace materializer, project state, and the
 * working-tree skills dir — behind one open/close pair.
 */

import { mkdtempSync, rmSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import type { LanguageModel } from "ai";
import { anthropicConnection } from "@aep/ae-design-agent/shared/model";
import { bootAgentsApp } from "./agents-app.js";
import { PlaygroundToolsSocket } from "./tools-fake.js";
import { playgroundModel } from "../kit/model-connection.js";
import { REPO_ROOT } from "../paths.js";
import { FsSpecWorkspace } from "../ports/spec-workspace.js";
import { FileConversationStore } from "../ports/conversation-store.js";
import { conversationsDir, loadProjectState, rememberProject, saveProjectState } from "../state/project.js";
import { rotateThread } from "./thread.js";
import type { TurnSession } from "./turn.js";

/** The working-tree skill library (edits apply next turn — no rebuild). */
export const SKILLS_DIR = join(REPO_ROOT, "skills");

export interface PlaygroundSession extends TurnSession {
  store: FileConversationStore;
  close: () => Promise<void>;
}

export interface OpenOptions {
  /** `--fresh`: rotate the project's current thread before the first turn. */
  fresh?: boolean;
  /** Test seam: scripted model instead of the `AEP_MODEL_*` / `ANTHROPIC_API_KEY` connection. */
  model?: LanguageModel;
  /** Override the skills library dir (tests). */
  skillsDir?: string;
  /** No one answers questions in this session (the one-shot phase verbs). */
  headless?: boolean;
}

/**
 * Open a session on `projectDir`. Throws when the connection has no key
 * (`AEP_MODEL_API_KEY`, or `ANTHROPIC_API_KEY` when no `AEP_MODEL_*` names a
 * connection) unless a model is injected.
 */
export async function openSession(projectDir: string, opts: OpenOptions = {}): Promise<PlaygroundSession> {
  const connection = opts.model ? anthropicConnection("playground-mock") : playgroundModel();

  const ws = new FsSpecWorkspace(projectDir);
  const state = loadProjectState(projectDir, ws.slug);
  saveProjectState(projectDir, state); // persist the minted thread id on first open
  rememberProject(projectDir);

  const skillsDir = opts.skillsDir ?? SKILLS_DIR;
  const store = new FileConversationStore(conversationsDir(projectDir));
  // The Turn socket's private dir (mkdtemp: 0700, so only this user reaches it).
  const socketDir = mkdtempSync(join(tmpdir(), "aep-play-sock-"));
  const app = await bootAgentsApp({
    store,
    tools: new PlaygroundToolsSocket(ws, skillsDir),
    snapshotsDir: ws.mountRoot,
    socketDir,
    connection,
    thread: { project: ws.slug, conversationId: state.conversationUuid },
    ...(opts.model ? { model: opts.model } : {}),
    ...(opts.headless ? { headless: true } : {}),
  });

  const session: PlaygroundSession = {
    projectDir,
    ws,
    project: ws.slug,
    baseUrl: app.baseUrl,
    headers: app.headers,
    turnSocket: app.turnSocket,
    state,
    skillsDir,
    store,
    close: async () => {
      await app.close();
      ws.cleanup();
      rmSync(socketDir, { recursive: true, force: true });
    },
  };
  if (opts.fresh) {
    try {
      await rotateThread(session);
    } catch (err) {
      await session.close();
      throw err;
    }
  }
  return session;
}
