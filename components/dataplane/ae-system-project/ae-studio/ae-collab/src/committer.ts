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

// The #86 phase-3 committer (#133), over the Files socket (07 §11): project a
// room's live doc into git as ONE commit per flush. ae-studio-tools owns git
// and resolves the project's repository per call; the socket is pod-local, so
// a flush carries no token. This module decides WHEN (Hocuspocus's debounced
// onStoreDocument, the last-leave unload, the `flush` message, shutdown) and
// WHAT (doc snapshot vs the room baseline).
//
// A failed flush never touches the doc: an outage (FilesUnavailableError,
// `disk_full` and `not_fast_forward` included) leaves the baseline where it
// was, so the next flush retries the same changes. Conflicts are doc-wins over
// the paths the room changed, bounded and REPORTED (#86 d6); see adoptHead.
// One path the pod's write rules refuse (named in its 400: outside specs/,
// over 5 MiB) is set aside and reported, and the rest of the flush is saved.

import type { Document } from "@hocuspocus/server";
import { deleteDocFile, hasPendingAgentMarks, isMarkdownPath, readDocFile, setDocFile, snapshotDoc } from "@aep/collab-doc";
import { isReferenceDocPath } from "./seed.js";
import {
  ApplyConflictError,
  FilesDeniedError,
  type ApplyConflict,
  FilesUnavailableError,
  type ApplyDelete,
  type ApplyWarning,
  type ApplyWrite,
  type FilesClient,
  type SpecFile,
} from "./files-client.js";
import { roomState, type RoomState } from "./rooms.js";
import type { FlushFailureCause, PodLog } from "./pod/log.js";

const MAX_CONFLICT_RETRIES = 2;
/**
 * How long the shutdown flush may take before shutdown goes on without it.
 * It must end inside ae-studio-tools' socket drain window
 * (`socketDrainWindow`, 10 s, cmd/ae-studio-tools/main.go): both
 * containers get SIGTERM together, and after that window the socket stops
 * accepting.
 */
export const SHUTDOWN_FLUSH_BUDGET_MS = 8_000;

/** Told about every path the pod's write rules refused, while it stays unsaved. */
const REFUSED =
  "AE Studio cannot save this file (a file must be under specs/ and at most 5 MiB); the room's " +
  "other edits were saved, and this one stays unsaved here until the file is changed or removed.";

/** Told to every path a flush saved over a commit made outside the room. */
const OVERWRITTEN =
  "This file was changed outside AE Studio while it was open here; the version from this " +
  "session was saved over that change, which stays in the repository history.";

export interface FlushDeps {
  files: FilesClient;
  log?: PodLog | undefined;
  /**
   * Called after every successful apply with that commit's warnings: the
   * pod's, plus one per path saved over an outside change. An empty list
   * means the commit had none (the console clears its Alert).
   */
  onWarnings?: ((warnings: ApplyWarning[]) => void) | undefined;
}

export interface FlushAllOptions {
  concurrency: number;
  force: boolean;
  /** Defaults to SHUTDOWN_FLUSH_BUDGET_MS. */
  budgetMs?: number;
}

function failureCause(err: unknown): FlushFailureCause {
  if (err instanceof ApplyConflictError) return "conflict";
  if (err instanceof FilesUnavailableError) return "files_unavailable";
  if (err instanceof FilesDeniedError) return "files_denied";
  return "internal";
}

function roomHasPendingChanges(doc: Document, state: RoomState, force: boolean): boolean {
  const { writes, deletes } = pendingChanges(doc, state, force);
  return writes.length > 0 || deletes.length > 0;
}

async function runPool<T>(
  items: readonly T[],
  concurrency: number,
  fn: (item: T) => Promise<void>,
): Promise<void> {
  if (items.length === 0) return;
  let next = 0;
  const workers = Array.from({ length: Math.min(concurrency, items.length) }, async () => {
    while (next < items.length) {
      const i = next++;
      await fn(items[i]!);
    }
  });
  await Promise.all(workers);
}

