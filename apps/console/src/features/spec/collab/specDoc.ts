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

import { useMemo } from "react";
import * as Y from "yjs";
import { isFileMutationTool, type StreamPart } from "@aep/agent-stream";
import { setDocFile } from "@aep/collab-doc";
import { useSession } from "../../../auth/SessionContext";
import { env } from "../../../config/env";
import { useMockSpecExtras, type MockSpecExtras } from "../api/specModel";
import { useSpecRoom } from "./specRoom";

// The project's spec document: ONE Y.Doc per project, every file a share keyed
// by its repo path (@aep/collab-doc's model). Everything in the app that reads
// or edits the spec gets the doc from here, and nothing else creates one.
//
// ON THE PLATFORM it is the collab room's doc (specRoom.ts): seeded server-side
// from git, edited by everyone in the project and by the agent, committed back
// by the collab server. Null until the room has synced.
//
// IN MOCK MODE it is a local doc, seeded once from the mock's files and kept
// for the browser session, so an edit survives moving between files. The
// agent's file writes in a chat turn are applied to it from the turn's stream
// (`applyAgentWrite`, agentWrites.ts), marked as the agent's the way the
// agents service writes into the room, so they land with a fading wash. No one else sees it, and a
// reload starts over.

/** Marks the seed's writes, so they are never mistaken for the user's edits. */
const SEED_ORIGIN = "console:local-seed";

/** Seed a doc as the room would hold it: the files. */
export function seedSpecDoc(doc: Y.Doc, seed: Pick<MockSpecExtras, "files">): void {
  for (const [path, markdown] of Object.entries(seed.files)) setDocFile(doc, path, markdown, SEED_ORIGIN);
}

const sessionDocs = new Map<string, Y.Doc>();

/**
 * The project's doc, seeded from the model the first time it is asked for.
 * The app reads it through `useSpecDoc`; MSW's design agent reads it too, as
 * the real agent reads the room, to see the spec as the user has edited it.
 */
export function projectSpecDoc(projectName: string, seed: Pick<MockSpecExtras, "files">): Y.Doc {
  const existing = sessionDocs.get(projectName);
  if (existing) return existing;
  const doc = new Y.Doc();
  seedSpecDoc(doc, seed);
  sessionDocs.set(projectName, doc);
  return doc;
}

/** The project's spec doc, or null until it is ready. */
export function useSpecDoc(projectName: string): Y.Doc | null {
  const mock = env.apiMode === "mock";
  const { orgHandle } = useSession();
  const room = useSpecRoom(orgHandle, projectName, !mock);
  const seed = useMockSpecExtras(projectName).data;
  const local = useMemo(() => (mock && seed ? projectSpecDoc(projectName, seed) : null), [mock, projectName, seed]);
  return mock ? local : room.doc;
}

/** Commit the room's pending edits to git before a build tags HEAD; nothing to commit in mock mode. */
export function useSpecFlush(projectName: string): () => Promise<void> {
  const { orgHandle } = useSession();
  return useSpecRoom(orgHandle, projectName, env.apiMode !== "mock").flush;
}

/**
 * The agent wrote a file in a project's spec: apply it to that project's doc.
 * A project whose doc was never opened has nothing to update; its doc is
 * seeded from the mock, which already holds the write, when it opens. On the
 * platform no local doc exists: the agent writes into the room.
 */
export function applyAgentWrite(projectName: string, part: StreamPart): void {
  const doc = sessionDocs.get(projectName);
  if (!doc || !part.toolName || !isFileMutationTool(part.toolName)) return;
  // The applier replays the write through @aep/agent-stream's FileBundle and
  // its gates (the prototype kit's parsers among them): mock-only code, so it
  // loads on the first write, never with the app. Writes apply in order: each
  // waits on the same module promise.
  agentWrites ??= import("./agentWrites");
  void agentWrites.then((m) => m.applyAgentToolCall(doc, part));
}

let agentWrites: Promise<typeof import("./agentWrites")> | null = null;
