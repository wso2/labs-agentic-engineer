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

// The #86 phase-3 committer (#133): project a room's live doc into git as
// ONE bot commit per flush, through the BFF's files/apply — git stays the
// BFF's monopoly; this module only decides WHEN (Hocuspocus's debounced
// onStoreDocument + the pre-unload final store) and WHAT (doc snapshot vs
// the room baseline). Conflicts are doc-wins: refetch shas, re-apply,
// bounded retries (#86 d6).

import type { Document } from "@hocuspocus/server";
import {
  hasPendingAgentMarks,
  isMarkdownPath,
  snapshotDoc,
} from "@aep/collab-doc";
import { isReferenceDocPath } from "./seed.js";
import {
  ApplyAuthError,
  ApplyConflictError,
  type ApplyDelete,
  type ApplyWrite,
  type BffClient,
} from "./bff.js";
import { roomState, type RoomState } from "./rooms.js";
import type { CollabContext } from "./server.js";

const MAX_CONFLICT_RETRIES = 2;
const SLOW_SHUTDOWN_FLUSH_MS = 25_000;

interface FlushDeps {
  bff: BffClient;
  log?: ((message: string) => void) | undefined;
  /**
   * Pull a fresh bearer after ApplyAuthError (D6). Typically asks a connected
   * client via `{type:"token-please"}`. Absent/empty → classify + log, no retry.
   */
  tokenRefresh?: (() => Promise<string | null>) | undefined;
}

function flushFailureLog(
  documentName: string,
  projectName: string,
  err: unknown,
  writes: number,
  deletes: number,
  refreshAttempted: boolean,
  refreshOutcome: "ok" | "failed" | "skipped" | "n/a",
): string {
  const status =
    err instanceof ApplyAuthError
      ? String(err.status)
      : err instanceof Error
        ? "error"
        : "n/a";
  return (
    `committer: ${documentName} project=${projectName} flush failed ` +
    `status=${status} writes=${writes} deletes=${deletes} ` +
    `refreshAttempted=${refreshAttempted} refreshOutcome=${refreshOutcome}`
  );
}

export interface FlushAllOptions {
  concurrency: number;
  force: boolean;
}

function roomHasPendingChanges(
  doc: Document,
  state: RoomState,
  force: boolean,
): boolean {
  const { writes, deletes } = pendingChanges(doc, state, force);
  return writes.length > 0 || deletes.length > 0;
}

function truncateRoomList(names: readonly string[], maxLen = 200): string {
  const joined = names.join(", ");
  return joined.length <= maxLen ? joined : `${joined.slice(0, maxLen)}…`;
}

async function runPool<T>(
  items: readonly T[],
  concurrency: number,
  fn: (item: T) => Promise<void>,
): Promise<void> {
  if (items.length === 0) return;
  let next = 0;
  const workers = Array.from(
    { length: Math.min(concurrency, items.length) },
    async () => {
      while (next < items.length) {
        const i = next++;
        await fn(items[i]!);
      }
    },
  );
  await Promise.all(workers);
}

/**
 * The diff between the live doc and the room's baseline. Interim flushes
 * (`force: false`) HOLD files with pending agentInsertion marks — unreviewed
 * agent text never reaches git mid-session; the forced session-end flush
 * commits everything (accept-by-default; the serializer strips marks).
 */
export function pendingChanges(
  doc: Document,
  state: RoomState,
  force: boolean,
): { writes: ApplyWrite[]; deletes: ApplyDelete[]; held: string[] } {
  const current = snapshotDoc(doc);
  const writes: ApplyWrite[] = [];
  const deletes: ApplyDelete[] = [];
  const held: string[] = [];
  for (const [path, content] of Object.entries(current)) {
    // A room seeded before the reference exclusion existed may still hold
    // reference-document entries — they never flush (see isReferenceDocPath).
    if (isReferenceDocPath(path)) continue;
    const base = state.baseline.get(path);
    if (base && base.content === content) continue;
    // Emptied md fragments write as empty (top-level fragments cannot be
    // deleted from a Y.Doc); empty NEW files are noise — skip them.
    if (!base && content === "") continue;
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
    deletes.push({ path, baseSha: base.sha });
  }
  return { writes, deletes, held };
}

function trailers(state: RoomState): string {
  const lines = [...state.participants.values()]
    .sort((a, b) => a.email.localeCompare(b.email))
    .map((p) => `Co-authored-by: ${p.name || p.email} <${p.email}>`);
  return lines.length > 0 ? "\n\n" + lines.join("\n") : "";
}

/**
 * Flush a room's pending changes as one commit. No-ops when clean, when the
 * room has no committer state (dev mode never registers any), or when no
 * participant token is available. Doc-wins on conflict: refresh the baseline
 * shas from HEAD and re-apply.
 */
