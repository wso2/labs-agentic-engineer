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
 * The turn start route both surfaces share: read the body (`turn-input.ts`),
 * start the turn, answer `202 {turnId}`. Refusals map to their status.
 */

import type { Request, RequestHandler, Response } from "express";
import { TurnStartError, type TurnInput } from "../turns/start-turn.js";
import { sendProblem, sendTurnStartError } from "./http.js";
import { InputError, readTurnInput } from "./turn-input.js";

export function startTurnRoute(
  start: (req: Request, res: Response, input: TurnInput) => Promise<string>,
): RequestHandler {
  return async (req, res) => {
    let input: TurnInput;
    try {
      input = await readTurnInput(req);
    } catch (err) {
      if (!(err instanceof InputError)) throw err;
      sendProblem(res, err.status, err.code, err.message);
      return;
    }
    try {
      res.status(202).json({ turnId: await start(req, res, input) });
    } catch (err) {
      if (!(err instanceof TurnStartError)) throw err;
      sendTurnStartError(res, err);
    }
  };
}
