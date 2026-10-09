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

// MCP tools the runner answers itself, inside the loopback MCP proxy.
//
// The platform's MCP server is one URL to a runtime (`McpPolicy.url`), but
// not every tool on it has to be served by `aep-api`: a tool that needs a
// credential only this Job holds (the mounted GitHub PAT, for the remote-git
// reads) is answered here, in-process, and never leaves the pod. The proxy
// asks `interceptJsonRpc` what to do with each request body:
//
//   - `tools/call` naming a local tool → answered here (`answer`);
//   - `tools/list` → forwarded, and the upstream list gains the local
//     descriptors (`forward-and-merge-list`, applied by `mergeToolsList`);
//   - anything else — another method, a tool upstream owns, a JSON-RPC batch,
//     a body that is not JSON — is forwarded unchanged (`forward`).

/** One MCP tool as `tools/list` describes it. */
export interface McpToolDescriptor {
  name: string;
  description: string;
  inputSchema: Record<string, unknown>;
}

/** A `tools/call` result: text content, flagged when it is a tool error. */
export interface McpToolResult {
  content: { type: string; text: string }[];
  isError?: boolean;
}

/** Tools the runner serves itself. */
export interface LocalMcpTools {
  descriptors: McpToolDescriptor[];
  /** The result of calling `name`, or `undefined` when it is not a local tool. */
  call(name: string, args: Record<string, unknown>): Promise<McpToolResult> | undefined;
}

export type JsonRpcDisposition =
  | { kind: "answer"; payload: unknown }
  | { kind: "forward" }
  | { kind: "forward-and-merge-list" };

const FORWARD: JsonRpcDisposition = { kind: "forward" };

function isObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Decides whether a request body is answered here or forwarded upstream. */
export async function interceptJsonRpc(body: Buffer, local: LocalMcpTools): Promise<JsonRpcDisposition> {
  let msg: unknown;
  try {
    msg = JSON.parse(body.toString("utf8"));
  } catch {
    return FORWARD;
  }
  // A batch forwards whole: splitting it would mean answering half of it here
  // and stitching the other half back in, for a shape no MCP client sends.
  if (!isObject(msg)) return FORWARD;
  if (msg.method === "tools/list") return { kind: "forward-and-merge-list" };
  if (msg.method !== "tools/call" || !isObject(msg.params) || typeof msg.params.name !== "string") return FORWARD;

  const args = isObject(msg.params.arguments) ? msg.params.arguments : {};
  const pending = local.call(msg.params.name, args);
  if (pending === undefined) return FORWARD;
  let result: McpToolResult;
  try {
    result = await pending;
  } catch (err) {
    // A local tool's failure is the agent's to read, like any tool error; it
    // must never fall through to upstream, which no longer serves the tool.
    result = { content: [{ type: "text", text: err instanceof Error ? err.message : String(err) }], isError: true };
  }
  return { kind: "answer", payload: { jsonrpc: "2.0", id: msg.id, result } };
}

/**
 * The upstream `tools/list` reply with the local descriptors appended, or
 * `undefined` when it is not a JSON-RPC result carrying a tool list (an error,
 * an SSE frame) and so must pass through untouched. An upstream entry under a
 * local tool's name is dropped: the local tool is the one that answers.
 */
export function mergeToolsList(upstreamBody: Buffer, descriptors: McpToolDescriptor[]): Buffer | undefined {
  let msg: unknown;
  try {
    msg = JSON.parse(upstreamBody.toString("utf8"));
  } catch {
    return undefined;
  }
  if (!isObject(msg) || !isObject(msg.result) || !Array.isArray(msg.result.tools)) return undefined;
  const local = new Set(descriptors.map((d) => d.name));
  const upstream = msg.result.tools.filter((t: unknown) => !(isObject(t) && typeof t.name === "string" && local.has(t.name)));
  msg.result.tools = [...upstream, ...descriptors];
  return Buffer.from(JSON.stringify(msg));
}