export async function flushRoom(
  deps: FlushDeps,
  documentName: string,
  doc: Document,
  context: CollabContext | undefined,
  force = false,
): Promise<void> {
  const state = roomState(documentName);
  if (!state) return;
  let token = context?.token ?? state.lastToken;
  if (!token) {
    deps.log?.(`committer: no token for ${documentName} — skipping flush`);
    return;
  }

  let authRetried = false;
  for (let attempt = 0; ; attempt++) {
    const { writes, deletes, held } = pendingChanges(doc, state, force);
    if (held.length > 0) {
      deps.log?.(
        `committer: ${documentName} holding ${held.join(", ")} (pending agent review)`,
      );
    }
    if (writes.length === 0 && deletes.length === 0) return;
    deps.log?.(
      `committer: ${documentName} applying writes=[${writes
        .map((w) => w.path)
        .join(", ")}] deletes=[${deletes.map((d) => d.path).join(", ")}]`,
    );

    try {
      const outcome = await deps.bff.applyFiles(token, state.projectName, {
        writes,
        deletes,
        message: "collab session" + trailers(state),
      });
      // Baseline moves to what we just landed.
      const newShas = new Map(outcome.files.map((f) => [f.path, f.sha]));
      for (const w of writes) {
        state.baseline.set(w.path, {
          content: w.content,
          sha: newShas.get(w.path) ?? "",
        });
      }
      for (const d of deletes) state.baseline.delete(d.path);
      deps.log?.(
        `committer: ${documentName} → ${outcome.commitSha.slice(0, 8)} ` +
          `(${writes.length} write(s), ${deletes.length} delete(s))`,
      );
      return;
    } catch (err) {
      if (
        err instanceof ApplyAuthError &&
        !authRetried &&
        deps.tokenRefresh
      ) {
        authRetried = true;
        const fresh = await deps.tokenRefresh();
        if (fresh) {
          token = fresh;
          state.lastToken = fresh;
          if (context) context.token = fresh;
          deps.log?.(
            `committer: ${documentName} project=${state.projectName} ` +
              `auth ${err.status} — retrying once with refreshed token ` +
              `(writes=${writes.length} deletes=${deletes.length} refreshOutcome=ok)`,
          );
          continue;
        }
        deps.log?.(
          flushFailureLog(
            documentName,
            state.projectName,
            err,
            writes.length,
            deletes.length,
            true,
            "failed",
          ),
        );
        throw err;
      }
      if (err instanceof ApplyConflictError && attempt < MAX_CONFLICT_RETRIES) {
        // Someone moved git under the session: the DOC wins (#86 d6) — adopt
        // HEAD's shas as the new base and re-apply our content over it.
        deps.log?.(
          `committer: ${documentName} conflict on ${err.paths.join(", ")} — re-applying (doc wins)`,
        );
        const head = await deps.bff.fetchSpecFiles(token, state.projectName);
        // Same exclusion as the seed: HEAD carries the reference documents,
        // and adopting them into the baseline is how they reached the delete
        // loop in the first place.
        for (const f of head.filter((h) => !isReferenceDocPath(h.path))) {
          const base = state.baseline.get(f.path);
          state.baseline.set(f.path, {
            // Keep OUR notion of content (so the diff still sees the doc's
            // version as a change), but adopt HEAD's sha as the precondition.
            content: base?.content ?? "",
            sha: f.sha,
          });
        }
        continue;
      }
      deps.log?.(
        flushFailureLog(
          documentName,
          state.projectName,
          err,
          writes.length,
          deletes.length,
          authRetried,
          err instanceof ApplyAuthError
            ? authRetried
              ? "failed"
              : "skipped"
            : "n/a",
        ),
      );
      throw err;
    }
  }
}

/**
 * Force-flush every loaded room during shutdown. Dirty rooms (pending git
 * changes) run first; up to `concurrency` flushes run in parallel so the
 * batch fits inside the default 30s termination grace.
 */
export async function flushAllRooms(
  deps: FlushDeps,
  documents: Map<string, Document>,
  options: FlushAllOptions,
): Promise<void> {
  const { concurrency, force } = options;
  const started = Date.now();
  const pending = new Set<string>();

  const names = [...documents.keys()].filter((name) => roomState(name));
  names.sort((a, b) => {
    const docA = documents.get(a)!;
    const docB = documents.get(b)!;
    const stateA = roomState(a)!;
    const stateB = roomState(b)!;
    const dirtyA = roomHasPendingChanges(docA, stateA, force);
    const dirtyB = roomHasPendingChanges(docB, stateB, force);
    if (dirtyA !== dirtyB) return dirtyA ? -1 : 1;
    return a.localeCompare(b);
  });

  for (const name of names) pending.add(name);

  const slowTimer = setTimeout(() => {
    if (pending.size > 0) {
      deps.log?.(
        `committer: shutdown flush exceeded ${SLOW_SHUTDOWN_FLUSH_MS}ms; ` +
          `pending rooms: ${truncateRoomList([...pending])}`,
      );
    }
  }, SLOW_SHUTDOWN_FLUSH_MS);

  try {
    await runPool(names, concurrency, async (documentName) => {
      const doc = documents.get(documentName);
      const state = roomState(documentName);
      if (!doc || !state) {
        pending.delete(documentName);
        return;
      }
      try {
        await flushRoom(deps, documentName, doc, undefined, force);
      } catch (err) {
        deps.log?.(
          `committer: shutdown flush failed for ${documentName} (${String(err)})`,
        );
      } finally {
        pending.delete(documentName);
      }
    });
  } finally {
    clearTimeout(slowTimer);
    const elapsed = Date.now() - started;
    if (elapsed >= SLOW_SHUTDOWN_FLUSH_MS) {
      deps.log?.(
        `committer: shutdown flush finished in ${elapsed}ms (${names.length} room(s))`,
      );
    }
  }
}
