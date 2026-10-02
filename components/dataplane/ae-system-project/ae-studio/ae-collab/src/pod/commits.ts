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
 */

import type {
  beforeUnloadDocumentPayload,
  Document,
  onStatelessPayload,
  onStoreDocumentPayload,
} from "@hocuspocus/server";
import { flushRoom, type FlushDeps } from "../committer.js";
import { FilesUnavailableError, type FilesClient } from "../files-client.js";
import type { PodLog } from "./log.js";

/** What the room is told when a flush meets an outage. */
export const RESTARTING = "AE Studio is restarting — your edits are kept and will save shortly.";

export interface CommitHookDeps {
  files: FilesClient;
  log: PodLog;
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
  return {
    onStoreDocument: async ({ document, documentName }: Pick<onStoreDocumentPayload, "document" | "documentName">) => {
      try {
        await flushRoom(flushDepsFor(deps, document), documentName, document);
      } catch (err) {
        // Never thrown on: Hocuspocus would print it, and the doc stays live anyway.
        document.broadcastStateless(JSON.stringify({ type: "flush-error", message: flushErrorMessage(err) }));
      }
    },

    beforeUnloadDocument: async ({ document, documentName }: Pick<beforeUnloadDocumentPayload, "document" | "documentName">) => {
      try {
        await flushRoom(flushDepsFor(deps, document), documentName, document, true);
      } catch (err) {
        // A verdict will not change on a retry: the room unloads. An outage
        // will: a throw keeps the doc loaded (Hocuspocus skips the unload),
        // so a rejoin finds the edits and the shutdown flush retries them.
        // The empty message keeps Hocuspocus from printing it.
        if (err instanceof FilesUnavailableError) {
          deps.log({ msg: "room_final_flush_deferred", source: "ae-collab" });
          throw new Error("");
        }
      }
    },

    onStateless: async ({ connection, document, documentName, payload }: Pick<onStatelessPayload, "connection" | "document" | "documentName" | "payload">) => {
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
  };
}
