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
 * The `/v1` edge (07 §1, flow 13): the browser API of the org's design agent,
 * served on the pod's public listener (`pod/listeners.ts`) behind
 * `authenticate`. A user token of the pod's org is the only admission; the
 * gate runs before route matching, so an unknown `/v1` path is 401/403 before
 * it is 404. The routes are thin: the TurnStarter starts turns, the TurnDesk
 * answers their status and stream, the ThreadBook and MarketplaceBook own the
 * conversations.
 *
 *   /v1/projects/{p}/...      project-routes.ts
 *   /v1/marketplace/...       marketplace-routes.ts
 */

import express, { type ErrorRequestHandler, type Express, type RequestHandler, type Response } from "express";
import type { MarketplaceBook } from "../conversations/marketplace-book.js";
import type { ThreadBook } from "../conversations/thread-book.js";
import type { TurnDesk } from "../turns/turn-desk.js";
import type { Credit, TurnStarter } from "../turns/start-turn.js";
import { AuthError, type Authenticate, type AuthenticatedUser } from "./authenticate.js";
import { notFound, sendProblem } from "./http.js";
import { marketplaceRoutes } from "./marketplace-routes.js";
import { projectRoutes } from "./project-routes.js";

/** SSE keep-alive cadence on an attached stream. */
const KEEP_ALIVE_MS = 15_000;

/** One structured, value-free log line. */
export interface EdgeLogLine {
  msg: "pod_request_failed";
  source: "ae-design-agent";
}

export interface EdgeDeps {
  authenticate: Authenticate;
  turns: TurnStarter;
  desk: TurnDesk;
  threads: ThreadBook;
  marketplace: MarketplaceBook;
  /** SSE keep-alive cadence (default 15 s). */
  keepAliveMs?: number;
  log?: (line: EdgeLogLine) => void;
}

/** What the routes share: the deps, the cadence, and the caller. */
export interface RouteContext {
  deps: EdgeDeps;
  keepAliveMs: number;
}

/** The verified caller, set by the gate. */
export function userOf(res: Response): AuthenticatedUser {
  return res.locals.user as AuthenticatedUser;
}

/** The caller as a turn's credit. */
export function creditOf(res: Response): Credit {
  const user = userOf(res);
  return { userId: user.sub, name: user.name, email: user.email };
}

function gate(authenticate: Authenticate): RequestHandler {
  return (req, res, next) => {
    authenticate(req).then(
      (user) => {
        res.locals.user = user;
        next();
      },
      (err: unknown) => {
        if (!(err instanceof AuthError)) return next(err);
        sendProblem(res, err.status, err.code, err.detail, err.headers);
      },
    );
  };
}

/** Never express's default page: no stack, no message. */
function internalError(log: (line: EdgeLogLine) => void): ErrorRequestHandler {
  return (err, _req, res, next) => {
    log({ msg: "pod_request_failed", source: "ae-design-agent" });
    if (res.headersSent) {
      next(err); // mid-response: let express tear the connection down
      return;
    }
    sendProblem(res, 500, "internal", "");
  };
}

const stdoutLog = (line: EdgeLogLine): void => {
  process.stdout.write(`${JSON.stringify(line)}\n`);
};

/** The public app: `/v1` behind the gate, nothing else. */
export function createApp(deps: EdgeDeps): Express {
  const ctx: RouteContext = { deps, keepAliveMs: deps.keepAliveMs ?? KEEP_ALIVE_MS };
  const app = express();
  app.disable("x-powered-by");
  app.use("/v1", gate(deps.authenticate));
  app.use("/v1/projects/:project", projectRoutes(ctx));
  app.use("/v1/marketplace", marketplaceRoutes(ctx));
  app.use((_req, res) => notFound(res, "not_found"));
  app.use(internalError(deps.log ?? stdoutLog));
  return app;
}
