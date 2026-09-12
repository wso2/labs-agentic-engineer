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
 * (docs/design/playground.md), the direct descendant of the in-package
 * playground's `runTurn`:
 *
 *   read project → files map → materialize snapshot (+ working-tree skills)
 *   → POST one workspace-shape turn → stream parts, folding + diff-writing each
 *     tool-call to disk the INSTANT it arrives (files show up as the agent
 *     creates them, not batched to turn end)
 *   → verify manifest sha256s (WARN, never block)
 *   → materialize derived artifacts → record the folded hash (D20).
 *
 * The conversation itself is persisted by the SERVICE via the injected
 * `FileConversationStore` — this loop owns only the disk reconcile.
 */

import { FileBundle, applyToolCall, streamTurn, type StreamPart, type TurnRequest, type TurnSpec } from "@aep/agent-stream";
import { filterTurnSnapshot } from "@aep/agents/conversation/load-workspace";
import { sha256Hex } from "@aep/agents/shared/hash";
import { loadRepoSkills, type RepoSkill } from "../kit/skills.js";
import { reconcileFile, type FileChange } from "../kit/project-fs.js";
import { compileDslDerived, projectCellDiagram, CELL_DIAGRAM_PATH, type DerivedNote } from "../kit/derived.js";
import { FsSpecWorkspace, hashFiles, playConversationId } from "../ports/spec-workspace.js";
import type { ProjectState } from "../state/project.js";
import { saveProjectState } from "../state/project.js";

export interface TurnSession {
  projectDir: string;
  ws: FsSpecWorkspace;
  baseUrl: string;
  headers: Record<string, string>;
  state: ProjectState;
  /** Repo-root `skills/` — read fresh EVERY turn (§8 hot-reload). */
  skillsDir: string;
}

export interface SpecTurnOptions {
  /** Conversation use-case segment; spec phases share ONE `general` conversation. */
  useCase?: string;
  /** Overrides the project's general-conversation uuid (one-shot plan turns). */
  conversationUuid?: string;
  mcp?: { url: string; token: string };
  /** The spec-bundle path this turn should write to, when one is pinned. */
  target?: string;
  /**
   * No interview is possible in this run (the one-shot phase verbs): the
   * service tells the agent to generate on stated assumptions instead of
   * calling the question tools.
   */
  headless?: boolean;
  /** Live rendering hook; every streamed part passes through it. */
  onPart?: (part: StreamPart) => void;
  /** Skip the disk reconcile (plan turns mutate nothing under specs/). */
  foldToDisk?: boolean;
}

export interface SpecTurnResult {
  parts: StreamPart[];
  toolCalls: StreamPart[];
  changes: FileChange[];
  derivedNotes: DerivedNote[];
  /** Paths whose folded sha256 disagreed with the server manifest (drift alarm). */
  manifestMismatches: string[];
  error?: string;
}

/**
 * Run one turn. `turn` states what the turn is FOR (see engine/turn-spec.ts);
 * the agents service composes the instruction text from it, so the playground
 * and production send byte-identical prompts.
 */
export async function runSpecTurn(session: TurnSession, turn: TurnSpec, opts: SpecTurnOptions = {}): Promise<SpecTurnResult> {
  const { projectDir, ws, state } = session;
  const useCase = opts.useCase ?? "general";
  const conversationId = playConversationId(state.slug, useCase, opts.conversationUuid ?? state.conversationUuid);
  const skills: RepoSkill[] = loadRepoSkills(session.skillsDir);

  const before = ws.readSpecFiles();
  // D20: a hand-edit between turns is first-class — flag it so the agent
  // re-reads rather than trusting its conversation memory.
  const filesChangedExternally = state.lastFoldedHash !== undefined && hashFiles(before) !== state.lastFoldedHash;

  const body: TurnRequest = {
    turn,
    workspace: ws.workspaceRef(conversationId, before, skills),
    ...(filesChangedExternally ? { filesChangedExternally: true } : {}),
    ...(opts.target ? { target: opts.target } : {}),
    ...(opts.headless ? { headless: true } : {}),
    ...(opts.mcp ? { mcp: opts.mcp } : {}),
  };

  const parts: StreamPart[] = [];
  const toolCalls: StreamPart[] = [];
  let manifest: StreamPart | undefined;
  let streamError: string | undefined;

  // Fold each tool-call over the SERVER'S filtered view of the snapshot — the
  // state the agent actually saw — and diff-write it to disk AS IT STREAMS, so
  // a file lands the instant the agent finishes emitting its tool-call rather
  // than batched to turn end. Tool-calls that stream before a mid-stream error
  // are real server-side mutations, so they still apply. Plan turns
  // (`foldToDisk === false`) mutate nothing under specs/ — they only observe.
  const fold = opts.foldToDisk !== false;
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
    if (!fold) return;
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
    for await (const part of streamTurn(session.baseUrl, conversationId, body, { headers: session.headers })) {
      parts.push(part);
      opts.onPart?.(part);
      if (part.type === "tool-call") foldToolCall(part);
      if (part.type === "manifest") manifest = part;
      if (part.type === "error") streamError = String(part.error ?? "stream error");
    }
  } catch (e) {
    // Transport failure mid-stream: tool-calls already folded above are
    // committed server-side and on disk (with their derived views), so report
    // them rather than claiming the project is untouched.
    return {
      parts,
      toolCalls,
      changes,
      derivedNotes: [...derived.values()],
      manifestMismatches: [],
      error: e instanceof Error ? e.message : String(e),
    };
  }

  if (!fold) {
    return { parts, toolCalls, changes: [], derivedNotes: [], manifestMismatches: [], ...(streamError ? { error: streamError } : {}) };
  }

  const folded = bundle.snapshot();

  // Cheap D14 parity: the terminal manifest's sha256s must match our fold.
  const manifestMismatches: string[] = [];
  if (manifest?.files) {
    for (const [path, sha] of Object.entries(manifest.files)) {
      const content = folded[path];
      if (content === undefined || sha256Hex(content) !== sha) manifestMismatches.push(path);
    }
  }

  // `after`, `changes`, and `derived` were all built incrementally in the
  // stream loop above over the FILTERED view: files the turn filter hid are
  // absent from both sides of every diff and so were never touched, deletions
  // applied only to files the agent could see (via removeFile tool-calls), and
  // every derived view (.excalidraw / cell-diagram.gen.json) was refreshed as
  // its source landed.
  const derivedNotes = [...derived.values()];

  state.lastFoldedHash = hashFiles(ws.readSpecFiles());
  saveProjectState(projectDir, state);

  return { parts, toolCalls, changes, derivedNotes, manifestMismatches, ...(streamError ? { error: streamError } : {}) };
}
