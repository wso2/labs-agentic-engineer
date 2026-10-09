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
 * The project's current thread, as the console keeps it: one thread
 * per project on the design agent, its id kept in the project state so the
 * next session resumes it. `--fresh` rotates it; a send the agent refuses as
 * `conversation_rotated` (the thread filled up) adopts the fresh one.
 */

import type { ProjectState } from "../state/project.js";
import { saveProjectState } from "../state/project.js";

/** What the thread calls need from a session. */
export interface ThreadSession {
  projectDir: string;
  /** The project's name on the `/v1` edge. */
  project: string;
  baseUrl: string;
  headers: Record<string, string>;
  state: ProjectState;
}

async function threadCall(session: ThreadSession, method: "GET" | "POST", path: string): Promise<string> {
  const res = await fetch(`${session.baseUrl}/v1/projects/${encodeURIComponent(session.project)}${path}`, {
    method,
    headers: session.headers,
  });
  if (!res.ok) throw new Error(`${method} ${path}: HTTP ${res.status} ${await res.text().catch(() => "")}`);
  const { conversationId } = (await res.json()) as { conversationId: string };
  session.state.conversationUuid = conversationId;
  saveProjectState(session.projectDir, session.state);
  return conversationId;
}

/** Replace the project's thread with a fresh one (`--fresh`); the old one's history is dropped. */
export function rotateThread(session: ThreadSession): Promise<string> {
  return threadCall(session, "POST", "/conversations");
}

/** Keep the thread the design agent now names current (after it rotated a full one). */
export function adoptCurrentThread(session: ThreadSession): Promise<string> {
  return threadCall(session, "GET", "/conversations/current");
}
