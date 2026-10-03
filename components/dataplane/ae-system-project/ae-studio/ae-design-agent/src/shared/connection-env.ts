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
 * The pod's model connection (07 §4): the organization's one connection,
 * rendered into the pod env by aep-api (`A/organization/aestudio/desired.go`).
 *
 * - `AE_MODEL_CONNECTION`: JSON, the `TurnConnection` wire shape plus
 *   `model` (`{format, baseURL, authScheme, contextWindow?, outputLimit?,
 *   capabilities, model}`), `""` when the org has no connection.
 * - `ANTHROPIC_API_KEY`: the Default key secret, absent when the org has none.
 *
 * Both or nothing: aep-api resolves the key and the connection together and
 * has no platform fallback, so either one missing is `null`, which a turn
 * answers as `no_default_key`. The pod starts without them.
 */

import { isTurnConnection } from "@aep/agent-stream";
import { connectionFromWire, isModelId, type ModelConnection } from "./model.js";

/** `AE_MODEL_CONNECTION` is set but is not a connection. Never quotes the value. */
export class ConnectionEnvError extends Error {
  constructor(reason: string) {
    super(`AE_MODEL_CONNECTION ${reason}`);
    this.name = "ConnectionEnvError";
  }
}

type Env = Readonly<Record<string, string | undefined>>;

/**
 * The connection turns run on, or `null` when the org has no key or no
 * connection (`no_default_key`). Throws `ConnectionEnvError` when
 * `AE_MODEL_CONNECTION` is set but malformed: a rendering fault, not a
 * missing key.
 */
export function connectionFromEnv(env: Env): ModelConnection | null {
  const apiKey = env.ANTHROPIC_API_KEY?.trim() ?? "";
  const raw = env.AE_MODEL_CONNECTION?.trim() ?? "";
  if (apiKey === "" || raw === "") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new ConnectionEnvError("is not JSON");
  }
  if (!isTurnConnection(parsed)) {
    throw new ConnectionEnvError(
      "is not { format, baseURL (https), authScheme, contextWindow?, outputLimit?, capabilities, model }",
    );
  }
  const model = (parsed as { model?: unknown }).model;
  if (typeof model !== "string" || !isModelId(model)) throw new ConnectionEnvError("has no model id");
  return connectionFromWire(parsed, apiKey, model);
}
