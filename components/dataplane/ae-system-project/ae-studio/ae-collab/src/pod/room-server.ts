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
 * The Room's Hocuspocus instance (07 §11): no server and no port of its own;
 * the listeners hand it sockets (`listeners.ts`). It authenticates per
 * listener (`auth.ts`), holds each connection to its token's `exp`
 * (`expiry.ts`), and seeds a room from the Files socket's bundle.
 *
 * A ROOM EXISTS ONLY IF IT WAS SEEDED (#586): a bundle read that fails refuses
 * the load, so no client ever sees an empty document that is not an empty
 * project. A Files verdict (4xx) is a plain refusal; an outage is tagged
 * `upstream-unavailable` so the client retries. It commits through the same
 * socket (`commits.ts`): debounced, on the last leave and on a `flush`.
 */

import { Hocuspocus, type Connection, type onAuthenticatePayload, type onTokenSyncPayload } from "@hocuspocus/server";
import { seedBaseline } from "../committer.js";
import { FilesDeniedError, type FilesClient } from "../files-client.js";
import { dropRoomState, ensureRoomState } from "../rooms.js";
import { isReferenceDocPath, seedDocument } from "../seed.js";
import { PERMISSION_DENIED, refusal, UPSTREAM_UNAVAILABLE, type CollabContext } from "./auth.js";
import { commitHooks } from "./commits.js";
import type { ExpiryGuard } from "./expiry.js";
import type { PodLog } from "./log.js";

/** The committer's cadence: a quiet period commits, continuous editing commits at least this often. */
export interface CommitCadence {
  debounceMs: number;
  maxDebounceMs: number;
}

const COMMIT_CADENCE: CommitCadence = { debounceMs: 60_000, maxDebounceMs: 300_000 };

export interface RoomServerDeps {
  files: FilesClient;
  /** `authenticateFor(...)` in the pod, `devAuthenticate` in dev mode. */
  authenticate: (data: onAuthenticatePayload<CollabContext>) => Promise<CollabContext>;
  /** `onTokenSyncFor(...)`; dev mode reads no token, so it has none. */
  onTokenSync?: (data: Pick<onTokenSyncPayload<CollabContext>, "token" | "connection">) => Promise<void>;
  expiry: ExpiryGuard;
  log: PodLog;
  /** COMMIT_CADENCE unless a test shortens it. */
  cadence?: CommitCadence;
}

export function createRoomServer(deps: RoomServerDeps): Hocuspocus<CollabContext> {
  const cadence = deps.cadence ?? COMMIT_CADENCE;
  return new Hocuspocus<CollabContext>({
    name: "ae-collab",
    // A doc's life is its room's life; git is the durable truth and a rejoin
    // reseeds from HEAD. The last leave runs the pending store, then unloads.
    unloadImmediately: true,
    debounce: cadence.debounceMs,
    maxDebounce: cadence.maxDebounceMs,
    ...commitHooks({ files: deps.files, log: deps.log }),
    onAuthenticate: deps.authenticate,
    ...(deps.onTokenSync ? { onTokenSync: deps.onTokenSync } : {}),
    connected: ({ connection, context }) => {
      deps.expiry.arm(connection as Connection, context.exp);
      return Promise.resolve();
    },
    onLoadDocument: async ({ document, documentName, context }) => {
      // The auth hook always resolves a project; a context without one is a wiring fault.
      if (!context.projectName) throw refusal(PERMISSION_DENIED);
      try {
        // Reference documents never enter the room, not seeded, not baselined.
        const files = (await deps.files.bundle(context.projectName)).filter((f) => !isReferenceDocPath(f.path));
        // An anomaly is never fatal (the content still lands), and never silent.
        seedDocument(document, files, () =>
          deps.log({ msg: "room_seed_anomaly", source: "ae-collab", listener: context.listener }),
        );
        // The committer's baseline: what was seeded, and the shas it preconditions on.
        seedBaseline(ensureRoomState(documentName, context.projectName), document, files);
      } catch (err) {
        // No room exists after a refused load: no other connection holds this
        // document, so its state goes too, the participants added at auth
        // included (a refused joiner must never be credited by a later commit).
        dropRoomState(documentName);
        // Hocuspocus (4.3) never destroys a document whose load failed: it is
        // not in `documents` yet, so its unload returns early and the doc's
        // awareness timer runs for the life of the process, one per refusal.
        document.destroy();
        deps.log({ msg: "room_seed_failed", source: "ae-collab", listener: context.listener });
        throw refusal(err instanceof FilesDeniedError ? PERMISSION_DENIED : UPSTREAM_UNAVAILABLE);
      }
      return document;
    },
    afterUnloadDocument: ({ documentName }) => {
      dropRoomState(documentName);
      return Promise.resolve();
    },
  });
}
