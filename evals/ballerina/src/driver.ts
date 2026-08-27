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
 * One case, one Agent SDK session, one scratch package.
 *
 * NOT the runner. `runners/remote-worker`'s `runClaudeQuery` throws without the
 * `aep` workflow skill and carries issue discovery, component contracts and
 * workload authoring — a procedure for building a project, when what is being
 * measured here is how one agent reads one library. Reusing it would put the
 * whole coding workflow between a skill edit and its number.
 *
 * What IS reused is the option shape: `debugQueryOptions` and the log sinks come
 * from the runner, so a transcript recorded here has the same fields as one
 * recorded by a real build — including the reasoning pair (ADR-0002 decision
 * 16) — and the metric extractors work on both. That is what lets
 * `evals/ballerina` re-score a playground run, which is how the fixtures in
 * `test/__fixtures__` exist. Importing them from `logger.js` rather than
 * `runner.js` keeps this package off the runner's dependency graph, which
 * carries the progress feed, the hooks and the workspace guard.
 */

import { query } from "@anthropic-ai/claude-agent-sdk";
import { debugQueryOptions, openDebugSinks, openTaskLog } from "remote-worker/src/lib/logger.js";
import { SESSION, SKILL_NAME } from "./config.js";
import { hostEnv } from "./preflight.js";

export interface SessionRequest {
  /** The scratch Ballerina package. `cwd`, and where `.claude/skills/` was mirrored. */
  workspace: string;
  /** Where `.logs/` goes. Kept apart from the workspace so a `bal build` never sees it. */
  runDir: string;
  prompt: string;
  /** Wall-clock ceiling; a wedged session must not hold a concurrency slot forever. */
  timeoutMs: number;
}

export interface SessionResult {
  /** The transcript, as NDJSON — the input to every path metric. */
  transcript: string;
  /** True when the session hit `timeoutMs` rather than finishing. */
  timedOut: boolean;
  /** A thrown error, if the SDK failed outright. */
  error?: string;
}

export async function runSession(req: SessionRequest): Promise<SessionResult> {
  const log = openTaskLog(req.runDir);
  const sinks = openDebugSinks(log.dir, (line) => line);
  const lines: string[] = [];
  let timedOut = false;
  let error: string | undefined;

  const q = query({
    prompt: req.prompt,
    options: {
      cwd: req.workspace,
      systemPrompt: { type: "preset" as const, preset: SESSION.systemPromptPreset },
      model: SESSION.model,
      // The one skill under test. `settingSources: ["project"]` is what makes
      // the SDK discover the mirror at `<workspace>/.claude/skills` — `cwd`
      // alone is not enough, which cost the platform a release.
      skills: [SKILL_NAME],
      settingSources: ["project"],
      allowedTools: [...SESSION.allowedTools],
      disallowedTools: [...SESSION.disallowedTools],
      permissionMode: "bypassPermissions",
      allowDangerouslySkipPermissions: true,
      persistSession: false,
      strictMcpConfig: true,
      // Host mode, `claude login` only — both credential variables stripped, so
      // the SDK falls through to the OS keychain. See hostEnv.
      env: hostEnv(process.env),
      ...debugQueryOptions(sinks),
    },
  });

  const timer = setTimeout(() => {
    timedOut = true;
    void q.interrupt?.();
  }, req.timeoutMs);

  try {
    for await (const message of q) {
      // Streaming frames are per-token and belong in neither file, for the
      // reason the runner's loop states. They are still what carries the
      // thinking deltas, so they go to the sink that reassembles them.
      if (isStreamFrame(message)) {
        continue;
      }
      const line = JSON.stringify(message);
      lines.push(line);
      log.write(message);
    }
  } catch (e) {
    error = e instanceof Error ? e.message : String(e);
  } finally {
    clearTimeout(timer);
    log.close();
    sinks.close();
  }

  return {
    transcript: lines.join("\n") + "\n",
    timedOut,
    ...(error !== undefined ? { error } : {}),
  };
}

function isStreamFrame(message: unknown): boolean {
  return !!message && typeof message === "object" && (message as { type?: unknown }).type === "stream_event";
}
