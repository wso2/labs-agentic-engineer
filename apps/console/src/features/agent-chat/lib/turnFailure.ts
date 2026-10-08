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

// The sentence a failed agent turn shows in the chat. A failure the design
// agent could name (TurnStatus.code — the model provider's usage limit, a
// write the output limit cut off) is phrased from its fields, so a provider
// limit says whose plan is spent and when to try again, in the reader's own
// time zone; anything else shows the message the platform recorded. Copied
// from the old console's agent-chat/lib/turnFailure.ts.

import type { components } from "../../../generated/ae-design-agent";
import { resetStamp } from "../../../lib/stamp";

type TurnStatus = components["schemas"]["TurnStatus"];

/** What a turn failure says about itself: the status read, or the `turn-failed` event. */
export type TurnFailure = Pick<TurnStatus, "code" | "host" | "resetAt" | "message">;

export function turnFailureText(f: TurnFailure, now: Date = new Date()): string {
  if (f.code === "provider_limit") {
    const whose = f.host ? `${f.host}'s` : "The model provider's";
    const when = f.resetAt ? `Try again after ${resetStamp(f.resetAt, now)}.` : "Try again later.";
    return `${whose} usage limit is reached. ${when}`;
  }
  // output_truncated: the design agent's own sentence already names the
  // limit and the file, and there is no time in it to localise.
  return f.message || "The agent turn failed.";
}
