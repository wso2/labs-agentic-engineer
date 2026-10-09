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
 * The pod's listeners. The public port serves the `/v1`
 * edge (`edge/routes.ts`) behind the Platform IdP adapter of `authenticate`:
 * a user token of the pod's org, never an M2M token. Nothing else is served
 * there. The Turn socket (`edge/turn-socket.ts`) is a Unix socket on the
 * emptyDir shared with ae-studio-tools only, mode 0660: the mount is its
 * gate. The health port (not in the Service, not routed) serves `/healthz`
 * (liveness) and `/readyz` (200 once the public port and the Turn socket are
 * bound).
 */

import { chmodSync, lstatSync, unlinkSync } from "node:fs";
import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import express from "express";
import type { JWTVerifyGetKey } from "jose";
import { idpAuthenticate } from "../edge/authenticate.js";
import { createApp, type EdgeDeps } from "../edge/routes.js";
import { notFound } from "../edge/http.js";
import { createTurnSocketApp } from "../edge/turn-socket.js";
import type { PodConfig } from "./config.js";

export interface PodListeners {
  publicUrl: string;
  healthUrl: string;
  /** Stop accepting, let ended responses drain for up to `graceMs` (default 1 s), then cut what is left. Idempotent. */
  close(graceMs?: number): Promise<void>;
}

/** One structured, value-free log line. */
export interface PodLogLine {
  msg: "pod_health_listening" | "pod_public_listening" | "pod_turn_socket_listening" | "pod_listeners_stopped" | "pod_request_failed";
  source: "ae-design-agent";
  port?: number;
}

export interface PodListenerDeps {
  /**
   * The `/v1` edge's services; the pod supplies `authenticate`. The Turn
   * socket shares its starter, desk and keep-alive cadence.
   */
  edge: Omit<EdgeDeps, "authenticate" | "log">;
  /** Replaces the IdP's remote JWKS (tests). */
  jwks?: JWTVerifyGetKey;
  /** Where log lines go; stdout unless a test captures them. */
  log?: (line: PodLogLine) => void;
}

const stdoutLog = (line: PodLogLine): void => {
  process.stdout.write(`${JSON.stringify(line)}\n`);
};

function healthApp(ready: () => boolean): express.Express {
  const app = express();
  app.disable("x-powered-by");
  app.get("/healthz", (_req, res) => {
    res.type("text/plain").send("OK\n");
  });
  app.get("/readyz", (_req, res) => {
    const ok = ready();
    res.status(ok ? 200 : 503).type("text/plain").send(ok ? "OK\n" : "Service Unavailable\n");
  });
  app.use((_req, res) => notFound(res, "not_found"));
  return app;
}

/** The wait for ended responses to leave before their connections are cut. */
const CLOSE_GRACE_MS = 1_000;
/** The Turn socket's mode: the pod's shared group (fsGroup) may connect. */
const SOCKET_MODE = 0o660;

function listen(app: express.Express, at: number | string): Promise<Server> {
  return new Promise((resolve, reject) => {
    const server = app.listen(at);
    server.once("listening", () => {
      server.off("error", reject);
      resolve(server);
    });
    server.once("error", reject);
  });
}

/**
 * Removes a socket file a previous run left at `path` (the emptyDir outlives a
 * container restart); nothing there is fine, and any other file is an error,
 * never removed (as ae-studio-tools' `ListenSocket`).
 */
function removeStaleSocket(path: string): void {
  let isSocket: boolean;
  try {
    isSocket = lstatSync(path).isSocket();
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return;
    throw err;
  }
  if (!isSocket) throw new Error(`turn socket ${path}: exists and is not a socket`);
  unlinkSync(path);
}

/** Binds the Turn socket at `path`, mode 0660. Closing the server unlinks it. */
async function listenSocket(app: express.Express, path: string): Promise<Server> {
  removeStaleSocket(path);
  const server = await listen(app, path);
  try {
    chmodSync(path, SOCKET_MODE);
  } catch (err) {
    await closeServer(server, 0);
    throw err;
  }
  return server;
}

/**
 * Stops accepting and closes idle connections; after `graceMs` ends what is
 * still open: an attached turn stream would otherwise hold `close` until its
 * turn ends. The grace lets a response that has just ended (a turn's last
 * line) leave before its connection is cut.
 */
function closeServer(server: Server, graceMs: number): Promise<void> {
  const closed = new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
  server.closeIdleConnections();
  if (graceMs <= 0) {
    server.closeAllConnections();
    return closed;
  }
  const cut = setTimeout(() => server.closeAllConnections(), graceMs);
  return closed.finally(() => clearTimeout(cut));
}

function urlOf(server: Server): string {
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
}

/**
 * Starts both listeners. Refuses to start on a Secret the pod was not
 * rendered for, so kubelet restarts the container until ESO has
 * refreshed it. The revisions are hashes of reference names, not secrets.
 */
export async function startPodListeners(cfg: PodConfig, deps: PodListenerDeps): Promise<PodListeners> {
  if (cfg.expectedSecretRev !== cfg.secretRev) throw new Error("secret revision mismatch");
  const log = deps.log ?? stdoutLog;
  const line = (msg: PodLogLine["msg"], server?: Server): PodLogLine => ({
    msg,
    source: "ae-design-agent",
    ...(server ? { port: (server.address() as AddressInfo).port } : {}),
  });
  let ready = false;
  const pub = createApp({
    ...deps.edge,
    authenticate: idpAuthenticate({
      issuer: cfg.issuer,
      jwksUrl: cfg.jwksUrl,
      userAudiences: cfg.userAudiences,
      orgId: cfg.orgId,
      orgHandle: cfg.orgHandle,
      ...(deps.jwks ? { jwks: deps.jwks } : {}),
    }),
    log,
  });
  const turnApp = createTurnSocketApp({
    turns: deps.edge.turns,
    desk: deps.edge.desk,
    ...(deps.edge.keepAliveMs ? { keepAliveMs: deps.edge.keepAliveMs } : {}),
  });
  const health = await listen(healthApp(() => ready), cfg.healthPort);
  log(line("pod_health_listening", health));
  let publicServer: Server;
  let turnServer: Server;
  try {
    publicServer = await listen(pub, cfg.listenPort);
  } catch (err) {
    await closeServer(health, 0);
    throw err;
  }
  log(line("pod_public_listening", publicServer));
  try {
    turnServer = await listenSocket(turnApp, cfg.turnSocket);
  } catch (err) {
    await Promise.all([closeServer(publicServer, 0), closeServer(health, 0)]);
    throw err;
  }
  log({ msg: "pod_turn_socket_listening", source: "ae-design-agent" });
  ready = true;
  let closing: Promise<void> | undefined;
  const close = async (graceMs: number): Promise<void> => {
    ready = false;
    try {
      await Promise.all([closeServer(publicServer, graceMs), closeServer(turnServer, graceMs)]);
    } finally {
      // The health port goes too, whatever the other closes did.
      await closeServer(health, 0);
    }
    log(line("pod_listeners_stopped"));
  };
  return {
    publicUrl: urlOf(publicServer),
    healthUrl: urlOf(health),
    // Once: a second call waits for the first.
    close: (graceMs = CLOSE_GRACE_MS) => (closing ??= close(graceMs)),
  };
}
