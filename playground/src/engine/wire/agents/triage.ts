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
 * TRIAGE: a service did not come up, and the log is 200 lines of a stack the
 * person reading it has never seen before.
 *
 * This task READS and answers in a paragraph. It writes nothing, runs nothing,
 * and proposes no patch — the fix for application code is a coding run against
 * an issue, which is the only thing in this system that writes application
 * code. What it is for is the sentence in the middle: is this the app, the
 * wiring, or the machine, and which file says so.
 *
 * Its answer is appended to `triage.log` so a second failure can be compared
 * with the first.
 */

import { appendFileSync } from "node:fs";
import { wirePaths } from "../state.js";
import { runAgentTask } from "./task.js";
import { maskedPlan } from "../compose.js";
import type { WirePlan } from "../plan.js";

export interface TriageRequest {
  projectDir: string;
  /** The compose service that failed. */
  service: string;
  logs: string;
  plan: WirePlan;
  useApiKey?: boolean;
}

export interface TriageResult {
  ok: boolean;
  summary: string;
}

export async function runTriageAgent(request: TriageRequest): Promise<TriageResult> {
  const paths = wirePaths(request.projectDir);
  const result = await runAgentTask({
    boundary: { task: "The triage task", slug: "triage", mayWrite: [] },
    prompt: triagePrompt(request),
    cwd: request.projectDir,
    maxTurns: 20, // 8 was measured too few to reach an answer; see ADR-0002.
    transcriptDir: paths.agents,
    ...(request.useApiKey ? { useApiKey: true } : {}),
  });

  const answer = result.text.trim();
  appendFileSync(
    paths.triageLog,
    `\n## ${new Date().toISOString()} — ${request.service}\n\n${answer}\n`,
  );
  return { ok: result.ok, summary: answer.split("\n")[0] ?? "no answer" };
}

function triagePrompt(request: TriageRequest): string {
  const service = request.plan.services.find((candidate) => candidate.name === request.service);
  return [
    `The compose service "${request.service}" exited or never turned healthy in a local wired run of this project.`,
    "",
    "It was built from its own Dockerfile and started beside its database, with the environment below. The",
    "environment is the plan's, with secrets masked — a masked value IS set, it is just not shown.",
    "",
    "```json",
    JSON.stringify(
      service ? maskedPlan(request.plan).services.find((s) => s.name === request.service) : { note: "not in the plan" },
      null,
      2,
    ),
    "```",
    "",
    "The last lines of its log:",
    "",
    "```",
    request.logs.trimEnd().split("\n").slice(-200).join("\n"),
    "```",
    "",
    `You may READ this project: ${service?.appPath ?? "the app path"}/, its Dockerfile, and specs/design/.`,
    "Stay inside it — the harness that started the container is not yours to read — and read a handful of files at",
    "most. You may not run anything, write anything, or propose a patch.",
    "",
    "Answer in one short paragraph, starting with whether this is APP CODE, WIRING or THE ENVIRONMENT, and naming",
    "the file and line when you can. If what you have does not settle it, say what you ruled out and stop. An",
    "answer with an open question in it is worth more than another five files read.",
  ].join("\n");
}
