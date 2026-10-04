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
 * `/v1/marketplace/...` (07 §6): conversations with no project, each owned by
 * the user who created it. Every call must come from the owner (`sub`); to
 * anyone else the conversation and its turns do not exist (404). The lock is
 * per conversation, so two users can register at once.
 */

import express, { type Request, type Router } from "express";
import { notFound } from "./http.js";
import { creditOf, userOf, type RouteContext } from "./routes.js";
import { resumeFrom, streamTurn } from "./sse.js";
import { startTurnRoute } from "./start-turn-route.js";

export function marketplaceRoutes(ctx: RouteContext): Router {
  const { deps } = ctx;
  const router = express.Router();

  router.post("/conversations", async (_req, res) => {
    res.status(201).json(await deps.marketplace.create(userOf(res).sub));
  });

  router.get("/conversations/:conversationId/messages", async (req, res) => {
    const messages = await deps.marketplace.history(req.params.conversationId as string, userOf(res).sub);
    if (!messages) {
      notFound(res, "conversation_unknown");
      return;
    }
    res.json({ messages });
  });

  router.post("/conversations/:conversationId/turns", (req, res, next) => {
    if (!deps.marketplace.owns(req.params.conversationId as string, userOf(res).sub)) {
      notFound(res, "conversation_unknown");
      return;
    }
    next();
  });
  router.post(
    "/conversations/:conversationId/turns",
    startTurnRoute((req, res, input) =>
      deps.turns.startMarketplaceTurn({ conversationId: req.params.conversationId as string, input, credit: creditOf(res) }),
    ),
  );

  /** A marketplace turn the caller owns, or `null`. */
  const turnOf = (req: Request, sub: string) => {
    const status = deps.desk.status(req.params.turnId as string);
    return status && status.project === undefined && deps.marketplace.owns(status.conversationId, sub) ? status : null;
  };

  router.get("/turns/:turnId", (req, res) => {
    const status = turnOf(req, userOf(res).sub);
    if (status) res.json(status);
    else notFound(res, "turn_unknown");
  });

  router.get("/turns/:turnId/stream", async (req, res) => {
    if (!turnOf(req, userOf(res).sub)) {
      notFound(res, "turn_unknown");
      return;
    }
    await streamTurn(req, res, deps.desk, req.params.turnId as string, resumeFrom(req), ctx.keepAliveMs);
  });

  return router;
}
