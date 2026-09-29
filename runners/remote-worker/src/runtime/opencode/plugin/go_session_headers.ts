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
 * KIND, either express or implied. See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

export interface ChatHeadersInput {
  sessionID: string;
  model: { providerID: string };
}

export interface ChatHeadersOutput {
  headers: Record<string, string>;
}

/** OpenCode Go routes and caches model calls by the conversation's session ID. */
export function addGoSessionHeaders(
  input: ChatHeadersInput,
  output: ChatHeadersOutput,
  format: string | undefined,
  baseURL: string | undefined,
): void {
  if (format !== "openai-compatible" || input.model.providerID !== "aep" || !baseURL) return;

  let endpoint: URL;
  try {
    endpoint = new URL(baseURL);
  } catch {
    return;
  }
  if (endpoint.protocol !== "https:" || endpoint.hostname !== "opencode.ai" || endpoint.pathname.replace(/\/+$/, "") !== "/zen/go/v1") return;
  if (!input.sessionID) throw new Error("aep-guard: OpenCode Go model request has no session ID");

  output.headers["x-opencode-session"] = input.sessionID;
  output.headers["User-Agent"] = "aep-remote-worker/opencode";
}
