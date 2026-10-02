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
 * When a Room commits (07 §11), as Hocuspocus hooks over the committer:
 *
 *   onStoreDocument       the debounced flush: a quiet period, the max age,
 *                         and the last leave all funnel here. Interim: files
 *                         with pending agent marks are held.
 *   beforeUnloadDocument  the forced session-end flush (accept-by-default).
 *   onStateless `flush`   the console's flush-before-build (#162): forced,
 *                         acked `flushed` or `flush-error` to the asker.
 *
 * A failed flush never closes a connection and never drops the doc: the next
 * flush retries from the same baseline. The room hears about it as a
 * stateless `flush-error`; an outage (`disk_full`, `aep_api_unavailable`, a
 * lost push race, a sidecar that is restarting) reads as RESTARTING. After
 * every commit the room hears the commit's warnings as `flush-warnings`.
 *
 * A last-leave flush that fails for any reason but a verdict keeps the room
 * loaded with nobody in it, so nothing would ever flush it again: it is
 * retried on a backoff (`DeferredRetry`) until it lands (then the room
 * unloads), someone rejoins, the room unloads, or shutdown stops the retries.
 */

import type {
  beforeUnloadDocumentPayload,
  Document,
  Hocuspocus,
  onStatelessPayload,
  onStoreDocumentPayload,
} from "@hocuspocus/server";
import { flushRoom, type FlushDeps } from "../committer.js";
import { FilesDeniedError, FilesUnavailableError, type FilesClient } from "../files-client.js";
import type { PodLog } from "./log.js";

/** What the room is told when a flush meets an outage. */
export const RESTARTING = "AE Studio is restarting — your edits are kept and will save shortly.";

/** The deferred final flush's backoff: `firstMs`, doubling, capped at `maxMs`. */
export interface DeferredRetry {
  firstMs: number;
  maxMs: number;
}

export interface CommitHookDeps {
  files: FilesClient;
  log: PodLog;
  retry: DeferredRetry;
}

function flushErrorMessage(err: unknown): string {
  if (err instanceof FilesUnavailableError) return RESTARTING;
  return err instanceof Error ? err.message : "flush failed";
}

function flushDepsFor(deps: CommitHookDeps, document: Document): FlushDeps {
  return {
    files: deps.files,
    log: deps.log,
    onWarnings: (warnings) => document.broadcastStateless(JSON.stringify({ type: "flush-warnings", warnings })),
  };
}

export function commitHooks(deps: CommitHookDeps) {
  const retries = new Map<string, NodeJS.Timeout>();
  let stopped = false;

  const cancelRetry = (documentName: string): void => {
    clearTimeout(retries.get(documentName));
    retries.delete(documentName);
  };

  /** Retries the forced flush of an empty, still-loaded room after `delayMs`. */
  const scheduleRetry = (instance: Hocuspocus, document: Document, delayMs: number): void => {
    cancelRetry(document.name);
    if (stopped) return;
    const timer = setTimeout(() => {
      retries.delete(document.name);
      // A rejoin or an unload took the room over.
      if (document.getConnectionsCount() > 0 || instance.documents.get(document.name) !== document) return;
      flushRoom(flushDepsFor(deps, document), document.name, document, true).then(
        // Landed: the room may unload now (its final flush finds nothing left).
        () => void instance.unloadDocument(document),
        (err: unknown) => {
          if (err instanceof FilesDeniedError) void instance.unloadDocument(document);
          else scheduleRetry(instance, document, Math.min(delayMs * 2, deps.retry.maxMs));
        },
      );
    }, delayMs);
    // Never what keeps the process alive: shutdown flushes loaded rooms itself.
    timer.unref();
    retries.set(document.name, timer);
  };

  return {
    /** A rejoin or an unload ends the room's retries. */
    cancelRetry,

    /** Shutdown: no retry starts after this; the shutdown flush covers every loaded room. */
    stopRetries(): void {
      stopped = true;
      for (const name of [...retries.keys()]) cancelRetry(name);
    },

    hooks: {
      onStoreDocument: async ({ document, documentName }: Pick<onStoreDocumentPayload, "document" | "documentName">) => {
        try {
          await flushRoom(flushDepsFor(deps, document), documentName, document);
        } catch (err) {
          // Never thrown on: Hocuspocus would print it, and the doc stays live anyway.
          document.broadcastStateless(JSON.stringify({ type: "flush-error", message: flushErrorMessage(err) }));
        }
      },

      beforeUnloadDocument: async ({
        instance,
        document,
        documentName,
      }: Pick<beforeUnloadDocumentPayload, "instance" | "document" | "documentName">) => {
        try {
          await flushRoom(flushDepsFor(deps, document), documentName, document, true);
        } catch (err) {
          // Only a verdict lets the room unload with its edits: it will not
          // change on a retry. Anything else (an outage, conflicts that kept
          // coming, a fault of ours) keeps the doc loaded: a throw makes
          // Hocuspocus skip the unload, and the retry takes it from there.
          // The empty message keeps Hocuspocus from printing it.
          if (err instanceof FilesDeniedError) return;
          deps.log({ msg: "room_final_flush_deferred", source: "ae-collab" });
          scheduleRetry(instance, document, deps.retry.firstMs);
          throw new Error("");
        }
      },

      onStateless: async ({
        connection,
        document,
        documentName,
        payload,
      }: Pick<onStatelessPayload, "connection" | "document" | "documentName" | "payload">) => {
        let msg: { type?: unknown; id?: unknown };
        try {
          msg = JSON.parse(payload) as typeof msg;
        } catch {
          return; // not our protocol
        }
        if (msg === null || typeof msg !== "object" || msg.type !== "flush") return;
        const id = typeof msg.id === "string" ? msg.id : undefined;
        try {
          await flushRoom(flushDepsFor(deps, document), documentName, document, true);
          connection.sendStateless(JSON.stringify({ type: "flushed", id }));
        } catch (err) {
          connection.sendStateless(JSON.stringify({ type: "flush-error", id, message: flushErrorMessage(err) }));
        }
      },
    },
  };
}
