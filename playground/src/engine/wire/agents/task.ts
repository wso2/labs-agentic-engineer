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
 * ONE AGENT TASK, run inside its boundary.
 *
 * Both wired-mode tasks are shaped the same: a single prompt, a hard turn cap,
 * a hook table that refuses everything outside the task's table, and a
 * transcript on disk so what the model saw and decided can be read afterwards —
 * the same reason a coding run keeps `runtime.log`.
 *
 * Credentials follow `code --host`: the developer's own `claude login` answers,
 * and the platform key sitting in `deployments/.env` is WITHHELD. These tasks
 * run on a developer's machine against their own project; billing a shared
 * credential because a file elsewhere defined one is the surprise that rule
 * exists to prevent. `useApiKey` opts back in, exactly as `--api-key` does.
 */

import { appendFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { query } from "@anthropic-ai/claude-agent-sdk";
import { applyCodingCredential, codingCredential } from "../../coding-run.js";
import { createBoundaryGuard, deniedTools, guardMatchers, type AgentBoundary } from "./guard.js";

export interface AgentTaskRequest {
  boundary: AgentBoundary;
  prompt: string;
  /** The task's working directory — always the project. */
  cwd: string;
  maxTurns: number;
  /** Where the transcript lands: `<state>/wire/agents/`. */
  transcriptDir: string;
  /** `--api-key`: authenticate with the API key rather than the developer's login. */
  useApiKey?: boolean;
}

export interface AgentTaskResult {
  ok: boolean;
  /** The model's final answer, or the reason there is none. */
  text: string;
  /** Every refusal the boundary made, so the session can say what it stopped. */
  denials: string[];
  transcript: string;
}

/**
 * The environment the task's process gets.
 *
 * Mirrors `hostInvocation` in ../../coding-run.ts, which explains at length why
 * a local agent run does not inherit the platform's key.
 */
function taskEnv(useApiKey: boolean): NodeJS.ProcessEnv {
  const env: NodeJS.ProcessEnv = { ...process.env };
  const coding = useApiKey ? codingCredential() : undefined;
  if (coding) {
    applyCodingCredential(env, coding);
    return env;
  }
  delete env.CLAUDE_CODE_OAUTH_TOKEN;
  if (!useApiKey) delete env.ANTHROPIC_API_KEY;
  return env;
}

export async function runAgentTask(request: AgentTaskRequest): Promise<AgentTaskResult> {
  mkdirSync(request.transcriptDir, { recursive: true });
  const stamp = new Date().toISOString().replace(/[:.]/g, "-");
  const transcript = join(request.transcriptDir, `${request.boundary.slug}-${stamp}.jsonl`);
  const record = (value: unknown): void => {
    appendFileSync(transcript, `${JSON.stringify(value)}\n`);
  };

  const denials: string[] = [];
  const guard = createBoundaryGuard(request.boundary, (reason) => {
    denials.push(reason);
    record({ type: "boundary_denied", reason });
  });

  let text = "";
  try {
    const stream = query({
      prompt: request.prompt,
      options: {
        cwd: request.cwd,
        maxTurns: request.maxTurns,
        // bypassPermissions with a hook table is the same arrangement the
        // coding run uses: nothing stops to ask, and the hook is the boundary
        // that actually holds.
        permissionMode: "bypassPermissions",
        allowDangerouslySkipPermissions: true,
        persistSession: false,
        // Derived from the boundary, so the list the model is offered and the
        // list the hook enforces cannot disagree.
        disallowedTools: deniedTools(request.boundary),
        env: taskEnv(request.useApiKey === true),
        hooks: { PreToolUse: guardMatchers(guard) },
      },
    });

    for await (const message of stream) {
      record(message);
      if (message.type === "result") {
        if (message.subtype === "success") text = message.result;
        else text = `the task ended as ${message.subtype}`;
        return { ok: message.subtype === "success", text, denials, transcript };
      }
    }
    return { ok: false, text: text || "the task produced no result", denials, transcript };
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    record({ type: "task_error", reason });
    return { ok: false, text: reason, denials, transcript };
  }
}
