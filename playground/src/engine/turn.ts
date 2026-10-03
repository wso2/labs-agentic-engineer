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
 * One engineering-agent turn end to end — the §5 engine loop
 * (docs/design/playground.md):
 *
 *   read project → POST the instruction verbatim to the design agent's `/v1`
 *     edge (it looks the project up through the in-process tools socket,
 *     which materializes the snapshot and the working-tree skills)
 *   → stream the turn, folding + diff-writing each tool-call to disk the
 *     INSTANT it arrives (files show up as the agent creates them)
 *   → refresh derived artifacts as their sources land.
 *
 * `/<command>` lines are parsed by the design agent, as the console's are.
 * The conversation itself is persisted by the SERVICE via the injected
 * `FileConversationStore` — this loop owns only the disk reconcile.
 */

import {
  FileBundle,
  applyToolCall,
  startAndStreamTurn,
  TurnRefusedError,
  type StreamPart,
  type TurnStartBody,
} from "@aep/agent-stream";
import { filterTurnSnapshot } from "@aep/ae-design-agent/conversation/load-workspace";
import { reconcileFile, type FileChange } from "../kit/project-fs.js";
import { compileDslDerived, projectCellDiagram, CELL_DIAGRAM_PATH, type DerivedNote } from "../kit/derived.js";
import type { FsSpecWorkspace } from "../ports/spec-workspace.js";
import { adoptCurrentThread, type ThreadSession } from "./thread.js";

export interface TurnSession extends ThreadSession {
  ws: FsSpecWorkspace;
  /** The Turn socket's path (Plan turns, `engine/plan-turn.ts`). */
  turnSocket: string;
  /** Repo-root `skills/` — read fresh EVERY turn (§8 hot-reload). */
  skillsDir: string;
}

export interface SpecTurnOptions {
  /** The spec-bundle path this turn should write to, when one is pinned. */
  target?: string;
  /** Live rendering hook; every streamed part passes through it. */
  onPart?: (part: StreamPart) => void;
}

export interface SpecTurnResult {
  parts: StreamPart[];
  toolCalls: StreamPart[];
  changes: FileChange[];
  derivedNotes: DerivedNote[];
  error?: string;
}

/** The failure a turn's terminal part reports, if it failed. */
function failureOf(part: StreamPart): string | undefined {
  const end = part as { type: string; reason?: string; code?: string; message?: string };
  if (end.type !== "turn-failed") return undefined;
  return end.message ?? end.code ?? end.reason ?? "turn failed";
}

/**
 * Start the turn on the project's current thread. A thread the agent rotated
 * away (it filled up) is refused once as `conversation_rotated`: the fresh
 * thread is adopted and the send repeated on it, as the console does.
 */
async function* streamOnThread(session: TurnSession, body: TurnStartBody): AsyncGenerator<StreamPart> {
  const start = () =>
    startAndStreamTurn(session.baseUrl, session.project, session.state.conversationUuid, body, session.headers);
  let stream = start();
  let first: IteratorResult<StreamPart, unknown>;
  try {
    first = await stream.next();
  } catch (err) {
    if (!(err instanceof TurnRefusedError) || err.code !== "conversation_rotated") throw err;
    await adoptCurrentThread(session);
    stream = start();
    first = await stream.next();
  }
  if (first.done) return;
  yield first.value;
  yield* stream;
}

/** Run one turn: `instruction` is sent verbatim, as a user typed it. */
export async function runSpecTurn(session: TurnSession, instruction: string, opts: SpecTurnOptions = {}): Promise<SpecTurnResult> {
  const { projectDir, ws, state } = session;
  const before = ws.readSpecFiles();

  const body: TurnStartBody = { instruction, ...(opts.target ? { target: opts.target } : {}) };

  const parts: StreamPart[] = [];
  const toolCalls: StreamPart[] = [];
  let streamError: string | undefined;

  // Fold each tool-call over the SERVER'S filtered view of the snapshot — the
  // state the agent actually saw — and diff-write it to disk AS IT STREAMS, so
  // a file lands the instant the agent finishes emitting its tool-call rather
  // than batched to turn end. Tool-calls that stream before a mid-stream error
  // are real server-side mutations, so they still apply.
  const view = filterTurnSnapshot(before);
  const bundle = new FileBundle(view);
  // `after` tracks the FILTERED view as the fold advances — never the raw disk
  // read. The reconcile below diffs `after[path]` against the bundle, and the
  // bundle only ever holds what the filter admitted: seeded from `before`, a
  // path the filter hid reads as present here and absent from the bundle, which
  // reconcileFile reads as a deletion and rmSync's off disk. That is how a chat
  // turn whose only write was REFUSED still ended with
  // `− specs/design/security.json` and the file gone. Seeding from the view
  // makes both sides of every diff the same state the agent actually saw, so a
  // path outside it is untouched (undefined → undefined → no change) whatever
  // the turn does.
  const after: Record<string, string> = { ...view };
  const changes: FileChange[] = [];
  // Derived-artifact notes keyed by output so a source touched twice in a turn
  // (or the aggregate cell-diagram rebuilt on every design change) collapses to
  // one last-write-wins note; the files themselves are refreshed on disk live.
  const derived = new Map<string, DerivedNote>();

  const foldToolCall = (part: StreamPart): void => {
    toolCalls.push(part);
    const input = part.input as { path?: unknown } | undefined;
    const path = typeof input?.path === "string" ? input.path : undefined;
    applyToolCall(bundle, part);
    if (!path) return;
    // A rejected op (INVALID_YAML, ALREADY_EXISTS, …) leaves the bundle
    // unchanged, so reconcileFile diffs equal and writes nothing.
    const change = reconcileFile(projectDir, path, after[path], bundle.read(path));
    if (!change) return;
    changes.push(change);
    if (bundle.has(path)) after[path] = bundle.read(path)!;
    else delete after[path];
    // Refresh this change's derived views immediately: the *.dsl → .excalidraw
    // compile is per-file; any specs/design/ change re-rolls the aggregate
    // cell-diagram from the current snapshot (last rebuild wins the final view).
    if (change.kind !== "remove") {
      const note = compileDslDerived(projectDir, change.path);
      if (note) derived.set(change.path, note);
    }
    if (change.path.startsWith("specs/design/")) {
      derived.set(CELL_DIAGRAM_PATH, projectCellDiagram(projectDir, state.slug, after));
    }
  };

  try {
    for await (const part of streamOnThread(session, body)) {
      parts.push(part);
      opts.onPart?.(part);
      if (part.type === "tool-call") foldToolCall(part);
      if (part.type === "error") streamError = String(part.error ?? "stream error");
      streamError = failureOf(part) ?? streamError;
    }
  } catch (e) {
    // Transport failure mid-stream: tool-calls already folded above are
    // committed server-side and on disk (with their derived views), so report
    // them rather than claiming the project is untouched.
    return { parts, toolCalls, changes, derivedNotes: [...derived.values()], error: e instanceof Error ? e.message : String(e) };
  }

  // `after`, `changes`, and `derived` were all built incrementally in the
  // stream loop above over the FILTERED view: files the turn filter hid are
  // absent from both sides of every diff and so were never touched, deletions
  // applied only to files the agent could see (via removeFile tool-calls), and
  // every derived view (.excalidraw / cell-diagram.gen.json) was refreshed as
  // its source landed.
  return { parts, toolCalls, changes, derivedNotes: [...derived.values()], ...(streamError ? { error: streamError } : {}) };
}
