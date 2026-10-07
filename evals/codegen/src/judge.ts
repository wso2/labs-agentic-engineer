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
 * The judge, a separate call from the walker (ADR-0004): reads the checklist
 * and the walker's evidence. Per item: pass/fail and the user-visible symptom,
 * never a cause.
 */

import { z } from "zod";
import type { Item, MustNot } from "./case.js";
import { JUDGE, MODELS, TIMEOUTS } from "./config.js";
import type { Judgement } from "./score.js";
import { runSession, throwIfCredentialRefused, type SessionResult } from "./session.js";
import type { WalkResult } from "./walker.js";

const JudgementSchema = z
  .object({
    items: z.array(
      z
        .object({
          id: z.string(),
          verdict: z.enum(["pass", "fail"]),
          symptom: z.string(),
        })
        .strict(),
    ),
    mustNot: z.array(
      z
        .object({
          id: z.string(),
          violated: z.boolean(),
          evidence: z.string(),
        })
        .strict(),
    ),
    summary: z.string(),
  })
  .strict();

const SYSTEM_PROMPT = `You judge a web application's acceptance walk from the tester's recorded evidence.
You report symptoms — what a user would see — never causes. You answer with one JSON object in the requested schema.`;

export function judgePrompt(items: Item[], mustNot: MustNot[], walk: WalkResult): string {
  return `A tester walked a running web application through a checklist and recorded evidence per item. Judge each item from that evidence.

RULES
- pass: the evidence shows the item's \`expect\` held, including any request it implies having left the page successfully.
- fail: anything else — the tester saw something different, the item was blocked, or the evidence does not show the expectation held. When the tester's verdict and their own observation disagree, the observation wins.
- symptom (fail only; "" on pass): ONE sentence describing what a user would experience ("Submitting the form shows no error and the claim never appears in My Claims"). No guesses about code, services, databases or why it happened.
- mustNot: for each entry, violated true only when the evidence (items or notes) shows it happening; quote that evidence. Otherwise false with "".
- summary: two or three sentences on the app as a user would find it.
Return one \`items\` entry per checklist id, in checklist order.

CHECKLIST
${JSON.stringify(items.map(({ id, role, screen, steps, expect }) => ({ id, role, screen, steps, expect })), null, 2)}

MUST NOT
${JSON.stringify(mustNot, null, 2)}

TESTER'S EVIDENCE
${JSON.stringify(walk, null, 2)}`;
}

export async function judge(opts: {
  items: Item[];
  mustNot: MustNot[];
  walk: WalkResult;
  cwd: string;
  env: NodeJS.ProcessEnv;
  transcriptFile: string;
  signal?: AbortSignal;
}): Promise<SessionResult<Judgement>> {
  let last: SessionResult<Judgement> | undefined;
  for (let attempt = 1; attempt <= JUDGE.attempts; attempt += 1) {
    last = await runSession({
      prompt: judgePrompt(opts.items, opts.mustNot, opts.walk),
      systemPrompt: SYSTEM_PROMPT,
      cwd: opts.cwd,
      model: MODELS.judge,
      tools: JUDGE.tools,
      schema: JudgementSchema,
      // One turn to answer, plus the structured-output tool's own round trip.
      maxTurns: 4,
      timeoutMs: TIMEOUTS.judgeMinutes * 60_000,
      env: opts.env,
      transcriptFile: opts.transcriptFile,
      ...(opts.signal ? { signal: opts.signal } : {}),
    });
    throwIfCredentialRefused(last);
    if (last.output || opts.signal?.aborted) return last;
  }
  return last as SessionResult<Judgement>;
}
