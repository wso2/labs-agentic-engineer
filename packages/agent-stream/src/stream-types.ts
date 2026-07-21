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

import type { TurnUsage } from "./contracts/sse-events.js";

/**
 * The minimal structural shape of the stream parts the loop forwards (the seam
 * `runTurn`'s `onEvent`, `change.ts`, and the SSE route are typed against).
 *
 * This is deliberately a small structural subset of the AI SDK's server
 * `TextStreamPart`: it covers the fields the consumers read, so synthetic parts
 * (tests, captured fixtures) satisfy it without reconstructing the full provider
 * union — and keeps this package free of any AI-SDK dependency. `tool-input-delta`
 * carries `.delta` (the server part), NOT `.inputTextDelta` (the UI chunk).
 */
export interface StreamPart {
  type: string;
  id?: string;
  delta?: string;
  text?: string;
  toolName?: string;
  toolCallId?: string;
  input?: unknown;
  output?: unknown;
  error?: unknown;
  /** On `finish` / `finish-step` frames: why the step ended (`stop`, `length`, …). */
  finishReason?: string;
  /**
   * Manifest-part fields (`type: "manifest"`, D14): path → sha256 hex of the
   * final content for every path mutated this turn and still present.
   */
  files?: Record<string, string>;
  /** Manifest-part field: touched paths no longer present at turn end. */
  deleted?: string[];
  /** Manifest-part field (#249): the turn's token usage + resolved model id. */
  usage?: TurnUsage;
}
