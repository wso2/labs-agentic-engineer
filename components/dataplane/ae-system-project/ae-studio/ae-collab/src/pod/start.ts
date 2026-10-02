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
 * Composition of the two Room modes: the pod (real IdP, real Files socket)
 * and dev mode (`pnpm dev`: auth bypassed, rooms served from a fake Files
 * socket holding the dev fixtures). Both run the same listeners and the same
 * Room; only who may join and where the files come from differ.
 *
 * Close is the SIGTERM path (07 §10, Q-32): the room listeners stop
 * accepting, every loaded room is force-flushed through the Files socket
 * (bounded, inside ae-studio-tools' drain window), then the room sockets end
 * and the health listener closes.
 */

import { createVerifier } from "@aep/platform-idp-auth";
import { createFilesClient } from "../files-client.js";
import { startFakeFilesSocket } from "../fake-files-socket.js";
import { devSpecFiles } from "../fixtures.js";
import { authenticateFor, devAuthenticate, onTokenSyncFor, type Verify } from "./auth.js";
import type { DevConfig, PodConfig } from "./config.js";
import { createExpiryGuard, systemClock, type Clock } from "./expiry.js";
import { startPodListeners, userGate, type PodListeners } from "./listeners.js";
import { stdoutLog, type PodLog } from "./log.js";
import { createRoomServer, type CommitCadence } from "./room-server.js";

export interface PodDeps {
  log?: PodLog;
  /** Drives the token deadlines; the system clock unless a test steps it. */
  clock?: Clock;
  /** The IdP token check; the cfg's issuer and JWKS unless a test stands one in. */
  verify?: Verify;
  /** The committer's cadence; the Room's default unless a test shortens it. */
  cadence?: Partial<CommitCadence>;
}

/** The AE Studio pod: both listeners, the Room, the user gate. */
export function startPod(cfg: PodConfig, deps: PodDeps = {}): Promise<PodListeners> {
  const log = deps.log ?? stdoutLog;
  // Built before anything binds: a wiring error (empty issuer, JWKS URL or
  // org) fails the start instead of every request.
  const verify: Verify = deps.verify ?? createVerifier({ issuer: cfg.issuer, jwksUrl: cfg.jwksUrl });
  const files = createFilesClient(cfg.filesSocket);
  const expiry = createExpiryGuard(deps.clock ?? systemClock, (connection) =>
    log({ msg: "room_token_expired", source: "ae-collab", listener: connection.context.listener }),
  );
  const room = createRoomServer({
    files,
    authenticate: authenticateFor(cfg, verify, files, log),
    onTokenSync: onTokenSyncFor(cfg, verify, expiry, log),
    expiry,
    log,
    ...(deps.cadence ? { cadence: deps.cadence } : {}),
  });
  return startPodListeners(cfg, { rooms: room.hocuspocus, gate: userGate(cfg, verify), log, drain: room.shutdownFlush });
}

/** Dev mode: never in a cluster (a pod env with `COLLAB_DEV` fails the boot). */
export async function startDev(cfg: DevConfig, deps: Pick<PodDeps, "log"> = {}): Promise<PodListeners> {
  const log = deps.log ?? stdoutLog;
  const fake = await startFakeFilesSocket({
    files: Object.fromEntries(devSpecFiles.map((f) => [f.path, f.content])),
  });
  const files = createFilesClient(fake.path);
  // Dev tokens never expire, so the guard never closes a connection.
  const expiry = createExpiryGuard(systemClock, () => {});
  const room = createRoomServer({ files, authenticate: devAuthenticate, expiry, log });
  let listeners: PodListeners;
  try {
    listeners = await startPodListeners(cfg, {
      rooms: room.hocuspocus,
      gate: () => Promise.resolve(null),
      log,
      drain: room.shutdownFlush,
    });
  } catch (err) {
    await fake.close();
    throw err;
  }
  log({ msg: "pod_dev_mode", source: "ae-collab" });
  return {
    ...listeners,
    async close() {
      try {
        await listeners.close();
      } finally {
        await fake.close();
      }
    },
  };
}
