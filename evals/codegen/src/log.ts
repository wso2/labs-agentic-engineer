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
 * `log --attempt`: which archived coding run to read. The reading itself is
 * the playground's own developer view (`renderLogView` in
 * `playground/src/engine/log-read.ts`, called by the CLI) — the same three
 * views `play log` prints, over the run dir the archive kept whole under
 * `coding/`.
 */

import { existsSync, readdirSync } from "node:fs";
import { basename, dirname, join } from "node:path";

export type CodingRunLookup = { ok: true; runDir: string } | { ok: false; reason: string };

/**
 * The coding run archived under an attempt: the newest `coding/*-code` dir.
 */
export function resolveCodingRun(attemptDir: string): CodingRunLookup {
  if (/^rewalk-\d+$/.test(basename(attemptDir))) {
    return {
      ok: false,
      reason: `${attemptDir} is a rewalk — it re-walked existing code and ran no coding session. Read its parent's: log --attempt ${dirname(attemptDir)}`,
    };
  }
  const coding = join(attemptDir, "coding");
  const runs = existsSync(coding)
    ? readdirSync(coding, { withFileTypes: true })
        .filter((entry) => entry.isDirectory() && entry.name.endsWith("-code"))
        .map((entry) => entry.name)
        .sort()
    : [];
  const newest = runs.pop();
  if (!newest) {
    return {
      ok: false,
      reason: `${attemptDir} archived no coding run — the attempt ended before \`play code\` wrote one (see its attempt.json and coding/play.log)`,
    };
  }
  return { ok: true, runDir: join(coding, newest) };
}

/** One agent's share of a coding run: the lead, or one fan-out subagent. */
export interface AgentUsage {
  /** `lead`, or the fan-out call's own description ("Build expense-api issue #1"). */
  agent: string;
  toolCalls: number;
  /** Wall clock from the SDK's `task_notification`; undefined for the lead. */
  durationMs?: number;
  cacheRead: number;
  cacheCreation: number;
}

interface UsageFields {
  cache_read_input_tokens?: number;
  cache_creation_input_tokens?: number;
}

/**
 * Tool calls, wall clock and cache tokens per agent, out of the run's
 * `.logs/runtime.log`. The SDK forwards every subagent message with
 * `parent_tool_use_id` (the lead's fan-out call, whose `description` names the
 * subagent) and closes each subagent with a `task_notification` whose `usage`
 * holds its own `tool_uses` and `duration_ms`.
 *
 * Only cache tokens are summed: a line's `output_tokens` is the
 * start-of-message value. One message spans several lines sharing
 * `message.id`, so each id counts once.
 */
export function usageByAgent(runtimeLog: string): AgentUsage[] {
  const names = new Map<string, string>();
  const usage = new Map<string, { agent: string; fields: UsageFields }>();
  const calls = new Map<string, number>();
  const settled = new Map<string, { toolUses: number | undefined; durationMs: number | undefined }>();
  for (const line of runtimeLog.split("\n")) {
    if (!line.trim()) continue;
    let record: {
      type?: string;
      subtype?: string;
      tool_use_id?: string;
      usage?: { tool_uses?: number; duration_ms?: number };
      parent_tool_use_id?: string | null;
      message?: { id?: string; usage?: UsageFields; content?: unknown };
    };
    try {
      record = JSON.parse(line) as typeof record;
    } catch {
      continue;
    }
    if (record.type === "system" && record.subtype === "task_notification" && record.tool_use_id && record.usage) {
      settled.set(record.tool_use_id, { toolUses: record.usage.tool_uses, durationMs: record.usage.duration_ms });
      continue;
    }
    if (record.type !== "assistant" || !record.message) continue;
    const parent = record.parent_tool_use_id ?? undefined;
    const agent = parent ?? "lead";
    for (const block of Array.isArray(record.message.content) ? record.message.content : []) {
      const b = block as { type?: string; id?: string; name?: string; input?: { description?: string } };
      if (b.type !== "tool_use") continue;
      calls.set(agent, (calls.get(agent) ?? 0) + 1);
      if ((b.name === "Agent" || b.name === "Task") && b.id) names.set(b.id, b.input?.description ?? b.id);
    }
    if (record.message.id && record.message.usage) usage.set(record.message.id, { agent, fields: record.message.usage });
  }
  // Keyed by the fan-out call's id while reading (a name can repeat); named at the end.
  const rows = new Map<string, AgentUsage>();
  const row = (key: string): AgentUsage => {
    let r = rows.get(key);
    if (!r) {
      const done = settled.get(key);
      r = {
        agent: key === "lead" ? "lead" : (names.get(key) ?? key),
        toolCalls: done?.toolUses ?? calls.get(key) ?? 0,
        ...(done?.durationMs !== undefined ? { durationMs: done.durationMs } : {}),
        cacheRead: 0,
        cacheCreation: 0,
      };
      rows.set(key, r);
    }
    return r;
  };
  row("lead");
  for (const { agent, fields } of usage.values()) {
    const r = row(agent);
    r.cacheRead += fields.cache_read_input_tokens ?? 0;
    r.cacheCreation += fields.cache_creation_input_tokens ?? 0;
  }
  for (const key of calls.keys()) row(key);
  return [...rows.values()];
}
