// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

/**
 * Minimal Model Context Protocol client for the BFF-hosted connection-discovery
 * server (asdlc-service/internal/feature/connections/mcp_server.go). It speaks the
 * JSON-RPC Streamable-HTTP transport in its simplest single-response form (POST a
 * request, read an application/json response) — the BFF server is stateless, so no
 * SSE/session handshake is needed.
 *
 * `loadMcpTools` DISCOVERS the server's tools via `tools/list` and wraps each as an
 * AI SDK `dynamicTool` whose execution proxies to `tools/call`. The architect merges
 * these into its tool set so the design LLM can look up the org's already-registered
 * `external` connections (so it reuses an existing connection name + key schema
 * instead of inventing one). The AI SDK v6 dropped its built-in MCP client, hence
 * this dependency-free shim.
 */

import { dynamicTool, jsonSchema, type ToolSet } from "ai";

interface JsonRpcResponse {
  jsonrpc: string;
  id: number;
  result?: any;
  error?: { code: number; message: string };
}

interface McpToolDescriptor {
  name: string;
  description?: string;
  inputSchema?: Record<string, unknown>;
}

async function rpc(
  url: string,
  method: string,
  params: unknown,
  id: number,
): Promise<any> {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id, method, params }),
  });
  if (!res.ok) {
    throw new Error(`mcp ${method}: HTTP ${res.status}`);
  }
  const body = (await res.json()) as JsonRpcResponse;
  if (body.error) {
    throw new Error(`mcp ${method}: ${body.error.message}`);
  }
  return body.result;
}

/**
 * Connect to the MCP server at `url`, discover its tools, and return them as an AI
 * SDK ToolSet. On any failure (server down, network) it logs and returns an empty
 * set — discovery is a best-effort enrichment, never a hard dependency of design.
 */
export async function loadMcpTools(url: string): Promise<ToolSet> {
  try {
    await rpc(url, "initialize", { protocolVersion: "2024-11-05", capabilities: {} }, 1);
    const listed = await rpc(url, "tools/list", {}, 2);
    const descriptors: McpToolDescriptor[] = listed?.tools ?? [];
    const tools: ToolSet = {};
    let callId = 100;
    for (const d of descriptors) {
      tools[d.name] = dynamicTool({
        description: d.description ?? "",
        inputSchema: jsonSchema(d.inputSchema ?? { type: "object", properties: {} }),
        execute: async (args) => {
          const result = await rpc(
            url,
            "tools/call",
            { name: d.name, arguments: args ?? {} },
            callId++,
          );
          // MCP tool result: { content: [{ type: 'text', text }], isError? }.
          const text = (result?.content ?? [])
            .filter((c: any) => c?.type === "text")
            .map((c: any) => c.text)
            .join("\n");
          return text || JSON.stringify(result ?? {});
        },
      });
    }
    console.log(`[mcp] loaded ${Object.keys(tools).length} tool(s) from ${url}`);
    return tools;
  } catch (err) {
    console.warn(`[mcp] tool discovery failed (${url}): ${(err as Error).message}`);
    return {};
  }
}

/**
 * Build the connection-discovery MCP server URL for an org from the BFF base URL
 * the agents-service already targets (ASDLC_API_URL).
 */
export function connectionsMcpUrl(orgId: string): string | null {
  const base = process.env.ASDLC_API_URL || process.env.GIT_SERVICE_URL;
  if (!base) return null;
  return `${base.replace(/\/$/, "")}/internal/organizations/${encodeURIComponent(orgId)}/mcp`;
}
