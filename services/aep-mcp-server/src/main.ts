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
 * The composition root: a stateless Streamable HTTP MCP server. Each POST
 * /mcp request gets its OWN McpServer + transport pair, built with the
 * caller's bearer token captured from the Authorization header — see
 * server.ts. AEP_MCP_DEFAULT_BEARER is an opt-in fallback for callers that
 * cannot send headers to a plaintext URL (the OC SRE extension loader refuses
 * that combination). Unset by default: every caller must then bring its own
 * Authorization header. When set, any caller that reaches this port acts with
 * that bearer, so deployments set it only where the port is limited to the
 * trusted handoff caller (the Helm chart pairs it with a NetworkPolicy; see
 * docs/developer-guide/sre-handoff-security.md). Stateless mode (sessionIdGenerator:
 * undefined) is required here: a shared/session-scoped server would let one
 * caller's bearer leak into another's tool calls.
 *
 *   pnpm --filter @aep/aep-mcp-server dev
 */

import express from "express";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";

import { intEnv, loadAepApiBaseUrl, parseDefaultBearer } from "./env.js";
import { createAepMcpServer } from "./server.js";

const port = intEnv(process.env.PORT, 3400);
const aepApiBaseUrl = loadAepApiBaseUrl();
const defaultBearer = parseDefaultBearer(process.env.AEP_MCP_DEFAULT_BEARER);

const app = express();
app.use(express.json());

app.get("/healthz", (_req, res) => {
  res.status(200).json({ status: "ok" });
});
// TODO: We have Two MCP servers, one for the AEP API and one for the AEP coding agent.
// The coding agent's MCP server is used by the coding agent to communicate with the AEP API. 
// We should consider merging these two servers into one.
app.post("/mcp", async (req, res) => {
  const bearer = req.headers.authorization ?? defaultBearer;
  if (!bearer) {
    res.status(401).json({ error: "missing Authorization header" });
    return;
  }

  const server = createAepMcpServer({ baseUrl: aepApiBaseUrl, bearer });
  // No options ⇒ sessionIdGenerator stays undefined ⇒ stateless mode (per the
  // SDK's own doc comment on StreamableHTTPServerTransport). Passing
  // `{ sessionIdGenerator: undefined }` explicitly is what the SDK's docs
  // show, but exactOptionalPropertyTypes rejects assigning `undefined` to
  // that optional field — omitting it entirely is equivalent and type-clean.
  const transport = new StreamableHTTPServerTransport();

  res.on("close", () => {
    void transport.close();
    void server.close();
  });

  try {
    // StreamableHTTPServerTransport's onclose/onerror/onmessage accessors
    // accept `T | undefined` in their setters, one notch wider than
    // Transport's `T | undefined`-less optional property declarations —
    // under exactOptionalPropertyTypes that variance alone (not an actual
    // protocol mismatch) makes the structural assignment fail. The SDK
    // ships its own examples assigning this transport straight to
    // `Transport`-typed params, so this is a typing gap in the library, not
    // a hidden defect here.
    await server.connect(transport as unknown as Transport);
    await transport.handleRequest(req, res, req.body);
  } catch (err) {
    // Log the full error server-side (stack, upstream message, URLs) but never
    // return it to the caller — the raw detail can leak internal information.
    // The client gets a generic 500 only.
    process.stderr.write(`mcp request failed: ${String(err)}\n`);
    if (!res.headersSent) {
      res.status(500).json({ error: "internal error" });
    }
  }
});

app.listen(port, () => {
  process.stdout.write(
    `@aep/aep-mcp-server listening on :${port} (aep-api: ${aepApiBaseUrl}, ` +
      `default bearer fallback: ${defaultBearer ? "enabled" : "disabled"})\n`,
  );
});
