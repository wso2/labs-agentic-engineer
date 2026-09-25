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

// The messages on an OpenCode session's stream, as the adapter reads them.
//
// Almost all of them are the server's bus events, exactly as `GET /event`
// delivers them (`{type, properties}`); `readEvent` is the one shape check the
// classifier and the translator share. Two are the ADAPTER's own, and each
// exists because the loop needs a fact the bus does not carry:
//
//   `aep.skills` — the skills the server DISCOVERED, read from `GET /skill` at
//     start and put first on the stream. Claude Code declares this on its
//     `init` message and the loop's preload check reads it there; OpenCode has
//     no such message, and a mirrored skill that was not discovered is dropped
//     in silence exactly as it was on Claude Code (skills_preload_check.ts).
//   `aep.tick` — a clock tick every ten seconds while the run is open. OpenCode
//     sends nothing while a tool runs (no tool-progress frame), so a long
//     `bash` would otherwise be indistinguishable from a wedged one; the
//     translator turns a tick into a rate-limited `heartbeat {waitingOn: tool}`
//     for each call still running.
//
// They are namespaced `aep.` so no OpenCode event can be mistaken for one, and
// they are written to the raw log like everything else on the stream.

import { obj } from "../fields.js";

/** The adapter's declaration of the skills the server discovered. */
export const SKILLS_DECLARED = "aep.skills";

/** The adapter's own clock, for tools that run silently. */
export const TOOL_TICK = "aep.tick";

export interface SkillsDeclared {
  type: typeof SKILLS_DECLARED;
  skills: string[];
}

export interface ToolTick {
  type: typeof TOOL_TICK;
}

/** One bus event, loosely: the type and its properties, nothing assumed beyond. */
export interface BusEvent {
  type: string;
  properties: Record<string, unknown>;
}

/** A message as a bus event, or undefined for anything that is not one. */
export function readEvent(message: unknown): BusEvent | undefined {
  if (!message || typeof message !== "object") return undefined;
  const m = message as Record<string, unknown>;
  if (typeof m.type !== "string") return undefined;
  const properties = obj(m.properties);
  return { type: m.type, properties };
}

/**
 * Bus events that say nothing about the run: server keep-alives and
 * connection, plugin/catalog/reference/integration announcements, the file
 * watcher's echo of a write the tool part already reported, diff and title
 * bookkeeping, LSP and installation chatter. Measured on every recording
 * (`test/fixtures/opencode-*`): 45 `plugin.added` per server start alone.
 */
export const NOISE_EVENTS = new Set([
  "server.connected",
  "server.heartbeat",
  "server.instance.disposed",
  "plugin.added",
  "catalog.updated",
  "reference.updated",
  "integration.updated",
  "file.watcher.updated",
  "file.edited",
  "session.diff",
  "session.updated",
  "installation.updated",
  "installation.update-available",
  "lsp.updated",
  "lsp.client.diagnostics",
  "project.updated",
  "vcs.branch.updated",
  "mcp.tools.changed",
]);
