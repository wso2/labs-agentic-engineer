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
 * The `error` frame a failed turn ends with. A failure the service can name
 * (`TurnErrorPart`: the provider's usage limit, a write the output limit cut
 * off) gets its coded frame, so a reader is told what happened and when to
 * retry instead of seeing the provider's raw error; anything else keeps the
 * plain `{type: "error", error: <message>}` frame.
 */

import type { TurnErrorPart } from "@aep/agent-stream";
import { providerLimitIn } from "../shared/provider-limit.js";
import { OutputTruncatedError } from "./truncation.js";

export type TurnErrorFrame = TurnErrorPart | { type: "error"; error: string };

/** The coded frame for `err`, if the turn failed for a reason the service can name. */
export function codedErrorFrame(err: unknown, host: string): TurnErrorPart | undefined {
  const limit = providerLimitIn(err, host);
  if (limit) {
    return {
      type: "error",
      code: "provider_limit",
      error: limit.message,
      host: limit.host,
      ...(limit.resetAt ? { resetAt: limit.resetAt.toISOString() } : {}),
    };
  }
  if (err instanceof OutputTruncatedError) {
    return {
      type: "error",
      code: "output_truncated",
      error: err.message,
      toolName: err.call.toolName,
      ...(err.call.path !== undefined ? { path: err.call.path } : {}),
    };
  }
  return undefined;
}

/** The frame a turn that threw ends with. */
export function turnErrorFrame(err: unknown, host: string): TurnErrorFrame {
  return codedErrorFrame(err, host) ?? { type: "error", error: err instanceof Error ? err.message : String(err) };
}
