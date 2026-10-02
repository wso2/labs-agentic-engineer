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
 * The pod-mode listeners (07 §8, 08 §2). The public port serves `/v1` behind
 * the user gate: a Platform IdP user token of the pod's org, never an M2M
 * token. The gate runs before route matching, so an unknown `/v1` path is
 * 401/403 before it is 404. Nothing else is served there. The health port
 * (not in the Service, not routed) serves `/healthz` (liveness) and
 * `/readyz` (200 once the public port is bound).
 */

import type { Server } from "node:http";
import type { AddressInfo } from "node:net";
import express, { type ErrorRequestHandler, type RequestHandler, type Response } from "express";
import type { JWTVerifyGetKey } from "jose";
import { createVerifier, IdpUnavailableError, problem, UnauthenticatedError, userRule } from "@aep/platform-idp-auth";
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
  /** Replaces the IdP's remote JWKS (tests). */
  jwks?: JWTVerifyGetKey;
  /** Where log lines go; stdout unless a test captures them. */
  log?: (line: PodLogLine) => void;
}

const stdoutLog = (line: PodLogLine): void => {
  process.stdout.write(`${JSON.stringify(line)}\n`);
};

const BEARER = /^Bearer ([^\s]+)$/i;

function sendProblem(res: Response, status: number, code: string, detail: string): void {
  const p = problem(status, code, detail);
  res.writeHead(p.status, { "content-type": "application/problem+json" }).end(JSON.stringify(p.body));
}

/** The `/v1` user gate. Details are fixed sentences: no token or claim value. */
function userGate(cfg: PodConfig, deps: PodListenerDeps): RequestHandler {
  const verify = createVerifier({
    issuer: cfg.issuer,
    jwksUrl: cfg.jwksUrl,
    ...(deps.jwks ? { jwks: deps.jwks } : {}),
  });
  const kinds = [{ name: "user" as const, audiences: cfg.userAudiences }];
  const pod = { orgId: cfg.orgId, orgHandle: cfg.orgHandle };
  return (req, res, next) => {
    const token = BEARER.exec(req.headers.authorization ?? "")?.[1];
    if (!token) {
      res.setHeader("www-authenticate", "Bearer");
      sendProblem(res, 401, "unauthenticated", "a bearer token is required");
      return;
    }
    verify(token, kinds).then(
      (verified) => {
        // Only the user kind is offered, so anything else is a wiring fault.
        if (verified.kind !== "user") throw new Error("pod gate: unexpected token kind");
        if (!userRule(verified.claims, pod)) {
          sendProblem(res, 403, "org_mismatch", "the token is not for this organization");
          return;
        }
        next();
      },
      (err: unknown) => {
        if (err instanceof IdpUnavailableError) {
          // No verdict on the token: the IdP's keys could not be fetched.
          res.setHeader("retry-after", "5");
          sendProblem(res, 503, "idp_unavailable", "the identity provider cannot be reached");
          return;
        }
        if (!(err instanceof UnauthenticatedError)) return next(err);
        res.setHeader("www-authenticate", 'Bearer error="invalid_token"');
        sendProblem(res, 401, "unauthenticated", "the bearer token is not valid here");
      },
    ).catch(next);
  };
}

const notFound: RequestHandler = (_req, res) => sendProblem(res, 404, "not_found", "no such route");

/** Never express's default page: no stack, no message. */
function internalError(log: (line: PodLogLine) => void): ErrorRequestHandler {
  return (err, _req, res, next) => {
    log({ msg: "pod_request_failed", source: "ae-design-agent" });
    if (res.headersSent) {
      next(err); // mid-response: let express tear the connection down
      return;
    }
    sendProblem(res, 500, "internal", "");
  };
}

function publicApp(cfg: PodConfig, deps: PodListenerDeps, log: (line: PodLogLine) => void): express.Express {
  const app = express();
  app.disable("x-powered-by");
  app.use("/v1", userGate(cfg, deps));
  // No /v1 operations yet: every admitted request is 404.
  app.use("/v1", notFound);
  app.use(notFound);
  app.use(internalError(log));
  return app;
}

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
  app.use(notFound);
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

function closeServer(server: Server): Promise<void> {
  return new Promise((resolve, reject) => server.close((err) => (err ? reject(err) : resolve())));
}

function urlOf(server: Server): string {
  return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
}

/**
 * Starts both listeners. Refuses to start on a Secret the pod was not
 * rendered for (08 §7), so kubelet restarts the container until ESO has
 * refreshed it. The revisions are hashes of reference names, not secrets.
 */
export async function startPodListeners(cfg: PodConfig, deps: PodListenerDeps = {}): Promise<PodListeners> {
  if (cfg.expectedSecretRev !== cfg.secretRev) throw new Error("secret revision mismatch");
  const log = deps.log ?? stdoutLog;
  const line = (msg: PodLogLine["msg"], server?: Server): PodLogLine => ({
    msg,
    source: "ae-design-agent",
    ...(server ? { port: (server.address() as AddressInfo).port } : {}),
  });
  let ready = false;
  const pub = publicApp(cfg, deps, log);
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
      await closeServer(publicServer);
      await closeServer(health);
      log(line("pod_listeners_stopped"));
    },
  };
}
