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
 * The Issues chat's `classify_report` tool: one choice question to Jev (the
 * report classifier) asking what kind of report the user's message is. The
 * agent files on a confident bug / feature / improvement and asks otherwise.
 *
 * Every failure — no key, a non-2xx, a malformed body, a timeout, a network
 * error — resolves to `unknown` with `needsClarification`, so a classifier
 * outage degrades to the agent asking and never fails a turn. The key goes only
 * into the request header; it is never logged or returned.
 */

import { tool, type Tool } from "ai";
import { z } from "zod";

export const CLASSIFY_REPORT = "classify_report";

export type ReportKind = "bug" | "feature" | "improvement" | "question";

export interface ClassifyReportResult {
  kind: ReportKind | "unknown";
  confidence: number;
  /** The two most probable kinds, most probable first. */
  alternatives: { kind: ReportKind; p: number }[];
  /** True when the agent must ask before filing. */
  needsClarification: boolean;
}

export interface JevOptions {
  apiKey: string | undefined;
  url: string;
  fetch: typeof globalThis.fetch;
  timeoutMs?: number;
}

/** Filing needs at least this much confidence; a `question` never files. */
const CLARIFY_BELOW = 0.8;
const DEFAULT_TIMEOUT_MS = 5000;

const KINDS = ["bug", "feature", "improvement", "question"] as const;

const CRITERIA: Record<ReportKind, string> = {
  bug: "Something that exists does not work as it should: an error, a crash, a broken button, wrong output.",
  feature: "A request for a new capability the product does not have yet.",
  improvement: "A change to make something that already works better: faster, clearer, easier.",
  question: "A question or remark that reports nothing and asks for nothing to change.",
};

const INSTRUCTIONS =
  "What kind of report is `message`? Use `recent_messages` to resolve words like it or that.";

const jevResponseSchema = z.object({
  answers: z.object({
    kind: z.object({
      choice: z.enum(KINDS),
      confidence: z.number(),
      probabilities: z.record(z.string(), z.number()).optional(),
    }),
  }),
});

const UNKNOWN: ClassifyReportResult = { kind: "unknown", confidence: 0, alternatives: [], needsClarification: true };

function unknown(reason: string): ClassifyReportResult {
  console.warn(`classify_report: ${reason}`);
  return { ...UNKNOWN, alternatives: [] };
}

export async function classifyReport(
  opts: JevOptions,
  input: { message: string; recentMessages?: string[] },
): Promise<ClassifyReportResult> {
  if (!opts.apiKey) return unknown("no Jev key configured");

  // A real (ref'd) timer rather than AbortSignal.timeout, whose timer is unref'd.
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), opts.timeoutMs ?? DEFAULT_TIMEOUT_MS);
  try {
    return await ask(opts, opts.apiKey, input, controller.signal);
  } finally {
    clearTimeout(timer);
  }
}

async function ask(
  opts: JevOptions,
  apiKey: string,
  input: { message: string; recentMessages?: string[] },
  signal: AbortSignal,
): Promise<ClassifyReportResult> {
  let res: Response;
  try {
    res = await opts.fetch(opts.url, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${apiKey}` },
      body: JSON.stringify({
        model: "jev-latest",
        state: {
          message: input.message,
          recent_messages: input.recentMessages ?? [],
          // No project name on purpose: the tool has none, and it keeps the project's name from a third party.
          where_the_user_is: { page: "issues" },
        },
        questions: { kind: { type: "choice", instructions: INSTRUCTIONS, criteria: CRITERIA } },
      }),
      signal,
    });
  } catch (err) {
    return unknown(err instanceof Error ? err.name : "request failed");
  }
  if (!res.ok) return unknown(`status ${res.status}`);

  let json: unknown;
  try {
    json = await res.json();
  } catch {
    return unknown("response is not JSON");
  }
  const parsed = jevResponseSchema.safeParse(json);
  if (!parsed.success) return unknown("malformed response");

  const { choice, confidence, probabilities } = parsed.data.answers.kind;
  const alternatives = KINDS.flatMap((kind) => {
    const p = probabilities?.[kind];
    return p === undefined ? [] : [{ kind, p }];
  })
    .sort((a, b) => b.p - a.p)
    .slice(0, 2);
  return {
    kind: choice,
    confidence,
    alternatives: alternatives.length > 0 ? alternatives : [{ kind: choice, p: confidence }],
    needsClarification: choice === "question" || confidence < CLARIFY_BELOW,
  };
}

const classifyReportInputSchema = z.object({
  message: z.string().min(1).describe("The user's message to classify, verbatim."),
  recentMessages: z
    .array(z.string())
    .max(6)
    .optional()
    .describe("Up to six of the latest earlier messages, oldest first, to resolve words like it or that."),
});

/** The `classify_report` tool: what kind of report the user's message is. */
export function buildClassifyReportTool(
  opts: JevOptions,
): Tool<z.infer<typeof classifyReportInputSchema>, ClassifyReportResult> {
  return tool({
    description:
      "Classify a user's message as a bug, feature, improvement or question, with a confidence. " +
      "Call it before filing an issue. A question is answered, not filed (do not ask which kind). Otherwise, when " +
      "needsClarification is true (this includes kind unknown, when the classifier is unavailable), ask the user " +
      "which kind it is instead of filing.",
    inputSchema: classifyReportInputSchema,
    execute: ({ message, recentMessages }) =>
      classifyReport(opts, { message, ...(recentMessages ? { recentMessages } : {}) }),
  });
}
