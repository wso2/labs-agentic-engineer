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

import type express from "express";
import { type LanguageModel } from "ai";
import { ArchitectInput } from "../../agents/architect/schema.js";
import { runArchitect } from "../../agents/architect/run.js";
import type { SseSink, FinalizeResolver } from "../../agents/architect/tools.js";
import { AnthropicKeyError } from "../../shared/anthropic-key-resolver.js";
import { resolveModelForOrg } from "../../shared/model.js";
import { getOrgId } from "../../middleware/org-id.js";
import { loadMcpTools, connectionsMcpUrl } from "../../shared/mcp-client.js";

function writeFrame(res: express.Response, frame: unknown): void {
  res.write(`data: ${JSON.stringify(frame)}\n\n`);
}

export function registerArchitect(app: express.Express) {
  app.post("/v1/agents/architect", async (req, res) => {
    const parsed = ArchitectInput.safeParse(req.body);
    if (!parsed.success) {
      res.status(400).json({ error: parsed.error.format() });
      return;
    }

    const orgId = getOrgId(res);
    let model: LanguageModel;
    try {
      model = await resolveModelForOrg(orgId);
    } catch (err) {
      if (err instanceof AnthropicKeyError) {
        res.status(err.status).json({ error: err.message, code: err.code });
        return;
      }
      throw err;
    }

    console.log(
      `[architect] streaming orgId=${orgId} (spec ${parsed.data.spec.length} chars, incremental=${parsed.data.previousDesign ? "yes" : "no"})`,
    );

    res.writeHead(200, {
      "Content-Type": "text/event-stream",
      "Cache-Control": "no-cache, no-transform",
      Connection: "keep-alive",
      "x-vercel-ai-ui-message-stream": "v1",
    });
    res.flushHeaders?.();

    const abortController = new AbortController();
    res.on("close", () => {
      if (!res.writableEnded) abortController.abort();
    });

    // Prevent OC API gateway idle-timeout from dropping the SSE connection
    // while waiting for the model. SSE comments are forwarded by the BFF and
    // ignored by browser SSE parsers.
    const keepAlive = setInterval(() => {
      if (!res.writableEnded) res.write(": keep-alive\n\n");
    }, 15_000);

    const sink: SseSink = {
      send(event, data) {
        if (res.writableEnded) return;
        writeFrame(res, { type: `data-${event}`, data });
      },
      isClosed() {
        return res.writableEnded;
      },
    };

    const finalizer: FinalizeResolver = {
      finalized: false,
      resolve: () => {
        // Once finalize() emits data-finish, we no longer need the model to
        // keep talking. Aborting the stream tears down the SDK loop cleanly.
        abortController.abort();
      },
    };

    // Discover the org's registered `external` connections via the BFF-hosted MCP
    // server so the design LLM reuses an existing connection (name + key schema)
    // instead of inventing one. Best-effort: an unreachable server yields no tools.
    const mcpUrl = connectionsMcpUrl(orgId);
    const extraTools = mcpUrl ? await loadMcpTools(mcpUrl) : {};

    try {
      const { finalized } = await runArchitect({
        model,
        input: parsed.data,
        sink,
        finalizer,
        abortSignal: abortController.signal,
        extraTools,
      });

      // Loop ended without finalize() succeeding. Could be: stepCountIs hit,
      // model gave up, or upstream error. Emit error so BFF doesn't write.
      if (!finalized && !res.writableEnded) {
        writeFrame(res, {
          type: "error",
          errorText:
            "architect agent ended without calling finalize — design not produced",
        });
      }

      if (!res.writableEnded) res.write("data: [DONE]\n\n");
    } catch (err) {
      // runArchitect swallows the expected finalize-abort; anything reaching
      // here is a real error (or a client-disconnect abort, where the socket
      // is already gone and the guarded writes simply no-op).
      console.error("[architect] pipe error:", err);
      if (!res.writableEnded) {
        writeFrame(res, {
          type: "error",
          errorText: err instanceof Error ? err.message : String(err),
        });
      }
    } finally {
      clearInterval(keepAlive);
      if (!res.writableEnded) res.end();
    }
  });
}
