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

/** Response helpers every `/v1` route shares: problems and the TurnConflict body. */

import type { Response } from "express";
import { problem } from "@aep/platform-idp-auth";
import { TurnStartError } from "../turns/start-turn.js";

/** An RFC 9457 problem. `detail` is a fixed sentence or a message naming no secret. */
export function sendProblem(
  res: Response,
  status: number,
  code: string,
  detail: string,
  headers: Record<string, string> = {},
): void {
  const p = problem(status, code, detail);
  res.writeHead(p.status, { ...headers, "content-type": "application/problem+json" }).end(JSON.stringify(p.body));
}

/** The contract's 404, by what was not found. */
export function notFound(res: Response, code: "project_unknown" | "conversation_unknown" | "turn_unknown" | "not_found"): void {
  const detail = {
    project_unknown: "no such project",
    conversation_unknown: "no such conversation",
    turn_unknown: "no such turn",
    not_found: "no such route",
  }[code];
  sendProblem(res, 404, code, detail);
}

/** Seconds a 503 asks the client to wait. */
const RETRY_AFTER_SECONDS = "5";

/**
 * A refused turn start. `turn_in_progress` and `conversation_rotated` are the
 * contract's plain-JSON TurnConflict (the console reads them by code); every
 * other refusal is a problem.
 */
export function sendTurnStartError(res: Response, err: TurnStartError): void {
  if (err.code === "turn_in_progress" || err.code === "conversation_rotated") {
    res.status(409).json({ code: err.code, ...(err.activeTurnId ? { activeTurnId: err.activeTurnId } : {}) });
    return;
  }
  sendProblem(res, err.status, err.code, err.message, err.status === 503 ? { "retry-after": RETRY_AFTER_SECONDS } : {});
}