/**
 * Records what a room was just seeded with as its baseline: each file's sha,
 * and its content AS THE DOC SERIALIZES IT. The markdown serializer is not
 * byte-identical to git (it drops a trailing newline), so a baseline of git's
 * bytes would make every unedited markdown file a change, and every session
 * would commit a rewrite of files nobody touched.
 */
export function seedBaseline(state: RoomState, doc: Document, files: readonly SpecFile[]): void {
  for (const f of files) {
    state.baseline.set(f.path, { content: readDocFile(doc, f.path) ?? f.content, sha: f.sha });
  }
}

/**
 * The diff between the live doc and the room's baseline. Interim flushes
 * (`force: false`) HOLD files with pending agentInsertion marks — unreviewed
 * agent text never reaches git mid-session; the forced session-end flush
 * commits everything (accept-by-default; the serializer strips marks). A
 * change the pod's write rules refused is set aside (`refused`) while the
 * doc still holds exactly what was refused: it is not committable as it is.
 */
export function pendingChanges(
  doc: Document,
  state: RoomState,
  force: boolean,
): { writes: ApplyWrite[]; deletes: ApplyDelete[]; held: string[]; refused: string[] } {
  const current = snapshotDoc(doc);
  const writes: ApplyWrite[] = [];
  const deletes: ApplyDelete[] = [];
  const held: string[] = [];
  const refused: string[] = [];
  for (const [path, content] of Object.entries(current)) {
    // A room seeded before the reference exclusion existed may still hold
    // reference-document entries — they never flush (see isReferenceDocPath).
    if (isReferenceDocPath(path)) continue;
    const base = state.baseline.get(path);
    if (base && base.content === content) continue;
    // Emptied md fragments write as empty (top-level fragments cannot be
    // deleted from a Y.Doc); empty NEW files are noise — skip them.
    if (!base && content === "") continue;
    if (state.refused.get(path) === content) {
      refused.push(path);
      continue;
    }
    if (!force && isMarkdownPath(path) && hasPendingAgentMarks(doc, path)) {
      held.push(path);
      continue;
    }
    writes.push({ path, content, baseSha: base?.sha ?? "" });
  }
  for (const [path, base] of state.baseline) {
    if (current[path] !== undefined) continue;
    // References are never in the doc BY DESIGN, so "absent from the doc"
    // must not mean "delete from git" for them — that reading removed two
    // uploaded documents from a real repo.
    if (isReferenceDocPath(path)) continue;
    if (isMarkdownPath(path)) continue; // fragments never vanish; guard anyway
    if (base.sha === "") continue; // never reached git — nothing to delete
    if (state.refused.get(path) === null) {
      refused.push(path);
      continue;
    }
    deletes.push({ path, baseSha: base.sha });
  }
  return { writes, deletes, held, refused };
}

/**
 * Sets aside the change to `path` the pod refused, when the batch made one:
 * true when it did (the flush goes on without it), false for a path the
 * batch does not hold (the refusal stays a verdict on the whole flush).
 */
function setAside(state: RoomState, writes: readonly ApplyWrite[], deletes: readonly ApplyDelete[], path: string): boolean {
  const write = writes.find((w) => w.path === path);
  if (write) {
    state.refused.set(path, write.content);
    return true;
  }
  if (deletes.some((d) => d.path === path)) {
    state.refused.set(path, null);
    return true;
  }
  return false;
}

function trailers(state: RoomState): string {
  const lines = [...state.participants.values()]
    .sort((a, b) => a.email.localeCompare(b.email))
    .map((p) => `Co-authored-by: ${p.name || p.email} <${p.email}>`);
  return lines.length > 0 ? "\n\n" + lines.join("\n") : "";
}

