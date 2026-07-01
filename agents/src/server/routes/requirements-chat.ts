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
import { streamText, stepCountIs, type LanguageModel } from "ai";
import {
  RequirementsChatInput,
  RequirementsDoc,
  systemPrompt,
  buildUserPrompt,
  buildTools,
  type SseSink,
  type FinalizeResolver,
} from "../../agents/requirements-chat/index.js";
import { AnthropicKeyError } from "../../shared/anthropic-key-resolver.js";
import { resolveModelForOrg } from "../../shared/model.js";
import { getOrgId } from "../../middleware/org-id.js";

function writeFrame(res: express.Response, frame: unknown): void {
  res.write(`data: ${JSON.stringify(frame)}\n\n`);
}

export function registerRequirementsChat(app: express.Express) {
  app.post("/v1/agents/requirements-chat", async (req, res) => {
    const parsed = RequirementsChatInput.safeParse(req.body);
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

    const fileCount = Object.keys(parsed.data.files).length;
    console.log(
      `[requirements-chat] streaming orgId=${orgId} mode=${parsed.data.mode} message=${parsed.data.message.length}c files=${fileCount}`,
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

    const keepAlive = setInterval(() => {
      if (!res.writableEnded) res.write(": keep-alive\n\n");
    }, 15_000);

    const sse: SseSink = {
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
        abortController.abort();
      },
    };

    const doc = new RequirementsDoc(parsed.data.files);
    const tools = buildTools(doc, sse, finalizer, parsed.data.mode);

    // The model can end a turn without calling finish() for two very
    // different reasons: a benign refusal from Anthropic's safety system
    // (finishReason === "content-filter" — the model declines the input and
    // emits ~nothing) versus an actual agent malfunction. We track the last
    // finishReason so the fallthrough below can surface the refusal case with
    // a clear, user-facing message instead of an opaque
    // "ended without calling finish".
    let lastFinishReason: string | undefined;
    try {
      const result = streamText({
        model,
        system: systemPrompt,
        prompt: buildUserPrompt(
          parsed.data.message,
          parsed.data.history,
          parsed.data.files,
          parsed.data.mode,
        ),
        tools,
        // Per the design: stepCountIs(64) runaway guard; finish() is the
        // intended terminator.
        stopWhen: stepCountIs(64),
        abortSignal: abortController.signal,
        onError: ({ error }) => {
          console.error("[requirements-chat] streamText error:", error);
        },
        onFinish: (ev) => {
          lastFinishReason = ev.finishReason;
          console.log(
            `[requirements-chat] finish=${ev.finishReason} steps=${ev.steps?.length ?? 0} in=${ev.usage?.inputTokens ?? 0} out=${ev.usage?.outputTokens ?? 0}`,
          );
        },
      });

      // Forward free-form model text to the client as text-delta frames so
      // the chat can render the assistant's thinking-out-loud commentary.
      for await (const chunk of result.textStream) {
        if (res.writableEnded) break;
        if (chunk.length > 0) {
          writeFrame(res, { type: "text-delta", delta: chunk });
        }
      }

      if (!finalizer.finalized && !res.writableEnded) {
        // Anthropic maps a model-level safety refusal to
        // finishReason "content-filter" — the model declines the input and
        // returns almost no output. Surface it as a clear, actionable message
        // rather than the generic agent-malfunction error so the user knows to
        // rephrase, and downstream logs aren't misread as an infra fault.
        const refused = lastFinishReason === "content-filter";
        writeFrame(res, {
          type: "error",
          errorText: refused
            ? "The AI assistant declined this request as it appears to conflict with its usage policies. Please rephrase and try again."
            : "requirements-chat agent ended without calling finish",
        });
      }

      if (!res.writableEnded) res.write("data: [DONE]\n\n");
    } catch (err) {
      const aborted =
        abortController.signal.aborted &&
        (err instanceof Error
          ? err.name === "AbortError" || /aborted/i.test(err.message)
          : false);
      if (aborted && finalizer.finalized) {
        if (!res.writableEnded) res.write("data: [DONE]\n\n");
      } else {
        console.error("[requirements-chat] pipe error:", err);
        if (!res.writableEnded) {
          writeFrame(res, {
            type: "error",
            errorText: err instanceof Error ? err.message : String(err),
          });
        }
      }
    } finally {
      clearInterval(keepAlive);
      if (!res.writableEnded) res.end();
    }
  });
}
