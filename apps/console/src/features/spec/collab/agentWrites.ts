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

import type * as Y from "yjs";
import { applyToolCall, FileBundle, isFileMutationTool, type StreamPart } from "@aep/agent-stream";
import { deleteDocFile, isMarkdownPath, listDocPaths, readDocFile, setDocFile, setDocFileAsAgent } from "@aep/collab-doc";

// The mock's stand-in for the agent writing into the room (specDoc.ts
// `applyAgentWrite`). It replays each write through @aep/agent-stream's
// FileBundle, gates and all, so it lives in its own module: specDoc loads it
// on the first agent write in mock mode, and the app never ships it eagerly.

/** Marks the agent's file writes, applied here while the room is not wired. */
const AGENT_ORIGIN = "console:agent-write";

/** The writer the agents service names on its marks (services/agents room-peer.ts). */
const AGENT_NAME = "Spec Agent";

/**
 * Apply one of the agent's accepted file writes (a file tool's `tool-result`,
 * which carries the call's input) to the doc, through @aep/agent-stream's own
 * applier, so an `editFile` matches exactly as the agents service matched it.
 * False when the part is not a file write or changed nothing. An edit is not
 * idempotent (its new text can contain its old), so the caller applies each
 * write once.
 *
 * An existing markdown file takes the write as the agents service writes into
 * the room (setDocFileAsAgent): character-exact, so the user's text is
 * untouched, and what the agent inserts carries its mark with the time it
 * wrote it, which draws the fading wash (specLinesPlugin.ts).
 *
 * The stand-in for the room: today no peer writes the agent's changes into
 * this local doc, so the chat's turn stream does. Once the Hocuspocus provider
 * is wired the agents service writes them into the room itself, and this and
 * `applyAgentWrite` are deleted.
 */
export function applyAgentToolCall(doc: Y.Doc, part: StreamPart): boolean {
  if (!part.toolName || !isFileMutationTool(part.toolName)) return false;
  const input = part.input as { path?: unknown } | undefined;
  if (typeof input?.path !== "string") return false;
  const path = input.path;
  const before = readDocFile(doc, path);
  // The whole doc, as the agent's bundle holds the whole workspace: a gate
  // that reads another file (a prototype's source needs its manifest) judges
  // the write as the agents service did.
  const files: Record<string, string> = {};
  for (const p of listDocPaths(doc)) {
    const text = readDocFile(doc, p);
    if (text !== undefined) files[p] = text;
  }
  const bundle = new FileBundle(files);
  applyToolCall(bundle, { ...part, input: { ...input, path } });
  const after = bundle.read(path);
  if (after === before) return false;
  if (after === undefined) deleteDocFile(doc, path, AGENT_ORIGIN);
  else if (before !== undefined && isMarkdownPath(path)) {
    setDocFileAsAgent(doc, path, after, AGENT_ORIGIN, { agent: AGENT_NAME, at: new Date().toISOString() });
  }
  // A new file, or a plain-text one (diff-and-patched by setDocFile).
  else setDocFile(doc, path, after, AGENT_ORIGIN);
  return true;
}