/**
 * After a conflict: fold HEAD (a fresh bundle) into the room. Returns the
 * conflicted paths whose HEAD content the next apply will save over, and
 * those that need no saving at all.
 *
 *   conflicted, HEAD already holds the doc's content (a lost reply, a racing
 *     flush): adopt HEAD, nothing to write, nothing to report.
 *   conflicted, the doc went back to the baseline while the bundle was read
 *     (an undo; for a file the room created, the file is gone again):
 *     nothing of the room's to write; re-seeded like an unedited file below,
 *     so a file created outside the room is adopted, never deleted.
 *   conflicted, HEAD differs: doc wins (#86 d6). Adopt HEAD's sha as the
 *     precondition but keep the old content in the baseline, so the diff still
 *     writes the doc's version. Reported, since the apply saves over a commit
 *     made outside the room, unless git holds a blob this room committed
 *     itself (its own earlier write, not an outside change).
 *   not conflicted, changed in git, unedited in the room: re-seeded. The doc
 *     and the baseline take HEAD's version, so the change shows in the room
 *     and a later edit preconditions on it instead of silently replacing it.
 *
 * Anything else is left alone: a path HEAD gained outside the room is not the
 * doc's (adopted into the baseline, "absent from the doc" would read as
 * "deleted"), and a file the room edited without a conflict keeps its sha.
 */
function adoptHead(
  doc: Document,
  state: RoomState,
  conflicts: readonly ApplyConflict[],
  head: readonly SpecFile[],
): { overwritten: string[]; landed: string[] } {
  const heads = new Map(head.filter((f) => !isReferenceDocPath(f.path)).map((f) => [f.path, f]));
  // Read after the bundle arrived: the doc may have moved while it was fetched.
  const current = snapshotDoc(doc);
  const overwritten: string[] = [];
  const landed: string[] = [];
  const isConflicted = new Set(conflicts.map((c) => c.path));
  const reseed = (path: string, at: SpecFile | undefined) => {
    if (at) {
      setDocFile(doc, path, at.content);
      seedBaseline(state, doc, [at]);
    } else {
      deleteDocFile(doc, path);
      state.baseline.delete(path);
    }
  };
  for (const { path, currentSha } of conflicts) {
    const at = heads.get(path);
    const base = state.baseline.get(path);
    if (at?.content === current[path]) {
      if (at) state.baseline.set(path, { content: at.content, sha: at.sha });
      else state.baseline.delete(path);
      landed.push(path);
      continue;
    }
    // Undone while the bundle was read: back to the baseline, or, for a file
    // the room created, gone again (an emptied markdown fragment reads "",
    // which the diff skips as a new file too). Without a baseline entry the
    // sha-only adoption below would turn the outside copy into a delete.
    const undone = base ? current[path] === base.content : current[path] === undefined || current[path] === "";
    if (undone) {
      reseed(path, at);
      landed.push(path);
      continue;
    }
    if (!state.committed.has(`${path}\0${currentSha}`)) overwritten.push(path);
    state.baseline.set(path, { content: base?.content ?? "", sha: at?.sha ?? "" });
  }
  for (const [path, at] of heads) {
    if (isConflicted.has(path)) continue;
    const base = state.baseline.get(path);
    if (!base || base.sha === at.sha || current[path] !== base.content) continue;
    reseed(path, at);
  }
  return { overwritten, landed };
}

/**
 * Flush a room's pending changes as one commit. No-ops when clean or when the
 * room has no committer state. Throws the FilesClient's error when the flush
 * does not land; the baseline then stays put, so the next flush retries.
 *
 * One flush per room at a time: the debounced store, a `flush` message, the
 * last leave and shutdown can all ask at once, and a second apply racing the
 * first would conflict on the room's own write. A later caller waits for the
 * running flush, then diffs against the baseline it left.
 */
export function flushRoom(
  deps: FlushDeps,
  documentName: string,
  doc: Document,
  force = false,
): Promise<void> {
  const state = roomState(documentName);
  if (!state) return Promise.resolve();
  const run = state.flushing.then(() => flushOnce(deps, state, doc, force));
  state.flushing = run.catch(() => {});
  return run;
}

