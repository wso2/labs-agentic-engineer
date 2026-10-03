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
 * The pod's listeners (07 §8/§9, 08 §2). The public port serves the `/v1`
 * edge (`edge/routes.ts`) behind the Platform IdP adapter of `authenticate`:
 * a user token of the pod's org, never an M2M token. Nothing else is served
 * there. The health port (not in the Service, not routed) serves `/healthz`
 * (liveness) and `/readyz` (200 once the public port is bound).
 */

import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import express from "express";
import type { JWTVerifyGetKey } from "jose";
import { idpAuthenticate } from "../edge/authenticate.js";
import { createApp, type EdgeDeps } from "../edge/routes.js";
import { notFound } from "../edge/http.js";
import type { PodConfig } from "./config.js";

export interface PodListeners {
  publicUrl: string;
  healthUrl: string;
  close(): Promise<void>;
}

/** One structured, value-free log line. */
export interface PodLogLine {
  msg: "pod_health_listening" | "pod_public_listening" | "pod_listeners_stopped" | "pod_request_failed";
  source: "ae-design-agent";
  port?: number;
}

export interface PodListenerDeps {
  /** The `/v1` edge's services; the pod supplies `authenticate`. */
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

function listen(app: express.Express, port: number): Promise<Server> {
  return new Promise((resolve, reject) => {
    const server = app.listen(port);
    server.once("listening", () => {
      server.off("error", reject);
      resolve(server);
    });
    server.once("error", reject);
  });
}

/**
 * Stops accepting, then ends open connections: an attached turn stream
 * would otherwise hold `close` until its turn ends.
 */
function closeServer(server: Server): Promise<void> {
  const closed = new Promise<void>((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
  server.closeAllConnections();
  return closed;
}

function urlOf(server: Server): string {
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
}

/**
 * Starts both listeners. Refuses to start on a Secret the pod was not
 * rendered for (08 §7), so kubelet restarts the container until ESO has
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
  const health = await listen(healthApp(() => ready), cfg.healthPort);
  log(line("pod_health_listening", health));
  let publicServer: Server;
  try {
    publicServer = await listen(pub, cfg.listenPort);
  } catch (err) {
    await closeServer(health);
    throw err;
  }
  log(line("pod_public_listening", publicServer));
  ready = true;
  return {
    publicUrl: urlOf(publicServer),
    healthUrl: urlOf(health),
    async close() {
      ready = false;
      try {
        await closeServer(publicServer);
      } finally {
        // The health port goes too, whatever the public close did.
        await closeServer(health);
      }
      log(line("pod_listeners_stopped"));
    },
  };
}
