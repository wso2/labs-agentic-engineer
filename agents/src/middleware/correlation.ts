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

import type { Request, Response, NextFunction, RequestHandler } from "express";
import { randomUUID } from "node:crypto";

export const CORRELATION_HEADER = "x-correlation-id";

/**
 * Reads X-Correlation-ID from the request, generating a UUID if missing.
 * Echoes it back on the response and attaches to res.locals.correlationId
 * so handlers and outbound clients can pick it up.
 */
export function correlationIdMiddleware(): RequestHandler {
  return function correlation(req: Request, res: Response, next: NextFunction): void {
    let id = req.header(CORRELATION_HEADER);
    if (!id || id.length === 0 || id.length > 128) {
      id = randomUUID();
    }
    res.setHeader(CORRELATION_HEADER, id);
    (res.locals as Record<string, unknown>).correlationId = id;
    next();
  };
}

/** Convenience getter — returns the correlation ID for the current request. */
export function getCorrelationId(res: Response): string {
  const v = (res.locals as Record<string, unknown>).correlationId;
  return typeof v === "string" ? v : "-";
}