async function flushOnce(deps: FlushDeps, state: RoomState, doc: Document, force: boolean): Promise<void> {
  const overwritten = new Set<string>();
  let setAsideNow = false;
  const refusedWarnings = (paths: readonly string[]): ApplyWarning[] => paths.map((path) => ({ path, message: REFUSED }));
  try {
    for (let attempt = 0; ; ) {
      const { writes, deletes, held, refused } = pendingChanges(doc, state, force);
      if (held.length > 0) deps.log?.({ msg: "room_flush_held", source: "ae-collab", held: held.length });
      if (writes.length === 0 && deletes.length === 0) {
        // Nothing committable is left; a path set aside by this flush is still news.
        if (setAsideNow) deps.onWarnings?.(refusedWarnings(refused));
        return;
      }
      let outcome;
      try {
        outcome = await deps.files.apply(state.projectName, {
          writes,
          deletes,
          message: "collab session" + trailers(state),
        });
      } catch (err) {
        // One path the write rules refuse must not wedge the room: set it
        // aside and save the rest. Each pass removes a path, so this ends.
        if (err instanceof FilesDeniedError && err.path !== undefined && setAside(state, writes, deletes, err.path)) {
          deps.log?.({ msg: "room_flush_path_refused", source: "ae-collab" });
          setAsideNow = true;
          continue;
        }
        if (!(err instanceof ApplyConflictError) || attempt >= MAX_CONFLICT_RETRIES) throw err;
        attempt++;
        deps.log?.({ msg: "room_flush_conflict", source: "ae-collab", writes: writes.length, deletes: deletes.length });
        const adopted = adoptHead(doc, state, err.conflicts, await deps.files.bundle(state.projectName));
        for (const path of adopted.overwritten) overwritten.add(path);
        for (const path of adopted.landed) overwritten.delete(path);
        continue;
      }
      // The baseline moves to what just landed.
      const shas = new Map(outcome.files.map((f) => [f.path, f.sha]));
      for (const w of writes) {
        const sha = shas.get(w.path) ?? "";
        state.baseline.set(w.path, { content: w.content, sha });
        state.committed.add(`${w.path}\0${sha}`);
        state.refused.delete(w.path);
      }
      for (const d of deletes) {
        state.baseline.delete(d.path);
        state.refused.delete(d.path);
      }
      deps.log?.({ msg: "room_flush_committed", source: "ae-collab", writes: writes.length, deletes: deletes.length });
      deps.onWarnings?.([
        ...outcome.warnings,
        ...[...overwritten].map((path) => ({ path, message: OVERWRITTEN })),
        // Every commit restates what is still unsaved, so an empty list
        // means nothing is (the console clears its Alert).
        ...refusedWarnings(refused),
      ]);
      return;
    }
  } catch (err) {
    deps.log?.({ msg: "room_flush_failed", source: "ae-collab", cause: failureCause(err) });
    throw err;
  }
}

/**
 * Force-flush every loaded room during shutdown. Dirty rooms run first; up to
 * `concurrency` flushes run in parallel. Returns when all are done or when the
 * budget runs out, whichever is first: shutdown then goes on, and the count
 * of rooms still flushing is logged.
 */
export async function flushAllRooms(
  deps: Pick<FlushDeps, "files" | "log">,
  documents: Map<string, Document>,
  options: FlushAllOptions,
): Promise<void> {
  const { concurrency, force, budgetMs = SHUTDOWN_FLUSH_BUDGET_MS } = options;
  const names = [...documents.keys()].filter((name) => roomState(name));
  const dirty = new Map(names.map((name) => [name, roomHasPendingChanges(documents.get(name)!, roomState(name)!, force)]));
  names.sort((a, b) => {
    if (dirty.get(a) !== dirty.get(b)) return dirty.get(a) ? -1 : 1;
    return a.localeCompare(b);
  });
  const pending = new Set(names);
  const all = runPool(names, concurrency, async (documentName) => {
    const doc = documents.get(documentName);
    try {
      // flushRoom logs its own failure; one room never stops the others.
      if (doc) await flushRoom(deps, documentName, doc, force);
    } catch {
      // logged
    } finally {
      pending.delete(documentName);
    }
  });
  let timer: NodeJS.Timeout | undefined;
  const overBudget = new Promise<"over">((resolve) => {
    timer = setTimeout(() => resolve("over"), budgetMs);
  });
  try {
    if ((await Promise.race([all.then(() => "done" as const), overBudget])) === "over") {
      deps.log?.({ msg: "room_shutdown_flush_over_budget", source: "ae-collab", rooms: pending.size });
    }
  } finally {
    clearTimeout(timer);
  }
}
