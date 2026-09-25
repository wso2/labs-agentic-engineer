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

/** What one turn to the agent came back as. */
export type TurnResult =
  | { kind: "reply"; conversationId: string; text: string; toolCalls: ToolCall[] }
  /** The gateway or the agent did not accept the token (401). */
  | { kind: "refused" }
  /** The token was accepted but lacks a scope the agent requires (403). */
  | { kind: "forbidden" }
  /** The agent answered, but not with a reply. */
  | { kind: "upstream"; status: number; body: string }
  /** No answer at all: wrong URL, CORS, or the gateway is down. */
  | { kind: "unreachable"; message: string };

export interface ToolCall {
  toolName: string;
}

export interface Turn {
  message: string;
  conversationId?: string;
}

/**
 * One turn to the agent's `/chat`, straight at its gateway URL, as the
 * signed-in test user. The gateway checks the token's audience and scope;
 * the agent reads `x-user-id` off it. Nothing here goes through aep-api.
 */
export async function sendTurn(endpoint: string, token: string, turn: Turn): Promise<TurnResult> {
  let response: Response;
  try {
    response = await fetch(`${endpoint.replace(/\/+$/, "")}/chat`, {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
      body: JSON.stringify(turn.conversationId ? turn : { message: turn.message }),
    });
  } catch (error) {
    return { kind: "unreachable", message: error instanceof Error ? error.message : String(error) };
  }
  if (response.status === 401) return { kind: "refused" };
  if (response.status === 403) return { kind: "forbidden" };
  const body = await response.text();
  if (response.status !== 200) return { kind: "upstream", status: response.status, body };
  let parsed: { conversationId?: string; text?: string; toolCalls?: unknown[] };
  try {
    parsed = JSON.parse(body) as typeof parsed;
  } catch {
    return { kind: "upstream", status: response.status, body };
  }
  return {
    kind: "reply",
    conversationId: parsed.conversationId ?? "",
    text: parsed.text ?? "",
    toolCalls: (parsed.toolCalls ?? []).flatMap((call) => {
      const name = (call as { toolName?: unknown } | null)?.toolName;
      return typeof name === "string" ? [{ toolName: name }] : [];
    }),
  };
}
