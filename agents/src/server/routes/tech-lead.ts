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
import { z } from "zod";
import { streamText, type LanguageModel } from "ai";
import { AnthropicKeyError } from "../../shared/anthropic-key-resolver.js";
import { resolveModelForOrg } from "../../shared/model.js";
import { getOrgId } from "../../middleware/org-id.js";
import {
  TechLeadPlanInput,
  TechLeadDetailInput,
} from "../../agents/tech-lead/schema.js";
import {
  detailSystemPrompt,
  buildDetailUserPrompt,
} from "../../agents/tech-lead/prompt.js";
import {
  runTechLeadPlan,
  MalformedPlanItemError,
} from "../../agents/tech-lead/run.js";

function writeFrame(res: express.Response, frame: unknown): void {
  if (res.writableEnded) return;
  res.write(`data: ${JSON.stringify(frame)}\n\n`);
}

function setupSse(res: express.Response): {
  abortController: AbortController;
  keepAlive: NodeJS.Timeout;
} {
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

  return { abortController, keepAlive };
}

// =============================================================================
// Phase 1 — Plan
// =============================================================================

// Wire shape extension — the validator wants pre-computed diff context for
// incremental coverage rules. BFF computes this; the route just shuttles it.
const PlanRequestBody = TechLeadPlanInput.extend({
  diff: z
    .object({
      added: z.array(z.string()),
      contractAffectedModified: z.array(z.string()),
      removed: z.array(z.string()),
    })
    .optional(),
});

export function registerTechLeadPlan(app: express.Express) {
  app.post("/v1/agents/tech-lead/plan", async (req, res) => {
    const parsed = PlanRequestBody.safeParse(req.body);
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
      `[tech-lead/plan] orgId=${orgId} mode=${parsed.data.mode} components=${parsed.data.slimDesign.length} existing=${parsed.data.existingTasks?.length ?? 0}`,
    );

    const { abortController, keepAlive } = setupSse(res);

    try {
      const { items, issues } = await runTechLeadPlan({
        model,
        input: parsed.data,
        diff: parsed.data.diff,
        onSealed: (item) =>
          writeFrame(res, { type: "data-plan-item", data: item }),
        isClosed: () => res.writableEnded,
        abortSignal: abortController.signal,
      });

      if (issues.length > 0) {
        writeFrame(res, { type: "error", data: { scope: "plan", issues } });
        return;
      }

      writeFrame(res, { type: "data-plan-complete", data: { items } });
    } catch (err) {
      if (err instanceof MalformedPlanItemError) {
        writeFrame(res, {
          type: "error",
          data: {
            scope: "plan",
            code: "malformed-plan-item",
            index: err.index,
            issues: err.issues,
          },
        });
      } else {
        const aborted =
          abortController.signal.aborted &&
          (err instanceof Error
            ? err.name === "AbortError" || /aborted/i.test(err.message)
            : false);
        if (!aborted) {
          console.error("[tech-lead/plan] error:", err);
          writeFrame(res, {
            type: "error",
            data: {
              scope: "plan",
              errorText: err instanceof Error ? err.message : String(err),
            },
          });
        }
      }
    } finally {
      clearInterval(keepAlive);
      if (!res.writableEnded) {
        res.write("data: [DONE]\n\n");
        res.end();
      }
    }
  });
}

// =============================================================================
// Phase 2 — Detail (parallel streamText fan-out)
// =============================================================================

const DETAIL_CONCURRENCY = parseInt(
  process.env.TECH_LEAD_PHASE2_CONCURRENCY || "4",
  10,
);
const DELTA_COALESCE_MS = 250;

export function registerTechLeadDetail(app: express.Express) {
  app.post("/v1/agents/tech-lead/detail", async (req, res) => {
    const parsed = TechLeadDetailInput.safeParse(req.body);
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
      `[tech-lead/detail] orgId=${orgId} tasks=${parsed.data.items.length} concurrency=${DETAIL_CONCURRENCY}`,
    );

    const { abortController, keepAlive } = setupSse(res);
    const data = parsed.data;

    try {
      // Tiny semaphore — keeps a bounded number of streamText calls in flight
      // without pulling in p-limit as a dep.
      const queue = [...data.items];
      const tasks: Promise<void>[] = [];

      async function runNext(): Promise<void> {
        const item = queue.shift();
        if (!item) return;
        await runDetailForItem(
          res,
          data.projectName,
          data.spec,
          item,
          abortController,
          model,
        );
        await runNext();
      }

      const initial = Math.min(DETAIL_CONCURRENCY, queue.length);
      for (let i = 0; i < initial; i++) tasks.push(runNext());
      await Promise.all(tasks);
    } catch (err) {
      const aborted =
        abortController.signal.aborted &&
        (err instanceof Error
          ? err.name === "AbortError" || /aborted/i.test(err.message)
          : false);
      if (!aborted) {
        console.error("[tech-lead/detail] fan-out error:", err);
        writeFrame(res, {
          type: "error",
          data: {
            scope: "detail",
            errorText: err instanceof Error ? err.message : String(err),
          },
        });
      }
    } finally {
      clearInterval(keepAlive);
      if (!res.writableEnded) {
        res.write("data: [DONE]\n\n");
        res.end();
      }
    }
  });
}

async function runDetailForItem(
  res: express.Response,
  projectName: string,
  spec: string,
  item: TechLeadDetailInput["items"][number],
  abortController: AbortController,
  model: LanguageModel,
): Promise<void> {
  if (res.writableEnded) return;

  const prompt = buildDetailUserPrompt(projectName, spec, item);
  let fullBody = "";
  let pendingDelta = "";
  let lastFlush = Date.now();

  const flush = () => {
    if (pendingDelta.length === 0) return;
    writeFrame(res, {
      type: "data-task-body-delta",
      data: { taskId: item.taskId, delta: pendingDelta },
    });
    pendingDelta = "";
    lastFlush = Date.now();
  };

  try {
    const result = streamText({
      model,
      system: detailSystemPrompt,
      prompt,
      abortSignal: abortController.signal,
      onError: ({ error }) => {
        console.error(`[tech-lead/detail ${item.taskId}] streamText error:`, error);
      },
    });

    for await (const chunk of result.textStream) {
      if (res.writableEnded) return;
      pendingDelta += chunk;
      fullBody += chunk;
      const now = Date.now();
      if (now - lastFlush >= DELTA_COALESCE_MS) flush();
    }
    flush();

    if (!res.writableEnded) {
      writeFrame(res, {
        type: "data-task-body-complete",
        data: { taskId: item.taskId, body: fullBody },
      });
    }
  } catch (err) {
    const aborted =
      abortController.signal.aborted &&
      (err instanceof Error
        ? err.name === "AbortError" || /aborted/i.test(err.message)
        : false);
    if (aborted) return;
    console.error(`[tech-lead/detail ${item.taskId}] error:`, err);
    writeFrame(res, {
      type: "error",
      data: {
        scope: "detail",
        taskId: item.taskId,
        errorText: err instanceof Error ? err.message : String(err),
      },
    });
  }
}
